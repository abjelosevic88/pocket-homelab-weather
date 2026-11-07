package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-weather/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/weather"
)

// ---- "for your plans": the weather meets the other Pocket apps ----
//
// Your events of the next three days with the weather they get (Pocket
// Calendar), the forecast for your coming trips (Pocket Travel), and advice
// for what lives in the other apps: outdoor plants (Pocket Home), tyres and
// the windscreen (Pocket Garage), dog walks (Pocket Pets), and your coming
// games at their venue (Pocket League). Everything is worked out from the
// home forecast unless an event, game or trip says where it is. Read on
// request and kept ten minutes per person.

// Advice is one thing to do because of the weather.
type Advice struct {
	Key    string     `json:"key"` // stable, for done / hide / task
	App    string     `json:"app"` // calendar | travel | home | garage | pets | league | weather
	Icon   string     `json:"icon"`
	Level  string     `json:"level"` // info | warn
	Day    string     `json:"day"`
	Title  string     `json:"title"`
	Detail string     `json:"detail"`
	Link   string     `json:"link,omitempty"`
	Task   *TaskDraft `json:"task,omitempty"`
	Mark   string     `json:"mark,omitempty"` // done | hidden | task
}

// TaskDraft is the task "Add as a task" would make.
type TaskDraft struct {
	Title string `json:"title"`
	Due   string `json:"due"`
}

// PlanEvent is an event with the weather it gets.
type PlanEvent struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Start    string   `json:"start"` // local wall clock, or YYYY-MM-DD all day
	End      string   `json:"end"`
	AllDay   bool     `json:"all_day"`
	Location string   `json:"location"`
	Place    string   `json:"place"` // the weather is for
	Code     int      `json:"code"`
	Cond     string   `json:"condition"`
	Temp     *float64 `json:"temp"`
	TMin     *float64 `json:"t_min,omitempty"`
	TMax     *float64 `json:"t_max,omitempty"`
	Prob     *float64 `json:"precip_prob"`
	Gust     *float64 `json:"gust"`
	Flags    []string `json:"flags"` // rain | storm | wind | heat | cold | snow
	Note     string   `json:"note"`
	Link     string   `json:"link,omitempty"`
	// a game from Pocket League
	Players string `json:"players,omitempty"` // "3 in, 4 needed · you: in"
	Indoor  bool   `json:"indoor,omitempty"`  // played inside: the weather is only for getting there
}

// PlanTrip is a coming trip with its forecast (or last year's weather).
type PlanTrip struct {
	ID          int64         `json:"id"`
	Name        string        `json:"name"`
	Destination string        `json:"destination"`
	Start       string        `json:"start_date"`
	End         string        `json:"end_date"`
	Place       string        `json:"place"`
	Lat         float64       `json:"lat"`
	Lon         float64       `json:"lon"`
	Days        []weather.Day `json:"days"`      // forecast days within the trip
	LastYear    []weather.Day `json:"last_year"` // the same dates a year ago, when the trip is too far ahead
	Summary     string        `json:"summary"`
	Link        string        `json:"link,omitempty"`
}

// AppState says how one linked app answered.
type AppState struct {
	On        bool   `json:"on"` // switched on in Settings
	Connected bool   `json:"connected"`
	Detail    string `json:"detail,omitempty"`
	Count     int    `json:"count"`
	OpenURL   string `json:"open_url,omitempty"`
}

// Plans is everything for the Plans page.
type Plans struct {
	Place  string              `json:"place"`
	Events []PlanEvent         `json:"events"`
	Games  []PlanEvent         `json:"games"`
	Trips  []PlanTrip          `json:"trips"`
	Advice []Advice            `json:"advice"`
	Apps   map[string]AppState `json:"apps"`
	Demo   bool                `json:"demo,omitempty"`
}

func (s *Server) plans(ctx context.Context, uid int64, fresh bool) (*Plans, error) {
	key := fmt.Sprintf("plans:%d", uid)
	if !fresh {
		if raw, at, ok := s.store.CacheGet(ctx, key); ok {
			if t, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(t) < 10*time.Minute {
				var p Plans
				if json.Unmarshal([]byte(raw), &p) == nil {
					s.applyMarks(ctx, uid, &p)
					return &p, nil
				}
			}
		}
	}
	p, err := s.buildPlans(ctx, uid)
	if err != nil {
		return nil, err
	}
	if b, err := json.Marshal(p); err == nil {
		_ = s.store.CachePut(ctx, key, string(b))
	}
	s.applyMarks(ctx, uid, p)
	return p, nil
}

func (s *Server) applyMarks(ctx context.Context, uid int64, p *Plans) {
	marks, _ := s.store.Marks(ctx, uid)
	for i := range p.Advice {
		p.Advice[i].Mark = marks[p.Advice[i].Key].State
	}
}

// forgetPlans drops the cached plans (a setting or a mark changed).
func (s *Server) forgetPlans(ctx context.Context, uid int64) {
	_ = s.store.CacheDelete(ctx, fmt.Sprintf("plans:%d", uid))
}

