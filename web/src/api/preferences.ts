import { http } from './client'
import type {
  EmploymentType,
  PreferenceArrangement,
  PreferenceLocation,
  Preferences,
  SalaryCurrency,
  SalaryPeriod,
} from './types'

/**
 * Request payload for PUT /api/v1/preferences.
 *
 * Same contract rule as the profile: the response carries `user_id` and
 * `updated_at`, which the strict server decoder rejects with
 * `invalid JSON body: unknown field "user_id"`. Only these fields are accepted,
 * so only these are sent.
 */
export interface PreferencesUpdate {
  job_titles: string[]
  keywords: string[]
  excluded_keywords: string[]
  locations: PreferenceLocation[]
  work_arrangements: PreferenceArrangement[]
  employment_types: EmploymentType[]
  seniority_levels: string[]
  min_salary: number | null
  salary_currency: SalaryCurrency | null
  salary_period: SalaryPeriod | null
  min_score: number
}

export function toPreferencesUpdate(preferences: Preferences): PreferencesUpdate {
  return {
    job_titles: [...(preferences.job_titles ?? [])],
    keywords: [...(preferences.keywords ?? [])],
    excluded_keywords: [...(preferences.excluded_keywords ?? [])],
    locations: (preferences.locations ?? []).map((l) => ({
      // Saved exactly as typed. Matching compares country case-insensitively
      // (strings.EqualFold on the server), so normalising the user's text here
      // only changed how their own input looked back at them.
      country: (l.country ?? '').trim(),
      city: (l.city ?? '').trim(),
      region: (l.region ?? '').trim(),
      mandatory: Boolean(l.mandatory),
    })),
    work_arrangements: (preferences.work_arrangements ?? []).map((a) => ({
      value: a.value,
      mandatory: Boolean(a.mandatory),
    })),
    employment_types: [...(preferences.employment_types ?? [])],
    seniority_levels: [...(preferences.seniority_levels ?? [])],
    min_salary: typeof preferences.min_salary === 'number' ? preferences.min_salary : null,
    salary_currency: preferences.salary_currency ?? null,
    salary_period: preferences.salary_period ?? null,
    min_score: Number.isFinite(preferences.min_score) ? preferences.min_score : 0,
  }
}

export const preferencesApi = {
  get: () => http.get<{ preferences: Preferences }>('/api/v1/preferences'),
  /** Accepts a full Preferences object but sends only the writable subset. */
  update: (preferences: Preferences) =>
    http.put<{ preferences: Preferences }>(
      '/api/v1/preferences',
      toPreferencesUpdate(preferences),
    ),
}
