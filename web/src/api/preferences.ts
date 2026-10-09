import { http } from './client'
import type { Preferences } from './types'

export const preferencesApi = {
  get: () => http.get<{ preferences: Preferences }>('/api/v1/preferences'),
  update: (preferences: Preferences) =>
    http.put<{ preferences: Preferences }>('/api/v1/preferences', preferences),
}
