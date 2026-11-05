package server

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-weather/internal/store"
	"github.com/abjelosevic88/pocket-homelab-weather/internal/weather"
)

// ---- warnings, worked out from the forecast ----
//
// These are Pocket Weather's own warnings, not the weather service's: each
// kind has the person's threshold (Settings → Warnings), and a "warning"
// instead of a "heads-up" when the forecast goes well past it. Only today
// and the next two days count; a morning that has passed no longer warns.

// warnDays is how many days ahead warnings look (today included).
const warnDays = 3

// Warning kinds, in the order they are listed.
var warnKinds = []string{"storm", "ice", "wind", "rain", "snow", "frost", "heat", "uv", "air", "pollen"}

// finding is one warning the forecast shows now.
type finding struct {
	Kind, Level, Day, Start, End, Title, Detail string
	Value                                       float64
}

func val(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}

// dayName says a date relative to today where the place is: today,
// tomorrow, Saturday.
func dayName(date, today string) string {
	switch date {
	case today:
		return "today"
	case store.AddDays(today, 1):
		return "tomorrow"
	}
	if t, err := store.ParseDate(date); err == nil {
		return t.Weekday().String()
	}
	return date
}

// partOfDay says when in the day an hour is: in the morning, this afternoon…
func partOfDay(hour string, today string) string {
	if len(hour) < 13 {
		return ""
	}
	h := 0
	fmt.Sscanf(hour[11:13], "%d", &h)
	part := "night"
	switch {
	case h >= 5 && h < 12:
		part = "morning"
	case h >= 12 && h < 18:
		part = "afternoon"
	case h >= 18 && h < 22:
		part = "evening"
	}
	day := hour[:10]
	switch {
	case day == today && part == "night" && h >= 22:
		return "tonight"
	case day == today:
		return "this " + part
	case day == store.AddDays(today, 1) && part == "night" && h < 5:
		return "tonight"
	}
	return dayName(day, today) + " " + part
}

func clock(hour string) string {
	if len(hour) >= 16 {
		return hour[11:16]
	}
	return ""
}

// span finds the hours of a day (after now) where ok holds: first, last
// and the hour where pick is largest.
func span(f *weather.Forecast, date string, ok func(h weather.Hour) bool, pick func(h weather.Hour) float64) (first, last, peak string, peakV float64, n int) {
	now := f.LocalHour()
	peakV = math.Inf(-1)
	for _, h := range f.Hours {
		if !strings.HasPrefix(h.Time, date) || h.Time < now || !ok(h) {
			continue
		}
		if first == "" {
			first = h.Time
		}
		last = h.Time
		n++
		if v := pick(h); v > peakV {
			peakV, peak = v, h.Time
		}
	}
	return
}

func endHour(h string) string {
	t, err := time.Parse("2006-01-02T15:04", h)
	if err != nil {
		return h
	}
	return t.Add(time.Hour).Format("2006-01-02T15:04")
}

