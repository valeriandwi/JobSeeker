import { http } from './client'
import type { JobDetail, JobFilters, JobsPage, SavedJob } from './types'

export const jobsApi = {
  list: (filters: JobFilters) => http.get<JobsPage>('/api/v1/jobs', filters as Record<string, string>),
  get: (id: string) => http.get<{ job: JobDetail }>(`/api/v1/jobs/${id}`),
  save: (id: string) => http.post<void>(`/api/v1/jobs/${id}/save`),
  unsave: (id: string) => http.del<void>(`/api/v1/jobs/${id}/save`),
  // The handler emits `saved_jobs` (flat rows), not `jobs`. Typing it as `jobs`
  // silently made the Saved Jobs page read undefined and render as empty.
  saved: () => http.get<{ saved_jobs: SavedJob[] }>('/api/v1/saved-jobs'),
}
