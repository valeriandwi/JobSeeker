import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ErrorBoundary } from './ErrorBoundary'

function Boom(): JSX.Element {
  throw new Error('field exploded')
}

describe('ErrorBoundary', () => {
  it('shows a message instead of a blank page when a child throws', () => {
    // React logs the caught error; silence it so the test output stays readable.
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
    render(
      <ErrorBoundary>
        <Boom />
      </ErrorBoundary>,
    )

    expect(screen.getByText(/Something went wrong rendering this page/)).toBeInTheDocument()
    expect(screen.getByText(/field exploded/)).toBeInTheDocument()
    spy.mockRestore()
  })

  it('renders children when nothing throws', () => {
    render(
      <ErrorBoundary>
        <p>all good</p>
      </ErrorBoundary>,
    )
    expect(screen.getByText('all good')).toBeInTheDocument()
  })
})
