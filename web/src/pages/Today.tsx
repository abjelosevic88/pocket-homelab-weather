// Today: the weather now at home (or one place), its warnings, the day in
// plain words, the next 48 hours, ten days, air and pollen.
import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useData, useOnChange } from '../lib/data'
import { fmtDate, weekday } from '../lib/format'
import type { Air, Day, Forecast, Hour, Warning } from '../lib/types'
import { amount, aqiTone, compass, deg, hourOf, LEVEL_LABEL, levelRank, POLLEN, temp, unitsOf, uvWord, WARN_ICON, windSpeed, type Units } from '../lib/units'
import { openAddPlace, useCrumb } from '../components/Layout'
import { Icon } from '../components/icons'
import { WeatherIcon } from '../components/WeatherIcon'
import { Card, CoreHint, Loading, useAsync, useToast } from '../components/ui'

export default function Today() {
  const { id } = useParams()
  const { settings } = useApp()
  const { overview } = useData()
  const navigate = useNavigate()
  const u = unitsOf(settings)
  const q = id ? `?location=${id}` : ''
  const { data, error, reload } = useAsync(() => api.get<Forecast>(`${V1}/forecast${q}`), [q])
  useOnChange(reload)
  // the forecast refreshes itself every 15 minutes while the page is open
  useEffect(() => { const t = window.setInterval(() => { if (document.visibilityState === 'visible') reload() }, 15 * 60 * 1000); return () => window.clearInterval(t) }, [reload])
  useCrumb(id && data?.location ? data.location.name : undefined, id ? 'places' : undefined)

  if (overview && overview.places.length === 0 && !id) return <Welcome />
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error === 'not found' ? 'That place is not one of yours.' : error}</span></div>
  if (!data) return <Loading />
  const place = data.location
  const places = overview?.places || []
  return (
    <div className="stack loose wx-page">
      {places.length > 1 && (
        <div className="wx-chips" role="tablist" aria-label="Your places">
          {places.map(p => {
            const on = place?.id === p.id
            return (
              <button key={p.id} type="button" role="tab" aria-selected={on} className={`wx-chip ${on ? 'on' : ''}`} onClick={() => navigate(p.is_home ? '/' : `/places/${p.id}`)}>
                {p.now && <WeatherIcon condition={p.now.condition} day={p.now.is_day} size={18} />}
                <span>{p.name}</span>{p.now && <b className="tnum">{deg(p.now.temp, u)}</b>}
                {p.warnings > 0 && <Icon name="alert" size={12} className="warn-text" />}
              </button>
            )
          })}
        </div>
      )}
      <Hero f={data} u={u} />
      {data.warnings.length > 0 && <Warnings list={data.warnings} today={data.today} />}
      {place && <Brief placeId={place.id} isHome={place.is_home} />}
      <Hours hours={data.hours} days={data.days} u={u} />
      <Days days={data.days} today={data.today} u={u} />
      {data.air && <AirCard air={data.air} today={data.today} />}
      {place && <LastYear placeId={place.id} today={data.today} u={u} />}
    </div>
  )
}

function Welcome() {
  return (
    <div className="welcome card">
      <div className="welcome-body">
        <img src="/favicon.svg" alt="" width={56} height={56} />
        <div className="col">
          <h2>Where do you want the weather for?</h2>
          <p className="muted">Add your town first; it becomes home. Then the cottage, your parents' town, wherever you go often. Pocket Weather warns you about frost, storms, heavy rain and heat there, and keeps a diary of every day.</p>
          <div className="row wrap mt"><button type="button" className="btn primary" onClick={openAddPlace}><Icon name="plus" size={15} />Add a place</button></div>
        </div>
      </div>
    </div>
  )
}

