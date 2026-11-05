package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-weather/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/weather"
)

// ---- what the pages and the other apps read ----

// placeNow is one place with the weather now and today.
type placeNow struct {
	store.Location
	Now      *weather.Now `json:"now"`
	Today    *weather.Day `json:"today"`
	Tomorrow *weather.Day `json:"tomorrow"`
	Warnings int          `json:"warnings"`
	Stale    bool         `json:"stale,omitempty"`
	Error    string       `json:"error,omitempty"`
}

func (s *Server) placeNow(ctx context.Context, l store.Location, warns []store.Warning) placeNow {
	pn := placeNow{Location: l}
	f, stale, err := s.forecastOf(ctx, &l)
	if err != nil {
		pn.Error = err.Error()
		return pn
	}
	pn.Now, pn.Today, pn.Stale = &f.Now, f.Today(), stale
	pn.Tomorrow = f.DayOf(store.AddDays(f.LocalDate(), 1))
	for _, w := range warns {
		if w.LocationID == l.ID {
			pn.Warnings++
		}
	}
	return pn
}

// handleOverview is every place with now and today, and the warnings.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	s.checkUser(ctx, uid, false)
	locs, err := s.store.ListLocations(ctx, uid)
	if err != nil {
		s.fail(w, err, "places")
		return
	}
	warns, _ := s.store.ListWarnings(ctx, uid, store.Today(), true, 0)
	out := make([]placeNow, 0, len(locs))
	for _, l := range locs {
		out = append(out, s.placeNow(ctx, l, warns))
	}
	writeJSON(w, http.StatusOK, map[string]any{"places": out, "warnings": warns, "today": store.Today()})
}

// handleSummary is home in a nutshell: Homepage widgets, Pocket Day, Pocket AI.
func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	s.checkUser(ctx, uid, false)
	st, _ := s.store.GetSettings(ctx, uid)
	home, err := s.locationParam(r, uid)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]any{"place": nil, "text": "No place yet: add one in Pocket Weather."})
			return
		}
		s.fail(w, err, "summary")
		return
	}
	f, stale, err := s.forecastOf(ctx, home)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	warns, _ := s.store.ListWarnings(ctx, uid, f.LocalDate(), true, home.ID)
	u := unitsOf(st)
	today, tomorrow := f.Today(), f.DayOf(store.AddDays(f.LocalDate(), 1))
	out := map[string]any{"place": home, "now": f.Now, "today": today, "tomorrow": tomorrow, "warnings": warns, "stale": stale,
		"text": daySentence(f, u), "units": map[string]string{"temp": st.TempUnit, "wind": st.WindUnit, "precip": st.PrecipUnit},
		"url": strings.TrimRight(s.cfg.BaseURL, "/") + "/"}
	if air, _, err := s.airAt(ctx, home.Lat, home.Lon); err == nil {
		out["air"] = airSummary(air, f.LocalDate())
	}
	writeJSON(w, http.StatusOK, out)
}

// daySentence is today in one line: "3 to 11 °C, sunny; rain from 15:00 (80 %)."
func daySentence(f *weather.Forecast, u units) string {
	d := f.Today()
	if d == nil {
		return ""
	}
	s := fmt.Sprintf("%s, %s to %s", d.Text, u.temp(val0(d.TMin)), u.temp(val0(d.TMax)))
	first, _, _, _, n := span(f, d.Date, func(h weather.Hour) bool { return val0(h.PrecipProb) >= 50 && val0(h.Precip) >= 0.2 }, func(h weather.Hour) float64 { return val0(h.PrecipProb) })
	if n > 0 {
		what := "rain"
		if h := f.HourAt(first); h != nil && val0(h.Snow) > 0 {
			what = "snow"
		}
		if first <= f.LocalHour() {
			s += "; " + what + " now"
		} else {
			s += fmt.Sprintf("; %s from %s", what, clock(first))
		}
	}
	if g := val0(d.Gust); g >= 50 {
		s += ", gusts up to " + u.wind(g)
	}
	return s + "."
}

// airView is air quality now and each day's worst, with pollen levels.
type airView struct {
	AQI     *float64          `json:"aqi"`
	Band    string            `json:"band"`
	PM25    *float64          `json:"pm2_5"`
	PM10    *float64          `json:"pm10"`
	Days    []airDay          `json:"days"`
	Hours   []weather.AirHour `json:"hours,omitempty"`
	Fetched string            `json:"fetched_at"`
}

type airDay struct {
	Date   string             `json:"date"`
	AQI    *float64           `json:"aqi"`
	Band   string             `json:"band"`
	Pollen map[string]float64 `json:"pollen"`
	Levels map[string]string  `json:"levels"`
}

