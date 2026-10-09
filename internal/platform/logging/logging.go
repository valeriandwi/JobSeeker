// Package logging provides a structured JSON logger built on log/slog with
// mandatory redaction of sensitive attribute names.
//
// CV contents, descriptions, credentials, tokens and cookies must never reach
// the log stream: LogAttrs drops or masks any attribute whose key looks
// sensitive, regardless of what the caller passes.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// sensitive are substrings that, when found in an attribute key, force
// redaction. Matching is case-insensitive.
var sensitive = []string{
	"password", "passwd", "secret", "token", "authorization", "cookie",
	"api_key", "apikey", "access_key", "session_id", "credit", "cvc",
	"cv_text", "resume_text", "description", "raw_payload", "payload",
	"email", "file_content", "body",
}

const redacted = "[REDACTED]"

// New returns a slog.Logger writing JSON to stderr with redaction applied.
func New(level string, env string) *slog.Logger {
	l := slog.New(NewHandler(os.Stderr, level))
	if env != "" {
		l = l.With("env", env)
	}
	return l
}

// NewHandler builds the redacting JSON handler over an arbitrary writer. It is
// exported so tests (and alternative sinks) can capture log output.
func NewHandler(w io.Writer, level string) slog.Handler {
	return slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       parseLevel(level),
		ReplaceAttr: redact,
	})
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// redact is a slog ReplaceAttr hook applied to every attribute, including
// group members (`groups` is the list of enclosing group names).
func redact(groups []string, a slog.Attr) slog.Attr {
	key := strings.ToLower(a.Key)
	for _, s := range sensitive {
		if strings.Contains(key, s) {
			return slog.String(a.Key, redacted)
		}
	}
	return a
}

// IsSensitiveKey reports whether a key would be redacted. Exported for tests
// and for callers that want to pre-check before logging.
func IsSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, s := range sensitive {
		if strings.Contains(key, s) {
			return true
		}
	}
	return false
}

type ctxKey struct{}

// WithRequestID stores a request id on the context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// RequestID returns the request id stored on the context, if any.
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}

// From returns the logger stored on the context, or the provided fallback.
func From(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return fallback
}

type loggerKey struct{}

// WithLogger stores a logger on the context.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}