function Hero({ f, u }: { f: Forecast; u: Units }) {
  const n = f.now
  const today = f.days.find(d => d.date === f.today)
  const place = f.location
  const facts: [string, string, React.ReactNode, string?][] = [
    ['wind', 'Wind', <>{windSpeed(n.wind, u)} {compass(n.wind_dir)}</>, n.gust != null && n.gust > (n.wind || 0) + 8 ? `gusts ${windSpeed(n.gust, u)}` : undefined],
    ['umbrella', 'Rain today', today?.precip_prob != null ? `${Math.round(today.precip_prob)} %` : '–', today?.precip ? amount(today.precip, u) : undefined],
    ['droplet', 'Humidity', n.humidity != null ? `${Math.round(n.humidity)} %` : '–'],
    ['sun', 'UV', today?.uv != null ? `${Math.round(today.uv)} ${uvWord(today.uv)}` : '–', n.uv != null && n.is_day ? `now ${Math.round(n.uv)}` : undefined],
    ['sunrise', 'Sunrise', today?.sunrise ? hourOf(today.sunrise) : '–', today?.daylight_h ? `${today.daylight_h} h of daylight` : undefined],
    ['sunset', 'Sunset', today?.sunset ? hourOf(today.sunset) : '–', today?.sunshine_h != null ? `${today.sunshine_h} h of sun` : undefined],
  ]
  return (
    <section className={`wx-hero card cond-${n.condition} ${n.is_day ? 'day' : 'night'}`}>
      <div className="wx-hero-main">
        <div className="wx-hero-place">
          <Icon name={place?.source === 'here' ? 'locate' : place?.is_home ? 'home' : 'pin'} size={14} />
          <span className="ellipsis">{place ? place.name : 'This spot'}{place?.country ? <span className="muted">, {place.country}</span> : null}</span>
          {f.stale && <span className="badge warn" title="Open-Meteo did not answer; this is the last forecast Weather has">older forecast</span>}
        </div>
        <div className="wx-hero-now">
          <WeatherIcon condition={n.condition} day={n.is_day} size={76} />
          <div>
            <div className="wx-hero-temp tnum">{deg(n.temp, u)}</div>
            <div className="wx-hero-text">{n.text}</div>
          </div>
        </div>
        <div className="wx-hero-sub muted">
          Feels like {deg(n.feels, u)}{today && <> · High {deg(today.t_max, u)} · Low {deg(today.t_min, u)}</>}
        </div>
      </div>
      <dl className="wx-facts">
        {facts.map(([icon, label, value, sub]) => (
          <div key={label} className="wx-fact">
            <dt><Icon name={icon} size={14} />{label}</dt>
            <dd className="tnum">{value}</dd>
            {sub && <dd className="wx-fact-sub muted small">{sub}</dd>}
          </div>
        ))}
      </dl>
    </section>
  )
}

export function WarningRow({ w, showPlace }: { w: Warning; showPlace?: boolean }) {
  const start = w.starts_at ? hourOf(w.starts_at) : ''
  const end = w.ends_at ? hourOf(w.ends_at) : ''
  return (
    <div className={`wx-warn ${w.level}`}>
      <span className="wx-warn-ico"><Icon name={WARN_ICON[w.kind] || 'alert'} size={17} /></span>
      <div className="grow">
        <div className="wx-warn-title">{w.title}{showPlace && w.place ? <span className="muted"> · {w.place}</span> : null}</div>
        <div className="muted small">{w.detail}</div>
      </div>
      <span className={`pill ${w.level === 'warning' ? 'bad' : 'warn'}`} title={start && end ? `${start}–${end}` : undefined}>
        <Icon name="alert" size={12} />{w.level === 'warning' ? 'Warning' : 'Heads-up'}
      </span>
    </div>
  )
}

// Today shows the three that matter most for today and tomorrow (warnings
// before heads-ups); the Warnings page has them all.
function Warnings({ list, today }: { list: Warning[]; today: string }) {
  const soon = list.filter(w => w.day <= nextDay(today))
  const top = [...soon].sort((a, b) => (a.level === b.level ? 0 : a.level === 'warning' ? -1 : 1) || a.day.localeCompare(b.day)).slice(0, 3)
  const more = list.length - top.length
  if (!top.length) return (
    <Link to="/warnings" className="wx-lastyear card"><Icon name="alert" size={16} className="warn-text" /><span className="grow">{list.length} {list.length === 1 ? 'warning' : 'warnings'} later this week</span><Icon name="chevron-right" size={15} className="muted" /></Link>
  )
  return (
    <Card title="Warnings" flush actions={<Link to="/warnings" className="btn ghost sm">{more > 0 ? `${more} more` : 'All'}<Icon name="chevron-right" size={14} /></Link>}>
      <div className="list">{top.map(w => <WarningRow key={w.id || w.kind + w.day} w={w} />)}</div>
    </Card>
  )
}