func airSummary(a *weather.Air, today string) airView {
	v := airView{AQI: a.Now.AQI, PM25: a.Now.PM25, PM10: a.Now.PM10, Days: []airDay{}, Fetched: a.Fetched}
	if a.Now.AQI != nil {
		v.Band = weather.AQIBand(*a.Now.AQI)
	}
	for i := 0; i < 4; i++ {
		date := store.AddDays(today, i)
		aqi, pollen := a.DayMax(date)
		if aqi == nil && len(pollen) == 0 {
			continue
		}
		d := airDay{Date: date, AQI: aqi, Pollen: pollen, Levels: map[string]string{}}
		if aqi != nil {
			d.Band = weather.AQIBand(*aqi)
		}
		for k, val := range pollen {
			d.Levels[k] = weather.PollenLevel(k, val)
		}
		v.Days = append(v.Days, d)
	}
	return v
}

// locationParam is ?location=ID, else home.
func (s *Server) locationParam(r *http.Request, uid int64) (*store.Location, error) {
	if id, _ := strconv.ParseInt(r.URL.Query().Get("location"), 10, 64); id > 0 {
		return s.store.GetLocation(r.Context(), uid, id)
	}
	return s.store.HomeLocation(r.Context(), uid)
}

// coordsParam is ?lat=&lon= (any spot), ok false when not given.
func coordsParam(r *http.Request) (float64, float64, bool, error) {
	q := r.URL.Query()
	if q.Get("lat") == "" && q.Get("lon") == "" {
		return 0, 0, false, nil
	}
	lat, err1 := strconv.ParseFloat(q.Get("lat"), 64)
	lon, err2 := strconv.ParseFloat(firstNonEmpty(q.Get("lon"), q.Get("lng")), 64)
	if err1 != nil || err2 != nil || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, true, errors.New("lat is −90 to 90 and lon −180 to 180")
	}
	return lat, lon, true, nil
}

// handleForecast is the full forecast of one place (?location=ID, home by
// default) or of any spot (?lat=&lon=): now, 48 hours, 10 days, air and
// pollen, and the place's warnings.
func (s *Server) handleForecast(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	lat, lon, given, err := coordsParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var loc *store.Location
	if !given {
		if loc, err = s.locationParam(r, uid); err != nil {
			s.fail(w, err, "place")
			return
		}
		lat, lon = loc.Lat, loc.Lon
		s.checkUser(ctx, uid, false)
	}
	f, stale, err := s.forecastAt(ctx, lat, lon)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	hours := 48
	if n := queryInt(r, "hours"); n > 0 && n <= 240 {
		hours = n
	}
	out := map[string]any{"location": loc, "now": f.Now, "hours": f.HoursFrom(hours), "days": f.Days, "today": f.LocalDate(), "timezone": f.Timezone,
		"elevation": f.Elevation, "fetched_at": f.Fetched, "stale": stale}
	if air, _, err := s.airAt(ctx, lat, lon); err == nil {
		av := airSummary(air, f.LocalDate())
		if queryBool(r, "air_hours") {
			av.Hours = air.Hours
		}
		out["air"] = av
	}
	if loc != nil {
		ws, _ := s.store.ListWarnings(ctx, uid, f.LocalDate(), true, loc.ID)
		out["warnings"] = ws
	} else {
		st, _ := s.store.GetSettings(ctx, uid)
		air, _, _ := s.airAt(ctx, lat, lon)
		found := evaluate(f, air, st, unitsOf(st))
		ws := make([]store.Warning, 0, len(found))
		for _, fd := range found {
			v := fd.Value
			ws = append(ws, store.Warning{Kind: fd.Kind, Level: fd.Level, Day: fd.Day, StartsAt: fd.Start, EndsAt: fd.End, Title: fd.Title, Detail: fd.Detail, Value: &v})
		}
		out["warnings"] = ws
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAir is air quality and pollen for one place, by the hour.
func (s *Server) handleAir(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lat, lon, given, err := coordsParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !given {
		loc, err := s.locationParam(r, actorID(ctx))
		if err != nil {
			s.fail(w, err, "place")
			return
		}
		lat, lon = loc.Lat, loc.Lon
	}
	a, _, err := s.airAt(ctx, lat, lon)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	v := airSummary(a, store.Today())
	v.Hours = a.Hours
	writeJSON(w, http.StatusOK, v)
}

// handleWarnings is what is active now, and (history=1) the last 30 days.
func (s *Server) handleWarnings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	s.checkUser(ctx, uid, queryBool(r, "fresh"))
	today := store.Today()
	active, err := s.store.ListWarnings(ctx, uid, today, true, int64(queryInt(r, "location")))
	if err != nil {
		s.fail(w, err, "warnings")
		return
	}
	out := map[string]any{"active": active, "today": today}
	if queryBool(r, "history") {
		past, _ := s.store.RecentWarnings(ctx, uid, store.AddDays(today, -30), store.AddDays(today, -1))
		out["history"] = past
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCheck runs the warnings check now ("Check again").
func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	s.checkUser(ctx, uid, true)
	s.forgetPlans(ctx, uid)
	active, _ := s.store.ListWarnings(ctx, uid, store.Today(), true, 0)
	writeJSON(w, http.StatusOK, map[string]any{"active": active})
}

// ---- plans and advice ----

func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.plans(ctx, actorID(ctx), queryBool(r, "fresh"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleMarkAdvice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key   string `json:"key"`
		State string `json:"state"` // done | hidden | "" (undo)
	}
	if err := decode(r, &in); err != nil || in.Key == "" || len(in.Key) > 200 || !oneOf(in.State, []string{"done", "hidden", ""}) {
		writeErr(w, http.StatusBadRequest, "key and a state of done, hidden or empty")
		return
	}
	ctx := r.Context()
	if err := s.store.SetMark(ctx, actorID(ctx), in.Key, in.State, ""); err != nil {
		s.fail(w, err, "advice")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAdviceTask makes a piece of advice into a task in Pocket Tasks.
func (s *Server) handleAdviceTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Key string `json:"key"`
	}
	if err := decode(r, &in); err != nil || in.Key == "" {
		writeErr(w, http.StatusBadRequest, "which advice?")
		return
	}
	p, err := s.plans(ctx, uid, false)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	var a *Advice
	for i := range p.Advice {
		if p.Advice[i].Key == in.Key {
			a = &p.Advice[i]
		}
	}
	if a == nil {
		writeErr(w, http.StatusNotFound, "that advice is no longer there")
		return
	}
	t := pocket.Task{Title: a.Title, Notes: a.Detail, Due: a.Day, URL: strings.TrimRight(s.cfg.BaseURL, "/") + "/plans", Tags: []string{"weather"}}
	if a.Task != nil {
		t.Title, t.Due = a.Task.Title, a.Task.Due
	}
	var id int64
	if p.Demo {
		id = time.Now().Unix() % 100000
	} else {
		sib, err := s.clients(ctx, uid).conn(ctx, appTasks, scopeTasksWrite)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if id, err = sib.client.CreateTask(ctx, t); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "Pocket Tasks: " + err.Error()})
			return
		}
	}
	_ = s.store.SetMark(ctx, uid, a.Key, "task", itoa(id))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task_id": id, "open_url": s.siblingLink(ctx, uid, appTasks, "")})
}

