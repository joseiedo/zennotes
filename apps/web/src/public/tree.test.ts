import { describe, expect, it } from 'vitest'
import type { PublicNoteMeta } from './api'
import { buildTree, noteUrl, pathFromUrl, type TreeNode } from './tree'

const meta = (path: string, title?: string): PublicNoteMeta => ({
  path,
  title: title ?? path.split('/').pop()!.replace(/\.md$/, ''),
  updatedAt: 0,
  wikilinks: []
})

function shape(nodes: TreeNode[]): unknown[] {
  return nodes.map((n) =>
    n.kind === 'folder' ? { folder: n.name, children: shape(n.children) } : n.path
  )
}

describe('buildTree', () => {
  it('nests notes under only the folders their paths contain', () => {
    const tree = buildTree([meta('Blog/a.md'), meta('Projects/zennotes/x.md')])
    expect(shape(tree)).toEqual([
      { folder: 'Blog', children: ['Blog/a.md'] },
      { folder: 'Projects', children: [{ folder: 'zennotes', children: ['Projects/zennotes/x.md'] }] }
    ])
  })

  it('sorts folders first, then by title', () => {
    const tree = buildTree([
      meta('zeta.md', 'Zeta'),
      meta('alpha.md', 'Alpha'),
      meta('Blog/b.md', 'B'),
      meta('Blog/a.md', 'A'),
      meta('Archive/x.md')
    ])
    expect(shape(tree)).toEqual([
      { folder: 'Archive', children: ['Archive/x.md'] },
      { folder: 'Blog', children: ['Blog/a.md', 'Blog/b.md'] },
      'alpha.md',
      'zeta.md'
    ])
  })
})

describe('noteUrl / pathFromUrl', () => {
  it('encodes each segment and drops .md', () => {
    expect(noteUrl('Blog/My Post.md')).toBe('/Blog/My%20Post')
  })

  it('mirrors Go url.PathEscape for reserved characters', () => {
    // PathEscape keeps $&+:=@ but escapes ! ' ( ) * , ; ? #
    expect(noteUrl('a/b:c@d+e=f&g$h.md')).toBe('/a/b:c@d+e=f&g$h')
    expect(noteUrl("x/(a)!'*,;?#.md")).toBe('/x/%28a%29%21%27%2A%2C%3B%3F%23')
    expect(noteUrl('café.md')).toBe('/caf%C3%A9')
  })

  it('round-trips through pathFromUrl', () => {
    expect(pathFromUrl('/Blog/My%20Post')).toBe('Blog/My Post.md')
    expect(pathFromUrl(noteUrl("x/(a)!'*,;?#.md"))).toBe("x/(a)!'*,;?#.md")
  })

  it('returns null for home and reserved paths', () => {
    expect(pathFromUrl('/')).toBeNull()
    expect(pathFromUrl('')).toBeNull()
    expect(pathFromUrl('/_zn/missing')).toBeNull()
    expect(pathFromUrl('/%E0%A4%A')).toBeNull()
  })
})