function nextDay(d: string): string {
  const t = new Date(d + 'T12:00:00')
  t.setDate(t.getDate() + 1)
  return `${t.getFullYear()}-${String(t.getMonth() + 1).padStart(2, '0')}-${String(t.getDate()).padStart(2, '0')}`
}

// ---- the day in plain words (Pocket AI) and a question ----

function Brief({ placeId, isHome }: { placeId: number; isHome: boolean }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const { data: ai } = useAsync(() => api.get<{ ready: boolean; provider: string; detail: string; brief: boolean }>(`${V1}/ai/status`), [])
  const [text, setText] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [question, setQuestion] = useState('')
  const [answer, setAnswer] = useState<{ q: string; a: string } | null>(null)
  const [asking, setAsking] = useState(false)
  const coreOn = !!settings?.core_configured && settings?.core_registered !== false
  const load = async (fresh = false) => {
    setBusy(true); setErr('')
    try {
      const r = await api.post<{ ok: boolean; text?: string; error?: string }>(`${V1}/ai/brief`, { location_id: placeId, fresh })
      if (r.ok) setText(r.text || ''); else setErr(r.error || 'The AI did not answer')
    } catch (e) { setErr((e as Error).message) } finally { setBusy(false) }
  }
  useEffect(() => { setText(''); setAnswer(null); if (ai?.ready && ai.brief) load() }, [ai?.ready, ai?.brief, placeId]) // eslint-disable-line react-hooks/exhaustive-deps
  const ask = async (e: React.FormEvent) => {
    e.preventDefault()
    const q = question.trim()
    if (q.length < 3) return
    setAsking(true)
    try {
      const r = await api.post<{ ok: boolean; answer?: string; error?: string }>(`${V1}/ai/ask`, { question: q, location_id: placeId })
      if (r.ok) { setAnswer({ q, a: r.answer || '' }); setQuestion('') } else toast(r.error || 'The AI did not answer', 'err')
    } catch (e) { toast((e as Error).message, 'err') } finally { setAsking(false) }
  }
  if (!ai) return null
  if (!ai.ready) {
    return (
      <Card title={<span className="row" style={{ gap: 6 }}><Icon name="sparkles" size={15} />In plain words</span>}>
        <p className="muted small">Pocket AI can explain the day in a few sentences — what to wear, whether you need an umbrella — and answer questions like “can I hang the washing out tomorrow?”.{' '}
          {coreOn ? <>{ai.detail ? `${ai.detail}. ` : ''}<Link to="/settings/ai">Settings → AI</Link></> : <CoreHint />}</p>
      </Card>
    )
  }
  const lines = text.split('\n').map(l => l.trim()).filter(Boolean)
  const wear = lines.find(l => /^wear:/i.test(l))
  const body = lines.filter(l => l !== wear)
  return (
    <Card title={<span className="row" style={{ gap: 6 }}><Icon name="sparkles" size={15} />In plain words</span>}
      actions={ai.brief || text ? <button type="button" className="btn ghost sm icon-only" onClick={() => load(true)} disabled={busy} title="Write it again" aria-label="Write it again"><Icon name="refresh" size={14} /></button> : undefined}>
      <div className="stack">
        {text ? <div className="wx-brief">{body.map((l, i) => <p key={i}>{l}</p>)}{wear && <p className="wx-wear"><Icon name="user" size={14} />{wear.replace(/^wear:\s*/i, '')}</p>}</div>
          : busy ? <p className="muted small">Pocket AI is reading the forecast{isHome ? ' and your plans' : ''}…</p>
          : err ? <p className="bad small">{err}</p>
          : <button type="button" className="btn sm" onClick={() => load()}><Icon name="sparkles" size={14} />Explain the day</button>}
        <form className="wx-ask" onSubmit={ask}>
          <input value={question} onChange={e => setQuestion(e.target.value)} placeholder="Ask: can I hang the washing out tomorrow?" aria-label="Ask about the weather" maxLength={1000} />
          <button className="btn sm" disabled={asking || question.trim().length < 3}>{asking ? 'Asking…' : 'Ask'}</button>
        </form>
        {answer && <div className="wx-answer"><div className="muted small">{answer.q}</div><p>{answer.a}</p></div>}
      </div>
    </Card>
  )
}

