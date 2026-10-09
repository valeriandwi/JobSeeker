import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { preferencesApi, scheduleApi, sourcesApi } from '../api'
import type {
  EmploymentType,
  PreferenceArrangement,
  PreferenceLocation,
  Preferences,
  Schedule,
  Source,
  WorkArrangementValue,
} from '../api/types'
import { errorMessage } from '../api/client'
import { Button } from '../components/ui/Button'
import { Input, Select } from '../components/ui/Input'
import { Badge } from '../components/ui/Badge'
import { Toggle } from '../components/ui/Toggle'
import { ChipInput } from '../components/ui/ChipInput'
import { ErrorBanner } from '../components/ErrorBanner'
import { ErrorState, LoadingState } from '../components/ui/States'
import { ARRANGEMENT_OPTIONS, EMPLOYMENT_TYPE_OPTIONS } from '../lib/filters'

const DEFAULT_PREFS: Preferences = {
  job_titles: [],
  keywords: [],
  excluded_keywords: [],
  locations: [],
  work_arrangements: [],
  employment_types: [],
  seniority_levels: [],
  min_salary: null,
  salary_currency: null,
  salary_period: null,
  min_score: 50,
}

function MultiSelectChips<T extends string>({
  label,
  options,
  selected,
  onChange,
}: {
  label: string
  options: readonly T[]
  selected: T[]
  onChange: (v: T[]) => void
}) {
  const toggle = (v: T) => onChange(selected.includes(v) ? selected.filter((x) => x !== v) : [...selected, v])
  return (
    <fieldset className="flex flex-col gap-1">
      <legend className="text-sm font-medium text-slate-700">{label}</legend>
      <div className="flex flex-wrap gap-2">
        {options.map((o) => (
          <label
            key={o}
            className={`inline-flex cursor-pointer items-center gap-1 rounded-full border px-3 py-1 text-xs ${
              selected.includes(o) ? 'border-brand-500 bg-brand-50 text-brand-700' : 'border-slate-300 text-slate-600'
            }`}
          >
            <input
              type="checkbox"
              className="sr-only"
              checked={selected.includes(o)}
              onChange={() => toggle(o)}
            />
            {selected.includes(o) ? '✓ ' : ''}
            {o.replace(/_/g, ' ')}
          </label>
        ))}
      </div>
    </fieldset>
  )
}

function LocationsEditor({
  locations,
  onChange,
}: {
  locations: PreferenceLocation[]
  onChange: (v: PreferenceLocation[]) => void
}) {
  const [country, setCountry] = useState('')
  const [city, setCity] = useState('')
  const add = () => {
    if (!country.trim()) return
    onChange([...locations, { country: country.trim(), city: city.trim() || null, region: null, mandatory: false }])
    setCountry('')
    setCity('')
  }
  return (
    <div className="space-y-2">
      <span className="text-sm font-medium text-slate-700">Locations</span>
      <ul className="space-y-1">
        {locations.map((loc, i) => (
          <li key={`${loc.country}-${i}`} className="flex items-center gap-2 rounded border border-slate-200 px-3 py-2 text-sm">
            <span className="flex-1">
              {loc.city ? `${loc.city}, ` : ''}
              {loc.country}
            </span>
            <Toggle
              label="Mandatory"
              checked={loc.mandatory}
              onChange={(v) => onChange(locations.map((l, j) => (i === j ? { ...l, mandatory: v } : l)))}
            />
            <Button variant="ghost" aria-label={`Remove ${loc.country}`} onClick={() => onChange(locations.filter((_, j) => j !== i))}>
              ✕
            </Button>
          </li>
        ))}
      </ul>
      <div className="flex items-end gap-2">
        <Input label="Country" value={country} onChange={(e) => setCountry(e.target.value)} />
        <Input label="City (optional)" value={city} onChange={(e) => setCity(e.target.value)} />
        <Button type="button" variant="secondary" onClick={add}>
          Add location
        </Button>
      </div>
    </div>
  )
}

