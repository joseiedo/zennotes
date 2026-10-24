import { describe, expect, it } from 'vitest'
import type { PublicNoteMeta } from './api'
import { publicBacklinks } from './Backlinks'

const meta = (path: string, wikilinks: string[]): PublicNoteMeta => ({
  path,
  title: path.split('/').pop()!.replace(/\.md$/, ''),
  updatedAt: 0,
  wikilinks
})

describe('publicBacklinks', () => {
  it('returns notes linking to the note, excluding itself', () => {
    const a = meta('Blog/a.md', ['a'])
    const b = meta('Blog/b.md', ['a'])
    const c = meta('Other/c.md', ['Blog/a'])
    const d = meta('Blog/d.md', ['nothing'])
    expect(publicBacklinks('Blog/a.md', [b, c, a, d])).toEqual([b, c])
  })
})
