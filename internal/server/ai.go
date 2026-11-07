package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/weather"
)

// aiFor is the person's AI: Pocket AI through Core, their own
// OpenAI-compatible key, or nil with why.
func (s *Server) aiFor(ctx context.Context, uid int64, st store.Settings) (*ai.Client, string, string) {
	email := ""
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		email = u.Email
	}
	pocket, status := s.core.SelectAI(ctx, email, st.AIProvider, st.OpenAIKey != "")
	if pocket != nil {
		if !status.Ready {
			return nil, "", "Pocket AI: " + status.Detail
		}
		c := ai.New("", "", "", 150*time.Second)
		c.Pocket = pocket.WithModel(st.AIPocketModel)
		return c, "pocket", ""
	}
	if st.OpenAIKey == "" {
		return nil, "", status.Detail
	}
	return ai.New(st.OpenAIBaseURL, st.OpenAIKey, st.OpenAIModel, 150*time.Second), "openai", ""
}

func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	cl, used, why := s.aiFor(ctx, actorID(ctx), st)
	writeJSON(w, http.StatusOK, map[string]any{"ready": cl != nil, "provider": used, "detail": why, "brief": st.AIBrief})
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, status := s.core.SelectAI(ctx, userFrom(ctx).Email, "pocket", false)
	if c == nil || !status.Ready {
		writeErr(w, http.StatusServiceUnavailable, status.Detail)
		return
	}
	models, err := c.Models(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func validAIProvider(v string) bool { return core.ValidAIProvider(v) }

func validPocketModel(v string) bool { return core.ValidInferenceModel(v) }

const aiVoice = `You are the weather app of one person. You speak plainly and briefly, like a friend who looked at the forecast for them: no jargon, no emojis, no markdown headings, no greeting. Only use the forecast and the facts you are given; never invent numbers. Use the units you are told. Times are the local time of the place.`

// weatherFacts is the forecast of a place as text for the AI: now, the next
// 24 hours every three hours, ten days, air and pollen, the warnings and
// the person's plans.
func (s *Server) weatherFacts(ctx context.Context, uid int64, st store.Settings, l *store.Location, f *weather.Forecast, days int) string {
	u := unitsOf(st)
	var b strings.Builder
	loc := f.Location()
	now := time.Now().In(loc)
	fmt.Fprintf(&b, "Place: %s. It is %s, %s there.\n", l.Label(), now.Format("Monday 2 January 2006"), now.Format("15:04"))
	n := f.Now
	fmt.Fprintf(&b, "Now: %s, %s (feels like %s), wind %s gusting %s, humidity %.0f %%.\n", n.Text, u.temp(val0(n.Temp)), u.temp(val0(n.Feels)),
		u.wind(val0(n.Wind)), u.wind(val0(n.Gust)), val0(n.Humidity))
	b.WriteString("Next 24 hours:\n")
	for i, h := range f.HoursFrom(24) {
		if i%3 != 0 {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s, %s, rain chance %.0f %% (%s), gusts %s\n", h.Time[11:16], weather.Describe(h.Code, h.IsDay), u.temp(val0(h.Temp)),
			val0(h.PrecipProb), u.precip(val0(h.Precip)), u.wind(val0(h.Gust)))
	}
	b.WriteString("Days:\n")
	today := f.LocalDate()
	for _, d := range f.Days {
		if d.Date < today || days <= 0 {
			continue
		}
		days--
		t, _ := store.ParseDate(d.Date)
		fmt.Fprintf(&b, "- %s %s: %s, %s to %s, rain %s (chance %.0f %%), gusts %s, UV %.0f, sunrise %s, sunset %s\n", t.Format("Mon 2 Jan"), dayName(d.Date, today),
			d.Text, u.temp(val0(d.TMin)), u.temp(val0(d.TMax)), u.precip(val0(d.Precip)), val0(d.PrecipProb), u.wind(val0(d.Gust)), val0(d.UV), clock(d.Sunrise), clock(d.Sunset))
	}
	if air, _, err := s.airAt(ctx, l.Lat, l.Lon); err == nil {
		av := airSummary(air, today)
		for _, d := range av.Days {
			var pol []string
			for k, lvl := range d.Levels {
				if weather.LevelRank(lvl) >= 2 {
					pol = append(pol, k+" "+strings.ReplaceAll(lvl, "_", " "))
				}
			}
			line := fmt.Sprintf("- Air %s: %s", dayName(d.Date, today), d.Band)
			if len(pol) > 0 {
				line += "; pollen: " + strings.Join(pol, ", ")
			}
			b.WriteString(line + "\n")
		}
	}
	if ws, _ := s.store.ListWarnings(ctx, uid, today, true, l.ID); len(ws) > 0 {
		b.WriteString("Warnings:\n")
		for _, w := range ws {
			fmt.Fprintf(&b, "- %s (%s)\n", w.Title, map[string]string{"warning": "warning", "advisory": "heads-up"}[w.Level])
		}
	}
	if l.IsHome {
		if p, err := s.plans(ctx, uid, false); err == nil {
			if len(p.Events) > 0 {
				b.WriteString("Their calendar:\n")
				for _, e := range p.Events {
					fmt.Fprintf(&b, "- %s %s at %s: %s, %s, rain chance %.0f %%\n", e.Start, e.Title, e.Place, weather.Describe(e.Code, true), u.temp(val0(e.Temp)), val0(e.Prob))
				}
			}
			if len(p.Games) > 0 {
				b.WriteString("Their coming games:\n")
				for _, e := range p.Games {
					where := e.Place
					if e.Indoor {
						where += " (indoors)"
					}
					fmt.Fprintf(&b, "- %s %s at %s: %s, %s, rain chance %.0f %%; %s\n", e.Start, e.Title, where, weather.Describe(e.Code, true), u.temp(val0(e.Temp)), val0(e.Prob), e.Players)
				}
			}
			if len(p.Outings) > 0 {
				b.WriteString("Their planned outings (Pocket Trails), with the weather where the route starts:\n")
				for _, e := range p.Outings {
					back := ""
					if e.Back != "" {
						back = ", back by " + e.Back
					}
					fmt.Fprintf(&b, "- %s%s %s at %s: %s, %s, rain chance %.0f %%; %s\n", e.Start, back, e.Title, e.Place, weather.Describe(e.Code, true), u.temp(val0(e.Temp)), val0(e.Prob), e.Outing)
				}
			}
			for _, t := range p.Trips {
				fmt.Fprintf(&b, "Trip %s to %s, %s to %s: %s\n", t.Name, t.Place, t.Start, t.End, t.Summary)
			}
			if len(p.Advice) > 0 {
				b.WriteString("Advice already worked out:\n")
				for _, a := range p.Advice {
					fmt.Fprintf(&b, "- %s: %s\n", a.Title, a.Detail)
				}
			}
		}
	}
	return b.String()
}

// handleAIBrief writes the day in plain words: what it will be like, what to
// wear, whether to take an umbrella. Kept per place for three hours.
func (s *Server) handleAIBrief(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Location int64 `json:"location_id"`
		Fresh    bool  `json:"fresh"`
	}
	_ = decode(r, &in)
	var l *store.Location
	var err error
	if in.Location > 0 {
		l, err = s.store.GetLocation(ctx, uid, in.Location)
	} else {
		l, err = s.store.HomeLocation(ctx, uid)
	}
	if err != nil {
		s.fail(w, err, "place")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	f, _, err := s.forecastOf(ctx, l)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	now := time.Now().In(f.Location())
	key := fmt.Sprintf("ai:brief:%d:%d:%s:%s%d", uid, l.ID, st.TempUnit, now.Format("2006-01-02"), now.Hour()/3)
	if !in.Fresh {
		if raw, at, ok := s.store.CacheGet(ctx, key); ok {
			var out map[string]any
			if json.Unmarshal([]byte(raw), &out) == nil {
				out["cached"], out["at"] = true, at
				writeJSON(w, http.StatusOK, out)
				return
			}
		}
	}
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": why})
		return
	}
	part := "the rest of today and tomorrow morning"
	if now.Hour() >= 18 {
		part = "this evening, tonight and tomorrow"
	}
	prompt := s.weatherFacts(ctx, uid, st, l, f, 3) + fmt.Sprintf(`
Write about %s in 3 or 4 short sentences: what it will be like, what to wear, whether an umbrella is needed, and anything from the warnings or their plans worth knowing. Then one line starting "Wear:" with a few words. Plain text only.`, part)
	text, err := cl.Complete(ctx, aiVoice, prompt, false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	out := map[string]any{"ok": true, "provider": used, "text": strings.TrimSpace(text), "place": l.Name}
	if b, err := json.Marshal(out); err == nil {
		_ = s.store.CachePut(ctx, key, string(b))
	}
	out["at"] = store.Now()
	writeJSON(w, http.StatusOK, out)
}

// handleAIAsk answers a question about the weather ("can I hang the washing
// out tomorrow?") from the place's forecast and the person's plans.
func (s *Server) handleAIAsk(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Question string `json:"question"`
		Location int64  `json:"location_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Question = strings.TrimSpace(in.Question)
	if len(in.Question) < 3 || len(in.Question) > 1000 {
		writeErr(w, http.StatusBadRequest, "ask a question of up to 1000 characters")
		return
	}
	var l *store.Location
	var err error
	if in.Location > 0 {
		l, err = s.store.GetLocation(ctx, uid, in.Location)
	} else {
		l, err = s.store.HomeLocation(ctx, uid)
	}
	if err != nil {
		s.fail(w, err, "place")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": why})
		return
	}
	f, _, err := s.forecastOf(ctx, l)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	prompt := s.weatherFacts(ctx, uid, st, l, f, 10) + `
Other places they follow: ` + s.otherPlaces(ctx, uid, l.ID) + `

Question: """` + in.Question + `"""
Answer in at most 4 short sentences from the forecast above. If it is about a day beyond ten days, say the forecast does not reach that far. If it is about another place than these, say you only have the forecast for their places. Plain text only.`
	text, err := cl.Complete(ctx, aiVoice, prompt, false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": used, "answer": strings.TrimSpace(text), "place": l.Name})
}

func (s *Server) otherPlaces(ctx context.Context, uid, skip int64) string {
	ls, _ := s.store.ListLocations(ctx, uid)
	u := s.unitsFor(ctx, uid)
	var out []string
	for _, l := range ls {
		if l.ID == skip {
			continue
		}
		f, _, err := s.forecastOf(ctx, &l)
		if err != nil {
			continue
		}
		var days []string
		for i, d := range f.Days {
			if d.Date < f.LocalDate() || i > 6 {
				continue
			}
			days = append(days, fmt.Sprintf("%s %s %s–%s", d.Date[5:], strings.ToLower(d.Text), u.temp(val0(d.TMin)), u.temp(val0(d.TMax))))
		}
		out = append(out, l.Name+" ("+strings.Join(days, "; ")+")")
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, "\n")
}
