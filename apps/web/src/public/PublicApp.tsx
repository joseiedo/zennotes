import { useCallback, useEffect, useState } from 'react'
import { Backlinks } from './Backlinks'
import { SearchBox } from './SearchBox'
import { fetchNote, fetchSite, type PublicNote, type PublicSite } from './api'
import { NoteView, type ThemeMode } from './NoteView'
import { NotFound } from './NotFound'
import { Sidebar } from './Sidebar'
import { pathFromUrl } from './tree'

// ZenNotes' default theme family (Gruvbox Material, hard contrast) in the
// mode the visitor's OS prefers. Tokens come from app-core's index.css.
const THEME_ID: Record<ThemeMode, string> = { light: 'light-hard', dark: 'dark-hard' }

function prefersDark(): boolean {
  return typeof window.matchMedia === 'function' && window.matchMedia('(prefers-color-scheme: dark)').matches
}

function useThemeMode(): ThemeMode {
  const [mode, setMode] = useState<ThemeMode>(() => (prefersDark() ? 'dark' : 'light'))
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (): void => setMode(mq.matches ? 'dark' : 'light')
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])
  useEffect(() => {
    const html = document.documentElement
    html.dataset.theme = THEME_ID[mode]
    html.dataset.themeMode = mode
  }, [mode])
  return mode
}

// Matches the drawer breakpoint in public.css.
const NARROW_QUERY = '(max-width: 768px)'

function useIsNarrow(): boolean {
  const [narrow, setNarrow] = useState(
    () => typeof window.matchMedia === 'function' && window.matchMedia(NARROW_QUERY).matches
  )
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const mq = window.matchMedia(NARROW_QUERY)
    const onChange = (): void => setNarrow(mq.matches)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])
  return narrow
}

const COLLAPSED_KEY = 'zn-public-nav-collapsed'

/** Desktop sidebar collapse, remembered per browser. Storage can throw
 *  (private mode, blocked site data); the sidebar then just starts open. */
function useNavCollapsed(): [boolean, (next: boolean) => void] {
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return window.localStorage.getItem(COLLAPSED_KEY) === '1'
    } catch {
      return false
    }
  })
  const update = useCallback((next: boolean) => {
    setCollapsed(next)
    try {
      if (next) window.localStorage.setItem(COLLAPSED_KEY, '1')
      else window.localStorage.removeItem(COLLAPSED_KEY)
    } catch {
      // ignore
    }
  }, [])
  return [collapsed, update]
}

interface Loc {
  pathname: string
  hash: string
  /** Navigation counter: same-URL navigations (re-clicking an anchor) still
   *  re-trigger scrolling. */
  nav: number
}

let navCounter = 0
const currentLoc = (): Loc => ({
  pathname: window.location.pathname,
  hash: window.location.hash,
  nav: ++navCounter
})

/** Same-origin page links are routed client-side; API/static URLs, new-tab
 *  links and modified clicks keep the browser's default. */
function routableHref(event: MouseEvent): URL | null {
  if (event.defaultPrevented || event.button !== 0) return null
  if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return null
  const anchor = (event.target as Element | null)?.closest?.('a[href]') as HTMLAnchorElement | null
  if (!anchor || (anchor.target && anchor.target !== '_self') || anchor.hasAttribute('download')) {
    return null
  }
  const url = new URL(anchor.href, window.location.href)
  if (url.origin !== window.location.origin) return null
  if (url.pathname.startsWith('/_zn/') && url.pathname !== '/_zn/missing') return null
  return url
}

type View =
  | { kind: 'loading' }
  | { kind: 'empty' }
  | { kind: 'missing' }
  | { kind: 'note'; note: PublicNote }