// ---- 48 hours ----

function Hours({ hours, days, u }: { hours: Hour[]; days: Day[]; u: Units }) {
  const ref = useRef<HTMLDivElement>(null)
  const temps = hours.map(h => h.temp ?? 0)
  const lo = Math.min(...temps), hi = Math.max(...temps)
  const suns = useMemo(() => {
    const m = new Map<string, string>()
    for (const d of days) { if (d.sunrise) m.set(d.sunrise.slice(0, 13), 'sunrise'); if (d.sunset) m.set(d.sunset.slice(0, 13), 'sunset') }
    return m
  }, [days])
  const wet = hours.some(h => (h.precip ?? 0) > 0)
  const scroll = (dir: number) => ref.current?.scrollBy({ left: dir * 360, behavior: 'smooth' })
  return (
    <Card title="Next 48 hours" flush actions={<span className="row hide-mobile"><button type="button" className="btn ghost sm icon-only" aria-label="Earlier" onClick={() => scroll(-1)}><Icon name="chevron-left" size={15} /></button><button type="button" className="btn ghost sm icon-only" aria-label="Later" onClick={() => scroll(1)}><Icon name="chevron-right" size={15} /></button></span>}>
      <div className="wx-hours" ref={ref}>
        {hours.map((h, i) => {
          const newDay = i > 0 && h.time.slice(11, 13) === '00'
          const top = hi > lo ? ((hi - (h.temp ?? 0)) / (hi - lo)) * 26 : 13
          const p = h.precip_prob ?? 0
          const sun = suns.get(h.time.slice(0, 13))
          return (
            <div key={h.time} className={`wx-hour ${newDay ? 'new-day' : ''}`}>
              <div className="wx-hour-time">{i === 0 ? 'Now' : newDay ? weekday(h.time.slice(0, 10)) : hourOf(h.time)}</div>
              <WeatherIcon condition={h.condition} day={h.is_day} size={28} />
              <div className="wx-hour-track"><span className="wx-hour-temp tnum" style={{ marginTop: top }}>{deg(h.temp, u)}</span></div>
              <div className={`wx-hour-rain ${p >= 50 ? 'likely' : ''}`}>{p >= 20 ? `${Math.round(p)}%` : ''}</div>
              {wet && <div className="wx-hour-bar"><span style={{ height: `${Math.min(100, (h.precip ?? 0) * 25)}%` }} /></div>}
              <div className="wx-hour-wind muted">{(h.gust ?? 0) >= 40 ? windSpeed(h.gust, u) : sun ? <Icon name={sun} size={13} /> : ''}</div>
            </div>
          )
        })}
      </div>
    </Card>
  )
}

// ---- ten days ----

function Days({ days, today, u }: { days: Day[]; today: string; u: Units }) {
  const list = days.filter(d => d.date >= today)
  const lo = Math.min(...list.map(d => d.t_min ?? 99)), hi = Math.max(...list.map(d => d.t_max ?? -99))
  const [open, setOpen] = useState<string | null>(null)
  const span = hi - lo || 1
  return (
    <Card title="Ten days" flush>
      <div className="list wx-days">
        {list.map(d => {
          const left = (((d.t_min ?? lo) - lo) / span) * 100
          const width = Math.max(4, (((d.t_max ?? hi) - (d.t_min ?? lo)) / span) * 100)
          const isOpen = open === d.date
          return (
            <div key={d.date} className={`wx-day ${isOpen ? 'open' : ''}`}>
              <button type="button" className="wx-day-row" aria-expanded={isOpen} onClick={() => setOpen(isOpen ? null : d.date)}>
                <span className="wx-day-name">{d.date === today ? 'Today' : weekday(d.date)}<span className="muted small hide-mobile"> {fmtDate(d.date, false)}</span></span>
                <WeatherIcon condition={d.condition} size={28} title={d.text} />
                <span className={`wx-day-rain small ${(d.precip_prob ?? 0) >= 50 ? 'likely' : ''}`}>{(d.precip_prob ?? 0) >= 20 ? `${Math.round(d.precip_prob ?? 0)}%` : ''}</span>
                <span className="wx-day-lo tnum muted">{deg(d.t_min, u)}</span>
                <span className="wx-range" aria-hidden><span style={{ left: `${left}%`, width: `${width}%` }} /></span>
                <span className="wx-day-hi tnum">{deg(d.t_max, u)}</span>
              </button>
              {isOpen && (
                <dl className="wx-day-more">
                  <div><dt>Weather</dt><dd>{d.text}</dd></div>
                  <div><dt>Rain</dt><dd>{amount(d.precip, u)}{d.snow ? ` · ${Math.round(d.snow)} cm snow` : ''}{d.precip_hours ? ` · ${Math.round(d.precip_hours)} h` : ''}</dd></div>
                  <div><dt>Wind</dt><dd>{windSpeed(d.wind, u)} {compass(d.wind_dir)} · gusts {windSpeed(d.gust, u)}</dd></div>
                  <div><dt>Feels like</dt><dd>{deg(d.feels_min, u)} to {deg(d.feels_max, u)}</dd></div>
                  <div><dt>UV</dt><dd>{d.uv != null ? `${Math.round(d.uv)} ${uvWord(d.uv)}` : '–'}</dd></div>
                  <div><dt>Sun</dt><dd>{d.sunrise ? hourOf(d.sunrise) : '–'}–{d.sunset ? hourOf(d.sunset) : '–'}{d.sunshine_h != null ? ` · ${d.sunshine_h} h of sun` : ''}</dd></div>
                </dl>
              )}
            </div>
          )
        })}
      </div>
    </Card>
  )
}

