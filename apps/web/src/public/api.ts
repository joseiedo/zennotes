// Client for the public site's read-only API (served by the Go server under
// /_zn/api/). The public bundle talks to nothing else.

export interface PublicNoteMeta {
  path: string
  title: string
  updatedAt: number
  wikilinks: string[]
}

export interface PublicSite {
  homeNote: string | null
  notes: PublicNoteMeta[]
}

export interface PublicNote {
  path: string
  title: string
  updatedAt: number
  body: string
}

export interface PublicSearchHit {
  path: string
  title: string
  lineText: string
}

const API = '/_zn/api'

async function getJSON<T>(url: string): Promise<T | null> {
  const res = await fetch(url, { headers: { Accept: 'application/json' } })
  if (res.status === 404) return null
  if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`)
  return (await res.json()) as T
}

export async function fetchSite(): Promise<PublicSite> {
  const site = await getJSON<PublicSite>(`${API}/site`)
  return {
    homeNote: site?.homeNote ?? null,
    notes: Array.isArray(site?.notes) ? site!.notes : []
  }
}

/** A public note, or null when it is missing or private (404). */
export function fetchNote(path: string): Promise<PublicNote | null> {
  return getJSON<PublicNote>(`${API}/note?path=${encodeURIComponent(path)}`)
}

/** URL of an asset referenced by `notePath`; `href` must be exactly as
 *  written in the note — the server matches it against the note's own refs. */
/** The asset API URL for `href` as written in `notePath`. The server lists
 *  asset refs without `#fragment`/`?query` (`![[doc.pdf#page=3]]` → `doc.pdf`),
 *  so they are dropped here; callers re-append a fragment (e.g. a PDF page) to
 *  the URL they put in the page. */
export function assetUrl(notePath: string, href: string): string {
  const path = href.split('#')[0]!.split('?')[0]!
  return `${API}/asset?note=${encodeURIComponent(notePath)}&href=${encodeURIComponent(path)}`
}

/** The server answered 429: too many searches from this client. */
export class SearchRateLimitedError extends Error {
  constructor() {
    super('search rate limited')
  }
}

export async function searchNotes(q: string): Promise<PublicSearchHit[]> {
  const url = `${API}/search?q=${encodeURIComponent(q)}`
  const res = await fetch(url, { headers: { Accept: 'application/json' } })
  if (res.status === 429) throw new SearchRateLimitedError()
  if (res.status === 404) return []
  if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`)
  const hits = (await res.json()) as PublicSearchHit[]
  return Array.isArray(hits) ? hits : []
}
