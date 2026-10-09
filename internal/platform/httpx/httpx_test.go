package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valeriandwipurnomo/jobseeker/internal/platform/logging"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)) }

func TestWriteErrorStatusMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"validation", NewValidationError("bad"), http.StatusBadRequest, CodeValidation},
		{"not found", ErrNotFound, http.StatusNotFound, CodeNotFound},
		{"conflict", ErrConflict, http.StatusConflict, CodeConflict},
		{"unauthorized", ErrUnauthorized, http.StatusUnauthorized, CodeUnauthorized},
		{"forbidden", ErrForbidden, http.StatusForbidden, CodeForbidden},
		{"too large", ErrTooLarge, http.StatusRequestEntityTooLarge, CodeTooLarge},
		{"unsupported", ErrUnsupportedMedia, http.StatusUnsupportedMediaType, CodeUnsupported},
		{"unknown", errors.New("kaboom: secret dsn"), http.StatusInternalServerError, CodeInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			WriteError(rec, req, discardLogger(), tc.err)

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			var body ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding envelope: %v", err)
			}
			if body.Error.Code != tc.code {
				t.Errorf("code = %q, want %q", body.Error.Code, tc.code)
			}
			if body.Error.Message == "" {
				t.Error("message must not be empty")
			}
			// Internal error messages must never leak the underlying error text.
			if tc.status == http.StatusInternalServerError && strings.Contains(body.Error.Message, "kaboom") {
				t.Error("internal error detail leaked to the client")
			}
		})
	}
}

func TestWriteErrorIncludesRequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(logging.WithRequestID(req.Context(), "req-123"))
	WriteError(rec, req, discardLogger(), ErrNotFound)

	var body ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.RequestID != "req-123" {
		t.Errorf("request_id = %q, want req-123", body.Error.RequestID)
	}
}

func TestValidationErrorDetailsSurvive(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	err := NewValidationError("invalid input", FieldError{Field: "email", Message: "required"})
	WriteError(rec, req, discardLogger(), err)

	if !strings.Contains(rec.Body.String(), `"field":"email"`) {
		t.Fatalf("field details missing from body: %s", rec.Body.String())
	}
}

func TestDecodeJSONStrictness(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	t.Run("valid", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ok"}`))
		req.Header.Set("Content-Type", "application/json")
		var p payload
		if err := DecodeJSON(rec, req, &p); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name != "ok" {
			t.Fatalf("decoded %q", p.Name)
		}
	})

	t.Run("unknown field rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ok","admin":true}`))
		req.Header.Set("Content-Type", "application/json")
		var p payload
		if err := DecodeJSON(rec, req, &p); err == nil {
			t.Fatal("unknown fields must be rejected")
		}
	})

	t.Run("trailing content rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ok"}{"name":"two"}`))
		req.Header.Set("Content-Type", "application/json")
		var p payload
		if err := DecodeJSON(rec, req, &p); err == nil {
			t.Fatal("trailing JSON must be rejected")
		}
	})

	t.Run("wrong content type rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ok"}`))
		req.Header.Set("Content-Type", "text/plain")
		var p payload
		err := DecodeJSON(rec, req, &p)
		if !errors.Is(err, ErrUnsupportedMedia) {
			t.Fatalf("err = %v, want ErrUnsupportedMedia", err)
		}
	})

	t.Run("oversized body rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		big := `{"name":"` + strings.Repeat("x", maxJSONBody+16) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(big))
		req.Header.Set("Content-Type", "application/json")
		var p payload
		err := DecodeJSON(rec, req, &p)
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
	})
}

func TestRequestIDMiddlewarePropagates(t *testing.T) {
	var seen string
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = logging.RequestID(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", "from-client")
	h.ServeHTTP(rec, req)

	if seen != "from-client" {
		t.Errorf("context request id = %q", seen)
	}
	if got := rec.Header().Get("X-Request-Id"); got != "from-client" {
		t.Errorf("response header = %q", got)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec2, req2)
	if got := rec2.Header().Get("X-Request-Id"); len(got) != 32 {
		t.Errorf("generated request id looks wrong: %q", got)
	}
}

func TestRecovererTurnsPanicInto500(t *testing.T) {
	h := Recoverer(discardLogger())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), CodeInternal) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestRateLimiterBlocksAfterBurst(t *testing.T) {
	rl := NewRateLimiter(2)
	if !rl.Allow("a") || !rl.Allow("a") {
		t.Fatal("burst of 2 must be allowed")
	}
	if rl.Allow("a") {
		t.Fatal("third immediate request must be limited")
	}
	if !rl.Allow("b") {
		t.Fatal("limits must be per key")
	}
}

func TestRateLimitMiddlewareReturns429(t *testing.T) {
	rl := NewRateLimiter(1)
	h := RateLimit(rl, ByIP, discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first request = %d", first.Code)
	}

	second := httptest.NewRecorder()
	h.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request = %d, want 429", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Error("429 must include Retry-After")
	}
}

func TestCORSAllowsConfiguredOriginOnly(t *testing.T) {
	h := CORS([]string{"http://localhost:5173"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	allowed := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	h.ServeHTTP(allowed, req)
	if got := allowed.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("allowed origin header = %q", got)
	}
	if got := allowed.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("credentials header = %q", got)
	}

	denied := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("Origin", "http://evil.example")
	h.ServeHTTP(denied, req2)
	if got := denied.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("unconfigured origin must not be allowed, got %q", got)
	}
}

func TestContextCancellationIsNotSwallowed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	WriteError(rec, req, discardLogger(), context.Canceled)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("canceled context should map to 500, got %d", rec.Code)
	}
}
