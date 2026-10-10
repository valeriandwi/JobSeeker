// Package api wires the HTTP surface: routing, middleware and handlers.
//
// Every route is either explicitly public (health, auth) or behind the session
// middleware, and every user-owned query is scoped by the authenticated user id
// taken from the session — never from the request body or query string.
package api

import (
	"log/slog"
	"net/http"

	"github.com/valeriandwipurnomo/jobseeker/internal/auth"
	"github.com/valeriandwipurnomo/jobseeker/internal/candidate"
	"github.com/valeriandwipurnomo/jobseeker/internal/jobs"
	"github.com/valeriandwipurnomo/jobseeker/internal/platform/config"
	"github.com/valeriandwipurnomo/jobseeker/internal/platform/httpx"
	"github.com/valeriandwipurnomo/jobseeker/internal/search"
	"github.com/valeriandwipurnomo/jobseeker/internal/sources"
	"github.com/valeriandwipurnomo/jobseeker/internal/storage"
	"github.com/valeriandwipurnomo/jobseeker/internal/tracker"
)

// Deps are everything the HTTP layer needs.
type Deps struct {
	Config    config.Config
	Logger    *slog.Logger
	DB        *storage.DB
	Auth      *auth.Service
	Candidate *candidate.Repository
	Jobs      *jobs.Repository
	Search    *search.Repository
	Runner    *search.Runner
	Tracker   *tracker.Repository
	// Registry is the source adapter set. Handlers consult it for capability
	// questions (e.g. which sources accept a company watchlist) so adding an
	// adapter never requires editing the HTTP layer.
	Registry *sources.Registry
}

// Server is the HTTP handler set.
type Server struct {
	deps Deps
	auth *auth.Handlers
}

// NewServer builds the API server.
func NewServer(deps Deps) *Server {
	return &Server{deps: deps, auth: auth.NewHandlers(deps.Auth, deps.Logger)}
}

// Routes builds the full route table with middleware applied.
func (s *Server) Routes() http.Handler {
	logger := s.deps.Logger
	mux := http.NewServeMux()

	// Public endpoints.
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("POST /api/v1/auth/register", s.rateLimited(s.deps.Config.RateLimitAuthPerMin, nil, s.auth.Register))
	mux.HandleFunc("POST /api/v1/auth/login", s.rateLimited(s.deps.Config.RateLimitAuthPerMin, nil, s.auth.Login))
	mux.HandleFunc("POST /api/v1/auth/logout", s.auth.Logout)

	// Authenticated endpoints.
	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/v1/me", s.auth.Me)

	protected.HandleFunc("GET /api/v1/profile", s.handleGetProfile)
	protected.HandleFunc("PUT /api/v1/profile", s.handlePutProfile)

	protected.HandleFunc("GET /api/v1/resumes", s.handleListResumes)
	protected.HandleFunc("POST /api/v1/resumes", s.handleUploadResume)
	protected.HandleFunc("DELETE /api/v1/resumes/{id}", s.handleDeleteResume)
	protected.HandleFunc("GET /api/v1/resumes/{id}/draft", s.handleGetDraft)

	protected.HandleFunc("GET /api/v1/preferences", s.handleGetPreferences)
	protected.HandleFunc("PUT /api/v1/preferences", s.handlePutPreferences)

	protected.HandleFunc("GET /api/v1/schedule", s.handleGetSchedule)
	protected.HandleFunc("PUT /api/v1/schedule", s.handlePutSchedule)

	protected.HandleFunc("GET /api/v1/sources", s.handleListSources)
	protected.HandleFunc("POST /api/v1/jobs/rescore", s.handleRescoreJobs)
	protected.HandleFunc("PATCH /api/v1/sources/{id}", s.handlePatchSource)
	protected.HandleFunc("GET /api/v1/sources/{id}/watchlist", s.handleListWatchlist)
	protected.HandleFunc("POST /api/v1/sources/{id}/watchlist", s.handleAddWatchlist)
	protected.HandleFunc("DELETE /api/v1/sources/{id}/watchlist/{slug}", s.handleDeleteWatchlist)

	protected.HandleFunc("POST /api/v1/searches/run", s.handleRunSearch)
	protected.HandleFunc("GET /api/v1/searches/runs", s.handleListRuns)
	protected.HandleFunc("GET /api/v1/searches/runs/{id}", s.handleGetRun)

	protected.HandleFunc("GET /api/v1/jobs", s.handleListJobs)
	protected.HandleFunc("GET /api/v1/jobs/{id}", s.handleGetJob)
	protected.HandleFunc("POST /api/v1/jobs/{id}/save", s.handleSaveJob)
	protected.HandleFunc("DELETE /api/v1/jobs/{id}/save", s.handleUnsaveJob)
	protected.HandleFunc("POST /api/v1/jobs/{id}/dismiss", s.handleDismissJob)
	protected.HandleFunc("GET /api/v1/saved-jobs", s.handleListSaved)

	protected.HandleFunc("GET /api/v1/applications", s.handleListApplications)
	protected.HandleFunc("PUT /api/v1/applications/{jobID}", s.handlePutApplication)

	protected.HandleFunc("GET /api/v1/dashboard/summary", s.handleDashboardSummary)

	mux.Handle("/api/v1/", s.deps.Auth.Require(logger)(protected))

	return httpx.Chain(mux,
		httpx.RequestID(),
		httpx.Recoverer(logger),
		httpx.CORS(s.deps.Config.AllowedOrigins),
		httpx.AccessLog(logger),
	)
}

// rateLimited applies a per-IP limiter to a handler.
func (s *Server) rateLimited(perMinute int, keyFn httpx.RateLimitKeyFunc, next http.HandlerFunc) http.HandlerFunc {
	limiter := httpx.NewRateLimiter(perMinute)
	wrapped := httpx.RateLimit(limiter, keyFn, s.deps.Logger)(next)
	return func(w http.ResponseWriter, r *http.Request) { wrapped.ServeHTTP(w, r) }
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.DB.Ping(r.Context()); err != nil {
		httpx.WriteError(w, r, s.deps.Logger, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
