import { findTheme, resolveAuto, type ThemeFamily } from '@renderer/lib/themes'

/** The admin app's theme as the server embeds it in the page shell
 *  (`<meta name="zn-theme">`). Mode `auto` follows the visitor's OS. */
export interface SiteTheme {
  id: string
  family: string
  mode: string
}

// Before the admin app has reported a theme: ZenNotes' default family
// (Gruvbox Material, hard contrast) in the visitor's OS mode.
const DEFAULT_SITE_THEME: SiteTheme = { id: 'dark-hard', family: 'gruvbox', mode: 'auto' }

export function readSiteTheme(doc: Document = document): SiteTheme {
  const meta = doc.querySelector<HTMLMetaElement>('meta[name="zn-theme"]')
  const id = meta?.content
  if (!meta || !id) return DEFAULT_SITE_THEME
  return { id, family: meta.dataset.family ?? '', mode: meta.dataset.mode ?? '' }
}

/** The data-theme id and light/dark mode to render `theme` with. */
export function resolveSiteTheme(
  theme: SiteTheme,
  prefersDark: boolean
): { id: string; mode: 'light' | 'dark' } {
  const picked = findTheme(theme.id)
  const id =
    theme.mode === 'auto' ? resolveAuto(picked.family as ThemeFamily, prefersDark, picked.id) : picked.id
  return { id, mode: findTheme(id).mode }
}

export function siteThemeIsAuto(theme: SiteTheme): boolean {
  return theme.mode === 'auto'
}
