import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { applicationsApi, jobsApi } from "../api";
import type { Application, ApplicationStatus, SavedJob } from "../api/types";
import { errorMessage } from "../api/client";
import { Button } from "../components/ui/Button";
import { Badge } from "../components/ui/Badge";
import { Select, Input, Textarea } from "../components/ui/Input";
import { Modal } from "../components/ui/Modal";
import { ErrorBanner } from "../components/ErrorBanner";
import { EmptyState, ErrorState, LoadingState } from "../components/ui/States";
import { ScoreBadge } from "../components/ScoreBadge";
import { relativeTime, UNKNOWN_LABEL } from "../lib/format";
import { APPLICATION_STATUSES, applicationStatusTone } from "../lib/status";

function TrackerEditor({
  jobId,
  application,
  onClose,
}: {
  jobId: string;
  application?: Application;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<ApplicationStatus>(
    application?.status ?? "applied",
  );
  const [appliedAt, setAppliedAt] = useState(
    application?.applied_at?.slice(0, 10) ?? "",
  );
  const [notes, setNotes] = useState(application?.notes ?? "");
  const [followUpAt, setFollowUpAt] = useState(
    application?.follow_up_at?.slice(0, 10) ?? "",
  );

  const mutation = useMutation({
    mutationFn: () =>
      applicationsApi.update(jobId, {
        status,
        applied_at: appliedAt ? new Date(appliedAt).toISOString() : null,
        notes: notes || null,
        follow_up_at: followUpAt ? new Date(followUpAt).toISOString() : null,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["applications"] });
      queryClient.invalidateQueries({ queryKey: ["saved-jobs"] });
      onClose();
    },
  });

  return (
    <>
      <ErrorBanner error={mutation.error} />
      <div className="space-y-3">
        <Select
          label="Status"
          value={status}
          onChange={(e) => setStatus(e.target.value as ApplicationStatus)}
        >
          {APPLICATION_STATUSES.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </Select>
        <Input
          label="Applied date"
          type="date"
          value={appliedAt}
          onChange={(e) => setAppliedAt(e.target.value)}
        />
        <Input
          label="Follow-up date"
          type="date"
          value={followUpAt}
          onChange={(e) => setFollowUpAt(e.target.value)}
        />
        <Textarea
          label="Notes"
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          rows={4}
        />
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button
            disabled={mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending ? "Saving…" : "Save"}
          </Button>
        </div>
      </div>
    </>
  );
}

/** One row of the tracker: a bookmark, an application, or both. */
interface TrackerEntry {
  jobId: string;
  title: string;
  company: string;
  url: string;
  /** Source id from the bookmark row; applications do not carry one. */
  source: string;
  matchScore: number | null;
  bookmarked: boolean;
  application?: Application;
}

/**
 * Merge bookmarks and applications into one list.
 *
 * Two reasons this is a union rather than just the bookmark list:
 *   1. A job can have an application record WITHOUT a bookmark — un-saving on
 *      this page removes the bookmark and deliberately keeps the application,
 *      so listing only bookmarks hid applied jobs from a page whose job is to
 *      track them.
 *   2. The two endpoints denormalise differently (a bookmark row has a source
 *      and a score, an application row has a status and dates), so the merge
 *      fills the gaps from whichever row exists.
 */
export function mergeTrackerEntries(
  savedJobs: SavedJob[],
  applications: Application[],
): TrackerEntry[] {
  const appByJob = new Map(applications.map((a) => [a.job_id, a]));
  const entries: TrackerEntry[] = [];
  const seen = new Set<string>();

  for (const saved of savedJobs) {
    seen.add(saved.job_id);
    entries.push({
      jobId: saved.job_id,
      title: saved.title,
      company: saved.company,
      url: saved.url,
      source: saved.source,
      matchScore: saved.match_score,
      bookmarked: true,
      application: appByJob.get(saved.job_id),
    });
  }

  for (const app of applications) {
    if (seen.has(app.job_id)) continue; // already listed as a bookmark
    entries.push({
      jobId: app.job_id,
      title: app.title,
      company: app.company,
      url: app.url,
      source: "",
      matchScore: null, // the applications endpoint returns no score
      bookmarked: false,
      application: app,
    });
  }

  return entries;
}

export function SavedJobsPage() {
  const queryClient = useQueryClient();
  const saved = useQuery({ queryKey: ["saved-jobs"], queryFn: jobsApi.saved });
  const apps = useQuery({
    queryKey: ["applications"],
    queryFn: applicationsApi.list,
  });
  const [editing, setEditing] = useState<string | null>(null);

  const unsave = useMutation({
    mutationFn: (id: string) => jobsApi.unsave(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["saved-jobs"] });
      queryClient.invalidateQueries({ queryKey: ["applications"] });
    },
  });

  // `saved_jobs`, matching the API. Reading `jobs` here is what made this page
  // render as empty no matter how many jobs were bookmarked.
  const entries = mergeTrackerEntries(
    saved.data?.saved_jobs ?? [],
    apps.data?.applications ?? [],
  );
  const appFor = (jobId: string): Application | undefined =>
    entries.find((e) => e.jobId === jobId)?.application;

  const isLoading = saved.isLoading || apps.isLoading;
  const isError = saved.isError || apps.isError;
  const error = saved.error ?? apps.error;

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">
        Saved Jobs &amp; Application Tracker
      </h1>
      <p className="text-sm text-slate-600">
        Bookmarked vacancies and every application you have recorded — an
        application stays here even if you remove its bookmark.
      </p>
      <ErrorBanner error={unsave.error} />

      {isLoading ? (
        <LoadingState label="Loading saved jobs…" />
      ) : isError ? (
        <ErrorState
          message={errorMessage(error)}
          onRetry={() => {
            saved.refetch();
            apps.refetch();
          }}
        />
      ) : entries.length === 0 ? (
        <EmptyState
          title="Nothing tracked yet"
          description="Save jobs or mark an application as sent from Job Discovery to track them here."
        />
      ) : (
        <ul className="space-y-3">
          {entries.map((entry) => {
            const app = entry.application;
            return (
              <li
                key={entry.jobId}
                className="rounded-lg border border-slate-200 bg-white p-4"
              >
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      {entry.matchScore !== null && (
                        <ScoreBadge score={entry.matchScore} />
                      )}
                      <p className="font-medium text-slate-900">{entry.title}</p>
                    </div>
                    <p className="text-sm text-slate-600">
                      {entry.company || UNKNOWN_LABEL}
                      {entry.source && ` · ${entry.source}`}
                    </p>
                    <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-slate-500">
                      <Badge tone={applicationStatusTone(app?.status ?? "saved")}>
                        {app?.status ?? "saved"}
                      </Badge>
                      {!entry.bookmarked && (
                        <Badge
                          tone="neutral"
                          title="You applied to this vacancy but it is not bookmarked. Use Save on Job Discovery to bookmark it."
                        >
                          not bookmarked
                        </Badge>
                      )}
                      {app?.applied_at && (
                        <span>Applied {relativeTime(app.applied_at)}</span>
                      )}
                      {app?.follow_up_at && (
                        <span>Follow-up {relativeTime(app.follow_up_at)}</span>
                      )}
                      {entry.url && (
                        <a
                          href={entry.url}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-brand-600 hover:underline"
                        >
                          Open original ↗
                        </a>
                      )}
                    </div>
                    {app?.notes && (
                      <p className="mt-1 text-sm text-slate-600">
                        Notes: {app.notes}
                      </p>
                    )}
                  </div>
                  <div className="flex gap-2">
                    <Button
                      variant="secondary"
                      onClick={() => setEditing(entry.jobId)}
                    >
                      Update status
                    </Button>
                    {/* Only a bookmark can be removed here: this page never
                        deletes an application, so an unsaved entry has nothing
                        destructive to offer. */}
                    {entry.bookmarked && (
                      <Button
                        variant="danger"
                        disabled={unsave.isPending}
                        onClick={() => unsave.mutate(entry.jobId)}
                      >
                        Unsave
                      </Button>
                    )}
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      )}

      {editing && (
        <Modal open title="Update application" onClose={() => setEditing(null)}>
          <TrackerEditor
            jobId={editing}
            application={appFor(editing)}
            onClose={() => setEditing(null)}
          />
        </Modal>
      )}
    </div>
  );
}
