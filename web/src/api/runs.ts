import { http } from './client'
import type { Run, RunsPage } from './types'

export const runsApi = {
  run: () => http.post<{ run: Run }>('/api/v1/searches/run'),
  list: (page = 1, perPage = 20) =>
    http.get<RunsPage>('/api/v1/searches/runs', { page, per_page: perPage }),
  get: (id: string) => http.get<{ run: Run }>(`/api/v1/searches/runs/${id}`),
}
