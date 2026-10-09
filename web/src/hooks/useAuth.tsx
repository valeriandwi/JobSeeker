import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, authApi } from '../api'
import type { User } from '../api/types'

interface AuthState {
  user: User | null
  isLoading: boolean
  error: unknown
  login: (email: string, password: string) => Promise<void>
  register: (email: string, password: string, displayName: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const [user, setUser] = useState<User | null>(null)
  const [probeDone, setProbeDone] = useState(false)

  const { data, isLoading, error } = useQuery({
    queryKey: ['me'],
    queryFn: authApi.me,
    retry: false,
    staleTime: 5 * 60_000,
  })

  useEffect(() => {
    if (data?.user) setUser(data.user)
    if (data || error) setProbeDone(true)
  }, [data, error])

  const login = useCallback(
    async (email: string, password: string) => {
      const res = await authApi.login(email, password)
      setUser(res.user)
      queryClient.setQueryData(['me'], { user: res.user })
    },
    [queryClient],
  )

  const register = useCallback(
    async (email: string, password: string, displayName: string) => {
      const res = await authApi.register({ email, password, display_name: displayName })
      setUser(res.user)
      queryClient.setQueryData(['me'], { user: res.user })
    },
    [queryClient],
  )

  const logout = useCallback(async () => {
    try {
      await authApi.logout()
    } finally {
      setUser(null)
      queryClient.clear()
    }
  }, [queryClient])

  const value = useMemo<AuthState>(
    () => ({
      user,
      isLoading: isLoading && !probeDone,
      error: error instanceof ApiError && error.isUnauthorized ? null : error,
      login,
      register,
      logout,
    }),
    [user, isLoading, probeDone, error, login, register, logout],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
