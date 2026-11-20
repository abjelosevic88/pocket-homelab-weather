package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/config"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
)

// moveLinks starts Weather behind a fake Core whose pcx_<who>_<export|import|read>
// tokens are Pocket Move links acting for alice@ or bob@ (plain members to Core).
func moveLinks(t *testing.T) *app {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "weather:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"weather","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
}

func (a *app) raw(tok, method, path, ctype string, body []byte) (int, []byte) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func TestMoveLinkScopes(t *testing.T) {
	a := moveLinks(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "GET", "/api/v1/export.csv", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/plans", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{exp, "GET", "/api/v1/users", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{imp, "POST", "/api/v1/locations", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
		{read, "POST", "/api/v1/import", 403},
	} {
		if code, body := a.raw(c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := a.raw(exp, "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 2 || len(p.Imports) != 1 || p.Exports[0].Kind != core.KindPocket {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's places and diary to Bob: the import adds once, keeps
// Bob's home and his own diary notes, and nothing crosses between them.
func TestMoveImportsAddOnce(t *testing.T) {
	a := moveLinks(t)
	a.ok(a.alice, "POST", "/api/v1/locations", sarajevo())
	a.ok(a.alice, "GET", "/api/v1/overview", nil) // fills the diary
	lid := id(a.list(a.alice, "/api/v1/locations")[0].(map[string]any))
	today := store.Today()
	a.ok(a.alice, "PUT", "/api/v1/diary/"+lid+"/"+today, map[string]any{"note": "Sunny walk"})
	a.ok(a.bob, "POST", "/api/v1/locations", map[string]any{"name": "Mostar", "lat": 43.3433, "lon": 17.8081})

	code, export := a.raw("pcx_alice_export", "GET", "/api/v1/export.json", "", nil)
	if code != 200 || !strings.Contains(string(export), "Sarajevo") || !strings.Contains(string(export), "Sunny walk") {
		t.Fatalf("alice's export: %d %s", code, export)
	}
	if _, csv := a.raw("pcx_alice_export", "GET", "/api/v1/export.csv", "", nil); !strings.Contains(string(csv), "Sunny walk") {
		t.Fatalf("alice's csv: %s", csv)
	}
	if _, bob := a.raw("pcx_bob_export", "GET", "/api/v1/export.json", "", nil); strings.Contains(string(bob), "Sarajevo") {
		t.Fatalf("bob's export holds alice's places: %s", bob)
	}

	imp := func() map[string]any {
		code, out := a.raw("pcx_bob_import", "POST", "/api/v1/import", "application/json", export)
		var m map[string]any
		if code != 200 || json.Unmarshal(out, &m) != nil {
			t.Fatalf("import: %d %s", code, out)
		}
		return m
	}
	first := imp()
	if num(first, "places") != 1 || num(first, "days") < 1 {
		t.Fatalf("first import: %v", first)
	}
	locs := a.list(a.bob, "/api/v1/locations")
	if len(locs) != 2 || locs[0].(map[string]any)["name"] != "Mostar" || locs[0].(map[string]any)["is_home"] != true {
		t.Fatalf("bob's places after import (Mostar stays home): %v", locs)
	}
	var bobSarajevo int64
	for _, l := range locs {
		if m := l.(map[string]any); m["name"] == "Sarajevo" {
			bobSarajevo = int64(num(m, "id"))
		}
	}
	a.ok(a.bob, "PUT", fmt.Sprintf("/api/v1/diary/%d/%s", bobSarajevo, today), map[string]any{"note": "Mine"})

	second := imp()
	if num(second, "places") != 0 || num(second, "already") != 1 || num(second, "days") != 0 {
		t.Fatalf("second import added: %v", second)
	}
	if d, err := a.st.GetDay(t.Context(), a.ids["Bob"], bobSarajevo, today); err != nil || d.Note != "Mine" {
		t.Fatalf("bob's own note was overwritten: %+v %v", d, err)
	}
	if l := a.list(a.bob, "/api/v1/locations"); len(l) != 2 {
		t.Fatalf("bob's places: %v", l)
	}
	if l := a.list(a.alice, "/api/v1/locations"); len(l) != 1 {
		t.Fatalf("alice's places changed: %v", l)
	}
}
