import { describe, expect, it } from 'vitest'
import type { NoteMeta } from '@shared/ipc'
import {
  buildPublicTree,
  homeNoteCandidates,
  isPublicPageCandidate,
  withMark
} from './PublicSiteSettings'

const n = (path: string, folder: NoteMeta['folder'] = 'inbox'): NoteMeta =>
  ({ path, folder, title: path.split('/').pop()!.replace(/\.md$/, '') }) as NoteMeta

describe('isPublicPageCandidate', () => {
  it('accepts plain markdown notes', () => {
    expect(isPublicPageCandidate(n('Blog/a.md'))).toBe(true)
  })
  it('rejects trash, non-md, drawings, dot-paths', () => {
    expect(isPublicPageCandidate(n('trash/a.md', 'trash'))).toBe(false)
    expect(isPublicPageCandidate(n('Blog/a.txt'))).toBe(false)
    expect(isPublicPageCandidate(n('Blog/a.excalidraw'))).toBe(false)
    expect(isPublicPageCandidate(n('Blog/a.excalidraw.md'))).toBe(false)
    expect(isPublicPageCandidate(n('Blog/.hid/a.md'))).toBe(false)
  })
  it('matches the server: the .md extension is case-sensitive, drawings are not', () => {
    expect(isPublicPageCandidate(n('Blog/A.MD'))).toBe(false)
    expect(isPublicPageCandidate(n('Blog/a.Md'))).toBe(false)
    expect(isPublicPageCandidate(n('Blog/d.Excalidraw.md'))).toBe(false)
  })
})

describe('buildPublicTree', () => {
  it('nests folders, sorts folders first, skips non-candidates', () => {
    const tree = buildPublicTree([
      n('b.md'),
      n('Blog/z.md'),
      n('Blog/Drafts/x.md'),
      n('Blog/a.excalidraw'),
      n('trash/t.md', 'trash')
    ])
    expect(tree.map((t) => t.path)).toEqual(['Blog', 'b.md'])
    expect(tree[0].children.map((t) => t.path)).toEqual(['Blog/Drafts', 'Blog/z.md'])
    expect(tree[0].children[0].children.map((t) => t.path)).toEqual(['Blog/Drafts/x.md'])
  })
})

describe('withMark', () => {
  it('sets, replaces and clears (inherit) without mutating', () => {
    const r = { Blog: 'public' as const }
    const a = withMark(r, 'Blog/Drafts', 'hidden')
    expect(a).toEqual({ Blog: 'public', 'Blog/Drafts': 'hidden' })
    expect(r).toEqual({ Blog: 'public' })
    expect(withMark(a, 'Blog/Drafts', null)).toEqual({ Blog: 'public' })
  })
})

describe('homeNoteCandidates', () => {
  it('lists only visible candidate notes', () => {
    const out = homeNoteCandidates(
      [n('Blog/a.md'), n('Blog/Drafts/x.md'), n('Other/z.md'), n('Blog/p.excalidraw')],
      { Blog: 'public', 'Blog/Drafts': 'hidden' }
    )
    expect(out.map((x) => x.path)).toEqual(['Blog/a.md'])
  })
})
