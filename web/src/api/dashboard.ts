import { http } from './client'
import type { DashboardSummary } from './types'

export const dashboardApi = {
  summary: () => http.get<DashboardSummary>('/api/v1/dashboard/summary'),
}
