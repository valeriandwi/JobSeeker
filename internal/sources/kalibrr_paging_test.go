package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// pagedKalibrrStub answers every page with a FULL page of public listings and a
// large total count, so the adapter only stops when it reaches the requested
// limit. It records the offset of each request.
func pagedKalibrrStub(t *testing.T, total int) (*httptest.Server, *[]int) {
	t.Helper()
	offsets := &[]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		*offsets = append(*offsets, offset)

		const pageSize = 100
		jobs := make([]map[string]any, 0, pageSize)
		for i := range pageSize {
			id := offset + i + 1
			jobs = append(jobs, map[string]any{
				"id":           id,
				"name":         fmt.Sprintf("Software Engineer %d", id),
				"slug":         fmt.Sprintf("software-engineer-%d", id),
				"visibility":   "public",
				"company":      map[string]any{"code": "acme", "name": "Acme"},
				"company_name": "Acme",
			})
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"count": total, "jobs": jobs}); err != nil {
			t.Errorf("encode stub response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, offsets
}

// The per-run limit is only real if the adapter walks enough pages to reach it:
// 1000 listings at 100 per page needs 10 pages. The paging bound used to stop at
// 5 pages (500 listings) no matter what limit the runner asked for, so a raised
// per-run limit silently delivered half of it.
func TestKalibrrFetchesTheFullPerRunLimit(t *testing.T) {
	srv, offsets := pagedKalibrrStub(t, 5000)

	k := NewKalibrr()
	k.baseURL = srv.URL

	got, rep, err := k.Search(context.Background(), Query{Limit: 1000})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "kalibrr", "ok")

	if len(*offsets) != 10 {
		t.Fatalf("pages requested: %d (offsets %v), want 10", len(*offsets), *offsets)
	}
	if (*offsets)[9] != 900 {
		t.Errorf("last offset: %d, want 900", (*offsets)[9])
	}
	if rep.Fetched != 1000 {
		t.Errorf("fetched: %d, want 1000", rep.Fetched)
	}
	if len(got) != 1000 {
		t.Errorf("listings kept: %d, want 1000", len(got))
	}
}

// A limit smaller than one page must stay one request: the page bound is a
// ceiling, not a target.
func TestKalibrrDoesNotOverFetchASmallLimit(t *testing.T) {
	srv, offsets := pagedKalibrrStub(t, 5000)

	k := NewKalibrr()
	k.baseURL = srv.URL

	_, rep, err := k.Search(context.Background(), Query{Limit: 100})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "kalibrr", "ok")

	if len(*offsets) != 1 || (*offsets)[0] != 0 {
		t.Errorf("offsets %v, want exactly one request at offset 0", *offsets)
	}
	if rep.Fetched != 100 {
		t.Errorf("fetched: %d, want 100", rep.Fetched)
	}
}
