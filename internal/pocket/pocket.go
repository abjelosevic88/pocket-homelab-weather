// Package pocket talks to sibling Pocket apps: Pocket Travel (trips),
// Pocket Calendar (events), Pocket Home (outdoor plants), Pocket Garage
// (vehicles and tyre changes), Pocket Pets (dogs to walk), Pocket Movement
// (where you are now) and Pocket Tasks (advice made into a task). Same
// bearer-token JSON API everywhere.
package pocket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a bearer-token JSON client for one Pocket app.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New creates a client.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

// Configured reports whether URL and token are set.
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" && c.Token != "" }

// ErrNotFound is a 404 from the sibling app.
var ErrNotFound = errors.New("not found")

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any, v any) error {
	if !c.Configured() {
		return errors.New("not configured")
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return errors.New("token rejected (401)")
	}
	if res.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2000))
		_ = json.Unmarshal(b, &e)
		if e.Error != "" {
			return fmt.Errorf("HTTP %d: %s", res.StatusCode, e.Error)
		}
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if v == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(v)
}

// Health is the connection test for any Pocket app: /healthz plus a token check.
func (c *Client) Health(ctx context.Context) (string, error) {
	var out struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if !c.Configured() {
		return "", errors.New("not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
	if err != nil {
		return "", err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	var me map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/auth/me", nil, nil, &me); err != nil {
		return out.Version, err
	}
	return out.Version, nil
}

// Do is a raw JSON call (for a connection test).
func (c *Client) Do(ctx context.Context, method, path string, q url.Values, body, v any) error {
	return c.do(ctx, method, path, q, body, v)
}

// list decodes a list that may be bare or wrapped in {key:[…]} / {results:[…]}.
func list[T any](raw json.RawMessage, keys ...string) ([]T, error) {
	var out []T
	if err := json.Unmarshal(raw, &out); err == nil {
		return out, nil
	}
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	for _, k := range append(keys, "results", "items") {
		if b, ok := wrap[k]; ok {
			if err := json.Unmarshal(b, &out); err != nil {
				return nil, err
			}
			return out, nil
		}
	}
	return []T{}, nil
}

// raw downloads a body as bytes (an attachment), at most limit bytes.
func (c *Client) raw(ctx context.Context, path string, q url.Values, limit int64) ([]byte, string, error) {
	if !c.Configured() {
		return nil, "", errors.New("not configured")
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return nil, "", errors.New("token rejected (401)")
	case res.StatusCode == http.StatusNotFound:
		return nil, "", ErrNotFound
	case res.StatusCode >= 300:
		return nil, "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > limit {
		return nil, "", errors.New("too large")
	}
	return b, res.Header.Get("Content-Type"), nil
}

// ---------------- Pocket Travel ----------------

// Trip is a trip in Pocket Travel.
type Trip struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Destination string   `json:"destination"`
	StartDate   string   `json:"start_date"`
	EndDate     string   `json:"end_date"`
	Status      string   `json:"status"`
	Archived    bool     `json:"archived"`
	Lat         *float64 `json:"lat"`
	Lng         *float64 `json:"lng"`
	Timezone    string   `json:"timezone"`
}

// Trips lists the person's trips.
func (c *Client) Trips(ctx context.Context) ([]Trip, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/trips", nil, nil, &raw); err != nil {
		return nil, err
	}
	return list[Trip](raw, "trips")
}

// ---------------- Pocket Calendar ----------------

// Event is one occurrence of an event on the person's calendars.
type Event struct {
	ID       string `json:"id"` // "<event_id>" or "<event_id>:<key>"
	EventID  int64  `json:"event_id"`
	Title    string `json:"title"`
	Location string `json:"location"`
	Start    string `json:"start"` // RFC 3339, or YYYY-MM-DD all day
	End      string `json:"end"`
	AllDay   bool   `json:"all_day"`
	Status   string `json:"status"`
	Masked   bool   `json:"masked"`
}

