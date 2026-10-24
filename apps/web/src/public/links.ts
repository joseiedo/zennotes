// Post-render link and asset rewriting for the public reader. renderMarkdown
// emits the app's internal URLs (zen://note/…, raw asset hrefs); here they
// become public page URLs and /_zn/api/asset URLs. Anything that does not
// resolve to a public note goes to /_zn/missing so a private note's existence
// is never revealed by a different link shape.

import { classifyLocalAssetHref, hrefFragment } from '@renderer/lib/local-asset-kind'
import {
  isSameFileHeadingLink,
  resolveWikilinkTarget,
  wikilinkHeadingAnchor
} from '@renderer/lib/wikilinks'
import { assetUrl, type PublicNoteMeta } from './api'
import { noteUrl } from './tree'

export const MISSING_URL = '/_zn/missing'

type NoteRef = PublicNoteMeta & { folder: 'inbox' }

/** resolveWikilinkTarget wants NoteMeta-like refs with a folder; public notes
 *  are never in the trash, so any non-trash folder works. */
export function noteRefs(notes: PublicNoteMeta[]): NoteRef[] {
  return notes.map((n) => ({ ...n, folder: 'inbox' as const }))
}

export function resolvePublicNote(
  notes: PublicNoteMeta[],
  target: string
): PublicNoteMeta | null {
  return resolveWikilinkTarget(noteRefs(notes), target)
}

// ---------------------------------------------------------------------------
// Code ranges (mirrors transclusion.ts) so embeds inside code are untouched.

function codeRanges(md: string): Array<[number, number]> {
  const ranges: Array<[number, number]> = []
  const fence = /^([ \t]*)(`{3,}|~{3,})[^\n]*\n[\s\S]*?(?:\n\1\2[^\n]*|$)/gm
  for (const m of md.matchAll(fence)) ranges.push([m.index!, m.index! + m[0].length])
  const inline = /`+[^`\n]*`+/g
  for (const m of md.matchAll(inline)) {
    const at = m.index!
    if (!ranges.some(([s, e]) => at >= s && at < e)) ranges.push([at, at + m[0].length])
  }
  return ranges
}

function inCode(index: number, ranges: Array<[number, number]>): boolean {
  return ranges.some(([s, e]) => index >= s && index < e)
}

function extensionOf(href: string): string {
  const clean = href.split('#')[0]!.split('?')[0]!
  const base = clean.split('/').pop() ?? clean
  const dot = base.lastIndexOf('.')
  return dot <= 0 ? '' : base.slice(dot).toLowerCase()
}

/** True when an embed/link target names a file asset rather than a note. */
function isAssetTarget(target: string): boolean {
  const kind = classifyLocalAssetHref(target)
  if (kind == null) return false
  if (kind !== 'file') return true
  const ext = extensionOf(target)
  return ext !== '' && ext !== '.md'
}

const EMBED_RE = /!\[\[([^\]\n]+?)\]\]/g
export const MISSING_EMBED_HTML = '<div class="zn-embed-missing">Not found</div>'

/** Replace `![[target]]` note embeds that don't resolve to a public note with
 *  a "Not found" block, so the renderer never turns them into asset requests
 *  or shows a private note's name. Asset embeds are kept. */
export function markUnresolvedEmbeds(markdown: string, notes: PublicNoteMeta[]): string {
  if (!markdown.includes('![[')) return markdown
  const ranges = codeRanges(markdown)
  const refs = noteRefs(notes)
  return markdown.replace(EMBED_RE, (whole, inner: string, offset: number) => {
    if (inCode(offset, ranges)) return whole
    const target = (inner.split('|', 1)[0] ?? '').trim()
    if (target && isAssetTarget(target)) return whole
    if (target && resolveWikilinkTarget(refs, target)) return whole
    return MISSING_EMBED_HTML
  })
}

// ---------------------------------------------------------------------------
// Asset refs as written in the markdown. The renderer normalizes URLs
// (`<my pic.png>` → `my%20pic.png`), but the asset API matches the href
// exactly as the note wrote it, so map rendered hrefs back to the original.

const MD_LINK_RE = /!?\[[^\]\n]*\]\(([^)\n]*)\)/g
const WIKI_RE = /!?\[\[([^\]\n]+)\]\]/g

