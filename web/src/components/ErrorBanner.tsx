import { ApiError, errorMessage } from '../api/client'
import type { FieldError } from '../api/types'

/**
 * Renders the API error envelope: a readable message plus, when present, the
 * field-level details[].field list.
 */
export function ErrorBanner({ error }: { error: unknown }) {
  if (!error) return null
  const message = errorMessage(error)
  const details: FieldError[] = error instanceof ApiError ? error.details : []
  const requestId = error instanceof ApiError ? error.requestId : undefined
  return (
    <div role="alert" className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">
      <p className="font-medium">{message}</p>
      {details.length > 0 && (
        <ul className="mt-2 list-disc space-y-1 pl-5">
          {details.map((d) => (
            <li key={d.field}>
              <span className="font-semibold">{d.field}</span>: {d.message}
            </li>
          ))}
        </ul>
      )}
      {requestId && <p className="mt-2 text-xs text-red-500">Request ID: {requestId}</p>}
    </div>
  )
}
