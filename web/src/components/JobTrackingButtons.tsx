import { Badge } from './ui/Badge'
import { Button } from './ui/Button'

/** Pipeline stages that can only exist after an application was actually sent. */
const SENT_STATUSES = new Set(['applied', 'interviewing', 'rejected', 'accepted', 'withdrawn'])

/**
 * True when the user has sent an application for this job.
 *
 * Deliberately derived from the applications record rather than `job.status`:
 * the tracker maps `accepted` and `withdrawn` to the job status `saved`
 * (`jobStatusFor`), so the job status alone under-reports "sent".
 */
export function hasApplied(status: string | null | undefined): boolean {
  return !!status && SENT_STATUSES.has(status)
}

/**
 * Human label for an application status. Falls back to the raw value so an
 * unknown status from the API is shown as-is rather than hidden.
 */
export function applicationStatusLabel(status: string): string {
  switch (status) {
    case 'saved':
      return 'Saved'
    case 'applied':
      return 'Applied'
    case 'interviewing':
      return 'Interviewing'
    case 'rejected':
      return 'Rejected'
    case 'accepted':
      return 'Accepted'
    case 'withdrawn':
      return 'Withdrawn'
    default:
      return status
  }
}

/** Icon colour for the pipeline stages that have no toggle. */
const STAGE_TEXT_CLASS: Record<string, string> = {
  interviewing: 'text-blue-600',
  accepted: 'text-green-600',
  rejected: 'text-red-600',
  withdrawn: 'text-slate-500',
}

/** Bookmark outline, filled once the job is saved. */
function BookmarkIcon({ filled }: { filled: boolean }) {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      viewBox="0 0 24 24"
      className="h-4 w-4"
      fill={filled ? 'currentColor' : 'none'}
      stroke="currentColor"
      strokeWidth="1.75"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M6 4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v16l-6-3.6L6 20V4z" />
    </svg>
  )
}

/** Paper plane, filled once an application has been sent. */
function SendIcon({ filled }: { filled: boolean }) {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      viewBox="0 0 24 24"
      className="h-4 w-4"
      fill={filled ? 'currentColor' : 'none'}
      stroke="currentColor"
      strokeWidth="1.75"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M21 3 2.8 10.6l6.9 2.7 2.7 6.9L21 3z" />
    </svg>
  )
}

interface JobTrackingButtonsProps {
  /** Bookmarked? From the user's saved jobs. */
  bookmarked: boolean
  /** The user's application for this job, if any. */
  applicationStatus?: string | null
  /** True while either mutation is in flight. */
  pending?: boolean
  /** Both actions, already wired to the API by the caller. */
  onToggleSave: () => void
  onToggleApplied: () => void
}

/**
 * Save + "application sent" controls for one row, as icon-only buttons.
 *
 * Two icons rather than one: bookmarking a vacancy and having applied to it are
 * different facts, stored in different tables (and the tracker bookmarks on
 * upsert anyway, so "saved" alone says nothing about whether you applied).
 *
 * Because the buttons carry no visible text, each one's accessible name states
 * the value AND the action ("Saved job — remove bookmark"), `aria-pressed`
 * carries the toggle state, and `title` provides the mouse tooltip.
 */
export function JobTrackingButtons({
  bookmarked,
  applicationStatus,
  pending = false,
  onToggleSave,
  onToggleApplied,
}: JobTrackingButtonsProps) {
  const sent = hasApplied(applicationStatus)

  return (
    <div className="flex items-center gap-1">
      <Button
        variant="secondary"
        className={`p-1.5 ${bookmarked ? 'text-brand-600' : 'text-slate-500'}`}
        aria-pressed={bookmarked}
        aria-label={bookmarked ? 'Saved job — remove bookmark' : 'Save job'}
        disabled={pending}
        title={bookmarked ? 'Saved — click to remove the bookmark' : 'Save this vacancy'}
        onClick={onToggleSave}
      >
        <BookmarkIcon filled={bookmarked} />
      </Button>

      {applicationStatus === 'applied' ? (
        // Exactly "applied": clicking again takes it back to saved (not sent).
        <Button
          variant="secondary"
          className="p-1.5 text-green-600"
          aria-pressed
          aria-label="Application sent — mark as not sent"
          disabled={pending}
          title="Application sent — click to take it back"
          onClick={onToggleApplied}
        >
          <SendIcon filled />
        </Button>
      ) : sent ? (
        // A later pipeline stage carries more information than a toggle can, so
        // the icon stays read-only and the stage travels in the label + tooltip.
        <Badge
          tone="info"
          className={STAGE_TEXT_CLASS[applicationStatus ?? ''] ?? ''}
          title={`Application status: ${applicationStatusLabel(applicationStatus ?? '')} — manage it on the Saved Jobs page.`}
        >
          <SendIcon filled />
          <span className="sr-only">Application status: {applicationStatusLabel(applicationStatus ?? '')}</span>
        </Badge>
      ) : (
        <Button
          variant="secondary"
          className="p-1.5 text-slate-500"
          aria-pressed={false}
          aria-label="Mark application as sent"
          disabled={pending}
          title="Record that you have sent an application"
          onClick={onToggleApplied}
        >
          <SendIcon filled={false} />
        </Button>
      )}
    </div>
  )
}
