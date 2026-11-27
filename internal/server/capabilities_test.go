package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/config"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/weather"
)

// countingWx is the demo weather that counts how often it is asked: the
// capability routes must never ask it.
type countingWx struct {
	weather.Demo
	n atomic.Int64
}

func (c *countingWx) Forecast(ctx context.Context, lat, lon float64) (*weather.Forecast, error) {
	c.n.Add(1)
	return c.Demo.Forecast(ctx, lat, lon)
}
func (c *countingWx) Air(ctx context.Context, lat, lon float64) (*weather.Air, error) {
	c.n.Add(1)
	return c.Demo.Air(ctx, lat, lon)
}
func (c *countingWx) Archive(ctx context.Context, lat, lon float64, from, to string) ([]weather.Day, error) {
	c.n.Add(1)
	return c.Demo.Archive(ctx, lat, lon, from, to)
}
func (c *countingWx) PastDays(ctx context.Context, lat, lon float64, from, to string) ([]weather.Day, error) {
	c.n.Add(1)
	return c.Demo.PastDays(ctx, lat, lon, from, to)
}
func (c *countingWx) Search(ctx context.Context, name, lang string, count int) ([]weather.Place, error) {
	c.n.Add(1)
	return c.Demo.Search(ctx, name, lang, count)
}
func (c *countingWx) Reverse(ctx context.Context, lat, lon float64, lang string) (*weather.Place, error) {
	c.n.Add(1)
	return c.Demo.Reverse(ctx, lat, lon, lang)
}

// capApp is Weather with Alice's two places (Sarajevo at home, Mostar),
// their forecast kept, warnings saved and the diary written, as an open
// page leaves them; wx counts what is fetched from then on.
func capApp(t *testing.T, tweak func(*config.Config)) (*app, *countingWx, map[string]string) {
	t.Helper()
	a := newApp(t, tweak)
	wx := &countingWx{}
	a.s.wx = wx
	home := a.expect(201, a.alice, "POST", "/api/v1/locations", sarajevo())
	mostar := a.expect(201, a.alice, "POST", "/api/v1/locations", map[string]any{"name": "Mostar", "country": "Bosnia and Herzegovina", "lat": 43.3433, "lon": 17.8081,
		"timezone": "Europe/Sarajevo", "note": "Mum and dad"})
	a.ok(a.alice, "GET", "/api/v1/overview", nil)
	wx.n.Store(0)
	return a, wx, map[string]string{"home": id(home), "mostar": id(mostar)}
}

// noFetch fails when anything was fetched since the last check.
func noFetch(t *testing.T, wx *countingWx, what string) {
	t.Helper()
	if n := wx.n.Swap(0); n != 0 {
		t.Fatalf("%s fetched the weather %d times", what, n)
	}
}

// capItems are the rows of a capability answer.
func capItems(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	l, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("no items list in %v", m)
	}
	out := []map[string]any{}
	for _, v := range l {
		out = append(out, v.(map[string]any))
	}
	return out
}

// capItem finds the row of that kind and title.
func capItem(t *testing.T, m map[string]any, kind, title string) map[string]any {
	t.Helper()
	for _, it := range capItems(t, m) {
		if it["kind"] == kind && (title == "" || it["title"] == title) {
			return it
		}
	}
	t.Fatalf("no %s %q in %v", kind, title, m["items"])
	return nil
}

func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }

// activeWarning is one of Alice's warnings that are on (the demo weather has some).
func activeWarning(t *testing.T, a *app) map[string]any {
	t.Helper()
	ws, _ := a.ok(a.alice, "GET", "/api/v1/warnings", nil)["active"].([]any)
	if len(ws) == 0 {
		t.Fatal("the demo weather has a frosty morning and a rainy afternoon; no warnings")
	}
	return ws[0].(map[string]any)
}

