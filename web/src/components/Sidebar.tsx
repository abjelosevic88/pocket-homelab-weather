// Left column: add a place, the sections with how many warnings are
// active, and your places with the temperature now — the Pocket Calendar sidebar.
import { Link, NavLink } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import { useData } from '../lib/data'
import { deg, unitsOf } from '../lib/units'
import { Icon } from './icons'
import { WeatherIcon } from './WeatherIcon'

export function Sidebar({ onAdd, onNavigate }: { onAdd: () => void; onNavigate: () => void }) {
  const { status, settings } = useApp()
  const { overview } = useData()
  const u = unitsOf(settings)
  const title = settings?.title || status?.title || 'Pocket Weather'
  const warns = overview?.warnings.length || 0
  const nav: { to: string; label: string; icon: string; end?: boolean; count?: string; tone?: string }[] = [
    { to: '/', label: 'Today', icon: 'sun', end: true },
    { to: '/warnings', label: 'Warnings', icon: 'alert', count: warns ? String(warns) : undefined, tone: overview?.warnings.some(w => w.level === 'warning') ? 'bad' : 'warn' },
    { to: '/plans', label: 'For your plans', icon: 'calendar-check' },
    { to: '/diary', label: 'Diary', icon: 'journal' },
    { to: '/places', label: 'Places', icon: 'pin' },
  ]
  const places = overview?.places || []
  return (
    <aside className="sidebar" aria-label="Pocket Weather">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Weather' ? title : <>Pocket <b>Weather</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" onClick={onAdd}>
        <Icon name="plus" size={15} strokeWidth={2.2} />Add a place
      </button>

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} end={n.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.count && <span className={`sb-count ${n.tone || ''}`}>{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      {places.length > 0 && <>
        <div className="sb-section">Your places</div>
        <nav className="sb-nav sb-places" aria-label="Your places">
          {places.map(p => (
            <NavLink key={p.id} to={p.is_home ? '/' : `/places/${p.id}`} end onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive && !p.is_home ? 'active' : ''}`}>
              {p.now ? <WeatherIcon condition={p.now.condition} day={p.now.is_day} size={18} /> : <Icon name="pin" size={16} />}
              <span className="ellipsis grow">{p.name}{p.source === 'here' && <span className="muted"> · here</span>}</span>
              {p.warnings > 0 && <Icon name="alert" size={13} className="sb-warn" />}
              <span className="sb-temp tnum">{p.now ? deg(p.now.temp, u) : ''}</span>
            </NavLink>
          ))}
        </nav>
      </>}

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pwx:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}
