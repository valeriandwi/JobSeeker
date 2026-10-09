import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ErrorBanner } from './ErrorBanner'
import { ApiError } from '../api/client'

describe('ErrorBanner error-envelope rendering', () => {
  it('renders the readable message from the envelope', () => {
    const err = new ApiError(400, {
      code: 'validation_error',
      message: 'The submitted data is invalid.',
      details: [],
      request_id: 'req-123',
    })
    render(<ErrorBanner error={err} />)
    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(screen.getByText('The submitted data is invalid.')).toBeInTheDocument()
    expect(screen.getByText(/req-123/)).toBeInTheDocument()
  })

  it('renders each field-level detail with its field name', () => {
    const err = new ApiError(422, {
      code: 'validation_error',
      message: 'Validation failed',
      details: [
        { field: 'email', message: 'must be a valid email' },
        { field: 'password', message: 'must be at least 8 characters' },
      ],
    })
    render(<ErrorBanner error={err} />)
    expect(screen.getByText('email')).toBeInTheDocument()
    expect(screen.getByText(/must be a valid email/)).toBeInTheDocument()
    expect(screen.getByText('password')).toBeInTheDocument()
    expect(screen.getByText(/must be at least 8 characters/)).toBeInTheDocument()
  })

  it('exposes fieldError lookup for form binding', () => {
    const err = new ApiError(400, {
      code: 'validation_error',
      message: 'bad',
      details: [{ field: 'email', message: 'taken' }],
    })
    expect(err.fieldError('email')).toBe('taken')
    expect(err.fieldError('other')).toBeUndefined()
  })

  it('falls back to the plain message for non-ApiError values', () => {
    render(<ErrorBanner error={new Error('boom')} />)
    expect(screen.getByText('boom')).toBeInTheDocument()
  })

  it('renders nothing when there is no error', () => {
    const { container } = render(<ErrorBanner error={null} />)
    expect(container).toBeEmptyDOMElement()
  })
})
