package server

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
)

// ---- places ----

type locationIn struct {
	Name        *string  `json:"name"`
	Region      *string  `json:"region"`
	Country     *string  `json:"country"`
	CountryCode *string  `json:"country_code"`
	Lat         *float64 `json:"lat"`
	Lon         *float64 `json:"lon"`
	Elevation   *float64 `json:"elevation"`
	Timezone    *string  `json:"timezone"`
	IsHome      *bool    `json:"is_home"`
	Warn        *bool    `json:"warn"`
	Note        *string  `json:"note"`
	SourceRef   *string  `json:"source_ref"`
}

func (in locationIn) apply(l *store.Location) string {
	trimP(&l.Name, in.Name)
	trimP(&l.Region, in.Region)
	trimP(&l.Country, in.Country)
	if in.CountryCode != nil {
		l.CountryCode = strings.ToUpper(strings.TrimSpace(*in.CountryCode))
	}
	if in.Lat != nil {
		l.Lat = *in.Lat
	}
	if in.Lon != nil {
		l.Lon = *in.Lon
	}
	if in.Elevation != nil {
		l.Elevation = in.Elevation
	}
	trimP(&l.Timezone, in.Timezone)
	if in.IsHome != nil {
		l.IsHome = *in.IsHome
	}
	if in.Warn != nil {
		l.Warn = *in.Warn
	}
	trimP(&l.Note, in.Note)
	switch {
	case l.Name == "":
		return "a place needs a name"
	case len(l.Name) > 80:
		return "the name is at most 80 characters"
	case math.IsNaN(l.Lat) || l.Lat < -90 || l.Lat > 90 || math.IsNaN(l.Lon) || l.Lon < -180 || l.Lon > 180:
		return "latitude is −90 to 90 and longitude −180 to 180"
	case l.Lat == 0 && l.Lon == 0:
		return "pick the place on the search first"
	}
	return ""
}

func (s *Server) handleListLocations(w http.ResponseWriter, r *http.Request) {
	ls, err := s.store.ListLocations(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "places")
		return
	}
	writeJSON(w, http.StatusOK, ls)
}

func (s *Server) handleGetLocation(w http.ResponseWriter, r *http.Request) {
	l, err := s.store.GetLocation(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "place")
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleCreateLocation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in locationIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	l := &store.Location{UserID: uid, Warn: true}
	if msg := in.apply(l); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if app := linkApp(ctx); app != "" {
		l.Source = app
		if in.SourceRef != nil {
			l.SourceRef = strings.TrimSpace(*in.SourceRef)
		}
		if l.SourceRef != "" {
			if old, err := s.store.LocationBySource(ctx, uid, app, l.SourceRef); err == nil {
				writeJSON(w, http.StatusOK, old)
				return
			}
		}
	}
	if l.Timezone == "" {
		if f, _, err := s.forecastAt(ctx, l.Lat, l.Lon); err == nil {
			l.Timezone = f.Timezone
			if l.Elevation == nil {
				e := f.Elevation
				l.Elevation = &e
			}
		}
	}
	if err := s.store.CreateLocation(ctx, l); err != nil {
		s.fail(w, err, "add place")
		return
	}
	s.afterPlaces(r, uid)
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) handleUpdateLocation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	l, err := s.store.GetLocation(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "place")
		return
	}
	if app := linkApp(ctx); app != "" && l.Source != app {
		writeErr(w, http.StatusForbidden, "this place was not added by "+app)
		return
	}
	var in locationIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := in.apply(l); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if l.Source == "here" {
		l.IsHome = false
	}
	if err := s.store.UpdateLocation(ctx, l); err != nil {
		s.fail(w, err, "save place")
		return
	}
	l, _ = s.store.GetLocation(ctx, uid, l.ID)
	s.afterPlaces(r, uid)
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleDeleteLocation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	l, err := s.store.GetLocation(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "place")
		return
	}
	if app := linkApp(ctx); app != "" && l.Source != app {
		writeErr(w, http.StatusForbidden, "this place was not added by "+app)
		return
	}
	if err := s.store.DeleteLocation(ctx, uid, l.ID); err != nil {
		s.fail(w, err, "remove place")
		return
	}
	s.afterPlaces(r, uid)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOrderLocations(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.ReorderLocations(r.Context(), actorID(r.Context()), in.IDs); err != nil {
		s.fail(w, err, "order")
		return
	}
	s.handleListLocations(w, r)
}

// afterPlaces makes the next page view check warnings and plans again.
func (s *Server) afterPlaces(r *http.Request, uid int64) {
	_ = s.store.CacheDelete(r.Context(), "checked:"+strconv.FormatInt(uid, 10))
	s.forgetPlans(r.Context(), uid)
}

// handleSearch finds places by name for "Add a place".
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < 2 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	ps, err := s.wx.Search(ctx, q, firstNonEmpty(r.URL.Query().Get("lang"), st.Language), queryInt(r, "count"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

// handleReverse names a spot ("Use my location").
func (s *Server) handleReverse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lat, err1 := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lon, err2 := strconv.ParseFloat(r.URL.Query().Get("lon"), 64)
	if err1 != nil || err2 != nil {
		writeErr(w, http.StatusBadRequest, "lat and lon are numbers")
		return
	}
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	p, err := s.placeName(ctx, lat, lon, st.Language)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if f, _, err := s.forecastAt(ctx, lat, lon); err == nil {
		p.Timezone, p.Elevation = f.Timezone, f.Elevation
	}
	writeJSON(w, http.StatusOK, p)
}
