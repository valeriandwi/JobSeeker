// Package scheduler runs due search schedules, guarding each schedule with a
// PostgreSQL advisory lock so multiple processes cannot run it concurrently.
package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log/slog"
	"time"

	"github.com/valeriandwipurnomo/jobseeker/internal/search"
	"github.com/valeriandwipurnomo/jobseeker/internal/storage"
)

// SchedulerConfig parameterises the scheduler loop.
type SchedulerConfig struct {
	PollInterval   time.Duration
	LockTimeout    time.Duration
	SourceTimeout  time.Duration
	RunLockTimeout time.Duration
}

// DefaultSchedulerConfig returns the production defaults.
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		PollInterval:   30 * time.Second,
		LockTimeout:    10 * time.Minute,
		SourceTimeout:  45 * time.Second,
		RunLockTimeout: 2 * time.Hour,
	}
}

// Scheduler runs due schedules, one at a time per schedule, guarded by a
// PostgreSQL advisory lock so that two API/scheduler processes cannot run the
// same schedule concurrently.
type Scheduler struct {
	db     *storage.DB
	repo   *search.Repository
	runner *search.Runner
	auth   interface {
		DeleteExpiredSessions(context.Context) (int64, error)
	}
	logger *slog.Logger
	cfg    SchedulerConfig
	now    func() time.Time
}

// NewScheduler builds the scheduler.
func NewScheduler(db *storage.DB, repo *search.Repository, runner *search.Runner, sessionPruner interface {
	DeleteExpiredSessions(context.Context) (int64, error)
}, logger *slog.Logger, cfg SchedulerConfig) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{db: db, repo: repo, runner: runner, auth: sessionPruner, logger: logger, cfg: cfg, now: time.Now}
}

// Run blocks until ctx is cancelled, ticking on the configured interval.
func (s *Scheduler) Run(ctx context.Context) error {
	s.startupCleanup(ctx)
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	// One pass immediately so a restart does not wait a whole interval.
	if _, err := s.Tick(ctx); err != nil {
		s.logger.Error("scheduler tick failed", "error", err.Error())
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := s.Tick(ctx); err != nil {
				s.logger.Error("scheduler tick failed", "error", err.Error())
			}
		}
	}
}

// startupCleanup closes runs abandoned by a crash and prunes expired sessions.
func (s *Scheduler) startupCleanup(ctx context.Context) {
	if n, err := s.repo.MarkAbandonedRuns(ctx, s.cfg.RunLockTimeout); err != nil {
		s.logger.Error("could not close abandoned runs", "error", err.Error())
	} else if n > 0 {
		s.logger.Warn("closed runs left running by an earlier process", "count", n)
	}
	if s.auth != nil {
		if n, err := s.auth.DeleteExpiredSessions(ctx); err != nil {
			s.logger.Error("could not prune sessions", "error", err.Error())
		} else if n > 0 {
			s.logger.Info("pruned expired sessions", "count", n)
		}
	}
}

// Tick runs every schedule that is due. It returns the number of runs started.
func (s *Scheduler) Tick(ctx context.Context) (int, error) {
	schedules, err := s.repo.ListEnabledSchedules(ctx)
	if err != nil {
		return 0, err
	}
	now := s.now()
	started := 0

	for _, sched := range schedules {
		if ctx.Err() != nil {
			return started, ctx.Err()
		}
		due, next, err := search.IsDue(sched.CronSpec, sched.Timezone, sched.LastRunAt, now)
		if err != nil {
			s.logger.Error("invalid schedule; skipping", "schedule_id", sched.ID.String(), "error", err.Error())
			continue
		}
		if !due {
			continue
		}

		locked, err := s.withAdvisoryLock(ctx, advisoryKey(sched.ID.String()), func() error {
			_, runErr := s.runner.Run(ctx, sched.UserID, "schedule", &sched.ID, "")
			if markErr := s.repo.MarkScheduleRun(ctx, sched.ID, now, next); markErr != nil {
				s.logger.Error("could not record schedule times", "error", markErr.Error())
			}
			return runErr
		})
		if err != nil {
			s.logger.Error("scheduled run failed", "schedule_id", sched.ID.String(), "error", err.Error())
		}
		if locked {
			started++
		} else {
			s.logger.Info("schedule is locked by another process; skipping this tick", "schedule_id", sched.ID.String())
		}
	}
	return started, nil
}

// withAdvisoryLock runs fn while holding a PostgreSQL advisory lock. It returns
// false when another process already holds the lock.
func (s *Scheduler) withAdvisoryLock(ctx context.Context, key int64, fn func() error) (bool, error) {
	conn, err := s.db.Pool.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("acquiring connection for advisory lock: %w", err)
	}
	defer conn.Release()

	var locked bool
	if err := conn.QueryRow(ctx, `select pg_try_advisory_lock($1)`, key).Scan(&locked); err != nil {
		return false, fmt.Errorf("acquiring advisory lock: %w", err)
	}
	if !locked {
		return false, nil
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `select pg_advisory_unlock($1)`, key); err != nil {
			s.logger.Error("releasing advisory lock failed", "error", err.Error())
		}
	}()

	return true, fn()
}

// advisoryKey derives a stable 64-bit key from a schedule id.
func advisoryKey(id string) int64 {
	sum := sha256.Sum256([]byte("jobseeker:schedule:" + id))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}
