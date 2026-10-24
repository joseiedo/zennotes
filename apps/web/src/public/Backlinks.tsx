import { backlinksForNote } from '@renderer/lib/wikilinks'
import type { PublicNoteMeta } from './api'
import { noteUrl } from './tree'

/** Public notes that link to `notePath` (never the note itself). */
export function publicBacklinks(notePath: string, notes: PublicNoteMeta[]): PublicNoteMeta[] {
  // The wikilink resolver wants a folder; public notes are never trash.
  const refs = notes.map((n) => ({ ...n, folder: 'inbox' as const }))
  const hits = backlinksForNote(refs, { path: notePath })
  const byPath = new Map(notes.map((n) => [n.path, n]))
  return hits.map((h) => byPath.get(h.path)!)
}

export function Backlinks({
  notePath,
  notes
}: {
  notePath: string
  notes: PublicNoteMeta[]
}): JSX.Element | null {
  const links = publicBacklinks(notePath, notes)
  if (links.length === 0) return null
  return (
    <section className="zn-backlinks" aria-label="Linked from">
      <h2>Linked from</h2>
      <ul>
        {links.map((n) => (
          <li key={n.path}>
            <a href={noteUrl(n.path)}>{n.title}</a>
          </li>
        ))}
      </ul>
    </section>
  )
}
