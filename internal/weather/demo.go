package weather

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"
)

// Source is where the weather comes from: Open-Meteo, or the made-up demo
// weather (DEMO_DATA) so `make run`, the CI smoke test and the screenshots
// work without the network and always show something to warn about.
type Source interface {
	Forecast(ctx context.Context, lat, lon float64) (*Forecast, error)
	Air(ctx context.Context, lat, lon float64) (*Air, error)
	Archive(ctx context.Context, lat, lon float64, from, to string) ([]Day, error)
	PastDays(ctx context.Context, lat, lon float64, from, to string) ([]Day, error)
	Search(ctx context.Context, name, lang string, count int) ([]Place, error)
	Reverse(ctx context.Context, lat, lon float64, lang string) (*Place, error)
}

// Demo is made-up but believable weather: the season's temperatures for the
// latitude, and around today a frosty morning, a rainy afternoon, a windy
// day and smoggy winter evenings. Same place and date, same weather.
type Demo struct{}

// demoPlan is the shape of the days around today: offsets from the season's
// mean, the weather code, rain (mm) and the strongest gust (km/h).
var demoPlan = []struct {
	lo, hi float64
	code   int
	rainMM float64
	wetAt  [2]int // hours of the day it falls
	gust   float64
}{
	{-2, -1, 3, 2, [2]int{5, 9}, 28},     // yesterday
	{-4, 1, 2, 0, [2]int{}, 22},          // today
	{-8, -1, 0, 0, [2]int{}, 15},         // tomorrow: frost in the morning
	{-1, -3, 63, 27, [2]int{13, 23}, 45}, // rain all afternoon
	{1, 2, 80, 6, [2]int{9, 17}, 74},     // windy with showers
	{-2, -2, 3, 0, [2]int{}, 30},
	{-6, -6, 71, 4, [2]int{2, 12}, 35}, // a little snow (or rain when it is warm)
	{-5, -3, 1, 0, [2]int{}, 18},
	{-3, 0, 2, 0, [2]int{}, 20},
	{-1, 1, 61, 5, [2]int{6, 14}, 32},
	{0, 2, 3, 1, [2]int{16, 19}, 26},
}

func seasonMean(lat float64, day time.Time) float64 {
	doy := float64(day.YearDay())
	amp := 11.0
	shift := 15.0
	if lat < 0 {
		shift += 182
	}
	base := 10.0 + (45-math.Abs(lat))*0.45
	return base - amp*math.Cos(2*math.Pi*(doy-shift)/365)
}

// spotShift makes places a little different from each other: −3 to +3 °C.
func spotShift(lat, lon float64) float64 {
	return 3 - float64(int(math.Abs(lat*100+lon*37))%7)
}

func round1(v float64) *float64 {
	r := math.Round(v*10) / 10
	return &r
}

