import type { Job } from '../api/types'

/** Human relative time, e.g. "3 days ago". Returns '' for missing input. */
export function relativeTime(iso: string | null | undefined, now: Date = new Date()): string {
  if (!iso) return ''
  const then = new Date(iso)
  if (Number.isNaN(then.getTime())) return ''
  const diffMs = now.getTime() - then.getTime()
  const future = diffMs < 0
  const sec = Math.round(Math.abs(diffMs) / 1000)
  const units: [number, string][] = [
    [60, 'second'],
    [60, 'minute'],
    [24, 'hour'],
    [7, 'day'],
    [4.345, 'week'],
    [12, 'month'],
    [Number.POSITIVE_INFINITY, 'year'],
  ]
  let value = sec
  let unit = 'second'
  for (const [step, name] of units) {
    if (value < step) {
      unit = name
      break
    }
    value = value / step
    unit = name
  }
  const rounded = Math.max(1, Math.floor(value))
  const label = `${rounded} ${unit}${rounded === 1 ? '' : 's'}`
  return future ? `in ${label}` : `${label} ago`
}

/** Format a salary range, or null when nothing is disclosed. */
export function formatSalary(job: Pick<Job, 'salary_min' | 'salary_max' | 'salary_currency' | 'salary_period'>): string | null {
  const { salary_min, salary_max, salary_currency, salary_period } = job
  if (salary_min == null && salary_max == null) return null
  const cur = salary_currency ? `${salary_currency} ` : ''
  const period = salary_period ? ` / ${salary_period}` : ''
  const fmt = (n: number) => {
    if (n >= 1000) return `${Math.round(n / 1000)}k`
    return String(n)
  }
  if (salary_min != null && salary_max != null) {
    if (salary_min === salary_max) return `${cur}${fmt(salary_min)}${period}`
    return `${cur}${fmt(salary_min)}–${fmt(salary_max)}${period}`
  }
  const single = (salary_min ?? salary_max) as number
  return `${cur}${fmt(single)}${period}`
}

/** Score badge threshold bucket, per the documented 0-100 min_score scale. */
export function scoreTier(score: number | null | undefined): 'high' | 'medium' | 'low' | 'none' {
  if (score == null) return 'none'
  if (score >= 75) return 'high'
  if (score >= 50) return 'medium'
  return 'low'
}

/** The explicit marker for an attribute the data does not contain. */
export const UNKNOWN_LABEL = 'Unknown'

/** Returns the value, or null when it is effectively absent (empty/undefined/null). */
export function presentOrNull(value: string | null | undefined): string | null {
  if (value === null || value === undefined) return null
  const trimmed = String(value).trim()
  if (trimmed === '') return null
  return trimmed
}
