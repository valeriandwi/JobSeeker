package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/valeriandwipurnomo/jobseeker/internal/candidate"
	"github.com/valeriandwipurnomo/jobseeker/internal/jobs"
	"github.com/valeriandwipurnomo/jobseeker/internal/matching"
	"github.com/valeriandwipurnomo/jobseeker/internal/sources"
)

// RunnerConfig parameterises a run.
type RunnerConfig struct {
	// PerSourceLimit caps how many listings are pulled from each source.
	PerSourceLimit int
	// SourceTimeout bounds one source's fetch.
	SourceTimeout time.Duration
	// Policy controls vacancy expiry.
	Policy jobs.ExpiryPolicy
	// HighMatchMin is the score threshold used for dashboard counters.
	HighMatchMin int
	// DefaultCron and DefaultTZ seed a user's schedule on first access.
	DefaultCron string
	DefaultTZ   string
}

// DefaultRunnerConfig returns sensible production defaults.
func DefaultRunnerConfig() RunnerConfig {
	return RunnerConfig{
		PerSourceLimit: 100,
		SourceTimeout:  45 * time.Second,
		Policy:         jobs.DefaultExpiryPolicy(),
		HighMatchMin:   70,
		DefaultCron:    "0 8 * * *",
		DefaultTZ:      "Asia/Jakarta",
	}
}

// Runner executes a search across the enabled sources for one user.
//
// Failure isolation is the core requirement: a source that times out or returns
// garbage degrades its own RunItem and nothing else. The run is recorded as
// partial rather than failed when some sources succeeded.
type Runner struct {
	repo     *Repository
	jobs     *jobs.Repository
	profiles *candidate.Repository
	registry *sources.Registry
	logger   *slog.Logger
	cfg      RunnerConfig
	now      func() time.Time
}

// NewRunner builds a runner.
func NewRunner(repo *Repository, jobsRepo *jobs.Repository, profiles *candidate.Repository, registry *sources.Registry, logger *slog.Logger, cfg RunnerConfig) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{repo: repo, jobs: jobsRepo, profiles: profiles, registry: registry, logger: logger, cfg: cfg, now: time.Now}
}

