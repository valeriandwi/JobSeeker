import { http } from './client'
import type { Profile } from './types'

export const profileApi = {
  get: () => http.get<{ profile: Profile }>('/api/v1/profile'),
  update: (profile: Profile) => http.put<{ profile: Profile }>('/api/v1/profile', profile),
}
