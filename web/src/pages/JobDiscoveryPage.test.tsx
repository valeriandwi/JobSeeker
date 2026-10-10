import { describe, expect, it, vi, beforeEach } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { JobDiscoveryPage, JobRow } from './JobDiscoveryPage'
import { renderWithProviders } from '../test/utils'
import type { Job } from '../api/types'

function makeJob(overrides: Partial<Job> = {}): Job {
  return {
    id: 'job-1',
    title: 'Senior Backend Engineer',
    company: 'Acme Corp',
    location_city: 'Berlin',
    location_country: 'DE',
    work_arrangement: 'remote',
    employment_type: 'full_time',
    seniority: 'senior',
    salary_min: 90000,
    salary_max: 110000,
    salary_currency: 'USD',
    salary_period: 'year',
    published_at: '2026-10-01T00:00:00Z',
    expires_at: null,
    status: 'new',
    first_discovered_at: '2026-10-05T00:00:00Z',
    last_verified_at: null,
    match_score: 88,
    match_explanation: [],
    sources: [
      {
        source_id: 'greenhouse',
        display_name: 'Greenhouse',
        url: 'https://boards.greenhouse.io/acme',
        apply_url: 'https://boards.greenhouse.io/acme/jobs/1',
        attribution_text: 'Powered by Greenhouse',
        attribution_url: 'https://greenhouse.io',
        backlink_required: true,
      },
    ],
    attributes_verified: ['salary'],
    ...overrides,
  }
}

function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status >= 400 ? 'Error' : 'OK',
    text: async () => JSON.stringify(body),
    json: async () => body,
  } as unknown as Response
}

describe('JobDiscoveryPage', () => {
  beforeEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows a loading state then the success list with job rows', async () => {
    const jobs = [makeJob(), makeJob({ id: 'job-2', title: 'Data Engineer', company: 'Globex', match_score: 40 })]
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(jsonResponse({ jobs, total: 2, page: 1, per_page: 20 })),
    )

    renderWithProviders(<JobDiscoveryPage />, { route: '/jobs' })

    expect(screen.getByText(/Loading jobs/i)).toBeInTheDocument()

    expect(await screen.findByText('Senior Backend Engineer')).toBeInTheDocument()
    expect(screen.getByText('Globex')).toBeInTheDocument()
    expect(screen.getByText('Match 88')).toBeInTheDocument()
    expect(screen.getByText('Match 40')).toBeInTheDocument()
    expect(screen.queryByText(/Loading jobs/i)).not.toBeInTheDocument()
  })

  it('renders the error state when the request fails with an envelope', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        jsonResponse(
          {
            error: {
              code: 'unauthorized',
              message: 'You must be signed in.',
              details: [],
              request_id: 'req-xyz',
            },
          },
          401,
        ),
      ),
    )

    renderWithProviders(<JobDiscoveryPage />, { route: '/jobs' })

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('You must be signed in.')
  })

  it('sends the documented query params from the URL', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ jobs: [], total: 0, page: 1, per_page: 20 }))
    vi.stubGlobal('fetch', fetchMock)

    renderWithProviders(<JobDiscoveryPage />, { route: '/jobs?q=rust&arrangement=remote&min_score=60' })

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    const calledUrl = String(fetchMock.mock.calls[0]?.[0])
    expect(calledUrl).toContain('q=rust')
    expect(calledUrl).toContain('arrangement=remote')
    expect(calledUrl).toContain('min_score=60')
  })
})

describe('JobRow unknown handling', () => {
  it('renders Unknown chips when attributes are absent and Never an empty cell', () => {
    const job = makeJob({
      company: '',
      location_city: null,
      location_country: null,
      work_arrangement: null,
      employment_type: null,
      salary_min: null,
      salary_max: null,
      match_score: null,
      first_discovered_at: null,
      sources: [],
    })
    const { container } = renderWithProviders(
      <table>
        <tbody>
          <JobRow job={job} />
        </tbody>
      </table>,
    )
    const unknowns = screen.getAllByText('Unknown')
    // location, employment type, arrangement, first discovered
    expect(unknowns.length).toBeGreaterThanOrEqual(4)
    expect(screen.getByText('Not disclosed')).toBeInTheDocument()
    // no empty <td> cells
    for (const td of Array.from(container.querySelectorAll('td'))) {
      expect(td.textContent?.trim().length ?? 0).toBeGreaterThan(0)
    }
  })

  it('renders a source attribution note when backlink is required', () => {
    renderWithProviders(
      <table>
        <tbody>
          <JobRow job={makeJob()} />
        </tbody>
      </table>,
    )
    expect(screen.getByText(/Attribution required/i)).toBeInTheDocument()
    expect(screen.getByText('Open original ↗')).toBeInTheDocument()
  })
})

