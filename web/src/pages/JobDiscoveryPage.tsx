import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { jobsApi, matchesApi, runsApi } from '../api'
import type { Job, JobFilters } from '../api/types'
import { errorMessage } from '../api/client'
import { Table } from '../components/ui/Table'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { Input, Select } from '../components/ui/Input'
import { ScoreBadge } from '../components/ScoreBadge'
import { UnknownChip } from '../components/UnknownChip'
import { EmptyState, ErrorState, LoadingState } from '../components/ui/States'
import { ErrorBanner } from '../components/ErrorBanner'
import { formatSalary, relativeTime, shortDescription } from '../lib/format'
import { TECH_CHIP_LIMIT, detectTechStack } from '../lib/techStack'
import {
  arrangementLabel,
  arrangementTone,
  employmentTypeLabel,
  employmentTypeTone,
} from '../lib/status'
import {
  ARRANGEMENT_OPTIONS,
  EMPLOYMENT_TYPE_OPTIONS,
  JOB_STATUS_OPTIONS,
  SORT_OPTIONS,
  DEFAULT_SORT,
  filtersToParams,
  parseFilters,
} from '../lib/filters'

const SORT_LABELS = new Set(SORT_OPTIONS.map((o) => o.value))

/** All Unknown-marker-aware attributes on a job row. */
export function JobRow({ job }: { job: Job }) {
  const salary = formatSalary(job)
  const primarySource = job.sources[0]
  // Derived from the listing text and labelled as such in the cell: the sources
  // publish no tech stack we could state as a fact (see lib/techStack.ts).
  const detectedTech = detectTechStack([job.title, job.description_summary].filter(Boolean).join(' '))
  return (
    <tr>
      <td className="px-3 py-3 align-top">
        <ScoreBadge score={job.match_score} />
      </td>
      <td className="px-3 py-3 align-top">
        <div className="font-medium text-slate-900">{job.title || <UnknownChip value={null} fallback="Untitled" />}</div>
        <div className="text-sm text-slate-600">
          <UnknownChip value={job.company} />
        </div>
      </td>
      {/* Short preview of the vacancy summary: enough to judge relevance without
          opening the posting. The full text stays in the title attribute. */}
      <td className="px-3 py-3 align-top text-sm text-slate-600">
        {job.description_summary ? (
          <span className="block max-w-sm" title={job.description_summary}>
            {shortDescription(job.description_summary)}
          </span>
        ) : (
          <UnknownChip value={null} />
        )}
      </td>
      {/* Derived, not source data: the sources publish no usable tech stack (see
          lib/techStack.ts). Chips are labelled as detected and never presented as
          a fact about the vacancy. */}
      <td className="px-3 py-3 align-top text-sm">
        {detectedTech.length > 0 ? (
          <div
            className="flex max-w-xs flex-wrap gap-1"
            title="Detected by scanning this listing's title and description. The source publishes no tech stack — treat it as a reading aid, not a fact."
          >
            {detectedTech.slice(0, TECH_CHIP_LIMIT).map((tech) => (
              <Badge key={tech} tone="brand">
                {tech}
              </Badge>
            ))}
            {detectedTech.length > TECH_CHIP_LIMIT && (
              <Badge tone="neutral" title={detectedTech.join(', ')}>
                +{detectedTech.length - TECH_CHIP_LIMIT}
              </Badge>
            )}
          </div>
        ) : (
          <Badge tone="neutral" title="Scanned the title and description; no known technology names were found.">
            None detected
          </Badge>
        )}
      </td>
      <td className="px-3 py-3 align-top text-sm text-slate-600">
        {job.location_city || job.location_country ? (
          <span>{[job.location_city, job.location_country].filter(Boolean).join(', ')}</span>
        ) : (
          <UnknownChip value={null} />
        )}
      </td>
      <td className="px-3 py-3 align-top whitespace-nowrap text-sm">
        {job.employment_type ? (
          <Badge
            tone={employmentTypeTone(job.employment_type)}
            title={
              job.attributes_verified.includes('employment_type')
                ? 'Published by the source.'
                : 'Value present, but the source did not mark it as verified.'
            }
          >
            {employmentTypeLabel(job.employment_type)}
          </Badge>
        ) : (
          <UnknownChip value={null} />
        )}
      </td>
      <td className="px-3 py-3 align-top whitespace-nowrap text-sm">
        {job.work_arrangement ? (
          <Badge
            tone={arrangementTone(job.work_arrangement)}
            title={
              job.attributes_verified.includes('work_arrangement')
                ? 'Published by the source.'
                : 'Value present, but the source did not mark it as verified.'
            }
          >
            {arrangementLabel(job.work_arrangement)}
          </Badge>
        ) : (
          <UnknownChip value={null} />
        )}
      </td>
      <td className="px-3 py-3 align-top text-sm">
        {salary ? <span>{salary}</span> : <Badge tone="neutral">Not disclosed</Badge>}
      </td>
      <td className="px-3 py-3 align-top text-sm">
        {primarySource ? (
          <div className="space-y-1">
            <a
              href={primarySource.url}
              target="_blank"
              rel="noopener noreferrer"
              className="text-brand-600 hover:underline"
            >
              {primarySource.display_name}
            </a>
            {primarySource.backlink_required && (
              <p className="text-xs text-amber-700">
                Attribution required
                {primarySource.attribution_url && (
                  <>
                    {' · '}
                    <a
                      href={primarySource.attribution_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="underline"
                    >
                      source
                    </a>
                  </>
                )}
              </p>
            )}
          </div>
        ) : (
          <UnknownChip value={null} fallback="No source" />
        )}
      </td>
      <td className="px-3 py-3 align-top whitespace-nowrap text-sm text-slate-600">
        {job.first_discovered_at ? relativeTime(job.first_discovered_at) : <UnknownChip value={null} />}
      </td>
      <td className="px-3 py-3 align-top text-sm">
        {primarySource?.apply_url || primarySource?.url ? (
          <a
            href={primarySource.apply_url ?? primarySource.url}
            target="_blank"
            rel="noopener noreferrer"
            className="text-brand-600 hover:underline"
          >
            Open original ↗
          </a>
        ) : (
          <UnknownChip value={null} fallback="No link" />
        )}
      </td>
    </tr>
  )
}

export function JobsFilterForm({
  filters,
  onChange,
}: {
  filters: JobFilters
  onChange: (next: JobFilters) => void
}) {
  const set = (key: keyof JobFilters, value: string) => {
    const next = { ...filters, page: '1' }
    if (value) next[key] = value
    else delete next[key]
    onChange(next)
  }

  return (
    <form
      aria-label="Job filters"
      className="grid grid-cols-1 gap-3 rounded-lg border border-slate-200 bg-white p-4 sm:grid-cols-2 lg:grid-cols-4"
      onSubmit={(e) => e.preventDefault()}
    >
      <Input label="Search" value={filters.q ?? ''} onChange={(e) => set('q', e.target.value)} placeholder="Title, company…" />
      <Input label="Country" value={filters.country ?? ''} onChange={(e) => set('country', e.target.value)} />
      <Input label="City" value={filters.city ?? ''} onChange={(e) => set('city', e.target.value)} />
      <Select label="Work arrangement" value={filters.arrangement ?? ''} onChange={(e) => set('arrangement', e.target.value)}>
        <option value="">Any</option>
        {ARRANGEMENT_OPTIONS.map((v) => (
          <option key={v} value={v}>
            {v}
          </option>
        ))}
      </Select>
      <Select
        label="Employment type"
        value={filters.employment_type ?? ''}
        onChange={(e) => set('employment_type', e.target.value)}
      >
        <option value="">Any</option>
        {EMPLOYMENT_TYPE_OPTIONS.map((v) => (
          <option key={v} value={v}>
            {v}
          </option>
        ))}
      </Select>
      <Input label="Seniority" value={filters.seniority ?? ''} onChange={(e) => set('seniority', e.target.value)} />
      <Input label="Source" value={filters.source ?? ''} onChange={(e) => set('source', e.target.value)} />
      <Input
        label="Minimum match score"
        type="number"
        min={0}
        max={100}
        value={filters.min_score ?? ''}
        onChange={(e) => set('min_score', e.target.value)}
      />
      <Select label="Status" value={filters.status ?? ''} onChange={(e) => set('status', e.target.value)}>
        <option value="">Any</option>
        {JOB_STATUS_OPTIONS.map((v) => (
          <option key={v} value={v}>
            {v}
          </option>
        ))}
      </Select>
      <Select
        label="Sort by"
        value={SORT_LABELS.has(filters.sort ?? '') ? (filters.sort as string) : DEFAULT_SORT}
        onChange={(e) => set('sort', e.target.value)}
      >
        {SORT_OPTIONS.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </Select>
      <div className="flex items-end">
        <Button type="button" variant="secondary" onClick={() => onChange({ sort: DEFAULT_SORT })}>
          Reset filters
        </Button>
      </div>
    </form>
  )
}

/**
 * Refresh control for the match list.
 *
 * Fetches from the enabled sources and then re-scores everything already
 * discovered against the current confirmed profile — so the scores reflect the
 * CV as it stands, not whatever profile happened to be loaded when each job was
 * last seen. A source with no adapter, or an empty catalog, produces a run whose
 * error_summary explains why; that is shown verbatim rather than reported as
 * "0 new jobs".
 */
export function RefreshMatchesPanel() {
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationFn: async () => {
      const run = await runsApi.run()
      // Re-score after fetching so the list reflects the current profile.
      const rescored = await matchesApi.rescore()
      return { run: run.run, rescored: rescored.rescored }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['jobs'] })
      queryClient.invalidateQueries({ queryKey: ['dashboard'] })
      queryClient.invalidateQueries({ queryKey: ['runs'] })
    },
  })

  const run = mutation.data?.run
  const rescored = mutation.data?.rescored ?? 0
  const created = run?.stats?.jobs_created ?? 0
  const duplicates = run?.stats?.jobs_duplicated ?? 0

  return (
    <div className="flex flex-wrap items-center justify-end gap-3">
      <Button onClick={() => mutation.mutate()} disabled={mutation.isPending}>
        {mutation.isPending ? 'Refreshing…' : 'Refresh matches'}
      </Button>
      {mutation.isPending && (
        <span role="status" className="text-sm text-slate-500">
          Searching the enabled sources…
        </span>
      )}
      {!mutation.isPending && run && (
        <span role="status" className={`text-sm ${run.status === 'failed' ? 'text-red-600' : 'text-green-700'}`}>
          {run.status === 'succeeded' &&
            `Refreshed: ${created} new, ${duplicates} duplicates · re-scored ${rescored}`}
          {run.status === 'partial' &&
            `Partly refreshed: ${created} new — ${run.error_summary ?? 'some sources failed'}`}
          {run.status === 'failed' && (run.error_summary || 'Refresh failed')}
        </span>
      )}
      {!mutation.isPending && <ErrorBanner error={mutation.error} />}
    </div>
  )
}