// Forecast implements Source.
func (Demo) Forecast(_ context.Context, lat, lon float64) (*Forecast, error) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	f := &Forecast{Lat: lat, Lon: lon, Elevation: 518, Timezone: time.Local.String(), TZAbbr: now.Format("MST"), Fetched: now.UTC().Format(time.RFC3339)}
	if f.Timezone == "Local" {
		f.Timezone = ""
	}
	rng := rand.New(rand.NewSource(int64(lat*100)*7919 + int64(lon*100) + int64(today.YearDay())))
	shift := spotShift(lat, lon)
	for i, p := range demoPlan {
		d := today.AddDate(0, 0, i-1)
		mean := seasonMean(lat, d) + shift
		lo, hi := mean-4+p.lo, mean+4+p.hi
		code, rain, snow := p.code, p.rainMM, 0.0
		if Wet(code) && hi < 2 {
			code, snow, rain = 73, math.Round(p.rainMM*1.2), 0
		} else if code >= 71 && code <= 77 && lo > 1 {
			code = 61
		}
		date := d.Format("2006-01-02")
		rise := time.Date(d.Year(), d.Month(), d.Day(), 6, 50-int(math.Round((mean-5)*1.5)), 0, 0, time.Local)
		set := time.Date(d.Year(), d.Month(), d.Day(), 16, 30+int(math.Round((mean-5)*4)), 0, 0, time.Local)
		dayH := set.Sub(rise).Hours()
		sun := dayH * 0.75
		if code == 3 {
			sun = dayH * 0.15
		} else if Wet(code) {
			sun = dayH * 0.1
		} else if code == 2 {
			sun = dayH * 0.45
		}
		uv := math.Max(0.5, (mean+4)/3.5)
		if Wet(code) || code == 3 {
			uv *= 0.5
		}
		prob := 5.0
		if rain+snow > 0 {
			prob = math.Min(95, 45+rain*2+snow*4)
		}
		precip := rain + snow*0.7
		f.Days = append(f.Days, Day{Date: date, Code: code, Condition: Condition(code), Text: Describe(code, true),
			TMax: round1(hi), TMin: round1(lo), FeelsMax: round1(hi - 1.5), FeelsMin: round1(lo - 3),
			Precip: round1(precip), Rain: round1(rain), Snow: round1(snow), PrecipH: round1(float64(p.wetAt[1] - p.wetAt[0])), PrecipProb: round1(prob),
			Wind: round1(p.gust * 0.55), Gust: round1(p.gust), WindDir: round1(float64(200 + rng.Intn(120))), UV: round1(uv),
			SunshineH: round1(sun), DaylightH: round1(dayH), Sunrise: rise.Format("2006-01-02T15:04"), Sunset: set.Format("2006-01-02T15:04")})
		wetHours := float64(max(1, p.wetAt[1]-p.wetAt[0]))
		for h := 0; h < 24; h++ {
			t := d.Add(time.Duration(h) * time.Hour)
			// coldest at 6, warmest at 14
			x := math.Cos(2 * math.Pi * float64(h-14) / 24)
			temp := lo + (hi-lo)*(x+1)/2
			hc := code
			wet := precip > 0 && h >= p.wetAt[0] && h < p.wetAt[1]
			var hp, hs float64
			pp := 5.0
			if Wet(code) {
				if wet {
					hp = precip / wetHours
					hs = snow / wetHours
					pp = prob - float64(rng.Intn(10))
				} else {
					hc, pp = 3, 15
				}
			} else if code <= 1 && (h < 7 || h > 17) {
				hc = 0
			}
			day := !t.Before(rise) && t.Before(set)
			hu := 0.0
			if day {
				hu = math.Max(0, uv*math.Sin(math.Pi*float64(h-7)/10))
			}
			gust := p.gust * (0.55 + 0.45*(x+1)/2)
			f.Hours = append(f.Hours, Hour{Time: t.Format("2006-01-02T15:04"), Temp: round1(temp), Feels: round1(temp - 1.5 - gust/40), Humidity: round1(60 + 30*(1-x)/2),
				PrecipProb: round1(pp), Precip: round1(hp), Snow: round1(hs), Code: hc, Wind: round1(gust * 0.55), WindDir: round1(float64(210 + rng.Intn(90))), Gust: round1(gust),
				UV: round1(hu), IsDay: day, Condition: Condition(hc)})
		}
	}
	cur := f.HourAt(now.Format("2006-01-02T15:04"))
	if cur == nil {
		return nil, fmt.Errorf("demo: no hour for now")
	}
	f.Now = Now{Time: now.Format("2006-01-02T15:04"), Temp: cur.Temp, Feels: cur.Feels, Humidity: cur.Humidity, Precip: cur.Precip, Code: cur.Code,
		Cloud: round1(map[bool]float64{true: 85, false: 25}[cur.Code >= 3]), Pressure: round1(1016 - float64(rng.Intn(9))), Wind: cur.Wind, WindDir: cur.WindDir,
		Gust: cur.Gust, UV: cur.UV, IsDay: cur.IsDay, Condition: cur.Condition, Text: Describe(cur.Code, cur.IsDay)}
	return f, nil
}

