import type { ReactNode } from 'react'

export function Spinner({ label = 'Loading…' }: { label?: string }) {
  return (
    <span role="status" aria-live="polite" className="inline-flex items-center gap-2 text-sm text-slate-600">
      <span
        aria-hidden="true"
        className="h-4 w-4 animate-spin rounded-full border-2 border-slate-300 border-t-brand-600"
      />
      {label}
    </span>
  )
}

export function LoadingState({ label = 'Loading…' }: { label?: string }) {
  return (
    <div className="flex justify-center py-10">
      <Spinner label={label} />
    </div>
  )
}

export function EmptyState({ title, description }: { title: string; description?: string }) {
  return (
    <div className="rounded-lg border border-dashed border-slate-300 bg-slate-50 px-6 py-10 text-center">
      <p className="text-sm font-medium text-slate-700">{title}</p>
      {description && <p className="mt-1 text-sm text-slate-500">{description}</p>}
    </div>
  )
}

export function ErrorState({
  message,
  onRetry,
  children,
}: {
  message: string
  onRetry?: () => void
  children?: ReactNode
}) {
  return (
    <div role="alert" className="rounded-lg border border-red-200 bg-red-50 px-4 py-4 text-sm text-red-800">
      <p className="font-medium">{message}</p>
      {children}
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="mt-3 rounded-md border border-red-300 bg-white px-3 py-1.5 text-xs font-medium text-red-700 hover:bg-red-100"
        >
          Retry
        </button>
      )}
    </div>
  )
}
