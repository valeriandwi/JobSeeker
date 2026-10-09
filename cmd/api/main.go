// Command api serves the JobSeeker REST API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/valeriandwipurnomo/jobseeker/internal/app"
	"github.com/valeriandwipurnomo/jobseeker/internal/platform/config"
	"github.com/valeriandwipurnomo/jobseeker/internal/platform/logging"
)

func main() {
	// Container healthcheck entry point: the runtime image is distroless, so it
	// has no shell or curl, and the binary must probe itself.
	if len(os.Args) > 1 && os.Args[1] == "--healthcheck" {
		os.Exit(healthcheck())
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "error", err.Error())
		os.Exit(1)
	}
	logger := logging.New(cfg.LogLevel, cfg.Env)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, cfg, logger)
	if err != nil {
		logger.Error("startup failed", "error", err.Error())
		os.Exit(1)
	}
	defer application.Close()

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           application.HTTPServer.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("api listening",
			"addr", cfg.HTTPAddr,
			"database", cfg.RedactedDatabaseURL(),
			"sources", application.Registry.Len())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		logger.Error("server failed", "error", err.Error())
		os.Exit(1)
	case <-ctx.Done():
		logger.Info("shutdown signal received; draining")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("api stopped")
}

// healthcheck probes the local /healthz endpoint and returns a process exit code.
func healthcheck() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck: configuration error:", err)
		return 1
	}
	host, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil {
		host, port = "127.0.0.1", "8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/healthz", net.JoinHostPort(host, port)))
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck: request failed:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}
