import type { JobFilters } from '../api/types'

/** The documented, sortable, filterable query parameter names for /api/v1/jobs. */
export const FILTER_KEYS = [
  'q',
  'country',
  'city',
  'arrangement',
  'employment_type',
  'seniority',
  'source',
  'min_score',
  'status',
  'sort',
  'page',
  'per_page',
] as const

export type FilterKey = (typeof FILTER_KEYS)[number]

export const SORT_OPTIONS: { value: string; label: string }[] = [
  { value: 'score', label: 'Match score' },
  { value: 'published_at', label: 'Published date' },
  { value: 'salary', label: 'Salary' },
  { value: 'company', label: 'Company' },
]

export const EMPLOYMENT_TYPE_OPTIONS = [
  'permanent',
  'full_time',
  'contract',
  'part_time',
  'internship',
] as const

export const ARRANGEMENT_OPTIONS = ['remote', 'onsite', 'hybrid'] as const

export const JOB_STATUS_OPTIONS = ['new', 'saved', 'applied', 'archived', 'expired'] as const

/** Read a JobFilters object out of a URLSearchParams (only documented keys). */
export function parseFilters(params: URLSearchParams): JobFilters {
  const filters: JobFilters = {}
  for (const key of FILTER_KEYS) {
    const value = params.get(key)
    if (value !== null && value !== '') {
      filters[key] = value
    }
  }
  return filters
}

/** Serialize JobFilters to a plain record, dropping empty values. */
export function filtersToQuery(filters: JobFilters): Record<string, string> {
  const query: Record<string, string> = {}
  for (const key of FILTER_KEYS) {
    const value = filters[key]
    if (value !== undefined && value !== null && value !== '') {
      query[key] = String(value)
    }
  }
  return query
}

/** Serialize JobFilters to a URLSearchParams, dropping empty values. */
export function filtersToParams(filters: JobFilters): URLSearchParams {
  return new URLSearchParams(filtersToQuery(filters))
}
