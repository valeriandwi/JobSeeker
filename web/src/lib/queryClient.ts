import { QueryClient } from '@tanstack/react-query'

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        retry: (failureCount, error) => {
          // Do not retry auth or not-found errors.
          const status = (error as { status?: number })?.status
          if (status && status >= 400 && status < 500) return false
          return failureCount < 1
        },
        refetchOnWindowFocus: false,
      },
      mutations: {
        retry: false,
      },
    },
  })
}
