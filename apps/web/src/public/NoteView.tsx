import { useEffect, useRef, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { renderMarkdown } from '@renderer/lib/markdown'
import { expandEmbeds } from '@renderer/lib/transclusion'
import { renderDiagrams } from '@renderer/lib/diagram-renderers'
import { renderMermaidSvg } from '@renderer/lib/mermaid-render'
import { attachInlineDiagramPanZoom } from '@renderer/lib/inline-diagram-pan-zoom'
import {
  CODE_COPY_BUTTON_SELECTOR,
  CODE_FOLD_BUTTON_SELECTOR,
  copyCodeBlockToClipboard,
  enhanceCodeBlockCopy,
  toggleCodeBlockFold
} from '@renderer/lib/code-block-copy'
import { renderBookmarks, renderEmbeds } from '@renderer/lib/embed-renderers'
import { extractObsidianExcalidrawScene, parseExcalidrawDocument } from '@shared/excalidraw'
import { fetchNote, type PublicNote, type PublicNoteMeta } from './api'
import {
  findHashTarget,
  markUnresolvedEmbeds,
  resolvePublicNote,
  rewriteRenderedLinks
} from './links'

export type ThemeMode = 'light' | 'dark'

/** Expand `![[Note]]` transclusions against public notes only. */
async function prepareMarkdown(note: PublicNote, notes: PublicNoteMeta[]): Promise<string> {
  const marked = markUnresolvedEmbeds(note.body, notes)
  if (!marked.includes('![[')) return marked
  return expandEmbeds(marked, note.path, {
    resolve: (target) => {
      const hit = resolvePublicNote(notes, target)
      return hit ? { path: hit.path, title: hit.title } : null
    },
    loadNote: async (path) => {
      try {
        const loaded = await fetchNote(path)
        return loaded ? markUnresolvedEmbeds(loaded.body, notes) : null
      } catch {
        return null
      }
    }
  })
}

async function renderMermaidBlocks(root: HTMLElement, mode: ThemeMode): Promise<void> {
  const blocks = Array.from(root.querySelectorAll<HTMLElement>('.mermaid'))
  if (blocks.length === 0) return
  try {
    await document.fonts?.ready
  } catch {
    /* render anyway */
  }
  for (const el of blocks) {
    const source = el.getAttribute('data-mermaid-source') ?? el.textContent ?? ''
    if (!source.trim()) continue
    el.setAttribute('data-mermaid-source', source)
    el.innerHTML = ''
    const surface = document.createElement('div')
    surface.className = 'zen-diagram-surface'
    el.appendChild(surface)
    const result = await renderMermaidSvg(source, mode, mode, 'zen-mermaid-preview')
    if (result.ok) {
      surface.innerHTML = result.svg
      attachInlineDiagramPanZoom(surface)
    } else {
      const pre = document.createElement('pre')
      pre.className = 'whitespace-pre-wrap text-xs'
      pre.textContent = `Mermaid error: ${result.error}`
      surface.appendChild(pre)
    }
  }
}

// ---------------------------------------------------------------------------
// Excalidraw: the admin preview reads drawings through window.zen, so the
// public site fetches the drawing via the asset API and exports it to SVG.

function PublicExcalidraw({
  url,
  target,
  mode,
  width,
  height
}: {
  url: string
  target: string
  mode: ThemeMode
  width?: number
  height?: number
}): JSX.Element {
  const [svg, setSvg] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const res = await fetch(url)
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const raw = await res.text()
        const doc = /\.excalidraw\.md$/i.test(target)
          ? extractObsidianExcalidrawScene(raw)
          : parseExcalidrawDocument(raw)
        if (!doc) throw new Error('unreadable drawing')
        const { exportToSvg } = await import('@excalidraw/excalidraw')
        const elements = doc.elements.filter(
          (el) => typeof el === 'object' && el !== null && !(el as { isDeleted?: boolean }).isDeleted
        )
        const out = await exportToSvg({
          elements: elements as never,
          appState: { ...doc.appState, exportBackground: true, exportWithDarkMode: mode === 'dark' } as never,
          files: (doc.files ?? null) as never,
          exportPadding: 8
        })
        out.removeAttribute('width')
        out.removeAttribute('height')
        if (!cancelled) setSvg(out.outerHTML)
      } catch (err) {
        console.error('excalidraw embed failed', target, err)
        if (!cancelled) setFailed(true)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [url, target, mode])

  const style: React.CSSProperties = {}
  if (width) style.maxWidth = `${width}px`
  if (height) style.maxHeight = `${height}px`
  if (failed) {
    return (
      <div className="zn-embed-missing" style={style}>
        Drawing unavailable
      </div>
    )
  }
  if (!svg) {
    return <div className="excalidraw-embed-loading excalidraw-embed-preview" style={style} />
  }
  return (
    <div
      className="excalidraw-embed-image excalidraw-embed-preview zn-excalidraw-svg"
      style={style}
      role="img"
      aria-label={target}
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  )
}

