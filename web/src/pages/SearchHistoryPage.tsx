import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { runsApi } from '../api'
import type { Run } from '../api/types'
import { errorMessage } from '../api/client'
import { Button } from '../components/ui/Button'
import { Badge } from '../components/ui/Badge'
import { Table } from '../components/ui/Table'
import { EmptyState, ErrorState, LoadingState } from '../components/ui/States'
import { ErrorBanner } from '../components/ErrorBanner'
import { relativeTime } from '../lib/format'
import { runStatusLabel, runStatusTone } from '../lib/status'

function RunSources({ run }: { run: Run }) {
  const items = run.items ?? []
  if (items.length === 0) return <p className="text-slate-500">No per-source detail.</p>
  return (
    <Table
      caption={`Per source results for run ${run.id}`}
      head={['Source', 'Status', 'Fetched', 'Accepted', 'Rejected', 'Duplicated', 'New', 'Updated', 'Duration', 'Error']}
    >
      {items.map((item) => (
        <tr key={item.source_id}>
          <td className="px-3 py-2">{item.source_id}</td>
          <td className="px-3 py-2">
            <Badge tone={item.status === 'failed' ? 'danger' : item.status === 'partial' ? 'warning' : 'success'}>
              {item.status}
            </Badge>
          </td>
          <td className="px-3 py-2">{item.fetched}</td>
          <td className="px-3 py-2">{item.accepted}</td>
          <td className="px-3 py-2">{item.rejected}</td>
          <td className="px-3 py-2">{item.duplicated}</td>
          <td className="px-3 py-2">{item.new_jobs}</td>
          <td className="px-3 py-2">{item.updated_jobs}</td>
          <td className="px-3 py-2">{item.duration_ms} ms</td>
          <td className="px-3 py-2 text-red-600">{item.error ?? '—'}</td>
        </tr>
      ))}
    </Table>
  )
}

function RunRow({ run }: { run: Run }) {
  const [open, setOpen] = useState(false)
  const isFailed = run.status === 'failed' || run.status === 'aborted'
  const isPartial = run.status === 'partial'
  return (
    <>
      <tr>
        <td className="px-3 py-2">
          <Badge tone={runStatusTone(run.status)} title={runStatusLabel(run.status)}>
            {run.status}
          </Badge>
          {isFailed && <Badge tone="danger" className="ml-1">Failed</Badge>}
          {isPartial && <Badge tone="warning" className="ml-1">Partial</Badge>}
        </td>
        <td className="px-3 py-2 text-slate-600">{run.trigger}</td>
        <td className="px-3 py-2 text-slate-600">{relativeTime(run.started_at)}</td>
        <td className="px-3 py-2 text-slate-600">{run.finished_at ? relativeTime(run.finished_at) : '—'}</td>
        <td className="px-3 py-2 text-red-600">{run.error_summary ?? '—'}</td>
        <td className="px-3 py-2">
          <Button variant="ghost" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
            {open ? 'Hide' : 'Details'}
          </Button>
        </td>
      </tr>
      {open && (
        <tr>
          <td colSpan={6} className="bg-slate-50 px-3 py-3">
            <RunSources run={run} />
          </td>
        </tr>
      )}
    </>
  )
}

export function SearchHistoryPage() {
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const query = useQuery({
    queryKey: ['runs', page],
    queryFn: () => runsApi.list(page, 20),
    refetchInterval: (q) => {
      const runs = q.state.data?.runs ?? []
      return runs.some((r) => r.status === 'running') ? 3000 : false
    },
  })

  const trigger = useMutation({
    mutationFn: () => runsApi.run(),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['runs'] }),
  })

  const totalPages = query.data ? Math.max(1, Math.ceil(query.data.total / query.data.per_page)) : 1

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Search History</h1>
        <Button onClick={() => trigger.mutate()} disabled={trigger.isPending}>
          {trigger.isPending ? 'Starting…' : 'Run search now'}
        </Button>
      </div>
      <ErrorBanner error={trigger.error} />

      {query.isLoading ? (
        <LoadingState label="Loading runs…" />
      ) : query.isError ? (
        <ErrorState message={errorMessage(query.error)} onRetry={() => query.refetch()} />
      ) : !query.data || query.data.runs.length === 0 ? (
        <EmptyState title="No runs yet" description="Trigger a search to see history here." />
      ) : (
        <>
          <div className="rounded-lg border border-slate-200 bg-white">
            <Table head={['Status', 'Trigger', 'Started', 'Finished', 'Error', 'Details']} caption="Search runs">
              {query.data.runs.map((run) => (
                <RunRow key={run.id} run={run} />
              ))}
            </Table>
          </div>
          <nav aria-label="Pagination" className="flex items-center justify-between text-sm">
            <span className="text-slate-500">
              Page {page} of {totalPages} · {query.data.total} runs
            </span>
            <div className="flex gap-2">
              <Button variant="secondary" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
                Previous
              </Button>
              <Button variant="secondary" disabled={page >= totalPages} onClick={() => setPage((p) => p + 1)}>
                Next
              </Button>
            </div>
          </nav>
        </>
      )}
    </div>
  )
}
