package sources

import (
	"context"
	"strings"
	"testing"
)

// Tests for the ATS expansion adapters: SmartRecruiters, Recruitee, Personio.
// Each of them is a watchlist source, so the "no watchlist -> skipped"
// contract matters as much as the field mapping.

// ---- SmartRecruiters -------------------------------------------------------

func TestSmartRecruitersSkippedWithoutWatchlist(t *testing.T) {
	s := NewSmartRecruiters()
	got, rep, err := s.Search(context.Background(), Query{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want no listings, got %d", len(got))
	}
	assertStatus(t, rep, "smartrecruiters", "skipped")
	if !s.Caps().RequiresWatchlist {
		t.Error("must require a watchlist")
	}
	if s.ID() != "smartrecruiters" {
		t.Errorf("id: %q", s.ID())
	}
}

func TestSmartRecruitersMapping(t *testing.T) {
	srv := serveFixture(t, "smartrecruiters.json")
	s := NewSmartRecruiters()
	s.baseURL = srv.URL

	q := Query{Limit: 10, Watchlist: []Watchlist{{SourceID: "smartrecruiters", BoardSlug: "sgs", Company: "SGS"}}}
	got, rep, err := s.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "smartrecruiters", "ok")
	if rep.Fetched != 3 || rep.Accepted != 3 {
		t.Fatalf("counters: fetched=%d accepted=%d", rep.Fetched, rep.Accepted)
	}

	first, ok := indexByJobID(got)["744000154698590"]
	if !ok {
		t.Fatalf("missing listing 744000154698590")
	}
	if first.Title != "Client Service Representative" {
		t.Errorf("title: %q", first.Title)
	}
	if first.Company != "SGS" {
		t.Errorf("company: %q", first.Company)
	}
	if first.URL != "https://jobs.smartrecruiters.com/SGS/744000154698590" {
		t.Errorf("url: %q", first.URL)
	}
	if first.City != "Fairfield" || first.Region != "NJ" {
		t.Errorf("city/region: %q/%q", first.City, first.Region)
	}
	// The API states the country as a lowercase ISO alpha-2 code.
	if first.Country != "US" {
		t.Errorf("country: %q", first.Country)
	}
	if first.EmploymentType != "permanent" || !first.Has(AttrEmploymentType) {
		t.Errorf("employment type: %q verified=%v", first.EmploymentType, first.Has(AttrEmploymentType))
	}
	if first.Seniority != "junior" || !first.Has(AttrSeniority) {
		t.Errorf("seniority: %q verified=%v", first.Seniority, first.Has(AttrSeniority))
	}
	if first.PublishedAt == nil {
		t.Error("published_at should be set from releasedDate")
	}
	// The postings list endpoint publishes no description: it must stay absent
	// rather than be filled with an invented summary.
	if first.Description != "" || first.Has(AttrDescription) {
		t.Errorf("description should be absent, got %q verified=%v", first.Description, first.Has(AttrDescription))
	}
	// "Fairfield, NJ, United States" states no arrangement.
	if first.WorkArrangement != "" || first.Has(AttrWorkArrangement) {
		t.Errorf("arrangement should be unverified, got %q", first.WorkArrangement)
	}
}

// ---- Recruitee -------------------------------------------------------------

func TestRecruiteeSkippedWithoutWatchlist(t *testing.T) {
	r := NewRecruitee()
	got, rep, err := r.Search(context.Background(), Query{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want no listings, got %d", len(got))
	}
	assertStatus(t, rep, "recruitee", "skipped")
}

func TestRecruiteeMapping(t *testing.T) {
	srv := serveFixture(t, "recruitee.json")
	r := NewRecruitee()
	r.baseURLTemplate = srv.URL + "/%s/api/offers/"

	q := Query{Limit: 10, Watchlist: []Watchlist{{SourceID: "recruitee", BoardSlug: "channable", Company: "Channable"}}}
	got, rep, err := r.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "recruitee", "ok")
	if rep.Fetched != 2 || rep.Accepted != 2 {
		t.Fatalf("counters: fetched=%d accepted=%d rejected=%d", rep.Fetched, rep.Accepted, rep.Rejected)
	}

	first, ok := indexByJobID(got)["2773400"]
	if !ok {
		t.Fatalf("missing offer 2773400")
	}
	if first.Title != "Talent Acquisition Partner" {
		t.Errorf("title: %q", first.Title)
	}
	if first.Company != "Channable" {
		t.Errorf("company: %q", first.Company)
	}
	if first.URL != "https://jobs.channable.com/o/talent-acquisition-partner" {
		t.Errorf("url: %q", first.URL)
	}
	if first.City != "Utrecht" || first.Region != "Utrecht" || first.Country != "NL" {
		t.Errorf("location: %q / %q / %q", first.City, first.Region, first.Country)
	}
	// The source states hygiene flags directly.
	if first.WorkArrangement != "hybrid" || !first.Has(AttrWorkArrangement) {
		t.Errorf("arrangement: %q verified=%v", first.WorkArrangement, first.Has(AttrWorkArrangement))
	}
	if first.EmploymentType != "permanent" {
		t.Errorf("employment type: %q", first.EmploymentType)
	}
	if first.Seniority != "mid" {
		t.Errorf("seniority: %q", first.Seniority)
	}
	assertPtr(t, "salary_min", first.SalaryMin, 4250)
	assertPtr(t, "salary_max", first.SalaryMax, 5000)
	if first.SalaryCurrency != "EUR" || first.SalaryPeriod != "month" {
		t.Errorf("salary currency/period: %q/%q", first.SalaryCurrency, first.SalaryPeriod)
	}
	if !first.Has(AttrSalary) {
		t.Error("salary should be verified when amounts are published")
	}
	if first.PublishedAt == nil {
		t.Error("published_at should parse the \"UTC\" suffix format")
	}
	if first.Description == "" || !first.Has(AttrDescription) {
		t.Errorf("description should be present, got %q", first.Description)
	}

	// The second offer is entry level and must map to the junior band.
	second, ok := indexByJobID(got)["2770498"]
	if !ok {
		t.Fatalf("missing offer 2770498")
	}
	if second.Seniority != "junior" {
		t.Errorf("second seniority: %q", second.Seniority)
	}
}