func (s *Server) buildPlans(ctx context.Context, uid int64) (*Plans, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	home, err := s.store.HomeLocation(ctx, uid)
	if err != nil {
		return &Plans{Events: []PlanEvent{}, Games: []PlanEvent{}, Trips: []PlanTrip{}, Advice: []Advice{}, Apps: map[string]AppState{}}, nil
	}
	f, _, err := s.forecastOf(ctx, home)
	if err != nil {
		return nil, err
	}
	src := s.planSource(ctx, uid)
	p := &Plans{Place: home.Name, Events: []PlanEvent{}, Games: []PlanEvent{}, Trips: []PlanTrip{}, Advice: []Advice{}, Apps: map[string]AppState{}, Demo: src.demo}
	u := unitsOf(st)
	today := f.LocalDate()

	state := func(a pocketApp, on bool, n int, err error) {
		as := AppState{On: on, Count: n}
		if on {
			as.Connected = err == nil
			if err != nil {
				as.Detail = err.Error()
			}
			as.OpenURL = s.siblingLink(ctx, uid, a, "")
		}
		p.Apps[a.slug] = as
	}

	// Pocket Calendar
	if st.CalendarWeather {
		evs, err := src.events(ctx, today, store.AddDays(today, 3))
		state(appCalendar, true, len(evs), err)
		for _, e := range evs {
			pe := s.planEvent(ctx, uid, st, home, f, e, u)
			if pe == nil {
				continue
			}
			p.Events = append(p.Events, *pe)
			if len(pe.Flags) > 0 && pe.Note != "" {
				day := pe.Start[:min(10, len(pe.Start))]
				p.Advice = append(p.Advice, Advice{Key: "event:" + pe.ID + ":" + day, App: "calendar", Icon: "calendar", Level: "warn", Day: day,
					Title: pe.Title + ", " + dayName(day, today) + whenClock(pe), Detail: pe.Note, Link: pe.Link})
			}
		}
	} else {
		state(appCalendar, false, 0, nil)
	}

	// Pocket Travel
	if st.TravelWeather {
		trips, err := src.trips(ctx)
		n := 0
		for _, t := range trips {
			pt := s.planTrip(ctx, uid, st, t, today, u)
			if pt == nil {
				continue
			}
			n++
			p.Trips = append(p.Trips, *pt)
			if pt.Summary != "" && len(pt.Days) > 0 && pt.Start <= store.AddDays(today, 7) {
				p.Advice = append(p.Advice, Advice{Key: fmt.Sprintf("trip:%d:%s", t.ID, pt.Start), App: "travel", Icon: "plane", Level: "info", Day: pt.Start,
					Title: "Packing for " + firstNonEmpty(pt.Place, pt.Name), Detail: pt.Summary, Link: pt.Link,
					Task: &TaskDraft{Title: "Pack for " + firstNonEmpty(pt.Place, pt.Name) + ": " + packList(pt.Days), Due: store.AddDays(pt.Start, -1)}})
			}
		}
		state(appTravel, true, n, err)
	} else {
		state(appTravel, false, 0, nil)
	}

	// Pocket Home: outdoor plants
	if st.HomePlants {
		plants, err := src.plants(ctx)
		var out []pocket.Plant
		for _, pl := range plants {
			if !pl.Archived && outdoor(pl) {
				out = append(out, pl)
			}
		}
		state(appHome, true, len(out), err)
		if len(out) > 0 {
			p.Advice = append(p.Advice, plantAdvice(out, f, today, u, s.siblingLink(ctx, uid, appHome, "/plants"))...)
		}
	} else {
		state(appHome, false, 0, nil)
	}

	// Pocket Garage: tyres and the windscreen
	if st.GarageTyres {
		cars, err := src.vehicles(ctx)
		state(appGarage, true, len(cars), err)
		if len(cars) > 0 {
			p.Advice = append(p.Advice, carAdvice(cars, f, today, u, s.siblingLink(ctx, uid, appGarage, ""))...)
		}
	} else {
		state(appGarage, false, 0, nil)
	}

	// Pocket Pets: dog walks
	if st.PetsWalks {
		pets, err := src.pets(ctx)
		var dogs []pocket.Pet
		for _, pt := range pets {
			if !pt.Archived && pt.PassedOn == "" && isDog(pt.Species) {
				dogs = append(dogs, pt)
			}
		}
		state(appPets, true, len(dogs), err)
		if len(dogs) > 0 {
			p.Advice = append(p.Advice, dogAdvice(dogs, f, today, u, s.siblingLink(ctx, uid, appPets, ""))...)
		}
	} else {
		state(appPets, false, 0, nil)
	}

	// Pocket League: the coming games of the next week at their venue
	if st.LeagueWeather {
		games, outdoor, err := src.games(ctx)
		n := 0
		for _, g := range games {
			pe := s.planGame(ctx, st, home, f, g, outdoor, today, u, s.siblingLink(ctx, uid, appLeague, fmt.Sprintf("/games/%d", g.ID)))
			if pe == nil {
				continue
			}
			n++
			p.Games = append(p.Games, *pe)
			if len(pe.Flags) > 0 && pe.Note != "" && g.MyAnswer != "out" {
				day := pe.Start[:min(10, len(pe.Start))]
				p.Advice = append(p.Advice, Advice{Key: fmt.Sprintf("game:%d:%s", g.ID, day), App: "league", Icon: "trophy", Level: "warn", Day: day,
					Title: pe.Title + ", " + dayName(day, today) + whenClock(pe), Detail: pe.Note, Link: pe.Link})
			}
		}
		state(appLeague, true, n, err)
	} else {
		state(appLeague, false, 0, nil)
	}

	// Pocket Movement and Tasks: only whether they answer
	mv, _, mvDetail := s.connStatus(ctx, s.userOf(ctx, uid), st, appMovement)
	p.Apps["movement"] = AppState{On: st.FollowMe, Connected: mv != modeOff && mvDetail == "", Detail: mvDetail}
	tk, via, tkDetail := s.connStatus(ctx, s.userOf(ctx, uid), st, appTasks)
	p.Apps["tasks"] = AppState{On: tk != modeOff, Connected: via != "" || src.demo, Detail: tkDetail}

	sort.SliceStable(p.Advice, func(a, b int) bool {
		if p.Advice[a].Day != p.Advice[b].Day {
			return p.Advice[a].Day < p.Advice[b].Day
		}
		return p.Advice[a].Level == "warn" && p.Advice[b].Level != "warn"
	})
	sort.SliceStable(p.Events, func(a, b int) bool { return p.Events[a].Start < p.Events[b].Start })
	sort.SliceStable(p.Games, func(a, b int) bool { return p.Games[a].Start < p.Games[b].Start })
	return p, nil
}

