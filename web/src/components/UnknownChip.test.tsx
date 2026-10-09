import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { UnknownChip } from './UnknownChip'

describe('UnknownChip', () => {
  it('renders the unknown marker for null and undefined', () => {
    const { rerender } = render(<UnknownChip value={null} />)
    expect(screen.getByText('Unknown')).toBeInTheDocument()
    rerender(<UnknownChip value={undefined} />)
    expect(screen.getByText('Unknown')).toBeInTheDocument()
  })

  it('renders the unknown marker for empty and whitespace-only values', () => {
    const { rerender } = render(<UnknownChip value="" />)
    expect(screen.getByText('Unknown')).toBeInTheDocument()
    rerender(<UnknownChip value="   " />)
    expect(screen.getByText('Unknown')).toBeInTheDocument()
  })

  it('renders the value when present and never an empty cell', () => {
    render(<UnknownChip value="remote" />)
    expect(screen.getByText('remote')).toBeInTheDocument()
    expect(screen.queryByText('Unknown')).not.toBeInTheDocument()
  })

  it('supports a custom fallback label', () => {
    render(<UnknownChip value={null} fallback="Not available" />)
    expect(screen.getByText('Not available')).toBeInTheDocument()
  })
})
