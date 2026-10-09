// Package tracker owns saved jobs, dismissals and the application pipeline.
package tracker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/valeriandwipurnomo/jobseeker/internal/storage"
)

// ErrNotFound reports a missing row.
var ErrNotFound = errors.New("tracker: not found")

// ErrInvalidStatus reports an application status outside the allowed set.
var ErrInvalidStatus = errors.New("tracker: invalid application status")

// Valid application statuses.
var applicationStatuses = map[string]bool{
	"saved": true, "applied": true, "interviewing": true,
	"rejected": true, "accepted": true, "withdrawn": true,
}

// IsValidStatus reports whether a status string is accepted by the schema.
func IsValidStatus(s string) bool { return applicationStatuses[s] }

// SavedJob is one bookmarked vacancy.
type SavedJob struct {
	JobID   uuid.UUID `json:"job_id"`
	Title   string    `json:"title"`
	Company string    `json:"company"`
	URL     string    `json:"url"`
	Source  string    `json:"source"`
	Score   *int      `json:"match_score"`
	SavedAt time.Time `json:"saved_at"`
}

// Application tracks a user's progress on a vacancy.
type Application struct {
	JobID      uuid.UUID  `json:"job_id"`
	Title      string     `json:"title"`
	Company    string     `json:"company"`
	URL        string     `json:"url"`
	Status     string     `json:"status"`
	AppliedAt  *time.Time `json:"applied_at"`
	FollowUpAt *time.Time `json:"follow_up_at"`
	Notes      string     `json:"notes"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// Repository persists tracker state.
type Repository struct {
	db *storage.DB
}

// NewRepository wires the repository to the connection pool.
func NewRepository(db *storage.DB) *Repository { return &Repository{db: db} }

// Save bookmarks a vacancy.
func (r *Repository) Save(ctx context.Context, userID, jobID uuid.UUID) error {
	if _, err := r.db.Pool.Exec(ctx, `
		insert into saved_jobs (user_id, job_id) values ($1, $2)
		on conflict (user_id, job_id) do nothing`, userID, jobID); err != nil {
		return fmt.Errorf("saving job: %w", err)
	}
	return nil
}

// Unsave removes a bookmark.
func (r *Repository) Unsave(ctx context.Context, userID, jobID uuid.UUID) error {
	if _, err := r.db.Pool.Exec(ctx, `delete from saved_jobs where user_id = $1 and job_id = $2`, userID, jobID); err != nil {
		return fmt.Errorf("unsaving job: %w", err)
	}
	return nil
}

// Dismiss hides a vacancy from the user's lists without deleting it globally.
func (r *Repository) Dismiss(ctx context.Context, userID, jobID uuid.UUID) error {
	if _, err := r.db.Pool.Exec(ctx, `
		insert into dismissed_jobs (user_id, job_id) values ($1, $2)
		on conflict (user_id, job_id) do nothing`, userID, jobID); err != nil {
		return fmt.Errorf("dismissing job: %w", err)
	}
	return nil
}

// ListSaved returns the user's bookmarked vacancies with their best source link.
func (r *Repository) ListSaved(ctx context.Context, userID uuid.UUID) ([]SavedJob, error) {
	rows, err := r.db.Pool.Query(ctx, `
		with lm as (
		    select distinct on (job_id) job_id, score from job_matches
		    where user_id = $1 order by job_id, computed_at desc
		)
		select s.job_id, j.title, j.company,
		       coalesce((select l.url from job_source_listings l where l.job_id = j.id order by l.first_seen_at limit 1), ''),
		       coalesce((select l.source_id from job_source_listings l where l.job_id = j.id order by l.first_seen_at limit 1), ''),
		       lm.score, s.saved_at
		from saved_jobs s
		join jobs j on j.id = s.job_id
		left join lm on lm.job_id = j.id
		where s.user_id = $1
		order by s.saved_at desc`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing saved jobs: %w", err)
	}
	defer rows.Close()
	out := []SavedJob{}
	for rows.Next() {
		var x SavedJob
		if err := rows.Scan(&x.JobID, &x.Title, &x.Company, &x.URL, &x.Source, &x.Score, &x.SavedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ListApplications returns the application pipeline, most recent first.
func (r *Repository) ListApplications(ctx context.Context, userID uuid.UUID) ([]Application, error) {
	rows, err := r.db.Pool.Query(ctx, `
		select a.job_id, j.title, j.company,
		       coalesce((select l.url from job_source_listings l where l.job_id = j.id order by l.first_seen_at limit 1), ''),
		       a.status, a.applied_at, a.follow_up_at, a.notes, a.updated_at
		from applications a
		join jobs j on j.id = a.job_id
		where a.user_id = $1
		order by a.updated_at desc`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing applications: %w", err)
	}
	defer rows.Close()
	out := []Application{}
	for rows.Next() {
		var x Application
		if err := rows.Scan(&x.JobID, &x.Title, &x.Company, &x.URL, &x.Status, &x.AppliedAt, &x.FollowUpAt, &x.Notes, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// UpsertApplication records or updates a user's application progress, and keeps
// the saved_jobs bookmark in sync so the vacancy is marked as in-progress in
// every list.
func (r *Repository) UpsertApplication(ctx context.Context, userID, jobID uuid.UUID, status string, appliedAt, followUpAt *time.Time, notes string) (Application, error) {
	if !IsValidStatus(status) {
		return Application{}, fmt.Errorf("%w: %q", ErrInvalidStatus, status)
	}
	var jobExists bool
	if err := r.db.Pool.QueryRow(ctx, `select exists(select 1 from jobs where id = $1)`, jobID).Scan(&jobExists); err != nil {
		return Application{}, fmt.Errorf("checking job: %w", err)
	}
	if !jobExists {
		return Application{}, ErrNotFound
	}

	var out Application
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			insert into applications (user_id, job_id, status, applied_at, follow_up_at, notes)
			values ($1, $2, $3, $4, $5, $6)
			on conflict (user_id, job_id) do update set
			    status = excluded.status,
			    applied_at = coalesce(excluded.applied_at, applications.applied_at),
			    follow_up_at = excluded.follow_up_at,
			    notes = excluded.notes,
			    updated_at = now()
			returning job_id, status, applied_at, follow_up_at, notes, updated_at`,
			userID, jobID, status, appliedAt, followUpAt, notes).
			Scan(&out.JobID, &out.Status, &out.AppliedAt, &out.FollowUpAt, &out.Notes, &out.UpdatedAt); err != nil {
			return fmt.Errorf("upserting application: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			insert into saved_jobs (user_id, job_id) values ($1, $2)
			on conflict (user_id, job_id) do nothing`, userID, jobID); err != nil {
			return fmt.Errorf("bookmarking applied job: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			update jobs set status = $2, updated_at = now()
			where id = $1 and status not in ('expired', 'removed')`, jobID, jobStatusFor(status)); err != nil {
			return fmt.Errorf("updating job status: %w", err)
		}
		return nil
	})
	if err != nil {
		return Application{}, err
	}
	return out, nil
}

// jobStatusFor maps an application status onto the vacancy lifecycle status.
func jobStatusFor(applicationStatus string) string {
	switch applicationStatus {
	case "applied":
		return "applied"
	case "interviewing":
		return "interviewing"
	case "rejected":
		return "rejected"
	default:
		return "saved"
	}
}
