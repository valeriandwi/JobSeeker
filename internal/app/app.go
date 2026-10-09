// Package app assembles the application: configuration, database, repositories,
// adapters, the runner and the HTTP server. Both binaries (api and scheduler)
// build the same object graph from here so they cannot drift apart.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/valeriandwipurnomo/jobseeker/internal/api"
	"github.com/valeriandwipurnomo/jobseeker/internal/auth"
	"github.com/valeriandwipurnomo/jobseeker/internal/candidate"
	"github.com/valeriandwipurnomo/jobseeker/internal/jobs"
	"github.com/valeriandwipurnomo/jobseeker/internal/platform/config"
	"github.com/valeriandwipurnomo/jobseeker/internal/platform/httpx"
	"github.com/valeriandwipurnomo/jobseeker/internal/scheduler"
	"github.com/valeriandwipurnomo/jobseeker/internal/search"
	"github.com/valeriandwipurnomo/jobseeker/internal/sources"
	"github.com/valeriandwipurnomo/jobseeker/internal/storage"
	"github.com/valeriandwipurnomo/jobseeker/internal/tracker"
)

// App holds every long-lived dependency.
type App struct {
	Config     config.Config
	Logger     *slog.Logger
	DB         *storage.DB
	Auth       *auth.Service
	Candidate  *candidate.Repository
	Jobs       *jobs.Repository
	Search     *search.Repository
	Tracker    *tracker.Repository
	Registry   *sources.Registry
	Runner     *search.Runner
	Scheduler  *scheduler.Scheduler
	HTTPServer *api.Server
}

// New builds the application object graph.
func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	db, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	authService := auth.NewService(auth.NewStore(db), auth.SessionConfig{
		CookieName:   cfg.SessionCookieName,
		TTL:          cfg.SessionTTL,
		CookieSecure: cfg.CookieSecure,
	})

	candidateRepo := candidate.NewRepository(db)
	jobsRepo := jobs.NewRepository(db)
	searchRepo := search.NewRepository(db)
	trackerRepo := tracker.NewRepository(db)

	registry := buildRegistry(cfg)

	runner := search.NewRunner(searchRepo, jobsRepo, candidateRepo, registry, logger, search.RunnerConfig{
		PerSourceLimit: 100,
		SourceTimeout:  cfg.SourceHTTPTimeout * 3,
		Policy:         jobs.DefaultExpiryPolicy(),
		HighMatchMin:   70,
		DefaultCron:    cfg.SchedulerDefaultCron,
		DefaultTZ:      cfg.SchedulerDefaultTZ,
	})

	sch := scheduler.NewScheduler(db, searchRepo, runner, auth.NewStore(db), logger, scheduler.SchedulerConfig{
		PollInterval:   cfg.SchedulerPollInterval,
		LockTimeout:    cfg.SchedulerLockTimeout,
		SourceTimeout:  cfg.SourceHTTPTimeout,
		RunLockTimeout: 2 * cfg.SessionTTL / 24,
	})

	server := api.NewServer(api.Deps{
		Config:    cfg,
		Logger:    logger,
		DB:        db,
		Auth:      authService,
		Candidate: candidateRepo,
		Jobs:      jobsRepo,
		Search:    searchRepo,
		Runner:    runner,
		Tracker:   trackerRepo,
	})

	return &App{
		Config: cfg, Logger: logger, DB: db,
		Auth: authService, Candidate: candidateRepo, Jobs: jobsRepo,
		Search: searchRepo, Tracker: trackerRepo,
		Registry: registry, Runner: runner, Scheduler: sch, HTTPServer: server,
	}, nil
}

// buildRegistry constructs the source adapters this build ships, all sharing one
// HTTP fetcher with per-source rate limits.
//
// Only sources with a verified, permissible access path are wired: the keyless
// feeds and the public ATS board APIs. Sources that require an operator key
// (adzuna, usajobs, jooble) have no adapter here and are marked unavailable in
// the catalog rather than pretending to search them.
func buildRegistry(cfg config.Config) *sources.Registry {
	fetcher := sources.NewFetcher(&sources.FetcherConfig{
		Timeout:     cfg.SourceHTTPTimeout,
		MaxAttempts: cfg.SourceMaxAttempts,
		BackoffBase: cfg.SourceBackoffBase,
	})
	// Conservative per-source budgets, well inside each provider's documented
	// tolerance. The fetcher also honours Retry-After on 429/5xx.
	for id, perMinute := range map[string]int{
		"arbeitnow":  30,
		"remotive":   20,
		"remoteok":   20,
		"jobicy":     30,
		"himalayas":  30,
		"greenhouse": 60,
		"lever":      60,
		"ashby":      60,
	} {
		fetcher.SetRateLimit(id, perMinute)
	}

	opt := sources.WithFetcher(fetcher)
	return sources.NewRegistry(
		sources.NewArbeitnow(opt),
		sources.NewRemotive(opt),
		sources.NewRemoteOK(opt),
		sources.NewJobicy(opt),
		sources.NewHimalayas(opt),
		sources.NewGreenhouse(opt),
		sources.NewLever(opt),
		sources.NewAshby(opt),
	)
}

// Close releases resources.
func (a *App) Close() {
	if a.DB != nil {
		a.DB.Close()
	}
}

// Ensure the httpx package's middleware chain is what the server exposes; this
// keeps a compile-time link so the import is never dropped by a refactor.
var _ = httpx.RequestID
