import { createContext, useCallback, useContext, useMemo } from 'react'
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

export const ME_QUERY_KEY = ['me'] as const

/**
 * Session state is derived directly from the `me` query.
 *
 * It used to be mirrored into local state by a useEffect, which produced a
 * render where `isLoading` was already false but the mirrored `user` was still
 * null. ProtectedRoute saw "no user" in that single render and redirected to
 * /login, so a reload (or any fresh load with a valid session) always bounced
 * back to the login form. Reading the query result directly removes the window
 * entirely: while the probe is pending we are loading, and once it settles the
 * user is either present or genuinely absent.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()

  const { data, isPending, error } = useQuery({
    queryKey: ME_QUERY_KEY,
    queryFn: authApi.me,
    retry: false,
    staleTime: 5 * 60_000,
  })

  const login = useCallback(
    async (email: string, password: string) => {
      const res = await authApi.login(email, password)
      queryClient.setQueryData(ME_QUERY_KEY, { user: res.user })
    },
    [queryClient],
  )

  const register = useCallback(
    async (email: string, password: string, displayName: string) => {
      const res = await authApi.register({ email, password, display_name: displayName })
      queryClient.setQueryData(ME_QUERY_KEY, { user: res.user })
    },
    [queryClient],
  )

  const logout = useCallback(async () => {
    try {
      await authApi.logout()
    } finally {
      // Drop every cached response, then pin the session probe to "signed out"
      // so the UI renders immediately instead of re-probing and redirecting.
      queryClient.clear()
      queryClient.setQueryData(ME_QUERY_KEY, { user: null })
    }
  }, [queryClient])

  const user = data?.user ?? null
  // A 401 is the normal "not signed in" answer, not an error to display.
  const visibleError = error instanceof ApiError && error.isUnauthorized ? null : error

  const value = useMemo<AuthState>(
    () => ({
      user,
      isLoading: isPending && visibleError === null,
      error: visibleError,
      login,
      register,
      logout,
    }),
    [user, isPending, visibleError, login, register, logout],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
