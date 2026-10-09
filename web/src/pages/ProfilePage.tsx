import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { profileApi, resumesApi } from '../api'
import type { DraftField, Profile, Resume } from '../api/types'
import { errorMessage } from '../api/client'
import { Button } from '../components/ui/Button'
import { Input } from '../components/ui/Input'
import { Badge } from '../components/ui/Badge'
import { ChipInput } from '../components/ui/ChipInput'
import { Modal } from '../components/ui/Modal'
import { ErrorBanner } from '../components/ErrorBanner'
import { EmptyState, ErrorState, LoadingState } from '../components/ui/States'

const MAX_BYTES = 10 * 1024 * 1024
const ACCEPT = '.pdf,.docx,application/pdf,application/vnd.openxmlformats-officedocument.wordprocessingml.document'

interface ReviewRow {
  key: string
  label: string
  field: DraftField
}

function draftRows(draft: Record<string, DraftField>): ReviewRow[] {
  return Object.entries(draft).map(([key, field]) => ({
    key,
    label: key.replace(/_/g, ' '),
    field,
  }))
}

/** A single field row distinguishing inferred vs confirmed values visually. */
export function ProvenanceField({
  label,
  field,
  value,
  confirmed,
  onChange,
  onConfirm,
}: {
  label: string
  field: DraftField
  value: string
  confirmed: boolean
  onChange: (v: string) => void
  onConfirm: () => void
}) {
  const inferred = !confirmed
  return (
    <div
      className={`rounded-md border p-3 ${
        inferred ? 'border-amber-200 bg-amber-50' : 'border-green-200 bg-green-50'
      }`}
    >
      <div className="mb-2 flex items-center justify-between gap-2">
        <span className="text-sm font-medium capitalize text-slate-700">{label}</span>
        <Badge tone={inferred ? 'warning' : 'success'}>
          {inferred ? 'Inferred' : 'Confirmed'}
          {inferred && field.confidence != null ? ` · ${Math.round(field.confidence * 100)}%` : ''}
        </Badge>
      </div>
      <div className="flex items-end gap-2">
        <Input
          label={`${label} value`}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className="flex-1"
        />
        {inferred && (
          <Button type="button" variant="secondary" onClick={onConfirm}>
            Accept
          </Button>
        )}
      </div>
    </div>
  )
}

function ResumeDropzone({
  onFile,
  busy,
}: {
  onFile: (file: File) => void
  busy: boolean
}) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [dragging, setDragging] = useState(false)
  const [localError, setLocalError] = useState<string | null>(null)

  const handle = (file: File | undefined) => {
    if (!file) return
    if (file.size > MAX_BYTES) {
      setLocalError('File exceeds the 10MB limit.')
      return
    }
    const ok = /\.(pdf|docx)$/i.test(file.name)
    if (!ok) {
      setLocalError('Only PDF or DOCX files are accepted.')
      return
    }
    setLocalError(null)
    onFile(file)
  }

  return (
    <div>
      <div
        role="button"
        tabIndex={0}
        aria-label="Upload resume"
        aria-disabled={busy}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') inputRef.current?.click()
        }}
        onClick={() => inputRef.current?.click()}
        onDragOver={(e) => {
          e.preventDefault()
          setDragging(true)
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault()
          setDragging(false)
          handle(e.dataTransfer.files[0])
        }}
        className={`cursor-pointer rounded-lg border-2 border-dashed px-6 py-8 text-center text-sm transition-colors ${
          dragging ? 'border-brand-500 bg-brand-50' : 'border-slate-300 bg-white'
        }`}
      >
        <p className="font-medium text-slate-700">Drag &amp; drop your CV here</p>
        <p className="mt-1 text-slate-500">or click to browse · PDF or DOCX · max 10MB</p>
        <input
          ref={inputRef}
          type="file"
          accept={ACCEPT}
          className="sr-only"
          disabled={busy}
          onChange={(e) => handle(e.target.files?.[0] ?? undefined)}
        />
      </div>
      {localError && (
        <p role="alert" className="mt-2 text-sm text-red-600">
          {localError}
        </p>
      )}
    </div>
  )
}

function ResumeList({
  resumes,
  onDelete,
  deleting,
}: {
  resumes: Resume[]
  onDelete: (id: string) => void
  deleting: boolean
}) {
  if (resumes.length === 0) return <EmptyState title="No CV uploaded yet" description="Upload a PDF or DOCX to start." />
  return (
    <ul className="divide-y divide-slate-100 rounded-lg border border-slate-200 bg-white">
      {resumes.map((r) => {
        const name = r.original_filename ?? r.file_name ?? 'Resume'
        const status = r.status ?? 'stored'
        return (
          <li key={r.id} className="flex items-center justify-between gap-3 px-4 py-3 text-sm">
            <div>
              <p className="font-medium text-slate-800">{name}</p>
              <p className="text-slate-500">
                Status: <Badge tone={status === 'parsed' ? 'success' : 'info'}>{status}</Badge>
              </p>
            </div>
            <Button variant="danger" disabled={deleting} onClick={() => onDelete(r.id)}>
              Delete
            </Button>
          </li>
        )
      })}
    </ul>
  )
}

