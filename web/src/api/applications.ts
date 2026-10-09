import { http } from './client'
import type { Application, ApplicationStatus } from './types'

export interface ApplicationUpdate {
  status: ApplicationStatus
  applied_at?: string | null
  notes?: string | null
  follow_up_at?: string | null
}

export const applicationsApi = {
  list: () => http.get<{ applications: Application[] }>('/api/v1/applications'),
  update: (jobId: string, update: ApplicationUpdate) =>
    http.put<{ application: Application }>(`/api/v1/applications/${jobId}`, update),
}
