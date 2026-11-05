// The diary: what every day was like at a place, a month at a time, next to
// the same month last year; your own notes; and any date looked up.
import { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useData, useOnChange } from '../lib/data'
import { fmtDate, weekday } from '../lib/format'
import type { Day, Diary, DayRecord, DiaryStats } from '../lib/types'
import { amount, deg, temp, unitsOf, type Units } from '../lib/units'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { WeatherIcon } from '../components/WeatherIcon'
import { Card, Empty, Field, Loading, Modal, useAsync, useToast } from '../components/ui'

const condOf = (code: number) => code === 0 ? 'clear' : code === 1 ? 'mostly-clear' : code === 2 ? 'partly' : code === 3 ? 'cloudy' : code === 45 || code === 48 ? 'fog'
  : code >= 51 && code <= 55 ? 'drizzle' : [56, 57, 66, 67].includes(code) ? 'freezing' : (code >= 61 && code <= 65) || (code >= 80 && code <= 82) ? 'rain'
  : (code >= 71 && code <= 77) || code === 85 || code === 86 ? 'snow' : code >= 95 ? 'storm' : 'unknown'

function shiftMonth(m: string, n: number): string {
  const d = new Date(Number(m.slice(0, 4)), Number(m.slice(5, 7)) - 1 + n, 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}
const monthName = (m: string) => new Date(Number(m.slice(0, 4)), Number(m.slice(5, 7)) - 1, 1).toLocaleDateString(undefined, { month: 'long', year: 'numeric' })

export default function DiaryPage() {
  const { settings } = useApp()
  const { overview } = useData()
  const u = unitsOf(settings)
  const places = (overview?.places || []).filter(p => p.warn)
  const [placeId, setPlaceId] = useState<number>(0)
  const [month, setMonth] = useState(() => new Date().toISOString().slice(0, 7))
  useEffect(() => { if (!placeId && places.length) setPlaceId((places.find(p => p.is_home) || places[0]).id) }, [places, placeId])
  const { data, error, reload } = useAsync(() => placeId ? api.get<Diary>(`${V1}/diary?location=${placeId}&month=${month}`) : Promise.resolve(null), [placeId, month])
  useOnChange(reload)
  const [edit, setEdit] = useState<DayRecord | null>(null)
  const thisMonth = new Date().toISOString().slice(0, 7)
  if (overview && places.length === 0) return <Card><Empty title="No diary yet" icon="journal">The diary keeps every day at the places you get warnings for. Add a place first.</Empty></Card>
  return (
    <div className="stack loose">
      <div className="row wrap" style={{ gap: 8 }}>
        {places.length > 1 && <div style={{ width: 200 }}><Select ariaLabel="Place" value={String(placeId)} onChange={v => setPlaceId(Number(v))} options={places.map(p => ({ value: String(p.id), label: p.name }))} /></div>}
        <div className="row" style={{ gap: 4 }}>
          <button type="button" className="btn ghost sm icon-only" aria-label="Previous month" onClick={() => setMonth(shiftMonth(month, -1))}><Icon name="chevron-left" size={15} /></button>
          <span className="wx-month">{monthName(month)}</span>
          <button type="button" className="btn ghost sm icon-only" aria-label="Next month" disabled={month >= thisMonth} onClick={() => setMonth(shiftMonth(month, 1))}><Icon name="chevron-right" size={15} /></button>
        </div>
        <span className="grow" />
        {placeId > 0 && <LookUp placeId={placeId} u={u} />}
      </div>
      {error && <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>}
      {!data ? <Loading /> : <>
        <Stats s={data.stats} last={data.last_year} u={u} />
        <Card title={`${monthName(month)} in ${data.location.name}`} flush>
          {data.days.length === 0 ? <div className="pad"><Empty title="No days in the diary for this month" icon="journal">The diary starts the day a place is added; any date before that can still be looked up.</Empty></div> : (
            <div className="list">
              {data.days.map(d => (
                <button key={d.date} type="button" className="list-row wx-diary-row" onClick={() => setEdit(d)}>
                  <span className="wx-diary-date"><b>{Number(d.date.slice(8))}</b><span className="muted small">{weekday(d.date)}</span></span>
                  <WeatherIcon condition={condOf(d.code)} size={26} />
                  <span className="tnum nowrap">{deg(d.t_min, u)} / {deg(d.t_max, u)}</span>
                  <span className="grow ellipsis muted small">{(d.precip ?? 0) >= 1 ? amount(d.precip, u) : ''}{(d.snow ?? 0) >= 1 ? ` · ${Math.round(d.snow ?? 0)} cm snow` : ''}{d.note ? <span className="wx-note"> “{d.note}”</span> : ''}</span>
                  <Icon name="edit" size={14} className="muted" />
                </button>
              ))}
            </div>
          )}
        </Card>
      </>}
      {edit && <NoteModal d={edit} placeId={placeId} u={u} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reload() }} />}
    </div>
  )
}

