// Package config loads and validates runtime configuration from environment
// variables (optionally seeded from a local .env file).
//
// Secrets are never logged: Config.String and the logging package both redact
// the database URL credentials.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config is the fully resolved application configuration.
type Config struct {
	Env      string
	HTTPAddr string
	LogLevel string
	DataDir  string

	DatabaseURL string

	SessionCookieName string
	SessionTTL        time.Duration
	CookieSecure      bool

	MaxUploadBytes int64

	RateLimitPublicPerMin int
	RateLimitAuthPerMin   int
	RateLimitWritePerMin  int

	AllowedOrigins []string

	SchedulerEnabled      bool
	SchedulerDefaultCron  string
	SchedulerDefaultTZ    string
	SchedulerPollInterval time.Duration
	SchedulerLockTimeout  time.Duration

	SourceHTTPTimeout time.Duration
	SourceMaxAttempts int
	SourceBackoffBase time.Duration

	AdzunaAppID   string
	AdzunaAppKey  string
	USAJobsAPIKey string
	USAJobsEmail  string
	JoobleAPIKey  string
}

// Load reads .env (if present) and then the process environment, applying
// defaults and validating required values.
func Load() (Config, error) {
	// A missing .env is expected in production; ignore the error but not others.
	if err := godotenv.Load(".env"); err != nil && !os.IsNotExist(err) {
		var pathErr *os.PathError
		if !asPathErr(err, &pathErr) {
			return Config{}, fmt.Errorf("loading .env: %w", err)
		}
	}

	c := Config{
		Env:      getenv("APP_ENV", "development"),
		HTTPAddr: getenv("HTTP_ADDR", "127.0.0.1:8080"),
		LogLevel: getenv("LOG_LEVEL", "info"),
		DataDir:  getenv("DATA_DIR", "./data"),

		DatabaseURL: getenv("DATABASE_URL", ""),

		SessionCookieName: getenv("SESSION_COOKIE_NAME", "js_session"),
		SessionTTL:        getdur("SESSION_TTL_HOURS", 720) * time.Hour,
		CookieSecure:      getbool("COOKIE_SECURE", false),

		MaxUploadBytes: getint64("MAX_UPLOAD_BYTES", 10<<20),

		RateLimitPublicPerMin: getint("RATE_LIMIT_PUBLIC_PER_MIN", 120),
		RateLimitAuthPerMin:   getint("RATE_LIMIT_AUTH_PER_MIN", 10),
		RateLimitWritePerMin:  getint("RATE_LIMIT_WRITE_PER_MIN", 30),

		AllowedOrigins: splitList(getenv("CORS_ALLOWED_ORIGINS", "http://localhost:5173")),

		SchedulerEnabled:      getbool("SCHEDULER_ENABLED", true),
		SchedulerDefaultCron:  getenv("DEFAULT_SCHEDULE_CRON", "0 8 * * *"),
		SchedulerDefaultTZ:    getenv("DEFAULT_SCHEDULE_TIMEZONE", "Asia/Jakarta"),
		SchedulerPollInterval: getduration("SCHEDULER_POLL_INTERVAL", 30*time.Second),
		SchedulerLockTimeout:  getduration("SCHEDULER_ADVISORY_LOCK_TIMEOUT", 5*time.Minute),

		SourceHTTPTimeout: getduration("SOURCE_HTTP_TIMEOUT", 20*time.Second),
		SourceMaxAttempts: getint("SOURCE_MAX_ATTEMPTS", 3),
		SourceBackoffBase: getduration("SOURCE_BACKOFF_BASE", 500*time.Millisecond),

		AdzunaAppID:   getenv("ADZUNA_APP_ID", ""),
		AdzunaAppKey:  getenv("ADZUNA_APP_KEY", ""),
		USAJobsAPIKey: getenv("USAJOBS_API_KEY", ""),
		USAJobsEmail:  getenv("USAJOBS_USER_AGENT_EMAIL", ""),
		JoobleAPIKey:  getenv("JOOBLE_API_KEY", ""),
	}

	if err := c.validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (c Config) validate() error {
	var problems []string
	if strings.TrimSpace(c.DatabaseURL) == "" {
		problems = append(problems, "DATABASE_URL is required")
	}
	if c.SessionTTL <= 0 {
		problems = append(problems, "SESSION_TTL_HOURS must be positive")
	}
	if c.MaxUploadBytes <= 0 {
		problems = append(problems, "MAX_UPLOAD_BYTES must be positive")
	}
	if _, err := time.LoadLocation(c.SchedulerDefaultTZ); err != nil {
		problems = append(problems, fmt.Sprintf("DEFAULT_SCHEDULE_TIMEZONE %q is not a known IANA timezone", c.SchedulerDefaultTZ))
	}
	if c.Env == "production" && !c.CookieSecure {
		problems = append(problems, "COOKIE_SECURE must be true in production")
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid configuration: %s", strings.Join(problems, "; "))
	}
	return nil
}

// IsProduction reports whether the app runs in production mode.
func (c Config) IsProduction() bool { return c.Env == "production" }

// RedactedDatabaseURL returns the database URL with any password removed.
func (c Config) RedactedDatabaseURL() string { return RedactURL(c.DatabaseURL) }

// RedactURL strips the password from a URL-like connection string.
func RedactURL(raw string) string {
	if raw == "" {
		return ""
	}
	at := strings.LastIndex(raw, "@")
	scheme := strings.Index(raw, "//")
	if at < 0 || scheme < 0 || at < scheme {
		return raw
	}
	userinfo := raw[scheme+2 : at]
	if colon := strings.Index(userinfo, ":"); colon >= 0 {
		userinfo = userinfo[:colon] + ":***"
	}
	return raw[:scheme+2] + userinfo + raw[at:]
}

func asPathErr(err error, target **os.PathError) bool {
	if pe, ok := err.(*os.PathError); ok {
		*target = pe
		return true
	}
	return false
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func getint(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func getint64(key string, def int64) int64 {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return n
		}
	}
	return def
}

func getbool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}

// getdur reads an integer number of hours (used for *_HOURS keys).
func getdur(key string, defHours int) time.Duration {
	return time.Duration(getint(key, defHours))
}

// getduration reads a Go duration string such as "30s" or "5m".
func getduration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return def
}

func splitList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
