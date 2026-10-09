package config

import (
	"strings"
	"testing"
)

func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5433/db?sslmode=disable")
}

func TestLoadDefaults(t *testing.T) {
	setBaseEnv(t)
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.HTTPAddr != "127.0.0.1:8080" {
		t.Errorf("HTTPAddr = %q", c.HTTPAddr)
	}
	if c.SchedulerDefaultCron != "0 8 * * *" {
		t.Errorf("default cron = %q", c.SchedulerDefaultCron)
	}
	if c.SchedulerDefaultTZ != "Asia/Jakarta" {
		t.Errorf("default timezone = %q", c.SchedulerDefaultTZ)
	}
	if c.MaxUploadBytes != 10<<20 {
		t.Errorf("MaxUploadBytes = %d", c.MaxUploadBytes)
	}
	if c.SessionCookieName != "js_session" {
		t.Errorf("SessionCookieName = %q", c.SessionCookieName)
	}
	if c.RateLimitAuthPerMin != 10 {
		t.Errorf("RateLimitAuthPerMin = %d", c.RateLimitAuthPerMin)
	}
	if c.IsProduction() {
		t.Error("development env must not report production")
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("DATABASE_URL must be required")
	} else if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("error should name the missing key: %v", err)
	}
}

func TestLoadRejectsUnknownTimezone(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("DEFAULT_SCHEDULE_TIMEZONE", "Mars/Olympus_Mons")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "timezone") {
		t.Fatalf("expected timezone validation error, got %v", err)
	}
}

func TestLoadRequiresSecureCookieInProduction(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("COOKIE_SECURE", "false")
	if _, err := Load(); err == nil {
		t.Fatal("production without COOKIE_SECURE must fail validation")
	}
	t.Setenv("COOKIE_SECURE", "true")
	if _, err := Load(); err != nil {
		t.Fatalf("production with COOKIE_SECURE=true must load: %v", err)
	}
}

func TestDurationAndListParsing(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("SCHEDULER_POLL_INTERVAL", "45s")
	t.Setenv("SOURCE_HTTP_TIMEOUT", "12s")
	t.Setenv("SESSION_TTL_HOURS", "24")
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://a.test, http://b.test ,")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.SchedulerPollInterval.Seconds() != 45 {
		t.Errorf("poll interval = %v", c.SchedulerPollInterval)
	}
	if c.SourceHTTPTimeout.Seconds() != 12 {
		t.Errorf("source timeout = %v", c.SourceHTTPTimeout)
	}
	if c.SessionTTL.Hours() != 24 {
		t.Errorf("session ttl = %v", c.SessionTTL)
	}
	if len(c.AllowedOrigins) != 2 {
		t.Errorf("origins = %v", c.AllowedOrigins)
	}
}

func TestRedactURL(t *testing.T) {
	cases := map[string]string{
		"postgres://user:supersecret@host:5432/db": "postgres://user:***@host:5432/db",
		"postgres://user@host/db":                  "postgres://user@host/db",
		"":                                         "",
		"not-a-url":                                "not-a-url",
	}
	for in, want := range cases {
		if got := RedactURL(in); got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", in, got, want)
		}
	}
	cfg := Config{DatabaseURL: "postgres://u:p@h/db"}
	if strings.Contains(cfg.RedactedDatabaseURL(), ":p@") {
		t.Error("RedactedDatabaseURL leaked the password")
	}
}
