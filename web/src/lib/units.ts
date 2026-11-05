// Everything arrives metric (°C, mm, cm, km/h); this shows it in the person's units.
import type { PrecipUnit, Settings, TempUnit, WindUnit } from './types'

export interface Units { temp: TempUnit; wind: WindUnit; precip: PrecipUnit }
export const unitsOf = (s: Settings | null | undefined): Units => ({ temp: s?.temp_unit || 'c', wind: s?.wind_unit || 'kmh', precip: s?.precip_unit || 'mm' })

const minus = (s: string) => s.replace(/^-/, '−')

/** 7° — the number alone, rounded (with ° and no unit). */
export function deg(c: number | null | undefined, u: Units): string {
  if (c == null) return '–'
  const v = Math.round(u.temp === 'f' ? c * 9 / 5 + 32 : c)
  return minus(String(v === 0 ? 0 : v)) + '°'
}
/** 7 °C */
export function temp(c: number | null | undefined, u: Units): string {
  if (c == null) return '–'
  return deg(c, u) + (u.temp === 'f' ? 'F' : 'C')
}
export function windSpeed(kmh: number | null | undefined, u: Units): string {
  if (kmh == null) return '–'
  switch (u.wind) {
    case 'ms': return `${Math.round(kmh / 3.6)} m/s`
    case 'mph': return `${Math.round(kmh / 1.609)} mph`
    case 'kn': return `${Math.round(kmh / 1.852)} kn`
    case 'bft': return `${beaufort(kmh)} Bft`
  }
  return `${Math.round(kmh)} km/h`
}
export function beaufort(kmh: number): number {
  const lim = [1, 6, 12, 20, 29, 39, 50, 62, 75, 89, 103, 118]
  const i = lim.findIndex(l => kmh < l)
  return i < 0 ? 12 : i
}
export function amount(mm: number | null | undefined, u: Units): string {
  if (mm == null) return '–'
  if (u.precip === 'in') return `${(mm / 25.4).toFixed(mm >= 25.4 ? 1 : 2)} in`
  return `${mm < 1 && mm > 0 ? mm.toFixed(1) : Math.round(mm)} mm`
}
export function compass(deg: number | null | undefined): string {
  if (deg == null) return ''
  return ['N', 'NE', 'E', 'SE', 'S', 'SW', 'W', 'NW'][Math.floor(((deg + 22.5) % 360 + 360) % 360 / 45) % 8]
}
export const tempUnitLabel = (u: Units) => u.temp === 'f' ? '°F' : '°C'

/** "14:00" from "2026-11-05T14:00" */
export const hourOf = (t: string) => t.slice(11, 16)

export function uvWord(uv: number | null | undefined): string {
  if (uv == null) return ''
  if (uv < 3) return 'low'
  if (uv < 6) return 'moderate'
  if (uv < 8) return 'high'
  if (uv < 11) return 'very high'
  return 'extreme'
}

export const POLLEN: { key: string; label: string }[] = [
  { key: 'alder', label: 'Alder' }, { key: 'birch', label: 'Birch' }, { key: 'grass', label: 'Grass' },
  { key: 'olive', label: 'Olive' }, { key: 'mugwort', label: 'Mugwort' }, { key: 'ragweed', label: 'Ragweed' },
]
export const LEVEL_LABEL: Record<string, string> = { none: 'none', low: 'low', moderate: 'moderate', high: 'high', very_high: 'very high' }
export const levelRank = (l: string) => ({ none: 0, low: 1, moderate: 2, high: 3, very_high: 4 } as Record<string, number>)[l] ?? 0

export function aqiTone(band: string): string {
  return { good: 'ok', fair: 'ok', moderate: 'warn', poor: 'bad', 'very poor': 'bad', 'extremely poor': 'bad' }[band] || ''
}

export const WARN_ICON: Record<string, string> = {
  frost: 'snowflake', ice: 'snowflake', heat: 'thermometer', wind: 'wind', rain: 'cloud-rain', snow: 'snowflake', storm: 'zap', uv: 'sun', pollen: 'flower', air: 'smog',
}
