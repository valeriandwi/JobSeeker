package sources

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixture reads a captured real payload from testdata.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// serveFixture starts a test server that always replies with the named fixture.
func serveFixture(t *testing.T, name string) *httptest.Server {
	t.Helper()
	body := fixture(t, name)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// serveFixtureFailing behaves like serveFixture but returns 404 for any request
// whose URL path contains failSubstr. It lets a single test exercise the
// "one bad board must not fail the others" contract.
func serveFixtureFailing(t *testing.T, name, failSubstr string) *httptest.Server {
	t.Helper()
	body := fixture(t, name)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, failSubstr) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// serveBody starts a test server that always replies with an inline payload.
// Used only for edge cases no captured fixture exhibits.
func serveBody(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad test time %q: %v", s, err)
	}
	return tm
}

// indexByJobID maps a result set by SourceJobID for direct assertion.
func indexByJobID(list []RawListing) map[string]RawListing {
	m := make(map[string]RawListing, len(list))
	for _, l := range list {
		m[l.SourceJobID] = l
	}
	return m
}

func assertPtr(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: want %v, got nil", name, want)
	}
	if *got != want {
		t.Fatalf("%s: want %v, got %v", name, want, *got)
	}
}

func assertStatus(t *testing.T, rep Report, sourceID, status string) {
	t.Helper()
	if rep.SourceID != sourceID {
		t.Fatalf("report source id: want %q, got %q", sourceID, rep.SourceID)
	}
	if rep.Status != status {
		t.Fatalf("report status: want %q, got %q (err=%q)", status, rep.Status, rep.Error)
	}
}
