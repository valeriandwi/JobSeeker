import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { JobTrackingButtons, applicationStatusLabel, hasApplied } from './JobTrackingButtons'

describe('JobTrackingButtons', () => {
  const noop = () => {}

  it('offers both actions for an untracked job', () => {
    render(
      <JobTrackingButtons bookmarked={false} applicationStatus={null} onToggleSave={noop} onToggleApplied={noop} />,
    )

    expect(screen.getByRole('button', { name: 'Save job' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: 'Mark application as sent' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('calls back for each action', () => {
    const onToggleSave = vi.fn()
    const onToggleApplied = vi.fn()
    render(
      <JobTrackingButtons
        bookmarked={false}
        applicationStatus={null}
        onToggleSave={onToggleSave}
        onToggleApplied={onToggleApplied}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Save job' }))
    fireEvent.click(screen.getByRole('button', { name: 'Mark application as sent' }))

    expect(onToggleSave).toHaveBeenCalledTimes(1)
    expect(onToggleApplied).toHaveBeenCalledTimes(1)
  })

  it('shows a bookmarked job as saved and still toggleable', () => {
    render(
      <JobTrackingButtons bookmarked applicationStatus="saved" onToggleSave={noop} onToggleApplied={noop} />,
    )

    // A "saved" application record means tracked-but-not-sent, so the button
    // still offers to mark the application as sent.
    expect(screen.getByRole('button', { name: /saved job/i })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Mark application as sent' })).toBeInTheDocument()
  })

  it('shows an applied job and offers to take it back', () => {
    render(
      <JobTrackingButtons bookmarked applicationStatus="applied" onToggleSave={noop} onToggleApplied={noop} />,
    )

    expect(screen.getByRole('button', { name: /application sent/i })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByRole('button', { name: 'Mark application as sent' })).not.toBeInTheDocument()
  })

  it('shows later pipeline stages as a status, not a toggle', () => {
    render(
      <JobTrackingButtons bookmarked applicationStatus="interviewing" onToggleSave={noop} onToggleApplied={noop} />,
    )

    // A toggle cannot express "interviewing" without destroying it, so this is
    // read-only and the Saved Jobs page owns the detail. The stage travels in
    // the accessible name because the icon has no text of its own.
    expect(screen.getByText('Application status: Interviewing')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /application sent/i })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /saved job/i })).toBeInTheDocument()
  })

  it('disables both controls while a mutation is in flight', () => {
    render(
      <JobTrackingButtons bookmarked={false} applicationStatus={null} pending onToggleSave={noop} onToggleApplied={noop} />,
    )

    expect(screen.getByRole('button', { name: 'Save job' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Mark application as sent' })).toBeDisabled()
  })

  it('draws the icons filled or outlined to show state', () => {
    const { container: untracked } = render(
      <JobTrackingButtons bookmarked={false} applicationStatus={null} onToggleSave={noop} onToggleApplied={noop} />,
    )
    // Both icons outlined: nothing saved, nothing sent.
    expect(untracked.querySelectorAll('svg[fill="none"]').length).toBe(2)

    const { container: tracked } = render(
      <JobTrackingButtons bookmarked applicationStatus="applied" onToggleSave={noop} onToggleApplied={noop} />,
    )
    // Both filled: saved and applied. With no visible text this fill IS the state.
    expect(tracked.querySelectorAll('svg[fill="currentColor"]').length).toBe(2)
  })

  it('renders the icons as decoration only', () => {
    const { container } = render(
      <JobTrackingButtons bookmarked applicationStatus="applied" onToggleSave={noop} onToggleApplied={noop} />,
    )

    // Screen readers get the button's label, not the SVG.
    for (const svg of Array.from(container.querySelectorAll('svg'))) {
      expect(svg).toHaveAttribute('aria-hidden', 'true')
    }
  })
})

describe('hasApplied', () => {
  it('counts every stage that can only exist after sending', () => {
    for (const status of ['applied', 'interviewing', 'rejected', 'accepted', 'withdrawn']) {
      expect(hasApplied(status)).toBe(true)
    }
  })

  it('does not count untracked or merely-saved jobs', () => {
    for (const status of ['saved', 'new', 'active', '', null, undefined]) {
      expect(hasApplied(status)).toBe(false)
    }
  })

  it('therefore catches what job.status hides', () => {
    // The tracker maps accepted/withdrawn onto the job status "saved", so a job
    // whose application was accepted would look untouched by job.status alone.
    expect(hasApplied('accepted')).toBe(true)
  })
})

describe('applicationStatusLabel', () => {
  it('labels known statuses', () => {
    expect(applicationStatusLabel('applied')).toBe('Applied')
    expect(applicationStatusLabel('interviewing')).toBe('Interviewing')
    expect(applicationStatusLabel('withdrawn')).toBe('Withdrawn')
  })

  it('falls back to the raw value rather than hiding it', () => {
    expect(applicationStatusLabel('ghosted')).toBe('ghosted')
  })
})