// ageCopies makes every kept forecast look fetched at a time.
func ageCopies(t *testing.T, a *app, at time.Time) {
	t.Helper()
	if _, err := a.st.DB.Exec(`UPDATE cache SET fetched_at = ? WHERE key LIKE 'fc:%'`, at.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
}

func TestPocketSearch(t *testing.T) {
	a, wx, ids := capApp(t, nil)
	res := a.ok(a.alice, "GET", "/api/v1/pocket/search?q=saraj", nil)
	got := capItems(t, res)
	if len(got) != 1 {
		t.Fatalf("saraj: %v", got)
	}
	p := got[0]
	if p["id"] != "place:"+ids["home"] || p["kind"] != "place" || p["title"] != "Sarajevo" || p["url"] != "/" {
		t.Fatalf("home: %v", p)
	}
	// "12°, light rain · updated 14:05"
	sn := str(p, "snippet")
	if !strings.Contains(sn, "°, ") || !strings.Contains(sn, " · updated "+time.Now().Format("15:")) {
		t.Fatalf("snippet: %q", sn)
	}
	// another place opens its own page; its note finds it too
	if m := capItem(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=dad", nil), "place", "Mostar"); m["url"] != "/places/"+ids["mostar"] {
		t.Fatalf("mostar: %v", m)
	}
	// not a geocoder: a town that is not saved finds nothing, and nothing is fetched
	if got := capItems(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=vienna", nil)); len(got) != 0 {
		t.Fatalf("vienna: %v", got)
	}
	noFetch(t, wx, "search")

	// a warning by what it says
	w := activeWarning(t, a)
	word := strings.Fields(str(w, "title"))[0]
	wx.n.Store(0)
	hit := capItem(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q="+word, nil), "warning", str(w, "title"))
	if hit["id"] != "warning:"+id(w) || hit["url"] != "/warnings" || hit["date"] != w["day"] || !strings.HasPrefix(str(hit, "snippet"), str(w, "place")+" · ") {
		t.Fatalf("warning: %v", hit)
	}

	// a diary note by its words, in any case
	y := store.AddDays(store.Today(), -1)
	a.ok(a.alice, "PUT", "/api/v1/diary/"+ids["home"]+"/"+y, map[string]any{"note": "First snow on Trebević\nWe went sledging"})
	d := capItem(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=TREBEVIĆ", nil), "diary", "First snow on Trebević")
	if d["id"] != "diary:"+ids["home"]+":"+y || d["url"] != "/diary" || d["date"] != y || !strings.HasPrefix(str(d, "snippet"), "Sarajevo · ") || !strings.Contains(str(d, "snippet"), "°C") {
		t.Fatalf("diary: %v", d)
	}
	if got := capItems(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=sledging", nil)); len(got) != 1 || got[0]["kind"] != "diary" {
		t.Fatalf("second line of the note: %v", got)
	}

	// an old copy is shown with its time; without a copy, only where the place is
	ageCopies(t, a, time.Now().Add(-24*time.Hour))
	if sn := str(capItem(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=sarajevo", nil), "place", "Sarajevo"), "snippet"); !strings.Contains(sn, "°") ||
		!strings.Contains(sn, "updated yesterday ") {
		t.Fatalf("old copy: %q", sn)
	}
	if _, err := a.st.DB.Exec(`DELETE FROM cache WHERE key LIKE 'fc:%'`); err != nil {
		t.Fatal(err)
	}
	if sn := str(capItem(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=sarajevo", nil), "place", "Sarajevo"), "snippet"); sn != "Bosnia and Herzegovina" {
		t.Fatalf("no copy: %q", sn)
	}
	noFetch(t, wx, "search")

	if got := capItems(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=o&limit=1", nil)); len(got) != 1 {
		t.Fatalf("limit 1: %v", got)
	}
	// nothing to look for
	for _, q := range []string{"", "%3F%3F", "*", "%25", "_", "+"} {
		if got := capItems(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q="+q, nil)); len(got) != 0 {
			t.Fatalf("q=%q finds %v", q, got)
		}
	}
	// another person's places, warnings and diary never appear
	for _, q := range []string{"sarajevo", "mostar", word, "snow", "e"} {
		if got := capItems(t, a.ok(a.bob, "GET", "/api/v1/pocket/search?q="+q, nil)); len(got) != 0 {
			t.Fatalf("bob finds alice's %q: %v", q, got)
		}
	}
	a.expect(401, "", "GET", "/api/v1/pocket/search?q=sarajevo", nil)
}

func TestPocketDay(t *testing.T) {
	a, wx, ids := capApp(t, nil)
	today, y := store.Today(), store.AddDays(store.Today(), -1)

	day := a.ok(a.alice, "GET", "/api/v1/pocket/day/"+today, nil)
	if day["date"] != today || !strings.HasPrefix(str(day, "summary"), "Sarajevo: ") || !strings.Contains(str(day, "summary"), "°C to ") || strings.Contains(str(day, "summary"), "forecast") {
		t.Fatalf("today: %v", day)
	}
	ms, _ := day["metrics"].([]any)
	if len(ms) < 3 || len(ms) > 4 || ms[0].(map[string]any)["label"] != "Low" || ms[1].(map[string]any)["label"] != "High" || ms[2].(map[string]any)["label"] != "Rain" {
		t.Fatalf("metrics: %v", ms)
	}
	// the warnings saved for a day, at any of the places
	w := activeWarning(t, a)
	wx.n.Store(0)
	wd := a.ok(a.alice, "GET", "/api/v1/pocket/day/"+str(w, "day"), nil)
	it := capItem(t, wd, "warning", str(w, "title"))
	if it["id"] != "warning:"+id(w) || it["url"] != "/warnings" || !strings.HasPrefix(str(it, "sub"), str(w, "place")) {
		t.Fatalf("warning: %v", it)
	}
	if strings.HasPrefix(str(w, "starts_at"), str(w, "day")) && it["time"] != clock(str(w, "starts_at")) {
		t.Fatalf("warning time: %v", it)
	}
	for _, x := range capItems(t, wd) {
		if x["kind"] != "warning" {
			t.Fatalf("only warnings that day: %v", x)
		}
	}
	// the day ahead says it is a forecast
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/"+store.AddDays(today, 2), nil); !strings.HasSuffix(str(d, "summary"), " (forecast)") {
		t.Fatalf("ahead: %v", d)
	}
	// the note of the day, from the diary
	a.ok(a.alice, "PUT", "/api/v1/diary/"+ids["home"]+"/"+y, map[string]any{"note": "Grey and cold\nFog until noon"})
	note := capItem(t, a.ok(a.alice, "GET", "/api/v1/pocket/day/"+y, nil), "diary", "Grey and cold")
	if note["note"] != "Grey and cold\nFog until noon" || note["url"] != "/diary" || note["id"] != "diary:"+ids["home"]+":"+y {
		t.Fatalf("diary note: %v", note)
	}
	noFetch(t, wx, "day")

	// a day long ago: nothing kept, nothing fetched; once looked up, it is there
	old := store.AddDays(today, -400)
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/"+old, nil); d["summary"] != nil || len(capItems(t, d)) != 0 {
		t.Fatalf("nothing kept: %v", d)
	}
	noFetch(t, wx, "an old day")
	a.ok(a.alice, "GET", "/api/v1/day/"+old, nil)
	wx.n.Store(0)
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/"+old, nil); !strings.HasPrefix(str(d, "summary"), "Sarajevo: ") {
		t.Fatalf("looked up before: %v", d)
	}
	// without a kept copy, the days ahead show nothing
	if _, err := a.st.DB.Exec(`DELETE FROM cache WHERE key LIKE 'fc:%'`); err != nil {
		t.Fatal(err)
	}
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/"+store.AddDays(today, 2), nil); d["summary"] != nil {
		t.Fatalf("no copy: %v", d)
	}
	noFetch(t, wx, "day")

	// somebody else's day
	if d := a.ok(a.bob, "GET", "/api/v1/pocket/day/"+today, nil); d["summary"] != nil || len(capItems(t, d)) != 0 {
		t.Fatalf("bob: %v", d)
	}
	a.expect(400, a.alice, "GET", "/api/v1/pocket/day/2026-13-01", nil)
	// the old route still answers as before
	if d := a.ok(a.alice, "GET", "/api/v1/day/"+y, nil); d["source"] != "diary" || d["note"] != "Grey and cold\nFog until noon" {
		t.Fatalf("/day changed: %v", d)
	}
}

func TestPocketWidget(t *testing.T) {
	a, wx, ids := capApp(t, nil)
	wg := a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil)
	line := str(wg, "line")
	// "12°, light rain · 8° to 14° today"
	if !strings.Contains(line, "°, ") || !strings.HasSuffix(line, "° today") || strings.Contains(line, "updated") || wg["url"] != "/" {
		t.Fatalf("line: %v", wg)
	}
	rows := capItems(t, wg)
	w := activeWarning(t, a)
	all, _ := a.ok(a.alice, "GET", "/api/v1/warnings", nil)["active"].([]any)
	wx.n.Store(0)
	// at most four warnings, the soonest; their number in the metrics
	if ms, _ := wg["metrics"].([]any); len(ms) != 1 || ms[0].(map[string]any)["label"] != "Warnings" || ms[0].(map[string]any)["value"] != itoa(int64(len(all))) {
		t.Fatalf("metrics: %v", wg["metrics"])
	}
	if len(rows) != min(len(all), widgetWarnings)+1 || rows[widgetWarnings-1]["url"] != "/warnings" {
		t.Fatalf("rows: %v", rows)
	}
	if rows[0]["kind"] != nil || rows[0]["title"] != w["title"] || rows[0]["url"] != "/warnings" || !strings.HasPrefix(str(rows[0], "detail"), str(w, "place")+" · ") {
		t.Fatalf("warnings first: %v", rows)
	}
	last := rows[len(rows)-1]
	if last["id"] != "place:"+ids["mostar"] || last["title"] != "Mostar" || last["url"] != "/places/"+ids["mostar"] || !strings.Contains(str(last, "detail"), "° today") {
		t.Fatalf("the other place last: %v", rows)
	}
	// an old copy says when it is from
	ageCopies(t, a, time.Now().Add(-3*time.Hour))
	if l := str(a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil), "line"); !strings.Contains(l, " · updated ") {
		t.Fatalf("old copy: %q", l)
	}
	noFetch(t, wx, "widget")

	// one place: the coming hours instead of other places
	a.expect(204, a.alice, "DELETE", "/api/v1/locations/"+ids["mostar"], nil)
	_, _ = a.st.DB.Exec(`DELETE FROM warnings`)
	one := capItems(t, a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil))
	if len(one) != widgetHours || !strings.HasPrefix(str(one[0], "id"), "hour:") || len(str(one[0], "title")) != 5 || !strings.Contains(str(one[0], "detail"), "°") {
		t.Fatalf("hours: %v", one)
	}
	noFetch(t, wx, "widget")

	// no place, no widget
	if b := a.ok(a.bob, "GET", "/api/v1/pocket/widget", nil); b["line"] != nil || len(capItems(t, b)) != 0 {
		t.Fatalf("bob: %v", b)
	}
}