function Stats({ s, last, u }: { s: DiaryStats; last: DiaryStats; u: Units }) {
  if (!s.days) return null
  const cmp = (a: number | null, b: number | null, unit: (v: number) => string) => b == null || a == null || !last.days ? '' : `last year ${unit(b)}`
  const tiles: [string, string, string, string][] = [
    ['thermometer', 'Warmest', temp(s.t_max, u), cmp(s.t_max, last.t_max, v => temp(v, u))],
    ['snowflake', 'Coldest', temp(s.t_min, u), cmp(s.t_min, last.t_min, v => temp(v, u))],
    ['sun', 'Average high', temp(s.mean_high, u), cmp(s.mean_high, last.mean_high, v => temp(v, u))],
    ['cloud-rain', 'Rain', amount(s.precip, u), last.days ? `last year ${amount(last.precip, u)}` : ''],
    ['umbrella', 'Wet days', `${s.wet_days} of ${s.days}`, last.days ? `last year ${last.wet_days}` : ''],
    ['sunrise', 'Sunshine', `${Math.round(s.sunshine_h)} h`, last.days ? `last year ${Math.round(last.sunshine_h)} h` : ''],
  ]
  return (
    <div className="set-stats wx-stats">
      {tiles.map(([icon, label, v, sub]) => (
        <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{v}</span><span className="set-stat-l">{label}</span>{sub && <span className="muted small">{sub}</span>}</div>
      ))}
    </div>
  )
}

function NoteModal({ d, placeId, u, onClose, onSaved }: { d: DayRecord; placeId: number; u: Units; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [note, setNote] = useState(d.note)
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    try { await api.put(`${V1}/diary/${placeId}/${d.date}`, { note }); toast('Saved'); onSaved() } catch (err) { toast((err as Error).message, 'err') }
  }
  return (
    <Modal title={`${weekday(d.date, true)}, ${fmtDate(d.date)}`} onClose={onClose} size="sm">
      <form onSubmit={save} className="stack">
        <div className="row" style={{ gap: 10 }}>
          <WeatherIcon condition={condOf(d.code)} size={36} />
          <div><b>{temp(d.t_min, u)} to {temp(d.t_max, u)}</b><div className="muted small">{(d.precip ?? 0) >= 0.1 ? `${amount(d.precip, u)} rain` : 'dry'}{d.gust ? ` · gusts ${Math.round(d.gust)} km/h` : ''}{d.sunshine_h != null ? ` · ${d.sunshine_h} h sun` : ''}{d.aqi != null ? ` · air ${Math.round(d.aqi)}` : ''}</div></div>
        </div>
        <Field label="Your note" help="What you remember about the day's weather. Pocket Day shows it in your journal."><textarea value={note} onChange={e => setNote(e.target.value)} rows={3} maxLength={2000} autoFocus /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary">Save</button></div>
      </form>
    </Modal>
  )
}

function LookUp({ placeId, u }: { placeId: number; u: Units }) {
  const [date, setDate] = useState('')
  const [res, setRes] = useState<{ day?: Day; text?: string; source?: string; note?: string; error?: string } | null>(null)
  const look = async (v: string) => {
    setDate(v)
    if (!/^\d{4}-\d{2}-\d{2}$/.test(v)) return
    try { setRes(await api.get(`${V1}/day/${v}?location=${placeId}`)) } catch (e) { setRes({ error: (e as Error).message }) }
  }
  return (
    <div className="wx-lookup">
      <label className="row small" style={{ gap: 6 }}><Icon name="calendar" size={14} />Any day: <input type="date" value={date} onChange={e => look(e.target.value)} max={new Date(Date.now() + 9 * 864e5).toISOString().slice(0, 10)} /></label>
      {res && <span className="small">{res.error ? <span className="bad">{res.error}</span> : res.day ? <><WeatherIcon condition={res.day.condition} size={18} /> {res.day.text}, {temp(res.day.t_min, u)} to {temp(res.day.t_max, u)}{(res.day.precip ?? 0) >= 1 ? `, ${amount(res.day.precip, u)}` : ''}{res.source === 'forecast' ? ' (forecast)' : ''}</> : null}</span>}
    </div>
  )
}