// ---- the diary ----

// handleDay is what the weather was (or will be) on a date at home or a
// place: the diary when it has the day, else the forecast or the archive.
// This is what Pocket Day writes into the journal.
func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	date := chi.URLParam(r, "date")
	if _, err := store.ParseDate(date); err != nil {
		writeErr(w, http.StatusBadRequest, "date is YYYY-MM-DD")
		return
	}
	lat, lon, given, err := coordsParam(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var loc *store.Location
	if !given {
		if loc, err = s.locationParam(r, uid); err != nil {
			s.fail(w, err, "place")
			return
		}
		lat, lon = loc.Lat, loc.Lon
		if rec, err := s.store.GetDay(ctx, uid, loc.ID, date); err == nil && date < store.Today() {
			writeJSON(w, http.StatusOK, map[string]any{"location": loc, "date": date, "source": "diary", "day": dayFromRecord(*rec), "note": rec.Note,
				"aqi": rec.AQI, "pollen": rec.Pollen, "text": recordText(*rec, s.unitsFor(ctx, uid))})
			return
		}
	}
	day, source, err := s.anyDay(ctx, lat, lon, date)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if day == nil {
		writeErr(w, http.StatusNotFound, "no weather for that day (the forecast goes 10 days ahead)")
		return
	}
	out := map[string]any{"location": loc, "date": date, "source": source, "day": day, "text": dayText(*day, s.unitsFor(ctx, uid))}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) unitsFor(ctx context.Context, uid int64) units {
	st, _ := s.store.GetSettings(ctx, uid)
	return unitsOf(st)
}

// anyDay finds one day at a spot: forecast (yesterday to 10 days ahead),
// else the past.
func (s *Server) anyDay(ctx context.Context, lat, lon float64, date string) (*weather.Day, string, error) {
	today := store.Today()
	if date >= store.AddDays(today, -1) {
		f, _, err := s.forecastAt(ctx, lat, lon)
		if err != nil {
			return nil, "", err
		}
		return f.DayOf(date), "forecast", nil
	}
	days, err := s.pastDays(ctx, lat, lon, date, date)
	if err != nil {
		return nil, "", err
	}
	for i := range days {
		if days[i].Date == date {
			return &days[i], "archive", nil
		}
	}
	return nil, "archive", nil
}

