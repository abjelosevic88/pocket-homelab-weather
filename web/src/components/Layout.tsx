// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { DataProvider } from '../lib/data'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { AddPlace } from './AddPlace'
import { Avatar, type MenuItem, MenuButton, modalOpen, useIsMobile, useMediaQuery } from './ui'
import { AskButton } from './AskButton'

export { announceChange, useOnChange } from '../lib/data'

const SIDEBAR_KEY = 'pwx-sidebar'

/** The page's own name for the top bar breadcrumb. */
const CrumbCtx = createContext<(t: string, section?: string) => void>(() => {})
/** The page's name in the top bar, under a section other than the path's when given ("places"). */
export function useCrumb(title: string | undefined, section?: string) {
  const set = useContext(CrumbCtx)
  useEffect(() => { set(title || '', section); return () => set('') }, [title, section, set])
}

/** Open "Add a place" from anywhere (a page's button). */
export const openAddPlace = () => window.dispatchEvent(new CustomEvent('pwx:add'))

export default function Layout() {
  return <DataProvider><Shell /></DataProvider>
}

function Shell() {
  const navigate = useNavigate()
  const loc = useLocation()
  const [sp, setSp] = useSearchParams()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [crumb, setCrumbRaw] = useState('')
  const [crumbSection, setCrumbSection] = useState('')
  const setCrumb = useCallback((t: string, section?: string) => { setCrumbRaw(t); setCrumbSection(section || '') }, [])

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [adding, setAdding] = useState(false)
  const add = () => { closeDrawer(); setAdding(true) }

  // /?add=place (the PWA shortcut) opens the dialog
  useEffect(() => { if (sp.get('add')) { setAdding(true); setSp(new URLSearchParams(), { replace: true }) } }, []) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const open = () => setShortcuts(true)
    const make = () => setAdding(true)
    window.addEventListener('pwx:shortcuts', open)
    window.addEventListener('pwx:add', make)
    return () => { window.removeEventListener('pwx:shortcuts', open); window.removeEventListener('pwx:add', make) }
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'a': e.preventDefault(); setAdding(true); break
        case 't': go('/'); break
        case 'w': go('/warnings'); break
        case 'p': go('/plans'); break
        case 'd': go('/diary'); break
        case 'l': go('/places'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'add', label: 'Add a place', icon: 'plus', hint: 'A', keywords: 'new city town location search', run: add },
    { id: 'today', label: 'Today', icon: 'sun', hint: 'T', keywords: 'now forecast home hourly', run: () => navigate('/') },
    { id: 'warnings', label: 'Warnings', icon: 'alert', hint: 'W', keywords: 'frost storm rain heat wind snow uv pollen air', run: () => navigate('/warnings') },
    { id: 'plans', label: 'For your plans', icon: 'calendar-check', hint: 'P', keywords: 'events trips plants tyres dog walk calendar travel garage home pets', run: () => navigate('/plans') },
    { id: 'diary', label: 'Diary', icon: 'journal', hint: 'D', keywords: 'history past last year month log', run: () => navigate('/diary') },
    { id: 'places', label: 'Places', icon: 'pin', hint: 'L', keywords: 'locations cities home', run: () => navigate('/places') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'units', label: 'Settings: units', icon: 'thermometer', keywords: 'celsius fahrenheit wind mph km/h mm inches', run: () => navigate('/settings') },
    { id: 'rules', label: 'Settings: warnings', icon: 'bell', keywords: 'thresholds push notifications frost heat pollen', run: () => navigate('/settings/warnings') },
    { id: 'apps', label: 'Settings: connected apps', icon: 'layers', keywords: 'calendar travel home garage pets movement tasks core', run: () => navigate('/settings/apps') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  return (
    <CrumbCtx.Provider value={setCrumb}>
      <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
        {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
        <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
          <Sidebar onAdd={add} onNavigate={closeDrawer} />
        </div>
        <div className="main">
          <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} crumb={crumb} crumbSection={crumbSection} />
          <main className="content"><div className="content-inner"><Outlet /></div></main>
        </div>
        {mobile && <button type="button" className="fab" aria-label="Add a place" title="Add a place" onClick={add}><Icon name="plus" size={24} strokeWidth={2.2} /></button>}
        {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} />}
        {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
        {adding && <AddPlace onClose={() => setAdding(false)} onAdded={p => { setAdding(false); navigate(p.is_home ? '/' : `/places/${p.id}`) }} />}
      </div>
    </CrumbCtx.Provider>
  )
}

const SECTION: Record<string, [string, string]> = { '': ['Today', '/'], places: ['Places', '/places'], warnings: ['Warnings', '/warnings'], plans: ['For your plans', '/plans'], diary: ['Diary', '/diary'] }

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, crumb, crumbSection }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; crumb: string; crumbSection: string }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [section, sectionTo] = SECTION[crumbSection || loc.pathname.split('/')[1] || ''] || ['', '/']
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('pwx:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumb ? <><button type="button" className="crumb" onClick={() => navigate(sectionTo)}>{section}</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{crumb}</span></> : section}
      </h1>
      <span className="spacer" />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