func (s *Server) userOf(ctx context.Context, uid int64) *store.User {
	u, _ := s.store.GetUser(ctx, uid)
	return u
}

func whenClock(e *PlanEvent) string {
	if e.AllDay || len(e.Start) < 16 {
		return ""
	}
	return " at " + e.Start[11:16]
}

// ---- where the plans come from ----

// planSource reads the person's linked apps (or the demo's made-up ones).
type planSource struct {
	demo     bool
	events   func(ctx context.Context, from, to string) ([]pocket.Event, error)
	trips    func(ctx context.Context) ([]pocket.Trip, error)
	plants   func(ctx context.Context) ([]pocket.Plant, error)
	vehicles func(ctx context.Context) ([]carInfo, error)
	pets     func(ctx context.Context) ([]pocket.Pet, error)
	// games are the coming games; outdoor says per group whether it plays
	// outside (a group not in it counts as outside)
	games func(ctx context.Context) (games []pocket.Game, outdoor map[int64]bool, err error)
}

// carInfo is a vehicle with when its tyres were last changed.
type carInfo struct {
	pocket.Vehicle
	LastTyres string `json:"last_tyres"`
}

func (s *Server) planSource(ctx context.Context, uid int64) planSource {
	if s.cfg.DemoData && !s.core.Configured() && uid == s.store.OwnerID(ctx) {
		return demoPlanSource()
	}
	c := s.clients(ctx, uid)
	conn := func(a pocketApp, scope string) (*pocket.Client, error) {
		sib, err := c.conn(ctx, a, scope)
		if err != nil {
			return nil, err
		}
		return sib.client, nil
	}
	return planSource{
		events: func(ctx context.Context, from, to string) ([]pocket.Event, error) {
			cl, err := conn(appCalendar, scopeCalendarRead)
			if err != nil {
				return nil, err
			}
			return cl.Events(ctx, from, to)
		},
		trips: func(ctx context.Context) ([]pocket.Trip, error) {
			cl, err := conn(appTravel, scopeTripsRead)
			if err != nil {
				return nil, err
			}
			return cl.Trips(ctx)
		},
		plants: func(ctx context.Context) ([]pocket.Plant, error) {
			cl, err := conn(appHome, scopeHomeRead)
			if err != nil {
				return nil, err
			}
			return cl.Plants(ctx)
		},
		vehicles: func(ctx context.Context) ([]carInfo, error) {
			cl, err := conn(appGarage, scopeVehiclesRead)
			if err != nil {
				return nil, err
			}
			vs, err := cl.Vehicles(ctx)
			if err != nil {
				return nil, err
			}
			var out []carInfo
			for _, v := range vs {
				if v.Archived || v.SoldDate != "" || (v.Status != "" && v.Status != "active") {
					continue
				}
				ci := carInfo{Vehicle: v}
				if recs, err := cl.Records(ctx, v.ID); err == nil {
					for _, r := range recs {
						if strings.EqualFold(r.Category, "tyres") && r.Date > ci.LastTyres {
							ci.LastTyres = r.Date
						}
					}
				}
				out = append(out, ci)
			}
			return out, nil
		},
		pets: func(ctx context.Context) ([]pocket.Pet, error) {
			cl, err := conn(appPets, scopePetsRead)
			if err != nil {
				return nil, err
			}
			return cl.Pets(ctx)
		},
		games: func(ctx context.Context) ([]pocket.Game, map[int64]bool, error) {
			cl, err := conn(appLeague, scopeLeagueRead)
			if err != nil {
				return nil, nil, err
			}
			gs, err := cl.Upcoming(ctx)
			if err != nil {
				return nil, nil, err
			}
			outdoor := map[int64]bool{}
			if groups, err := cl.LeagueGroups(ctx); err == nil {
				for _, g := range groups {
					outdoor[g.ID] = g.Outdoor
				}
			}
			return gs, outdoor, nil
		},
	}
}

// ---- events ----

// planEvent puts the weather on one event: at its start for a timed event,
// the day for an all-day one; at the place the event names when Weather can
// find it, else at home.
func (s *Server) planEvent(ctx context.Context, uid int64, st store.Settings, home *store.Location, hf *weather.Forecast, e pocket.Event, u units) *PlanEvent {
	if e.Masked || strings.EqualFold(e.Status, "cancelled") {
		return nil
	}
	f, place := hf, home.Name
	if e.Location != "" {
		if l := s.matchLocation(ctx, uid, e.Location); l != nil {
			if lf, _, err := s.forecastOf(ctx, l); err == nil {
				f, place = lf, l.Name
			}
		} else if g := s.geocode(ctx, e.Location, st.Language); g != nil && weather.Distance(g.Lat, g.Lon, home.Lat, home.Lon) > 15 {
			if gf, _, err := s.forecastAt(ctx, g.Lat, g.Lon); err == nil {
				f, place = gf, g.Name
			}
		}
	}
	return eventWeather(f, place, e, st, u)
}

