package server

import (
	"context"
	"math/rand"

	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/weather"
)

// seedDemo gives the owner three places — home in Sarajevo, the cottage on
// Jahorina, the parents in Mostar — and fourteen months of diary at home,
// so last year's month can be compared (DEMO_DATA, for `make run`, the CI
// smoke test and the screenshots). The weather itself is the demo source's
// (weather.Demo), made up around today: a frosty morning, a rainy
// afternoon, a windy day and smoggy evenings.
func (s *Server) seedDemo(ctx context.Context, uid int64) error {
	// with AUTH_MODE=none this runs inside the first request; finish even
	// when that client goes away
	ctx = context.WithoutCancel(ctx)
	if ls, _ := s.store.ListLocations(ctx, uid); len(ls) > 0 {
		return nil
	}
	rng := rand.New(rand.NewSource(42))
	elev := func(v float64) *float64 { return &v }
	places := []store.Location{
		{Name: "Sarajevo", Region: "Federation of Bosnia and Herzegovina", Country: "Bosnia and Herzegovina", CountryCode: "BA", Lat: 43.8486, Lon: 18.3564, Elevation: elev(518), Timezone: "Europe/Sarajevo", IsHome: true, Warn: true},
		{Name: "Jahorina", Region: "Republika Srpska", Country: "Bosnia and Herzegovina", CountryCode: "BA", Lat: 43.7383, Lon: 18.5675, Elevation: elev(1557), Timezone: "Europe/Sarajevo", Warn: true, Note: "The cottage"},
		{Name: "Mostar", Region: "Federation of Bosnia and Herzegovina", Country: "Bosnia and Herzegovina", CountryCode: "BA", Lat: 43.3433, Lon: 17.8081, Elevation: elev(64), Timezone: "Europe/Sarajevo", Warn: true, Note: "Mum and dad"},
	}
	for i := range places {
		places[i].UserID = uid
		if err := s.store.CreateLocation(ctx, &places[i]); err != nil {
			return err
		}
	}
	home := places[0]
	today := store.Today()
	days, err := weather.Demo{}.Archive(ctx, home.Lat, home.Lon, store.AddDays(today, -425), store.AddDays(today, -2))
	if err != nil {
		return err
	}
	notes := []string{"First snow on Trebević", "Fog in the valley all morning", "Perfect day for a walk on the Vrelo Bosne", "Smog: stayed in", "Thunderstorm in the evening, power went out for an hour"}
	for i, d := range days {
		rec := recordOf(home.ID, d)
		aqi := 25 + rng.Float64()*40
		if m := d.Date[5:7]; m == "11" || m == "12" || m == "01" || m == "02" {
			aqi += 30
		}
		rec.AQI = &aqi
		if err := s.store.SaveDay(ctx, uid, rec); err != nil {
			return err
		}
		if i%67 == 5 {
			_ = s.store.SetDayNote(ctx, uid, home.ID, d.Date, notes[(i/67)%len(notes)])
		}
	}
	s.checkUser(ctx, uid, true)
	return nil
}
