import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { applicationsApi, jobsApi } from '../api'
import type { Application, ApplicationStatus } from '../api/types'
import { errorMessage } from '../api/client'
import { Button } from '../components/ui/Button'
import { Badge } from '../components/ui/Badge'
import { Select, Input, Textarea } from '../components/ui/Input'
import { Modal } from '../components/ui/Modal'
import { ErrorBanner } from '../components/ErrorBanner'
import { EmptyState, ErrorState, LoadingState } from '../components/ui/States'
import { relativeTime, UNKNOWN_LABEL } from '../lib/format'
import { APPLICATION_STATUSES, applicationStatusTone } from '../lib/status'

function TrackerEditor({
  jobId,
  application,
  onClose,
}: {
  jobId: string
  application?: Application
  onClose: () => void
}) {
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<ApplicationStatus>(application?.status ?? 'applied')
  const [appliedAt, setAppliedAt] = useState(application?.applied_at?.slice(0, 10) ?? '')
  const [notes, setNotes] = useState(application?.notes ?? '')
  const [followUpAt, setFollowUpAt] = useState(application?.follow_up_at?.slice(0, 10) ?? '')

  const mutation = useMutation({
    mutationFn: () =>
      applicationsApi.update(jobId, {
        status,
        applied_at: appliedAt ? new Date(appliedAt).toISOString() : null,
        notes: notes || null,
        follow_up_at: followUpAt ? new Date(followUpAt).toISOString() : null,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['applications'] })
      queryClient.invalidateQueries({ queryKey: ['saved-jobs'] })
      onClose()
    },
  })

  return (
    <>
      <ErrorBanner error={mutation.error} />
      <div className="space-y-3">
        <Select label="Status" value={status} onChange={(e) => setStatus(e.target.value as ApplicationStatus)}>
          {APPLICATION_STATUSES.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </Select>
        <Input label="Applied date" type="date" value={appliedAt} onChange={(e) => setAppliedAt(e.target.value)} />
        <Input label="Follow-up date" type="date" value={followUpAt} onChange={(e) => setFollowUpAt(e.target.value)} />
        <Textarea label="Notes" value={notes} onChange={(e) => setNotes(e.target.value)} rows={4} />
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button disabled={mutation.isPending} onClick={() => mutation.mutate()}>
            {mutation.isPending ? 'Saving…' : 'Save'}
          </Button>
        </div>
      </div>
    </>
  )
}

export function SavedJobsPage() {
  const queryClient = useQueryClient()
  const saved = useQuery({ queryKey: ['saved-jobs'], queryFn: jobsApi.saved })
  const apps = useQuery({ queryKey: ['applications'], queryFn: applicationsApi.list })
  const [editing, setEditing] = useState<string | null>(null)

  const unsave = useMutation({
    mutationFn: (id: string) => jobsApi.unsave(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['saved-jobs'] })
      queryClient.invalidateQueries({ queryKey: ['applications'] })
    },
  })

  const appFor = (jobId: string): Application | undefined => apps.data?.applications.find((a) => a.job_id === jobId)

  const isLoading = saved.isLoading || apps.isLoading
  const isError = saved.isError || apps.isError
  const error = saved.error ?? apps.error

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">Saved Jobs &amp; Application Tracker</h1>
      <ErrorBanner error={unsave.error} />

      {isLoading ? (
        <LoadingState label="Loading saved jobs…" />
      ) : isError ? (
        <ErrorState
          message={errorMessage(error)}
          onRetry={() => {
            saved.refetch()
            apps.refetch()
          }}
        />
      ) : !saved.data || saved.data.jobs.length === 0 ? (
        <EmptyState title="No saved jobs" description="Save jobs from Job Discovery to track them here." />
      ) : (
        <ul className="space-y-3">
          {saved.data.jobs.map((job) => {
            const app = appFor(job.id)
            return (
              <li key={job.id} className="rounded-lg border border-slate-200 bg-white p-4">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <p className="font-medium text-slate-900">{job.title}</p>
                    <p className="text-sm text-slate-600">
                      {job.company} · {job.location_city ?? UNKNOWN_LABEL}
                    </p>
                    <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-slate-500">
                      <Badge tone={applicationStatusTone(app?.status ?? 'saved')}>{app?.status ?? 'saved'}</Badge>
                      {app?.applied_at && <span>Applied {relativeTime(app.applied_at)}</span>}
                      {app?.follow_up_at && <span>Follow-up {relativeTime(app.follow_up_at)}</span>}
                    </div>
                    {app?.notes && <p className="mt-1 text-sm text-slate-600">Notes: {app.notes}</p>}
                  </div>
                  <div className="flex gap-2">
                    <Button variant="secondary" onClick={() => setEditing(job.id)}>
                      Update status
                    </Button>
                    <Button variant="danger" disabled={unsave.isPending} onClick={() => unsave.mutate(job.id)}>
                      Unsave
                    </Button>
                  </div>
                </div>
              </li>
            )
          })}
        </ul>
      )}

      {editing && (
        <Modal open title="Update application" onClose={() => setEditing(null)}>
          <TrackerEditor jobId={editing} application={appFor(editing)} onClose={() => setEditing(null)} />
        </Modal>
      )}
    </div>
  )
}