func TestRecruiteePartialBoardFailure(t *testing.T) {
	// A failing board must not take the healthy one down with it.
	srv := serveFixtureFailing(t, "recruitee.json", "broken")
	r := NewRecruitee()
	r.baseURLTemplate = srv.URL + "/%s/api/offers/"

	q := Query{Limit: 10, Watchlist: []Watchlist{
		{SourceID: "recruitee", BoardSlug: "broken", Company: "Broken"},
		{SourceID: "recruitee", BoardSlug: "channable", Company: "Channable"},
	}}
	got, rep, err := r.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("a partial failure must not return an error: %v", err)
	}
	assertStatus(t, rep, "recruitee", "partial")
	if len(got) != 2 {
		t.Fatalf("healthy board should still contribute, got %d", len(got))
	}
	if rep.Error == "" {
		t.Error("partial status must record why")
	}
}

func TestRecruiteeAllBoardsFail(t *testing.T) {
	srv := serveFixtureFailing(t, "recruitee.json", "api")
	r := NewRecruitee()
	r.baseURLTemplate = srv.URL + "/%s/api/offers/"

	q := Query{Watchlist: []Watchlist{{SourceID: "recruitee", BoardSlug: "channable", Company: "Channable"}}}
	_, rep, err := r.Search(context.Background(), q)
	if err == nil {
		t.Fatal("expected an error when every board fails")
	}
	assertStatus(t, rep, "recruitee", "error")
}

// ---- Personio --------------------------------------------------------------

func TestPersonioSkippedWithoutWatchlist(t *testing.T) {
	p := NewPersonio()
	got, rep, err := p.Search(context.Background(), Query{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want no listings, got %d", len(got))
	}
	assertStatus(t, rep, "personio", "skipped")
}

func TestPersonioMapping(t *testing.T) {
	srv := serveFixture(t, "personio.xml")
	p := NewPersonio()
	p.baseURLTemplate = srv.URL + "/%s/xml"

	q := Query{Limit: 10, Watchlist: []Watchlist{{SourceID: "personio", BoardSlug: "thermondo", Company: "Thermondo"}}}
	got, rep, err := p.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "personio", "ok")
	if rep.Fetched != 2 || rep.Accepted != 2 {
		t.Fatalf("counters: fetched=%d accepted=%d", rep.Fetched, rep.Accepted)
	}

	first, ok := indexByJobID(got)["2692963"]
	if !ok {
		t.Fatalf("missing position 2692963")
	}
	if first.Title != "Anlagenmechaniker SHK  (m/w/d) - Mobiles Arbeiten" {
		t.Errorf("title: %q", first.Title)
	}
	if first.Company != "Thermondo GmbH" {
		t.Errorf("company: %q", first.Company)
	}
	// The feed carries no URL element; the adapter builds the board's own job
	// path from the id.
	if first.URL != "https://thermondo.jobs.personio.de/job/2692963" {
		t.Errorf("url: %q", first.URL)
	}
	if first.City != "Neumünster" {
		t.Errorf("city: %q", first.City)
	}
	if first.EmploymentType != "permanent" || !first.Has(AttrEmploymentType) {
		t.Errorf("employment type: %q verified=%v", first.EmploymentType, first.Has(AttrEmploymentType))
	}
	// "experienced" is the source's own wording for the band.
	if first.Seniority != "senior" || !first.Has(AttrSeniority) {
		t.Errorf("seniority: %q verified=%v", first.Seniority, first.Has(AttrSeniority))
	}
	if first.PublishedAt == nil {
		t.Error("published_at should come from createdAt")
	}
	if first.Description == "" || !first.Has(AttrDescription) {
		t.Errorf("description should be flattened from jobDescriptions, got %q", first.Description)
	}
	// The feed states no country; it must stay empty rather than be inferred.
	if first.Country != "" {
		t.Errorf("country must not be invented, got %q", first.Country)
	}
}

func TestPersonioNonXMLFeedIsAnError(t *testing.T) {
	// An unknown subdomain answers with an HTML page, not XML.
	srv := serveBody(t, "<!DOCTYPE html><html><head><title>Page Not Found</title></head></html>")
	p := NewPersonio()
	p.baseURLTemplate = srv.URL + "/%s/xml"

	q := Query{Watchlist: []Watchlist{{SourceID: "personio", BoardSlug: "nope", Company: "Nope"}}}
	got, rep, err := p.Search(context.Background(), q)
	if err == nil {
		t.Fatal("a non-XML feed must be reported, not treated as empty")
	}
	if len(got) != 0 {
		t.Fatalf("want no listings, got %d", len(got))
	}
	assertStatus(t, rep, "personio", "error")
	if !strings.Contains(rep.Error, "not XML") {
		t.Errorf("error should explain the cause, got %q", rep.Error)
	}
}
