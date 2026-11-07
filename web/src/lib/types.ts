export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = '' | 'core' | 'direct'
export type AppSlug = 'calendar' | 'travel' | 'home' | 'garage' | 'pets' | 'league' | 'movement' | 'tasks'

type ConnFields = { [K in AppSlug as `${K}_mode`]: ConnMode } & { [K in AppSlug as `${K}_via`]: ConnVia } & { [K in AppSlug as `${K}_detail` | `${K}_url` | `${K}_token` | `${K}_public_url` | `${K}_open_url`]: string } & { [K in AppSlug as `${K}_configured`]: boolean }

export type TempUnit = 'c' | 'f'
export type WindUnit = 'kmh' | 'ms' | 'mph' | 'kn' | 'bft'
export type PrecipUnit = 'mm' | 'in'
export type PollenKind = 'alder' | 'birch' | 'grass' | 'olive' | 'mugwort' | 'ragweed'

export type Settings = ConnFields & {
  core_configured: boolean
  core_registered?: boolean
  title: string; date_format: string; time_format: string; week_start: number
  temp_unit: TempUnit; wind_unit: WindUnit; precip_unit: PrecipUnit; language: string; follow_me: boolean
  warn_frost: boolean; frost_c: number; warn_heat: boolean; heat_c: number; warn_wind: boolean; gust_kmh: number
  warn_rain: boolean; rain_mm: number; warn_snow: boolean; snow_cm: number; warn_storm: boolean
  warn_uv: boolean; uv_max: number; warn_pollen: boolean; pollen_kinds: PollenKind[]; pollen_level: 'moderate' | 'high' | 'very_high'
  warn_air: boolean; aqi_max: number
  notify_morning: boolean; notify_warnings: boolean; quiet_hours: boolean; notify_hour: number
  notify_url: string; notify_format: string; notify_configured: boolean
  calendar_weather: boolean; travel_weather: boolean; home_plants: boolean; garage_tyres: boolean; pets_walks: boolean; league_weather: boolean
  ai_brief: boolean
  ai_provider: 'auto' | 'pocket' | 'openai'; ai_pocket_model: string; openai_api_key: string; openai_model: string; openai_base_url: string
  setup_complete: boolean; base_url: string
}

export type Condition = 'clear' | 'mostly-clear' | 'partly' | 'cloudy' | 'fog' | 'drizzle' | 'freezing' | 'rain' | 'snow' | 'storm' | 'unknown'

export interface Place {
  id: number; name: string; region: string; country: string; country_code: string; lat: number; lon: number
  elevation: number | null; timezone: string; is_home: boolean; warn: boolean; sort: number; note: string
  source: string; source_ref: string; created_at: string; updated_at: string
}

export interface Now {
  time: string; temp: number | null; feels: number | null; humidity: number | null; precip: number | null; code: number
  cloud: number | null; pressure: number | null; wind: number | null; wind_dir: number | null; gust: number | null; uv: number | null
  is_day: boolean; condition: Condition; text: string
}

export interface Hour {
  time: string; temp: number | null; feels: number | null; humidity: number | null; precip_prob: number | null; precip: number | null
  snow: number | null; code: number; wind: number | null; wind_dir: number | null; gust: number | null; uv: number | null; is_day: boolean; condition: Condition
}

export interface Day {
  date: string; code: number; condition: Condition; text: string; t_max: number | null; t_min: number | null
  feels_max: number | null; feels_min: number | null; precip: number | null; rain: number | null; snow: number | null
  precip_hours: number | null; precip_prob: number | null; wind: number | null; gust: number | null; wind_dir: number | null
  uv: number | null; sunshine_h: number | null; daylight_h: number | null; sunrise: string; sunset: string
}

export interface Warning {
  id: number; location_id: number; kind: string; level: 'advisory' | 'warning'; day: string; starts_at: string; ends_at: string
  title: string; detail: string; value: number | null; notified: string; gone: boolean; place: string
}

export interface PlaceNow extends Place { now: Now | null; today: Day | null; tomorrow: Day | null; warnings: number; stale?: boolean; error?: string }
export interface Overview { places: PlaceNow[]; warnings: Warning[]; today: string }

export interface AirHour { time: string; aqi: number | null; pm2_5: number | null; pm10: number | null; ozone: number | null; alder: number | null; birch: number | null; grass: number | null; mugwort: number | null; olive: number | null; ragweed: number | null }
export interface AirDay { date: string; aqi: number | null; band: string; pollen: Record<string, number>; levels: Record<string, string> }
export interface Air { aqi: number | null; band: string; pm2_5: number | null; pm10: number | null; days: AirDay[]; hours?: AirHour[]; fetched_at: string }

export interface Forecast {
  location: Place | null; now: Now; hours: Hour[]; days: Day[]; today: string; timezone: string; elevation: number
  fetched_at: string; stale: boolean; air?: Air; warnings: Warning[]
}

export interface SearchPlace { name: string; region: string; country: string; country_code: string; lat: number; lon: number; elevation: number; timezone: string; population: number }

export interface Advice {
  key: string; app: string; icon: string; level: 'info' | 'warn'; day: string; title: string; detail: string
  link?: string; task?: { title: string; due: string }; mark?: '' | 'done' | 'hidden' | 'task'
}
export interface PlanEvent {
  id: string; title: string; start: string; end: string; all_day: boolean; location: string; place: string
  code: number; condition: Condition; temp: number | null; t_min?: number | null; t_max?: number | null
  precip_prob: number | null; gust: number | null; flags: string[]; note: string; link?: string
  players?: string; indoor?: boolean
}
export interface PlanTrip { id: number; name: string; destination: string; start_date: string; end_date: string; place: string; lat: number; lon: number; days: Day[]; last_year: Day[]; summary: string; link?: string }
export interface AppState { on: boolean; connected: boolean; detail?: string; count: number; open_url?: string }
export interface Plans { place: string; events: PlanEvent[]; games?: PlanEvent[]; trips: PlanTrip[]; advice: Advice[]; apps: Record<string, AppState>; demo?: boolean }

export interface DayRecord {
  location_id: number; date: string; code: number; t_min: number | null; t_max: number | null; precip: number | null; snow: number | null
  wind: number | null; gust: number | null; uv: number | null; sunshine_h: number | null; aqi: number | null; pollen: Record<string, number>; note: string; updated_at: string
}
export interface DiaryStats { days: number; t_min: number | null; t_max: number | null; mean_high: number | null; mean_low: number | null; precip: number; wet_days: number; snow: number; sunshine_h: number; frost_days: number }
export interface Diary { location: Place; month: string; days: DayRecord[]; stats: DiaryStats; last_year: DiaryStats }
