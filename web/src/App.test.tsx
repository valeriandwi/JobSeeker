import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { AppRoutes } from './App'
import { AuthProvider } from './hooks/useAuth'
import { makeTestQueryClient } from './test/utils'

function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: '',
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as unknown as Response
}

const user = { id: 'u-1', email: 'me@example.com', display_name: 'Me', created_at: '2026-10-09T00:00:00Z' }

function renderApp() {
  return render(
    <QueryClientProvider client={makeTestQueryClient()}>
      <MemoryRouter initialEntries={['/']}>
        <AuthProvider>
          <AppRoutes />
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

/** Stub the API: /me answers per the test, everything else returns empty data. */
function stubApi(meResponse: () => Response) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/v1/me')) return meResponse()
      if (url.includes('/api/v1/dashboard/summary')) {
        return jsonResponse({
          total_jobs: 3,
          new_since_last_run: 1,
          high_match_count: 0,
          last_run: null,
          next_run_at: null,
          schedule: null,
          recent_runs: [],
        })
      }
      if (url.includes('/api/v1/jobs')) return jsonResponse({ jobs: [], total: 0, page: 1, per_page: 5 })
      return jsonResponse({ error: { code: 'not_found', message: 'unexpected call ' + url } }, 404)
    }),
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('session bootstrap', () => {
  /**
   * Regression: a fresh load with a valid session used to redirect to /login.
   * The auth provider mirrored the query result into local state, so there was a
   * render where loading had finished but the user was still null, and
   * ProtectedRoute acted on it.
   */
  it('stays on the dashboard when a fresh load finds a valid session', async () => {
    stubApi(() => jsonResponse({ user }))

    renderApp()

    await waitFor(() => expect(screen.getByText('Dashboard')).toBeInTheDocument())
    expect(screen.queryByRole('heading', { name: /sign in/i })).not.toBeInTheDocument()
    expect(screen.getByText('Me')).toBeInTheDocument()
  })

  it('redirects to the login form when there is no session', async () => {
    stubApi(() => jsonResponse({ error: { code: 'unauthorized', message: 'authentication required' } }, 401))

    renderApp()

    await waitFor(() => expect(screen.getByRole('heading', { name: /sign in/i })).toBeInTheDocument())
    expect(screen.queryByText('Dashboard')).not.toBeInTheDocument()
  })

  it('does not treat the 401 probe as an error to display', async () => {
    stubApi(() => jsonResponse({ error: { code: 'unauthorized', message: 'authentication required' } }, 401))

    renderApp()

    await waitFor(() => expect(screen.getByRole('heading', { name: /sign in/i })).toBeInTheDocument())
    expect(screen.queryByText(/You are not signed in/)).not.toBeInTheDocument()
  })
})
