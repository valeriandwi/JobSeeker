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
    description_summary: 'Build and maintain our Go services.',
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
    // The tracking lookups (/saved-jobs, /applications) now load alongside the
    // list, so pick the list call rather than assuming it is first.
    const listCall = fetchMock.mock.calls.find(([url]) => String(url).includes('/api/v1/jobs?'))
    const calledUrl = String(listCall?.[0])
    expect(calledUrl).toContain('q=rust')
    expect(calledUrl).toContain('arrangement=remote')
    expect(calledUrl).toContain('min_score=60')
  })

  it('explains what the page shows', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ jobs: [], total: 0, page: 1, per_page: 20 }))
    vi.stubGlobal('fetch', fetchMock)

    renderWithProviders(<JobDiscoveryPage />, { route: '/jobs' })

    expect(await screen.findByRole('heading', { name: /job discovery/i })).toBeInTheDocument()
    expect(
      screen.getByText(/Vacancies found by your enabled sources, scored against your confirmed profile/i),
    ).toBeInTheDocument()
  })

  it('sorts by first discovered by default and offers it in the list', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ jobs: [], total: 0, page: 1, per_page: 20 }))
    vi.stubGlobal('fetch', fetchMock)

    renderWithProviders(<JobDiscoveryPage />, { route: '/jobs' })

    const select = await screen.findByLabelText(/sort by/i)
    expect((select as HTMLSelectElement).value).toBe('first_discovered')
    expect(screen.getByRole('option', { name: /first discovered/i })).toBeInTheDocument()

    // No sort param is sent, so the URL stays clean; the server applies the same
    // default. What must never happen is the old score-first default creeping back.
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    const listCall = fetchMock.mock.calls.find(([url]) => String(url).includes('/api/v1/jobs?'))
    expect(String(listCall?.[0])).not.toContain('sort=score')
  })

  it('keeps an explicit sort from the URL', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ jobs: [], total: 0, page: 1, per_page: 20 }))
    vi.stubGlobal('fetch', fetchMock)

    renderWithProviders(<JobDiscoveryPage />, { route: '/jobs?sort=score' })

    const select = await screen.findByLabelText(/sort by/i)
    expect((select as HTMLSelectElement).value).toBe('score')
  })

  it('resets back to the default sort, not the old score-first one', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ jobs: [], total: 0, page: 1, per_page: 20 }))
    vi.stubGlobal('fetch', fetchMock)

    renderWithProviders(<JobDiscoveryPage />, { route: '/jobs?sort=company&q=rust' })

    const select = await screen.findByLabelText(/sort by/i)
    expect((select as HTMLSelectElement).value).toBe('company')

    fireEvent.click(screen.getByRole('button', { name: /reset filters/i }))

    await waitFor(() =>
      expect((screen.getByLabelText(/sort by/i) as HTMLSelectElement).value).toBe('first_discovered'),
    )
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
          {/* tracking={false}: the tracking cell is icon-only by design, so it
              deliberately has no text — these assertions are about data cells. */}
          <JobRow job={job} tracking={false} />
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

  it('previews the vacancy summary and keeps the full text on the cell', () => {
    const full =
      'Develop and maintain SWITCH modules for mission-critical payment and switching platforms, plus a great deal of extra detail that will never fit inside a single table row.'
    renderWithProviders(
      <table>
        <tbody>
          <JobRow job={makeJob({ description_summary: full })} />
        </tbody>
      </table>,
    )

    const cell = screen.getByTitle(full)
    expect(cell.textContent).toContain('Develop and maintain SWITCH modules')
    expect(cell.textContent?.endsWith('…')).toBe(true)
    expect((cell.textContent ?? '').length).toBeLessThan(full.length)
  })

  it('shows Unknown when the source published no summary', () => {
    const { container } = renderWithProviders(
      <table>
        <tbody>
          <JobRow job={makeJob({ description_summary: '' })} tracking={false} />
        </tbody>
      </table>,
    )

    expect(screen.getAllByText('Unknown').length).toBeGreaterThanOrEqual(1)
    // The preview cell must never render blank.
    for (const td of Array.from(container.querySelectorAll('td'))) {
      expect(td.textContent?.trim().length ?? 0).toBeGreaterThan(0)
    }
  })

  it('shows detected technologies as clearly derived, never as source data', () => {
    renderWithProviders(
      <table>
        <tbody>
          <JobRow
            job={makeJob({
              title: 'Senior Backend Engineer',
              description_summary: 'Golang, PostgreSQL and Docker on AWS.',
              // Nothing here claims a tech stack: detection is derived from text.
              attributes_verified: [],
            })}
          />
        </tbody>
      </table>,
    )

    for (const tech of ['Golang', 'PostgreSQL', 'Docker', 'AWS']) {
      expect(screen.getByText(tech)).toBeInTheDocument()
    }
    // The cell itself says where the chips came from.
    expect(screen.getByTitle(/Detected by scanning this listing/i)).toBeInTheDocument()
  })

  it('says None detected rather than Unknown when the text names no technology', () => {
    const { container } = renderWithProviders(
      <table>
        <tbody>
          <JobRow job={makeJob({ title: 'Warehouse Operative', description_summary: 'Pick and pack orders.' })} tracking={false} />
        </tbody>
      </table>,
    )

    // "we looked and found none" is a different statement from "the source does
    // not publish this" — the row must not conflate them.
    expect(screen.getByText('None detected')).toBeInTheDocument()
    for (const td of Array.from(container.querySelectorAll('td'))) {
      expect(td.textContent?.trim().length ?? 0).toBeGreaterThan(0)
    }
  })

  it('caps the chips and keeps the full list reachable on the overflow badge', () => {
    const stack = ['Golang', 'React', 'PostgreSQL', 'AWS', 'Kubernetes', 'Docker']
    renderWithProviders(
      <table>
        <tbody>
          <JobRow
            job={makeJob({
              title: 'Platform Engineer',
              // Prose around the list, so the overflow badge's title is not
              // ambiguous with the description cell's.
              description_summary: `You will work with ${stack.join(', ')} every day.`,
            })}
          />
        </tbody>
      </table>,
    )

    expect(screen.getByText('+2')).toBeInTheDocument()
    expect(screen.getByTitle(stack.join(', '))).toBeInTheDocument()
  })

  it('colour-codes employment type and arrangement without hiding the value', () => {
    renderWithProviders(
      <table>
        <tbody>
          <JobRow
            job={makeJob({
              employment_type: 'full_time',
              work_arrangement: 'hybrid',
              attributes_verified: ['employment_type', 'work_arrangement'],
            })}
          />
        </tbody>
      </table>,
    )

    // The label still states the value: colour is a scanning aid, not the signal.
    expect(screen.getByText('Full time').className).toContain('bg-green-50')
    expect(screen.getByText('Hybrid').className).toContain('bg-amber-50')
    expect(screen.getAllByTitle('Published by the source.')).toHaveLength(2)
  })

  it('does not claim source verification the source never gave', () => {
    renderWithProviders(
      <table>
        <tbody>
          <JobRow
            job={makeJob({
              employment_type: 'contract',
              work_arrangement: 'onsite',
              attributes_verified: [],
            })}
          />
        </tbody>
      </table>,
    )

    expect(screen.getByText('Contract').className).toContain('bg-amber-50')
    expect(screen.getByText('On-site').className).toContain('bg-slate-100')
    expect(screen.getAllByTitle(/did not mark it as verified/i)).toHaveLength(2)
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

describe('tracking from the job list', () => {
  it('saves a job and records that an application was sent, from the row', async () => {
    const calls: string[] = []
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      const target = String(url)
      calls.push(`${init?.method ?? 'GET'} ${target}`)
      if (target.includes('/saved-jobs')) return Promise.resolve(jsonResponse({ saved_jobs: [] }))
      if (target.includes('/applications')) return Promise.resolve(jsonResponse({ applications: [] }))
      if (target.includes('/api/v1/jobs')) {
        return Promise.resolve(jsonResponse({ jobs: [makeJob()], total: 1, page: 1, per_page: 20 }))
      }
      return Promise.resolve(jsonResponse({}))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderWithProviders(<JobDiscoveryPage />, { route: '/jobs' })

    fireEvent.click(await screen.findByRole('button', { name: 'Save job' }))
    await waitFor(() =>
      expect(calls.some((c) => c.startsWith('POST ') && c.includes('/jobs/job-1/save'))).toBe(true),
    )

    fireEvent.click(screen.getByRole('button', { name: 'Mark application as sent' }))
    await waitFor(() =>
      expect(calls.some((c) => c.startsWith('PUT ') && c.includes('/applications/job-1'))).toBe(true),
    )
  })

  it('reflects an already-tracked job and offers to take the application back', async () => {
    const calls: string[] = []
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      const target = String(url)
      calls.push(`${init?.method ?? 'GET'} ${target}`)
      // Real wire shape: `saved_jobs`, flat rows. Reading `jobs` here is what
      // kept the bookmark icon unpressed for saved jobs.
      if (target.includes('/saved-jobs')) {
        return Promise.resolve(
          jsonResponse({
            saved_jobs: [
              {
                job_id: 'job-1',
                title: 'Senior Backend Engineer',
                company: 'Acme Corp',
                url: 'https://kalibrr.test/jobs/1',
                source: 'kalibrr',
                match_score: 88,
                saved_at: '2026-10-10T00:00:00Z',
              },
            ],
          }),
        )
      }
      if (target.includes('/applications')) {
        return Promise.resolve(
          jsonResponse({
            applications: [
              {
                job_id: 'job-1',
                title: 'Senior Backend Engineer',
                company: 'Acme Corp',
                url: 'https://kalibrr.test/jobs/1',
                status: 'applied',
                applied_at: '2026-10-10T00:00:00Z',
              },
            ],
          }),
        )
      }
      if (target.includes('/api/v1/jobs')) {
        return Promise.resolve(jsonResponse({ jobs: [makeJob()], total: 1, page: 1, per_page: 20 }))
      }
      return Promise.resolve(jsonResponse({}))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderWithProviders(<JobDiscoveryPage />, { route: '/jobs' })

    // Both facts are visible on the row without opening anything.
    expect(await screen.findByRole('button', { name: /saved job/i })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(screen.getByRole('button', { name: /application sent/i }))

    // Un-marking goes back to "saved" and clears the timestamp.
    await waitFor(() =>
      expect(calls.some((c) => c.startsWith('PUT ') && c.includes('/applications/job-1'))).toBe(true),
    )
  })

  it('puts the tracking icons in the first column', async () => {
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      const target = String(url)
      if (target.includes('/saved-jobs')) return Promise.resolve(jsonResponse({ saved_jobs: [] }))
      if (target.includes('/applications')) return Promise.resolve(jsonResponse({ applications: [] }))
      return Promise.resolve(jsonResponse({ jobs: [makeJob()], total: 1, page: 1, per_page: 20 }))
    })
    vi.stubGlobal('fetch', fetchMock)

    renderWithProviders(<JobDiscoveryPage />, { route: '/jobs' })

    const headers = (await screen.findAllByRole('columnheader')).map((h) => h.textContent)
    expect(headers[0]).toBe('Track')

    // And the controls really are in the row's first cell.
    const firstCell = screen.getAllByRole('row')[1]?.querySelector('td')
    expect(firstCell?.querySelector('button')).not.toBeNull()
  })
})
