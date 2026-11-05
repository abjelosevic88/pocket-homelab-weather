// The person's places with the weather now, and the active warnings, loaded
// once for the sidebar and the pages; any change in the app ("pwx:changed")
// reloads them, and they refresh by themselves every ten minutes.
import React, { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { api, V1 } from './api'
import type { Overview } from './types'

interface Data { overview: Overview | null; error: string; reload: () => void }
const Ctx = createContext<Data>({ overview: null, error: '', reload: () => {} })
export const useData = () => useContext(Ctx)

/** Something was added or changed: places and pages reload. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('pwx:changed'))
export function useOnChange(cb: () => void) {
  const ref = React.useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('pwx:changed', h)
    return () => window.removeEventListener('pwx:changed', h)
  }, [])
}

export function DataProvider({ children }: { children: React.ReactNode }) {
  const [overview, setOverview] = useState<Overview | null>(null)
  const [error, setError] = useState('')
  const reload = useCallback(() => {
    api.get<Overview>(`${V1}/overview`).then(o => { setOverview(o); setError('') }).catch(e => setError((e as Error).message))
  }, [])
  useEffect(() => {
    reload()
    const t = window.setInterval(() => { if (document.visibilityState === 'visible') reload() }, 10 * 60 * 1000)
    const vis = () => { if (document.visibilityState === 'visible') reload() }
    document.addEventListener('visibilitychange', vis)
    return () => { window.clearInterval(t); document.removeEventListener('visibilitychange', vis) }
  }, [reload])
  useOnChange(reload)
  return <Ctx.Provider value={{ overview, error, reload }}>{children}</Ctx.Provider>
}
