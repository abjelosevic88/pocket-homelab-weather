package store

import "strings"

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Location is a place whose weather the person follows.
type Location struct {
	ID          int64    `json:"id"`
	UserID      int64    `json:"-"`
	Name        string   `json:"name"`
	Region      string   `json:"region"`
	Country     string   `json:"country"`
	CountryCode string   `json:"country_code"`
	Lat         float64  `json:"lat"`
	Lon         float64  `json:"lon"`
	Elevation   *float64 `json:"elevation"`
	Timezone    string   `json:"timezone"`
	IsHome      bool     `json:"is_home"`
	Warn        bool     `json:"warn"`
	Sort        int      `json:"sort"`
	Note        string   `json:"note"`
	Source      string   `json:"source"`
	SourceRef   string   `json:"source_ref"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// Label is the name with its country when that helps ("Vienna, Austria").
func (l Location) Label() string {
	if l.Country != "" && !strings.Contains(l.Name, l.Country) {
		return l.Name + ", " + l.Country
	}
	return l.Name
}

// DayRecord is one day in the weather diary.
type DayRecord struct {
	LocationID int64              `json:"location_id"`
	Date       string             `json:"date"`
	Code       int                `json:"code"`
	TMin       *float64           `json:"t_min"`
	TMax       *float64           `json:"t_max"`
	Precip     *float64           `json:"precip"`
	Snow       *float64           `json:"snow"`
	Wind       *float64           `json:"wind"`
	Gust       *float64           `json:"gust"`
	UV         *float64           `json:"uv"`
	SunshineH  *float64           `json:"sunshine_h"`
	AQI        *float64           `json:"aqi"`
	Pollen     map[string]float64 `json:"pollen"`
	Note       string             `json:"note"`
	UpdatedAt  string             `json:"updated_at"`
}

// Warning is a warning Weather worked out from the forecast.
type Warning struct {
	ID         int64    `json:"id"`
	LocationID int64    `json:"location_id"`
	Kind       string   `json:"kind"`
	Level      string   `json:"level"` // advisory | warning
	Day        string   `json:"day"`
	StartsAt   string   `json:"starts_at"`
	EndsAt     string   `json:"ends_at"`
	Title      string   `json:"title"`
	Detail     string   `json:"detail"`
	Value      *float64 `json:"value"`
	Notified   string   `json:"notified"`
	Gone       bool     `json:"gone"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`

	// Filled on the way out.
	Place string `json:"place"`
}

// Mark is what the person did with a piece of advice.
type Mark struct {
	Key       string `json:"key"`
	State     string `json:"state"` // done | hidden | task
	Ref       string `json:"ref"`
	CreatedAt string `json:"created_at"`
}

// Conn is how a person reaches one sibling Pocket app: "core" (an app link
// through Pocket Core), "direct" (URL + token) or "off"; "" = not chosen yet.
type Conn struct {
	Mode      string `json:"mode"`
	URL       string `json:"url"`
	Token     string `json:"token"`
	PublicURL string `json:"public_url"`
}

// Settings are the box-wide values plus one person's preferences and their
// own connections.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`
	WeekStart  int    `json:"week_start"` // 1 = Monday, 7 = Sunday

	// Units for display (the data is always metric).
	TempUnit   string `json:"temp_unit"`   // c | f
	WindUnit   string `json:"wind_unit"`   // kmh | ms | mph | kn | bft
	PrecipUnit string `json:"precip_unit"` // mm | in
	Language   string `json:"language"`    // place names: en, bs, de… ('' = English)

	// Where you are now, through Pocket Movement (a "here" place that follows you).
	FollowMe bool `json:"follow_me"`

	// Warnings: what counts, per kind.
	WarnFrost   bool     `json:"warn_frost"`
	FrostC      float64  `json:"frost_c"` // the night's low at or below
	WarnHeat    bool     `json:"warn_heat"`
	HeatC       float64  `json:"heat_c"` // the day's high at or above
	WarnWind    bool     `json:"warn_wind"`
	GustKmh     float64  `json:"gust_kmh"` // gusts at or above
	WarnRain    bool     `json:"warn_rain"`
	RainMM      float64  `json:"rain_mm"` // in a day
	WarnSnow    bool     `json:"warn_snow"`
	SnowCm      float64  `json:"snow_cm"` // in a day
	WarnStorm   bool     `json:"warn_storm"`
	WarnUV      bool     `json:"warn_uv"`
	UVMax       float64  `json:"uv_max"`
	WarnPollen  bool     `json:"warn_pollen"`
	PollenKinds []string `json:"pollen_kinds"` // which ones you react to
	PollenLevel string   `json:"pollen_level"` // moderate | high | very_high
	WarnAir     bool     `json:"warn_air"`
	AQIMax      float64  `json:"aqi_max"` // European AQI at or above

	// Notifications.
	NotifyMorning  bool   `json:"notify_morning"`  // the day ahead at your hour
	NotifyWarnings bool   `json:"notify_warnings"` // a new warning, when it appears
	QuietHours     bool   `json:"quiet_hours"`     // heads-ups wait for the morning between 22:00 and 07:00
	NotifyHour     int    `json:"notify_hour"`
	NotifyURL      string `json:"notify_url"`
	NotifyFormat   string `json:"notify_format"` // apprise | ntfy | json

	// Advice from the other Pocket apps, each switchable.
	CalendarWeather bool `json:"calendar_weather"` // your events with the weather they get
	TravelWeather   bool `json:"travel_weather"`   // your trips' forecast
	HomePlants      bool `json:"home_plants"`      // outdoor plants: frost, heat, storms
	GarageTyres     bool `json:"garage_tyres"`     // winter and summer tyres, frost on the windscreen
	PetsWalks       bool `json:"pets_walks"`       // dog walks in heat, frost and thunder

	// AI: the day in plain words and questions about the forecast.
	AIBrief bool `json:"ai_brief"` // write "the day in plain words" on the Today page

	// Each sibling Pocket app, by slug.
	Conns map[string]Conn `json:"conns"`

	// Pocket AI through Core, or an OpenAI-compatible API with the person's own key.
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`
}

// SplitTags turns "a, b ,c" into clean lowercase tags.
func SplitTags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// JoinTags is the stored form of tags.
func JoinTags(s string) string { return strings.Join(SplitTags(s), ", ") }
