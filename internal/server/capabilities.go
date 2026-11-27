package server

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/weather"
)

// What Pocket Search, Pocket Day and Pocket Hub ask Weather (the
// capabilities value in links.go): one route each, in the shape every Pocket
// app answers in, as the request's person.
//
// None of them goes to Open-Meteo. They read the copy of each place's
// forecast the app already keeps (the background check renews it every 30
// minutes for the places with warnings, the pages whenever they are open),
// the warnings it saved and the diary. A copy past its 20 minutes is still
// shown, with the time it is from; a place without a copy shows no weather.
// The janitor drops copies older than two days.

// noteMax is how long a diary note may be (what the diary's own form takes).
const noteMax = 2000

// hasWord says whether there is a letter or a digit to look for: "??" and
// "*" are not words, and would match everything or nothing by accident.
func hasWord(q string) bool {
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// dots joins the parts of a line that are there: "12°, light rain · updated 14:05".
func dots(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// deg is a temperature as the pages show it: "12°" (no unit), "−3°".
func (u units) deg(c float64) string {
	if u.t == "f" {
		c = c*9/5 + 32
	}
	v := math.Round(c)
	if v == 0 {
		v = 0 // no "−0"
	}
	return strings.Replace(fmt.Sprintf("%.0f°", v), "-", "−", 1)
}

// lowerFirst makes "Light rain" read inside a line: "12°, light rain".
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// placeURL opens a place on the Today page.
func placeURL(l store.Location) string {
	if l.IsHome {
		return "/"
	}
	return "/places/" + itoa(l.ID)
}

// shortDate is a date for a line: "Fri 27 Nov", with the year when it is not this one.
func shortDate(date string) string {
	t, err := store.ParseDate(date)
	if err != nil {
		return date
	}
	if t.Year() != time.Now().Year() {
		return t.Format("Mon 2 Jan 2006")
	}
	return t.Format("Mon 2 Jan")
}

// levelWord is a warning's level in the words the Warnings page uses.
func levelWord(level string) string {
	if level == "warning" {
		return "Warning"
	}
	return "Heads-up"
}

// ---------- the stored copy ----------

// wxCopy is the forecast kept for a place and when it was fetched.
type wxCopy struct {
	f  *weather.Forecast
	at time.Time
}

// copyOf is the kept forecast of a place, nil when there is none.
func (s *Server) copyOf(r *http.Request, l store.Location) *wxCopy {
	f, at, ok := stored[*weather.Forecast](r.Context(), s, "fc:"+spot(l.Lat, l.Lon))
	if !ok || f == nil {
		return nil
	}
	return &wxCopy{f: f, at: at}
}

// fresh: younger than an hour, so its "now" is still now.
func (c *wxCopy) fresh() bool { return time.Since(c.at) < time.Hour }

// clock is the wall clock where the place is now, as the forecast writes it.
func (c *wxCopy) clock() string { return time.Now().In(c.f.Location()).Format("2006-01-02T15:04") }

// today is the date where the place is now (an old copy's own "today" may be yesterday).
func (c *wxCopy) today() string { return c.clock()[:10] }

// now is the temperature and the sky now: what was measured when the copy
// is fresh, else the copy's forecast for this hour.
func (c *wxCopy) now() (*float64, string) {
	if c.fresh() {
		return c.f.Now.Temp, c.f.Now.Text
	}
	if h := c.f.HourAt(c.clock()); h != nil {
		return h.Temp, weather.Describe(h.Code, h.IsDay)
	}
	return nil, ""
}

// nowWords is "12°, light rain" (nothing when the copy does not reach now).
func (c *wxCopy) nowWords(u units) string {
	t, text := c.now()
	if text == "—" {
		text = ""
	}
	if t == nil {
		return text
	}
	if text == "" {
		return u.deg(*t)
	}
	return u.deg(*t) + ", " + lowerFirst(text)
}

// todayWords is "8° to 14° today".
func (c *wxCopy) todayWords(u units) string {
	d := c.f.DayOf(c.today())
	if d == nil || d.TMin == nil || d.TMax == nil {
		return ""
	}
	return u.deg(*d.TMin) + " to " + u.deg(*d.TMax) + " today"
}

// updated says when the copy is from: "updated 14:05", "updated yesterday
// 14:05", "updated Wed 25 Nov".
func (c *wxCopy) updated() string {
	at := c.at.In(time.Local)
	switch at.Format("2006-01-02") {
	case store.Today():
		return "updated " + at.Format("15:04")
	case store.AddDays(store.Today(), -1):
		return "updated yesterday " + at.Format("15:04")
	}
	return "updated " + shortDate(at.Format("2006-01-02"))
}

// ---------- search ----------

// pocketSearch answers Pocket Search: the person's saved places by name
// (with the weather now from the kept copy), their warnings of the last 30
// days and the ones ahead by what they say, and the diary days whose note
// has the words. It is not a geocoder: a town that is not saved finds
// nothing.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	if !hasWord(q) {
		return nil, nil
	}
	ctx := r.Context()
	uid := actorID(ctx)
	u := s.unitsFor(ctx, uid)
	low := strings.ToLower(q)
	has := func(parts ...string) bool {
		for _, p := range parts {
			if strings.Contains(strings.ToLower(p), low) {
				return true
			}
		}
		return false
	}
	locs, err := s.store.ListLocations(ctx, uid)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	out := []core.SearchItem{}
	for _, l := range locs {
		names[l.ID] = l.Name
		if !has(l.Name, l.Region, l.Country, l.Note) {
			continue
		}
		score := 2.0
		if has(l.Name) {
			score = 5
		}
		snippet := dots(l.Region, l.Country)
		if c := s.copyOf(r, l); c != nil {
			if now := c.nowWords(u); now != "" {
				snippet = dots(now, c.updated())
			}
		}
		out = append(out, core.SearchItem{ID: "place:" + itoa(l.ID), Title: l.Name, Kind: "place", URL: placeURL(l), Snippet: snippet, Score: score})
	}

	today := store.Today()
	ws, err := s.store.ListWarnings(ctx, uid, store.AddDays(today, -30), true, 0)
	if err != nil {
		return nil, err
	}
	// the ones ahead first, soonest first; then the past ones, newest first
	sort.SliceStable(ws, func(i, j int) bool {
		ai, aj := ws[i].Day >= today, ws[j].Day >= today
		if ai != aj {
			return ai
		}
		return ai && ws[i].Day < ws[j].Day || !ai && ws[i].Day > ws[j].Day
	})
	for _, w := range ws {
		if !has(w.Title, w.Detail) {
			continue
		}
		score := 1.0
		if has(w.Title) {
			score = 4
		}
		if w.Day >= today {
			score += 0.5
		}
		out = append(out, core.SearchItem{ID: "warning:" + itoa(w.ID), Title: w.Title, Kind: "warning", Date: w.Day, URL: "/warnings",
			Snippet: dots(w.Place, shortDate(w.Day), levelWord(w.Level)), Score: score})
	}

	days, err := s.store.NotedDays(ctx, uid)
	if err != nil {
		return nil, err
	}
	for _, d := range days {
		if !has(d.Note) {
			continue
		}
		title, _, _ := strings.Cut(d.Note, "\n")
		out = append(out, core.SearchItem{ID: fmt.Sprintf("diary:%d:%s", d.LocationID, d.Date), Title: title, Kind: "diary", Date: d.Date, URL: "/diary",
			Snippet: dots(names[d.LocationID], shortDate(d.Date), recordText(d, u)), Score: 3})
		if len(out) >= limit*4 {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---------- day ----------

// pocketDay answers Pocket Day with what /day has for the date at the main
// place, without fetching: the diary for a past day, else the kept forecast
// (yesterday to nine days ahead), else a past day looked up before. The
// summary is the day in one line, the metrics its numbers; the items are the
// warnings saved for that day at any of the person's places and the note
// they wrote in the diary, both counted as "notes".
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	today := store.Today()
	if tz != nil {
		today = time.Now().In(tz).Format("2006-01-02")
	}
	home, err := s.store.HomeLocation(ctx, uid)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u := s.unitsFor(ctx, uid)
	day := &core.Day{Date: date, Items: []core.DayItem{}}
	rec, err := s.store.GetDay(ctx, uid, home.ID, date)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if wd, ahead := s.keptDay(r, home, rec, date, today); wd != nil {
		day.Summary = home.Name + ": " + dayText(*wd, u)
		if ahead {
			day.Summary += " (forecast)"
		}
		day.Metrics = dayMetrics(*wd, u)
	}
	ws, err := s.store.RecentWarnings(ctx, uid, date, date)
	if err != nil {
		return nil, err
	}
	for _, w := range ws {
		it := core.DayItem{ID: "warning:" + itoa(w.ID), Title: w.Title, Sub: dots(w.Place, w.Detail), Kind: "warning", URL: "/warnings"}
		if strings.HasPrefix(w.StartsAt, date) {
			it.Time = clock(w.StartsAt)
			if strings.HasPrefix(w.EndsAt, date) {
				it.End = clock(w.EndsAt)
			}
		}
		day.Items = append(day.Items, it)
	}
	if rec != nil && rec.Note != "" {
		title, _, _ := strings.Cut(rec.Note, "\n")
		day.Items = append(day.Items, core.DayItem{ID: fmt.Sprintf("diary:%d:%s", home.ID, date), Title: title, Sub: "Your weather diary, " + home.Name,
			Kind: "diary", Note: rec.Note, URL: "/diary"})
	}
	if day.Summary == "" && len(day.Items) == 0 {
		return nil, nil
	}
	return day, nil
}

// keptDay is the weather of a date at a place from what is kept, and
// whether it is still ahead (a forecast).
func (s *Server) keptDay(r *http.Request, l *store.Location, rec *store.DayRecord, date, today string) (*weather.Day, bool) {
	fromDiary := func() *weather.Day {
		if rec == nil {
			return nil
		}
		d := dayFromRecord(*rec)
		return &d
	}
	fromCopy := func() *weather.Day {
		if c := s.copyOf(r, *l); c != nil {
			return c.f.DayOf(date)
		}
		return nil
	}
	if date >= today {
		if d := fromCopy(); d != nil {
			return d, date > today
		}
		return fromDiary(), false
	}
	if d := fromDiary(); d != nil {
		return d, false
	}
	if d := fromCopy(); d != nil {
		return d, false
	}
	// a day looked up before (the diary's "Any day" and /day keep it a month)
	past, _, ok := stored[[]weather.Day](r.Context(), s, "past:"+spot(l.Lat, l.Lon)+":"+date+":"+date)
	if ok {
		for i := range past {
			if past[i].Date == date {
				return &past[i], false
			}
		}
	}
	return nil, false
}

// dayMetrics are a day's numbers: low, high, rain, and snow or sunshine.
func dayMetrics(d weather.Day, u units) []core.Metric {
	var m []core.Metric
	if d.TMin != nil {
		m = append(m, core.Metric{Label: "Low", Value: u.temp(*d.TMin)})
	}
	if d.TMax != nil {
		m = append(m, core.Metric{Label: "High", Value: u.temp(*d.TMax)})
	}
	if d.Precip != nil {
		m = append(m, core.Metric{Label: "Rain", Value: u.precip(*d.Precip)})
	}
	switch {
	case val0(d.Snow) >= 1:
		m = append(m, core.Metric{Label: "Snow", Value: fmt.Sprintf("%.0f cm", *d.Snow)})
	case d.SunshineH != nil:
		m = append(m, core.Metric{Label: "Sunshine", Value: fmt.Sprintf("%.0f h", *d.SunshineH)})
	}
	return m
}

// ---------- widget ----------

// widgetHours is how many coming hours the widget shows when there is only
// one place, three hours apart; widgetWarnings how many warnings it shows
// at most (the soonest), so the weather keeps its rows.
const (
	widgetHours    = 4
	widgetWarnings = 4
)

// pocketWidget answers Pocket Hub: now and today at the main place from the
// kept copy ("12°, light rain · 8° to 14° today", with the time it is from
// when that is more than an hour ago), then the soonest warnings that are on
// (all of them counted in the metrics), then the other places with their weather now, or the coming hours when there
// is only one place. Someone without a place gets nothing.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	locs, err := s.store.ListLocations(ctx, uid)
	if err != nil || len(locs) == 0 {
		return nil, err
	}
	home, err := s.store.HomeLocation(ctx, uid)
	if err != nil {
		return nil, err
	}
	u := s.unitsFor(ctx, uid)
	w := &core.WidgetData{URL: "/", Items: []core.WidgetItem{}}
	hc := s.copyOf(r, *home)
	if hc != nil {
		w.Line = dots(hc.nowWords(u), hc.todayWords(u))
		if w.Line != "" && !hc.fresh() {
			w.Line = dots(w.Line, hc.updated())
		}
	}
	today := store.Today()
	ws, err := s.store.ListWarnings(ctx, uid, today, true, 0)
	if err != nil {
		return nil, err
	}
	if len(ws) > 0 {
		w.Metrics = []core.Metric{{Label: "Warnings", Value: itoa(int64(len(ws)))}}
	}
	for _, x := range ws {
		if len(w.Items) == widgetWarnings {
			break
		}
		when := dayName(x.Day, today)
		if from, to := clock(x.StartsAt), clock(x.EndsAt); from != "" && to == "00:00" {
			when += " " + from + "–midnight"
		} else if from != "" && to != "" {
			when += " " + from + "–" + to
		} else if from != "" {
			when += " from " + from
		}
		w.Items = append(w.Items, core.WidgetItem{ID: "warning:" + itoa(x.ID), Title: x.Title, Detail: dots(x.Place, when), URL: "/warnings"})
	}
	if len(locs) > 1 {
		for _, l := range locs {
			if len(w.Items) == 8 {
				break
			}
			if l.ID == home.ID {
				continue
			}
			c := s.copyOf(r, l)
			if c == nil {
				continue
			}
			if now := c.nowWords(u); now != "" {
				w.Items = append(w.Items, core.WidgetItem{ID: "place:" + itoa(l.ID), Title: l.Name, Detail: dots(now, c.todayWords(u)), URL: placeURL(l)})
			}
		}
	} else if hc != nil {
		// from the next full hour, three hours apart
		now, next := hc.clock()[:13]+":00", -1
		for i, h := range hc.f.Hours {
			if h.Time > now {
				next = i
				break
			}
		}
		for i, n := next, 0; next >= 0 && i < len(hc.f.Hours) && n < widgetHours && len(w.Items) < 8; i, n = i+3, n+1 {
			h := hc.f.Hours[i]
			detail := weather.Describe(h.Code, h.IsDay)
			if h.Temp != nil {
				detail = u.deg(*h.Temp) + ", " + lowerFirst(detail)
			}
			if p := val0(h.PrecipProb); p >= 20 {
				detail += fmt.Sprintf(", %.0f%% chance of rain", p)
			}
			w.Items = append(w.Items, core.WidgetItem{ID: "hour:" + h.Time, Title: clock(h.Time), Detail: detail, URL: "/"})
		}
	}
	if w.Line == "" && len(w.Items) == 0 {
		return nil, nil
	}
	return w, nil
}

// ---------- capture ----------

// pocketCapture keeps a quick line in the weather diary: under the note of
// today (or the date sent) at the main place, on a line of its own, so what
// is written there stays. The diary keeps the days of the places with
// warnings since they were added; a day not in it is refused with a
// sentence that says why. The same request id answers the same and adds
// nothing (the receipt is written with the note).
func (s *Server) pocketCapture(r *http.Request, in core.CaptureInput) (*core.CaptureResult, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	op, err := core.NewOperation(fmt.Sprintf("weather:user:%d:%s", uid, core.CapturePath), in.RequestID, in)
	if err != nil {
		return nil, err
	}
	var res core.CaptureResult
	replay := func() (*core.CaptureResult, error) {
		found, err := op.Replay(ctx, s.store.DB, &res)
		switch {
		case errors.Is(err, core.ErrOperationConflict):
			return nil, fmt.Errorf("%w: this was already kept with other words", core.ErrCapture)
		case err != nil || !found:
			return nil, err
		}
		return &res, nil
	}
	if kept, err := replay(); kept != nil || err != nil {
		return kept, err
	}

	var parts []string
	for _, p := range []string{in.Title, in.Text} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	line := strings.Join(parts, "\n")
	today := store.Today()
	date := firstNonEmpty(in.Date, today)
	if date > today {
		return nil, fmt.Errorf("%w: the weather diary only has days that have begun; pick today or an earlier day", core.ErrCapture)
	}
	home, err := s.store.HomeLocation(ctx, uid)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: add a place in Pocket Weather first; the diary is kept for your places", core.ErrCapture)
	}
	if err != nil {
		return nil, err
	}
	if !home.Warn {
		return nil, fmt.Errorf("%w: the weather diary is kept for the places you get warnings for; turn warnings on for %s first", core.ErrCapture, home.Name)
	}
	if _, err := s.store.GetDay(ctx, uid, home.ID, date); errors.Is(err, store.ErrNotFound) && date >= store.AddDays(today, -1) {
		// today or yesterday before the next check wrote it: write it from the kept forecast, as the check would
		if c := s.copyOf(r, *home); c != nil {
			if d := c.f.DayOf(date); d != nil {
				rec := recordOf(home.ID, *d)
				if air, _, ok := stored[*weather.Air](ctx, s, "aq:"+spot(home.Lat, home.Lon)); ok {
					if aqi, pollen := air.DayMax(date); aqi != nil || len(pollen) > 0 {
						rec.AQI, rec.Pollen = aqi, pollen
					}
				}
				if err := s.store.SaveDay(ctx, uid, rec); err != nil {
					return nil, err
				}
			}
		}
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	_, err = s.store.AppendDayNote(ctx, uid, home.ID, date, line, noteMax, func(tx *sql.Tx) error {
		res = core.CaptureResult{ID: fmt.Sprintf("diary:%d:%s", home.ID, date), Title: line, URL: "/diary"}
		return op.Save(ctx, tx, res)
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, fmt.Errorf("%w: %s is not in the weather diary for %s; the diary keeps the days since the place was added", core.ErrCapture, shortDate(date), home.Name)
	case errors.Is(err, store.ErrNoteFull):
		return nil, fmt.Errorf("%w: the note for %s is full (%d characters at most); shorten it in the diary first", core.ErrCapture, shortDate(date), noteMax)
	case err != nil:
		// the same line sent twice at once: the other one kept it
		if kept, rerr := replay(); kept != nil || rerr != nil {
			return kept, rerr
		}
		return nil, err
	}
	return &res, nil
}