func TestPocketCapture(t *testing.T) {
	a, wx, ids := capApp(t, nil)
	today, y := store.Today(), store.AddDays(store.Today(), -1)
	note := func(date string) string {
		var n string
		_ = a.st.DB.QueryRow(`SELECT note FROM days WHERE user_id = ? AND location_id = ? AND date = ?`, a.ids["Alice"], n64(ids["home"]), date).Scan(&n)
		return n
	}
	a.ok(a.alice, "PUT", "/api/v1/diary/"+ids["home"]+"/"+today, map[string]any{"note": "Cold morning"})
	body := map[string]any{"request_id": "hub-0001-aaaaaaaaaaaa", "text": "Sun came out at noon"}
	res := a.expect(201, a.alice, "POST", "/api/v1/pocket/capture", body)
	if res["id"] != "diary:"+ids["home"]+":"+today || res["title"] != "Sun came out at noon" || res["url"] != "/diary" {
		t.Fatalf("capture: %v", res)
	}
	if got := note(today); got != "Cold morning\nSun came out at noon" {
		t.Fatalf("appended: %q", got)
	}
	// the same request again: the same answer, nothing added
	if again := a.expect(201, a.alice, "POST", "/api/v1/pocket/capture", body); again["id"] != res["id"] || again["title"] != res["title"] {
		t.Fatalf("again: %v", again)
	}
	if got := note(today); got != "Cold morning\nSun came out at noon" {
		t.Fatalf("added twice: %q", got)
	}
	// the same id with other words
	a.expect(400, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "hub-0001-aaaaaaaaaaaa", "text": "Something else"})
	// a date of its own, the kind by name
	a.expect(201, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "hub-0002-aaaaaaaaaaaa", "kind": "diary", "text": "Windy", "date": y})
	if got := note(y); got != "Windy" {
		t.Fatalf("yesterday: %q", got)
	}
	// refused with a sentence: a day ahead, a day not in the diary, a full note, an unknown kind
	for _, c := range []struct {
		body map[string]any
		want string
	}{
		{map[string]any{"request_id": "hub-0003-aaaaaaaaaaaa", "text": "x", "date": store.AddDays(today, 1)}, "only has days that have begun"},
		{map[string]any{"request_id": "hub-0004-aaaaaaaaaaaa", "text": "x", "date": store.AddDays(today, -40)}, "is not in the weather diary for Sarajevo"},
		{map[string]any{"request_id": "hub-0005-aaaaaaaaaaaa", "text": strings.Repeat("x", 1990)}, "is full"},
		{map[string]any{"request_id": "hub-0006-aaaaaaaaaaaa", "kind": "note", "text": "x"}, "does not keep"},
	} {
		if m := a.expect(400, a.alice, "POST", "/api/v1/pocket/capture", c.body); !strings.Contains(str(m, "error"), c.want) {
			t.Fatalf("%v: %v", c.body, m)
		}
	}
	if got := note(today); got != "Cold morning\nSun came out at noon" {
		t.Fatalf("a refusal changed the note: %q", got)
	}
	// today before the check wrote it: written from the kept forecast
	_, _ = a.st.DB.Exec(`DELETE FROM days WHERE date = ?`, today)
	a.expect(201, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "hub-0007-aaaaaaaaaaaa", "text": "Fresh start"})
	if got := note(today); got != "Fresh start" {
		t.Fatalf("today written from the copy: %q", got)
	}
	noFetch(t, wx, "capture")

	// no place, or a home without warnings (no diary there)
	if m := a.expect(400, a.bob, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "bob-0001-aaaaaaaaaaaa", "text": "x"}); !strings.Contains(str(m, "error"), "add a place") {
		t.Fatalf("bob: %v", m)
	}
	a.ok(a.alice, "PUT", "/api/v1/locations/"+ids["home"], map[string]any{"warn": false})
	if m := a.expect(400, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "hub-0008-aaaaaaaaaaaa", "text": "x"}); !strings.Contains(str(m, "error"), "turn warnings on for Sarajevo") {
		t.Fatalf("no warnings: %v", m)
	}
	// Bob's request ids are his own; nothing of Alice's changed
	var nd int
	_ = a.st.DB.QueryRow(`SELECT COUNT(1) FROM days WHERE user_id = ?`, a.ids["Bob"]).Scan(&nd)
	if nd != 0 {
		t.Fatalf("bob has %d diary days", nd)
	}
}