function ArrangementsEditor({
  arrangements,
  onChange,
}: {
  arrangements: PreferenceArrangement[]
  onChange: (v: PreferenceArrangement[]) => void
}) {
  const setValue = (value: WorkArrangementValue, mandatory: boolean) => {
    const exists = arrangements.find((a) => a.value === value)
    if (exists) onChange(arrangements.map((a) => (a.value === value ? { ...a, mandatory } : a)))
    else onChange([...arrangements, { value, mandatory }])
  }
  const remove = (value: WorkArrangementValue) => onChange(arrangements.filter((a) => a.value !== value))
  return (
    <fieldset className="space-y-2">
      <legend className="text-sm font-medium text-slate-700">Work arrangements</legend>
      <div className="space-y-1">
        {ARRANGEMENT_OPTIONS.map((value) => {
          const current = arrangements.find((a) => a.value === value)
          return (
            <div key={value} className="flex items-center gap-4 rounded border border-slate-200 px-3 py-2 text-sm">
              <Toggle label={value} checked={Boolean(current)} onChange={(v) => (v ? setValue(value, false) : remove(value))} />
              {current && (
                <Toggle
                  label="Mandatory"
                  checked={current.mandatory}
                  onChange={(v) => setValue(value, v)}
                />
              )}
            </div>
          )
        })}
      </div>
    </fieldset>
  )
}

function SourceRow({ source, onToggle }: { source: Source; onToggle: (enabled: boolean) => void }) {
  return (
    <li className="flex flex-col gap-1 border-b border-slate-100 py-3 last:border-none">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-slate-800">{source.display_name}</span>
          <Badge tone="neutral">{source.kind}</Badge>
          {source.requires_key && <Badge tone={source.configured ? 'success' : 'danger'}>
            {source.configured ? 'Key set' : 'Key required'}
          </Badge>}
          {source.backlink_required && (
            <Badge tone="warning" title="This source requires an attribution backlink">
              ⚠ Attribution required
            </Badge>
          )}
        </div>
        <Toggle label="Enabled" checked={source.enabled} onChange={onToggle} />
      </div>
      {source.attribution_text && (
        <p className="text-xs text-slate-500">
          {source.attribution_text}
          {source.attribution_url && (
            <>
              {' · '}
              <a href={source.attribution_url} target="_blank" rel="noopener noreferrer" className="text-brand-600 underline">
                {source.attribution_url}
              </a>
            </>
          )}
        </p>
      )}
      {source.notes && <p className="text-xs text-slate-400">{source.notes}</p>}
    </li>
  )
}

function WatchlistEditor({ sourceId }: { sourceId: string }) {
  const queryClient = useQueryClient()
  const list = useQuery({ queryKey: ['watchlist', sourceId], queryFn: () => sourcesApi.watchlist(sourceId) })
  const [slug, setSlug] = useState('')
  const [company, setCompany] = useState('')

  const add = useMutation({
    mutationFn: () => sourcesApi.addWatchlist(sourceId, { board_slug: slug.trim(), company: company.trim() }),
    onSuccess: () => {
      setSlug('')
      setCompany('')
      queryClient.invalidateQueries({ queryKey: ['watchlist', sourceId] })
    },
  })
  const remove = useMutation({
    mutationFn: (s: string) => sourcesApi.removeWatchlist(sourceId, s),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['watchlist', sourceId] }),
  })

  return (
    <div className="mt-2 space-y-2 rounded border border-slate-200 p-3">
      <p className="text-xs font-semibold uppercase text-slate-500">ATS board watchlist</p>
      <ErrorBanner error={add.error ?? remove.error} />
      {list.isLoading ? (
        <LoadingState label="Loading watchlist…" />
      ) : list.isError ? (
        <ErrorState message={errorMessage(list.error)} onRetry={() => list.refetch()} />
      ) : (
        <ul className="space-y-1">
          {(list.data?.watchlist ?? []).map((w) => (
            <li key={w.board_slug} className="flex items-center justify-between text-sm">
              <span>
                {w.company} <span className="text-slate-400">({w.board_slug})</span>
              </span>
              <Button variant="ghost" aria-label={`Remove ${w.company}`} onClick={() => remove.mutate(w.board_slug)}>
                ✕
              </Button>
            </li>
          ))}
          {(list.data?.watchlist ?? []).length === 0 && <li className="text-sm text-slate-500">No boards yet.</li>}
        </ul>
      )}
      <div className="flex items-end gap-2">
        <Input label="Board slug" value={slug} onChange={(e) => setSlug(e.target.value)} />
        <Input label="Company" value={company} onChange={(e) => setCompany(e.target.value)} />
        <Button type="button" variant="secondary" disabled={!slug || !company || add.isPending} onClick={() => add.mutate()}>
          Add
        </Button>
      </div>
    </div>
  )
}