// evaluate lists the warnings a place's forecast shows for the person.
func evaluate(f *weather.Forecast, air *weather.Air, st store.Settings, u units) []finding {
	if f == nil {
		return nil
	}
	today := f.LocalDate()
	var out []finding
	add := func(fd finding) { out = append(out, fd) }
	for i := 0; i < warnDays; i++ {
		date := store.AddDays(today, i)
		d := f.DayOf(date)
		if d == nil {
			continue
		}
		when := dayName(date, today)

		if st.WarnStorm {
			first, last, peak, code, n := span(f, date, func(h weather.Hour) bool { return h.Code >= 95 }, func(h weather.Hour) float64 { return float64(h.Code) })
			if n > 0 {
				lvl, what := "advisory", "Thunderstorms"
				if code >= 96 {
					lvl, what = "warning", "Thunderstorms with hail"
				}
				add(finding{Kind: "storm", Level: lvl, Day: date, Start: first, End: endHour(last), Value: code,
					Title:  fmt.Sprintf("%s %s", what, partOfDay(peak, today)),
					Detail: fmt.Sprintf("From about %s. Stay off hills and open water, and bring in what the wind can take.", clock(first))})
			}
		}
		if st.WarnFrost {
			first, last, peak, _, n := span(f, date, func(h weather.Hour) bool {
				c := weather.Condition(h.Code)
				return c == "freezing" || ((c == "rain" || c == "drizzle") && val(h.Temp) <= 0.5)
			}, func(h weather.Hour) float64 { return val(h.Precip) })
			if n > 0 {
				add(finding{Kind: "ice", Level: "warning", Day: date, Start: first, End: endHour(last),
					Title:  "Freezing rain " + partOfDay(peak, today) + ": black ice",
					Detail: fmt.Sprintf("Rain that freezes on the ground from about %s. Roads and pavements will be slippery.", clock(first))})
			}
		}
		if st.WarnWind {
			if g := val(d.Gust); g >= st.GustKmh {
				first, last, peak, pv, _ := span(f, date, func(h weather.Hour) bool { return val(h.Gust) >= st.GustKmh }, func(h weather.Hour) float64 { return val(h.Gust) })
				if first != "" {
					lvl := "advisory"
					if g >= st.GustKmh+25 {
						lvl = "warning"
					}
					add(finding{Kind: "wind", Level: lvl, Day: date, Start: first, End: endHour(last), Value: math.Max(g, pv),
						Title:  fmt.Sprintf("Strong wind %s: gusts up to %s", partOfDay(peak, today), u.wind(math.Max(g, pv))),
						Detail: fmt.Sprintf("From about %s to %s. Secure what can blow away on balconies and in the garden.", clock(first), clock(endHour(last)))})
				}
			}
		}
		if st.WarnRain {
			if p := val(d.Rain); p >= st.RainMM {
				first, last, peak, _, _ := span(f, date, func(h weather.Hour) bool { return val(h.Precip) >= 0.5 }, func(h weather.Hour) float64 { return val(h.Precip) })
				if first != "" || i > 0 {
					lvl := "advisory"
					if p >= st.RainMM*2 {
						lvl = "warning"
					}
					title := fmt.Sprintf("Heavy rain %s: %s", when, u.precip(p))
					detail := "A lot of rain in one day: puddles, full gutters, maybe flooded underpasses."
					if first != "" {
						title = fmt.Sprintf("Heavy rain %s: %s", partOfDay(peak, today), u.precip(p))
						detail = fmt.Sprintf("Mostly between %s and %s. %s", clock(first), clock(endHour(last)), detail)
					}
					add(finding{Kind: "rain", Level: lvl, Day: date, Start: first, End: endHour(last), Value: p, Title: title, Detail: detail})
				}
			}
		}
		if st.WarnSnow {
			if sn := val(d.Snow); sn >= st.SnowCm {
				first, last, _, _, _ := span(f, date, func(h weather.Hour) bool { return val(h.Snow) > 0 }, func(h weather.Hour) float64 { return val(h.Snow) })
				if first != "" || i > 0 {
					lvl := "advisory"
					if sn >= st.SnowCm*3 {
						lvl = "warning"
					}
					add(finding{Kind: "snow", Level: lvl, Day: date, Start: first, End: endHour(last), Value: sn,
						Title:  fmt.Sprintf("Snow %s: about %.0f cm", when, sn),
						Detail: "Allow extra time on the roads; winter tyres make the difference."})
				}
			}
		}
		if st.WarnFrost {
			// the morning's frost: the coldest hour between midnight and nine
			first, last, peak, low, _ := span(f, date, func(h weather.Hour) bool {
				hh := h.Time[11:13]
				return hh < "10" && val(h.Temp) <= st.FrostC
			}, func(h weather.Hour) float64 { return -val(h.Temp) })
			if first != "" {
				low = -low
				lvl := "advisory"
				if low <= st.FrostC-5 {
					lvl = "warning"
				}
				add(finding{Kind: "frost", Level: lvl, Day: date, Start: first, End: endHour(last), Value: low,
					Title:  fmt.Sprintf("Frost %s: down to %s", partOfDay(peak, today), u.temp(low)),
					Detail: "Ice on the windscreen and on bridges; bring frost-tender plants in or cover them."})
			}
		}
		if st.WarnHeat {
			if hi := val(d.TMax); hi >= st.HeatC {
				first, last, peak, _, _ := span(f, date, func(h weather.Hour) bool { return val(h.Temp) >= st.HeatC-2 }, func(h weather.Hour) float64 { return val(h.Temp) })
				if first != "" {
					lvl := "advisory"
					if hi >= st.HeatC+4 {
						lvl = "warning"
					}
					add(finding{Kind: "heat", Level: lvl, Day: date, Start: first, End: endHour(last), Value: hi,
						Title:  fmt.Sprintf("Hot %s: up to %s", when, u.temp(hi)),
						Detail: fmt.Sprintf("Hottest around %s. Drink enough, keep out of the midday sun, check on older neighbours.", clock(peak))})
				}
			}
		}
		if st.WarnUV {
			if uv := val(d.UV); uv >= st.UVMax {
				first, last, _, _, _ := span(f, date, func(h weather.Hour) bool { return val(h.UV) >= 6 }, func(h weather.Hour) float64 { return val(h.UV) })
				if first != "" {
					lvl := "advisory"
					if uv >= 11 {
						lvl = "warning"
					}
					add(finding{Kind: "uv", Level: lvl, Day: date, Start: first, End: endHour(last), Value: uv,
						Title:  fmt.Sprintf("Strong sun %s: UV %.0f", when, uv),
						Detail: fmt.Sprintf("Between %s and %s skin burns quickly: sunscreen, a hat, shade.", clock(first), clock(endHour(last)))})
				}
			}
		}
		aqi, pollen := air.DayMax(date)
		if st.WarnAir && aqi != nil && *aqi >= st.AQIMax && air != nil {
			first, last, peak := "", "", ""
			pv := 0.0
			for _, h := range air.Hours {
				if strings.HasPrefix(h.Time, date) && h.Time >= f.LocalHour() && h.AQI != nil && *h.AQI >= st.AQIMax {
					if first == "" {
						first = h.Time
					}
					last = h.Time
					if *h.AQI > pv {
						pv, peak = *h.AQI, h.Time
					}
				}
			}
			if first != "" {
				lvl := "advisory"
				if pv >= st.AQIMax+20 {
					lvl = "warning"
				}
				add(finding{Kind: "air", Level: lvl, Day: date, Start: first, End: endHour(last), Value: pv,
					Title:  fmt.Sprintf("Air quality %s: %s (AQI %.0f)", partOfDay(peak, today), weather.AQIBand(pv), pv),
					Detail: fmt.Sprintf("Worst around %s. Keep windows shut then, and skip running outside; people with asthma, take care.", clock(peak))})
			}
		}
		if st.WarnPollen && len(st.PollenKinds) > 0 {
			var high []string
			top, topLvl := 0.0, "none"
			for _, k := range st.PollenKinds {
				v, ok := pollen[k]
				if !ok {
					continue
				}
				lvl := weather.PollenLevel(k, v)
				if weather.LevelRank(lvl) >= weather.LevelRank(st.PollenLevel) {
					high = append(high, k)
					if weather.LevelRank(lvl) > weather.LevelRank(topLvl) {
						topLvl, top = lvl, v
					}
				}
			}
			if len(high) > 0 && (i > 0 || f.LocalHour()[11:13] < "18") {
				lvl := "advisory"
				if topLvl == "very_high" {
					lvl = "warning"
				}
				add(finding{Kind: "pollen", Level: lvl, Day: date, Value: top,
					Title:  fmt.Sprintf("%s pollen %s: %s", strings.ToUpper(high[0][:1])+high[0][1:]+andMore(high[1:]), when, strings.ReplaceAll(topLvl, "_", " ")),
					Detail: "Highest around midday. Air rooms early in the morning, and wash your hair before bed."})
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Day != out[b].Day {
			return out[a].Day < out[b].Day
		}
		return kindRank(out[a].Kind) < kindRank(out[b].Kind)
	})
	return out
}

func andMore(rest []string) string {
	switch len(rest) {
	case 0:
		return ""
	case 1:
		return " and " + rest[0]
	}
	return " and " + strings.Join(rest[:len(rest)-1], ", ") + " and " + rest[len(rest)-1]
}

func kindRank(k string) int {
	for i, w := range warnKinds {
		if w == k {
			return i
		}
	}
	return len(warnKinds)
}

// ---- units for words the server writes (pushes, AI prompts) ----

type units struct{ t, w, p string }

func unitsOf(st store.Settings) units { return units{st.TempUnit, st.WindUnit, st.PrecipUnit} }

func (u units) temp(c float64) string {
	if u.t == "f" {
		return fmt.Sprintf("%.0f °F", c*9/5+32)
	}
	v := math.Round(c)
	if v == 0 {
		v = 0 // no "−0"
	}
	return strings.Replace(fmt.Sprintf("%.0f °C", v), "-", "−", 1)
}

func (u units) wind(kmh float64) string {
	switch u.w {
	case "ms":
		return fmt.Sprintf("%.0f m/s", kmh/3.6)
	case "mph":
		return fmt.Sprintf("%.0f mph", kmh/1.609)
	case "kn":
		return fmt.Sprintf("%.0f kn", kmh/1.852)
	case "bft":
		return fmt.Sprintf("%d Bft", beaufort(kmh))
	}
	return fmt.Sprintf("%.0f km/h", kmh)
}

func (u units) precip(mm float64) string {
	if u.p == "in" {
		return fmt.Sprintf("%.1f in", mm/25.4)
	}
	return fmt.Sprintf("%.0f mm", mm)
}

func beaufort(kmh float64) int {
	limits := []float64{1, 6, 12, 20, 29, 39, 50, 62, 75, 89, 103, 118}
	for i, l := range limits {
		if kmh < l {
			return i
		}
	}
	return 12
}
