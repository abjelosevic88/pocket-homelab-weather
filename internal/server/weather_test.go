package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-weather/internal/config"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/weather"
)

func sarajevo() map[string]any {
	return map[string]any{"name": "Sarajevo", "country": "Bosnia and Herzegovina", "country_code": "ba", "lat": 43.8486, "lon": 18.3564, "timezone": "Europe/Sarajevo"}
}

// Every place, diary day and warning belongs to one person; someone else's
// ids are 404, and an admin sees no more than anyone else.
func TestPerPersonIsolation(t *testing.T) {
	a := newApp(t, nil)
	home := a.expect(201, a.alice, "POST", "/api/v1/locations", sarajevo())
	if home["is_home"] != true || home["country_code"] != "BA" {
		t.Fatalf("the first place is home: %v", home)
	}
	hid := id(home)
	a.ok(a.alice, "POST", "/api/v1/locations", map[string]any{"name": "Mostar", "lat": 43.3433, "lon": 17.8081})

	// Bob sees nothing of Alice's and cannot touch it
	if l := a.list(a.bob, "/api/v1/locations"); len(l) != 0 {
		t.Fatalf("bob sees %d places", len(l))
	}
	a.expect(404, a.bob, "GET", "/api/v1/locations/"+hid, nil)
	a.expect(404, a.bob, "PUT", "/api/v1/locations/"+hid, map[string]any{"name": "Mine"})
	a.expect(404, a.bob, "DELETE", "/api/v1/locations/"+hid, nil)
	a.expect(404, a.bob, "GET", "/api/v1/forecast?location="+hid, nil)
	a.expect(404, a.bob, "GET", "/api/v1/diary?location="+hid, nil)
	a.expect(404, a.bob, "PUT", "/api/v1/diary/"+hid+"/"+store.Today(), map[string]any{"note": "x"})
	a.expect(404, a.bob, "POST", "/api/v1/ai/brief", map[string]any{"location_id": n(hid)})

	// Alice's overview has her places and runs the warnings check
	ov := a.ok(a.alice, "GET", "/api/v1/overview", nil)
	if ps, _ := ov["places"].([]any); len(ps) != 2 {
		t.Fatalf("alice has %d places", len(ps))
	}
	if ws, _ := ov["warnings"].([]any); len(ws) == 0 {
		t.Fatal("the demo weather has a frosty morning and a rainy afternoon; no warnings found")
	}
	bob := a.ok(a.bob, "GET", "/api/v1/warnings", nil)
	if ws, _ := bob["active"].([]any); len(ws) != 0 {
		t.Fatalf("bob sees %d of alice's warnings", len(ws))
	}
	// the diary got today and yesterday for her places only
	var nd int
	_ = a.st.DB.QueryRow(`SELECT COUNT(1) FROM days WHERE user_id = ?`, a.ids["Bob"]).Scan(&nd)
	if nd != 0 {
		t.Fatalf("bob has %d diary days", nd)
	}
	_ = a.st.DB.QueryRow(`SELECT COUNT(1) FROM days WHERE user_id = ?`, a.ids["Alice"]).Scan(&nd)
	if nd < 2 {
		t.Fatalf("alice has %d diary days, want today and yesterday per place", nd)
	}

	// a second place made home takes home from the first
	m := a.list(a.alice, "/api/v1/locations")
	mostar := m[1].(map[string]any)
	a.ok(a.alice, "PUT", "/api/v1/locations/"+id(mostar), map[string]any{"is_home": true})
	if l := a.list(a.alice, "/api/v1/locations"); l[0].(map[string]any)["name"] != "Mostar" || l[1].(map[string]any)["is_home"] != false {
		t.Fatalf("home did not move: %v", l)
	}
	// removing home makes the other place home
	a.expect(204, a.alice, "DELETE", "/api/v1/locations/"+id(mostar), nil)
	if l := a.list(a.alice, "/api/v1/locations"); len(l) != 1 || l[0].(map[string]any)["is_home"] != true {
		t.Fatalf("after removing home: %v", l)
	}
}

