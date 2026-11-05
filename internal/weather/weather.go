// Package weather talks to Open-Meteo (free, no key): the forecast with the
// weather now, 48 hours and 10 days, air quality and pollen, the archive for
// any past date and place search; and to OpenStreetMap's Nominatim for the
// name of a spot on the map ("where am I"). Everything comes back metric:
// °C, mm, cm of snow, km/h, hPa; the web app converts for display.
package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Endpoints, variables so tests can point them at a fake server.
var (
	ForecastURL = "https://api.open-meteo.com/v1/forecast"
	AirURL      = "https://air-quality-api.open-meteo.com/v1/air-quality"
	ArchiveURL  = "https://archive-api.open-meteo.com/v1/archive"
	GeocodeURL  = "https://geocoding-api.open-meteo.com/v1/search"
	ReverseURL  = "https://nominatim.openstreetmap.org/reverse"
)

// Client fetches weather.
type Client struct {
	UserAgent string
	HTTP      *http.Client
}

// New creates a client.
func New(ua string) *Client {
	return &Client{UserAgent: ua, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// Now is the weather at this moment.
type Now struct {
	Time      string   `json:"time"` // local wall clock where the place is: 2026-11-05T14:15
	Temp      *float64 `json:"temp"`
	Feels     *float64 `json:"feels"`
	Humidity  *float64 `json:"humidity"`
	Precip    *float64 `json:"precip"`
	Code      int      `json:"code"`
	Cloud     *float64 `json:"cloud"`
	Pressure  *float64 `json:"pressure"`
	Wind      *float64 `json:"wind"`
	WindDir   *float64 `json:"wind_dir"`
	Gust      *float64 `json:"gust"`
	UV        *float64 `json:"uv"`
	IsDay     bool     `json:"is_day"`
	Condition string   `json:"condition"`
	Text      string   `json:"text"`
}

// Hour is one hour of the forecast.
type Hour struct {
	Time       string   `json:"time"`
	Temp       *float64 `json:"temp"`
	Feels      *float64 `json:"feels"`
	Humidity   *float64 `json:"humidity"`
	PrecipProb *float64 `json:"precip_prob"`
	Precip     *float64 `json:"precip"`
	Snow       *float64 `json:"snow"`
	Code       int      `json:"code"`
	Wind       *float64 `json:"wind"`
	WindDir    *float64 `json:"wind_dir"`
	Gust       *float64 `json:"gust"`
	UV         *float64 `json:"uv"`
	IsDay      bool     `json:"is_day"`
	Condition  string   `json:"condition"`
}

// Day is one day of the forecast (or of the past).
type Day struct {
	Date       string   `json:"date"`
	Code       int      `json:"code"`
	Condition  string   `json:"condition"`
	Text       string   `json:"text"`
	TMax       *float64 `json:"t_max"`
	TMin       *float64 `json:"t_min"`
	FeelsMax   *float64 `json:"feels_max"`
	FeelsMin   *float64 `json:"feels_min"`
	Precip     *float64 `json:"precip"`
	Rain       *float64 `json:"rain"`
	Snow       *float64 `json:"snow"`
	PrecipH    *float64 `json:"precip_hours"`
	PrecipProb *float64 `json:"precip_prob"`
	Wind       *float64 `json:"wind"`
	Gust       *float64 `json:"gust"`
	WindDir    *float64 `json:"wind_dir"`
	UV         *float64 `json:"uv"`
	SunshineH  *float64 `json:"sunshine_h"`
	DaylightH  *float64 `json:"daylight_h"`
	Sunrise    string   `json:"sunrise"`
	Sunset     string   `json:"sunset"`
}

// Forecast is what one place gets: now, the hours and the days.
type Forecast struct {
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Elevation float64 `json:"elevation"`
	Timezone  string  `json:"timezone"`
	TZAbbr    string  `json:"tz_abbr"`
	Fetched   string  `json:"fetched_at"`
	Now       Now     `json:"now"`
	Hours     []Hour  `json:"hours"` // from the start of yesterday, hourly
	Days      []Day   `json:"days"`  // yesterday, today and the next 9 days
}

// Today is the forecast's day for the place's own date ("" when missing).
func (f *Forecast) Today() *Day {
	if f == nil {
		return nil
	}
	return f.DayOf(f.LocalDate())
}

// LocalDate is today's date where the place is.
func (f *Forecast) LocalDate() string {
	if f == nil {
		return ""
	}
	if len(f.Now.Time) >= 10 {
		return f.Now.Time[:10]
	}
	return time.Now().In(f.Location()).Format("2006-01-02")
}

// LocalHour is the hour now where the place is, as in Hour.Time ("2026-11-05T14:00").
func (f *Forecast) LocalHour() string {
	if len(f.Now.Time) >= 13 {
		return f.Now.Time[:13] + ":00"
	}
	return time.Now().In(f.Location()).Format("2006-01-02T15") + ":00"
}

// Location is the place's time zone (the box's when unknown).
func (f *Forecast) Location() *time.Location {
	if f != nil && f.Timezone != "" {
		if l, err := time.LoadLocation(f.Timezone); err == nil {
			return l
		}
	}
	return time.Local
}

// DayOf finds one date.
func (f *Forecast) DayOf(date string) *Day {
	if f == nil {
		return nil
	}
	for i := range f.Days {
		if f.Days[i].Date == date {
			return &f.Days[i]
		}
	}
	return nil
}

// HoursFrom returns n hours starting at the hour now.
func (f *Forecast) HoursFrom(n int) []Hour {
	if f == nil {
		return nil
	}
	start := f.LocalHour()
	for i, h := range f.Hours {
		if h.Time >= start {
			end := i + n
			if end > len(f.Hours) {
				end = len(f.Hours)
			}
			return f.Hours[i:end]
		}
	}
	return nil
}

// HourAt returns the hour of a local wall clock time ("2026-11-05T18:30" → 18:00).
func (f *Forecast) HourAt(at string) *Hour {
	if f == nil || len(at) < 13 {
		return nil
	}
	key := at[:13] + ":00"
	for i := range f.Hours {
		if f.Hours[i].Time == key {
			return &f.Hours[i]
		}
	}
	return nil
}

const (
	currentVars = "temperature_2m,apparent_temperature,relative_humidity_2m,is_day,precipitation,weather_code,cloud_cover,pressure_msl,wind_speed_10m,wind_direction_10m,wind_gusts_10m,uv_index"
	hourlyVars  = "temperature_2m,apparent_temperature,relative_humidity_2m,precipitation_probability,precipitation,snowfall,weather_code,wind_speed_10m,wind_direction_10m,wind_gusts_10m,uv_index,is_day"
	dailyVars   = "weather_code,temperature_2m_max,temperature_2m_min,apparent_temperature_max,apparent_temperature_min,sunrise,sunset,daylight_duration,sunshine_duration,uv_index_max,precipitation_sum,rain_sum,snowfall_sum,precipitation_hours,precipitation_probability_max,wind_speed_10m_max,wind_gusts_10m_max,wind_direction_10m_dominant"
	archiveVars = "weather_code,temperature_2m_max,temperature_2m_min,apparent_temperature_max,apparent_temperature_min,sunrise,sunset,daylight_duration,sunshine_duration,precipitation_sum,rain_sum,snowfall_sum,precipitation_hours,wind_speed_10m_max,wind_gusts_10m_max,wind_direction_10m_dominant"
)

type series []*float64

func (s series) at(i int) *float64 {
	if i < len(s) && s[i] != nil {
		v := math.Round(*s[i]*10) / 10
		return &v
	}
	return nil
}

func (s series) code(i int) int {
	if i < len(s) && s[i] != nil {
		return int(*s[i])
	}
	return -1
}

func hours(s series, i int) *float64 {
	if v := s.at(i); v != nil {
		h := math.Round(*v/360) / 10
		return &h
	}
	return nil
}

type dailyBody struct {
	Time       []string `json:"time"`
	Code       series   `json:"weather_code"`
	TMax       series   `json:"temperature_2m_max"`
	TMin       series   `json:"temperature_2m_min"`
	FeelsMax   series   `json:"apparent_temperature_max"`
	FeelsMin   series   `json:"apparent_temperature_min"`
	Sunrise    []string `json:"sunrise"`
	Sunset     []string `json:"sunset"`
	Daylight   series   `json:"daylight_duration"`
	Sunshine   series   `json:"sunshine_duration"`
	UV         series   `json:"uv_index_max"`
	Precip     series   `json:"precipitation_sum"`
	Rain       series   `json:"rain_sum"`
	Snow       series   `json:"snowfall_sum"`
	PrecipH    series   `json:"precipitation_hours"`
	PrecipProb series   `json:"precipitation_probability_max"`
	Wind       series   `json:"wind_speed_10m_max"`
	Gust       series   `json:"wind_gusts_10m_max"`
	WindDir    series   `json:"wind_direction_10m_dominant"`
}

func (d dailyBody) days() []Day {
	str := func(a []string, i int) string {
		if i < len(a) {
			return a[i]
		}
		return ""
	}
	out := make([]Day, 0, len(d.Time))
	for i, date := range d.Time {
		code := d.Code.code(i)
		if code < 0 && d.TMax.at(i) == nil {
			continue
		}
		day := Day{Date: date, Code: code, Condition: Condition(code), Text: Describe(code, true),
			TMax: d.TMax.at(i), TMin: d.TMin.at(i), FeelsMax: d.FeelsMax.at(i), FeelsMin: d.FeelsMin.at(i),
			Precip: d.Precip.at(i), Rain: d.Rain.at(i), Snow: d.Snow.at(i), PrecipH: d.PrecipH.at(i), PrecipProb: d.PrecipProb.at(i),
			Wind: d.Wind.at(i), Gust: d.Gust.at(i), WindDir: d.WindDir.at(i), UV: d.UV.at(i),
			SunshineH: hours(d.Sunshine, i), DaylightH: hours(d.Daylight, i), Sunrise: str(d.Sunrise, i), Sunset: str(d.Sunset, i)}
		out = append(out, day)
	}
	return out
}

// Forecast fetches now, the hours from yesterday on and 11 days (yesterday + 10).
func (c *Client) Forecast(ctx context.Context, lat, lon float64) (*Forecast, error) {
	q := url.Values{}
	q.Set("latitude", coord(lat))
	q.Set("longitude", coord(lon))
	q.Set("current", currentVars)
	q.Set("hourly", hourlyVars)
	q.Set("daily", dailyVars)
	q.Set("timezone", "auto")
	q.Set("forecast_days", "10")
	q.Set("past_days", "1")
	var body struct {
		Lat       float64 `json:"latitude"`
		Lon       float64 `json:"longitude"`
		Elevation float64 `json:"elevation"`
		Timezone  string  `json:"timezone"`
		TZAbbr    string  `json:"timezone_abbreviation"`
		Current   struct {
			Time     string   `json:"time"`
			Temp     *float64 `json:"temperature_2m"`
			Feels    *float64 `json:"apparent_temperature"`
			Humidity *float64 `json:"relative_humidity_2m"`
			IsDay    *float64 `json:"is_day"`
			Precip   *float64 `json:"precipitation"`
			Code     *float64 `json:"weather_code"`
			Cloud    *float64 `json:"cloud_cover"`
			Pressure *float64 `json:"pressure_msl"`
			Wind     *float64 `json:"wind_speed_10m"`
			WindDir  *float64 `json:"wind_direction_10m"`
			Gust     *float64 `json:"wind_gusts_10m"`
			UV       *float64 `json:"uv_index"`
		} `json:"current"`
		Hourly struct {
			Time       []string `json:"time"`
			Temp       series   `json:"temperature_2m"`
			Feels      series   `json:"apparent_temperature"`
			Humidity   series   `json:"relative_humidity_2m"`
			PrecipProb series   `json:"precipitation_probability"`
			Precip     series   `json:"precipitation"`
			Snow       series   `json:"snowfall"`
			Code       series   `json:"weather_code"`
			Wind       series   `json:"wind_speed_10m"`
			WindDir    series   `json:"wind_direction_10m"`
			Gust       series   `json:"wind_gusts_10m"`
			UV         series   `json:"uv_index"`
			IsDay      series   `json:"is_day"`
		} `json:"hourly"`
		Daily dailyBody `json:"daily"`
	}
	if err := c.get(ctx, ForecastURL, q, &body); err != nil {
		return nil, err
	}
	one := func(p *float64) *float64 { return series{p}.at(0) }
	cur := body.Current
	code := series{cur.Code}.code(0)
	isDay := cur.IsDay == nil || *cur.IsDay > 0
	f := &Forecast{Lat: body.Lat, Lon: body.Lon, Elevation: body.Elevation, Timezone: body.Timezone, TZAbbr: body.TZAbbr,
		Fetched: time.Now().UTC().Format(time.RFC3339),
		Now: Now{Time: cur.Time, Temp: one(cur.Temp), Feels: one(cur.Feels), Humidity: one(cur.Humidity), Precip: one(cur.Precip), Code: code,
			Cloud: one(cur.Cloud), Pressure: one(cur.Pressure), Wind: one(cur.Wind), WindDir: one(cur.WindDir), Gust: one(cur.Gust), UV: one(cur.UV),
			IsDay: isDay, Condition: Condition(code), Text: Describe(code, isDay)},
		Days: body.Daily.days()}
	h := body.Hourly
	f.Hours = make([]Hour, 0, len(h.Time))
	for i, t := range h.Time {
		hc := h.Code.code(i)
		day := true
		if v := h.IsDay.at(i); v != nil {
			day = *v > 0
		}
		f.Hours = append(f.Hours, Hour{Time: t, Temp: h.Temp.at(i), Feels: h.Feels.at(i), Humidity: h.Humidity.at(i), PrecipProb: h.PrecipProb.at(i),
			Precip: h.Precip.at(i), Snow: h.Snow.at(i), Code: hc, Wind: h.Wind.at(i), WindDir: h.WindDir.at(i), Gust: h.Gust.at(i), UV: h.UV.at(i),
			IsDay: day, Condition: Condition(hc)})
	}
	if len(f.Days) == 0 {
		return nil, errors.New("open-meteo: no forecast for this place")
	}
	return f, nil
}

// Archive fetches past days (from the ERA5 archive, a few days behind).
func (c *Client) Archive(ctx context.Context, lat, lon float64, from, to string) ([]Day, error) {
	q := url.Values{}
	q.Set("latitude", coord(lat))
	q.Set("longitude", coord(lon))
	q.Set("daily", archiveVars)
	q.Set("timezone", "auto")
	q.Set("start_date", from)
	q.Set("end_date", to)
	var body struct {
		Daily dailyBody `json:"daily"`
	}
	if err := c.get(ctx, ArchiveURL, q, &body); err != nil {
		return nil, err
	}
	return body.Daily.days(), nil
}

// PastDays fetches recent past days from the forecast API (it keeps about three months).
func (c *Client) PastDays(ctx context.Context, lat, lon float64, from, to string) ([]Day, error) {
	q := url.Values{}
	q.Set("latitude", coord(lat))
	q.Set("longitude", coord(lon))
	q.Set("daily", dailyVars)
	q.Set("timezone", "auto")
	q.Set("start_date", from)
	q.Set("end_date", to)
	var body struct {
		Daily dailyBody `json:"daily"`
	}
	if err := c.get(ctx, ForecastURL, q, &body); err != nil {
		return nil, err
	}
	return body.Daily.days(), nil
}

// ---- air quality and pollen ----

// AirHour is one hour of air quality.
type AirHour struct {
	Time    string   `json:"time"`
	AQI     *float64 `json:"aqi"` // European AQI
	PM25    *float64 `json:"pm2_5"`
	PM10    *float64 `json:"pm10"`
	Ozone   *float64 `json:"ozone"`
	Alder   *float64 `json:"alder"`
	Birch   *float64 `json:"birch"`
	Grass   *float64 `json:"grass"`
	Mugwort *float64 `json:"mugwort"`
	Olive   *float64 `json:"olive"`
	Ragweed *float64 `json:"ragweed"`
}

// Air is air quality now and per hour for four days. Pollen is only known in Europe.
type Air struct {
	Fetched string    `json:"fetched_at"`
	Now     AirHour   `json:"now"`
	Hours   []AirHour `json:"hours"`
}

// PollenKinds are the pollen Open-Meteo knows (Europe only), in season order.
var PollenKinds = []string{"alder", "birch", "grass", "olive", "mugwort", "ragweed"}

// Pollen returns one kind's value.
func (h AirHour) Pollen(kind string) *float64 {
	switch kind {
	case "alder":
		return h.Alder
	case "birch":
		return h.Birch
	case "grass":
		return h.Grass
	case "mugwort":
		return h.Mugwort
	case "olive":
		return h.Olive
	case "ragweed":
		return h.Ragweed
	}
	return nil
}

// Air fetches air quality and pollen.
func (c *Client) Air(ctx context.Context, lat, lon float64) (*Air, error) {
	vars := "european_aqi,pm2_5,pm10,ozone,alder_pollen,birch_pollen,grass_pollen,mugwort_pollen,olive_pollen,ragweed_pollen"
	q := url.Values{}
	q.Set("latitude", coord(lat))
	q.Set("longitude", coord(lon))
	q.Set("current", "european_aqi,pm2_5,pm10,ozone")
	q.Set("hourly", vars)
	q.Set("timezone", "auto")
	q.Set("forecast_days", "4")
	q.Set("past_days", "1")
	var body struct {
		Current struct {
			Time  string   `json:"time"`
			AQI   *float64 `json:"european_aqi"`
			PM25  *float64 `json:"pm2_5"`
			PM10  *float64 `json:"pm10"`
			Ozone *float64 `json:"ozone"`
		} `json:"current"`
		Hourly struct {
			Time    []string `json:"time"`
			AQI     series   `json:"european_aqi"`
			PM25    series   `json:"pm2_5"`
			PM10    series   `json:"pm10"`
			Ozone   series   `json:"ozone"`
			Alder   series   `json:"alder_pollen"`
			Birch   series   `json:"birch_pollen"`
			Grass   series   `json:"grass_pollen"`
			Mugwort series   `json:"mugwort_pollen"`
			Olive   series   `json:"olive_pollen"`
			Ragweed series   `json:"ragweed_pollen"`
		} `json:"hourly"`
	}
	if err := c.get(ctx, AirURL, q, &body); err != nil {
		return nil, err
	}
	one := func(p *float64) *float64 { return series{p}.at(0) }
	a := &Air{Fetched: time.Now().UTC().Format(time.RFC3339),
		Now: AirHour{Time: body.Current.Time, AQI: one(body.Current.AQI), PM25: one(body.Current.PM25), PM10: one(body.Current.PM10), Ozone: one(body.Current.Ozone)}}
	h := body.Hourly
	for i, t := range h.Time {
		a.Hours = append(a.Hours, AirHour{Time: t, AQI: h.AQI.at(i), PM25: h.PM25.at(i), PM10: h.PM10.at(i), Ozone: h.Ozone.at(i),
			Alder: h.Alder.at(i), Birch: h.Birch.at(i), Grass: h.Grass.at(i), Mugwort: h.Mugwort.at(i), Olive: h.Olive.at(i), Ragweed: h.Ragweed.at(i)})
		if t[:13] == a.Now.Time[:min(13, len(a.Now.Time))] {
			cur := a.Hours[len(a.Hours)-1]
			a.Now.Alder, a.Now.Birch, a.Now.Grass, a.Now.Mugwort, a.Now.Olive, a.Now.Ragweed = cur.Alder, cur.Birch, cur.Grass, cur.Mugwort, cur.Olive, cur.Ragweed
		}
	}
	return a, nil
}

// DayMax is the highest value of each pollen kind and of the AQI on a date.
func (a *Air) DayMax(date string) (aqi *float64, pollen map[string]float64) {
	pollen = map[string]float64{}
	if a == nil {
		return nil, pollen
	}
	for _, h := range a.Hours {
		if !strings.HasPrefix(h.Time, date) {
			continue
		}
		if h.AQI != nil && (aqi == nil || *h.AQI > *aqi) {
			v := *h.AQI
			aqi = &v
		}
		for _, k := range PollenKinds {
			if v := h.Pollen(k); v != nil && *v > pollen[k] {
				pollen[k] = *v
			}
		}
	}
	return aqi, pollen
}

// PollenLevel turns grains per m³ into none | low | moderate | high | very_high.
// The steps follow the usual European pollen services; ragweed and mugwort
// trouble people at much lower counts than tree pollen.
func PollenLevel(kind string, v float64) string {
	steps := map[string][3]float64{
		"alder": {10, 50, 200}, "birch": {10, 50, 200}, "olive": {10, 50, 200},
		"grass": {5, 20, 80}, "mugwort": {5, 15, 50}, "ragweed": {3, 10, 40},
	}[kind]
	switch {
	case v < 1:
		return "none"
	case v < steps[0]:
		return "low"
	case v < steps[1]:
		return "moderate"
	case v < steps[2]:
		return "high"
	}
	return "very_high"
}

// LevelRank orders pollen levels.
func LevelRank(l string) int {
	return map[string]int{"none": 0, "low": 1, "moderate": 2, "high": 3, "very_high": 4}[l]
}

// AQIBand names a European AQI value.
func AQIBand(v float64) string {
	switch {
	case v <= 20:
		return "good"
	case v <= 40:
		return "fair"
	case v <= 60:
		return "moderate"
	case v <= 80:
		return "poor"
	case v <= 100:
		return "very poor"
	}
	return "extremely poor"
}

// ---- places ----

// Place is a search result.
type Place struct {
	Name        string  `json:"name"`
	Region      string  `json:"region"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Elevation   float64 `json:"elevation"`
	Timezone    string  `json:"timezone"`
	Population  int     `json:"population"`
}

// Search finds places by name (cities, towns, villages).
func (c *Client) Search(ctx context.Context, name, lang string, count int) ([]Place, error) {
	if count <= 0 || count > 20 {
		count = 8
	}
	q := url.Values{}
	q.Set("name", name)
	q.Set("count", fmt.Sprint(count))
	q.Set("format", "json")
	if lang != "" {
		q.Set("language", lang)
	}
	var body struct {
		Results []struct {
			Name        string  `json:"name"`
			Admin1      string  `json:"admin1"`
			Country     string  `json:"country"`
			CountryCode string  `json:"country_code"`
			Lat         float64 `json:"latitude"`
			Lon         float64 `json:"longitude"`
			Elevation   float64 `json:"elevation"`
			Timezone    string  `json:"timezone"`
			Population  int     `json:"population"`
		} `json:"results"`
	}
	if err := c.get(ctx, GeocodeURL, q, &body); err != nil {
		return nil, err
	}
	out := []Place{}
	for _, r := range body.Results {
		out = append(out, Place{Name: r.Name, Region: r.Admin1, Country: r.Country, CountryCode: strings.ToUpper(r.CountryCode), Lat: r.Lat, Lon: r.Lon,
			Elevation: r.Elevation, Timezone: r.Timezone, Population: r.Population})
	}
	return out, nil
}

// Reverse names a spot on the map: the town or city it is in.
func (c *Client) Reverse(ctx context.Context, lat, lon float64, lang string) (*Place, error) {
	q := url.Values{}
	q.Set("lat", coord(lat))
	q.Set("lon", coord(lon))
	q.Set("format", "jsonv2")
	q.Set("zoom", "10")
	if lang != "" {
		q.Set("accept-language", lang)
	}
	var body struct {
		Name    string `json:"name"`
		Address struct {
			City        string `json:"city"`
			Town        string `json:"town"`
			Village     string `json:"village"`
			Municipalty string `json:"municipality"`
			County      string `json:"county"`
			State       string `json:"state"`
			Country     string `json:"country"`
			CountryCode string `json:"country_code"`
		} `json:"address"`
	}
	if err := c.get(ctx, ReverseURL, q, &body); err != nil {
		return nil, err
	}
	a := body.Address
	// a city split into municipalities (Sarajevo, Belgrade) names the
	// municipality first; the county "City of Sarajevo" is what people say
	county := ""
	for _, p := range []string{"City of ", "Grad ", "Stadt "} {
		if strings.HasPrefix(a.County, p) {
			county = strings.TrimPrefix(a.County, p)
		}
	}
	muni := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(a.Municipalty, " Municipality"), " municipality"))
	name := firstNonEmpty(a.City, a.Town, a.Village, county, muni, body.Name, a.County)
	if name == "" {
		return nil, errors.New("no name for this spot")
	}
	return &Place{Name: name, Region: a.State, Country: a.Country, CountryCode: strings.ToUpper(a.CountryCode), Lat: lat, Lon: lon}, nil
}

func (c *Client) get(ctx context.Context, base string, q url.Values, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", host(base), err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		var e struct {
			Reason string `json:"reason"`
			Error  any    `json:"error"`
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4000))
		_ = json.Unmarshal(b, &e)
		if e.Reason == "" {
			e.Reason = res.Status
		}
		return fmt.Errorf("%s: %s", host(base), e.Reason)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(v); err != nil {
		return fmt.Errorf("%s: %w", host(base), err)
	}
	return nil
}

func host(u string) string {
	if p, err := url.Parse(u); err == nil {
		return p.Host
	}
	return u
}

func coord(v float64) string { return fmt.Sprintf("%.4f", v) }

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// ---- words ----

// Condition maps a WMO weather code to a short key the app draws an icon for.
func Condition(code int) string {
	switch {
	case code == 0:
		return "clear"
	case code == 1:
		return "mostly-clear"
	case code == 2:
		return "partly"
	case code == 3:
		return "cloudy"
	case code == 45 || code == 48:
		return "fog"
	case code >= 51 && code <= 55:
		return "drizzle"
	case code == 56 || code == 57 || code == 66 || code == 67:
		return "freezing"
	case code >= 61 && code <= 65, code >= 80 && code <= 82:
		return "rain"
	case code >= 71 && code <= 77, code == 85 || code == 86:
		return "snow"
	case code >= 95:
		return "storm"
	}
	return "unknown"
}

// Describe says a WMO weather code in plain words.
func Describe(code int, day bool) string {
	clear := "Clear"
	if day {
		clear = "Sunny"
	}
	m := map[int]string{
		0: clear, 1: "Mostly " + strings.ToLower(clear), 2: "Partly cloudy", 3: "Cloudy",
		45: "Fog", 48: "Freezing fog",
		51: "Light drizzle", 53: "Drizzle", 55: "Heavy drizzle", 56: "Freezing drizzle", 57: "Heavy freezing drizzle",
		61: "Light rain", 63: "Rain", 65: "Heavy rain", 66: "Freezing rain", 67: "Heavy freezing rain",
		71: "Light snow", 73: "Snow", 75: "Heavy snow", 77: "Snow grains",
		80: "Rain showers", 81: "Heavy showers", 82: "Violent showers", 85: "Snow showers", 86: "Heavy snow showers",
		95: "Thunderstorm", 96: "Thunderstorm with hail", 99: "Thunderstorm with heavy hail",
	}
	if t, ok := m[code]; ok {
		return t
	}
	return "—"
}

// Wet reports whether a code means something falls from the sky.
func Wet(code int) bool {
	c := Condition(code)
	return c == "drizzle" || c == "rain" || c == "snow" || c == "storm" || c == "freezing"
}

// Compass turns degrees into N, NE, E…
func Compass(deg float64) string {
	dirs := []string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}
	return dirs[int(math.Mod(deg+22.5+360, 360)/45)%8]
}

// Distance is the great-circle distance in km.
func Distance(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp, dl := (lat2-lat1)*math.Pi/180, (lon2-lon1)*math.Pi/180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
