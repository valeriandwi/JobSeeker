package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKalibrrMapping(t *testing.T) {
	srv := serveFixture(t, "kalibrr.json")
	k := NewKalibrr()
	k.baseURL = srv.URL

	got, rep, err := k.Search(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "kalibrr", "ok")
	if rep.Fetched != 3 || rep.Accepted != 3 {
		t.Fatalf("counters: fetched=%d accepted=%d", rep.Fetched, rep.Accepted)
	}

	by := indexByJobID(got)

	se, ok := by["140474"]
	if !ok {
		t.Fatalf("missing listing 140474")
	}
	if se.Title != "Software Engineer" {
		t.Errorf("title: %q", se.Title)
	}
	if se.Company != "PT. Prima Vista Solusi" {
		t.Errorf("company: %q", se.Company)
	}
	// Public URL is the board's own path shape (verified 200 live).
	if se.URL != "https://www.kalibrr.com/c/prima-vista-solusi/jobs/140474/software-engineer-3" {
		t.Errorf("url: %q", se.URL)
	}
	if se.City != "South Jakarta City" || se.Region != "Jakarta" || se.Country != "ID" {
		t.Errorf("location: city=%q region=%q country=%q", se.City, se.Region, se.Country)
	}
	if se.EmploymentType != "permanent" || !se.Has(AttrEmploymentType) {
		t.Errorf("employment type: %q verified=%v", se.EmploymentType, se.Has(AttrEmploymentType))
	}
	// Amounts are published even when salary_shown is false.
	assertPtr(t, "salary_max", se.SalaryMax, 6500000)
	if se.SalaryCurrency != "IDR" || se.SalaryPeriod != "month" {
		t.Errorf("salary currency/period: %q/%q", se.SalaryCurrency, se.SalaryPeriod)
	}
	if !se.Has(AttrSalary) {
		t.Error("salary should be verified when amounts are present")
	}
	if se.PublishedAt == nil || se.ExpiresAt == nil {
		t.Error("published_at and expires_at should both parse")
	}
	// Neither remote nor hybrid is claimed, and the place name says nothing
	// about arrangement.
	if se.WorkArrangement != "" || se.Has(AttrWorkArrangement) {
		t.Errorf("arrangement should be unverified, got %q", se.WorkArrangement)
	}
	// work_experience is an undocumented numeric code: seniority must NOT be
	// inferred from it.
	if se.Seniority != "" || se.Has(AttrSeniority) {
		t.Errorf("seniority must stay unknown, got %q verified=%v", se.Seniority, se.Has(AttrSeniority))
	}

	contractual, ok := by["273608"]
	if !ok {
		t.Fatalf("missing listing 273608")
	}
	if contractual.EmploymentType != "contract" {
		t.Errorf("Contractual should map to contract, got %q", contractual.EmploymentType)
	}
	// No amounts published: salary must stay unverified rather than zero.
	if contractual.Has(AttrSalary) || contractual.SalaryMin != nil || contractual.SalaryMax != nil {
		t.Errorf("salary must be absent, got min=%v max=%v verified=%v",
			contractual.SalaryMin, contractual.SalaryMax, contractual.Has(AttrSalary))
	}
}

// The country parameter is honoured only for Indonesia and Philippines; any
// other value is silently ignored by the API and returns the whole feed, which
// would mislabel listings. Unsupported values must never be sent.
func TestKalibrrCountryParamOnlyForSupportedValues(t *testing.T) {
	cases := map[string]string{
		"ID": "Indonesia", "id": "Indonesia",
		"PH": "Philippines", "ph": "Philippines",
		"SG": "", "MY": "", "US": "", "": "",
	}
	for iso, want := range cases {
		if got := kalibrrCountryParam(iso); got != want {
			t.Errorf("kalibrrCountryParam(%q) = %q, want %q", iso, got, want)
		}
	}
}

func TestKalibrrSendsOnlyVerifiedParams(t *testing.T) {
	var rawQuery string
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"count":0,"jobs":[]}`))
	}))
	defer srv.Close()

	k := NewKalibrr()
	k.baseURL = srv.URL

	// A supported market: every parameter is one the feed honours.
	if _, _, err := k.Search(context.Background(), Query{
		Titles:  []string{"Software Engineer"},
		City:    "Jakarta",
		Country: "ID",
		Limit:   10,
	}); err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, want := range []string{"text=Software", "location=Jakarta", "country=Indonesia", "limit=", "offset="} {
		if !strings.Contains(rawQuery, want) {
			t.Errorf("query %q missing %q", rawQuery, want)
		}
	}

	// An unsupported market: no request is made at all, so no wrong-market data
	// can ever be returned under that label.
	calls, rawQuery = 0, ""
	if _, rep, err := k.Search(context.Background(), Query{
		Titles:  []string{"Software Engineer"},
		City:    "Singapore",
		Country: "SG",
		Limit:   10,
	}); err != nil {
		t.Fatalf("search: %v", err)
	} else if rep.Status != "skipped" {
		t.Errorf("unsupported market should skip, got %q", rep.Status)
	}
	if calls != 0 {
		t.Errorf("unsupported market must not issue a request, got %d call(s) to %q", calls, rawQuery)
	}
}

func TestKalibrrSupportedCountryIsSent(t *testing.T) {
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"count":0,"jobs":[]}`))
	}))
	defer srv.Close()

	k := NewKalibrr()
	k.baseURL = srv.URL
	if _, _, err := k.Search(context.Background(), Query{Country: "ID", Limit: 5}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(rawQuery, "country=Indonesia") {
		t.Errorf("supported country should be sent, query was %q", rawQuery)
	}
}

// A market Kalibrr does not serve must be reported, never answered with another
// market's listings: the feed ignores unsupported locations.
func TestKalibrrSkipsMarketsItDoesNotServe(t *testing.T) {
	for _, iso := range []string{"SG", "MY"} {
		k := NewKalibrr()
		got, rep, err := k.Search(context.Background(), Query{Country: iso, City: "Singapore", Limit: 10})
		if err != nil {
			t.Fatalf("%s: search should not error, got %v", iso, err)
		}
		if len(got) != 0 {
			t.Errorf("%s: expected no listings, got %d", iso, len(got))
		}
		assertStatus(t, rep, "kalibrr", "skipped")
		if !strings.Contains(rep.Error, "Indonesia and the Philippines only") {
			t.Errorf("%s: error should explain coverage, got %q", iso, rep.Error)
		}
	}
}

// An unverified place name must not be forwarded: the API would ignore it and
// return the full marketplace.
func TestKalibrrOnlyForwardsVerifiedPlaces(t *testing.T) {
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"count":0,"jobs":[]}`))
	}))
	defer srv.Close()

	k := NewKalibrr()
	k.baseURL = srv.URL

	// Unverified place, supported country: the location must be dropped.
	if _, _, err := k.Search(context.Background(), Query{Country: "ID", City: "Atlantis", Limit: 5}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if strings.Contains(rawQuery, "location=") {
		t.Errorf("unverified place must not be forwarded, query was %q", rawQuery)
	}
	if !strings.Contains(rawQuery, "country=Indonesia") {
		t.Errorf("country should still be sent, query was %q", rawQuery)
	}

	// Verified place: forwarded, with the feed's own spelling.
	rawQuery = ""
	if _, _, err := k.Search(context.Background(), Query{Country: "ID", City: "jakarta selatan", Limit: 5}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(rawQuery, "location=Jakarta+Selatan") {
		t.Errorf("verified place should be forwarded with canonical spelling, query was %q", rawQuery)
	}
}