// eventWeather is the weather an event gets from forecast f (for place):
// at its start and the hours it lasts for a timed event, the day for an
// all-day one, with flags and a note when it will rain, storm, blow or be
// very hot or cold then.
func eventWeather(f *weather.Forecast, place string, e pocket.Event, st store.Settings, u units) *PlanEvent {
	pe := &PlanEvent{ID: e.ID, Title: firstNonEmpty(e.Title, "(no title)"), AllDay: e.AllDay, Location: e.Location, Place: place, Flags: []string{}}
	loc := f.Location()
	if e.AllDay || len(e.Start) == 10 {
		pe.AllDay = true
		pe.Start, pe.End = e.Start[:min(10, len(e.Start))], e.End
		d := f.DayOf(pe.Start)
		if d == nil {
			return nil
		}
		pe.Code, pe.Cond, pe.TMin, pe.TMax, pe.Prob, pe.Gust = d.Code, d.Condition, d.TMin, d.TMax, d.PrecipProb, d.Gust
		pe.Temp = d.TMax
		flagDay(pe, d, st, u)
		return pe
	}
	start, err := time.Parse(time.RFC3339, e.Start)
	if err != nil {
		return nil
	}
	end, err := time.Parse(time.RFC3339, e.End)
	if err != nil || !end.After(start) {
		end = start.Add(time.Hour)
	}
	if end.Before(time.Now()) {
		return nil
	}
	pe.Start, pe.End = start.In(loc).Format("2006-01-02T15:04"), end.In(loc).Format("2006-01-02T15:04")
	h := f.HourAt(pe.Start)
	if h == nil {
		return nil
	}
	pe.Code, pe.Cond, pe.Temp, pe.Gust = h.Code, h.Condition, h.Temp, h.Gust
	// the worst of the hours it lasts (at most six)
	var prob, gust, precip float64
	storm := false
	for t := start; t.Before(end) && t.Before(start.Add(6*time.Hour)); t = t.Add(time.Hour) {
		if hh := f.HourAt(t.In(loc).Format("2006-01-02T15:04")); hh != nil {
			prob = math.Max(prob, val0(hh.PrecipProb))
			gust = math.Max(gust, val0(hh.Gust))
			precip += val0(hh.Precip)
			storm = storm || hh.Code >= 95
		}
	}
	pe.Prob, pe.Gust = &prob, &gust
	var notes []string
	switch {
	case storm && st.WarnStorm:
		pe.Flags = append(pe.Flags, "storm")
		notes = append(notes, "Thunderstorms are likely then; have a plan to go inside")
	case prob >= 60 && precip >= 0.5:
		pe.Flags = append(pe.Flags, "rain")
		notes = append(notes, fmt.Sprintf("Take an umbrella: %.0f %% chance of rain, about %s", prob, u.precip(precip)))
	case prob >= 40 && precip >= 0.3:
		pe.Flags = append(pe.Flags, "rain")
		notes = append(notes, fmt.Sprintf("It may rain (%.0f %%)", prob))
	}
	if gust >= math.Min(st.GustKmh, 55) {
		pe.Flags = append(pe.Flags, "wind")
		notes = append(notes, "windy, gusts up to "+u.wind(gust))
	}
	if t := val0(h.Temp); t >= st.HeatC {
		pe.Flags = append(pe.Flags, "heat")
		notes = append(notes, "hot: "+u.temp(t)+"; bring water")
	} else if t <= -5 {
		pe.Flags = append(pe.Flags, "cold")
		notes = append(notes, "very cold: "+u.temp(t)+"; dress warmly")
	}
	if h.Snow != nil && *h.Snow > 0.5 {
		pe.Flags = append(pe.Flags, "snow")
		notes = append(notes, "snow; leave earlier")
	}
	pe.Note = sentence(notes)
	return pe
}

func flagDay(pe *PlanEvent, d *weather.Day, st store.Settings, u units) {
	var notes []string
	if d.Code >= 95 && st.WarnStorm {
		pe.Flags = append(pe.Flags, "storm")
		notes = append(notes, "Thunderstorms are likely")
	} else if val0(d.PrecipProb) >= 60 && val0(d.Precip) >= 2 {
		pe.Flags = append(pe.Flags, "rain")
		notes = append(notes, fmt.Sprintf("Rain is likely (%.0f %%, %s)", val0(d.PrecipProb), u.precip(val0(d.Precip))))
	}
	if val0(d.Gust) >= st.GustKmh {
		pe.Flags = append(pe.Flags, "wind")
		notes = append(notes, "gusts up to "+u.wind(val0(d.Gust)))
	}
	if val0(d.TMax) >= st.HeatC {
		pe.Flags = append(pe.Flags, "heat")
		notes = append(notes, "hot: up to "+u.temp(val0(d.TMax)))
	}
	if val0(d.Snow) >= 2 {
		pe.Flags = append(pe.Flags, "snow")
		notes = append(notes, fmt.Sprintf("snow (%.0f cm)", val0(d.Snow)))
	}
	pe.Note = sentence(notes)
}

func sentence(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	s := strings.Join(parts, "; ")
	return strings.ToUpper(s[:1]) + s[1:] + "."
}

func val0(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// matchLocation finds one of the person's places an event's location names.
func (s *Server) matchLocation(ctx context.Context, uid int64, text string) *store.Location {
	t := strings.ToLower(text)
	locs, _ := s.store.ListLocations(ctx, uid)
	for i := range locs {
		if n := strings.ToLower(locs[i].Name); len(n) >= 3 && strings.Contains(t, n) {
			return &locs[i]
		}
	}
	return nil
}

// cleanPlaceText keeps the town of an address-like location: the last part
// that has no digits ("Zmaja od Bosne 8, 71000 Sarajevo" → "Sarajevo").
func cleanPlaceText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "http") {
		return ""
	}
	parts := strings.Split(text, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		var words []string
		for _, w := range strings.Fields(p) {
			if !strings.ContainsAny(w, "0123456789") {
				words = append(words, w)
			}
		}
		if len(words) > 0 && len(words) <= 4 && len(parts) > 1 && i == len(parts)-1 && isCountry(strings.Join(words, " ")) {
			continue // "…, Austria": the town is before it
		}
		if len(words) > 0 && len(words) <= 4 {
			return strings.Join(words, " ")
		}
	}
	return ""
}

