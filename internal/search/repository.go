// Package search orchestrates a search run across the enabled sources and owns
// the per-user search configuration and run history.
package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/valeriandwipurnomo/jobseeker/internal/storage"
)

// ErrNotFound reports a missing run, schedule, source setting or watchlist entry.
var ErrNotFound = errors.New("search: not found")

// Repository is the persistence layer for search configuration and run history.
type Repository struct {
	db *storage.DB
}

// NewRepository wires the repository to the connection pool.
func NewRepository(db *storage.DB) *Repository { return &Repository{db: db} }

// SourceSetting is a source catalog entry combined with this user's enablement
// state and what the operator has configured for it.
type SourceSetting struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name"`
	Kind             string `json:"kind"`
	RequiresKey      bool   `json:"requires_key"`
	KeyConfigured    bool   `json:"key_configured"`
	EnabledGlobally  bool   `json:"enabled_globally"`
	Enabled          bool   `json:"enabled"`
	AttributionText  string `json:"attribution_text"`
	AttributionURL   string `json:"attribution_url"`
	BacklinkRequired bool   `json:"backlink_required"`
	Notes            string `json:"notes"`
}

// ListSourceSettings returns the catalog with the user's enablement applied.
// Attribution obligations travel with the row so the UI cannot show a source
// without also being able to show what using it requires.
func (r *Repository) ListSourceSettings(ctx context.Context, userID uuid.UUID) ([]SourceSetting, error) {
	rows, err := r.db.Pool.Query(ctx, `
		select s.id, s.display_name, s.kind, s.requires_key, s.enabled_globally,
		       coalesce(us.enabled, s.enabled_globally) as enabled,
		       s.attribution_text, s.attribution_url, s.backlink_required, s.notes
		from job_sources s
		left join user_source_settings us on us.source_id = s.id and us.user_id = $1
		order by s.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing source settings: %w", err)
	}
	defer rows.Close()
	out := []SourceSetting{}
	for rows.Next() {
		var s SourceSetting
		if err := rows.Scan(&s.ID, &s.DisplayName, &s.Kind, &s.RequiresKey, &s.EnabledGlobally,
			&s.Enabled, &s.AttributionText, &s.AttributionURL, &s.BacklinkRequired, &s.Notes); err != nil {
			return nil, fmt.Errorf("scanning source setting: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetSourceEnabled toggles one source for a user.
func (r *Repository) SetSourceEnabled(ctx context.Context, userID uuid.UUID, sourceID string, enabled bool) error {
	tag, err := r.db.Pool.Exec(ctx, `
		insert into user_source_settings (user_id, source_id, enabled)
		values ($1, $2, $3)
		on conflict (user_id, source_id) do update set enabled = excluded.enabled`,
		userID, sourceID, enabled)
	if err != nil {
		return fmt.Errorf("updating source setting: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// EnabledSourceIDs returns the source ids this user wants searched.
func (r *Repository) EnabledSourceIDs(ctx context.Context, userID uuid.UUID) ([]string, error) {
	rows, err := r.db.Pool.Query(ctx, `
		select s.id from job_sources s
		left join user_source_settings us on us.source_id = s.id and us.user_id = $1
		where s.enabled_globally and coalesce(us.enabled, true)
		order by s.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("loading enabled sources: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// WatchlistEntry is one ATS company board to search.
type WatchlistEntry struct {
	SourceID  string    `json:"source_id"`
	BoardSlug string    `json:"board_slug"`
	Company   string    `json:"company"`
	CreatedAt time.Time `json:"created_at"`
}

// ListWatchlist returns the user's ATS boards.
func (r *Repository) ListWatchlist(ctx context.Context, userID uuid.UUID) ([]WatchlistEntry, error) {
	rows, err := r.db.Pool.Query(ctx, `
		select source_id, board_slug, company, created_at
		from source_watchlist where user_id = $1 order by source_id, board_slug`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing watchlist: %w", err)
	}
	defer rows.Close()
	out := []WatchlistEntry{}
	for rows.Next() {
		var w WatchlistEntry
		if err := rows.Scan(&w.SourceID, &w.BoardSlug, &w.Company, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// AddWatchlistEntry registers a company board for a user.
func (r *Repository) AddWatchlistEntry(ctx context.Context, userID uuid.UUID, sourceID, boardSlug, company string) (WatchlistEntry, error) {
	var w WatchlistEntry
	err := r.db.Pool.QueryRow(ctx, `
		insert into source_watchlist (user_id, source_id, board_slug, company)
		values ($1, $2, $3, $4)
		on conflict (user_id, source_id, board_slug) do update set company = excluded.company
		returning source_id, board_slug, company, created_at`,
		userID, sourceID, boardSlug, company).Scan(&w.SourceID, &w.BoardSlug, &w.Company, &w.CreatedAt)
	if err != nil {
		return WatchlistEntry{}, fmt.Errorf("adding watchlist entry: %w", err)
	}
	return w, nil
}

// DeleteWatchlistEntry removes a board from a user's watchlist.
func (r *Repository) DeleteWatchlistEntry(ctx context.Context, userID uuid.UUID, sourceID, boardSlug string) error {
	tag, err := r.db.Pool.Exec(ctx, `
		delete from source_watchlist where user_id = $1 and source_id = $2 and board_slug = $3`,
		userID, sourceID, boardSlug)
	if err != nil {
		return fmt.Errorf("deleting watchlist entry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Schedule is a user's cron configuration.
type Schedule struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	CronSpec  string     `json:"cron_spec"`
	Timezone  string     `json:"timezone"`
	Enabled   bool       `json:"enabled"`
	LastRunAt *time.Time `json:"last_run_at"`
	NextRunAt *time.Time `json:"next_run_at"`
}

// GetSchedule loads (creating defaults on first access) a user's schedule.
func (r *Repository) GetSchedule(ctx context.Context, userID uuid.UUID, defaultCron, defaultTZ string) (Schedule, error) {
	var s Schedule
	err := r.db.Pool.QueryRow(ctx, `
		insert into search_schedules (user_id, cron_spec, timezone)
		values ($1, $2, $3)
		on conflict (user_id) do update set user_id = excluded.user_id
		returning id, user_id, cron_spec, timezone, enabled, last_run_at, next_run_at`,
		userID, defaultCron, defaultTZ).
		Scan(&s.ID, &s.UserID, &s.CronSpec, &s.Timezone, &s.Enabled, &s.LastRunAt, &s.NextRunAt)
	if err != nil {
		return Schedule{}, fmt.Errorf("loading schedule: %w", err)
	}
	return s, nil
}

// UpdateSchedule persists a schedule change.
func (r *Repository) UpdateSchedule(ctx context.Context, userID uuid.UUID, cronSpec, tz string, enabled bool) (Schedule, error) {
	var s Schedule
	err := r.db.Pool.QueryRow(ctx, `
		update search_schedules
		set cron_spec = $2, timezone = $3, enabled = $4, updated_at = now()
		where user_id = $1
		returning id, user_id, cron_spec, timezone, enabled, last_run_at, next_run_at`,
		userID, cronSpec, tz, enabled).
		Scan(&s.ID, &s.UserID, &s.CronSpec, &s.Timezone, &s.Enabled, &s.LastRunAt, &s.NextRunAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return Schedule{}, ErrNotFound
		}
		return Schedule{}, fmt.Errorf("updating schedule: %w", err)
	}
	return s, nil
}

// ListEnabledSchedules returns every enabled schedule (used by the scheduler).
func (r *Repository) ListEnabledSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := r.db.Pool.Query(ctx, `
		select id, user_id, cron_spec, timezone, enabled, last_run_at, next_run_at
		from search_schedules where enabled order by id`)
	if err != nil {
		return nil, fmt.Errorf("listing schedules: %w", err)
	}
	defer rows.Close()
	var out []Schedule
	for rows.Next() {
		var s Schedule
		if err := rows.Scan(&s.ID, &s.UserID, &s.CronSpec, &s.Timezone, &s.Enabled, &s.LastRunAt, &s.NextRunAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkScheduleRun records the last and next run times.
func (r *Repository) MarkScheduleRun(ctx context.Context, scheduleID uuid.UUID, lastRun, nextRun time.Time) error {
	_, err := r.db.Pool.Exec(ctx, `
		update search_schedules set last_run_at = $2, next_run_at = $3, updated_at = now() where id = $1`,
		scheduleID, lastRun, nextRun)
	if err != nil {
		return fmt.Errorf("marking schedule run: %w", err)
	}
	return nil
}

// Run statuses.
const (
	RunRunning   = "running"
	RunSucceeded = "succeeded"
	RunPartial   = "partial"
	RunFailed    = "failed"
	RunAborted   = "aborted"
)

// Run is one search execution.
type Run struct {
	ID           uuid.UUID      `json:"id"`
	UserID       uuid.UUID      `json:"user_id"`
	ScheduleID   *uuid.UUID     `json:"schedule_id,omitempty"`
	Trigger      string         `json:"trigger"`
	Status       string         `json:"status"`
	StartedAt    time.Time      `json:"started_at"`
	FinishedAt   *time.Time     `json:"finished_at"`
	ErrorSummary string         `json:"error_summary"`
	Stats        map[string]any `json:"stats"`
	Items        []RunItem      `json:"items,omitempty"`
}

// RunItem is the per-source result of a run.
type RunItem struct {
	SourceID    string `json:"source_id"`
	Status      string `json:"status"`
	Fetched     int    `json:"fetched"`
	Accepted    int    `json:"accepted"`
	Rejected    int    `json:"rejected"`
	Duplicated  int    `json:"duplicated"`
	NewJobs     int    `json:"new_jobs"`
	UpdatedJobs int    `json:"updated_jobs"`
	DurationMs  int    `json:"duration_ms"`
	Error       string `json:"error"`
}

// StartRun creates a run row, reusing an existing one when the idempotency key
// matches (so a double-clicked "Run now" does not start two searches).
func (r *Repository) StartRun(ctx context.Context, userID uuid.UUID, scheduleID *uuid.UUID, trigger, idempotencyKey string) (Run, bool, error) {
	if idempotencyKey != "" {
		var existing Run
		err := r.db.Pool.QueryRow(ctx, `
			select id, user_id, schedule_id, trigger, status, started_at, finished_at, error_summary, stats
			from search_runs where user_id = $1 and idempotency_key = $2`,
			userID, idempotencyKey).
			Scan(&existing.ID, &existing.UserID, &existing.ScheduleID, &existing.Trigger, &existing.Status,
				&existing.StartedAt, &existing.FinishedAt, &existing.ErrorSummary, &existing.Stats)
		if err == nil {
			return existing, true, nil
		}
	}

	var run Run
	err := r.db.Pool.QueryRow(ctx, `
		insert into search_runs (user_id, schedule_id, trigger, status, idempotency_key)
		values ($1, $2, $3, 'running', nullif($4, ''))
		returning id, user_id, schedule_id, trigger, status, started_at, finished_at, error_summary, stats`,
		userID, scheduleID, trigger, idempotencyKey).
		Scan(&run.ID, &run.UserID, &run.ScheduleID, &run.Trigger, &run.Status, &run.StartedAt, &run.FinishedAt, &run.ErrorSummary, &run.Stats)
	if err != nil {
		return Run{}, false, fmt.Errorf("starting run: %w", err)
	}
	return run, false, nil
}

// FinishRun closes a run with its final status, stats and error summary.
func (r *Repository) FinishRun(ctx context.Context, runID uuid.UUID, status, errorSummary string, stats map[string]any) error {
	encoded, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("encoding run stats: %w", err)
	}
	_, err = r.db.Pool.Exec(ctx, `
		update search_runs
		set status = $2, finished_at = now(), error_summary = $3, stats = $4
		where id = $1`, runID, status, errorSummary, encoded)
	if err != nil {
		return fmt.Errorf("finishing run: %w", err)
	}
	return nil
}

// AddRunItem records one source's contribution to a run.
func (r *Repository) AddRunItem(ctx context.Context, runID uuid.UUID, item RunItem) error {
	_, err := r.db.Pool.Exec(ctx, `
		insert into search_run_items
		    (run_id, source_id, status, fetched, accepted, rejected, duplicated, new_jobs, updated_jobs, duration_ms, error, finished_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())`,
		runID, item.SourceID, item.Status, item.Fetched, item.Accepted, item.Rejected,
		item.Duplicated, item.NewJobs, item.UpdatedJobs, item.DurationMs, item.Error)
	if err != nil {
		return fmt.Errorf("adding run item: %w", err)
	}
	return nil
}

// GetRun loads one run with its per-source items.
func (r *Repository) GetRun(ctx context.Context, userID, runID uuid.UUID) (Run, error) {
	var run Run
	err := r.db.Pool.QueryRow(ctx, `
		select id, user_id, schedule_id, trigger, status, started_at, finished_at, error_summary, stats
		from search_runs where id = $1 and user_id = $2`, runID, userID).
		Scan(&run.ID, &run.UserID, &run.ScheduleID, &run.Trigger, &run.Status, &run.StartedAt, &run.FinishedAt, &run.ErrorSummary, &run.Stats)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("loading run: %w", err)
	}
	items, err := r.runItems(ctx, runID)
	if err != nil {
		return Run{}, err
	}
	run.Items = items
	return run, nil
}

// ListRuns returns a page of run history, newest first.
func (r *Repository) ListRuns(ctx context.Context, userID uuid.UUID, page, perPage int) ([]Run, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage <= 0 || perPage > 100 {
		perPage = 20
	}
	var total int
	if err := r.db.Pool.QueryRow(ctx, `select count(*) from search_runs where user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting runs: %w", err)
	}
	rows, err := r.db.Pool.Query(ctx, `
		select id, user_id, schedule_id, trigger, status, started_at, finished_at, error_summary, stats
		from search_runs where user_id = $1
		order by started_at desc
		limit $2 offset $3`, userID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("listing runs: %w", err)
	}
	defer rows.Close()

	out := []Run{}
	for rows.Next() {
		var run Run
		if err := rows.Scan(&run.ID, &run.UserID, &run.ScheduleID, &run.Trigger, &run.Status,
			&run.StartedAt, &run.FinishedAt, &run.ErrorSummary, &run.Stats); err != nil {
			return nil, 0, err
		}
		items, err := r.runItems(ctx, run.ID)
		if err != nil {
			return nil, 0, err
		}
		run.Items = items
		out = append(out, run)
	}
	return out, total, rows.Err()
}

// LastRunStartedAt returns the start of the most recent run, used to count
// "new since last search".
func (r *Repository) LastRunStartedAt(ctx context.Context, userID uuid.UUID) (*time.Time, error) {
	var t *time.Time
	err := r.db.Pool.QueryRow(ctx, `
		select started_at from search_runs where user_id = $1 order by started_at desc limit 1`, userID).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading last run: %w", err)
	}
	return t, nil
}

func (r *Repository) runItems(ctx context.Context, runID uuid.UUID) ([]RunItem, error) {
	rows, err := r.db.Pool.Query(ctx, `
		select source_id, status, fetched, accepted, rejected, duplicated, new_jobs, updated_jobs, duration_ms, error
		from search_run_items where run_id = $1 order by source_id`, runID)
	if err != nil {
		return nil, fmt.Errorf("loading run items: %w", err)
	}
	defer rows.Close()
	out := []RunItem{}
	for rows.Next() {
		var it RunItem
		if err := rows.Scan(&it.SourceID, &it.Status, &it.Fetched, &it.Accepted, &it.Rejected,
			&it.Duplicated, &it.NewJobs, &it.UpdatedJobs, &it.DurationMs, &it.Error); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// MarkAbandonedRuns closes runs that were still marked running when the process
// restarted, so a crash cannot leave a run "running" forever.
func (r *Repository) MarkAbandonedRuns(ctx context.Context, olderThan time.Duration) (int, error) {
	tag, err := r.db.Pool.Exec(ctx, `
		update search_runs
		set status = 'aborted',
		    finished_at = now(),
		    error_summary = case when error_summary = '' then 'process restarted while this run was in progress' else error_summary end
		where status = 'running' and started_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(olderThan.Seconds())))
	if err != nil {
		return 0, fmt.Errorf("aborting stale runs: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
