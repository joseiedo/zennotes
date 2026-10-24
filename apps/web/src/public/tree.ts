import type { PublicNoteMeta } from './api'

export type TreeNode =
  | { kind: 'folder'; name: string; path: string; children: TreeNode[] }
  | { kind: 'note'; name: string; path: string }

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' })

function sortNodes(nodes: TreeNode[]): void {
  nodes.sort((a, b) => {
    if (a.kind !== b.kind) return a.kind === 'folder' ? -1 : 1
    return collator.compare(a.name, b.name) || collator.compare(a.path, b.path)
  })
  for (const n of nodes) if (n.kind === 'folder') sortNodes(n.children)
}

/** Folder tree of the public notes: only folders that contain a public note
 *  exist, sorted folders-first then by title. */
export function buildTree(notes: PublicNoteMeta[]): TreeNode[] {
  const root: TreeNode[] = []
  const folders = new Map<string, TreeNode[]>()
  for (const note of notes) {
    const segs = note.path.split('/')
    let level = root
    let prefix = ''
    for (const seg of segs.slice(0, -1)) {
      prefix = prefix ? `${prefix}/${seg}` : seg
      let children = folders.get(prefix)
      if (!children) {
        children = []
        folders.set(prefix, children)
        level.push({ kind: 'folder', name: seg, path: prefix, children })
      }
      level = children
    }
    level.push({ kind: 'note', name: note.title, path: note.path })
  }
  sortNodes(root)
  return root
}

// Go's url.PathEscape keeps unreserved characters plus $ & + : = @ in a path
// segment and percent-escapes every other byte (uppercase hex).
const PATH_SEGMENT_SAFE = /[A-Za-z0-9\-_.~$&+:=@]/

function pathEscape(segment: string): string {
  let out = ''
  for (const ch of segment) {
    if (PATH_SEGMENT_SAFE.test(ch)) {
      out += ch
      continue
    }
    for (const byte of new TextEncoder().encode(ch)) {
      out += `%${byte.toString(16).toUpperCase().padStart(2, '0')}`
    }
  }
  return out
}

/** Public page URL of a note; mirrors `publicPageURL` in
 *  apps/server/internal/httpserver/public_shell.go exactly. */
export function noteUrl(path: string): string {
  const trimmed = path.endsWith('.md') ? path.slice(0, -3) : path
  return '/' + trimmed.split('/').map(pathEscape).join('/')
}

/** Inverse of noteUrl: the note path a page URL addresses, or null for the
 *  home page, reserved paths and undecodable URLs. Mirrors the server, which
 *  matches the decoded request path + ".md". */
export function pathFromUrl(pathname: string): string | null {
  if (!pathname || pathname === '/' || pathname.startsWith('/_zn/')) return null
  let decoded: string
  try {
    decoded = decodeURIComponent(pathname)
  } catch {
    return null
  }
  const rel = decoded.replace(/^\//, '')
  return rel ? `${rel}.md` : null
}
