import { http } from './client'
import type { Schedule } from './types'

/**
 * GET /api/v1/schedule answers `{ schedule: {...}, next_run_at }`, not a bare
 * Schedule. The previous typing said otherwise, so the page read
 * `data.cron_spec` — always undefined — and then saved an empty cron spec, which
 * the server correctly rejects. The wrapper is modelled here so callers cannot
 * make that mistake again.
 */
export interface ScheduleResponse {
  schedule: Schedule
  next_run_at?: string | null
  warning?: string
}

/** Request payload for PUT /api/v1/schedule. */
export interface ScheduleUpdate {
  cron_spec: string
  timezone: string
  enabled: boolean
}

export const scheduleApi = {
  get: () => http.get<ScheduleResponse>('/api/v1/schedule'),
  update: (schedule: ScheduleUpdate) =>
    http.put<ScheduleResponse>('/api/v1/schedule', schedule),
}

/** Extract the schedule from a response, tolerating a bare object as well. */
export function readSchedule(
  response: ScheduleResponse | Schedule | undefined | null,
): Schedule | null {
  if (!response) return null
  const candidate = (response as ScheduleResponse).schedule ?? (response as Schedule)
  if (!candidate || typeof candidate.cron_spec !== 'string') return null
  return candidate
}

/** A schedule can only be saved with a non-empty cron spec. */
export function isCompleteSchedule(schedule: Partial<Schedule> | null): boolean {
  return Boolean(
    schedule && typeof schedule.cron_spec === 'string' && schedule.cron_spec.trim() !== '',
  )
}
