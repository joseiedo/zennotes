// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { renderMarkdown } from '@renderer/lib/markdown'
import type { PublicNoteMeta } from './api'
import { findHashTarget, markUnresolvedEmbeds, rewriteRenderedLinks } from './links'

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

describe('rewriteRenderedLinks with real renderMarkdown output', () => {
  async function renderReal(md: string, notePath = 'Blog/post.md'): Promise<HTMLElement> {
    const root = document.createElement('div')
    root.innerHTML = await renderMarkdown(md)
    rewriteRenderedLinks(root, { notePath, notes, markdown: md })
    return root
  }

  it('requests a PDF embed without its #page fragment and keeps the fragment in the page', async () => {
    const root = await renderReal('![[doc.pdf#page=3]]')
    const urls = Array.from(root.querySelectorAll<HTMLElement>('[src],[href],[data-local-asset-url]'))
      .flatMap((el) => [el.getAttribute('src'), el.getAttribute('href'), el.dataset.localAssetUrl])
      .filter((u): u is string => !!u && u.startsWith('/_zn/api/asset'))
    expect(urls.length).toBeGreaterThan(0)
    for (const u of urls) {
      const q = new URLSearchParams(u.split('?')[1]!.split('#')[0])
      expect(q.get('href')).toBe('doc.pdf')
    }
    const frame = root.querySelector('iframe')
    if (frame) expect(frame.getAttribute('src')).toMatch(/#page=3$/)
  })

  it('sends note-relative ../ asset hrefs unchanged', async () => {
    const root = await renderReal('![](../attachments/a.png)')
    expect(root.querySelector('img')!.getAttribute('src')).toBe(
      '/_zn/api/asset?note=Blog%2Fpost.md&href=..%2Fattachments%2Fa.png'
    )
  })
})

describe('findHashTarget', () => {
  function doc(html: string): HTMLElement {
    const root = document.createElement('div')
    root.innerHTML = html
    return root
  }

  it('finds a footnote definition and back-ref by id', () => {
    const root = doc(
      '<p>Text<sup><a href="#user-content-fn-1" id="user-content-fnref-1">1</a></sup></p>' +
        '<section><ol><li id="user-content-fn-1">Note <a href="#user-content-fnref-1">↩</a></li></ol></section>'
    )
    expect(findHashTarget(root, '#user-content-fn-1')?.tagName).toBe('LI')
    expect(findHashTarget(root, '#user-content-fnref-1')?.tagName).toBe('A')
  })

  it('resolves footnote links in real renderMarkdown output', () => {
    const root = doc(renderMarkdown('Text[^1]\n\n[^1]: The note.\n'))
    const ref = root.querySelector<HTMLAnchorElement>('sup a[href^="#"]')!
    const def = findHashTarget(root, ref.getAttribute('href')!)
    expect(def?.textContent).toContain('The note.')
    const back = def!.querySelector<HTMLAnchorElement>('a[href^="#"]')!
    expect(findHashTarget(root, back.getAttribute('href')!)).toBe(ref)
  })

  it('finds an element by name', () => {
    const root = doc('<a name="old-anchor">x</a>')
    expect(findHashTarget(root, '#old-anchor')?.textContent).toBe('x')
  })

  it('falls back to matching heading text (percent-encoded, case-insensitive)', () => {
    const root = doc('<h1>Top</h1><h2>My Heading</h2>')
    expect(findHashTarget(root, '#my%20heading')?.textContent).toBe('My Heading')
  })

  it('returns null for empty or unknown hashes', () => {
    const root = doc('<h1>Top</h1>')
    expect(findHashTarget(root, '')).toBeNull()
    expect(findHashTarget(root, '#')).toBeNull()
    expect(findHashTarget(root, '#nope')).toBeNull()
  })
})