// Air implements Source: a smoggy winter evening, pollen in its season.
func (Demo) Air(_ context.Context, lat, lon float64) (*Air, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -1)
	a := &Air{Fetched: now.UTC().Format(time.RFC3339)}
	month := int(now.Month())
	winter := month >= 11 || month <= 2
	season := func(kind string, m int) float64 {
		peak := map[string][2]int{"alder": {2, 3}, "birch": {4, 4}, "grass": {5, 6}, "olive": {5, 5}, "mugwort": {8, 8}, "ragweed": {8, 9}}[kind]
		if m >= peak[0] && m <= peak[1] {
			return 1
		}
		if m == peak[0]-1 || m == peak[1]+1 {
			return 0.3
		}
		return 0
	}
	for i := 0; i < 5*24; i++ {
		t := start.Add(time.Duration(i) * time.Hour)
		h := t.Hour()
		evening := math.Max(0, math.Sin(math.Pi*float64((h+24-16)%24)/10))
		aqi := 22 + 14*evening
		if winter {
			aqi = 38 + 50*evening
		}
		aqi *= 1 + spotShift(lat, lon)/12
		pm := aqi * 0.6
		ah := AirHour{Time: t.Format("2006-01-02T15:04"), AQI: round1(aqi), PM25: round1(pm), PM10: round1(pm * 1.4), Ozone: round1(40 + 30*math.Sin(math.Pi*float64(h-6)/14))}
		noon := math.Max(0, math.Sin(math.Pi*float64(h-6)/14))
		for _, k := range PollenKinds {
			v := round1(season(k, month) * 70 * noon)
			switch k {
			case "alder":
				ah.Alder = v
			case "birch":
				ah.Birch = v
			case "grass":
				ah.Grass = v
			case "olive":
				ah.Olive = v
			case "mugwort":
				ah.Mugwort = v
			case "ragweed":
				ah.Ragweed = v
			}
		}
		a.Hours = append(a.Hours, ah)
		if t.Format("2006-01-02T15") == now.Format("2006-01-02T15") {
			a.Now = ah
			a.Now.Time = now.Format("2006-01-02T15:04")
		}
	}
	return a, nil
}

// Archive implements Source with the season's weather.
func (d Demo) Archive(_ context.Context, lat, lon float64, from, to string) ([]Day, error) {
	return d.past(lat, lon, from, to), nil
}

// PastDays implements Source.
func (d Demo) PastDays(_ context.Context, lat, lon float64, from, to string) ([]Day, error) {
	return d.past(lat, lon, from, to), nil
}

func (Demo) past(lat, lon float64, from, to string) []Day {
	a, err1 := time.ParseInLocation("2006-01-02", from, time.Local)
	b, err2 := time.ParseInLocation("2006-01-02", to, time.Local)
	if err1 != nil || err2 != nil || b.Before(a) || b.Sub(a) > 800*24*time.Hour {
		return nil
	}
	var out []Day
	for d := a; !d.After(b); d = d.AddDate(0, 0, 1) {
		rng := rand.New(rand.NewSource(int64(d.Year())*1000 + int64(d.YearDay()) + int64(lat*10)))
		mean := seasonMean(lat, d) + rng.NormFloat64()*3
		codes := []int{0, 1, 2, 2, 3, 3, 61, 63, 80, 45}
		code := codes[rng.Intn(len(codes))]
		lo, hi := mean-3-rng.Float64()*3, mean+3+rng.Float64()*4
		rain, snow := 0.0, 0.0
		if Wet(code) {
			rain = math.Round(1 + rng.Float64()*14)
			if hi < 2 {
				code, snow, rain = 73, rain, 0
			}
		}
		out = append(out, Day{Date: d.Format("2006-01-02"), Code: code, Condition: Condition(code), Text: Describe(code, true), TMax: round1(hi), TMin: round1(lo),
			Precip: round1(rain + snow*0.7), Rain: round1(rain), Snow: round1(snow), Wind: round1(10 + rng.Float64()*20), Gust: round1(25 + rng.Float64()*35),
			SunshineH: round1(math.Max(0, 9-float64(code)/9-rng.Float64()*3)), UV: round1(math.Max(0.5, (mean+4)/3.5))})
	}
	return out
}

