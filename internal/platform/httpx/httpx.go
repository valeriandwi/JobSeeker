// Package httpx contains the HTTP plumbing shared by all handlers: the single
// error envelope, strict JSON decoding, request-scoped middleware (request id,
// panic recovery, access logging, CORS, rate limiting) and small helpers.
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/valeriandwipurnomo/jobseeker/internal/platform/logging"
)

// Error codes used in the error envelope. Clients may switch on these.
const (
	CodeValidation   = "validation_error"
	CodeUnauthorized = "unauthorized"
	CodeForbidden    = "forbidden"
	CodeNotFound     = "not_found"
	CodeConflict     = "conflict"
	CodeTooLarge     = "payload_too_large"
	CodeUnsupported  = "unsupported_media_type"
	CodeRateLimited  = "rate_limited"
	CodeInternal     = "internal_error"
	CodeUnavailable  = "service_unavailable"
	CodeBadRequest   = "bad_request"
)

// maxJSONBody bounds request bodies decoded as JSON.
const maxJSONBody = 1 << 20 // 1 MiB

// ErrorResponse is the only error shape the API ever returns.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody carries a stable code, a human message and optional field details.
type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Details   any    `json:"details,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// FieldError describes a single validation problem.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError is returned by handlers for 400 responses with field detail.
type ValidationError struct {
	Message string
	Details []FieldError
}

func (e *ValidationError) Error() string { return e.Message }

// NewValidationError builds a validation error from field problems.
func NewValidationError(message string, details ...FieldError) *ValidationError {
	return &ValidationError{Message: message, Details: details}
}

// WriteJSON marshals v with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The response is already partially written; there is nothing left to do
		// but record it. Callers must not attempt a second write.
		slog.Default().Error("encoding response failed", "error", err.Error())
	}
}

// WriteError writes the standard error envelope. Internal errors are logged
// with full detail but never leaked to the client.
func WriteError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	status, code, message, details := classify(err)
	if status >= 500 {
		logger.ErrorContext(r.Context(), "request failed",
			"status", status,
			"code", code,
			"error", err.Error(),
			"path", r.URL.Path,
		)
	}
	WriteJSON(w, status, ErrorResponse{Error: ErrorBody{
		Code:      code,
		Message:   message,
		Details:   details,
		RequestID: logging.RequestID(r.Context()),
	}})
}

func classify(err error) (status int, code, message string, details any) {
	var ve *ValidationError
	switch {
	case err == nil:
		return http.StatusInternalServerError, CodeInternal, "internal error", nil
	case errors.As(err, &ve):
		return http.StatusBadRequest, CodeValidation, ve.Message, ve.Details
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, CodeNotFound, "resource not found", nil
	case errors.Is(err, ErrConflict):
		return http.StatusConflict, CodeConflict, "resource already exists", nil
	case errors.Is(err, ErrUnauthorized):
		return http.StatusUnauthorized, CodeUnauthorized, "authentication required", nil
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden, CodeForbidden, "not permitted", nil
	case errors.Is(err, ErrTooLarge):
		return http.StatusRequestEntityTooLarge, CodeTooLarge, "request body too large", nil
	case errors.Is(err, ErrUnsupportedMedia):
		return http.StatusUnsupportedMediaType, CodeUnsupported, "unsupported media type", nil
	case errors.Is(err, ErrBadRequest):
		return http.StatusBadRequest, CodeBadRequest, "malformed request", nil
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusServiceUnavailable, CodeUnavailable, "upstream timeout", nil
	default:
		return http.StatusInternalServerError, CodeInternal, "internal error", nil
	}
}

// Sentinel errors mapped to HTTP statuses by WriteError.
var (
	ErrNotFound         = errors.New("not found")
	ErrConflict         = errors.New("conflict")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrForbidden        = errors.New("forbidden")
	ErrTooLarge         = errors.New("payload too large")
	ErrUnsupportedMedia = errors.New("unsupported media type")
	ErrBadRequest       = errors.New("bad request")
)

// DecodeJSON strictly decodes a JSON request body into dst: it enforces a size
// limit, rejects unknown fields and rejects trailing content.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		return fmt.Errorf("%w: expected application/json, got %q", ErrUnsupportedMedia, ct)
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return fmt.Errorf("%w: body exceeds %d bytes", ErrTooLarge, maxErr.Limit)
		}
		return &ValidationError{Message: "invalid JSON body: " + err.Error()}
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return &ValidationError{Message: "invalid JSON body: unexpected trailing content"}
	}
	return nil
}

// NewRequestID returns a random request identifier.
func NewRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// Middleware is a standard HTTP middleware.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares so that the first listed runs outermost.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// RequestID injects a request id and echoes it back to the client.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-Id")
			if id == "" {
				id = NewRequestID()
			}
			w.Header().Set("X-Request-Id", id)
			ctx := logging.WithRequestID(r.Context(), id)
			ctx = logging.WithLogger(ctx, logging.From(ctx, slog.Default()).With("request_id", id))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Recoverer converts panics into 500 responses and logs them.
func Recoverer(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorContext(r.Context(), "panic recovered",
						"panic", fmt.Sprint(rec), "path", r.URL.Path)
					WriteError(w, r, logger, errors.New("panic"))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder captures the response status for access logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// AccessLog logs one line per request with method, path, status and duration.
func AccessLog(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			logging.From(r.Context(), logger).Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", clientIP(r),
			)
		})
	}
}

// CORS allows the configured frontend origins to call the API with credentials.
func CORS(allowed []string) Middleware {
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		set[o] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				if _, ok := set[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Vary", "Origin")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-Id, Idempotency-Key")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				}
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimiter is a per-key token bucket with idle eviction.
type RateLimiter struct {
	mu      sync.Mutex
	limit   rate.Limit
	burst   int
	buckets map[string]*bucket
	now     func() time.Time
	lastGC  time.Time
}

type bucket struct {
	lim  *rate.Limiter
	seen time.Time
}

// NewRateLimiter builds a limiter allowing perMinute requests per key.
func NewRateLimiter(perMinute int) *RateLimiter {
	burst := perMinute
	if burst < 1 {
		burst = 1
	}
	return &RateLimiter{
		limit:   rate.Limit(float64(perMinute) / 60.0),
		burst:   burst,
		buckets: make(map[string]*bucket),
		now:     time.Now,
	}
}

// Allow reports whether the key may proceed.
func (rl *RateLimiter) Allow(key string) bool {
	if rl == nil {
		return true
	}
	now := rl.now()
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if now.Sub(rl.lastGC) > 5*time.Minute {
		for k, b := range rl.buckets {
			if now.Sub(b.seen) > 10*time.Minute {
				delete(rl.buckets, k)
			}
		}
		rl.lastGC = now
	}
	b, ok := rl.buckets[key]
	if !ok {
		b = &bucket{lim: rate.NewLimiter(rl.limit, rl.burst)}
		rl.buckets[key] = b
	}
	b.seen = now
	return b.lim.Allow()
}

// RateLimitKeyFunc derives the bucket key for a request.
type RateLimitKeyFunc func(r *http.Request) string

// ByIP keys the limiter on the client IP.
func ByIP(r *http.Request) string { return clientIP(r) }

// RateLimit rejects requests over the configured budget with 429.
func RateLimit(rl *RateLimiter, keyFn RateLimitKeyFunc, logger *slog.Logger) Middleware {
	if keyFn == nil {
		keyFn = ByIP
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rl != nil && !rl.Allow(keyFn(r)) {
				w.Header().Set("Retry-After", "60")
				logger.WarnContext(r.Context(), "rate limited", "path", r.URL.Path, "remote", clientIP(r))
				WriteJSON(w, http.StatusTooManyRequests, ErrorResponse{Error: ErrorBody{
					Code:      CodeRateLimited,
					Message:   "too many requests",
					RequestID: logging.RequestID(r.Context()),
				}})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP returns the best-effort client address (does not trust XFF, since
// this service sits behind its own proxy only when explicitly configured).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
