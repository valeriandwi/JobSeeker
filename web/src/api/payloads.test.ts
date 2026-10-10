import { describe, expect, it } from 'vitest'
import { formatCertification, formatEducation, formatProject, toProfileUpdate } from './profile'
import { toPreferencesUpdate } from './preferences'
import { isCompleteSchedule, readSchedule } from './schedule'
import type { Preferences, Profile } from './types'

/** Keys the Go API accepts for PUT /api/v1/profile (mirrors api.profileBody). */
const PROFILE_WRITABLE_KEYS = [
  'full_name',
  'headline',
  'current_title',
  'years_experience',
  'seniority',
  'skills',
  'industries',
  'education',
  'certifications',
  'languages',
  'projects',
  'source_resume_id',
] as const

/** Keys the Go API accepts for PUT /api/v1/preferences. */
const PREFERENCES_WRITABLE_KEYS = [
  'job_titles',
  'keywords',
  'excluded_keywords',
  'locations',
  'work_arrangements',
  'employment_types',
  'seniority_levels',
  'min_salary',
  'salary_currency',
  'salary_period',
  'min_score',
] as const

/**
 * Regression: the API rejects unknown JSON fields, and the response objects
 * carry server-owned ones. Sending a response straight back produced
 * `invalid JSON body: unknown field "user_id"`, so the profile could never be
 * saved. These tests pin the outgoing bodies.
 */
describe('profile update payload', () => {
  // A profile exactly as the API returns it, including server-owned fields.
  const fromApi = {
    user_id: 'u-1',
    full_name: 'Valerian',
    headline: 'Senior Full Stack Engineer',
    current_title: 'Senior Full Stack Engineer',
    years_experience: 10.2,
    seniority: 'senior',
    skills: ['React', 'Go', ''],
    industries: ['Fintech'],
    education: [{ degree: 'Bachelor of Computer Science', institution: 'Institut Teknologi Bandung', end_year: 2016 }],
    certifications: [{ name: 'AWS Certified Solutions Architect', issuer: 'Amazon', year: 2022 }],
    languages: ['English', 'Indonesian'],
    projects: [{ name: 'Job aggregation service', description: 'Go service with dedup' }],
    source_resume_id: 'r-9',
    version: 3,
    confirmed_at: '2026-10-09T00:00:00Z',
    updated_at: '2026-10-09T00:00:00Z',
    confirmed_field_keys: ['skills'],
  } as unknown as Profile

  it('sends exactly the writable keys and no server-owned field', () => {
    const payload = toProfileUpdate(fromApi)

    expect(Object.keys(payload).sort()).toEqual([...PROFILE_WRITABLE_KEYS].sort())
    // The fields that caused the 400 in production:
    for (const forbidden of ['user_id', 'version', 'confirmed_at', 'updated_at', 'confirmed_field_keys']) {
      expect(payload).not.toHaveProperty(forbidden)
    }
    // And the value counts are preserved.
    expect(payload.skills).toEqual(['React', 'Go'])
    expect(payload.source_resume_id).toBe('r-9')
  })

  it('serialises entries as the strings the API stores', () => {
    const payload = toProfileUpdate(fromApi)

    expect(payload.education).toEqual([
      'Bachelor of Computer Science, Institut Teknologi Bandung, 2016',
    ])
    expect(payload.certifications).toEqual(['AWS Certified Solutions Architect, Amazon, 2022'])
    expect(payload.projects).toEqual(['Job aggregation service — Go service with dedup'])
    // Everything the API expects to be a string array really is one.
    for (const key of ['skills', 'industries', 'education', 'certifications', 'languages', 'projects'] as const) {
      expect(Array.isArray(payload[key])).toBe(true)
      for (const item of payload[key]) expect(typeof item).toBe('string')
    }
  })

  it('tolerates strings where objects are also accepted', () => {
    expect(formatEducation('MSc, TU Delft')).toBe('MSc, TU Delft')
    expect(formatCertification('CKA')).toBe('CKA')
    expect(formatProject('Side project')).toBe('Side project')
  })

  it('handles a sparse profile without inventing values', () => {
    const sparse = { skills: [], industries: [], education: [], certifications: [], languages: [], projects: [], version: 1, confirmed_field_keys: [] } as unknown as Profile
    const payload = toProfileUpdate(sparse)

    expect(payload.full_name).toBe('')
    expect(payload.years_experience).toBeNull()
    expect(payload.seniority).toBe('unknown')
    expect(payload.source_resume_id).toBeNull()
    expect(JSON.parse(JSON.stringify(payload))).toEqual(payload)
  })
})

describe('preferences update payload', () => {
  const fromApi = {
    user_id: 'u-1',
    updated_at: '2026-10-09T00:00:00Z',
    job_titles: ['Frontend Engineer'],
    keywords: ['react'],
    excluded_keywords: ['unreal'],
    locations: [{ country: 'my', city: 'Kuala Lumpur', region: '', mandatory: true }],
    work_arrangements: [{ value: 'remote', mandatory: false }],
    employment_types: ['permanent'],
    seniority_levels: ['senior'],
    min_salary: 120000,
    salary_currency: 'USD',
    salary_period: 'year',
    min_score: 60,
  } as unknown as Preferences

  it('sends exactly the writable keys and no server-owned field', () => {
    const payload = toPreferencesUpdate(fromApi)

    expect(Object.keys(payload).sort()).toEqual([...PREFERENCES_WRITABLE_KEYS].sort())
    expect(payload).not.toHaveProperty('user_id')
    expect(payload).not.toHaveProperty('updated_at')
  })

  it('normalises values the API validates', () => {
    const payload = toPreferencesUpdate(fromApi)
    expect(payload.min_score).toBe(60)
    expect(typeof payload.min_salary).toBe('number')
  })

  // The user's own text is saved exactly as typed: matching compares country
  // case-insensitively server-side, so upper-casing it here only changed how
  // their input looked back at them.
  it('preserves location values exactly as typed', () => {
    const payload = toPreferencesUpdate(fromApi)

    expect(payload.locations[0].country).toBe('my')
    expect(payload.locations[0].city).toBe('Kuala Lumpur')
  })
})

describe('schedule response', () => {
  it('reads the schedule out of the wrapper the API returns', () => {
    const response = {
      schedule: { cron_spec: '0 8 * * *', timezone: 'Asia/Jakarta', enabled: true, last_run_at: null, next_run_at: null },
      next_run_at: '2026-10-10T01:00:00Z',
    }
    const schedule = readSchedule(response)

    expect(schedule?.cron_spec).toBe('0 8 * * *')
    expect(schedule?.timezone).toBe('Asia/Jakarta')
    // The old code read response.cron_spec, which is undefined in this shape.
    expect((response as Record<string, unknown>).cron_spec).toBeUndefined()
  })

  it('returns null rather than a half-built schedule', () => {
    expect(readSchedule(null)).toBeNull()
    expect(readSchedule(undefined)).toBeNull()
    expect(readSchedule({ schedule: undefined } as never)).toBeNull()
  })

  it('only allows saving a schedule with a cron spec', () => {
    expect(isCompleteSchedule({ cron_spec: '0 8 * * *' })).toBe(true)
    expect(isCompleteSchedule({ cron_spec: '   ' })).toBe(false)
    expect(isCompleteSchedule({ cron_spec: '' })).toBe(false)
    expect(isCompleteSchedule(null)).toBe(false)
  })
})