var demoPlaces = []Place{
	{Name: "Sarajevo", Region: "Federation of Bosnia and Herzegovina", Country: "Bosnia and Herzegovina", CountryCode: "BA", Lat: 43.8486, Lon: 18.3564, Elevation: 518, Timezone: "Europe/Sarajevo", Population: 696731},
	{Name: "Mostar", Region: "Federation of Bosnia and Herzegovina", Country: "Bosnia and Herzegovina", CountryCode: "BA", Lat: 43.3433, Lon: 17.8081, Elevation: 64, Timezone: "Europe/Sarajevo", Population: 104518},
	{Name: "Jahorina", Region: "Republika Srpska", Country: "Bosnia and Herzegovina", CountryCode: "BA", Lat: 43.7383, Lon: 18.5675, Elevation: 1557, Timezone: "Europe/Sarajevo"},
	{Name: "Vienna", Region: "Vienna", Country: "Austria", CountryCode: "AT", Lat: 48.2085, Lon: 16.3721, Elevation: 171, Timezone: "Europe/Vienna", Population: 1691468},
	{Name: "Zagreb", Region: "City of Zagreb", Country: "Croatia", CountryCode: "HR", Lat: 45.8144, Lon: 15.978, Elevation: 158, Timezone: "Europe/Zagreb", Population: 698966},
	{Name: "Split", Region: "Split-Dalmatia", Country: "Croatia", CountryCode: "HR", Lat: 43.5089, Lon: 16.4392, Elevation: 0, Timezone: "Europe/Zagreb", Population: 176314},
	{Name: "Munich", Region: "Bavaria", Country: "Germany", CountryCode: "DE", Lat: 48.1374, Lon: 11.5755, Elevation: 524, Timezone: "Europe/Berlin", Population: 1260391},
	{Name: "Berlin", Region: "Berlin", Country: "Germany", CountryCode: "DE", Lat: 52.5244, Lon: 13.4105, Elevation: 43, Timezone: "Europe/Berlin", Population: 3426354},
	{Name: "London", Region: "England", Country: "United Kingdom", CountryCode: "GB", Lat: 51.5085, Lon: -0.1257, Elevation: 25, Timezone: "Europe/London", Population: 7556900},
	{Name: "Lisbon", Region: "Lisbon", Country: "Portugal", CountryCode: "PT", Lat: 38.7167, Lon: -9.1333, Elevation: 45, Timezone: "Europe/Lisbon", Population: 517802},
	{Name: "New York", Region: "New York", Country: "United States", CountryCode: "US", Lat: 40.7143, Lon: -74.006, Elevation: 10, Timezone: "America/New_York", Population: 8175133},
}

// Search implements Source over a few well-known places.
func (Demo) Search(_ context.Context, name, _ string, count int) ([]Place, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	out := []Place{}
	for _, p := range demoPlaces {
		if n != "" && strings.HasPrefix(strings.ToLower(p.Name), n) {
			out = append(out, p)
		}
	}
	if count > 0 && len(out) > count {
		out = out[:count]
	}
	return out, nil
}

// Reverse implements Source: the nearest known place.
func (Demo) Reverse(_ context.Context, lat, lon float64, _ string) (*Place, error) {
	best, dist := demoPlaces[0], math.MaxFloat64
	for _, p := range demoPlaces {
		if d := Distance(lat, lon, p.Lat, p.Lon); d < dist {
			best, dist = p, d
		}
	}
	best.Lat, best.Lon = lat, lon
	return &best, nil
}
