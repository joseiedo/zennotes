// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { readSiteTheme, resolveSiteTheme } from './theme'

function docWith(head: string): Document {
  return new DOMParser().parseFromString(`<html><head>${head}</head></html>`, 'text/html')
}

describe('site theme', () => {
  it('defaults to Gruvbox hard in the OS mode without a server theme', () => {
    const t = readSiteTheme(docWith(''))
    expect(resolveSiteTheme(t, true)).toEqual({ id: 'dark-hard', mode: 'dark' })
    expect(resolveSiteTheme(t, false)).toEqual({ id: 'light-hard', mode: 'light' })
  })

  it('keeps a fixed admin theme whatever the visitor prefers', () => {
    const t = readSiteTheme(
      docWith('<meta name="zn-theme" content="nord-dark" data-family="nord" data-mode="dark">')
    )
    expect(resolveSiteTheme(t, false)).toEqual({ id: 'nord-dark', mode: 'dark' })
  })

  it('follows the visitor OS within the family in auto mode', () => {
    const t = readSiteTheme(
      docWith('<meta name="zn-theme" content="catppuccin-mocha" data-family="catppuccin" data-mode="auto">')
    )
    expect(resolveSiteTheme(t, false).id).toBe('catppuccin-latte')
    expect(resolveSiteTheme(t, true).id).toBe('catppuccin-mocha')
  })

  it('falls back to a known theme for an unknown id', () => {
    const t = readSiteTheme(docWith('<meta name="zn-theme" content="gone" data-family="x" data-mode="dark">'))
    expect(resolveSiteTheme(t, true).id).toBe(resolveSiteTheme(t, false).id)
  })
})
