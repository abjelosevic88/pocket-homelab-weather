// Warnings: what the forecast warns about at your places now, by day, and
// the last 30 days.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange, useOnChange } from '../lib/data'
import { fmtDate, weekday } from '../lib/format'
import type { Warning } from '../lib/types'
import { Icon } from '../components/icons'
import { Card, Empty, Loading, useAsync, useToast } from '../components/ui'
import { WarningRow } from './Today'

export default function WarningsPage() {
  const { settings } = useApp()
  const { toast } = useToast()
  const { data, error, reload } = useAsync(() => api.get<{ active: Warning[]; history: Warning[]; today: string }>(`${V1}/warnings?history=1`), [])
  useOnChange(reload)
  const [busy, setBusy] = useState(false)
  const [past, setPast] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const check = async () => {
    setBusy(true)
    try { await api.post(`${V1}/check`); announceChange(); toast('Checked the latest forecast', 'ok') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const days = [...new Set(data.active.map(w => w.day))]
  const label = (d: string) => d === data.today ? 'Today' : d === addDay(data.today) ? 'Tomorrow' : `${weekday(d, true)}, ${fmtDate(d, false)}`
  const off = settings ? (['frost', 'heat', 'wind', 'rain', 'snow', 'storm', 'uv', 'pollen', 'air'] as const).filter(k => !settings[`warn_${k}`]) : []
  return (
    <div className="stack loose">
      <div className="row wrap" style={{ gap: 8 }}>
        <p className="muted small grow" style={{ margin: 0 }}>Pocket Weather's own warnings, worked out from the forecast for today and the next two days with your limits — not the official warnings of the weather service. {off.length > 0 && <>Off: {off.join(', ')}. </>}<Link to="/settings/warnings">Change what counts</Link></p>
        <button type="button" className="btn sm" onClick={check} disabled={busy}><Icon name="refresh" size={14} />{busy ? 'Checking…' : 'Check again'}</button>
      </div>
      {data.active.length === 0 && <Card><Empty title="No warnings" icon="check-circle">Nothing to watch out for at your places in the next three days.</Empty></Card>}
      {days.map(d => (
        <Card key={d} title={label(d)} flush>
          <div className="list">{data.active.filter(w => w.day === d).map(w => <WarningRow key={w.id} w={w} showPlace />)}</div>
        </Card>
      ))}
      {data.history.length > 0 && (
        <Card title="The last 30 days" flush actions={<button type="button" className="btn ghost sm" onClick={() => setPast(!past)}>{past ? 'Hide' : `Show ${data.history.length}`}</button>}>
          {past && <div className="list faded">{data.history.map(w => (
            <div key={w.id} className="list-row static">
              <span className="muted small nowrap" style={{ width: 84 }}>{fmtDate(w.day, false)}</span>
              <span className="grow ellipsis">{w.title}<span className="muted"> · {w.place}</span></span>
              <span className={`pill ${w.level === 'warning' ? 'bad' : 'warn'}`}>{w.level === 'warning' ? 'Warning' : 'Heads-up'}</span>
            </div>
          ))}</div>}
        </Card>
      )}
    </div>
  )
}

function addDay(d: string): string {
  const t = new Date(d + 'T12:00:00')
  t.setDate(t.getDate() + 1)
  return t.toISOString().slice(0, 10)
}
