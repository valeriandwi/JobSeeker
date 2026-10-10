import { http } from './client'
import type { CertificationEntry, EducationEntry, Profile, ProjectEntry } from './types'

/**
 * Request payload for PUT /api/v1/profile.
 *
 * The API decodes request bodies strictly and rejects unknown fields, while the
 * response object carries server-owned ones (`user_id`, `version`,
 * `confirmed_at`, `updated_at`, `confirmed_field_keys`). Sending a response
 * object straight back therefore failed with
 * `invalid JSON body: unknown field "user_id"`.
 *
 * This type is the write contract: exactly the fields the server accepts.
 */
export interface ProfileUpdate {
  full_name: string
  headline: string
  current_title: string
  years_experience: number | null
  seniority: string
  skills: string[]
  industries: string[]
  education: string[]
  certifications: string[]
  languages: string[]
  projects: string[]
  source_resume_id: string | null
}

/** The API stores education, certifications and projects as plain strings. */
export function formatEducation(entry: EducationEntry | string): string {
  if (typeof entry === 'string') return entry.trim()
  return [entry.degree, entry.field, entry.institution, entry.end_year]
    .filter((part) => part !== null && part !== undefined && part !== '')
    .join(', ')
}

export function formatCertification(entry: CertificationEntry | string): string {
  if (typeof entry === 'string') return entry.trim()
  return [entry.name, entry.issuer, entry.year]
    .filter((part) => part !== null && part !== undefined && part !== '')
    .join(', ')
}

export function formatProject(entry: ProjectEntry | string): string {
  if (typeof entry === 'string') return entry.trim()
  return [entry.name, entry.description, entry.url]
    .filter((part) => part !== null && part !== undefined && part !== '')
    .join(' — ')
}

function cleanStrings(values: unknown): string[] {
  if (!Array.isArray(values)) return []
  return values
    .map((v) =>
      typeof v === 'string' ? v.trim() : v === null || v === undefined ? '' : String(v),
    )
    .filter((v) => v !== '')
}

/** Map a profile (usually straight from GET) onto the write contract. */
export function toProfileUpdate(profile: Profile): ProfileUpdate {
  return {
    full_name: (profile.full_name ?? '').trim(),
    headline: (profile.headline ?? '').trim(),
    current_title: (profile.current_title ?? '').trim(),
    years_experience:
      typeof profile.years_experience === 'number' ? profile.years_experience : null,
    seniority: profile.seniority ?? 'unknown',
    skills: cleanStrings(profile.skills),
    industries: cleanStrings(profile.industries),
    education: (profile.education ?? []).map(formatEducation).filter((v) => v !== ''),
    certifications: (profile.certifications ?? [])
      .map(formatCertification)
      .filter((v) => v !== ''),
    languages: cleanStrings(profile.languages),
    projects: (profile.projects ?? []).map(formatProject).filter((v) => v !== ''),
    source_resume_id: profile.source_resume_id ?? null,
  }
}

export const profileApi = {
  get: () => http.get<{ profile: Profile }>('/api/v1/profile'),
  /** Accepts a full Profile but sends only the writable subset. */
  update: (profile: Profile) =>
    http.put<{ profile: Profile }>('/api/v1/profile', toProfileUpdate(profile)),
}
