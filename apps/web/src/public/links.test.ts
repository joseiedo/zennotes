// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import type { PublicNoteMeta } from './api'
import { markUnresolvedEmbeds, rewriteRenderedLinks } from './links'

const meta = (path: string): PublicNoteMeta => ({
  path,
  title: path.split('/').pop()!.replace(/\.md$/, ''),
  updatedAt: 0,
  wikilinks: []
})

const notes = [meta('Blog/a.md')]

describe('markUnresolvedEmbeds', () => {
  it('keeps asset and public-note embeds, replaces private ones', () => {
    const out = markUnresolvedEmbeds('![[Private]] ![[img.png]] ![[Blog/a]]', notes)
    expect(out).toContain('![[img.png]]')
    expect(out).toContain('![[Blog/a]]')
    expect(out).not.toContain('Private')
    expect(out).toContain('<div class="zn-embed-missing">Not found</div>')
  })

  it('leaves embeds inside code alone', () => {
    const md = '`![[Private]]`\n\n```\n![[Private]]\n```'
    expect(markUnresolvedEmbeds(md, notes)).toBe(md)
  })

  it('treats .md embeds as notes, not assets', () => {
    expect(markUnresolvedEmbeds('![[Secret.md]]', notes)).not.toContain('Secret')
    expect(markUnresolvedEmbeds('![[drawing.excalidraw]]', notes)).toContain('drawing.excalidraw')
  })
})

describe('rewriteRenderedLinks', () => {
  function render(html: string): HTMLElement {
    const root = document.createElement('div')
    root.innerHTML = html
    rewriteRenderedLinks(root, { notePath: 'Blog/a.md', notes })
    return root
  }

  it('points unresolved wikilinks at /_zn/missing and keeps their text', () => {
    const root = render(
      '<p><a href="zen://note/Private" class="wikilink" data-wikilink="Private">Private</a></p>'
    )
    const a = root.querySelector('a')!
    expect(a.getAttribute('href')).toBe('/_zn/missing')
    expect(a.textContent).toBe('Private')
  })

  it('points public wikilinks at the note URL, with heading anchors', () => {
    const root = render(
      '<a class="wikilink" data-wikilink="Blog/a" href="zen://note/Blog%2Fa">a</a>' +
        '<a class="wikilink" data-wikilink="a#My Heading" href="zen://note/x">h</a>'
    )
    const [plain, heading] = Array.from(root.querySelectorAll('a'))
    expect(plain!.getAttribute('href')).toBe('/Blog/a')
    expect(heading!.getAttribute('href')).toBe('/Blog/a#My%20Heading')
  })

  it('rewrites local image src to the asset API', () => {
    const root = render('<p>see <img src="img.png" alt=""></p>')
    expect(root.querySelector('img')!.getAttribute('src')).toBe(
      '/_zn/api/asset?note=Blog%2Fa.md&href=img.png'
    )
  })

  it('sends plain links to excalidraw files to /_zn/missing', () => {
    const root = render('<p>x <a href="d.excalidraw">drawing</a></p>')
    expect(root.querySelector('a')!.getAttribute('href')).toBe('/_zn/missing')
  })

  it('stamps excalidraw hosts with their asset URL', () => {
    const root = render('<div class="excalidraw-embed-host" data-excalidraw-embed="d.excalidraw"></div>')
    expect(root.querySelector<HTMLElement>('[data-excalidraw-embed]')!.dataset.znAssetUrl).toBe(
      '/_zn/api/asset?note=Blog%2Fa.md&href=d.excalidraw'
    )
  })

  it('resolves assets inside a transcluded note against that note', () => {
    const root = render(
      '<div class="note-embed" data-embed-src="Blog/a"><p><img src="pic.png"></p></div>'
    )
    expect(root.querySelector('img')!.getAttribute('src')).toBe(
      '/_zn/api/asset?note=Blog%2Fa.md&href=pic.png'
    )
  })

  it('uses the href exactly as written in the markdown when it was normalized', () => {
    const root = document.createElement('div')
    root.innerHTML = '<p><img src="my%20pic.png"></p>'
    rewriteRenderedLinks(root, {
      notePath: 'Blog/a.md',
      notes,
      markdown: '![](<my pic.png>)'
    })
    expect(root.querySelector('img')!.getAttribute('src')).toBe(
      '/_zn/api/asset?note=Blog%2Fa.md&href=my%20pic.png'
    )
  })
})