export function ProfilePage() {
  const queryClient = useQueryClient()
  const profileQuery = useQuery({ queryKey: ['profile'], queryFn: profileApi.get })
  const resumesQuery = useQuery({ queryKey: ['resumes'], queryFn: resumesApi.list })

  const [reviewDraft, setReviewDraft] = useState<Record<string, DraftField> | null>(null)
  const [editedValues, setEditedValues] = useState<Record<string, string>>({})

  const uploadMutation = useMutation({
    mutationFn: (file: File) => resumesApi.upload(file),
    onSuccess: (result) => {
      if (result.draft && Object.keys(result.draft).length > 0) {
        setReviewDraft(result.draft)
        setEditedValues({})
      }
      queryClient.invalidateQueries({ queryKey: ['resumes'] })
      queryClient.invalidateQueries({ queryKey: ['profile'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => resumesApi.remove(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['resumes'] }),
  })

  const profileMutation = useMutation({
    mutationFn: (profile: Profile) => profileApi.update(profile),
    onSuccess: () => {
      setReviewDraft(null)
      queryClient.invalidateQueries({ queryKey: ['profile'] })
    },
  })

  const confirmField = (key: string) => {
    if (!profileQuery.data || reviewDraft === null) return
    const current = profileQuery.data.profile
    const rawValue = editedValues[key] ?? String(reviewDraft[key]?.value ?? '')
    const next: Profile = {
      ...current,
      confirmed_field_keys: Array.from(new Set([...current.confirmed_field_keys, key])),
    }
    if (key === 'skills') {
      next.skills = rawValue
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean)
    } else if (key in next) {
      ;(next as unknown as Record<string, unknown>)[key] = rawValue
    }
    profileMutation.mutate(next)
  }

  if (profileQuery.isLoading || resumesQuery.isLoading) return <LoadingState label="Loading profile…" />

  return (
    <div className="space-y-6">
      <h1 className="text-xl font-semibold">Candidate Profile</h1>

      <section aria-labelledby="cv-heading" className="space-y-3">
        <h2 id="cv-heading" className="text-sm font-semibold text-slate-700">
          CV upload
        </h2>
        <ResumeDropzone onFile={(file) => uploadMutation.mutate(file)} busy={uploadMutation.isPending} />
        {uploadMutation.isPending && <LoadingState label="Parsing your CV…" />}
        <ErrorBanner error={uploadMutation.error} />
        {resumesQuery.isError ? (
          <ErrorState message={errorMessage(resumesQuery.error)} onRetry={() => resumesQuery.refetch()} />
        ) : (
          <ResumeList
            resumes={resumesQuery.data?.resumes ?? []}
            onDelete={(id) => deleteMutation.mutate(id)}
            deleting={deleteMutation.isPending}
          />
        )}
        <ErrorBanner error={deleteMutation.error} />
      </section>

      {reviewDraft && (
        <Modal
          open
          title="Review parsed fields"
          onClose={() => setReviewDraft(null)}
          footer={
            <Button variant="secondary" onClick={() => setReviewDraft(null)}>
              Close
            </Button>
          }
        >
          <p className="mb-3 text-sm text-slate-600">
            Values marked <strong>Inferred</strong> were extracted automatically. Accept or edit each before it is
            treated as confirmed.
          </p>
          <ErrorBanner error={profileMutation.error} />
          <div className="space-y-3">
            {draftRows(reviewDraft).map((row) => (
              <ProvenanceField
                key={row.key}
                label={row.label}
                field={row.field}
                value={editedValues[row.key] ?? String(row.field.value ?? '')}
                confirmed={false}
                onChange={(v) => setEditedValues((prev) => ({ ...prev, [row.key]: v }))}
                onConfirm={() => confirmField(row.key)}
              />
            ))}
          </div>
        </Modal>
      )}

      <section aria-labelledby="profile-heading" className="space-y-3">
        <h2 id="profile-heading" className="text-sm font-semibold text-slate-700">
          Profile
        </h2>
        {profileQuery.isError ? (
          <ErrorState message={errorMessage(profileQuery.error)} onRetry={() => profileQuery.refetch()} />
        ) : (
          <ProfileEditor
            profile={profileQuery.data!.profile}
            onSave={(p) => profileMutation.mutate(p)}
            saving={profileMutation.isPending}
            error={profileMutation.error}
          />
        )}
      </section>
    </div>
  )
}

function ProfileEditor({
  profile,
  onSave,
  saving,
  error,
}: {
  profile: Profile
  onSave: (p: Profile) => void
  saving: boolean
  error: unknown
}) {
  const [draft, setDraft] = useState<Profile>(profile)
  const set = <K extends keyof Profile>(key: K, value: Profile[K]) => setDraft((d) => ({ ...d, [key]: value }))

  return (
    <div className="space-y-4 rounded-lg border border-slate-200 bg-white p-4">
      <ErrorBanner error={error} />
      <div className="grid gap-3 sm:grid-cols-2">
        <Input label="Full name" value={draft.full_name ?? ''} onChange={(e) => set('full_name', e.target.value)} />
        <Input label="Current title" value={draft.current_title ?? ''} onChange={(e) => set('current_title', e.target.value)} />
        <Input label="Headline" value={draft.headline ?? ''} onChange={(e) => set('headline', e.target.value)} />
        <Input label="Seniority" value={draft.seniority ?? ''} onChange={(e) => set('seniority', e.target.value)} />
        <Input
          label="Years of experience"
          type="number"
          value={draft.years_experience ?? ''}
          onChange={(e) => set('years_experience', e.target.value === '' ? null : Number(e.target.value))}
        />
      </div>
      <ChipInput label="Skills" values={draft.skills} onChange={(v) => set('skills', v)} />
      <ChipInput label="Industries" values={draft.industries} onChange={(v) => set('industries', v)} />
      <ChipInput label="Languages" values={draft.languages} onChange={(v) => set('languages', v)} />
      <div className="flex justify-end">
        <Button disabled={saving} onClick={() => onSave(draft)}>
          {saving ? 'Saving…' : 'Save profile'}
        </Button>
      </div>
    </div>
  )
}
