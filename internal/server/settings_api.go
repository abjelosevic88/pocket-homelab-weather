package server

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-weather/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/weather"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with the secrets
// masked and, per Pocket app, the mode in effect (<app>_mode), whether it
// works and how (<app>_via: core | direct | "") and why not (<app>_detail).
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	out := map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "date_format": st.DateFormat, "time_format": st.TimeFormat, "week_start": st.WeekStart,
		"temp_unit": st.TempUnit, "wind_unit": st.WindUnit, "precip_unit": st.PrecipUnit, "language": st.Language, "follow_me": st.FollowMe,
		"warn_frost": st.WarnFrost, "frost_c": st.FrostC, "warn_heat": st.WarnHeat, "heat_c": st.HeatC, "warn_wind": st.WarnWind, "gust_kmh": st.GustKmh,
		"warn_rain": st.WarnRain, "rain_mm": st.RainMM, "warn_snow": st.WarnSnow, "snow_cm": st.SnowCm, "warn_storm": st.WarnStorm,
		"warn_uv": st.WarnUV, "uv_max": st.UVMax, "warn_pollen": st.WarnPollen, "pollen_kinds": st.PollenKinds, "pollen_level": st.PollenLevel,
		"warn_air": st.WarnAir, "aqi_max": st.AQIMax,
		"notify_morning": st.NotifyMorning, "notify_warnings": st.NotifyWarnings, "quiet_hours": st.QuietHours, "notify_hour": st.NotifyHour,
		"notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_configured": st.NotifyURL != "",
		"calendar_weather": st.CalendarWeather, "travel_weather": st.TravelWeather, "home_plants": st.HomePlants, "garage_tyres": st.GarageTyres, "pets_walks": st.PetsWalks,
		"league_weather": st.LeagueWeather, "trails_weather": st.TrailsWeather,
		"ai_brief":    st.AIBrief,
		"ai_provider": st.AIProvider, "ai_pocket_model": st.AIPocketModel, "openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel, "openai_base_url": st.OpenAIBaseURL,
		"setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL,
	}
	for _, a := range pocketApps {
		mode, via, detail := s.connStatus(ctx, u, st, a)
		out[a.slug+"_mode"], out[a.slug+"_via"], out[a.slug+"_detail"] = mode, via, detail
		out[a.slug+"_configured"] = via != ""
		_, url, token, public := connSettings(st, a)
		out[a.slug+"_url"], out[a.slug+"_token"], out[a.slug+"_public_url"] = url, mask(token), public
		if public == "" && via != "" {
			public = s.resolvePublic(ctx, a.slug)
		}
		out[a.slug+"_open_url"] = public
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

// handlePutSettings saves the caller's personal settings. The box-wide title
// is only taken from an admin.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	raw := map[string]any{}
	var in store.Settings
	keys, err := decodeKeys(r, &raw)
	if err != nil || remarshal(raw, &in) != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	bad := func(msg string) { writeErr(w, http.StatusBadRequest, msg) }
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	if keys["date_format"] {
		cur.DateFormat = in.DateFormat
	}
	if keys["time_format"] {
		cur.TimeFormat = in.TimeFormat
	}
	if keys["week_start"] && (in.WeekStart == 1 || in.WeekStart == 7 || in.WeekStart == 6) {
		cur.WeekStart = in.WeekStart
	}
	if keys["temp_unit"] && oneOf(in.TempUnit, []string{"c", "f"}) {
		cur.TempUnit = in.TempUnit
	}
	if keys["wind_unit"] && oneOf(in.WindUnit, []string{"kmh", "ms", "mph", "kn", "bft"}) {
		cur.WindUnit = in.WindUnit
	}
	if keys["precip_unit"] && oneOf(in.PrecipUnit, []string{"mm", "in"}) {
		cur.PrecipUnit = in.PrecipUnit
	}
	if keys["language"] {
		l := strings.ToLower(strings.TrimSpace(in.Language))
		if l != "" && (len(l) != 2 || strings.Trim(l, "abcdefghijklmnopqrstuvwxyz") != "") {
			bad("the language is a two-letter code like en or bs")
			return
		}
		cur.Language = l
	}
	num := func(k string, dst *float64, v, lo, hi float64, msg string) bool {
		if !keys[k] {
			return true
		}
		if v < lo || v > hi {
			bad(msg)
			return false
		}
		*dst = v
		return true
	}
	if !num("frost_c", &cur.FrostC, in.FrostC, -20, 5, "frost is −20 to 5 °C") || !num("heat_c", &cur.HeatC, in.HeatC, 20, 45, "heat is 20 to 45 °C") ||
		!num("gust_kmh", &cur.GustKmh, in.GustKmh, 30, 150, "gusts are 30 to 150 km/h") || !num("rain_mm", &cur.RainMM, in.RainMM, 5, 200, "rain is 5 to 200 mm a day") ||
		!num("snow_cm", &cur.SnowCm, in.SnowCm, 1, 100, "snow is 1 to 100 cm a day") || !num("uv_max", &cur.UVMax, in.UVMax, 3, 12, "UV is 3 to 12") ||
		!num("aqi_max", &cur.AQIMax, in.AQIMax, 20, 150, "the air quality index is 20 to 150") {
		return
	}
	if keys["pollen_level"] && oneOf(in.PollenLevel, []string{"moderate", "high", "very_high"}) {
		cur.PollenLevel = in.PollenLevel
	}
	if keys["pollen_kinds"] {
		kinds := []string{}
		for _, k := range in.PollenKinds {
			if oneOf(k, weather.PollenKinds) && !oneOf(k, kinds) {
				kinds = append(kinds, k)
			}
		}
		cur.PollenKinds = kinds
	}
	if keys["notify_hour"] {
		if in.NotifyHour < 0 || in.NotifyHour > 23 {
			bad("the hour is 0 to 23")
			return
		}
		cur.NotifyHour = in.NotifyHour
	}
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	url := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	text := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimSpace(v)
		}
	}
	for k, dst := range map[string]*bool{"follow_me": &cur.FollowMe, "warn_frost": &cur.WarnFrost, "warn_heat": &cur.WarnHeat, "warn_wind": &cur.WarnWind,
		"warn_rain": &cur.WarnRain, "warn_snow": &cur.WarnSnow, "warn_storm": &cur.WarnStorm, "warn_uv": &cur.WarnUV, "warn_pollen": &cur.WarnPollen, "warn_air": &cur.WarnAir,
		"notify_morning": &cur.NotifyMorning, "notify_warnings": &cur.NotifyWarnings, "quiet_hours": &cur.QuietHours,
		"calendar_weather": &cur.CalendarWeather, "travel_weather": &cur.TravelWeather, "home_plants": &cur.HomePlants, "garage_tyres": &cur.GarageTyres,
		"pets_walks": &cur.PetsWalks, "league_weather": &cur.LeagueWeather, "trails_weather": &cur.TrailsWeather, "ai_brief": &cur.AIBrief} {
		if v, ok := raw[k].(bool); ok && keys[k] {
			*dst = v
		}
	}
	text("notify_url", &cur.NotifyURL, in.NotifyURL)
	if keys["notify_format"] && in.NotifyFormat != "" {
		cur.NotifyFormat = in.NotifyFormat
	}
	// each Pocket app: mode, URL, token, public URL
	inMap := map[string]string{}
	for k, v := range raw {
		if sv, ok := v.(string); ok {
			inMap[k] = sv
		}
	}
	for _, a := range pocketApps {
		c := cur.Conns[a.slug]
		if v := inMap[a.slug+"_mode"]; keys[a.slug+"_mode"] && (v == modeCore || v == modeDirect || v == modeOff) {
			c.Mode = v
		}
		url(a.slug+"_url", &c.URL, inMap[a.slug+"_url"])
		secret(a.slug+"_token", &c.Token, inMap[a.slug+"_token"])
		url(a.slug+"_public_url", &c.PublicURL, inMap[a.slug+"_public_url"])
		if c != (store.Conn{}) {
			cur.Conns[a.slug] = c
		}
	}
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			bad("AI is automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			bad("unknown model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	text("openai_model", &cur.OpenAIModel, in.OpenAIModel)
	url("openai_base_url", &cur.OpenAIBaseURL, in.OpenAIBaseURL)
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && keys["title"] {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	for k := range keys {
		if isModeKey(k) {
			s.reportModesSoon()
			break
		}
	}
	// what counts as a warning or a plan may have changed
	s.afterPlaces(r, me.ID)
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	uid := actorID(r.Context())
	st, _ := s.store.GetSettings(r.Context(), uid)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return strings.TrimRight(strings.TrimSpace(in.URL), "/")
	}
	var msg string
	var err error
	if a, ok := appBySlug(in.Which); ok {
		mode, savedURL, savedTok, _ := connSettings(st, a)
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var conn *siblingConn
		if tok := keyOr(savedTok); tok != "" && (typed || s.connMode(mode, savedTok) == modeDirect) {
			base := urlOr(savedURL)
			if base == "" {
				base = s.resolveSibling(ctx, a.slug)
			}
			conn = &siblingConn{client: pocket.New(base, tok, 15*time.Second), via: modeDirect}
		} else {
			conn, err = s.clients(ctx, uid).conn(ctx, a, a.scopes...)
		}
		if err == nil {
			how := "direct connection"
			if conn.via == modeCore {
				how = "through Pocket Core"
			}
			var v string
			if v, err = conn.client.Health(ctx); err == nil {
				msg = fmt.Sprintf("%s %s, %s", a.name, v, how)
				msg += s.testDetail(ctx, a, conn)
			}
		}
	} else if in.Which == "openai" {
		c := ai.New(urlOr(st.OpenAIBaseURL), keyOr(st.OpenAIKey), st.OpenAIModel, 20*time.Second)
		var ids []string
		if ids, err = c.Models(ctx); err == nil {
			msg = fmt.Sprintf("The key works (%d models)", len(ids))
		}
	} else if in.Which == "notify" {
		f := in.Format
		if f == "" {
			f = st.NotifyFormat
		}
		n := notify.New(urlOr(st.NotifyURL), f)
		err = n.Send(ctx, notify.Message{Title: "Pocket Weather", Body: notifyTestBody, Priority: "info", Tags: []string{"partly_sunny"}})
		msg = "Test message sent via " + n.Format
	} else {
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

// ---- system / backup / export ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())),
	})
}

