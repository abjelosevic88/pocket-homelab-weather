import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { sendFile } from '../lib/api'
import { announceChange } from '../lib/data'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, AppSlug, ConnMode, PollenKind, Settings as SettingsT, User } from '../lib/types'
import { POLLEN } from '../lib/units'
import { DATE_FORMATS, TIME_FORMATS, fmtBytes, fmtDateTime, type DateFormat, type TimeFormat } from '../lib/format'
import { Avatar, Confirm, CoreHint, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { PocketModelSelect } from '../components/PocketModelSelect'

type TabId = 'general' | 'warnings' | 'apps' | 'ai' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'General', icon: 'home', sub: 'Units, where you are now, place names, dates and the app title.' },
  { id: 'warnings', label: 'Warnings', icon: 'bell', sub: 'What counts as frost, a storm, heavy rain or heat for you, and where the morning message and warnings go.' },
  { id: 'apps', label: 'Connected apps', icon: 'layers', sub: 'Pocket Calendar, Travel, Home, Garage, Pets, Movement and Tasks: the weather for your plans.' },
  { id: 'ai', label: 'AI', icon: 'sparkles', sub: 'The day in plain words and questions about the forecast: Pocket AI through Core, or your own OpenAI key.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Personal tokens for Homepage widgets, Siri Shortcuts and scripts.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Export everything, import an export, download the database.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const tab: TabId = TABS.find(t => t.id === raw)?.id || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])
  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back" title="Back (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title"><h2>{current.label}</h2><p>{current.sub}</p></div>
            {tab === 'general' && <General />}
            {tab === 'warnings' && <WarningsTab />}
            {tab === 'apps' && <Integrations />}
            {tab === 'ai' && <AITab />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (patch?: Partial<SettingsT>) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

function useTester() {
  const [test, setTest] = useState<Record<string, string>>({})
  const run = async (which: string, url: string, key: string, extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  return { run, status }
}

const HOURS = Array.from({ length: 24 }, (_, h) => ({ value: String(h), label: `${String(h).padStart(2, '0')}:00` }))

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  return (
    <div className="stack loose">
      <Section title="Units" icon="thermometer" sub="The forecast arrives in °C, mm and km/h; this is how you see it.">
        <SettingRow title="Temperature"><Segmented size="sm" value={f.temp_unit} onChange={v => setF({ ...f, temp_unit: v })} options={[{ value: 'c', label: '°C' }, { value: 'f', label: '°F' }]} /></SettingRow>
        <SettingRow title="Wind"><Segmented size="sm" value={f.wind_unit} onChange={v => setF({ ...f, wind_unit: v })} options={[{ value: 'kmh', label: 'km/h' }, { value: 'ms', label: 'm/s' }, { value: 'mph', label: 'mph' }, { value: 'kn', label: 'knots' }, { value: 'bft', label: 'Beaufort' }]} /></SettingRow>
        <SettingRow title="Rain and snow"><Segmented size="sm" value={f.precip_unit} onChange={v => setF({ ...f, precip_unit: v })} options={[{ value: 'mm', label: 'mm' }, { value: 'in', label: 'inches' }]} /></SettingRow>
      </Section>
      <Section title="Places" icon="pin">
        <SettingRow title="Show where I am now" help={<>From Pocket Movement: when you are more than 25 km from your places, a “where you are” place appears with its weather and warnings. Only your latest position is read.{!coreOn && <CoreHint />}</>}>
          <Switch checked={f.follow_me && coreOn} disabled={!coreOn && f.movement_mode !== 'direct'} onChange={v => setF({ ...f, follow_me: v })} label="Where I am" />
        </SettingRow>
        <SettingRow title="Place names in" help="A two-letter language code for the search (en, bs, de, hr…). Empty is English."><input value={f.language} onChange={e => setF({ ...f, language: e.target.value.toLowerCase() })} maxLength={2} className="set-time" placeholder="en" /></SettingRow>
      </Section>
      <Section title="Dates" icon="calendar">
        <SettingRow title="Week starts on"><Segmented size="sm" value={f.week_start} onChange={v => setF({ ...f, week_start: v })} options={[{ value: 1, label: 'Monday' }, { value: 6, label: 'Saturday' }, { value: 7, label: 'Sunday' }]} /></SettingRow>
        <SettingRow title="Date format"><Select ariaLabel="Date format" value={f.date_format} onChange={v => setF({ ...f, date_format: v as DateFormat })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Time format"><Select ariaLabel="Time format" value={f.time_format} onChange={v => setF({ ...f, time_format: v as TimeFormat })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        {isAdmin && <SettingRow title="App title" help="Shown in the sidebar for everyone on this box."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} /></SettingRow>}
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

const LEVELS = [{ value: 'moderate', label: 'Moderate and up' }, { value: 'high', label: 'High and up' }, { value: 'very_high', label: 'Only very high' }]

function WarningsTab() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const { run, status } = useTester()
  if (!f) return <Loading />
  const fC = f.temp_unit === 'f'
  // limits are stored in °C, mm, km/h; shown in the person's units
  const tIn = (c: number) => fC ? Math.round(c * 9 / 5 + 32) : c
  const tOut = (v: number) => fC ? Math.round(((v - 32) * 5 / 9) * 10) / 10 : v
  const num = (k: keyof SettingsT, v: number) => setF({ ...f, [k]: v } as SettingsT)
  const rule = (on: keyof SettingsT, title: string, help: string, control?: React.ReactNode) => (
    <SettingRow title={title} help={help}>
      <span className="row" style={{ gap: 10 }}>{f[on] && control}<Switch checked={!!f[on]} onChange={v => setF({ ...f, [on]: v } as SettingsT)} label={title} /></span>
    </SettingRow>
  )
  const input = (k: keyof SettingsT, unit: string, conv?: [(c: number) => number, (v: number) => number], step = 1) => (
    <span className="row small" style={{ gap: 4 }}><input type="number" step={step} className="set-time" value={conv ? conv[0](Number(f[k])) : Number(f[k])} onChange={e => num(k, conv ? conv[1](Number(e.target.value)) : Number(e.target.value))} />{unit}</span>
  )
  const deg = fC ? '°F' : '°C'
  const togglePollen = (k: PollenKind) => setF({ ...f, pollen_kinds: f.pollen_kinds.includes(k) ? f.pollen_kinds.filter(x => x !== k) : [...f.pollen_kinds, k] })
  const now = async () => { try { const r = await api.post<{ sent: boolean; body: string }>(`${V1}/notify/check`); toast(r.sent ? 'Sent' : r.body ? `Nothing sent (no push URL): ${r.body.split('\n')[0]}` : 'Add a place first') } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>These are Pocket Weather's own warnings, worked out from the forecast for today and the next two days — not the official warnings of the weather service. A “warning” instead of a “heads-up” means the forecast goes well past your limit.</div></div>
      <Section title="What counts" icon="alert">
        {rule('warn_frost', 'Frost and black ice', 'A morning at or below this, and freezing rain.', input('frost_c', deg, [tIn, tOut]))}
        {rule('warn_heat', 'Heat', 'A day at or above this.', input('heat_c', deg, [tIn, tOut]))}
        {rule('warn_wind', 'Strong wind', 'Gusts at or above this (in km/h).', input('gust_kmh', 'km/h'))}
        {rule('warn_rain', 'Heavy rain', 'This much rain in one day (in mm).', input('rain_mm', 'mm'))}
        {rule('warn_snow', 'Snow', 'This much snow in one day.', input('snow_cm', 'cm'))}
        {rule('warn_storm', 'Thunderstorms', 'Any thunderstorm; with hail it is a warning.')}
        {rule('warn_uv', 'Strong sun', 'UV index at or above this.', input('uv_max', 'UV'))}
        {rule('warn_air', 'Poor air', 'European air quality index at or above this (60 moderate, 80 poor, 100 very poor).', input('aqi_max', 'AQI'))}
        {rule('warn_pollen', 'Pollen', 'Europe only. Pick what you react to.', <Select ariaLabel="Pollen level" value={f.pollen_level} onChange={v => setF({ ...f, pollen_level: v as SettingsT['pollen_level'] })} options={LEVELS} />)}
        {f.warn_pollen && <div className="chips mt">{POLLEN.map(k => <button key={k.key} type="button" className={`chip ${f.pollen_kinds.includes(k.key as PollenKind) ? 'on' : ''}`} aria-pressed={f.pollen_kinds.includes(k.key as PollenKind)} onClick={() => togglePollen(k.key as PollenKind)}>{k.label}</button>)}</div>}
      </Section>
      <Section title="Push" icon="bell" actions={<span className={`badge ${f.notify_configured ? 'ok' : ''}`}>{f.notify_configured ? 'on' : 'not set'}</span>}
        sub={<>Where messages go: an Apprise gateway (<code>…/notify/&lt;key&gt;</code>), an ntfy topic URL, or any webhook that takes JSON.</>}>
        <SettingRow title="URL" stack><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://apprise:8000/notify/weather" /></SettingRow>
        <SettingRow title="Format"><Segmented value={f.notify_format} onChange={v => setF({ ...f, notify_format: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'json', label: 'JSON' }]} size="sm" /></SettingRow>
        <SettingRow title="The morning message" help="The day ahead at home, today's warnings and what to do, in one message."><Switch checked={f.notify_morning} onChange={v => setF({ ...f, notify_morning: v })} label="Morning message" /></SettingRow>
        {f.notify_morning && <SettingRow title="At" help="Server time."><Select ariaLabel="Hour" value={String(f.notify_hour)} onChange={v => setF({ ...f, notify_hour: Number(v) })} options={HOURS} /></SettingRow>}
        <SettingRow title="New warnings right away" help="When the forecast shows a new warning (checked every 30 minutes), or one gets worse."><Switch checked={f.notify_warnings} onChange={v => setF({ ...f, notify_warnings: v })} label="New warnings" /></SettingRow>
        {f.notify_warnings && <SettingRow title="Quiet at night" help="Between 22:00 and 07:00 only real warnings come through; heads-ups wait for the morning message."><Switch checked={f.quiet_hours} onChange={v => setF({ ...f, quiet_hours: v })} label="Quiet at night" /></SettingRow>}
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        <button type="button" className="btn" onClick={() => run('notify', f.notify_url, '', { format: f.notify_format })} disabled={!f.notify_url}><Icon name="mail" size={15} />Send a test</button>
        <button type="button" className="btn" onClick={now}><Icon name="refresh" size={15} />Send the morning message now</button>
        {status('notify')}
      </div>
    </div>
  )
}

const APPS: { slug: AppSlug; name: string; icon: string; title: string; sub: string; token: string; port: string; flag?: keyof SettingsT; flagTitle?: string }[] = [
  { slug: 'calendar', name: 'Pocket Calendar', icon: 'calendar', title: 'Pocket Calendar — the weather for your events', sub: 'Your events of the next three days with the weather at their time and place (or at home), and a note when it will rain, storm or be very hot then. Only read, nothing is changed.', token: 'Pocket Calendar → Settings → API tokens', port: '8190', flag: 'calendar_weather', flagTitle: 'Show the weather for my events' },
  { slug: 'travel', name: 'Pocket Travel', icon: 'plane', title: 'Pocket Travel — the forecast for your trips', sub: 'The forecast at the destination of your trips in the next two months, what to pack, and last year\'s weather there while the trip is too far ahead for a forecast.', token: 'Pocket Travel → Settings → API tokens', port: '8186', flag: 'travel_weather', flagTitle: 'Show the forecast for my trips' },
  { slug: 'home', name: 'Pocket Home', icon: 'leaf', title: 'Pocket Home — balcony and garden plants', sub: 'Plants whose room or spot says balcony, terrace, garden or outside: bring them in before a frost night, water them in the heat, secure the pots before a storm.', token: 'Pocket Home → Settings → API tokens', port: '8229', flag: 'home_plants', flagTitle: 'Advice for my outdoor plants' },
  { slug: 'garage', name: 'Pocket Garage', icon: 'car', title: 'Pocket Garage — tyres and the windscreen', sub: 'When the coming week turns cold (or warm in spring), time for winter or summer tyres, unless a tyre change is already in your records; a frozen windscreen or snow the next morning.', token: 'Pocket Garage → Settings → API tokens', port: '8175', flag: 'garage_tyres', flagTitle: 'Advice for my vehicles' },
  { slug: 'pets', name: 'Pocket Pets', icon: 'paw', title: 'Pocket Pets — walking the dog', sub: 'The best hour for a walk today, early and late walks in the heat, short ones in the cold, and staying in when thunder is coming.', token: 'Pocket Pets → Settings → API tokens', port: '8232', flag: 'pets_walks', flagTitle: 'Advice for my dogs' },
  { slug: 'league', name: 'Pocket League', icon: 'trophy', title: 'Pocket League — the weather for your games', sub: 'Your coming games of the next week with the weather at the venue (else at home) at their time, and a warning when it will rain, storm, blow or be very hot at an outdoor game. Games in a hall get the weather without warnings. Only read, nothing is changed.', token: 'Pocket League → Settings → API & shortcuts', port: '8250', flag: 'league_weather', flagTitle: 'Show the weather for my games' },
  { slug: 'trails', name: 'Pocket Trails', icon: 'mountain', title: 'Pocket Trails — the weather for your outings', sub: 'Your planned hikes and rides of the next week with the weather where the route starts (else at home), from setting off to when you plan to be back, and a warning when it will storm, rain, blow or be very hot or cold on the way. Only read, nothing is changed.', token: 'Pocket Trails → Settings → API & shortcuts', port: '8254', flag: 'trails_weather', flagTitle: 'Show the weather for my outings' },
  { slug: 'movement', name: 'Pocket Movement', icon: 'locate', title: 'Pocket Movement — where you are now', sub: 'Your latest position, so Weather shows the weather where you are when you are away from your places (see General → Show where I am now). Nothing else of your timeline is read.', token: 'Pocket Movement → Settings → Devices', port: '8197' },
  { slug: 'tasks', name: 'Pocket Tasks', icon: 'check-square', title: 'Pocket Tasks — advice as a task', sub: '“Add as a task” on a piece of advice (winter tyres, bring the plants in) makes it a task with a date, tagged weather. Weather only adds tasks.', token: 'Pocket Tasks → Settings → API tokens', port: '8184' },
]

function Integrations() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const eff = (m: ConnMode): ConnMode => !coreOn && m === 'core' ? 'off' : m
  const set = (k: string, v: string) => setF({ ...f, [k]: v } as SettingsT)
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>These connections are yours. Through <b>Pocket Core</b> Weather acts as you in your own Pocket apps with nothing to paste (switch it off for yourself in Pocket Core → Settings → Apps acting for me); <b>Direct</b> uses a URL and an API token you enter.{!coreOn && <CoreHint />}</div></div>
      {APPS.map(a => {
        const mode = f[`${a.slug}_mode`]
        const via = f[`${a.slug}_via`]
        return (
          <Section key={a.slug} title={a.title} icon={a.icon} actions={connBadge(eff(mode), via)} sub={a.sub}>
            <Connection app={a.name} mode={mode} via={via} detail={f[`${a.slug}_detail`]} core={coreOn} hasToken={!!f[`${a.slug}_token`]}
              onMode={m => save({ [`${a.slug}_mode`]: m } as Partial<SettingsT>)} onDisconnect={() => save({ [`${a.slug}_url`]: '', [`${a.slug}_token`]: '' } as Partial<SettingsT>)}
              test={<><button type="button" className="btn sm" onClick={() => run(a.slug, f[`${a.slug}_url`], f[`${a.slug}_token`])}>Test connection</button>{status(a.slug)}</>}
              direct={<div className="two-col tight">
                <Field label="URL" help="Inside Docker use http://host.docker.internal:PORT or the service name."><input value={f[`${a.slug}_url`]} onChange={e => set(`${a.slug}_url`, e.target.value)} placeholder={`http://host.docker.internal:${a.port}`} /></Field>
                <Field label="API token" help={a.token}><input value={f[`${a.slug}_token`]} onChange={e => set(`${a.slug}_token`, e.target.value)} autoComplete="off" /></Field>
              </div>} />
            {a.flag && eff(mode) !== 'off' && <div className="mt">
              <SettingRow title={a.flagTitle}><Switch checked={!!f[a.flag]} disabled={!via} onChange={v => save({ [a.flag as string]: v } as Partial<SettingsT>)} label={a.flagTitle} /></SettingRow>
            </div>}
          </Section>
        )
      })}
      <Section title="Other Pocket apps" icon="layers" sub="Weather offers your weather to the rest of the box through Pocket Core: Pocket Day writes each day's weather (and your diary notes) into your journal, Pocket AI shows it in your briefing and answers “will it rain on Saturday?”. Switched off until you allow them in Pocket Core → Links.">
        <dl className="set-dl">
          <div><dt>weather:read</dt><dd className="muted">The weather for your places and any spot, warnings and the diary. Never your events, games, outings, plants, car or pets.</dd></div>
          <div><dt>weather:write</dt><dd className="muted">Add a place to follow (a trip's city); only what that app added can be changed by it.</dd></div>
        </dl>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function AITab() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const { data: ai } = useAsync(() => api.get<{ ready: boolean; provider: string; detail: string }>(`${V1}/ai/status`), [])
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const shown = !coreOn && f.ai_provider === 'pocket' ? 'openai' : (f.ai_provider || 'auto')
  return (
    <div className="stack loose">
      <Section title="What the AI does" icon="sparkles" sub="On the Today page the AI writes the day in plain words — what it will be like, what to wear, whether you need an umbrella — and answers questions about the forecast. It gets the forecast, your warnings and, at home, the plans from your connected apps (event titles, trips); nothing else.">
        <SettingRow title="Write the day in plain words" help="By itself when you open Today (kept three hours). Off: only when you press the button."><Switch checked={f.ai_brief} onChange={v => setF({ ...f, ai_brief: v })} label="The day in plain words" /></SettingRow>
        {ai && (ai.ready ? <p className="muted small row" style={{ gap: 6 }}><Icon name="sparkles" size={13} />{ai.provider === 'pocket' ? 'Pocket AI' : 'Your OpenAI key'} is ready.</p>
          : <div className="callout info"><Icon name="info" size={15} /><div>No AI connected{ai.detail ? ` (${ai.detail})` : ''}.{!coreOn && <CoreHint />}</div></div>)}
      </Section>
      <Section title="Provider" icon="sparkles">
        <div className="grid" style={{ gap: 12 }}>
          <Field group label="Who writes" help="Automatic prefers your enabled Pocket AI connection; without one it uses your OpenAI settings.">
            <Select ariaLabel="AI provider" value={shown} onChange={v => setF({ ...f, ai_provider: v as SettingsT['ai_provider'] })} options={[{ value: 'auto', label: coreOn ? 'Automatic — prefer Pocket AI' : 'Automatic' }, { value: 'pocket', label: 'Pocket AI', disabled: !coreOn, hint: coreOn ? undefined : 'needs Pocket Core' }, { value: 'openai', label: 'OpenAI / compatible API' }]} />
          </Field>
          {!coreOn && <CoreHint />}
          {coreOn && shown !== 'openai' && <PocketModelSelect value={f.ai_pocket_model || ''} onChange={v => setF({ ...f, ai_pocket_model: v })} />}
          {(shown === 'openai' || (!coreOn && shown === 'auto')) && <>
            <Field label="OpenAI API key" help="platform.openai.com → API keys. Stored on the server; shown masked."><input value={f.openai_api_key} onChange={e => setF({ ...f, openai_api_key: e.target.value })} autoComplete="off" placeholder="sk-…" /></Field>
            <div className="two-col tight">
              <Field label="Model" help="Default gpt-4o-mini."><input value={f.openai_model} onChange={e => setF({ ...f, openai_model: e.target.value })} placeholder="gpt-4o-mini" /></Field>
              <Field label="Base URL (optional)" help="Ollama, LM Studio, OpenRouter…"><input value={f.openai_base_url} onChange={e => setF({ ...f, openai_base_url: e.target.value })} placeholder="https://api.openai.com/v1" /></Field>
            </div>
          </>}
        </div>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        {(shown === 'openai' || (!coreOn && shown === 'auto')) && <><button type="button" className="btn" onClick={() => run('openai', f.openai_base_url, f.openai_api_key)} disabled={!f.openai_api_key}>Test the OpenAI key</button>{status('openai')}</>}
      </div>
    </div>
  )
}

function Connection({ app, mode: saved, via, detail, core, onMode, onDisconnect, hasToken, test, direct }: {
  app: string; mode: ConnMode; via: string; detail: string; core: boolean; onMode: (m: ConnMode) => void; onDisconnect: () => void
  hasToken: boolean; test: React.ReactNode; direct: React.ReactNode
}) {
  const [advanced, setAdvanced] = useState(saved === 'direct')
  const mode: ConnMode = !core && saved === 'core' ? 'off' : saved
  const ok = via !== '' && mode !== 'off'
  const showDirect = !core || mode === 'direct' || advanced
  const line = mode === 'off' ? `Off: Weather does not talk to ${app}.`
    : mode === 'core' ? (ok ? `Connected through Pocket Core, as you${detail ? ` (${detail})` : ''}. Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.` : `Not connected: ${detail}.`)
    : (ok ? `Direct connection with your own ${app} URL and token; use Test connection to check it.` : `Not connected: ${detail || 'add the URL and a token'}.`)
  return (
    <div className="conn-block">
      <div className="conn-mode">
        <Segmented size="sm" value={mode} onChange={m => onMode(m as ConnMode)} options={[
          { value: 'core', label: 'Pocket Core', title: core ? 'Through Pocket Core: per person, nothing to paste' : 'Needs Pocket Core: there is none on this box', disabled: !core },
          ...(showDirect ? [{ value: 'direct', label: 'Direct', title: 'Your own URL and API token' }] : []),
          { value: 'off', label: 'Off' },
        ]} />
        {!showDirect && <button type="button" className="conn-advanced" onClick={() => setAdvanced(true)}>Advanced: connect directly with a URL and key</button>}
      </div>
      <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
      {mode === 'direct' && <>
        {direct}
        <div className="row wrap">{test}{hasToken && <button type="button" className="btn sm ghost danger-text" onClick={onDisconnect} title="Remove the saved URL and token">Disconnect</button>}</div>
      </>}
      {mode === 'core' && <div className="row wrap">{test}</div>}
    </div>
  )
}

const connBadge = (mode: ConnMode, via: string) => mode === 'off' ? <span className="badge">off</span>
  : via ? <span className="badge ok">{via === 'core' ? 'Pocket Core' : 'direct'}</span>
  : <span className="badge warn">not connected</span>

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and has their own places, warnings, diary and connections. Admins manage accounts; they do not see anyone else’s places.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with their places, diary and warnings." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You">
        <div className="stack">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const base = settings?.base_url || window.location.origin
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read and change everything, feed a Homepage widget (<code>GET /api/v1/summary</code>) or ask the forecast from a Shortcut. See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Homepage)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Endpoints" icon="code" sub={<>At <code>{base}/api/v1</code>.</>}>
        <dl className="set-dl">
          <div><dt><code>GET /summary</code></dt><dd className="muted">Home now, today, tomorrow and the warnings with a one-line text — for Homepage or a morning Shortcut.</dd></div>
          <div><dt><code>GET /forecast?lat=43.85&amp;lon=18.36</code></dt><dd className="muted">The full forecast of any spot (or <code>?location=ID</code>): now, 48 hours, ten days, air and pollen.</dd></div>
          <div><dt><code>GET /warnings</code></dt><dd className="muted">The active warnings at your places.</dd></div>
          <div><dt><code>GET /day/YYYY-MM-DD</code></dt><dd className="muted">What the weather was or will be on a date — what Pocket Day reads.</dd></div>
          <div><dt><code>GET /diary?month=YYYY-MM</code></dt><dd className="muted">A month of the diary at home with the month's numbers.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const [busy, setBusy] = useState(false)
  const importFile = async (file: File) => {
    setBusy(true)
    try {
      const r = await sendFile<Record<string, number>>('POST', `${V1}/import`, file, 'application/json')
      toast(`Imported: ${r.places} places (${r.already} already there), ${r.days} diary days`, 'ok'); announceChange()
    } catch (e) { toast(`${file.name}: ${(e as Error).message}`, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack loose">
      <Section title="Export" icon="archive" sub="Everything of yours as one JSON file (places, the diary with your notes, warnings, what you did with advice), or the diary as a CSV that opens in a spreadsheet.">
        <div className="row wrap"><a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Export JSON</a><a className="btn" href={`${V1}/export.csv`}><Icon name="download" size={15} />Diary as CSV</a></div>
      </Section>
      <Section title="Import" icon="upload" sub="A Pocket Weather export (from this box or another). Places already here are matched by name and spot, so importing twice adds nothing; diary days are filled in.">
        <label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Choose a .json file…'}<input type="file" accept=".json,application/json" hidden onChange={e => { const f = e.target.files?.[0]; if (f) importFile(f); e.target.value = '' }} /></label>
      </Section>
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Restore by stopping the app and replacing <code>pocket-weather.db</code> in the data directory. Forecasts are not backed up; they are fetched again.</p>
        <a className="btn" href={`${V1}/backup`}><Icon name="database" size={15} />Download database</a>
      </Section>}
    </div>
  )
}

function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const core = (data.core || {}) as { configured?: boolean; registered?: boolean; url?: string; error?: string }
  const tiles: [string, number | undefined, string][] = [['Places', counts.locations, 'pin'], ['Diary days', counts.days, 'journal'], ['Warnings so far', counts.warnings, 'alert'], ['API tokens', counts.api_tokens, 'key']]
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Pocket Core', core.configured ? (core.registered ? <span key="c" className="ok">connected · {core.url}</span> : <span key="c" className="bad">{core.error || 'not registered yet'}</span>) : <span key="c" className="muted">not configured</span>],
    ['Database', <span key="db"><code>{String(data.db_path)}</code> · {fmtBytes(Number(data.db_bytes))}</span>],
    ['People', `${counts.users ?? 0} accounts on this box`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Weather</h3>
          <p className="muted small">The weather for your places and where you are: now, 48 hours and ten days, air and pollen, warnings for frost, storms, rain and heat, the weather for your events and trips, and a diary of every day. Forecasts from Open-Meteo. One binary, one SQLite file, one volume.</p>
        </div>
      </div>
      <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{(n ?? 0).toLocaleString()}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setKeys(true)}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-weather" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Part of the Pocket apps: Pocket Core connects them — Calendar, Travel, Home, Garage, Pets, Movement, Tasks, Day, Pocket AI and more. Weather data by Open-Meteo.com (CC BY 4.0); place names by OpenStreetMap.</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}
