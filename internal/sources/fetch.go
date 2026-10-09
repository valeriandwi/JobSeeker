package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

// DefaultUserAgent identifies this client to job sources. It is intentionally
// explicit so operators of the upstream services can contact us.
const DefaultUserAgent = "JobSeeker/0.1 (+personal job discovery; contact via configured operator email)"

// maxResponseBytes caps how much of a source response we will read. Sources are
// third parties; a hostile or broken one must not exhaust memory.
const maxResponseBytes = 16 << 20 // 16 MiB

// ErrUpstreamStatus reports a non-2xx response from a source.
type ErrUpstreamStatus struct {
	URL        string
	StatusCode int
	Body       string
}

func (e *ErrUpstreamStatus) Error() string {
	return fmt.Sprintf("upstream %s returned %d: %s", e.URL, e.StatusCode, truncate(e.Body, 200))
}

// Fetcher performs HTTP requests against job sources with timeouts, bounded
// retries with exponential backoff+jitter, and a per-source rate limit.
type Fetcher struct {
	Client      *http.Client
	MaxAttempts int
	BackoffBase time.Duration
	UserAgent   string
	Logger      *slog.Logger

	// limiter throttles requests per source id.
	limiters map[string]*rate.Limiter
	// perMinute is the configured per-source budget (0 = unlimited).
	perMinute map[string]int
}

// FetcherConfig configures a Fetcher.
type FetcherConfig struct {
	Timeout     time.Duration
	MaxAttempts int
	BackoffBase time.Duration
	UserAgent   string
}

// NewFetcher builds a Fetcher. Passing nil cfg uses package defaults.
func NewFetcher(cfg *FetcherConfig) *Fetcher {
	c := FetcherConfig{
		Timeout:     20 * time.Second,
		MaxAttempts: 3,
		BackoffBase: 500 * time.Millisecond,
		UserAgent:   DefaultUserAgent,
	}
	if cfg != nil {
		if cfg.Timeout > 0 {
			c.Timeout = cfg.Timeout
		}
		if cfg.MaxAttempts > 0 {
			c.MaxAttempts = cfg.MaxAttempts
		}
		if cfg.BackoffBase > 0 {
			c.BackoffBase = cfg.BackoffBase
		}
		if cfg.UserAgent != "" {
			c.UserAgent = cfg.UserAgent
		}
	}
	return &Fetcher{
		Client:      &http.Client{Timeout: c.Timeout},
		MaxAttempts: c.MaxAttempts,
		BackoffBase: c.BackoffBase,
		UserAgent:   c.UserAgent,
		Logger:      slog.Default(),
		limiters:    map[string]*rate.Limiter{},
		perMinute:   map[string]int{},
	}
}

// SetRateLimit configures a per-minute request budget for a source.
func (f *Fetcher) SetRateLimit(sourceID string, perMinute int) {
	if perMinute <= 0 {
		return
	}
	f.perMinute[sourceID] = perMinute
	f.limiters[sourceID] = rate.NewLimiter(rate.Limit(float64(perMinute)/60.0), 1)
}

// GetJSON performs a GET and decodes the JSON body into dst, retrying on
// transient failures (network errors, 429 and 5xx).
func (f *Fetcher) GetJSON(ctx context.Context, sourceID, url string, dst any) error {
	body, _, err := f.GetBytes(ctx, sourceID, url)
	if err != nil {
		return err
	}
	if dst == nil {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decoding JSON from %s: %w", url, err)
	}
	return nil
}

// GetBytes performs a GET with retries and returns the body.
func (f *Fetcher) GetBytes(ctx context.Context, sourceID, url string) ([]byte, int, error) {
	return f.do(ctx, sourceID, http.MethodGet, url, nil, "", nil)
}

// PostJSON performs a POST with a JSON body and returns the raw response.
func (f *Fetcher) PostJSON(ctx context.Context, sourceID, url string, payload any, extraHeaders map[string]string) ([]byte, int, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("encoding request payload: %w", err)
	}
	return f.do(ctx, sourceID, http.MethodPost, url, encoded, "application/json", extraHeaders)
}

func (f *Fetcher) do(ctx context.Context, sourceID, method, url string, body []byte, contentType string, extraHeaders map[string]string) ([]byte, int, error) {
	if err := f.wait(ctx, sourceID); err != nil {
		return nil, 0, err
	}

	attempts := f.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			if err := sleepCtx(ctx, f.backoff(attempt)); err != nil {
				return nil, 0, err
			}
			if err := f.wait(ctx, sourceID); err != nil {
				return nil, 0, err
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bytesReader(body))
		if err != nil {
			return nil, 0, fmt.Errorf("building request: %w", err)
		}
		req.Header.Set("Accept", "application/json, text/plain;q=0.8, */*;q=0.5")
		req.Header.Set("User-Agent", f.UserAgent)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}

		resp, err := f.Client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request to %s failed: %w", url, err)
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			continue
		}

		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("reading response from %s: %w", url, readErr)
			continue
		}

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return data, resp.StatusCode, nil
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = &ErrUpstreamStatus{URL: url, StatusCode: resp.StatusCode, Body: string(data)}
			// Honour Retry-After when the source provides it.
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, convErr := strconv.Atoi(ra); convErr == nil && secs > 0 && secs <= 60 {
					if err := sleepCtx(ctx, time.Duration(secs)*time.Second); err != nil {
						return nil, 0, err
					}
				}
			}
			continue
		default:
			// 4xx other than 429 are permanent for this request: no retry.
			return nil, resp.StatusCode, &ErrUpstreamStatus{URL: url, StatusCode: resp.StatusCode, Body: string(data)}
		}
	}
	return nil, 0, lastErr
}

func (f *Fetcher) wait(ctx context.Context, sourceID string) error {
	lim, ok := f.limiters[sourceID]
	if !ok {
		return nil
	}
	return lim.Wait(ctx)
}

// backoff returns an exponentially growing delay with ±25% jitter.
func (f *Fetcher) backoff(attempt int) time.Duration {
	base := f.BackoffBase
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	d := base * time.Duration(1<<(attempt-2))
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	jitter := 1 + (rand.Float64()-0.5)/2
	return time.Duration(float64(d) * jitter)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func bytesReader(b []byte) io.Reader {
	if len(b) == 0 {
		return nil
	}
	return newBytesReader(b)
}

// IsPermanent reports whether an error should not be retried.
func IsPermanent(err error) bool {
	var status *ErrUpstreamStatus
	if errors.As(err, &status) {
		return status.StatusCode >= 400 && status.StatusCode < 500 && status.StatusCode != http.StatusTooManyRequests
	}
	return false
}