export function PublicApp(): JSX.Element {
  const mode = useThemeMode()
  const [site, setSite] = useState<PublicSite | null>(null)
  const [siteError, setSiteError] = useState(false)
  const [loc, setLoc] = useState<Loc>(currentLoc)
  const [view, setView] = useState<View>({ kind: 'loading' })
  const [navOpen, setNavOpen] = useState(false)
  const [navCollapsed, setNavCollapsed] = useNavCollapsed()
  const narrow = useIsNarrow()
  // One button: the overlay drawer on narrow screens, the collapse on desktop.
  const sidebarShown = narrow ? navOpen : !navCollapsed
  const toggleSidebar = (): void => {
    if (narrow) setNavOpen((o) => !o)
    else setNavCollapsed(!navCollapsed)
  }

  // The server pre-renders the note text for crawlers; the app replaces it.
  useEffect(() => {
    const seo = document.getElementById('zn-seo')
    if (seo) seo.replaceChildren()
  }, [])

  useEffect(() => {
    fetchSite()
      .then(setSite)
      .catch(() => setSiteError(true))
  }, [])

  const navigate = useCallback((url: URL) => {
    const next = url.pathname + url.hash
    if (next !== window.location.pathname + window.location.hash) {
      window.history.pushState(null, '', next)
    }
    setLoc({ pathname: url.pathname, hash: url.hash, nav: ++navCounter })
    setNavOpen(false)
    if (!url.hash) window.scrollTo(0, 0)
  }, [])

  useEffect(() => {
    const onPop = (): void => setLoc(currentLoc())
    const onClick = (event: MouseEvent): void => {
      const url = routableHref(event)
      if (!url) return
      event.preventDefault()
      navigate(url)
    }
    window.addEventListener('popstate', onPop)
    document.addEventListener('click', onClick)
    return () => {
      window.removeEventListener('popstate', onPop)
      document.removeEventListener('click', onClick)
    }
  }, [navigate])

  // Resolve the URL to a note once the site listing is known.
  const targetPath =
    site == null
      ? null
      : loc.pathname === '/'
        ? site.homeNote
        : pathFromUrl(loc.pathname)
  const isPublic = site != null && targetPath != null && site.notes.some((n) => n.path === targetPath)

  useEffect(() => {
    if (site == null) return
    if (!isPublic || targetPath == null) {
      setView(loc.pathname === '/' ? { kind: 'empty' } : { kind: 'missing' })
      return
    }
    let cancelled = false
    setView((v) => (v.kind === 'note' && v.note.path === targetPath ? v : { kind: 'loading' }))
    fetchNote(targetPath)
      .then((note) => {
        if (!cancelled) setView(note ? { kind: 'note', note } : { kind: 'missing' })
      })
      .catch(() => {
        if (!cancelled) setView({ kind: 'missing' })
      })
    return () => {
      cancelled = true
    }
  }, [site, targetPath, isPublic, loc.pathname])

  useEffect(() => {
    document.title =
      view.kind === 'note' ? view.note.title : view.kind === 'missing' ? 'Not found' : 'ZenNotes'
  }, [view])

  const currentPath = view.kind === 'note' ? view.note.path : null

  return (
    <div className={`zn-public${navOpen ? ' nav-open' : ''}${navCollapsed ? ' nav-collapsed' : ''}`}>
      <button
        type="button"
        className="zn-nav-toggle"
        aria-label={sidebarShown ? 'Hide notes' : 'Show notes'}
        aria-expanded={sidebarShown}
        onClick={toggleSidebar}
      >
        <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true">
          <path d="M2 4h12M2 8h12M2 12h12" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
        </svg>
      </button>
      <Sidebar
        notes={site?.notes ?? []}
        currentPath={currentPath}
        searchSlot={<SearchBox onOpen={navigate} />}
      />
      <main className="zn-main">
        {siteError ? (
          <div className="zn-message" role="alert">
            <h1>Unavailable</h1>
            <p>The site could not be loaded. Try again later.</p>
          </div>
        ) : view.kind === 'loading' ? (
          <div className="zn-loading" aria-busy="true" />
        ) : view.kind === 'empty' ? (
          <div className="zn-message">
            <h1>Nothing here yet</h1>
          </div>
        ) : view.kind === 'missing' ? (
          <NotFound />
        ) : (
          <>
            <NoteView note={view.note} notes={site!.notes} mode={mode} hash={loc.hash} navKey={loc.nav} />
            <Backlinks notePath={view.note.path} notes={site!.notes} />
          </>
        )}
      </main>
    </div>
  )
}
