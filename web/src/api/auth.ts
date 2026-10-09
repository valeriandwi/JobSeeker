import { http } from './client'
import type { User } from './types'

export interface RegisterInput {
  email: string
  password: string
  display_name: string
}

export const authApi = {
  register: (input: RegisterInput) =>
    http.post<{ user: User }>('/api/v1/auth/register', input),
  login: (email: string, password: string) =>
    http.post<{ user: User }>('/api/v1/auth/login', { email, password }),
  logout: () => http.post<void>('/api/v1/auth/logout'),
  me: () => http.get<{ user: User }>('/api/v1/me'),
}