func TestSummaryForecastAndDay(t *testing.T) {
	a := newApp(t, nil)
	// no place yet: the summary says so instead of failing
	if s := a.ok(a.alice, "GET", "/api/v1/summary", nil); s["place"] != nil {
		t.Fatalf("summary without places: %v", s)
	}
	a.ok(a.alice, "POST", "/api/v1/locations", sarajevo())
	s := a.ok(a.alice, "GET", "/api/v1/summary", nil)
	if s["today"] == nil || !strings.Contains(s["text"].(string), "°C") {
		t.Fatalf("summary: %v", s)
	}
	f := a.ok(a.alice, "GET", "/api/v1/forecast", nil)
	if hs, _ := f["hours"].([]any); len(hs) != 48 {
		t.Fatalf("48 hours, got %d", len(hs))
	}
	if ds, _ := f["days"].([]any); len(ds) < 10 {
		t.Fatalf("10 days, got %d", len(ds))
	}
	// any spot, without a saved place
	spot := a.ok(a.alice, "GET", "/api/v1/forecast?lat=48.2&lon=16.37&hours=12", nil)
	if hs, _ := spot["hours"].([]any); len(hs) != 12 || spot["location"] != nil {
		t.Fatalf("spot forecast: %d hours, location %v", len(hs), spot["location"])
	}
	a.expect(400, a.alice, "GET", "/api/v1/forecast?lat=100&lon=1", nil)
	// a day: tomorrow from the forecast, last year from the archive
	if d := a.ok(a.alice, "GET", "/api/v1/day/"+store.AddDays(store.Today(), 1), nil); d["source"] != "forecast" {
		t.Fatalf("tomorrow: %v", d["source"])
	}
	if d := a.ok(a.alice, "GET", "/api/v1/day/"+store.AddDays(store.Today(), -400), nil); d["source"] != "archive" || d["text"] == "" {
		t.Fatalf("last year: %v", d)
	}
	a.expect(400, a.alice, "GET", "/api/v1/day/not-a-date", nil)
	// yesterday is in the diary after the check, and takes a note
	locs := a.list(a.alice, "/api/v1/locations")
	lid := id(locs[0].(map[string]any))
	y := store.AddDays(store.Today(), -1)
	a.ok(a.alice, "PUT", "/api/v1/diary/"+lid+"/"+y, map[string]any{"note": "Grey and cold"})
	if d := a.ok(a.alice, "GET", "/api/v1/day/"+y, nil); d["source"] != "diary" || d["note"] != "Grey and cold" {
		t.Fatalf("yesterday from the diary: %v", d)
	}
	dia := a.ok(a.alice, "GET", "/api/v1/diary?month="+y[:7], nil)
	if ds, _ := dia["days"].([]any); len(ds) == 0 {
		t.Fatal("the diary month is empty")
	}
}

// Settings are per person; thresholds are checked.
func TestSettingsPerPerson(t *testing.T) {
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"temp_unit": "f", "frost_c": -2, "warn_pollen": true, "pollen_kinds": []string{"grass", "nope", "grass"}})
	al := a.ok(a.alice, "GET", "/api/v1/settings", nil)
	if al["temp_unit"] != "f" || num(al, "frost_c") != -2 || al["warn_pollen"] != true {
		t.Fatalf("alice: %v %v %v", al["temp_unit"], al["frost_c"], al["warn_pollen"])
	}
	if k, _ := al["pollen_kinds"].([]any); len(k) != 1 {
		t.Fatalf("pollen kinds are cleaned: %v", k)
	}
	bob := a.ok(a.bob, "GET", "/api/v1/settings", nil)
	if bob["temp_unit"] != "c" || num(bob, "frost_c") != 0 || bob["warn_pollen"] != false {
		t.Fatalf("bob got alice's settings: %v %v", bob["temp_unit"], bob["frost_c"])
	}
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"heat_c": 80})
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"language": "english"})
}

// Exporting and importing twice adds the places once.
func TestExportImportIdempotent(t *testing.T) {
	a := newApp(t, nil)
	a.ok(a.alice, "POST", "/api/v1/locations", sarajevo())
	a.ok(a.alice, "GET", "/api/v1/overview", nil) // fills the diary
	ctx := context.Background()
	doc := a.s.export(ctx, a.ids["Alice"])
	if len(doc.Locations) != 1 || len(doc.Days) == 0 {
		t.Fatalf("export: %d places, %d days", len(doc.Locations), len(doc.Days))
	}
	for i := 0; i < 2; i++ {
		res, err := a.s.importDoc(ctx, a.ids["Bob"], doc)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 && res["places"] != 1 || i == 1 && (res["places"] != 0 || res["already"] != 1) {
			t.Fatalf("import %d: %v", i, res)
		}
	}
	if l := a.list(a.bob, "/api/v1/locations"); len(l) != 1 || l[0].(map[string]any)["is_home"] != true {
		t.Fatalf("bob after import: %v", l)
	}
	// and the import endpoint refuses what is not an export
	a.expect(400, a.bob, "POST", "/api/v1/import", map[string]any{"app": "pocket-wallet"})
}