export function SearchConfigPage() {
  const queryClient = useQueryClient()
  const prefsQuery = useQuery({ queryKey: ['preferences'], queryFn: preferencesApi.get })
  const scheduleQuery = useQuery({ queryKey: ['schedule'], queryFn: scheduleApi.get })
  const sourcesQuery = useQuery({ queryKey: ['sources'], queryFn: sourcesApi.list })

  const [prefs, setPrefs] = useState<Preferences>(DEFAULT_PREFS)
  const [schedule, setSchedule] = useState<Pick<Schedule, 'cron_spec' | 'timezone' | 'enabled'>>({
    cron_spec: '',
    timezone: 'UTC',
    enabled: false,
  })

  useEffect(() => {
    if (prefsQuery.data) setPrefs(prefsQuery.data.preferences)
  }, [prefsQuery.data])
  useEffect(() => {
    if (scheduleQuery.data) {
      setSchedule({
        cron_spec: scheduleQuery.data.cron_spec,
        timezone: scheduleQuery.data.timezone,
        enabled: scheduleQuery.data.enabled,
      })
    }
  }, [scheduleQuery.data])

  const prefsMutation = useMutation({
    mutationFn: preferencesApi.update,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['preferences'] })
      queryClient.invalidateQueries({ queryKey: ['dashboard'] })
    },
  })
  const scheduleMutation = useMutation({
    mutationFn: scheduleApi.update,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['schedule'] }),
  })
  const sourceMutation = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => sourcesApi.setEnabled(id, enabled),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['sources'] }),
  })

  if (prefsQuery.isLoading || scheduleQuery.isLoading || sourcesQuery.isLoading) {
    return <LoadingState label="Loading configuration…" />
  }
  if (prefsQuery.isError) return <ErrorState message={errorMessage(prefsQuery.error)} onRetry={() => prefsQuery.refetch()} />

  const set = <K extends keyof Preferences>(key: K, value: Preferences[K]) =>
    setPrefs((p) => ({ ...p, [key]: value }))

  return (
    <div className="space-y-6">
      <h1 className="text-xl font-semibold">Search Configuration</h1>

      <section className="space-y-4 rounded-lg border border-slate-200 bg-white p-4">
        <h2 className="text-sm font-semibold text-slate-700">Targeting</h2>
        <ChipInput label="Job titles" values={prefs.job_titles} onChange={(v) => set('job_titles', v)} />
        <ChipInput label="Keywords" values={prefs.keywords} onChange={(v) => set('keywords', v)} />
        <ChipInput label="Excluded keywords" values={prefs.excluded_keywords} onChange={(v) => set('excluded_keywords', v)} />
      </section>

      <section className="space-y-4 rounded-lg border border-slate-200 bg-white p-4">
        <h2 className="text-sm font-semibold text-slate-700">Locations &amp; arrangements</h2>
        <LocationsEditor locations={prefs.locations} onChange={(v) => set('locations', v)} />
        <ArrangementsEditor arrangements={prefs.work_arrangements} onChange={(v) => set('work_arrangements', v)} />
        <MultiSelectChips<EmploymentType>
          label="Employment types"
          options={EMPLOYMENT_TYPE_OPTIONS}
          selected={prefs.employment_types}
          onChange={(v) => set('employment_types', v)}
        />
        <ChipInput label="Seniority levels" values={prefs.seniority_levels} onChange={(v) => set('seniority_levels', v)} />
      </section>

      <section className="space-y-4 rounded-lg border border-slate-200 bg-white p-4">
        <h2 className="text-sm font-semibold text-slate-700">Compensation &amp; match</h2>
        <div className="grid gap-3 sm:grid-cols-3">
          <Input
            label="Minimum salary"
            type="number"
            value={prefs.min_salary ?? ''}
            onChange={(e) => set('min_salary', e.target.value === '' ? null : Number(e.target.value))}
          />
          <Select
            label="Currency"
            value={prefs.salary_currency ?? ''}
            onChange={(e) => set('salary_currency', (e.target.value || null) as Preferences['salary_currency'])}
          >
            <option value="">None</option>
            <option value="USD">USD</option>
          </Select>
          <Select
            label="Period"
            value={prefs.salary_period ?? ''}
            onChange={(e) => set('salary_period', (e.target.value || null) as Preferences['salary_period'])}
          >
            <option value="">None</option>
            <option value="year">Year</option>
            <option value="month">Month</option>
          </Select>
        </div>
        <div>
          <label htmlFor="min-score" className="text-sm font-medium text-slate-700">
            Minimum match score: <strong>{prefs.min_score}</strong>
          </label>
          <input
            id="min-score"
            type="range"
            min={0}
            max={100}
            value={prefs.min_score}
            onChange={(e) => set('min_score', Number(e.target.value))}
            className="mt-2 w-full accent-brand-600"
          />
        </div>
      </section>

      <section className="space-y-2 rounded-lg border border-slate-200 bg-white p-4">
        <h2 className="text-sm font-semibold text-slate-700">Sources</h2>
        {sourcesQuery.isError ? (
          <ErrorState message={errorMessage(sourcesQuery.error)} onRetry={() => sourcesQuery.refetch()} />
        ) : (
          <ul>
            {(sourcesQuery.data?.sources ?? []).map((s) => (
              <div key={s.id}>
                <SourceRow source={s} onToggle={(enabled) => sourceMutation.mutate({ id: s.id, enabled })} />
                {s.kind === 'ats' && s.enabled && <WatchlistEditor sourceId={s.id} />}
              </div>
            ))}
          </ul>
        )}
        <ErrorBanner error={sourceMutation.error} />
      </section>

      <section className="space-y-4 rounded-lg border border-slate-200 bg-white p-4">
        <h2 className="text-sm font-semibold text-slate-700">Schedule</h2>
        {scheduleQuery.isError ? (
          <ErrorState message={errorMessage(scheduleQuery.error)} onRetry={() => scheduleQuery.refetch()} />
        ) : (
          <div className="grid gap-3 sm:grid-cols-3">
            <Input
              label="Cron spec"
              value={schedule.cron_spec}
              onChange={(e) => setSchedule({ ...schedule, cron_spec: e.target.value })}
              hint="e.g. 0 6 * * *"
            />
            <Input
              label="Timezone"
              value={schedule.timezone}
              onChange={(e) => setSchedule({ ...schedule, timezone: e.target.value })}
            />
            <div className="flex items-end pb-2">
              <Toggle
                label="Enabled"
                checked={schedule.enabled}
                onChange={(v) => setSchedule({ ...schedule, enabled: v })}
              />
            </div>
          </div>
        )}
      </section>

      <div className="flex items-center justify-end gap-3">
        <ErrorBanner error={prefsMutation.error} />
        <ErrorBanner error={scheduleMutation.error} />
        <Button
          disabled={prefsMutation.isPending || scheduleMutation.isPending}
          onClick={() => {
            prefsMutation.mutate(prefs)
            scheduleMutation.mutate(schedule)
          }}
        >
          Save configuration
        </Button>
      </div>
      {(prefsMutation.isSuccess || scheduleMutation.isSuccess) && (
        <p role="status" className="text-right text-sm text-green-700">
          Saved.
        </p>
      )}
    </div>
  )
}
