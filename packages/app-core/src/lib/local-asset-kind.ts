import { isExcalidrawPath, isObsidianExcalidrawPath } from '@shared/excalidraw'

// Pure href classification, split from local-assets.ts so the markdown
// pipeline (and the public site bundle) can use it without importing the
// app store and bridge-backed helpers local-assets.ts depends on.

const IMAGE_EXTENSIONS = new Set([
  '.apng',
  '.avif',
  '.gif',
  '.jpeg',
  '.jpg',
  '.png',
  '.svg',
  '.webp'
])
const PDF_EXTENSIONS = new Set(['.pdf'])
const AUDIO_EXTENSIONS = new Set(['.aac', '.flac', '.m4a', '.mp3', '.ogg', '.wav'])
const VIDEO_EXTENSIONS = new Set(['.m4v', '.mov', '.mp4', '.ogv', '.webm'])

export type LocalAssetKind = 'image' | 'pdf' | 'audio' | 'video' | 'excalidraw' | 'file'

export function stripQueryAndHash(href: string): string {
  return href.split('#')[0]?.split('?')[0] ?? href
}

export function hrefFragment(href: string): string {
  const hashIdx = href.indexOf('#')
  return hashIdx >= 0 ? href.slice(hashIdx) : ''
}

export function decodeHrefPath(value: string): string {
  const cleaned = stripQueryAndHash(value)
  try {
    return decodeURIComponent(cleaned)
  } catch {
    return cleaned
  }
}

export function posixJoin(a: string, b: string): string {
  if (!a) return b
  if (!b) return a
  if (a.endsWith('/')) return `${a}${b}`
  return `${a}/${b}`
}

export function posixNormalize(input: string): string {
  const parts = input.split('/')
  const out: string[] = []
  for (const part of parts) {
    if (!part || part === '.') continue
    if (part === '..') {
      if (out.length === 0) return '..'
      out.pop()
    } else {
      out.push(part)
    }
  }
  return out.join('/')
}

function assetExtension(href: string): string {
  const clean = stripQueryAndHash(href)
  const lastDot = clean.lastIndexOf('.')
  return lastDot === -1 ? '' : clean.slice(lastDot).toLowerCase()
}

export function classifyLocalAssetHref(href: string): LocalAssetKind | null {
  if (!href || href.startsWith('#') || href.startsWith('//')) return null
  if (/^[a-zA-Z][a-zA-Z\d+.-]*:/.test(href)) return null
  if (isExcalidrawPath(href) || isObsidianExcalidrawPath(href)) return 'excalidraw'
  const ext = assetExtension(href)
  if (IMAGE_EXTENSIONS.has(ext)) return 'image'
  if (PDF_EXTENSIONS.has(ext)) return 'pdf'
  if (AUDIO_EXTENSIONS.has(ext)) return 'audio'
  if (VIDEO_EXTENSIONS.has(ext)) return 'video'
  return 'file'
}