// Link tokens: weather:read reads the weather but never the plans (they
// carry other apps' data); weather:write only adds places.
func TestLinkScopes(t *testing.T) {
	req := func(m, p string) *http.Request { return httptest.NewRequest(m, p, nil) }
	read := []string{"weather:read"}
	write := []string{"weather:write"}
	for _, p := range []string{"/api/v1/summary", "/api/v1/forecast", "/api/v1/warnings", "/api/v1/day/2026-11-05", "/api/v1/locations", "/api/v1/locations/3", "/api/v1/diary", "/api/v1/air"} {
		if !linkAllows(read, req("GET", p)) {
			t.Errorf("weather:read should allow GET %s", p)
		}
	}
	for _, p := range []string{"/api/v1/plans", "/api/v1/settings", "/api/v1/export.json", "/api/v1/tokens", "/api/v1/users", "/api/v1/backup"} {
		if linkAllows(read, req("GET", p)) || linkAllows(write, req("GET", p)) {
			t.Errorf("GET %s must stay closed to links", p)
		}
	}
	for _, m := range []string{"POST /api/v1/advice/task", "POST /api/v1/ai/ask", "POST /api/v1/import", "PUT /api/v1/settings"} {
		parts := strings.Fields(m)
		if linkAllows(append(read, write...), req(parts[0], parts[1])) {
			t.Errorf("%s must stay closed to links", m)
		}
	}
	if linkAllows(read, req("POST", "/api/v1/locations")) || !linkAllows(write, req("POST", "/api/v1/locations")) {
		t.Error("adding a place needs weather:write")
	}
	if !linkAllows(read, req("GET", "/api/v1/auth/me")) {
		t.Error("auth/me is open to any link")
	}
}

// The rules: the demo weather has frost tomorrow morning and heavy rain the
// day after; thresholds and switches decide.
func TestWarningRules(t *testing.T) {
	f, err := weather.Demo{}.Forecast(context.Background(), 43.85, 18.36)
	if err != nil {
		t.Fatal(err)
	}
	air, _ := weather.Demo{}.Air(context.Background(), 43.85, 18.36)
	st := store.Settings{WarnFrost: true, FrostC: 50, WarnRain: true, RainMM: 25, TempUnit: "c", WindUnit: "kmh", PrecipUnit: "mm"}
	found := evaluate(f, air, st, unitsOf(st))
	kinds := map[string]finding{}
	for _, fd := range found {
		kinds[fd.Kind] = fd
	}
	if _, ok := kinds["frost"]; !ok {
		t.Errorf("frost threshold 50 °C: every morning is frost, got %v", found)
	}
	if r, ok := kinds["rain"]; !ok || r.Day != store.AddDays(f.LocalDate(), 2) || !strings.Contains(r.Title, "27 mm") {
		t.Errorf("heavy rain the day after tomorrow: %+v", r)
	}
	st.WarnRain = false
	for _, fd := range evaluate(f, air, st, unitsOf(st)) {
		if fd.Kind == "rain" {
			t.Error("rain warnings are switched off")
		}
	}
	if got := unitsOf(store.Settings{TempUnit: "f"}).temp(0); got != "32 °F" {
		t.Errorf("0 °C in °F: %q", got)
	}
	if got := (units{t: "c"}).temp(-3.4); got != "−3 °C" {
		t.Errorf("minus sign: %q", got)
	}
}

func TestPlaceText(t *testing.T) {
	for in, want := range map[string]string{
		"Zmaja od Bosne 8, 71000 Sarajevo": "Sarajevo",
		"Vienna, Austria":                  "Vienna",
		"Koševo":                           "Koševo",
		"https://meet.example.com/abc":     "",
		"":                                 "",
	} {
		if got := cleanPlaceText(in); got != want {
			t.Errorf("cleanPlaceText(%q) = %q, want %q", in, got, want)
		}
	}
	if !outdoor(pocket.Plant{Name: "Lemon", Room: "Balcony"}) || outdoor(pocket.Plant{Name: "Monstera", Room: "Living room"}) {
		t.Error("balcony plants are outside, living room ones are not")
	}
}

