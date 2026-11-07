package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/weather"
)

// handleImport takes a Pocket Weather export back: places (matched by name
// and spot, so importing twice adds nothing), their diary (your notes are
// kept) and what was done with advice.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	body, err := s.readBody(r)
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "the file is too large")
		return
	}
	var doc exportDoc
	if err := json.Unmarshal(body, &doc); err != nil || doc.App != "pocket-weather" {
		writeErr(w, http.StatusBadRequest, "that is not a Pocket Weather export (.json)")
		return
	}
	res, err := s.importDoc(ctx, uid, doc)
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	s.afterPlaces(r, uid)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) importDoc(ctx context.Context, uid int64, doc exportDoc) (map[string]int, error) {
	res := map[string]int{"places": 0, "already": 0, "days": 0}
	have, err := s.store.ListLocations(ctx, uid)
	if err != nil {
		return nil, err
	}
	ids := map[int64]int64{} // id in the file → id here
	for _, in := range doc.Locations {
		if in.Source == "here" || in.Name == "" {
			continue
		}
		found := int64(0)
		for _, l := range have {
			if l.Name == in.Name && weather.Distance(l.Lat, l.Lon, in.Lat, in.Lon) < 1 {
				found = l.ID
			}
		}
		if found > 0 {
			ids[in.ID] = found
			res["already"]++
			continue
		}
		old := in.ID
		l := in
		l.ID, l.UserID, l.Sort = 0, uid, 0
		if l.Source != "" && l.Source != "import" {
			l.Source, l.SourceRef = "import", ""
		}
		if err := s.store.CreateLocation(ctx, &l); err != nil {
			return nil, err
		}
		if in.IsHome {
			l.IsHome = true
			_ = s.store.UpdateLocation(ctx, &l)
		}
		have = append(have, l)
		ids[old] = l.ID
		res["places"]++
	}
	for _, d := range doc.Days {
		to, ok := ids[d.LocationID]
		if !ok {
			continue
		}
		d.LocationID = to
		if err := s.store.SaveDay(ctx, uid, d); err != nil {
			return nil, err
		}
		if d.Note != "" {
			_ = s.store.SetDayNote(ctx, uid, to, d.Date, d.Note)
		}
		res["days"]++
	}
	for _, m := range doc.Marks {
		if m.Key != "" && oneOf(m.State, []string{"done", "hidden", "task"}) {
			_ = s.store.SetMark(ctx, uid, m.Key, m.State, m.Ref)
		}
	}
	return res, nil
}

// testDetail adds what "Test connection" found in a linked app.
func (s *Server) testDetail(ctx context.Context, a pocketApp, conn *siblingConn) string {
	cl := conn.client
	switch a.slug {
	case "calendar":
		today := store.Today()
		if evs, err := cl.Events(ctx, today, store.AddDays(today, 3)); err == nil {
			return fmt.Sprintf(", %d %s in the next three days", len(evs), plural(len(evs), "event", "events"))
		} else {
			return ", but reading events failed: " + err.Error()
		}
	case "travel":
		if trips, err := cl.Trips(ctx); err == nil {
			return fmt.Sprintf(", %d %s", len(trips), plural(len(trips), "trip", "trips"))
		} else {
			return ", but reading trips failed: " + err.Error()
		}
	case "home":
		if ps, err := cl.Plants(ctx); err == nil {
			out := 0
			for _, p := range ps {
				if outdoor(p) {
					out++
				}
			}
			return fmt.Sprintf(", %d plants, %d of them outside (a room like Balcony or Garden)", len(ps), out)
		} else {
			return ", but reading plants failed: " + err.Error()
		}
	case "garage":
		if vs, err := cl.Vehicles(ctx); err == nil {
			return fmt.Sprintf(", %d %s", len(vs), plural(len(vs), "vehicle", "vehicles"))
		} else {
			return ", but reading vehicles failed: " + err.Error()
		}
	case "pets":
		if ps, err := cl.Pets(ctx); err == nil {
			dogs := 0
			for _, p := range ps {
				if isDog(p.Species) {
					dogs++
				}
			}
			return fmt.Sprintf(", %d %s (%d %s)", len(ps), plural(len(ps), "pet", "pets"), dogs, plural(dogs, "dog", "dogs"))
		} else {
			return ", but reading pets failed: " + err.Error()
		}
	case "league":
		if gs, err := cl.Upcoming(ctx); err == nil {
			return fmt.Sprintf(", %d coming %s", len(gs), plural(len(gs), "game", "games"))
		} else {
			return ", but reading your games failed: " + err.Error()
		}
	case "movement":
		if p, err := cl.Now(ctx); err == nil {
			if p == nil {
				return ", no position recorded yet"
			}
			ago := time.Since(time.Unix(p.TS, 0))
			return fmt.Sprintf(", last position %s ago", roundDur(ago))
		} else {
			return ", but reading your position failed: " + err.Error()
		}
	}
	return ""
}

func roundDur(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%.0f min", math.Max(1, d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%.0f h", d.Hours())
	}
	return fmt.Sprintf("%.0f days", d.Hours()/24)
}
