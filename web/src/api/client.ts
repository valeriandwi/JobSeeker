import type { ErrorBody, ErrorCode, FieldError } from './types'

export const API_BASE_URL: string =
  (import.meta.env?.VITE_API_BASE_URL as string | undefined) ?? 'http://127.0.0.1:8080'

const FALLBACK_MESSAGES: Record<ErrorCode, string> = {
  validation_error: 'Please correct the highlighted fields.',
  unauthorized: 'You are not signed in.',
  forbidden: 'You do not have access to this resource.',
  not_found: 'The requested resource was not found.',
  conflict: 'This conflicts with existing data.',
  payload_too_large: 'The file is too large.',
  unsupported_media_type: 'That file type is not supported.',
  rate_limited: 'Too many requests. Please slow down.',
  internal_error: 'Something went wrong on our side.',
  service_unavailable: 'The service is temporarily unavailable.',
  bad_request: 'The request was malformed.',
}

/** Typed error parsed from the API error envelope. */
export class ApiError extends Error {
  readonly code: ErrorCode
  readonly status: number
  readonly details: FieldError[]
  readonly requestId?: string

  constructor(status: number, body: ErrorBody) {
    super(body.message || FALLBACK_MESSAGES[body.code] || 'Request failed')
    this.name = 'ApiError'
    this.status = status
    this.code = body.code
    this.details = Array.isArray(body.details) ? body.details : []
    this.requestId = body.request_id
  }

  /** Message for a given form field, if the server reported one. */
  fieldError(field: string): string | undefined {
    return this.details.find((d) => d.field === field)?.message
  }

  get isUnauthorized(): boolean {
    return this.code === 'unauthorized' || this.status === 401
  }
}

/** Error used when the response is not a parseable error envelope. */
export class NetworkError extends Error {
  constructor(message = 'Network request failed') {
    super(message)
    this.name = 'NetworkError'
  }
}

async function parseError(res: Response): Promise<ApiError> {
  let body: ErrorBody
  try {
    const data = (await res.json()) as { error?: ErrorBody }
    if (data && data.error && typeof data.error.code === 'string') {
      body = data.error
    } else {
      body = { code: 'internal_error', message: res.statusText || 'Request failed' }
    }
  } catch {
    body = { code: 'internal_error', message: res.statusText || 'Request failed' }
  }
  return new ApiError(res.status, body)
}

export interface RequestOptions {
  method?: string
  query?: Record<string, string | number | boolean | undefined | null>
  body?: unknown
  formData?: FormData
  signal?: AbortSignal
}

function buildUrl(path: string, query?: RequestOptions['query']): string {
  const base = API_BASE_URL.replace(/\/+$/, '')
  const url = new URL(base + path)
  if (query) {
    for (const [key, value] of Object.entries(query)) {
      if (value === undefined || value === null || value === '') continue
      url.searchParams.set(key, String(value))
    }
  }
  return url.toString()
}

/**
 * Core request helper. Always sends the session cookie and parses the
 * documented error envelope into an ApiError.
 */
export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = 'GET', query, body, formData, signal } = options
  const headers: Record<string, string> = { Accept: 'application/json' }
  let payload: BodyInit | undefined
  if (formData) {
    payload = formData
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    payload = JSON.stringify(body)
  }

  let res: Response
  try {
    res = await fetch(buildUrl(path, query), {
      method,
      headers,
      body: payload,
      credentials: 'include',
      signal,
    })
  } catch (err) {
    throw new NetworkError(err instanceof Error ? err.message : 'Network request failed')
  }

  if (!res.ok) {
    throw await parseError(res)
  }

  if (res.status === 204) {
    return undefined as T
  }
  const text = await res.text()
  if (!text) return undefined as T
  return JSON.parse(text) as T
}

export const http = {
  get: <T>(path: string, query?: RequestOptions['query']) => request<T>(path, { query }),
  post: <T>(path: string, body?: unknown) => request<T>(path, { method: 'POST', body }),
  put: <T>(path: string, body?: unknown) => request<T>(path, { method: 'PUT', body }),
  patch: <T>(path: string, body?: unknown) => request<T>(path, { method: 'PATCH', body }),
  del: <T>(path: string, query?: RequestOptions['query']) =>
    request<T>(path, { method: 'DELETE', query }),
  upload: <T>(path: string, formData: FormData) => request<T>(path, { method: 'POST', formData }),
}

/** Normalise any thrown value into a readable message for the UI. */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof NetworkError) return err.message
  if (err instanceof Error) return err.message
  return 'Unexpected error'
}
