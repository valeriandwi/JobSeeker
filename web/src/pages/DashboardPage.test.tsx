import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { DashboardPage } from './DashboardPage'
import { renderWithProviders } from '../test/utils'

function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as unknown as Response
}

/**
 * Regression: the API once returned `recent_runs: null` (a Go nil slice), and
 * the dashboard called .length on it, which crashed React and left the user with
 * a blank page. A list field arriving as null must degrade to an empty list.
 */
describe('DashboardPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renders empty states when list fields arrive as null', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/dashboard/summary')) {
          return jsonResponse({
            total_jobs: 0,
            new_since_last_run: 0,
            high_match_count: 0,
            last_run: null,
            next_run_at: null,
            schedule: null,
            recent_runs: null,
          })
        }
        if (url.includes('/jobs')) {
          return jsonResponse({ jobs: null, total: 0, page: 1, per_page: 5 })
        }
        return jsonResponse({ error: { code: 'not_found', message: 'unexpected call' } }, 404)
      }),
    )

    renderWithProviders(<DashboardPage />)

    await waitFor(() => expect(screen.getByText('Dashboard')).toBeInTheDocument())
    expect(screen.getByText('No recent runs')).toBeInTheDocument()
    expect(screen.getByText('No matches yet')).toBeInTheDocument()
    expect(screen.getByText('No runs yet.')).toBeInTheDocument()
  })

  it('renders counters and recent runs when data is present', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/dashboard/summary')) {
          return jsonResponse({
            total_jobs: 281,
            new_since_last_run: 12,
            high_match_count: 4,
            last_run: { id: 'r1', status: 'partial', started_at: '2026-10-09T01:00:00Z', trigger: 'schedule', error_summary: 'remotive: timeout' },
            next_run_at: '2026-10-10T01:00:00Z',
            schedule: null,
            recent_runs: [
              { id: 'r1', status: 'partial', started_at: '2026-10-09T01:00:00Z', trigger: 'schedule', error_summary: '' },
            ],
          })
        }
        return jsonResponse({ jobs: [], total: 0, page: 1, per_page: 5 })
      }),
    )

    renderWithProviders(<DashboardPage />)

    await waitFor(() => expect(screen.getByText('281')).toBeInTheDocument())
    expect(screen.getByText('12')).toBeInTheDocument()
    expect(screen.getByText('4')).toBeInTheDocument()
    expect(screen.getByText(/remotive: timeout/)).toBeInTheDocument()
    expect(screen.queryByText('No recent runs')).not.toBeInTheDocument()
  })
})
