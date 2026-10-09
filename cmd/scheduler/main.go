// Command scheduler runs the cron-driven search loop.
//
// It runs as a separate process from the API so a long search cannot block
// request handling, and every schedule execution is guarded by a PostgreSQL
// advisory lock, making it safe to run more than one instance.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/valeriandwipurnomo/jobseeker/internal/app"
	"github.com/valeriandwipurnomo/jobseeker/internal/platform/config"
	"github.com/valeriandwipurnomo/jobseeker/internal/platform/logging"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "error", err.Error())
		os.Exit(1)
	}
	logger := logging.New(cfg.LogLevel, cfg.Env)
	slog.SetDefault(logger)

	if !cfg.SchedulerEnabled {
		logger.Info("scheduler disabled by configuration (SCHEDULER_ENABLED=false); exiting")
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, cfg, logger)
	if err != nil {
		logger.Error("startup failed", "error", err.Error())
		os.Exit(1)
	}
	defer application.Close()

	logger.Info("scheduler started",
		"poll_interval", cfg.SchedulerPollInterval.String(),
		"default_cron", cfg.SchedulerDefaultCron,
		"default_timezone", cfg.SchedulerDefaultTZ,
		"sources", application.Registry.Len())

	if err := application.Scheduler.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("scheduler stopped with an error", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("scheduler stopped")
}
