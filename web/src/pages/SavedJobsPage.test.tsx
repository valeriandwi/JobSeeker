import { describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { SavedJobsPage, mergeTrackerEntries } from './SavedJobsPage'
import { renderWithProviders } from '../test/utils'
import type { Application, SavedJob } from '../api/types'

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

/**
 * Shaped exactly like a row from GET /saved-jobs (flat, denormalised).
 *
 * These fixtures deliberately mirror the wire format: an earlier version of
 * this file stubbed the shape the page *expected* (`{jobs: [...]}`), so the
 * suite passed while the real page rendered empty for every bookmark.
 */
function makeSavedJob(overrides: Partial<SavedJob> = {}): SavedJob {
  return {
    job_id: 'job-1',
    title: 'Senior Backend Engineer',
    company: 'Acme Corp',
    url: 'https://kalibrr.test/jobs/1',
    source: 'kalibrr',
    match_score: 88,
    saved_at: '2026-10-10T09:00:00Z',
    ...overrides,
  }
}

function makeApplication(overrides: Partial<Application> = {}): Application {
  return {
    job_id: 'job-1',
    title: 'Senior Backend Engineer',
    company: 'Acme Corp',
    url: 'https://kalibrr.test/jobs/1',
    status: 'applied',
    applied_at: '2026-10-10T09:00:00Z',
    notes: null,
    follow_up_at: null,
    updated_at: '2026-10-10T09:00:00Z',
    ...overrides,
  }
}

/** Fetch stub answering the two endpoints with their real payload keys. */
function stub(savedJobs: SavedJob[], applications: Application[]) {
  const fetchMock = vi.fn().mockImplementation((url: string) => {
    const target = String(url)
    if (target.includes('/saved-jobs')) return Promise.resolve(jsonResponse({ saved_jobs: savedJobs }))
    if (target.includes('/applications')) return Promise.resolve(jsonResponse({ applications }))
    return Promise.resolve(jsonResponse({}))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('mergeTrackerEntries', () => {
  it('lists a bookmark with its source and score', () => {
    const entries = mergeTrackerEntries([makeSavedJob()], [])

    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({
      jobId: 'job-1',
      title: 'Senior Backend Engineer',
      source: 'kalibrr',
      matchScore: 88,
      bookmarked: true,
    })
    expect(entries[0].application).toBeUndefined()
  })

  it('lists an application that is not bookmarked', () => {
    // The regression this exists for: an applied job whose bookmark was removed
    // used to disappear from a page called "Application Tracker".
    const entries = mergeTrackerEntries([], [makeApplication({ job_id: 'job-9', title: 'Platform Engineer' })])

    expect(entries).toHaveLength(1)
    expect(entries[0]).toMatchObject({
      jobId: 'job-9',
      title: 'Platform Engineer',
      bookmarked: false,
      source: '', // applications carry no source
      matchScore: null, // and no score
    })
    expect(entries[0].application?.status).toBe('applied')
  })

  it('lists a job that is both, exactly once', () => {
    const entries = mergeTrackerEntries([makeSavedJob()], [makeApplication()])

    expect(entries).toHaveLength(1)
    expect(entries[0].bookmarked).toBe(true)
    expect(entries[0].application?.status).toBe('applied')
  })
})

describe('SavedJobsPage', () => {
  it('renders the bookmarks returned by the API', async () => {
    // Guards the payload contract: the handler emits `saved_jobs`, so a page
    // reading `jobs` would show the empty state here.
    stub([makeSavedJob(), makeSavedJob({ job_id: 'job-2', title: 'Data Engineer' })], [])

    renderWithProviders(<SavedJobsPage />, { route: '/saved' })

    expect(await screen.findByText('Senior Backend Engineer')).toBeInTheDocument()
    expect(screen.getByText('Data Engineer')).toBeInTheDocument()
    expect(screen.queryByText('Nothing tracked yet')).not.toBeInTheDocument()
    expect(screen.getAllByRole('listitem')).toHaveLength(2)
  })

  it('shows an applied job that is no longer bookmarked', async () => {
    stub([], [makeApplication()])

    renderWithProviders(<SavedJobsPage />, { route: '/saved' })

    expect(await screen.findByText('Senior Backend Engineer')).toBeInTheDocument()
    expect(screen.getByText('applied')).toBeInTheDocument()
    // Says why it is listed without a bookmark, and offers no destructive action
    // because this page never deletes an application.
    expect(screen.getByText('not bookmarked')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /open original/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /unsave/i })).not.toBeInTheDocument()
  })

  it('shows a bookmarked job with its status and an Unsave button', async () => {
    stub([makeSavedJob()], [])

    renderWithProviders(<SavedJobsPage />, { route: '/saved' })

    expect(await screen.findByText('Senior Backend Engineer')).toBeInTheDocument()
    expect(screen.getByText('saved')).toBeInTheDocument()
    expect(screen.getByText(/Acme Corp/)).toBeInTheDocument()
    expect(screen.getByText(/kalibrr/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /unsave/i })).toBeInTheDocument()
    expect(screen.queryByText('not bookmarked')).not.toBeInTheDocument()
  })

  it('never lists the same vacancy twice', async () => {
    stub([makeSavedJob()], [makeApplication()])

    renderWithProviders(<SavedJobsPage />, { route: '/saved' })

    const titles = await screen.findAllByText('Senior Backend Engineer')
    expect(titles).toHaveLength(1)
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
    expect(screen.getByText('applied')).toBeInTheDocument()
  })

  it('shows the empty state only when nothing is tracked at all', async () => {
    stub([], [])

    renderWithProviders(<SavedJobsPage />, { route: '/saved' })

    expect(await screen.findByText('Nothing tracked yet')).toBeInTheDocument()
  })

  it('shows an updated status from the applications record', async () => {
    stub([makeSavedJob()], [makeApplication({ status: 'interviewing' })])

    renderWithProviders(<SavedJobsPage />, { route: '/saved' })

    expect(await screen.findByText('interviewing')).toBeInTheDocument()
  })
})
