// "Add a place": find a town by name, or use where this phone is.
import { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import { announceChange } from '../lib/data'
import type { Place, SearchPlace } from '../lib/types'
import { Icon } from './icons'
import { Modal, Switch, useDebounce, useToast } from './ui'

export function AddPlace({ onClose, onAdded }: { onClose: () => void; onAdded: (p: Place) => void }) {
  const { toast } = useToast()
  const [q, setQ] = useState('')
  const dq = useDebounce(q.trim(), 250)
  const [found, setFound] = useState<SearchPlace[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [locating, setLocating] = useState(false)
  const [home, setHome] = useState(false)
  const [err, setErr] = useState('')
  useEffect(() => {
    if (dq.length < 2) { setFound(null); return }
    let live = true
    api.get<SearchPlace[]>(`${V1}/search?q=${encodeURIComponent(dq)}&count=8`).then(r => { if (live) { setFound(r); setErr('') } }).catch(e => { if (live) setErr((e as Error).message) })
    return () => { live = false }
  }, [dq])
  const add = async (p: SearchPlace) => {
    setBusy(true)
    try {
      const l = await api.post<Place>(`${V1}/locations`, { name: p.name, region: p.region, country: p.country, country_code: p.country_code, lat: p.lat, lon: p.lon, elevation: p.elevation, timezone: p.timezone, is_home: home || undefined })
      toast(`${l.name} added`, 'ok'); announceChange(); onAdded(l)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const here = () => {
    if (!navigator.geolocation) { toast('This browser cannot tell where you are', 'err'); return }
    setLocating(true)
    navigator.geolocation.getCurrentPosition(async pos => {
      try {
        const p = await api.get<SearchPlace>(`${V1}/reverse?lat=${pos.coords.latitude}&lon=${pos.coords.longitude}`)
        await add(p)
      } catch (e) { toast((e as Error).message, 'err') } finally { setLocating(false) }
    }, e => { setLocating(false); toast(e.code === 1 ? 'Location is not allowed for this site' : 'Could not find where you are', 'err') }, { timeout: 15000, maximumAge: 600000 })
  }
  return (
    <Modal title="Add a place" onClose={onClose} size="sm">
      <div className="stack">
        <div className="home-search">
          <Icon name="search" size={16} />
          <input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="A town or city, e.g. Mostar" aria-label="Find a place" />
        </div>
        <button type="button" className="btn" onClick={here} disabled={locating || busy}><Icon name="locate" size={15} />{locating ? 'Finding you…' : 'Use where I am now'}</button>
        {err && <p className="bad small">{err}</p>}
        {found && found.length === 0 && <p className="muted small">Nothing found for “{dq}”. Try the town's name without the street.</p>}
        {found && found.length > 0 && (
          <div className="list bordered">
            {found.map(p => (
              <button key={`${p.lat},${p.lon}`} type="button" className="list-row" disabled={busy} onClick={() => add(p)}>
                <Icon name="pin" size={16} className="muted" />
                <span className="row-main grow"><span className="row-title">{p.name}</span><span className="row-sub muted small ellipsis">{[p.region, p.country].filter(Boolean).join(', ')}{p.elevation ? ` · ${Math.round(p.elevation)} m` : ''}</span></span>
                <Icon name="plus" size={15} />
              </button>
            ))}
          </div>
        )}
        <label className="row small muted" style={{ gap: 8 }}><Switch size="sm" checked={home} onChange={setHome} label="Make it home" />Make it home (the morning message, Pocket Day and Pocket AI use home)</label>
      </div>
    </Modal>
  )
}