function mdHref(inner: string): string {
  const t = inner.trim()
  if (t.startsWith('<')) {
    const end = t.indexOf('>')
    if (end > 0) return t.slice(1, end).trim()
  }
  const m = /^(\S+)(?:[ \t]+["'(][\s\S]*)?$/.exec(t)
  return m ? m[1]! : t
}

function writtenRefs(markdown: string | undefined): string[] {
  if (!markdown) return []
  const out: string[] = []
  for (const m of markdown.matchAll(MD_LINK_RE)) out.push(mdHref(m[1]!))
  for (const m of markdown.matchAll(WIKI_RE)) {
    out.push((m[1]!.split('|', 1)[0] ?? '').trim())
  }
  return out
}

function safeDecode(v: string): string {
  try {
    return decodeURIComponent(v)
  } catch {
    return v
  }
}

function originalHref(rendered: string, refs: string[]): string {
  if (refs.includes(rendered)) return rendered
  const want = safeDecode(rendered)
  return refs.find((r) => safeDecode(r) === want) ?? rendered
}

// ---------------------------------------------------------------------------

export interface RewriteContext {
  notePath: string
  notes: PublicNoteMeta[]
  /** The (expanded) markdown that was rendered; used to recover hrefs exactly
   *  as written. Optional. */
  markdown?: string
}

function isExternal(href: string): boolean {
  return /^[a-zA-Z][a-zA-Z\d+.-]*:/.test(href) || href.startsWith('//')
}

function headingSuffix(target: string): string {
  const heading = wikilinkHeadingAnchor(target)
  return heading ? `#${encodeURIComponent(heading)}` : ''
}

/** The note an element's assets belong to: the transcluded note when inside a
 *  `.note-embed`, else the page's own note. */
function ownerNotePath(el: Element, ctx: RewriteContext): string {
  const embed = el.parentElement?.closest<HTMLElement>('.note-embed[data-embed-src]')
  if (embed && !embed.classList.contains('note-embed-notice')) {
    const resolved = resolvePublicNote(ctx.notes, embed.dataset.embedSrc ?? '')
    if (resolved) return resolved.path
  }
  return ctx.notePath
}

function dirOf(path: string): string {
  const i = path.lastIndexOf('/')
  return i < 0 ? '' : path.slice(0, i)
}

function normalizePath(p: string): string | null {
  const out: string[] = []
  for (const part of p.split('/')) {
    if (!part || part === '.') continue
    if (part === '..') {
      if (!out.length) return null
      out.pop()
    } else out.push(part)
  }
  return out.join('/')
}

/** Resolve a relative markdown link (`../Other.md`, `Other%20Note`) to a
 *  public note: note-relative first, then vault-root, then by name. */
function resolveMarkdownNoteLink(
  href: string,
  owner: string,
  notes: PublicNoteMeta[]
): PublicNoteMeta | null {
  const clean = safeDecode(href.split('#')[0]!.split('?')[0]!)
  if (!clean) return null
  const withExt = /\.md$/i.test(clean) ? clean : `${clean}.md`
  const candidates = [
    normalizePath(clean.startsWith('/') ? withExt : `${dirOf(owner)}/${withExt}`),
    normalizePath(withExt)
  ]
  for (const c of candidates) {
    const hit = c && notes.find((n) => n.path.toLowerCase() === c.toLowerCase())
    if (hit) return hit
  }
  return resolvePublicNote(notes, clean)
}

function isStandaloneIn(el: Element, tag: 'img' | 'a'): HTMLParagraphElement | null {
  const p = el.parentElement
  if (!p || p.tagName !== 'P') return null
  const same = p.querySelectorAll(tag)
  if (same.length !== 1 || same[0] !== el) return null
  const text = p.textContent?.trim() ?? ''
  const own = tag === 'a' ? (el.textContent?.trim() ?? '') : ''
  return text === own ? (p as HTMLParagraphElement) : null
}

function fileLabel(href: string, fallback: string): string {
  const last = href.split('#')[0]!.split('?')[0]!.split('/').filter(Boolean).pop()
  return last ? safeDecode(last) : fallback
}

function buildImageFigure(img: HTMLImageElement, href: string): HTMLElement {
  const figure = document.createElement('figure')
  figure.className = 'local-image-embed not-prose'
  const frame = document.createElement('div')
  frame.className = 'local-image-embed-frame'
  img.classList.add('local-image-embed-image')
  const w = Number(img.getAttribute('width')) || 0
  const h = Number(img.getAttribute('height')) || 0
  if (w > 0) img.style.width = `${w}px`
  if (h > 0) img.style.height = `${h}px`
  frame.append(img)
  const caption = document.createElement('figcaption')
  caption.className = 'local-image-embed-caption'
  caption.textContent = img.getAttribute('alt')?.trim() || fileLabel(href, 'Image')
  figure.append(frame, caption)
  return figure
}

function buildMediaFigure(
  kind: 'pdf' | 'audio' | 'video',
  url: string,
  href: string,
  label: string
): HTMLElement {
  const figure = document.createElement('figure')
  figure.className = 'local-asset-embed not-prose'
  figure.dataset.localAssetUrl = url
  figure.dataset.localAssetKind = kind
  figure.dataset.localAssetHref = href
  const header = document.createElement('div')
  header.className = 'local-asset-embed-header'
  const title = document.createElement('div')
  title.className = 'local-asset-embed-title'
  title.textContent = label
  const open = document.createElement('a')
  open.className = 'local-asset-embed-open'
  open.href = url
  open.target = '_blank'
  open.rel = 'noreferrer'
  open.textContent = 'Open'
  header.append(title, open)
  figure.append(header)
  if (kind === 'pdf') {
    const frame = document.createElement('iframe')
    frame.className = 'local-asset-embed-frame'
    frame.src = url + hrefFragment(href)
    frame.title = label
    figure.append(frame)
  } else {
    const media = document.createElement(kind)
    media.className = kind === 'audio' ? 'local-asset-embed-audio' : 'local-asset-embed-video'
    media.src = url
    media.controls = true
    media.preload = 'metadata'
    figure.append(media)
  }
  return figure
}

function buildAttachment(url: string, href: string, label: string, standalone: boolean): HTMLElement {
  const link = document.createElement('a')
  link.href = url
  link.rel = 'noreferrer'
  link.dataset.localAssetKind = 'file'
  link.dataset.localAssetHref = href
  if (!standalone) {
    link.className = 'local-file-attachment-inline'
    link.textContent = label
    return link
  }
  const figure = document.createElement('figure')
  figure.className = 'local-file-attachment not-prose'
  link.className = 'local-file-attachment-button'
  const name = document.createElement('span')
  name.className = 'local-file-attachment-name'
  name.textContent = label
  const hint = document.createElement('span')
  hint.className = 'local-file-attachment-open'
  hint.setAttribute('aria-hidden', 'true')
  hint.textContent = '↗'
  link.append(name, hint)
  figure.append(link)
  return figure
}

function rewriteWikilink(a: HTMLAnchorElement, ctx: RewriteContext): void {
  const target = a.dataset.wikilink ?? ''
  if (isSameFileHeadingLink(target)) {
    a.setAttribute('href', headingSuffix(target) || '#')
    return
  }
  const resolved = resolvePublicNote(ctx.notes, target)
  if (resolved) {
    a.setAttribute('href', noteUrl(resolved.path) + headingSuffix(target))
    a.classList.remove('broken')
  } else {
    a.setAttribute('href', MISSING_URL)
    a.classList.add('broken')
  }
}

function rewriteAnchor(a: HTMLAnchorElement, ctx: RewriteContext, refs: string[]): void {
  const rendered = a.getAttribute('href') ?? ''
  if (!rendered || rendered.startsWith('#')) return
  if (rendered.startsWith('zen://')) {
    // Hashtags and other app-internal links have no public destination.
    a.removeAttribute('href')
    return
  }
  if (isExternal(rendered)) {
    a.target = '_blank'
    a.rel = 'noreferrer'
    return
  }
  const href = originalHref(rendered, refs)
  const owner = ownerNotePath(a, ctx)
  const kind = classifyLocalAssetHref(href)
  if (kind === 'excalidraw') {
    a.setAttribute('href', MISSING_URL)
    return
  }
  if (!isAssetTarget(href)) {
    const note = resolveMarkdownNoteLink(href, owner, ctx.notes)
    a.setAttribute('href', note ? noteUrl(note.path) + hrefFragment(href) : MISSING_URL)
    if (!note) a.classList.add('broken')
    return
  }
  const url = assetUrl(owner, href)
  a.setAttribute('href', url + (kind === 'pdf' ? hrefFragment(href) : ''))
  a.dataset.localAssetKind = kind ?? 'file'
  a.dataset.localAssetHref = href
  a.target = '_blank'
  a.rel = 'noreferrer'
  if (kind === 'pdf' || kind === 'audio' || kind === 'video') {
    const p = isStandaloneIn(a, 'a')
    if (p) p.replaceWith(buildMediaFigure(kind, url, href, fileLabel(href, a.textContent?.trim() || 'Asset')))
  }
}

function rewriteImage(img: HTMLImageElement, ctx: RewriteContext, refs: string[]): void {
  const rendered = img.getAttribute('src') ?? ''
  if (!rendered || isExternal(rendered)) return
  const href = originalHref(rendered, refs)
  const owner = ownerNotePath(img, ctx)
  const url = assetUrl(owner, href)
  const kind = classifyLocalAssetHref(href)
  const standalone = isStandaloneIn(img, 'img')
  if (kind !== 'image') {
    // A non-image file embedded with image syntax: a download chip/link.
    const label = fileLabel(href, 'Attachment')
    if (standalone) standalone.replaceWith(buildAttachment(url, href, label, true))
    else img.replaceWith(buildAttachment(url, href, label, false))
    return
  }
  img.setAttribute('src', url)
  img.loading = 'lazy'
  img.dataset.localAssetHref = href
  if (standalone) standalone.replaceWith(buildImageFigure(img, href))
}

/** Rewrite rendered note HTML in place for the public site. */
export function rewriteRenderedLinks(root: HTMLElement, ctx: RewriteContext): void {
  const refs = writtenRefs(ctx.markdown)
  root.querySelectorAll<HTMLAnchorElement>('a.wikilink[data-wikilink]').forEach((a) =>
    rewriteWikilink(a, ctx)
  )
  root.querySelectorAll<HTMLAnchorElement>('a[href]:not(.wikilink)').forEach((a) =>
    rewriteAnchor(a, ctx, refs)
  )
  root.querySelectorAll<HTMLImageElement>('img[src]').forEach((img) => rewriteImage(img, ctx, refs))
  root.querySelectorAll<HTMLElement>('[data-excalidraw-embed]').forEach((host) => {
    const target = host.getAttribute('data-excalidraw-embed') ?? ''
    if (!target.trim()) return
    host.dataset.znAssetUrl = assetUrl(ownerNotePath(host, ctx), originalHref(target, refs))
  })
}
