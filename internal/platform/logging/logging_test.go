package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestSensitiveKeysAreDetected(t *testing.T) {
	yes := []string{"password", "Password", "user_password", "api_key", "Authorization", "cookie", "cv_text", "resume_text", "raw_payload", "email", "body"}
	for _, k := range yes {
		if !IsSensitiveKey(k) {
			t.Errorf("IsSensitiveKey(%q) = false, want true", k)
		}
	}
	no := []string{"user_id", "status", "duration_ms", "path", "source_id"}
	for _, k := range no {
		if IsSensitiveKey(k) {
			t.Errorf("IsSensitiveKey(%q) = true, want false", k)
		}
	}
}

func TestRedactionKeepsSecretsOutOfLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := newBuffered(&buf, "debug")

	// Nested groups and typed values must be redacted too.
	logger.Info("login attempt",
		"email", "candidate@example.com",
		"password", "hunter2",
		"cv_text", "FULL CV CONTENTS",
		"api_key", "abcd1234",
		"user_id", "11111111-1111-1111-1111-111111111111",
	)
	logger.With("raw_payload", `{"description":"secret job text"}`).Info("stored")

	out := buf.String()
	for _, secret := range []string{"candidate@example.com", "hunter2", "FULL CV CONTENTS", "abcd1234", "secret job text"} {
		if strings.Contains(out, secret) {
			t.Errorf("log output leaked %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, redacted) {
		t.Errorf("expected redaction marker in output:\n%s", out)
	}
	if !strings.Contains(out, "11111111-1111-1111-1111-111111111111") {
		t.Errorf("non-sensitive values must survive:\n%s", out)
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := newBuffered(&buf, "warn")
	logger.Debug("debug detail")
	logger.Info("info detail")
	logger.Warn("warn detail")
	out := buf.String()
	if strings.Contains(out, "debug detail") || strings.Contains(out, "info detail") {
		t.Errorf("level filtering failed:\n%s", out)
	}
	if !strings.Contains(out, "warn detail") {
		t.Errorf("warn must be emitted:\n%s", out)
	}
}

func TestOutputIsJSONWithEnv(t *testing.T) {
	var buf bytes.Buffer
	logger := newBuffered(&buf, "info").With("env", "test")
	logger.Info("hello", "k", "v")

	var decoded map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &decoded); err != nil {
		t.Fatalf("log line is not JSON: %v (%s)", err, buf.String())
	}
	if decoded["env"] != "test" || decoded["msg"] != "hello" || decoded["k"] != "v" {
		t.Errorf("unexpected fields: %v", decoded)
	}
}

func TestRequestIDContext(t *testing.T) {
	ctx := WithRequestID(context.Background(), "abc")
	if got := RequestID(ctx); got != "abc" {
		t.Errorf("RequestID = %q", got)
	}
	if got := RequestID(context.Background()); got != "" {
		t.Errorf("empty context should yield empty id, got %q", got)
	}
}

func TestFromFallsBack(t *testing.T) {
	fallback := newBuffered(&bytes.Buffer{}, "info")
	if got := From(context.Background(), fallback); got != fallback {
		t.Error("From must return the fallback when no logger is on the context")
	}
	ctx := WithLogger(context.Background(), fallback.With("request_id", "r1"))
	if got := From(ctx, fallback); got == fallback {
		t.Error("From must prefer the context logger")
	}
}

// newBuffered builds a logger with the production redaction config but writing
// to a test buffer.
func newBuffered(buf *bytes.Buffer, level string) *slog.Logger {
	return slog.New(NewHandler(buf, level))
}
