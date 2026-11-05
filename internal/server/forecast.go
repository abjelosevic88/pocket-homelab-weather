package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/weather"
)

// ---- the forecast, cached ----
//
// Forecasts are public data about a spot on the map, so they are cached by
// rounded coordinates (about a kilometre) and shared by everyone who follows
// a place there: 20 minutes for the forecast, an hour for air quality, a
// month for place names. When Open-Meteo does not answer, the last copy is
// served (stale) rather than nothing.

const (
	forecastTTL = 20 * time.Minute
	airTTL      = time.Hour
	nameTTL     = 30 * 24 * time.Hour
)

// wxLocks serialises fetches of the same spot so a page full of places
// asking at once fetches each one only once.
type wxLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (l *wxLocks) lock(key string) func() {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = map[string]*sync.Mutex{}
	}
	m, ok := l.locks[key]
	if !ok {
		m = &sync.Mutex{}
		l.locks[key] = m
	}
	l.mu.Unlock()
	m.Lock()
	return m.Unlock
}

func spot(lat, lon float64) string {
	return fmt.Sprintf("%.2f:%.2f", math.Round(lat*100)/100, math.Round(lon*100)/100)
}

// cached returns a cached value younger than ttl, else fetches a new one;
// a fetch that fails falls back to any old copy (stale = true).
func cached[T any](ctx context.Context, s *Server, key string, ttl time.Duration, fetch func() (T, error)) (T, bool, error) {
	var zero T
	unlock := s.wxLocks.lock(key)
	defer unlock()
	raw, at, ok := s.store.CacheGet(ctx, key)
	if ok {
		if t, err := time.Parse(time.RFC3339Nano, at); err == nil && time.Since(t) < ttl {
			var v T
			if json.Unmarshal([]byte(raw), &v) == nil {
				return v, false, nil
			}
		}
	}
	v, err := fetch()
	if err != nil {
		if ok {
			var old T
			if json.Unmarshal([]byte(raw), &old) == nil {
				s.log.Debug("weather: serving stale copy", "key", key, "err", err)
				return old, true, nil
			}
		}
		return zero, false, err
	}
	if b, err := json.Marshal(v); err == nil {
		_ = s.store.CachePut(ctx, key, string(b))
	}
	return v, false, nil
}

// forecastAt is the forecast for a spot.
func (s *Server) forecastAt(ctx context.Context, lat, lon float64) (*weather.Forecast, bool, error) {
	return cached(ctx, s, "fc:"+spot(lat, lon), forecastTTL, func() (*weather.Forecast, error) { return s.wx.Forecast(ctx, lat, lon) })
}

// airAt is air quality and pollen for a spot.
func (s *Server) airAt(ctx context.Context, lat, lon float64) (*weather.Air, bool, error) {
	return cached(ctx, s, "aq:"+spot(lat, lon), airTTL, func() (*weather.Air, error) { return s.wx.Air(ctx, lat, lon) })
}

// forecastOf is the forecast for one of the person's places.
func (s *Server) forecastOf(ctx context.Context, l *store.Location) (*weather.Forecast, bool, error) {
	return s.forecastAt(ctx, l.Lat, l.Lon)
}

// pastDays returns days before today at a spot: the forecast API keeps about
// three months, the archive anything older (a month's cache: the past does
// not change).
func (s *Server) pastDays(ctx context.Context, lat, lon float64, from, to string) ([]weather.Day, error) {
	days, _, err := cached(ctx, s, "past:"+spot(lat, lon)+":"+from+":"+to, nameTTL, func() ([]weather.Day, error) {
		if from >= store.AddDays(store.Today(), -85) {
			return s.wx.PastDays(ctx, lat, lon, from, to)
		}
		return s.wx.Archive(ctx, lat, lon, from, to)
	})
	return days, err
}

// placeName names a spot on the map ("Vienna"), cached for a month.
func (s *Server) placeName(ctx context.Context, lat, lon float64, lang string) (*weather.Place, error) {
	key := fmt.Sprintf("rev:%.2f:%.2f:%s", math.Round(lat*20)/20, math.Round(lon*20)/20, lang)
	p, _, err := cached(ctx, s, key, nameTTL, func() (*weather.Place, error) { return s.wx.Reverse(ctx, lat, lon, lang) })
	return p, err
}

// geocode finds the best match for a free-text place ("Vienna, Austria",
// an event's location), cached for a month; nil when nothing matches.
func (s *Server) geocode(ctx context.Context, text, lang string) *weather.Place {
	text = cleanPlaceText(text)
	if text == "" {
		return nil
	}
	ps, _, err := cached(ctx, s, "geo1:"+lang+":"+text, nameTTL, func() ([]weather.Place, error) { return s.wx.Search(ctx, text, lang, 1) })
	if err != nil || len(ps) == 0 {
		return nil
	}
	return &ps[0]
}