// Run executes one search. idempotencyKey may be empty; when it matches an
// earlier run for the same user, that run is returned instead of starting a new
// one.
func (r *Runner) Run(ctx context.Context, userID uuid.UUID, trigger string, scheduleID *uuid.UUID, idempotencyKey string) (Run, error) {
	run, reused, err := r.repo.StartRun(ctx, userID, scheduleID, trigger, idempotencyKey)
	if err != nil {
		return Run{}, err
	}
	if reused {
		// An earlier request already owns this idempotency key.
		return r.repo.GetRun(ctx, userID, run.ID)
	}

	started := time.Now()
	logger := r.logger.With("run_id", run.ID.String(), "user_id", userID.String(), "trigger", trigger)

	prefs, _, err := r.profiles.GetPreferences(ctx, userID)
	if err != nil {
		r.failRun(ctx, run.ID, logger, "loading preferences", err)
		return Run{}, err
	}
	profile, _, err := r.profiles.GetProfile(ctx, userID)
	if err != nil {
		r.failRun(ctx, run.ID, logger, "loading profile", err)
		return Run{}, err
	}
	profileMatching := profile.ToMatching()
	prefsMatching := prefs.ToMatching()

	query, err := r.buildQuery(ctx, userID, prefs, profile)
	if err != nil {
		r.failRun(ctx, run.ID, logger, "building query", err)
		return Run{}, err
	}

	enabled, err := r.repo.EnabledSourceIDs(ctx, userID)
	if err != nil {
		r.failRun(ctx, run.ID, logger, "loading enabled sources", err)
		return Run{}, err
	}

	stats := map[string]any{
		"sources_attempted": 0,
		"sources_ok":        0,
		"sources_failed":    0,
		"listings_fetched":  0,
		"jobs_created":      0,
		"jobs_updated":      0,
		"jobs_duplicated":   0,
		"jobs_scored":       0,
		"jobs_excluded":     0,
	}
	var errs []string
	touched := map[uuid.UUID]struct{}{}
	attempted := 0

	for _, sourceID := range enabled {
		adapter, ok := r.registry.Get(sourceID)
		if !ok {
			// The catalog may list a source this build has no adapter for; that
			// is reported, never invented.
			if err := r.repo.AddRunItem(ctx, run.ID, RunItem{
				SourceID: sourceID, Status: "skipped",
				Error: "no adapter available in this build",
			}); err != nil {
				logger.Error("recording run item failed", "error", err.Error(), "source_id", sourceID)
			}
			continue
		}
		attempted++
		item, jobIDs, reportErr := r.runSource(ctx, run.ID, adapter, query, logger)
		if reportErr != nil {
			stats["sources_failed"] = stats["sources_failed"].(int) + 1
			errs = append(errs, fmt.Sprintf("%s: %v", sourceID, reportErr))
		} else {
			stats["sources_ok"] = stats["sources_ok"].(int) + 1
		}
		stats["listings_fetched"] = stats["listings_fetched"].(int) + item.Fetched
		stats["jobs_created"] = stats["jobs_created"].(int) + item.NewJobs
		stats["jobs_updated"] = stats["jobs_updated"].(int) + item.UpdatedJobs
		stats["jobs_duplicated"] = stats["jobs_duplicated"].(int) + item.Duplicated
		for _, id := range jobIDs {
			touched[id] = struct{}{}
		}
		if err := r.repo.AddRunItem(ctx, run.ID, item); err != nil {
			logger.Error("recording run item failed", "error", err.Error(), "source_id", sourceID)
		}
	}
	stats["sources_attempted"] = attempted

	// Score every vacancy touched by this run against the confirmed profile.
	scored, excluded := 0, 0
	for id := range touched {
		if ctx.Err() != nil {
			break
		}
		record, err := r.jobs.GetJob(ctx, userID, id)
		if err != nil {
			logger.Warn("scoring skipped: job could not be loaded", "job_id", id.String(), "error", err.Error())
			continue
		}
		result := matching.Score(jobForScoring(record), profileMatching, prefsMatching)
		if err := r.jobs.UpsertMatch(ctx, id, userID, profileMatching.Version, result); err != nil {
			logger.Warn("storing match failed", "job_id", id.String(), "error", err.Error())
			continue
		}
		scored++
		if result.Excluded {
			excluded++
		}
	}
	stats["jobs_scored"] = scored
	stats["jobs_excluded"] = excluded

	// Lifecycle: expire what the policy says is gone. Never on a single miss.
	if expired, err := r.jobs.ExpireVacancies(ctx, r.now(), r.cfg.Policy); err != nil {
		logger.Warn("expiry sweep failed", "error", err.Error())
	} else {
		stats["jobs_expired"] = expired
	}

	status := RunSucceeded
	switch {
	case attempted == 0:
		status = RunFailed
		// Distinguish "nothing is switched on" from "there is nothing to switch
		// on": with an empty catalog the first message is unactionable.
		if settings, err := r.repo.ListSourceSettings(ctx, userID); err == nil && len(settings) == 0 {
			errs = append(errs, "no sources are configured: the source catalog is empty")
		} else {
			errs = append(errs, "no sources were runnable: enable at least one source with a configured adapter")
		}
	case len(errs) > 0:
		status = RunPartial
	}

	stats["duration_ms"] = time.Since(started).Milliseconds()
	summary := strings.Join(errs, "; ")
	if err := r.repo.FinishRun(ctx, run.ID, status, summary, stats); err != nil {
		return Run{}, err
	}
	if scheduleID != nil {
		if err := r.repo.MarkScheduleRun(ctx, *scheduleID, r.now(), r.nextRun(ctx, userID)); err != nil {
			logger.Warn("could not record schedule run times", "error", err.Error())
		}
	}

	logger.Info("search run finished",
		"status", status,
		"sources_ok", stats["sources_ok"],
		"sources_failed", stats["sources_failed"],
		"created", stats["jobs_created"],
		"updated", stats["jobs_updated"],
		"duplicated", stats["jobs_duplicated"],
		"scored", stats["jobs_scored"],
		"duration_ms", stats["duration_ms"])

	return r.repo.GetRun(ctx, userID, run.ID)
}