// handleBackup downloads the whole database, an admin-only box operation.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-weather-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}

// exportDoc is the JSON export of one person's weather: places, diary,
// warnings and what was done with advice.
type exportDoc struct {
	App       string            `json:"app"`
	Version   string            `json:"version"`
	Exported  string            `json:"exported_at"`
	Locations []store.Location  `json:"locations"`
	Days      []store.DayRecord `json:"days"`
	Warnings  []store.Warning   `json:"warnings"`
	Marks     []store.Mark      `json:"advice_marks"`
}

func (s *Server) export(ctx context.Context, uid int64) exportDoc {
	doc := exportDoc{App: "pocket-weather", Version: Version, Exported: store.Now()}
	doc.Locations, _ = s.store.ListLocations(ctx, uid)
	doc.Days, _ = s.store.AllDays(ctx, uid)
	doc.Warnings, _ = s.store.RecentWarnings(ctx, uid, "0000-01-01", "9999-12-31")
	doc.Marks, _ = s.store.ListMarks(ctx, uid)
	return doc
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-weather-export.json"`)
	writeJSON(w, http.StatusOK, s.export(r.Context(), actorID(r.Context())))
}

// handleExportCSV writes the diary as CSV: one row per place and day.
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	locs, err := s.store.ListLocations(ctx, uid)
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	names := map[int64]string{}
	for _, l := range locs {
		names[l.ID] = l.Name
	}
	days, err := s.store.AllDays(ctx, uid)
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-weather-diary.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"place", "date", "weather", "t_min_c", "t_max_c", "precip_mm", "snow_cm", "wind_kmh", "gust_kmh", "uv", "sunshine_h", "aqi", "note"})
	f := func(p *float64) string {
		if p == nil {
			return ""
		}
		return strconv.FormatFloat(*p, 'f', 1, 64)
	}
	for _, d := range days {
		_ = cw.Write([]string{names[d.LocationID], d.Date, weather.Describe(d.Code, true), f(d.TMin), f(d.TMax), f(d.Precip), f(d.Snow), f(d.Wind), f(d.Gust), f(d.UV), f(d.SunshineH), f(d.AQI), d.Note})
	}
	cw.Flush()
}