func n64(s string) int64 { return n(s) }

// fakeCapCore is a Pocket Core that vouches for link tokens
// pcx_<alice|bob>_<search|day|widget|capture|read|write> from Pocket Hub
// and remembers the manifest Weather registers with.
type fakeCapCore struct {
	*httptest.Server
	mu       sync.Mutex
	manifest map[string]any
}

func newCapCore(t *testing.T) *fakeCapCore {
	t.Helper()
	fc := &fakeCapCore{}
	scopes := map[string]string{"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
		"read": "weather:read", "write": "weather:write"}
	fc.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apps/register":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			fc.mu.Lock()
			fc.manifest = m
			fc.mu.Unlock()
			io.WriteString(w, `{}`)
		case "/api/v1/auth/introspect":
			var in struct{ Token string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			parts := strings.Split(in.Token, "_")
			if len(parts) != 3 || parts[0] != "pcx" || scopes[parts[2]] == "" {
				io.WriteString(w, `{"active":false}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "audience": "weather", "scope": scopes[parts[2]],
				"user": map[string]any{"id": 1, "sub": parts[1] + "@example.com", "email": parts[1] + "@example.com", "name": parts[1], "role": "member"},
				"app":  map[string]any{"id": 9, "slug": "hub", "name": "Pocket Hub"}})
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(fc.Close)
	return fc
}

// Each pocket:* link opens its one route (and /auth/me), nothing else; the
// old weather:read and weather:write links do not open them.
func TestPocketLinkScopes(t *testing.T) {
	fc := newCapCore(t)
	a, _, _ := capApp(t, func(c *config.Config) { c.CoreURL, c.CoreRegistrationKey = fc.URL, "key" })
	capture := `{"request_id":"link-0001-aaaaaaaaaaaa","text":"Light frost on the car"}`
	routes := []struct{ method, path, body string }{
		{"GET", "/api/v1/pocket/search?q=sarajevo", ""},
		{"GET", "/api/v1/pocket/day/" + store.Today(), ""},
		{"GET", "/api/v1/pocket/widget", ""},
		{"POST", "/api/v1/pocket/capture", capture},
	}
	opens := map[string]int{"search": 0, "day": 1, "widget": 2, "capture": 3}
	for _, scope := range []string{"search", "day", "widget", "capture", "read", "write"} {
		tok := "pcx_alice_" + scope
		for i, rt := range routes {
			want := 403
			if o, ok := opens[scope]; ok && o == i {
				want = 200
				if rt.method == "POST" {
					want = 201
				}
			}
			if code, body := a.raw(tok, rt.method, rt.path, "application/json", []byte(rt.body)); code != want {
				t.Errorf("%s %s %s → %d, want %d (%s)", tok, rt.method, rt.path, code, want, body)
			}
		}
		if code, _ := a.raw(tok, "GET", "/api/v1/auth/me", "", nil); code != 200 {
			t.Errorf("%s /auth/me → %d", tok, code)
		}
		if _, ok := opens[scope]; ok {
			for _, p := range []string{"/api/v1/forecast", "/api/v1/overview", "/api/v1/plans", "/api/v1/settings", "/api/v1/export.json"} {
				if code, _ := a.raw(tok, "GET", p, "", nil); code != 403 {
					t.Errorf("%s GET %s → %d, want 403", tok, p, code)
				}
			}
			if code, _ := a.raw(tok, "POST", "/api/v1/locations", "application/json", []byte(`{"name":"X","lat":1,"lon":1}`)); code != 403 {
				t.Errorf("%s POST /locations → %d, want 403", tok, code)
			}
		}
	}
	// the right methods only
	if code, _ := a.raw("pcx_alice_capture", "GET", "/api/v1/pocket/capture", "", nil); code != 403 && code != 405 {
		t.Errorf("GET capture → %d", code)
	}
	if code, _ := a.raw("pcx_alice_search", "POST", "/api/v1/pocket/search", "application/json", []byte(`{}`)); code != 403 && code != 405 {
		t.Errorf("POST search → %d", code)
	}
	// a link acts for its person: Bob's search finds nothing of Alice's
	if code, body := a.raw("pcx_bob_search", "GET", "/api/v1/pocket/search?q=sarajevo", "", nil); code != 200 || strings.Contains(string(body), "Sarajevo") {
		t.Errorf("bob's link: %d %s", code, body)
	}

	// the manifest: category, capabilities and the pocket:* offers
	var m map[string]any
	for i := 0; i < 100; i++ {
		fc.mu.Lock()
		m = fc.manifest
		fc.mu.Unlock()
		if m != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if m == nil {
		t.Fatal("Weather did not register")
	}
	if m["category"] != "Places" {
		t.Errorf("category: %v", m["category"])
	}
	// the capabilities sit at the manifest's top level
	search, _ := m["search"].(map[string]any)
	day, _ := m["day"].(map[string]any)
	capt, _ := m["capture"].(map[string]any)
	if m["widget"] == nil || day["one"] != "note" || day["many"] != "notes" || capt == nil {
		t.Errorf("capabilities: %v", m)
	}
	if b, _ := json.Marshal(search["bangs"]); string(b) != `["weather","forecast"]` {
		t.Errorf("bangs: %s", b)
	}
	if b, _ := json.Marshal(capt["kinds"]); !strings.Contains(string(b), `"kind":"diary"`) || !strings.Contains(string(b), `"fields":["date"]`) {
		t.Errorf("capture kinds: %s", b)
	}
	offered := map[string]bool{}
	for _, o := range m["offers"].([]any) {
		offered[str(o.(map[string]any), "scope")] = true
	}
	for _, sc := range []string{"weather:read", "weather:write", core.ScopeSearch, core.ScopeDay, core.ScopeWidget, core.ScopeCapture} {
		if !offered[sc] {
			t.Errorf("offer %s missing: %v", sc, offered)
		}
	}
}

func TestPocketLinkAllows(t *testing.T) {
	req := func(m, p string) *http.Request { return httptest.NewRequest(m, p, nil) }
	if linkAllows([]string{"weather:read"}, req("GET", "/api/v1/pocket/day/2026-11-05")) || linkAllows([]string{"weather:read", "weather:write"}, req("POST", "/api/v1/pocket/capture")) {
		t.Error("the old scopes must not open the pocket routes")
	}
	if !linkAllows([]string{core.ScopeDay}, req("GET", "/api/v1/pocket/day/2026-11-05")) || linkAllows([]string{core.ScopeDay}, req("GET", "/api/v1/day/2026-11-05")) {
		t.Error("pocket:day opens its route only")
	}
}
