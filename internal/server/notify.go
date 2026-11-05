package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-weather/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
)

// pushes runs every minute: once a day, at each person's own hour, one
// message with the day ahead at home, the warnings that are still waiting
// and the advice for today (and "weather.today" on the Core bus).
func (s *Server) pushes(ctx context.Context) {
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	for _, uid := range ids {
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil {
			continue
		}
		if now.Hour() != st.NotifyHour || now.Minute() > 10 {
			continue
		}
		if !s.once(ctx, fmt.Sprintf("push:%d:%s", uid, store.Today())) {
			continue
		}
		s.morning(ctx, uid, st, false)
	}
}

// morning builds and sends one person's morning message (force: for "Send
// it now", sent even when switched off). It returns the text.
func (s *Server) morning(ctx context.Context, uid int64, st store.Settings, force bool) (string, bool) {
	home, err := s.store.HomeLocation(ctx, uid)
	if err != nil {
		return "", false
	}
	s.checkUser(ctx, uid, true)
	f, _, err := s.forecastOf(ctx, home)
	if err != nil {
		return "", false
	}
	u := unitsOf(st)
	today := f.Today()
	if today == nil {
		return "", false
	}
	lines := []string{daySentence(f, u)}
	if tm := f.DayOf(store.AddDays(f.LocalDate(), 1)); tm != nil {
		lines = append(lines, fmt.Sprintf("Tomorrow: %s, %s to %s.", strings.ToLower(tm.Text), u.temp(val0(tm.TMin)), u.temp(val0(tm.TMax))))
	}
	warns, _ := s.store.ListWarnings(ctx, uid, f.LocalDate(), true, 0)
	prio := "info"
	for _, w := range warns {
		if w.Day > store.AddDays(f.LocalDate(), 1) {
			continue
		}
		where := ""
		if w.LocationID != home.ID {
			where = " (" + w.Place + ")"
		}
		lines = append(lines, "⚠ "+w.Title+where)
		if w.Level == "warning" {
			prio = "high"
		}
	}
	if p, err := s.plans(ctx, uid, true); err == nil {
		n := 0
		for _, a := range p.Advice {
			if a.Day == f.LocalDate() && a.Mark == "" && n < 3 {
				lines = append(lines, "• "+a.Title)
				n++
			}
		}
	}
	body := strings.Join(lines, "\n")
	if !force {
		s.publish(ctx, uid, "weather.today", "place:"+itoa(home.ID), map[string]any{"place": home.Name, "date": f.LocalDate(), "text": daySentence(f, u),
			"t_min": today.TMin, "t_max": today.TMax, "code": today.Code, "condition": today.Condition, "precip": today.Precip, "warnings": len(warns)})
	}
	if (!st.NotifyMorning && !force) || st.NotifyURL == "" {
		return body, false
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	title := fmt.Sprintf("%s today: %s to %s", home.Name, u.temp(val0(today.TMin)), u.temp(val0(today.TMax)))
	if err := n.Send(ctx, notify.Message{Title: title, Body: body, Priority: prio, Tags: []string{"partly_sunny"}, Link: strings.TrimRight(s.cfg.BaseURL, "/") + "/"}); err != nil {
		s.log.Warn("notify", "user", uid, "err", err)
		return body, false
	}
	for _, w := range warns {
		if w.Notified != w.Level {
			_ = s.store.SetWarningNotified(ctx, uid, w.ID, w.Level)
		}
	}
	return body, true
}

// once reports true the first time a key is seen.
func (s *Server) once(ctx context.Context, key string) bool {
	if _, _, ok := s.store.CacheGet(ctx, "once:"+key); ok {
		return false
	}
	_ = s.store.CachePut(ctx, "once:"+key, "1")
	return true
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Pocket Weather", Body: notifyTestBody, Priority: "info", Tags: []string{"partly_sunny"}}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleCheckNow sends the morning message now and says what it said.
func (s *Server) handleCheckNow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	body, sent := s.morning(ctx, uid, st, true)
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent, "body": body})
}

const notifyTestBody = "Notifications work. The day ahead, and frost, storms, heavy rain, heat and the other warnings you chose, will arrive here."
