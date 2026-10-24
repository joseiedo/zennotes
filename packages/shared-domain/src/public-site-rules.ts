/**
 * Client-side mirror of the Go public-site policy
 * (apps/server/internal/publicsite/policy.go). Keep the two in lockstep: the
 * server is authoritative; this only drives the admin UI's preview.
 */
export type PublicMark = 'public' | 'hidden'

export type PublicSiteConfig = {
  homeNote: string
  rules: Record<string, PublicMark>
  missingPaths: string[]
  homeNotePublic: boolean
}

export type PublicVisibility = {
  visible: boolean
  source: 'explicit' | 'inherited' | 'default'
}

/** Nearest mark on the path or its ancestors wins; no mark means private. */
export function effectiveVisibility(
  path: string,
  rules: Record<string, PublicMark>
): PublicVisibility {
  const never: PublicVisibility = { visible: false, source: 'default' }
  if (path === '' || path.includes('\\') || path.startsWith('/')) return never
  const segs = path.split('/')
  for (const s of segs) {
    if (s === '' || s === '.' || s === '..' || s.startsWith('.')) return never
  }
  for (let i = segs.length; i >= 1; i--) {
    const mark = Object.prototype.hasOwnProperty.call(rules, segs.slice(0, i).join('/'))
      ? rules[segs.slice(0, i).join('/')]
      : undefined
    if (mark !== undefined) {
      return { visible: mark === 'public', source: i === segs.length ? 'explicit' : 'inherited' }
    }
  }
  return never
}
