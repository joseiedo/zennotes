import { useEffect, useRef, useState } from 'react'
import { searchNotes, SearchRateLimitedError, type PublicSearchHit } from './api'
import { noteUrl } from './tree'

export function SearchBox({ onOpen }: { onOpen: (url: URL) => void }): JSX.Element {
  const [query, setQuery] = useState('')
  const [hits, setHits] = useState<PublicSearchHit[]>([])
  const [error, setError] = useState<string | null>(null)
  const [searched, setSearched] = useState(false)
  const seq = useRef(0)

  useEffect(() => {
    const q = query.trim()
    const id = ++seq.current
    if (!q) {
      setHits([])
      setError(null)
      setSearched(false)
      return
    }
    const timer = window.setTimeout(() => {
      searchNotes(q)
        .then((res) => {
          if (id !== seq.current) return
          setHits(res)
          setError(null)
          setSearched(true)
        })
        .catch((err) => {
          if (id !== seq.current) return
          setHits([])
          setSearched(false)
          setError(
            err instanceof SearchRateLimitedError
              ? 'Too many searches, try again in a minute'
              : 'Search is unavailable'
          )
        })
    }, 200)
    return () => window.clearTimeout(timer)
  }, [query])

  const open = (path: string): void => {
    onOpen(new URL(noteUrl(path), window.location.origin))
    setQuery('')
  }

  return (
    <div className="zn-search">
      <input
        type="search"
        className="zn-search-input"
        placeholder="Search notes"
        aria-label="Search notes"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && hits[0]) {
            e.preventDefault()
            open(hits[0].path)
          } else if (e.key === 'Escape') {
            setQuery('')
          }
        }}
      />
      {error && (
        <p className="zn-search-msg" role="alert">
          {error}
        </p>
      )}
      {!error && searched && hits.length === 0 && <p className="zn-search-msg">No results</p>}
      {hits.length > 0 && (
        <ul className="zn-search-results">
          {hits.map((h, i) => (
            <li key={`${h.path}:${i}`}>
              <a
                href={noteUrl(h.path)}
                onClick={() => setQuery('')}
              >
                <span className="zn-search-title">{h.title}</span>
                {h.lineText && <span className="zn-search-line">{h.lineText}</span>}
              </a>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