func isCountry(s string) bool {
	switch strings.ToLower(s) {
	case "austria", "österreich", "germany", "deutschland", "croatia", "hrvatska", "bosnia and herzegovina", "bih", "bosna i hercegovina", "serbia", "srbija",
		"slovenia", "slovenija", "montenegro", "crna gora", "italy", "italia", "france", "spain", "españa", "portugal", "united kingdom", "uk", "usa", "united states",
		"switzerland", "schweiz", "netherlands", "belgium", "hungary", "czechia", "czech republic", "poland", "greece", "turkey", "türkiye":
		return true
	}
	return false
}

// ---- games ----

// planGame puts the weather on one coming game from Pocket League, within
// the next week: at the venue when the game says where it is on the map,
// else at home; at its start and the hours it lasts. Games put to a vote
// (no time yet), cancelled or over are left out. An indoor game keeps its
// weather (for getting there) without warnings.
func (s *Server) planGame(ctx context.Context, st store.Settings, home *store.Location, hf *weather.Forecast, g pocket.Game, outdoor map[int64]bool, today string, u units, link string) *PlanEvent {
	if len(g.StartsAt) < 16 || g.Ended || g.Status == "cancelled" || g.Status == "voting" {
		return nil
	}
	if d := g.StartsAt[:10]; d < today || d > store.AddDays(today, 7) {
		return nil
	}
	f, place := hf, home.Name
	if g.VenueLat != nil && g.VenueLon != nil && (*g.VenueLat != 0 || *g.VenueLon != 0) {
		if gf, _, err := s.forecastAt(ctx, *g.VenueLat, *g.VenueLon); err == nil {
			f, place = gf, firstNonEmpty(g.VenueName, home.Name)
		}
	}
	start, err := time.ParseInLocation("2006-01-02T15:04", g.StartsAt[:16], f.Location())
	if err != nil {
		return nil
	}
	mins := g.DurationMin
	if mins <= 0 {
		mins = 90
	}
	title := strings.TrimSpace(g.Emoji + " " + firstNonEmpty(g.Title, g.GroupName, "Game"))
	e := pocket.Event{ID: fmt.Sprintf("game:%d", g.ID), Title: title, Location: firstNonEmpty(g.VenueName, g.VenueAddr),
		Start: start.Format(time.RFC3339), End: start.Add(time.Duration(mins) * time.Minute).Format(time.RFC3339)}
	if f.HourAt(start.Format("2006-01-02T15:04")) == nil {
		// further ahead than the hours: the day's weather
		e.AllDay, e.Start, e.End = true, start.Format("2006-01-02"), start.AddDate(0, 0, 1).Format("2006-01-02")
	}
	pe := eventWeather(f, place, e, st, u)
	if pe == nil {
		return nil
	}
	if e.AllDay {
		pe.AllDay, pe.Start = false, start.Format("2006-01-02T15:04") // still at its time
	}
	pe.Link = link
	pe.Players = gamePlayers(g)
	if out, ok := outdoor[g.GroupID]; ok && !out {
		pe.Indoor, pe.Flags, pe.Note = true, []string{}, ""
	}
	return pe
}

// gamePlayers is who is coming: "3 in, 4 needed · you: not answered yet".
func gamePlayers(g pocket.Game) string {
	who := fmt.Sprintf("%d in", g.In)
	if g.MinPlayers > g.In {
		who += fmt.Sprintf(", %d needed", g.MinPlayers)
	}
	switch g.MyAnswer {
	case "in", "maybe", "out":
		return who + " · you: " + g.MyAnswer
	}
	return who + " · you: not answered yet"
}

// ---- trips ----

func (s *Server) planTrip(ctx context.Context, uid int64, st store.Settings, t pocket.Trip, today string, u units) *PlanTrip {
	if t.Archived || t.StartDate == "" || strings.EqualFold(t.Status, "cancelled") {
		return nil
	}
	end := firstNonEmpty(t.EndDate, t.StartDate)
	if end < today || t.StartDate > store.AddDays(today, 60) {
		return nil
	}
	pt := &PlanTrip{ID: t.ID, Name: t.Name, Destination: t.Destination, Start: t.StartDate, End: end, Days: []weather.Day{}, LastYear: []weather.Day{},
		Link: s.siblingLink(ctx, uid, appTravel, fmt.Sprintf("/trips/%d", t.ID))}
	switch {
	case t.Lat != nil && t.Lng != nil && (*t.Lat != 0 || *t.Lng != 0):
		pt.Lat, pt.Lon, pt.Place = *t.Lat, *t.Lng, firstNonEmpty(cleanPlaceText(t.Destination), t.Destination, t.Name)
	default:
		g := s.geocode(ctx, firstNonEmpty(t.Destination, t.Name), st.Language)
		if g == nil {
			return nil
		}
		pt.Lat, pt.Lon, pt.Place = g.Lat, g.Lon, g.Name
	}
	f, _, err := s.forecastAt(ctx, pt.Lat, pt.Lon)
	if err == nil {
		for _, d := range f.Days {
			if d.Date >= t.StartDate && d.Date <= end && d.Date >= today {
				pt.Days = append(pt.Days, d)
			}
		}
	}
	if len(pt.Days) > 0 {
		pt.Summary = tripSummary(pt.Days, u, false)
		return pt
	}
	// too far ahead: the same dates last year
	from, to := lastYear(t.StartDate), lastYear(end)
	if to > store.AddDays(from, 21) {
		to = store.AddDays(from, 21)
	}
	if days, err := s.pastDays(ctx, pt.Lat, pt.Lon, from, to); err == nil && len(days) > 0 {
		pt.LastYear = days
		pt.Summary = "Too far ahead for a forecast. These days last year: " + tripSummary(days, u, true)
	}
	return pt
}

func lastYear(d string) string {
	t, err := store.ParseDate(d)
	if err != nil {
		return d
	}
	return t.AddDate(-1, 0, 0).Format("2006-01-02")
}

