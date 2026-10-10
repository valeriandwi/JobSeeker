import { describe, expect, it } from 'vitest'
import { shortDescription } from './format'

describe('shortDescription', () => {
  it('returns an empty string for missing input so callers render Unknown', () => {
    expect(shortDescription(null)).toBe('')
    expect(shortDescription(undefined)).toBe('')
    expect(shortDescription('   ')).toBe('')
  })

  it('keeps a short summary intact', () => {
    expect(shortDescription('Build APIs in Go.')).toBe('Build APIs in Go.')
  })

  it('collapses the source\u2019s newlines and runs of spaces onto one line', () => {
    expect(shortDescription('Line one.\n\n  Line   two.')).toBe('Line one. Line two.')
  })

  it('truncates on a word boundary and marks the cut', () => {
    const text = 'word '.repeat(60).trim()
    const out = shortDescription(text, 50)

    expect(out.endsWith('…')).toBe(true)
    // Never cut mid-word: everything before the ellipsis is whole words.
    expect(out.slice(0, -1).trimEnd()).toBe('word '.repeat(10).trim())
    expect(out.length).toBeLessThanOrEqual(51)
  })

  it('cuts hard when a single token is longer than the budget', () => {
    const out = shortDescription('x'.repeat(300), 40)
    expect(out).toBe(`${'x'.repeat(40)}…`)
  })
})
