import { createRoot } from 'react-dom/client'
import '@renderer/styles/index.css'
import './public.css'
import { PublicApp } from './PublicApp'
import { readSiteTheme, resolveSiteTheme } from './theme'

// Excalidraw loads its hand-drawn fonts from here (copied into dist-public by
// the excalidrawFonts plugin); unset, it would fall back to a CDN the CSP blocks.
;(window as unknown as { EXCALIDRAW_ASSET_PATH?: string }).EXCALIDRAW_ASSET_PATH =
  '/_zn/static/excalidraw-assets/'

// Apply the theme before the first paint (PublicApp keeps it in sync).
const dark = window.matchMedia?.('(prefers-color-scheme: dark)').matches === true
const initial = resolveSiteTheme(readSiteTheme(), dark)
document.documentElement.dataset.theme = initial.id
document.documentElement.dataset.themeMode = initial.mode

const root = document.getElementById('zn-root')
if (!root) throw new Error('#zn-root not found')
createRoot(root).render(<PublicApp />)