func dayFromRecord(r store.DayRecord) weather.Day {
	return weather.Day{Date: r.Date, Code: r.Code, Condition: weather.Condition(r.Code), Text: weather.Describe(r.Code, true), TMin: r.TMin, TMax: r.TMax,
		Precip: r.Precip, Snow: r.Snow, Wind: r.Wind, Gust: r.Gust, UV: r.UV, SunshineH: r.SunshineH}
}

func dayText(d weather.Day, u units) string {
	s := fmt.Sprintf("%s, %s to %s", d.Text, u.temp(val0(d.TMin)), u.temp(val0(d.TMax)))
	if p := val0(d.Precip); p >= 1 {
		s += ", " + u.precip(p)
		if sn := val0(d.Snow); sn >= 1 {
			s += fmt.Sprintf(" (%.0f cm snow)", sn)
		}
	}
	return s
}

func recordText(r store.DayRecord, u units) string { return dayText(dayFromRecord(r), u) }

// handleDiary is a month of the diary for a place, with last year's same
// month to compare when the diary has it.
func (s *Server) handleDiary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	loc, err := s.locationParam(r, uid)
	if err != nil {
		s.fail(w, err, "place")
		return
	}
	month := r.URL.Query().Get("month")
	if month == "" {
		month = store.Today()[:7]
	}
	start, err := time.Parse("2006-01", month)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "month is YYYY-MM")
		return
	}
	from, to := start.Format("2006-01-02"), start.AddDate(0, 1, -1).Format("2006-01-02")
	days, err := s.store.ListDays(ctx, uid, loc.ID, from, to)
	if err != nil {
		s.fail(w, err, "diary")
		return
	}
	// last year's same days: the whole month, or up to today's date for this month
	ly := start.AddDate(-1, 0, 0)
	lyTo := ly.AddDate(0, 1, -1).Format("2006-01-02")
	if today := store.Today(); today < to {
		lyTo = ly.Format("2006-01-") + today[8:]
	}
	last, _ := s.store.ListDays(ctx, uid, loc.ID, ly.Format("2006-01-02"), lyTo)
	writeJSON(w, http.StatusOK, map[string]any{"location": loc, "month": month, "days": days, "stats": diaryStats(days), "last_year": diaryStats(last)})
}

type stats struct {
	Days     int      `json:"days"`
	TMin     *float64 `json:"t_min"` // coldest
	TMax     *float64 `json:"t_max"` // warmest
	MeanHigh *float64 `json:"mean_high"`
	MeanLow  *float64 `json:"mean_low"`
	Precip   float64  `json:"precip"`
	WetDays  int      `json:"wet_days"`
	Snow     float64  `json:"snow"`
	SunH     float64  `json:"sunshine_h"`
	Frost    int      `json:"frost_days"`
}

func diaryStats(days []store.DayRecord) stats {
	st := stats{Days: len(days)}
	var hi, lo float64
	var nh, nl int
	for _, d := range days {
		if d.TMax != nil {
			hi += *d.TMax
			nh++
			if st.TMax == nil || *d.TMax > *st.TMax {
				v := *d.TMax
				st.TMax = &v
			}
		}
		if d.TMin != nil {
			lo += *d.TMin
			nl++
			if st.TMin == nil || *d.TMin < *st.TMin {
				v := *d.TMin
				st.TMin = &v
			}
			if *d.TMin < 0 {
				st.Frost++
			}
		}
		st.Precip += val0(d.Precip)
		st.Snow += val0(d.Snow)
		st.SunH += val0(d.SunshineH)
		if val0(d.Precip) >= 1 {
			st.WetDays++
		}
	}
	if nh > 0 {
		v := round1(hi / float64(nh))
		st.MeanHigh = &v
	}
	if nl > 0 {
		v := round1(lo / float64(nl))
		st.MeanLow = &v
	}
	st.Precip, st.Snow, st.SunH = round1(st.Precip), round1(st.Snow), round1(st.SunH)
	return st
}

func round1(v float64) float64 { return float64(int64(v*10+sign(v)*0.5)) / 10 }

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// handleDayNote saves the person's own words for a diary day.
func (s *Server) handleDayNote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Note string `json:"note"`
	}
	if err := decode(r, &in); err != nil || len(in.Note) > 2000 {
		writeErr(w, http.StatusBadRequest, "a note of at most 2000 characters")
		return
	}
	date := chi.URLParam(r, "date")
	if err := s.store.SetDayNote(ctx, uid, idParam(r, "id"), date, in.Note); err != nil {
		s.fail(w, err, "note")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
