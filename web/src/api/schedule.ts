import { http } from './client'
import type { Schedule } from './types'

export const scheduleApi = {
  get: () => http.get<Schedule>('/api/v1/schedule'),
  update: (schedule: Pick<Schedule, 'cron_spec' | 'timezone' | 'enabled'>) =>
    http.put<Schedule>('/api/v1/schedule', schedule),
}
