// For your plans: what to do because of the weather (plants, tyres, the
// dog, events, trips), your next days' events with their weather, and
// your coming trips — from the Pocket apps you connected.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useOnChange } from '../lib/data'
import { fmtDate, fmtRange, weekday } from '../lib/format'
import type { Advice, AppState, PlanEvent, PlanTrip, Plans } from '../lib/types'
import { deg, hourOf, temp, unitsOf, windSpeed, type Units } from '../lib/units'
import { Icon } from '../components/icons'
import { WeatherIcon } from '../components/WeatherIcon'
import { Card, CoreHint, Empty, Loading, useAsync, useToast } from '../components/ui'

const APPS: { slug: string; name: string; icon: string; what: string }[] = [
  { slug: 'calendar', name: 'Calendar', icon: 'calendar', what: 'events' },
  { slug: 'travel', name: 'Travel', icon: 'plane', what: 'trips' },
  { slug: 'home', name: 'Home', icon: 'leaf', what: 'outdoor plants' },
  { slug: 'garage', name: 'Garage', icon: 'car', what: 'vehicles' },
  { slug: 'pets', name: 'Pets', icon: 'paw', what: 'dogs' },
  { slug: 'tasks', name: 'Tasks', icon: 'check-square', what: '' },
]

export default function PlansPage() {
  const { settings } = useApp()
  const u = unitsOf(settings)
  const { data, error, reload, loading } = useAsync(() => api.get<Plans>(`${V1}/plans`), [])
  useOnChange(reload)
  const [showDone, setShowDone] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const coreOn = !!settings?.core_configured && settings?.core_registered !== false
  const open = data.advice.filter(a => !a.mark)
  const done = data.advice.filter(a => a.mark && a.mark !== 'hidden')
  const anyOn = APPS.some(a => a.slug !== 'tasks' && data.apps[a.slug]?.connected)
  const refresh = () => api.get<Plans>(`${V1}/plans?fresh=1`).then(reload).catch(() => {})
  return (
    <div className="stack loose">
      <div className="row wrap" style={{ gap: 8 }}>
        {APPS.map(a => <AppChip key={a.slug} a={a} st={data.apps[a.slug]} />)}
        <span className="grow" />
        <button type="button" className="btn ghost sm" onClick={refresh} disabled={loading}><Icon name="refresh" size={14} />Check again</button>
      </div>
      {data.demo && <div className="callout info"><Icon name="info" size={15} /><div>Demo: these events, trips, plants, the car and the dog are made up so you can see the page. With Pocket Core they come from your own Pocket apps.</div></div>}
      {!anyOn && !data.demo && (
        <div className="callout info"><Icon name="layers" size={15} /><div>Connect Pocket Calendar, Travel, Home, Garage or Pets and this page puts the weather on your events and trips, and says when to bring the balcony plants in, change the tyres or walk the dog. <Link to="/settings/apps">Settings → Connected apps</Link>{!coreOn && <CoreHint />}</div></div>
      )}

      <Card title="What to do" flush sub={open.length ? undefined : 'Nothing the weather asks of you right now.'}
        actions={done.length > 0 ? <button type="button" className="btn ghost sm" onClick={() => setShowDone(!showDone)}>{showDone ? 'Hide done' : `Done (${done.length})`}</button> : undefined}>
        {open.length > 0 && <div className="list">{open.map(a => <AdviceRow key={a.key} a={a} tasks={data.apps.tasks} today={data.events[0]?.start.slice(0, 10)} />)}</div>}
        {showDone && done.length > 0 && <div className="list faded">{done.map(a => <AdviceRow key={a.key} a={a} tasks={data.apps.tasks} />)}</div>}
      </Card>

      {data.apps.calendar?.on && (
        <Card title="Your next days" flush sub={`The weather at each event: where it is when Weather can find the place, else ${data.place}.`}>
          {data.events.length ? <div className="list">{data.events.map(e => <EventRow key={e.id + e.start} e={e} u={u} />)}</div>
            : <div className="pad"><Empty title="No events in the next three days" icon="calendar" /></div>}
        </Card>
      )}

      {data.apps.travel?.on && (
        <Card title="Your trips" flush>
          {data.trips.length ? <div className="list">{data.trips.map(t => <TripRow key={t.id} t={t} u={u} />)}</div>
            : <div className="pad"><Empty title="No trips in the next two months" icon="plane" /></div>}
        </Card>
      )}
    </div>
  )
}

function AppChip({ a, st }: { a: typeof APPS[number]; st?: AppState }) {
  const on = !!st?.on, ok = !!st?.connected
  const title = !on ? `Switched off in Settings → Connected apps` : ok ? (a.what ? `${st?.count ?? 0} ${a.what}` : 'Connected') : st?.detail || 'Not connected'
  return (
    <Link to="/settings/apps" className={`app-chip ${!on ? 'off' : ok ? 'ok' : 'bad'}`} title={title}>
      <Icon name={a.icon} size={14} />{a.name}<span className="dot" />
    </Link>
  )
}

const APP_NAME: Record<string, string> = { calendar: 'Pocket Calendar', travel: 'Pocket Travel', home: 'Pocket Home', garage: 'Pocket Garage', pets: 'Pocket Pets' }

