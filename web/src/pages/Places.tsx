// Places: the places you follow, with the weather now. Make one home, switch
// warnings and the diary on or off per place, rename, reorder, remove.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { announceChange, useData } from '../lib/data'
import type { Place, PlaceNow } from '../lib/types'
import { deg, unitsOf } from '../lib/units'
import { openAddPlace } from '../components/Layout'
import { Icon } from '../components/icons'
import { WeatherIcon } from '../components/WeatherIcon'
import { Card, Confirm, Empty, Field, Loading, Modal, Switch, useToast } from '../components/ui'

export default function PlacesPage() {
  const { settings } = useApp()
  const { overview } = useData()
  const { toast } = useToast()
  const u = unitsOf(settings)
  const [edit, setEdit] = useState<PlaceNow | null>(null)
  const [del, setDel] = useState<PlaceNow | null>(null)
  if (!overview) return <Loading />
  const places = overview.places
  const save = async (p: Place, patch: Partial<Place>, msg?: string) => {
    try { await api.put(`${V1}/locations/${p.id}`, patch); announceChange(); if (msg) toast(msg) } catch (e) { toast((e as Error).message, 'err') }
  }
  const move = async (i: number, dir: number) => {
    const ids = places.filter(p => p.source !== 'here').map(p => p.id)
    const j = ids.indexOf(places[i].id), k = j + dir
    if (j < 0 || k < 0 || k >= ids.length) return
    ;[ids[j], ids[k]] = [ids[k], ids[j]]
    try { await api.post(`${V1}/locations/order`, { ids }); announceChange() } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <div className="stack loose">
      <Card title="Your places" flush actions={<button type="button" className="btn primary sm" onClick={openAddPlace}><Icon name="plus" size={14} />Add a place</button>}
        sub="Home is the place the morning message, Pocket Day and Pocket AI use. Warnings and the diary are kept for the places that have them switched on.">
        {places.length === 0 ? <div className="pad"><Empty title="No places yet" icon="pin"><button type="button" className="link-btn" onClick={openAddPlace}>Add your town</button></Empty></div> : (
          <div className="list">
            {places.map((p, i) => (
              <div key={p.id} className="wx-place">
                <Link to={p.is_home ? '/' : `/places/${p.id}`} className="wx-place-main">
                  {p.now ? <WeatherIcon condition={p.now.condition} day={p.now.is_day} size={34} /> : <Icon name="pin" size={20} />}
                  <span className="grow">
                    <span className="row-title">{p.name}{p.is_home && <span className="badge accent">home</span>}{p.source === 'here' && <span className="badge">where you are</span>}</span>
                    <span className="muted small block-line ellipsis">{[p.note, p.region, p.country].filter(Boolean).join(' · ')}{p.elevation ? ` · ${Math.round(p.elevation)} m` : ''}</span>
                  </span>
                  <span className="wx-place-temp tnum">{p.now ? deg(p.now.temp, u) : p.error ? <Icon name="alert" size={14} className="bad" /> : ''}</span>
                  <span className="muted small tnum hide-mobile wx-place-range">{p.today ? `${deg(p.today.t_min, u)} / ${deg(p.today.t_max, u)}` : ''}</span>
                </Link>
                <span className="wx-place-ctl">
                  <span className="row small muted" title="Warnings and the diary for this place"><Switch size="sm" checked={p.warn} onChange={v => save(p, { warn: v }, v ? 'Warnings on' : 'Warnings off')} label={`Warnings for ${p.name}`} /><span className="hide-mobile">Warnings</span></span>
                  {p.source !== 'here' && <>
                    <button type="button" className="btn ghost sm icon-only" aria-label="Move up" disabled={i === 0 || p.is_home} onClick={() => move(i, -1)}><Icon name="chevron-up" size={14} /></button>
                    <button type="button" className="btn ghost sm icon-only" aria-label="Move down" disabled={i === places.length - 1} onClick={() => move(i, 1)}><Icon name="chevron-down" size={14} /></button>
                    <button type="button" className="btn ghost sm icon-only" aria-label="Edit" onClick={() => setEdit(p)}><Icon name="edit" size={14} /></button>
                  </>}
                  <button type="button" className="btn ghost sm icon-only danger-text" aria-label="Remove" onClick={() => setDel(p)}><Icon name="trash" size={14} /></button>
                </span>
              </div>
            ))}
          </div>
        )}
      </Card>
      {settings && <p className="muted small">{settings.follow_me ? <>“Where you are” comes from Pocket Movement when you are more than 25 km from your places. </> : null}<Link to="/settings">Settings</Link></p>}
      {edit && <EditPlace p={edit} onClose={() => setEdit(null)} onSave={async patch => { await save(edit, patch, 'Saved'); setEdit(null) }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name}?`} message="Its diary and warnings go with it." onCancel={() => setDel(null)}
        onConfirm={async () => { const d = del; setDel(null); try { await api.del(`${V1}/locations/${d.id}`); announceChange(); toast(`${d.name} removed`) } catch (e) { toast((e as Error).message, 'err') } }} />}
    </div>
  )
}

function EditPlace({ p, onClose, onSave }: { p: Place; onClose: () => void; onSave: (patch: Partial<Place>) => void }) {
  const [f, setF] = useState({ name: p.name, note: p.note, is_home: p.is_home })
  return (
    <Modal title={`Edit ${p.name}`} onClose={onClose} size="sm">
      <form onSubmit={e => { e.preventDefault(); onSave(f) }} className="stack">
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required maxLength={80} autoFocus /></Field>
        <Field label="Note" help="For you: “The cottage”, “Mum and dad”."><input value={f.note} onChange={e => setF({ ...f, note: e.target.value })} maxLength={120} /></Field>
        <label className="row" style={{ gap: 8 }}><Switch checked={f.is_home} onChange={v => setF({ ...f, is_home: v })} label="Home" disabled={p.is_home} />This is home</label>
        <p className="muted small">{p.lat.toFixed(4)}, {p.lon.toFixed(4)}{p.timezone ? ` · ${p.timezone}` : ''}</p>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary">Save</button></div>
      </form>
    </Modal>
  )
}
