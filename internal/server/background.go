package server

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-weather/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/weather"
)

// ---- the background work ----
//
// Every 30 minutes, for each person and each place they get warnings for:
// the forecast is read, warnings are saved (a new or worse one is pushed and
// published), today and yesterday go into the diary; and where they are now
// is asked from Pocket Movement. Pages run the same check when the last one
// is older than 15 minutes, so what they show is never older than that.

// checkUser runs the check for one person (force: even when it just ran).
func (s *Server) checkUser(ctx context.Context, uid int64, force bool) {
	key := fmt.Sprintf("checked:%d", uid)
	if !force {
		if _, at, ok := s.store.CacheGet(ctx, key); ok {
			if t, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(t) < 15*time.Minute {
				return
			}
		}
	}
	_ = s.store.CachePut(ctx, key, "1")
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil {
		return
	}
	if st.FollowMe {
		s.followMe(ctx, uid, st)
	}
	locs, err := s.store.ListLocations(ctx, uid)
	if err != nil {
		return
	}
	for i := range locs {
		l := &locs[i]
		if !l.Warn {
			continue
		}
		f, _, err := s.forecastOf(ctx, l)
		if err != nil {
			s.log.Debug("weather: forecast failed", "place", l.Name, "err", err)
			continue
		}
		air, _, _ := s.airAt(ctx, l.Lat, l.Lon)
		s.saveWarnings(ctx, uid, st, l, f, air)
		s.saveDiary(ctx, uid, l, f, air)
	}
}

// saveWarnings stores what the forecast shows and tells the person about
// new or worse warnings (heads-ups wait for the morning in quiet hours).
func (s *Server) saveWarnings(ctx context.Context, uid int64, st store.Settings, l *store.Location, f *weather.Forecast, air *weather.Air) {
	found := evaluate(f, air, st, unitsOf(st))
	keep := make([]int64, 0, len(found))
	var fresh []store.Warning
	for _, fd := range found {
		v := fd.Value
		w, isNew, err := s.store.UpsertWarning(ctx, uid, store.Warning{LocationID: l.ID, Kind: fd.Kind, Level: fd.Level, Day: fd.Day, StartsAt: fd.Start, EndsAt: fd.End,
			Title: fd.Title, Detail: fd.Detail, Value: &v})
		if err != nil {
			s.log.Warn("save warning", "err", err)
			continue
		}
		keep = append(keep, w.ID)
		if isNew || w.Notified == "" || (w.Notified == "advisory" && w.Level == "warning") {
			fresh = append(fresh, *w)
		}
		if isNew {
			s.publish(ctx, uid, "weather.warning", fmt.Sprintf("warning:%d", w.ID), warningPayload(w, l))
		}
	}
	_ = s.store.ClearWarnings(ctx, uid, l.ID, f.LocalDate(), keep)
	if len(fresh) == 0 || !st.NotifyWarnings || st.NotifyURL == "" {
		return
	}
	hour := time.Now().In(f.Location()).Hour()
	quiet := st.QuietHours && (hour >= 22 || hour < 7)
	var send []store.Warning
	for _, w := range fresh {
		if quiet && w.Level != "warning" {
			continue // the morning message carries it
		}
		send = append(send, w)
	}
	if len(send) == 0 {
		return
	}
	title := send[0].Title
	if len(send) > 1 {
		title = fmt.Sprintf("%d weather warnings for %s", len(send), l.Name)
	} else if !strings.Contains(title, l.Name) {
		title += " in " + l.Name
	}
	var lines []string
	prio := "info"
	for _, w := range send {
		if len(send) > 1 {
			lines = append(lines, "• "+w.Title)
		} else {
			lines = append(lines, w.Detail)
		}
		if w.Level == "warning" {
			prio = "high"
		}
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: title, Body: strings.Join(lines, "\n"), Priority: prio, Tags: []string{"warning"}, Link: strings.TrimRight(s.cfg.BaseURL, "/") + "/warnings"}); err != nil {
		s.log.Warn("notify", "user", uid, "err", err)
		return
	}
	for _, w := range send {
		_ = s.store.SetWarningNotified(ctx, uid, w.ID, w.Level)
	}
}