describe('Refresh matches', () => {
  beforeEach(() => {
    vi.unstubAllGlobals()
  })

  /** fetch stub that answers the jobs list, the run and the rescore endpoints. */
  function stubApi(runBody: unknown, runStatus = 200, rescored = 17) {
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      const target = String(url)
      if (target.includes('/api/v1/searches/run')) {
        return Promise.resolve(jsonResponse(runBody, runStatus))
      }
      if (target.includes('/api/v1/jobs/rescore')) {
        return Promise.resolve(jsonResponse({ rescored }))
      }
      return Promise.resolve(jsonResponse({ jobs: [], total: 0, page: 1, per_page: 20 }))
    })
    vi.stubGlobal('fetch', fetchMock)
    return fetchMock
  }

  it('POSTs a manual run, re-scores the discovered jobs and reports both', async () => {
    const fetchMock = stubApi({
      run: {
        id: 'run-1',
        trigger: 'manual',
        status: 'succeeded',
        started_at: '2026-10-10T00:00:00Z',
        finished_at: '2026-10-10T00:00:07Z',
        error_summary: null,
        stats: { jobs_created: 12, jobs_duplicated: 3 },
      },
    })

    renderWithProviders(<JobDiscoveryPage />, { route: '/jobs' })

    fireEvent.click(await screen.findByRole('button', { name: /Refresh matches/i }))

    // A run only scores what it fetches, so the refresh must also re-score the
    // jobs already stored against the current (CV-derived) profile.
    expect(await screen.findByText(/Refreshed: 12 new, 3 duplicates · re-scored 17/i)).toBeInTheDocument()
    const runCall = fetchMock.mock.calls.find(([url]) => String(url).includes('/api/v1/searches/run'))
    expect(runCall).toBeDefined()
    expect((runCall?.[1] as RequestInit)?.method).toBe('POST')
    const rescoreCall = fetchMock.mock.calls.find(([url]) => String(url).includes('/api/v1/jobs/rescore'))
    expect(rescoreCall).toBeDefined()
    expect((rescoreCall?.[1] as RequestInit)?.method).toBe('POST')
  })

  it('surfaces the run failure reason instead of claiming a refresh', async () => {
    // The state after the catalog is emptied: nothing to search.
    stubApi({
      run: {
        id: 'run-2',
        trigger: 'manual',
        status: 'failed',
        started_at: '2026-10-10T00:00:00Z',
        finished_at: '2026-10-10T00:00:01Z',
        error_summary: 'no sources are configured: the source catalog is empty',
        stats: { jobs_created: 0 },
      },
    })

    renderWithProviders(<JobDiscoveryPage />, { route: '/jobs' })
    fireEvent.click(await screen.findByRole('button', { name: /Refresh matches/i }))

    expect(
      await screen.findByText(/no sources are configured: the source catalog is empty/i),
    ).toBeInTheDocument()
    expect(screen.queryByText(/Refreshed:/i)).not.toBeInTheDocument()
  })

  it('shows a partly-refreshed run as partial', async () => {
    stubApi({
      run: {
        id: 'run-3',
        trigger: 'manual',
        status: 'partial',
        started_at: '2026-10-10T00:00:00Z',
        finished_at: '2026-10-10T00:00:09Z',
        error_summary: 'personio: timeout',
        stats: { jobs_created: 5 },
      },
    })

    renderWithProviders(<JobDiscoveryPage />, { route: '/jobs' })
    fireEvent.click(await screen.findByRole('button', { name: /Refresh matches/i }))

    expect(await screen.findByText(/Partly refreshed: 5 new — personio: timeout/i)).toBeInTheDocument()
  })

  it('reports a transport failure through the error banner', async () => {
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      if (String(url).includes('/api/v1/searches/run')) {
        return Promise.reject(new Error('network down'))
      }
      return Promise.resolve(jsonResponse({ jobs: [], total: 0, page: 1, per_page: 20 }))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderWithProviders(<JobDiscoveryPage />, { route: '/jobs' })
    fireEvent.click(await screen.findByRole('button', { name: /Refresh matches/i }))

    expect(await screen.findByRole('alert')).toBeInTheDocument()
  })
})