func tripSummary(days []weather.Day, u units, past bool) string {
	lo, hi := math.Inf(1), math.Inf(-1)
	wet, snow, storm := 0, 0, 0
	for _, d := range days {
		lo, hi = math.Min(lo, val0(d.TMin)), math.Max(hi, val0(d.TMax))
		if val0(d.Precip) >= 1 {
			wet++
		}
		if val0(d.Snow) >= 1 {
			snow++
		}
		if d.Code >= 95 {
			storm++
		}
	}
	s := fmt.Sprintf("%s to %s", u.temp(lo), u.temp(hi))
	switch {
	case wet == 0:
		s += ", dry"
	case wet == len(days):
		s += ", rain every day"
	default:
		s += fmt.Sprintf(", rain on %d of %d days", wet, len(days))
	}
	if snow > 0 {
		s += fmt.Sprintf(", snow on %d", snow)
	}
	if storm > 0 {
		s += ", thunderstorms possible"
	}
	if past {
		return s + "."
	}
	return s + ". Pack " + packList(days) + "."
}

// packList is what the weather says to pack.
func packList(days []weather.Day) string {
	lo, hi := math.Inf(1), math.Inf(-1)
	wet := false
	sun := 0.0
	for _, d := range days {
		lo, hi = math.Min(lo, val0(d.TMin)), math.Max(hi, val0(d.TMax))
		wet = wet || val0(d.Precip) >= 1
		sun = math.Max(sun, val0(d.UV))
	}
	var items []string
	switch {
	case lo <= 0:
		items = append(items, "a winter coat, hat and gloves")
	case hi < 12:
		items = append(items, "a warm jacket")
	case hi < 20:
		items = append(items, "a light jacket")
	case hi >= 28:
		items = append(items, "light clothes")
	default:
		items = append(items, "layers")
	}
	if wet {
		items = append(items, "an umbrella")
	}
	if sun >= 6 {
		items = append(items, "sunscreen")
	}
	if len(items) == 1 {
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// ---- plants ----

var outdoorWords = []string{"balcony", "balkon", "terrace", "terasa", "terrasse", "garden", "bašta", "basta", "vrt", "outside", "outdoor", "patio", "yard",
	"dvorište", "dvoriste", "porch", "roof", "veranda", "loggia", "lođa", "garten"}

func outdoor(p pocket.Plant) bool {
	t := strings.ToLower(p.Room + " " + p.Name)
	for _, w := range outdoorWords {
		if strings.Contains(t, w) {
			return true
		}
	}
	return false
}

func names(list []string) string {
	switch len(list) {
	case 0:
		return ""
	case 1:
		return list[0]
	case 2:
		return list[0] + " and " + list[1]
	}
	if len(list) > 4 {
		return strings.Join(list[:3], ", ") + fmt.Sprintf(" and %d more", len(list)-3)
	}
	return strings.Join(list[:len(list)-1], ", ") + " and " + list[len(list)-1]
}

func plantAdvice(plants []pocket.Plant, f *weather.Forecast, today string, u units, link string) []Advice {
	var ns []string
	for _, p := range plants {
		ns = append(ns, p.Name)
	}
	who := names(ns)
	var out []Advice
	for i := 0; i < 2; i++ {
		date := store.AddDays(today, i)
		next := f.DayOf(store.AddDays(date, 1))
		d := f.DayOf(date)
		if d == nil {
			continue
		}
		// tonight's low is the next morning's
		if next != nil && val0(next.TMin) <= 2 {
			lo := val0(next.TMin)
			night := "tonight"
			if i == 1 {
				night = "tomorrow night"
			}
			out = append(out, Advice{Key: "plants:frost:" + date, App: "home", Icon: "leaf", Level: "warn", Day: date, Link: link,
				Title:  fmt.Sprintf("Frost %s: bring the outdoor plants in", night),
				Detail: fmt.Sprintf("Down to %s %s. Bring %s inside, or cover them with fleece and move the pots against the wall.", u.temp(lo), night, who),
				Task:   &TaskDraft{Title: "Bring the outdoor plants in (frost " + night + ")", Due: date}})
			break
		}
	}
	for i := 0; i < 3; i++ {
		date := store.AddDays(today, i)
		d := f.DayOf(date)
		if d == nil {
			continue
		}
		when := dayName(date, today)
		if val0(d.TMax) >= 30 {
			out = append(out, Advice{Key: "plants:heat:" + date, App: "home", Icon: "leaf", Level: "info", Day: date, Link: link,
				Title:  fmt.Sprintf("Hot %s: water the outdoor plants in the evening", when),
				Detail: fmt.Sprintf("Up to %s. Water %s early or after sunset, and move pots out of the midday sun.", u.temp(val0(d.TMax)), who)})
			break
		}
		if val0(d.Gust) >= 60 {
			out = append(out, Advice{Key: "plants:wind:" + date, App: "home", Icon: "wind", Level: "warn", Day: date, Link: link,
				Title:  fmt.Sprintf("Storm %s: secure the pots", when),
				Detail: fmt.Sprintf("Gusts up to %s. Put light pots on the floor and tie up tall ones (%s).", u.wind(val0(d.Gust)), who)})
			break
		}
	}
	if d := f.DayOf(today); d != nil && val0(d.Rain) >= 10 {
		out = append(out, Advice{Key: "plants:rain:" + today, App: "home", Icon: "drop", Level: "info", Day: today, Link: link,
			Title:  "No watering needed outside today",
			Detail: fmt.Sprintf("About %s of rain falls today; %s get enough.", u.precip(val0(d.Rain)), who)})
	}
	return out
}

// ---- cars ----

func carAdvice(cars []carInfo, f *weather.Forecast, today string, u units, link string) []Advice {
	var out []Advice
	var carNames []string
	for _, c := range cars {
		if isMotorbike(c.Kind) {
			continue
		}
		carNames = append(carNames, firstNonEmpty(c.Name, strings.TrimSpace(c.Make+" "+c.Model), "the car"))
	}
	if len(carNames) == 0 {
		return nil
	}
	who := names(carNames)
	month := 0
	fmt.Sscanf(today[5:7], "%d", &month)
	year := today[:4]
	// the coming week's mean temperature
	sum, n, frostNights := 0.0, 0, 0
	for i := 0; i < 7; i++ {
		if d := f.DayOf(store.AddDays(today, i)); d != nil && d.TMin != nil && d.TMax != nil {
			sum += (*d.TMin + *d.TMax) / 2
			n++
			if *d.TMin <= 0 {
				frostNights++
			}
		}
	}
	if n >= 5 {
		mean := sum / float64(n)
		recent := func(c carInfo) bool { return c.LastTyres != "" && c.LastTyres >= store.AddDays(today, -75) }
		var due []string
		for _, c := range cars {
			if !isMotorbike(c.Kind) && !recent(c) {
				due = append(due, firstNonEmpty(c.Name, strings.TrimSpace(c.Make+" "+c.Model), "the car"))
			}
		}
		autumn := month >= 9 || month <= 2
		if len(due) > 0 && autumn && (mean < 7 || frostNights >= 2) {
			season := year
			if month <= 2 {
				season = fmt.Sprint(atoiSafe(year) - 1)
			}
			out = append(out, Advice{Key: "tyres:winter:" + season, App: "garage", Icon: "car", Level: "warn", Day: today, Link: link,
				Title:  "Time for winter tyres",
				Detail: fmt.Sprintf("The coming week averages %s with frost on %d %s. Below about 7 °C winter tyres grip better; book the change for %s.", u.temp(mean), frostNights, plural(frostNights, "night", "nights"), names(due)),
				Task:   &TaskDraft{Title: "Switch to winter tyres: " + names(due), Due: store.AddDays(today, 7)}})
		}
		spring := month >= 3 && month <= 5
		if len(due) > 0 && spring && mean >= 10 && frostNights == 0 {
			out = append(out, Advice{Key: "tyres:summer:" + year, App: "garage", Icon: "car", Level: "info", Day: today, Link: link,
				Title:  "Time for summer tyres",
				Detail: fmt.Sprintf("The coming week averages %s and no more frost is forecast. Summer tyres brake shorter in the warmth; book the change for %s.", u.temp(mean), names(due)),
				Task:   &TaskDraft{Title: "Switch to summer tyres: " + names(due), Due: store.AddDays(today, 14)}})
		}
	}
	// tomorrow morning's windscreen
	tomorrow := store.AddDays(today, 1)
	cold := math.Inf(1)
	for _, h := range f.Hours {
		if strings.HasPrefix(h.Time, tomorrow) && h.Time[11:13] >= "05" && h.Time[11:13] <= "09" {
			cold = math.Min(cold, val0(h.Temp))
		}
	}
	if d := f.DayOf(tomorrow); d != nil && val0(d.Snow) >= 3 {
		out = append(out, Advice{Key: "car:snow:" + tomorrow, App: "garage", Icon: "car", Level: "warn", Day: tomorrow, Link: link,
			Title:  "Snow tomorrow: leave earlier",
			Detail: fmt.Sprintf("About %.0f cm. Clear the whole car of snow, not just the windscreen, and allow extra time.", val0(d.Snow))})
	} else if cold <= 0 {
		out = append(out, Advice{Key: "car:frost:" + tomorrow, App: "garage", Icon: "car", Level: "info", Day: tomorrow, Link: link,
			Title:  "Frozen windscreen tomorrow morning",
			Detail: fmt.Sprintf("%s at 7:00. Allow five minutes to clear %s, or cover the windscreen tonight.", u.temp(cold), who)})
	}
	return out
}

func isMotorbike(kind string) bool {
	k := strings.ToLower(kind)
	return strings.Contains(k, "motor") || strings.Contains(k, "bike") || strings.Contains(k, "scooter")
}

func atoiSafe(s string) int {
	n := 0
	fmt.Sscanf(s, "%d", &n)
	return n
}

// ---- dogs ----

func isDog(species string) bool {
	s := strings.ToLower(species)
	return s == "dog" || s == "pas" || s == "hund" || strings.Contains(s, "dog")
}

func dogAdvice(dogs []pocket.Pet, f *weather.Forecast, today string, u units, link string) []Advice {
	var ns []string
	for _, d := range dogs {
		ns = append(ns, d.Name)
	}
	who := names(ns)
	var out []Advice
	for i := 0; i < 2; i++ {
		date := store.AddDays(today, i)
		d := f.DayOf(date)
		if d == nil {
			continue
		}
		when := dayName(date, today)
		switch {
		case d.Code >= 95:
			first, _, _, _, _ := span(f, date, func(h weather.Hour) bool { return h.Code >= 95 }, func(h weather.Hour) float64 { return 0 })
			if first == "" {
				continue
			}
			out = append(out, Advice{Key: "pets:storm:" + date, App: "pets", Icon: "paw", Level: "warn", Day: date, Link: link,
				Title:  fmt.Sprintf("Thunder %s from about %s", when, clock(first)),
				Detail: fmt.Sprintf("Many dogs are afraid of thunder. Walk %s before then and keep a quiet corner ready inside.", who)})
		case val0(d.TMax) >= 27:
			out = append(out, Advice{Key: "pets:heat:" + date, App: "pets", Icon: "paw", Level: "warn", Day: date, Link: link,
				Title:  fmt.Sprintf("Hot %s: walk %s early and late", when, who),
				Detail: fmt.Sprintf("Up to %s. Pavement gets hot enough to burn paws: walk before 9:00 and after 20:00, take water along.", u.temp(val0(d.TMax)))})
		case val0(d.TMin) <= -8 || val0(d.Snow) >= 3:
			out = append(out, Advice{Key: "pets:cold:" + date, App: "pets", Icon: "paw", Level: "info", Day: date, Link: link,
				Title:  fmt.Sprintf("Cold %s: shorter walks", when),
				Detail: fmt.Sprintf("Down to %s. Keep walks short for %s and rinse road salt off the paws afterwards.", u.temp(val0(d.TMin)), who)})
		}
	}
	// the best hour for a walk today: dry first, then closest to 15 °C, 7:00–21:00
	best, bestScore := "", math.Inf(1)
	var bestH weather.Hour
	now := f.LocalHour()
	for _, h := range f.Hours {
		if !strings.HasPrefix(h.Time, today) || h.Time < now || h.Time[11:13] < "07" || h.Time[11:13] > "21" {
			continue
		}
		score := val0(h.PrecipProb)*2 + math.Abs(val0(h.Temp)-15) + val0(h.Gust)/5
		if score < bestScore {
			best, bestScore, bestH = h.Time, score, h
		}
	}
	if best != "" {
		dry := "dry"
		if val0(bestH.PrecipProb) >= 40 {
			dry = fmt.Sprintf("%.0f %% chance of rain", val0(bestH.PrecipProb))
		}
		out = append(out, Advice{Key: "pets:walk:" + today, App: "pets", Icon: "paw", Level: "info", Day: today, Link: link,
			Title:  fmt.Sprintf("Best time for a walk today: %s", clock(best)),
			Detail: fmt.Sprintf("%s (%s), %s at %s: the nicest hour left today for %s.", weather.Describe(bestH.Code, bestH.IsDay), dry, u.temp(val0(bestH.Temp)), clock(best), who)})
	}
	return out
}

// ---- the demo's linked apps ----

// demoPlanSource makes up the linked apps for DEMO_DATA (no Pocket Core):
// a dentist and a football match, a trip to Vienna, two balcony plants, a
// car without winter tyres, a dog and two games (tennis outside, futsal in
// a hall) — so the Plans page can be seen.
func demoPlanSource() planSource {
	now := time.Now()
	day := func(n int, hh, mm int) time.Time {
		d := now.AddDate(0, 0, n)
		return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, time.Local)
	}
	ev := func(id, title, loc string, start time.Time, mins int) pocket.Event {
		return pocket.Event{ID: id, Title: title, Location: loc, Start: start.Format(time.RFC3339), End: start.Add(time.Duration(mins) * time.Minute).Format(time.RFC3339)}
	}
	trip := func(id int64, name, dest string, from, to int, lat, lng float64) pocket.Trip {
		return pocket.Trip{ID: id, Name: name, Destination: dest, StartDate: now.AddDate(0, 0, from).Format("2006-01-02"), EndDate: now.AddDate(0, 0, to).Format("2006-01-02"), Lat: &lat, Lng: &lng}
	}
	return planSource{
		demo: true,
		events: func(context.Context, string, string) ([]pocket.Event, error) {
			return []pocket.Event{
				ev("101", "Dentist", "Titova 12, Sarajevo", day(1, 10, 30), 45),
				ev("102", "Football with the team", "Koševo", day(2, 17, 0), 90),
				ev("103", "Dinner at Kibe Mahala", "Vratnik, Sarajevo", day(2, 20, 0), 120),
				ev("104", "Farmers' market", "Markale, Sarajevo", day(3, 9, 0), 60),
				{ID: "105", Title: "Mum's birthday", Start: now.AddDate(0, 0, 3).Format("2006-01-02"), End: now.AddDate(0, 0, 4).Format("2006-01-02"), AllDay: true},
			}, nil
		},
		trips: func(context.Context) ([]pocket.Trip, error) {
			return []pocket.Trip{
				trip(3, "Vienna weekend", "Vienna, Austria", 5, 8, 48.2085, 16.3721),
				trip(4, "Lisbon in spring", "Lisbon, Portugal", 40, 46, 38.7167, -9.1333),
			}, nil
		},
		plants: func(context.Context) ([]pocket.Plant, error) {
			return []pocket.Plant{{ID: 1, Name: "Lemon tree", Room: "Balcony"}, {ID: 2, Name: "Basil", Room: "Balcony"}, {ID: 3, Name: "Monstera", Room: "Living room"}}, nil
		},
		vehicles: func(context.Context) ([]carInfo, error) {
			return []carInfo{{Vehicle: pocket.Vehicle{ID: 1, Name: "Golf", Make: "Volkswagen", Model: "Golf", Kind: "car"}, LastTyres: now.AddDate(0, -7, 0).Format("2006-01-02")}}, nil
		},
		pets: func(context.Context) ([]pocket.Pet, error) {
			return []pocket.Pet{{ID: 1, Name: "Rex", Species: "dog"}, {ID: 2, Name: "Mici", Species: "cat"}}, nil
		},
		games: func(context.Context) ([]pocket.Game, map[int64]bool, error) {
			lat, lon := 43.8737, 18.4093
			at := func(n, hh int) string { return day(n, hh, 0).Format("2006-01-02T15:04") }
			return []pocket.Game{
				{ID: 31, GroupID: 1, GroupName: "Tuesday Tennis", Emoji: "🎾", Status: "planned", StartsAt: at(1, 20), DurationMin: 120, VenueName: "TK Koševo", VenueLat: &lat, VenueLon: &lon, In: 3, MinPlayers: 4},
				{ID: 32, GroupID: 2, GroupName: "Friday 5-a-side", Emoji: "⚽", Status: "planned", StartsAt: at(4, 19), DurationMin: 60, VenueName: "Dvorana Mojmilo", In: 8, MinPlayers: 10, MyAnswer: "in"},
				{ID: 33, GroupID: 3, GroupName: "Padel crew", Emoji: "🎾", Status: "voting"},
			}, map[int64]bool{1: true, 2: false}, nil
		},
	}
}
