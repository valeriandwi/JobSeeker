import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { dashboardApi, jobsApi } from '../api'
import { errorMessage } from '../api/client'
import { Table } from '../components/ui/Table'
import { Badge } from '../components/ui/Badge'
import { ScoreBadge } from '../components/ScoreBadge'
import { EmptyState, ErrorState, LoadingState } from '../components/ui/States'
import { relativeTime } from '../lib/format'
import { runStatusTone } from '../lib/status'

function Counter({ label, value }: { label: string; value: number | string }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4">
      <p className="text-sm text-slate-500">{label}</p>
      <p className="mt-1 text-2xl font-semibold text-slate-900">{value}</p>
    </div>
  )
}

export function DashboardPage() {
  const summary = useQuery({ queryKey: ['dashboard'], queryFn: dashboardApi.summary })
  const highMatch = useQuery({
    queryKey: ['jobs', { sort: 'score', per_page: '5', page: '1' }],
    queryFn: () => jobsApi.list({ sort: 'score', per_page: '5', page: '1' }),
  })

  if (summary.isLoading) return <LoadingState label="Loading dashboard…" />
  if (summary.isError) return <ErrorState message={errorMessage(summary.error)} onRetry={() => summary.refetch()} />
  const data = summary.data!
  // The API contract promises arrays for list fields, but a defensive default
  // here means a contract regression shows an empty list instead of blanking
  // the whole application.
  const recentRuns = data.recent_runs ?? []
  const highMatchJobs = highMatch.data?.jobs ?? []

  return (
    <div className="space-y-6">
      <h1 className="text-xl font-semibold">Dashboard</h1>

      <section aria-label="Summary counters" className="grid grid-cols-2 gap-4 sm:grid-cols-3">
        <Counter label="Total jobs" value={data.total_jobs} />
        <Counter label="New since last run" value={data.new_since_last_run} />
        <Counter label="High matches" value={data.high_match_count} />
      </section>

      <section className="grid gap-4 md:grid-cols-2">
        <div className="rounded-lg border border-slate-200 bg-white p-4">
          <h2 className="text-sm font-semibold text-slate-700">Last run</h2>
          {data.last_run ? (
            <div className="mt-2 space-y-1 text-sm">
              <p className="flex items-center gap-2">
                <Badge tone={runStatusTone(data.last_run.status)}>{data.last_run.status}</Badge>
                <span className="text-slate-500">{relativeTime(data.last_run.started_at)}</span>
              </p>
              {data.last_run.error_summary && (
                <p className="text-red-600">{data.last_run.error_summary}</p>
              )}
            </div>
          ) : (
            <p className="mt-2 text-sm text-slate-500">No runs yet.</p>
          )}
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-4">
          <h2 className="text-sm font-semibold text-slate-700">Next scheduled run</h2>
          <p className="mt-2 text-sm text-slate-600">
            {data.next_run_at ? relativeTime(data.next_run_at) : 'No schedule configured.'}
          </p>
        </div>
      </section>

      <section aria-labelledby="high-match-heading">
        <div className="mb-2 flex items-center justify-between">
          <h2 id="high-match-heading" className="text-sm font-semibold text-slate-700">
            Top matches
          </h2>
          <Link to="/jobs?sort=score" className="text-sm text-brand-600 hover:underline">
            View all jobs
          </Link>
        </div>
        {highMatch.isLoading ? (
          <LoadingState label="Loading top matches…" />
        ) : highMatch.isError ? (
          <ErrorState message={errorMessage(highMatch.error)} onRetry={() => highMatch.refetch()} />
        ) : highMatch.data && highMatchJobs.length > 0 ? (
          <ul className="divide-y divide-slate-100 rounded-lg border border-slate-200 bg-white">
            {highMatchJobs.map((job) => (
              <li key={job.id} className="flex items-center justify-between gap-3 px-4 py-3">
                <div>
                  <Link to={`/jobs?q=${encodeURIComponent(job.title)}`} className="font-medium text-slate-900 hover:underline">
                    {job.title}
                  </Link>
                  <p className="text-sm text-slate-500">
                    {job.company} · {job.location_city ?? 'Unknown'}
                  </p>
                </div>
                <ScoreBadge score={job.match_score} />
              </li>
            ))}
          </ul>
        ) : (
          <EmptyState title="No matches yet" description="Run a search to populate matches." />
        )}
      </section>

      <section aria-labelledby="recent-runs-heading">
        <h2 id="recent-runs-heading" className="mb-2 text-sm font-semibold text-slate-700">
          Recent runs
        </h2>
        {recentRuns.length === 0 ? (
          <EmptyState title="No recent runs" />
        ) : (
          <div className="rounded-lg border border-slate-200 bg-white">
            <Table head={['Status', 'Started', 'Trigger', 'Error']} caption="Recent runs">
              {recentRuns.map((run) => (
                <tr key={run.id}>
                  <td className="px-3 py-2">
                    <Badge tone={runStatusTone(run.status)}>{run.status}</Badge>
                  </td>
                  <td className="px-3 py-2 text-slate-600">{relativeTime(run.started_at)}</td>
                  <td className="px-3 py-2 text-slate-600">{run.trigger}</td>
                  <td className="px-3 py-2 text-slate-600">{run.error_summary ?? '—'}</td>
                </tr>
              ))}
            </Table>
          </div>
        )}
      </section>
    </div>
  )
}