// runSource executes one adapter and stores its listings.
func (r *Runner) runSource(ctx context.Context, runID uuid.UUID, adapter sources.Adapter, query sources.Query, logger *slog.Logger) (RunItem, []uuid.UUID, error) {
	item := RunItem{SourceID: adapter.ID(), Status: "ok"}
	started := time.Now()

	sourceCtx, cancel := context.WithTimeout(ctx, r.cfg.SourceTimeout)
	defer cancel()

	listings, report, err := adapter.Search(sourceCtx, query)
	item.Fetched = report.Fetched
	item.Rejected = report.Rejected

	if err != nil {
		item.Status = "error"
		item.Error = err.Error()
		item.DurationMs = int(time.Since(started).Milliseconds())
		logger.Warn("source failed", "source_id", adapter.ID(), "error", err.Error())
		return item, nil, err
	}
	if report.Status != "" && report.Status != "ok" {
		item.Status = report.Status
		item.Error = report.Error
	}

	seenSourceJobIDs := make([]string, 0, len(listings))
	var touched []uuid.UUID

	for _, listing := range listings {
		if ctx.Err() != nil {
			break
		}
		if strings.TrimSpace(listing.SourceJobID) != "" {
			seenSourceJobIDs = append(seenSourceJobIDs, listing.SourceJobID)
		}
		verified := listing.AttributesVerified
		res, err := r.jobs.UpsertListing(ctx, jobs.UpsertInput{
			Listing:  listing,
			Verified: verified,
			Now:      r.now(),
		})
		if err != nil {
			item.Status = "partial"
			item.Error = joinErrText(item.Error, err.Error())
			continue
		}
		switch res.Outcome {
		case jobs.OutcomeNew:
			item.NewJobs++
		case jobs.OutcomeUpdated:
			item.UpdatedJobs++
		case jobs.OutcomeDuplicate:
			item.Duplicated++
			item.UpdatedJobs++
		case jobs.OutcomeSkipped:
			item.Rejected++
		}
		if res.JobID != uuid.Nil {
			touched = append(touched, res.JobID)
		}
	}
	item.Accepted = item.NewJobs + item.UpdatedJobs

	// The fetched set is this source's own statement about which listings are
	// still live; listings that did not come back are marked unverified, which
	// only leads to expiry through the grace-window policy.
	if _, _, err := r.jobs.ApplyVerificationUpdates(ctx, adapter.ID(), seenSourceJobIDs, r.now()); err != nil {
		item.Status = "partial"
		item.Error = joinErrText(item.Error, "verification update failed: "+err.Error())
	}

	item.DurationMs = int(time.Since(started).Milliseconds())
	return item, touched, nil
}

func (r *Runner) buildQuery(ctx context.Context, userID uuid.UUID, prefs candidate.Preferences, profile candidate.Profile) (sources.Query, error) {
	watchlist, err := r.repo.ListWatchlist(ctx, userID)
	if err != nil {
		return sources.Query{}, err
	}
	q := sources.Query{
		Titles:          sources.DedupeStrings(append(append([]string{}, prefs.JobTitles...), profile.CurrentTitle)),
		Keywords:        sources.DedupeStrings(prefs.Keywords),
		ExcludeKeywords: sources.DedupeStrings(prefs.ExcludedKeywords),
		IncludeRemote:   len(prefs.WorkArrangements) == 0 || hasArrangement(prefs.WorkArrangements, "remote"),
		Limit:           r.cfg.PerSourceLimit,
		Now:             r.now(),
	}
	for _, loc := range prefs.Locations {
		if q.Country == "" && loc.Country != "" {
			q.Country = loc.Country
		}
		if q.City == "" && loc.City != "" {
			q.City = loc.City
		}
	}
	for _, w := range watchlist {
		q.Watchlist = append(q.Watchlist, sources.Watchlist{SourceID: w.SourceID, BoardSlug: w.BoardSlug, Company: w.Company})
	}
	return q, nil
}

func (r *Runner) nextRun(ctx context.Context, userID uuid.UUID) time.Time {
	schedule, err := r.repo.GetSchedule(ctx, userID, r.cfg.DefaultCron, r.cfg.DefaultTZ)
	if err != nil {
		return time.Time{}
	}
	next, err := NextRun(schedule.CronSpec, schedule.Timezone, r.now())
	if err != nil {
		return time.Time{}
	}
	return next
}