// Events lists the occurrences between two days (to is exclusive).
func (c *Client) Events(ctx context.Context, from, to string) ([]Event, error) {
	q := url.Values{}
	q.Set("from", from)
	q.Set("to", to)
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/events", q, nil, &raw); err != nil {
		return nil, err
	}
	return list[Event](raw, "events")
}

// ---------------- Pocket Home ----------------

// Plant is a house plant in Pocket Home; Room says where it stands.
type Plant struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Species  string `json:"species"`
	Kind     string `json:"kind"`
	Room     string `json:"room"`
	HomeName string `json:"home_name"`
	Archived bool   `json:"archived"`
}

// Plants lists the person's plants.
func (c *Client) Plants(ctx context.Context) ([]Plant, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/plants", nil, nil, &raw); err != nil {
		return nil, err
	}
	return list[Plant](raw, "plants")
}

// ---------------- Pocket Garage ----------------

// Vehicle is a vehicle in Pocket Garage.
type Vehicle struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Make     string `json:"make"`
	Model    string `json:"model"`
	Kind     string `json:"kind"`
	SoldDate string `json:"sold_date"`
	Status   string `json:"status"`
	Archived bool   `json:"archived"`
}

// Vehicles lists the person's vehicles.
func (c *Client) Vehicles(ctx context.Context) ([]Vehicle, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/vehicles", nil, nil, &raw); err != nil {
		return nil, err
	}
	return list[Vehicle](raw, "vehicles")
}

// Record is a service record of a vehicle ("tyres" when they were changed).
type Record struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	Category    string `json:"category"`
	Date        string `json:"date"`
	Description string `json:"description"`
}

// Records lists a vehicle's service records.
func (c *Client) Records(ctx context.Context, vehicle int64) ([]Record, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/vehicles/%d/records", vehicle), nil, nil, &raw); err != nil {
		return nil, err
	}
	return list[Record](raw, "records")
}

// ---------------- Pocket Pets ----------------

// Pet is a pet in Pocket Pets.
type Pet struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Species  string `json:"species"`
	Breed    string `json:"breed"`
	Archived bool   `json:"archived"`
	PassedOn string `json:"passed_on"`
}

// Pets lists the person's pets.
func (c *Client) Pets(ctx context.Context) ([]Pet, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/pets", nil, nil, &raw); err != nil {
		return nil, err
	}
	return list[Pet](raw, "pets")
}

// ---------------- Pocket Movement ----------------

// Position is where the person was last seen.
type Position struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	TS      int64   `json:"ts"` // unix seconds
	Country string  `json:"country"`
	Place   string  `json:"-"` // the visit's place name, when there is one
}

// Now is the person's latest position (nil when Movement has none).
func (c *Client) Now(ctx context.Context) (*Position, error) {
	var out struct {
		Point *Position `json:"point"`
		Visit *struct {
			Name    string `json:"name"`
			OwnName string `json:"own_name"`
			Place   *struct {
				Name string `json:"name"`
			} `json:"place"`
		} `json:"visit"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/now", nil, nil, &out); err != nil {
		return nil, err
	}
	if out.Point == nil {
		return nil, nil
	}
	if v := out.Visit; v != nil {
		out.Point.Place = v.OwnName
		if out.Point.Place == "" && v.Place != nil {
			out.Point.Place = v.Place.Name
		}
		if out.Point.Place == "" {
			out.Point.Place = v.Name
		}
	}
	return out.Point, nil
}

// ---------------- Pocket Tasks ----------------

// Task is a task to add.
type Task struct {
	Title string   `json:"title"`
	Notes string   `json:"notes,omitempty"`
	Due   string   `json:"due,omitempty"` // YYYY-MM-DD
	URL   string   `json:"url,omitempty"`
	Tags  []string `json:"tags,omitempty"`
}

// CreateTask adds a task and returns its id.
func (c *Client) CreateTask(ctx context.Context, t Task) (int64, error) {
	var out struct {
		ID any `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/tasks", nil, t, &out); err != nil {
		return 0, err
	}
	var id int64
	_, _ = fmt.Sscan(strings.SplitN(fmt.Sprint(out.ID), ":", 2)[0], &id)
	return id, nil
}
