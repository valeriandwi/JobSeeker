package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// jobStreetStub records every request (path + query) and answers with a body
// built by respond. It lets a test assert which markets were called, what was
// asked, and how many times.
func jobStreetStub(t *testing.T, respond func(r *http.Request) any) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	seen := &[]*http.Request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Clone(context.Background()))
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(respond(r)); err != nil {
			t.Errorf("encode stub response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

func jobStreetPage(offset, count int) map[string]any {
	jobs := make([]map[string]any, 0, count)
	for i := range count {
		id := offset + i + 1
		jobs = append(jobs, map[string]any{
			"id":          strconv.Itoa(id),
			"title":       fmt.Sprintf("Software Engineer %d", id),
			"companyName": "Acme",
			"teaser":      "Build things.",
			"listingDate": "2026-10-09T03:42:47Z",
		})
	}
	return map[string]any{"data": jobs, "totalCount": 5000}
}

func TestJobStreetMapping(t *testing.T) {
	srv := serveFixture(t, "jobstreet.json")
	js := NewJobStreet()
	js.hostOverride = srv.URL

	got, rep, err := js.Search(context.Background(), Query{Countries: []string{"ID"}, Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "jobstreet", "ok")
	if rep.Fetched != 3 || rep.Accepted != 3 {
		t.Fatalf("counters: fetched=%d accepted=%d", rep.Fetched, rep.Accepted)
	}

	by := indexByJobID(got)

	// Salary, arrangement, employment type, classification tags and the split
	// "City, Region" location all come from real fields.
	se, ok := by["95163313"]
	if !ok {
		t.Fatalf("missing listing 95163313")
	}
	if se.Title != "Software Engineer" {
		t.Errorf("title: %q", se.Title)
	}
	if se.Company != "PT Strange Tech Cikarang Indonesia" {
		t.Errorf("company: %q", se.Company)
	}
	if se.City != "South Cikarang" || se.Region != "West Java" || se.Country != "ID" {
		t.Errorf("location: city=%q region=%q country=%q", se.City, se.Region, se.Country)
	}
	// "Rp 6.500.000 – Rp 8.000.000 per month"
	assertPtr(t, "salary_min", se.SalaryMin, 6500000)
	assertPtr(t, "salary_max", se.SalaryMax, 8000000)
	if se.SalaryCurrency != "IDR" || se.SalaryPeriod != "month" {
		t.Errorf("salary currency/period: %q/%q", se.SalaryCurrency, se.SalaryPeriod)
	}
	if !se.Has(AttrSalary) {
		t.Error("salary should be verified: the label states amounts")
	}
	if se.WorkArrangement != "onsite" || !se.Has(AttrWorkArrangement) {
		t.Errorf("arrangement: %q verified=%v", se.WorkArrangement, se.Has(AttrWorkArrangement))
	}
	if se.EmploymentType != "contract" || !se.Has(AttrEmploymentType) {
		t.Errorf("employment type: %q verified=%v", se.EmploymentType, se.Has(AttrEmploymentType))
	}
	if !strings.Contains(strings.Join(se.Tags, "|"), "Developers/Programmers") {
		t.Errorf("tags should carry the SEEK classification, got %v", se.Tags)
	}
	if se.PublishedAt == nil {
		t.Error("published_at should parse from listingDate")
	}

	// The payload publishes neither seniority nor an expiry, so both must stay
	// unknown AND unverified — never inferred.
	if se.Seniority != "" || se.Has(AttrSeniority) {
		t.Errorf("seniority must stay unknown, got %q verified=%v", se.Seniority, se.Has(AttrSeniority))
	}
	if se.ExpiresAt != nil || se.Has(AttrExpiresAt) {
		t.Errorf("expires_at must stay unset, got %v verified=%v", se.ExpiresAt, se.Has(AttrExpiresAt))
	}

	// The listing URL is inferred from the board's own path shape, not fetched:
	// job pages answer a Cloudflare challenge, so there is no URL attribute to
	// verify and the string must not claim otherwise.
	if se.URL != "https://id.jobstreet.com/job/95163313" {
		t.Errorf("url: %q", se.URL)
	}
	if se.Has("url") || se.Has("apply_url") {
		t.Error("the listing URL is inferred; it must not be advertised as verified")
	}
}

func TestJobStreetSalaryLabelParsing(t *testing.T) {
	cases := []struct {
		label    string
		min, max *float64
		currency string
		period   string
	}{
		{label: "Rp 6.500.000 – Rp 8.000.000 per month", min: f(6500000), max: f(8000000), currency: "IDR", period: "month"},
		{label: "From Rp 5.000.000 per month", min: f(5000000), currency: "IDR", period: "month"},
		{label: "Up to SGD 9,000 per month", max: f(9000), currency: "SGD", period: "month"},
		{label: "RM 4,500 – RM 6,000 per month", min: f(4500), max: f(6000), currency: "MYR", period: "month"},
		{label: "SGD 90,000 per annum", min: f(90000), currency: "SGD", period: "year"},
		// No figure at all: nothing is stored and nothing is verified.
		{label: "Competitive salary"},
		{label: ""},
	}

	for _, c := range cases {
		min, max, currency, period := parseJobStreetSalary(c.label)
		if !equalPtr(min, c.min) || !equalPtr(max, c.max) {
			t.Errorf("%q: amounts %v-%v, want %v-%v", c.label, deref(min), deref(max), deref(c.min), deref(c.max))
		}
		if currency != c.currency || period != c.period {
			t.Errorf("%q: currency=%q period=%q, want %q/%q", c.label, currency, period, c.currency, c.period)
		}
	}
}

// A market SEEK does not serve must be refused, and refused without touching the
// network: the API ignores unknown siteKeys, so an unserved request would file
// foreign listings under the requested country.
func TestJobStreetRefusesUnservedMarketsWithoutRequesting(t *testing.T) {
	srv, seen := jobStreetStub(t, func(*http.Request) any { return jobStreetPage(0, 0) })
	js := NewJobStreet()
	js.hostOverride = srv.URL

	got, rep, err := js.Search(context.Background(), Query{Countries: []string{"US", "GB"}, Limit: 50})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("listings: %d, want 0", len(got))
	}
	if rep.Status != "skipped" {
		t.Fatalf("status: %q (err=%q)", rep.Status, rep.Error)
	}
	for _, want := range []string{"ID", "MY", "SG", "US", "GB"} {
		if !strings.Contains(rep.Error, want) {
			t.Errorf("refusal should mention %s: %q", want, rep.Error)
		}
	}
	if len(*seen) != 0 {
		t.Errorf("no HTTP request may be made for an unserved market, saw %d", len(*seen))
	}
}

// One configured location per market: Jakarta + Singapore + Malaysia must each
// be searched, not just the first.
func TestJobStreetSearchesEveryConfiguredMarket(t *testing.T) {
	srv, seen := jobStreetStub(t, func(r *http.Request) any {
		return jobStreetPage(0, 10)
	})
	js := NewJobStreet()
	js.hostOverride = srv.URL

	_, rep, err := js.Search(context.Background(), Query{
		Country:   "ID",
		City:      "Jakarta",
		Countries: []string{"ID", "MY", "SG"},
		Limit:     300,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "jobstreet", "ok")

	keys := map[string]bool{}
	var withWhere, withoutWhere int
	for _, r := range *seen {
		keys[r.URL.Query().Get("siteKey")] = true
		if r.URL.Query().Get("where") == "Jakarta" {
			withWhere++
		} else {
			withoutWhere++
		}
		// The per-run budget is split across markets: 300 / 3 = 100 per market.
		if got := r.URL.Query().Get("pageSize"); got != "100" {
			t.Errorf("pageSize for %s: %q, want 100 (budget split across markets)", r.URL.Query().Get("siteKey"), got)
		}
	}
	for _, want := range []string{"ID-Main", "MY-Main", "SG-Main"} {
		if !keys[want] {
			t.Errorf("market %s was never searched (saw %v)", want, keys)
		}
	}
	// Jakarta belongs to the Indonesian market: `where` must not be sent to MY/SG.
	if withWhere != 1 || withoutWhere != 2 {
		t.Errorf("where=Jakarta sent %d time(s), expected exactly 1 of 3 requests", withWhere)
	}
}

func TestJobStreetFetchesTheFullPerRunLimit(t *testing.T) {
	srv, seen := jobStreetStub(t, func(r *http.Request) any {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		size, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
		return jobStreetPage((page-1)*size, size)
	})
	js := NewJobStreet()
	js.hostOverride = srv.URL

	got, rep, err := js.Search(context.Background(), Query{Countries: []string{"ID"}, Limit: 1000})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "jobstreet", "ok")

	if len(*seen) != 10 {
		t.Fatalf("pages requested: %d, want 10 (1000 listings at 100 per page)", len(*seen))
	}
	if rep.Fetched != 1000 {
		t.Errorf("fetched: %d, want 1000", rep.Fetched)
	}
	if len(got) != 1000 {
		t.Errorf("listings kept: %d, want 1000", len(got))
	}
}

func TestJobStreetDoesNotOverFetchASmallLimit(t *testing.T) {
	srv, seen := jobStreetStub(t, func(r *http.Request) any {
		size, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
		return jobStreetPage(0, size)
	})
	js := NewJobStreet()
	js.hostOverride = srv.URL

	_, rep, err := js.Search(context.Background(), Query{Countries: []string{"ID"}, Limit: 40})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "jobstreet", "ok")
	if len(*seen) != 1 {
		t.Errorf("requests: %d, want exactly 1 for a limit below one page", len(*seen))
	}
	if rep.Fetched != 40 {
		t.Errorf("fetched: %d, want 40", rep.Fetched)
	}
}

// No location configured at all should still search the home market rather than
// silently returning nothing.
func TestJobStreetDefaultsToHomeMarketWithoutALocation(t *testing.T) {
	srv, seen := jobStreetStub(t, func(r *http.Request) any { return jobStreetPage(0, 5) })
	js := NewJobStreet()
	js.hostOverride = srv.URL

	_, rep, err := js.Search(context.Background(), Query{Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "jobstreet", "ok")
	if len(*seen) != 1 {
		t.Fatalf("requests: %d, want 1", len(*seen))
	}
	if key := (*seen)[0].URL.Query().Get("siteKey"); key != "ID-Main" {
		t.Errorf("siteKey: %q, want ID-Main", key)
	}
}

func f(v float64) *float64 { return &v }

func equalPtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func deref(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}