// ---- air and pollen ----

function AirCard({ air, today }: { air: Air; today: string }) {
  const days = air.days.filter(d => d.date >= today)
  const any = POLLEN.filter(k => days.some(d => (d.pollen[k.key] ?? 0) >= 1))
  return (
    <Card title="Air and pollen" sub={any.length === 0 ? 'No pollen in the air now (pollen is known in Europe only).' : undefined}>
      <div className="wx-air">
        <div className={`wx-aqi ${aqiTone(air.band)}`}>
          <span className="wx-aqi-n tnum">{air.aqi != null ? Math.round(air.aqi) : '–'}</span>
          <span><b>{air.band ? air.band[0].toUpperCase() + air.band.slice(1) : '–'}</b><span className="muted small block-line">European air quality index now{air.pm2_5 != null ? ` · fine dust ${Math.round(air.pm2_5)} µg/m³` : ''}</span></span>
        </div>
        <div className="wx-air-days">
          {days.map(d => (
            <div key={d.date} className="wx-air-day">
              <span className="muted small">{d.date === today ? 'Today' : weekday(d.date)}</span>
              <span className={`pill ${aqiTone(d.band)}`}>{d.band || '–'}</span>
            </div>
          ))}
        </div>
      </div>
      {any.length > 0 && (
        <table className="table compact wx-pollen mt">
          <thead><tr><th>Pollen</th>{days.map(d => <th key={d.date}>{d.date === today ? 'Today' : weekday(d.date)}</th>)}</tr></thead>
          <tbody>{any.map(k => (
            <tr key={k.key}><td>{k.label}</td>{days.map(d => { const l = d.levels[k.key] || 'none'; return <td key={d.date}><span className={`pollen-lvl l${levelRank(l)}`}>{LEVEL_LABEL[l]}</span></td> })}</tr>
          ))}</tbody>
        </table>
      )}
    </Card>
  )
}

// ---- this day last year ----

function LastYear({ placeId, today, u }: { placeId: number; today: string; u: Units }) {
  const date = `${Number(today.slice(0, 4)) - 1}${today.slice(4)}`
  const { data } = useAsync(() => api.get<{ day: Day; source: string; note?: string }>(`${V1}/day/${date}?location=${placeId}`).catch(() => null), [placeId, date])
  if (!data?.day) return null
  const d = data.day
  return (
    <Link to="/diary" className="wx-lastyear card">
      <WeatherIcon condition={d.condition} size={30} />
      <span className="grow"><span className="muted small block-line">This day last year</span><span>{d.text}, {temp(d.t_min, u)} to {temp(d.t_max, u)}{(d.precip ?? 0) >= 1 ? `, ${amount(d.precip, u)}` : ''}{data.note ? ` — “${data.note}”` : ''}</span></span>
      <Icon name="chevron-right" size={15} className="muted" />
    </Link>
  )
}