// The demo's made-up linked apps give advice for every app, and the
// marks stick per person.
func TestDemoPlans(t *testing.T) {
	a := newApp(t, func(c *config.Config) { c.DemoData = true })
	if a.s.store.OwnerID(context.Background()) != a.ids["Alice"] {
		t.Skip("owner is not alice")
	}
	a.ok(a.alice, "POST", "/api/v1/locations", sarajevo())
	p := a.ok(a.alice, "GET", "/api/v1/plans?fresh=1", nil)
	if p["demo"] != true {
		t.Fatalf("demo plans: %v", p["demo"])
	}
	apps := map[string]bool{}
	var key string
	for _, x := range p["advice"].([]any) {
		ad := x.(map[string]any)
		apps[ad["app"].(string)] = true
		if ad["app"] == "garage" && ad["task"] != nil {
			key = ad["key"].(string)
		}
	}
	for _, app := range []string{"home", "garage", "pets", "travel"} {
		if !apps[app] {
			t.Errorf("no advice from %s: %v", app, p["advice"])
		}
	}
	if evs, _ := p["events"].([]any); len(evs) == 0 {
		t.Error("the demo calendar has events")
	}
	// Pocket League: the two games with a time (the one put to a vote is left
	// out), the tennis at its venue, the futsal in a hall without warnings
	games, _ := p["games"].([]any)
	if len(games) != 2 {
		t.Fatalf("demo games: %v", p["games"])
	}
	for _, x := range games {
		g := x.(map[string]any)
		switch g["id"] {
		case "game:31":
			if g["place"] != "TK Koševo" || g["indoor"] == true || !strings.Contains(fmt.Sprint(g["players"]), "3 in, 4 needed") {
				t.Errorf("tennis: %v", g)
			}
		case "game:32":
			if g["indoor"] != true || len(g["flags"].([]any)) != 0 || !strings.Contains(fmt.Sprint(g["players"]), "you: in") {
				t.Errorf("futsal: %v", g)
			}
		default:
			t.Errorf("unexpected game %v", g)
		}
	}
	if la, _ := p["apps"].(map[string]any)["league"].(map[string]any); la["on"] != true || la["count"] != float64(2) {
		t.Errorf("league app state: %v", la)
	}
	if !strings.Contains(fmt.Sprint(linkUses()), "league [league:read]") {
		t.Errorf("Weather does not ask for league:read: %v", linkUses())
	}
	// Pocket Trails: the hike at its route's start from setting off to back
	// by, the ride without a route at home for the day; a planned outing of
	// a past day is left out
	outs, _ := p["outings"].([]any)
	if len(outs) != 2 {
		t.Fatalf("demo outings: %v", p["outings"])
	}
	for _, x := range outs {
		o := x.(map[string]any)
		switch o["id"] {
		case "outing:41":
			if o["all_day"] == true || !strings.HasSuffix(fmt.Sprint(o["start"]), "T08:30") || !strings.HasSuffix(fmt.Sprint(o["back"]), "T15:00") ||
				o["outing"] != "14.2 km · 980 m up" || o["location"] != "Trebević from Bistrik" {
				t.Errorf("hike: %v", o)
			}
		case "outing:42":
			if o["place"] != p["place"] || o["back"] != nil || o["title"] != "Evening ride" {
				t.Errorf("ride: %v", o)
			}
		default:
			t.Errorf("unexpected outing %v", o)
		}
	}
	if ta, _ := p["apps"].(map[string]any)["trails"].(map[string]any); ta["on"] != true || ta["count"] != float64(2) {
		t.Errorf("trails app state: %v", ta)
	}
	if !strings.Contains(fmt.Sprint(linkUses()), "trails [trails:read]") {
		t.Errorf("Weather does not ask for trails:read: %v", linkUses())
	}
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"trails_weather": false})
	off := a.ok(a.alice, "GET", "/api/v1/plans?fresh=1", nil)
	if ta, _ := off["apps"].(map[string]any)["trails"].(map[string]any); ta["on"] != false || len(off["outings"].([]any)) != 0 {
		t.Errorf("trails switched off: %v / %v", ta, off["outings"])
	}
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"trails_weather": true})
	if key == "" {
		t.Fatalf("no tyre advice with a task in %s (month %s)", p["advice"], time.Now().Month())
	}
	a.ok(a.alice, "POST", "/api/v1/advice/task", map[string]any{"key": key})
	p = a.ok(a.alice, "GET", "/api/v1/plans", nil)
	for _, x := range p["advice"].([]any) {
		if ad := x.(map[string]any); ad["key"] == key && ad["mark"] != "task" {
			t.Fatalf("the advice is not marked as a task: %v", ad)
		}
	}
	// bob has no demo apps and no places: nothing
	bp := a.ok(a.bob, "GET", "/api/v1/plans", nil)
	if ad, _ := bp["advice"].([]any); len(ad) != 0 {
		t.Fatalf("bob got advice: %v", ad)
	}
}