func (r *Runner) failRun(ctx context.Context, runID uuid.UUID, logger *slog.Logger, stage string, err error) {
	logger.Error("search run failed", "stage", stage, "error", err.Error())
	if ferr := r.repo.FinishRun(ctx, runID, RunFailed, stage+": "+err.Error(), map[string]any{}); ferr != nil {
		logger.Error("could not mark run failed", "error", ferr.Error())
	}
}

// jobForScoring maps a stored vacancy onto the matching engine's input.
func jobForScoring(rec jobs.JobRecord) matching.Job {
	keywords := make([]string, 0, len(rec.Sources))
	for _, s := range rec.Sources {
		keywords = append(keywords, s.SourceID)
	}
	return matching.Job{
		ID:                 rec.ID.String(),
		Title:              rec.Title,
		Company:            rec.Company,
		DescriptionSummary: rec.DescriptionSummary,
		Keywords:           keywords,
		City:               rec.City,
		Region:             rec.Region,
		Country:            rec.Country,
		WorkArrangement:    rec.WorkArrangement,
		EmploymentType:     rec.EmploymentType,
		Seniority:          rec.Seniority,
		SalaryMin:          rec.SalaryMin,
		SalaryMax:          rec.SalaryMax,
		SalaryCurrency:     rec.SalaryCurrency,
		SalaryPeriod:       rec.SalaryPeriod,
		AttributesVerified: rec.AttributesVerified,
	}
}

func hasArrangement(prefs []matching.ArrangementPref, want string) bool {
	for _, p := range prefs {
		if strings.EqualFold(p.Value, want) {
			return true
		}
	}
	return false
}

func joinErrText(existing, add string) string {
	if add == "" {
		return existing
	}
	if existing == "" {
		return add
	}
	if strings.Contains(existing, add) {
		return existing
	}
	return existing + "; " + add
}

// Rescore recomputes the stored matches for every job this user can already
// see, against the CURRENT confirmed profile.
//
// A run only scores the jobs it touched, so without this a profile change (a CV
// uploaded and confirmed, a title corrected) leaves older matches scored against
// the previous profile version — which is exactly what "the score must come from
// my CV" needs to avoid. Returns how many matches were written.
func (r *Runner) Rescore(ctx context.Context, userID uuid.UUID) (int, error) {
	prefs, _, err := r.profiles.GetPreferences(ctx, userID)
	if err != nil {
		return 0, err
	}
	profile, _, err := r.profiles.GetProfile(ctx, userID)
	if err != nil {
		return 0, err
	}
	profileMatching := profile.ToMatching()
	prefsMatching := prefs.ToMatching()

	const perPage = 100
	scored := 0
	for page := 1; page <= maxRescorePages; page++ {
		if ctx.Err() != nil {
			break
		}
		records, total, err := r.jobs.ListJobs(ctx, userID, jobs.JobFilter{Page: page, PerPage: perPage})
		if err != nil {
			return scored, err
		}
		if len(records) == 0 {
			break
		}
		for _, record := range records {
			result := matching.Score(jobForScoring(record), profileMatching, prefsMatching)
			if err := r.jobs.UpsertMatch(ctx, record.ID, userID, profileMatching.Version, result); err != nil {
				r.logger.Warn("rescoring job failed", "job_id", record.ID.String(), "error", err.Error())
				continue
			}
			scored++
		}
		if page*perPage >= total {
			break
		}
	}
	return scored, nil
}

// maxRescorePages bounds one request's work: 100 pages x 100 jobs.
const maxRescorePages = 100

// RunNow runs a manual search for the user.
func (r *Runner) RunNow(ctx context.Context, userID uuid.UUID, idempotencyKey string) (Run, error) {
	return r.Run(ctx, userID, "manual", nil, idempotencyKey)
}

// ErrRunInProgress is returned when a second run is requested while one is
// already in flight for the same user.
var ErrRunInProgress = errors.New("search: a run is already in progress for this user")

// Concurrency guard: a user must never have two runs in flight, which would
// double-count listings and fight over the same rows.
var userRunLocks sync.Map

// WithUserLock runs fn while holding an in-process lock for the user.
func WithUserLock(userID uuid.UUID, fn func() error) error {
	mu, _ := userRunLocks.LoadOrStore(userID, &sync.Mutex{})
	m := mu.(*sync.Mutex)
	if !m.TryLock() {
		return ErrRunInProgress
	}
	defer m.Unlock()
	return fn()
}
