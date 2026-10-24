import { describe, it, expect } from 'vitest'
import { effectiveVisibility, type PublicMark } from './public-site-rules'

const rules: Record<string, PublicMark> = {
  Blog: 'public',
  'Blog/Drafts': 'hidden',
  'Blog/Drafts/Ready': 'public',
  'Notes/one.md': 'public'
}

describe('effectiveVisibility (parity with Go Policy.IsPublic)', () => {
  const cases: Record<string, boolean> = {
    'Blog/a.md': true,
    'Blog/Drafts/x.md': false,
    'Blog/Drafts/Ready/y.md': true,
    'Notes/one.md': true,
    'Notes/two.md': false,
    'Other/z.md': false,
    'Blog/.secret/a.md': false,
    '../Blog/a.md': false,
    '/Blog/a.md': false,
    'Blog\\a.md': false,
    'blog/a.md': false,
    '': false,
    'Blog/../Blog/a.md': false
  }
  for (const [input, want] of Object.entries(cases)) {
    it(`visible(${JSON.stringify(input)}) = ${want}`, () => {
      expect(effectiveVisibility(input, rules).visible).toBe(want)
    })
  }

  it('reports source', () => {
    expect(effectiveVisibility('Blog/a.md', rules)).toEqual({ visible: true, source: 'inherited' })
    expect(effectiveVisibility('Blog', rules)).toEqual({ visible: true, source: 'explicit' })
    expect(effectiveVisibility('Blog/Drafts/x.md', rules)).toEqual({ visible: false, source: 'inherited' })
    expect(effectiveVisibility('Other/z.md', rules)).toEqual({ visible: false, source: 'default' })
    expect(effectiveVisibility('Blog/.secret/a.md', rules)).toEqual({ visible: false, source: 'default' })
  })
})
