import { afterEach, describe, expect, it, vi } from 'vitest'
import { API_BASE_URL, ApiError, errorMessage, http, request } from './client'

interface Call {
  url: string
  init?: RequestInit
}

/** Install a fetch stub that records calls and replies with `responder`. */
function stubFetch(responder: (call: Call) => Response): Call[] {
  const calls: Call[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const call: Call = { url: String(input), init }
      calls.push(call)
      return responder(call)
    }),
  )
  return calls
}

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

function emptyResponse(status: number): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: '',
    headers: new Headers(),
    json: async () => ({}),
    text: async () => '',
  } as unknown as Response
}

/** Await a promise expected to reject and return the typed error. */
async function capture<T>(promise: Promise<T>): Promise<unknown> {
  try {
    await promise
    throw new Error('expected the request to reject')
  } catch (err) {
    return err
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('API base URL', () => {
  it('defaults to same-origin so the session cookie stays first-party', () => {
    // An absolute default is what made login look broken: the cookie was stored
    // for one host (127.0.0.1) and never sent from the page on another
    // (localhost), because that request is cross-site.
    expect(API_BASE_URL).toBe('')
  })

  it('resolves relative paths against the current origin', async () => {
    const calls = stubFetch(() => jsonResponse({ jobs: [] }))

    await http.get('/api/v1/jobs', { per_page: 25, page: 1 })

    expect(calls[0].url).toBe(`${window.location.origin}/api/v1/jobs?per_page=25&page=1`)
  })

  it('always sends credentials so the cookie travels with the request', async () => {
    const calls = stubFetch(() => jsonResponse({ user: null }))

    await http.get('/api/v1/me')

    expect(calls[0].init?.credentials).toBe('include')
  })

  it('omits empty query values but keeps zero', async () => {
    const calls = stubFetch(() => jsonResponse({ jobs: [] }))

    await http.get('/api/v1/jobs', { q: '', min_score: 0, country: 'MY' })

    expect(calls[0].url).toContain('country=MY')
    expect(calls[0].url).toContain('min_score=0')
    expect(calls[0].url).not.toContain('q=')
  })
})

describe('error handling', () => {
  it('parses the documented error envelope, including field details', async () => {
    stubFetch(() =>
      jsonResponse(
        {
          error: {
            code: 'validation_error',
            message: 'invalid search configuration',
            details: [{ field: 'min_score', message: 'must be between 0 and 100' }],
            request_id: 'req-1',
          },
        },
        400,
      ),
    )

    const err = await capture(request('/api/v1/preferences', { method: 'PUT', body: {} }))

    expect(err).toBeInstanceOf(ApiError)
    const apiErr = err as ApiError
    expect(apiErr.code).toBe('validation_error')
    expect(apiErr.fieldError('min_score')).toBe('must be between 0 and 100')
    expect(apiErr.requestId).toBe('req-1')
    expect(errorMessage(err)).toBe('invalid search configuration')
  })

  it('flags 401 as unauthorized so the app can route to /login', async () => {
    stubFetch(() =>
      jsonResponse({ error: { code: 'unauthorized', message: 'authentication required' } }, 401),
    )

    const err = (await capture(request('/api/v1/me'))) as ApiError
    expect(err.isUnauthorized).toBe(true)
  })

  it('survives a non-JSON error body', async () => {
    stubFetch(() => {
      return {
        ok: false,
        status: 502,
        statusText: 'Bad Gateway',
        headers: new Headers(),
        json: async () => {
          throw new Error('not json')
        },
        text: async () => '<html>bad gateway</html>',
      } as unknown as Response
    })

    const err = (await capture(request('/api/v1/jobs'))) as ApiError
    expect(err).toBeInstanceOf(ApiError)
    expect(err.code).toBe('internal_error')
  })

  it('treats 204 as an empty success', async () => {
    stubFetch(() => emptyResponse(204))

    await expect(http.del('/api/v1/jobs/abc/save')).resolves.toBeUndefined()
  })
})