function AdviceRow({ a, tasks }: { a: Advice; tasks?: AppState; today?: string }) {
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const mark = async (state: string) => {
    try { await api.post(`${V1}/advice/mark`, { key: a.key, state }); window.dispatchEvent(new CustomEvent('pwx:changed')) } catch (e) { toast((e as Error).message, 'err') }
  }
  const task = async () => {
    setBusy(true)
    try {
      const r = await api.post<{ ok: boolean; error?: string }>(`${V1}/advice/task`, { key: a.key })
      if (r.ok) { toast('Added to Pocket Tasks', 'ok'); window.dispatchEvent(new CustomEvent('pwx:changed')) } else toast(r.error || 'Pocket Tasks did not answer', 'err')
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const tasksOn = !!tasks?.connected
  return (
    <div className={`wx-advice ${a.level}`}>
      <span className="wx-advice-ico"><Icon name={a.icon} size={17} /></span>
      <div className="grow">
        <div className="wx-advice-title">{a.title}</div>
        <div className="muted small">{a.detail}</div>
        <div className="row wrap wx-advice-actions">
          {!a.mark && <>
            <button type="button" className="btn sm" onClick={() => mark('done')}><Icon name="check" size={14} />Done</button>
            {a.task && <button type="button" className="btn sm ghost" onClick={task} disabled={busy || !tasksOn} title={tasksOn ? 'Make it a task in Pocket Tasks' : 'Connect Pocket Tasks in Settings → Connected apps'}><Icon name="list-plus" size={14} />Add as a task</button>}
            <button type="button" className="btn sm ghost" onClick={() => mark('hidden')}>Hide</button>
          </>}
          {a.mark && <><span className="badge ok">{a.mark === 'task' ? 'in Pocket Tasks' : 'done'}</span><button type="button" className="btn sm ghost" onClick={() => mark('')}>Undo</button></>}
          {a.link && <a className="btn sm ghost" href={a.link} target="_blank" rel="noreferrer"><Icon name="external" size={13} />{APP_NAME[a.app] || 'Open'}</a>}
        </div>
      </div>
      <span className="muted small nowrap">{weekday(a.day)}</span>
    </div>
  )
}

const FLAG: Record<string, [string, string]> = { rain: ['umbrella', 'Rain'], storm: ['zap', 'Thunder'], wind: ['wind', 'Windy'], heat: ['thermometer', 'Hot'], cold: ['snowflake', 'Cold'], snow: ['snowflake', 'Snow'] }

function EventRow({ e, u }: { e: PlanEvent; u: Units }) {
  const when = e.all_day ? `${weekday(e.start)} · all day` : `${weekday(e.start.slice(0, 10))} ${hourOf(e.start)}`
  return (
    <div className={`wx-event ${e.flags.length ? 'flagged' : ''}`}>
      <WeatherIcon condition={e.condition} size={30} />
      <div className="grow">
        <div className="row-title">{e.link ? <a href={e.link} target="_blank" rel="noreferrer">{e.title}</a> : e.title}</div>
        <div className="muted small">{when} · {e.place}{e.location && !e.location.toLowerCase().includes(e.place.toLowerCase()) ? ` (${e.location})` : ''}</div>
        {e.note && <div className="small wx-event-note">{e.note}</div>}
      </div>
      <div className="wx-event-wx">
        <span className="tnum">{e.all_day ? `${deg(e.t_min, u)} / ${deg(e.t_max, u)}` : deg(e.temp, u)}</span>
        {(e.precip_prob ?? 0) >= 20 && <span className="small rain-text">{Math.round(e.precip_prob ?? 0)}%</span>}
        {e.flags.map(f => FLAG[f] && <span key={f} className="pill warn" title={f === 'wind' && e.gust ? windSpeed(e.gust, u) : undefined}><Icon name={FLAG[f][0]} size={12} />{FLAG[f][1]}</span>)}
      </div>
    </div>
  )
}

function TripRow({ t, u }: { t: PlanTrip; u: Units }) {
  const days = t.days.length ? t.days : t.last_year
  return (
    <div className="wx-trip">
      <div className="row" style={{ gap: 10 }}>
        <Icon name="plane" size={16} className="muted" />
        <div className="grow">
          <div className="row-title">{t.link ? <a href={t.link} target="_blank" rel="noreferrer">{t.name}</a> : t.name}</div>
          <div className="muted small">{t.place} · {fmtRange(t.start_date, t.end_date)}</div>
        </div>
      </div>
      {t.summary && <p className="small">{t.summary}</p>}
      {days.length > 0 && (
        <div className="wx-trip-days">
          {days.slice(0, 10).map(d => (
            <div key={d.date} className="wx-trip-day" title={d.text}>
              <span className="muted small">{t.days.length ? weekday(d.date) : fmtDate(d.date, false)}</span>
              <WeatherIcon condition={d.condition} size={26} />
              <span className="small tnum">{deg(d.t_max, u)}</span>
              <span className="small tnum muted">{deg(d.t_min, u)}</span>
            </div>
          ))}
        </div>
      )}
      {!t.days.length && t.last_year.length > 0 && <p className="muted small">Last year's weather on these dates ({temp(Math.min(...t.last_year.map(d => d.t_min ?? 99)), u)} at the coldest). The forecast starts ten days before.</p>}
    </div>
  )
}