export function NoteView({
  note,
  notes,
  mode,
  hash,
  navKey = 0
}: {
  note: PublicNote
  notes: PublicNoteMeta[]
  mode: ThemeMode
  hash: string
  /** Bumped on every navigation so re-clicking the same #anchor rescrolls. */
  navKey?: number
}): JSX.Element {
  const ref = useRef<HTMLElement | null>(null)
  const [rendered, setRendered] = useState(0)

  useEffect(() => {
    const root = ref.current
    if (!root) return
    let cancelled = false
    const drawings: Root[] = []

    void (async () => {
      const markdown = await prepareMarkdown(note, notes)
      if (cancelled) return
      const stage = document.createElement('div')
      stage.innerHTML = renderMarkdown(markdown)
      rewriteRenderedLinks(stage, { notePath: note.path, notes, markdown })
      enhanceCodeBlockCopy(stage, { notePath: note.path })
      stage.querySelectorAll<HTMLInputElement>('input[type="checkbox"]').forEach((input) => {
        input.disabled = true
      })
      try {
        await renderMermaidBlocks(stage, mode)
      } catch {
        /* per-block errors are shown inline */
      }
      if (cancelled) return
      root.replaceChildren(...Array.from(stage.childNodes))
      try {
        await renderDiagrams(root, { themeKey: mode, expanded: false })
      } catch {
        /* per-block errors are shown inline */
      }
      if (cancelled) return
      renderEmbeds(root)
      renderBookmarks(root)
      root.querySelectorAll<HTMLElement>('[data-excalidraw-embed]').forEach((host) => {
        const url = host.dataset.znAssetUrl
        const target = host.getAttribute('data-excalidraw-embed') ?? ''
        if (!url) return
        const w = Number(host.getAttribute('data-embed-width')) || undefined
        const h = Number(host.getAttribute('data-embed-height')) || undefined
        const r = createRoot(host)
        drawings.push(r)
        r.render(
          <PublicExcalidraw
            url={url}
            target={target}
            mode={mode}
            width={w}
            height={h}
          />
        )
      })
      setRendered((n) => n + 1)
    })()

    return () => {
      cancelled = true
      // Unmount after the current commit; React warns on synchronous
      // unmounts during another root's render.
      const pending = drawings.splice(0)
      queueMicrotask(() => pending.forEach((r) => r.unmount()))
    }
  }, [note, notes, mode])

  useEffect(() => {
    if (!ref.current) return
    const dest = findHashTarget(ref.current, hash)
    if (dest) dest.scrollIntoView({ block: dest.matches('h1,h2,h3,h4,h5,h6') ? 'start' : 'center' })
  }, [hash, navKey, rendered])

  const onClick = (event: React.MouseEvent<HTMLElement>): void => {
    const target = event.target as HTMLElement
    const copy = target.closest<HTMLButtonElement>(CODE_COPY_BUTTON_SELECTOR)
    if (copy) {
      event.preventDefault()
      copyCodeBlockToClipboard(copy)
      return
    }
    const fold = target.closest<HTMLButtonElement>(CODE_FOLD_BUTTON_SELECTOR)
    if (fold) {
      event.preventDefault()
      toggleCodeBlockFold(fold)
    }
  }

  return (
    <article
      ref={ref}
      data-preview-content
      className="prose-zen zn-note py-8"
      onClick={onClick}
    />
  )
}
