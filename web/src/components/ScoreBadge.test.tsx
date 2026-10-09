import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ScoreBadge } from './ScoreBadge'
import { scoreTier } from '../lib/format'

describe('scoreTier thresholds', () => {
  it('buckets scores at 75 and 50', () => {
    expect(scoreTier(null)).toBe('none')
    expect(scoreTier(undefined)).toBe('none')
    expect(scoreTier(0)).toBe('low')
    expect(scoreTier(49)).toBe('low')
    expect(scoreTier(50)).toBe('medium')
    expect(scoreTier(74)).toBe('medium')
    expect(scoreTier(75)).toBe('high')
    expect(scoreTier(100)).toBe('high')
  })
})

describe('ScoreBadge', () => {
  it('renders the rounded score and a tier label', () => {
    render(<ScoreBadge score={82} />)
    expect(screen.getByText('Match 82')).toBeInTheDocument()
    expect(screen.getByText(/High match/)).toBeInTheDocument()
  })

  it('renders a marker and No score title when score is null', () => {
    render(<ScoreBadge score={null} />)
    expect(screen.getByText('Match —')).toBeInTheDocument()
    const badge = screen.getByTitle('No score')
    expect(badge).toBeInTheDocument()
  })

  it('applies medium tier for a mid-range score', () => {
    render(<ScoreBadge score={60} />)
    expect(screen.getByTitle('Medium match')).toBeInTheDocument()
  })
})
