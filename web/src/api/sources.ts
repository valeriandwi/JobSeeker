import { http } from './client'
import type { Source, WatchlistItem } from './types'

export const sourcesApi = {
  list: () => http.get<{ sources: Source[] }>('/api/v1/sources'),
  setEnabled: (id: string, enabled: boolean) =>
    http.patch<{ source: Source }>(`/api/v1/sources/${id}`, { enabled }),
  watchlist: (id: string) => http.get<{ watchlist: WatchlistItem[] }>(`/api/v1/sources/${id}/watchlist`),
  addWatchlist: (id: string, item: { board_slug: string; company: string }) =>
    http.post<{ item: WatchlistItem }>(`/api/v1/sources/${id}/watchlist`, item),
  removeWatchlist: (id: string, slug: string) =>
    http.del<void>(`/api/v1/sources/${id}/watchlist`, { board_slug: slug }),
}