func warningPayload(w *store.Warning, l *store.Location) map[string]any {
	p := map[string]any{"kind": w.Kind, "level": w.Level, "day": w.Day, "starts_at": w.StartsAt, "ends_at": w.EndsAt, "title": w.Title, "detail": w.Detail,
		"place": l.Name, "location_id": l.ID}
	if w.Value != nil {
		p["value"] = *w.Value
	}
	return p
}

// saveDiary writes today (so far) and yesterday into the diary.
func (s *Server) saveDiary(ctx context.Context, uid int64, l *store.Location, f *weather.Forecast, air *weather.Air) {
	today := f.LocalDate()
	for _, date := range []string{store.AddDays(today, -1), today} {
		d := f.DayOf(date)
		if d == nil {
			continue
		}
		rec := recordOf(l.ID, *d)
		if aqi, pollen := air.DayMax(date); aqi != nil || len(pollen) > 0 {
			rec.AQI, rec.Pollen = aqi, pollen
		}
		if err := s.store.SaveDay(ctx, uid, rec); err != nil {
			s.log.Warn("save diary", "err", err)
		}
	}
}

func recordOf(loc int64, d weather.Day) store.DayRecord {
	return store.DayRecord{LocationID: loc, Date: d.Date, Code: d.Code, TMin: d.TMin, TMax: d.TMax, Precip: d.Precip, Snow: d.Snow, Wind: d.Wind, Gust: d.Gust,
		UV: d.UV, SunshineH: d.SunshineH, Pollen: map[string]float64{}}
}

// followMe keeps a "here" place where Pocket Movement last saw the person,
// when that is more than 25 km from all their own places and less than six
// hours ago; otherwise there is no "here" place.
func (s *Server) followMe(ctx context.Context, uid int64, st store.Settings) {
	c := s.clients(ctx, uid)
	conn, err := optional(c.conn(ctx, appMovement, scopeMovementRead))
	if err != nil || conn == nil {
		return
	}
	pos, err := conn.client.Now(ctx)
	if err != nil {
		s.log.Debug("movement: now", "err", err)
		return
	}
	here, _ := s.store.LocationBySource(ctx, uid, "here", "movement")
	drop := func() {
		if here != nil {
			_ = s.store.DeleteLocation(ctx, uid, here.ID)
		}
	}
	if pos == nil || time.Since(time.Unix(pos.TS, 0)) > 6*time.Hour {
		drop()
		return
	}
	locs, _ := s.store.ListLocations(ctx, uid)
	for _, l := range locs {
		if l.Source != "here" && weather.Distance(l.Lat, l.Lon, pos.Lat, pos.Lng) < 25 {
			drop()
			return
		}
	}
	name := pos.Place
	p, err := s.placeName(ctx, pos.Lat, pos.Lng, st.Language)
	if err == nil && p != nil {
		name = p.Name
	}
	if name == "" {
		name = "Where you are"
	}
	if here != nil {
		if weather.Distance(here.Lat, here.Lon, pos.Lat, pos.Lng) < 2 && here.Name == name {
			return
		}
		here.Name, here.Lat, here.Lon = name, pos.Lat, pos.Lng
		if p != nil {
			here.Region, here.Country, here.CountryCode = p.Region, p.Country, p.CountryCode
		}
		_ = s.store.UpdateLocation(ctx, here)
		return
	}
	l := &store.Location{UserID: uid, Name: name, Lat: pos.Lat, Lon: pos.Lng, Warn: true, Source: "here", SourceRef: "movement", Sort: -1}
	if p != nil {
		l.Region, l.Country, l.CountryCode = p.Region, p.Country, p.CountryCode
	}
	if err := s.store.CreateLocation(ctx, l); err != nil {
		s.log.Debug("movement: here", "err", err)
	}
}

// checkAll runs the check for everybody (the janitor, every 30 minutes).
func (s *Server) checkAll(ctx context.Context) {
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	for _, uid := range ids {
		if ctx.Err() != nil {
			return
		}
		s.checkUser(ctx, uid, true)
	}
	_ = s.store.CachePurge(ctx, "fc:", time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339Nano))
	_ = s.store.CachePurge(ctx, "aq:", time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339Nano))
	_ = s.store.CachePurge(ctx, "once:", time.Now().AddDate(0, 0, -30).UTC().Format(time.RFC3339Nano))
}

// round rounds for display in plain text.
func round(v float64) float64 { return math.Round(v) }