export function JobDiscoveryPage() {
  const [params, setParams] = useSearchParams()
  const filters = useMemo(() => parseFilters(params), [params])

  const query = useQuery({
    queryKey: ['jobs', filters],
    queryFn: () => jobsApi.list(filters),
    placeholderData: keepPreviousData,
  })

  const onFilterChange = (next: JobFilters) => {
    setParams(filtersToParams(next), { replace: false })
  }

  const page = Number(filters.page ?? '1')
  const perPage = Number(filters.per_page ?? '20')
  const total = query.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / perPage))

  const setPage = (p: number) => {
    setParams(filtersToParams({ ...filters, page: String(p) }))
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <h1 className="text-xl font-semibold">Job Discovery</h1>
          <p className="text-sm text-slate-600">
            Vacancies found by your enabled sources, scored against your confirmed profile —
            newest discovery first. Use <strong>Refresh matches</strong> to fetch new listings and
            re-score the ones already here.
          </p>
        </div>
        <RefreshMatchesPanel />
      </div>
      <JobsFilterForm filters={filters} onChange={onFilterChange} />

      {query.isLoading ? (
        <LoadingState label="Loading jobs…" />
      ) : query.isError ? (
        <ErrorState message={errorMessage(query.error)} onRetry={() => query.refetch()} />
      ) : !query.data || query.data.jobs.length === 0 ? (
        <EmptyState
          title="No jobs match these filters"
          description="Use Refresh matches to search your enabled sources, or widen the filters below."
        />
      ) : (
        <>
          <div className="rounded-lg border border-slate-200 bg-white">
            <Table
              caption="Job search results"
              minWidthClassName="min-w-[1120px]"
              head={[
                'Score',
                'Title / Company',
                'Description',
                'Tech (detected)',
                'Location',
                'Type',
                'Arrangement',
                'Salary',
                'Source',
                'First discovered',
                'Link',
              ]}
            >
              {query.data.jobs.map((job) => (
                <JobRow key={job.id} job={job} />
              ))}
            </Table>
          </div>
          <nav aria-label="Pagination" className="flex items-center justify-between text-sm">
            <span className="text-slate-500">
              Page {page} of {totalPages} · {total} results
            </span>
            <div className="flex gap-2">
              <Button variant="secondary" disabled={page <= 1} onClick={() => setPage(page - 1)}>
                Previous
              </Button>
              <Button variant="secondary" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
                Next
              </Button>
            </div>
          </nav>
        </>
      )}
    </div>
  )
}
