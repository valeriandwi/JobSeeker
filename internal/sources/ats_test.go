package sources

import (
	"context"
	"strings"
	"testing"
)

// ---- Greenhouse ------------------------------------------------------------

func TestGreenhouseSkippedWithoutWatchlist(t *testing.T) {
	g := NewGreenhouse()
	got, rep, err := g.Search(context.Background(), Query{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want no listings, got %d", len(got))
	}
	assertStatus(t, rep, "greenhouse", "skipped")
	if !g.Caps().RequiresWatchlist {
		t.Errorf("greenhouse must require a watchlist")
	}
	if g.ID() != "greenhouse" {
		t.Errorf("id: %q", g.ID())
	}
}

func TestGreenhouseMapping(t *testing.T) {
	srv := serveFixture(t, "greenhouse.json")
	g := NewGreenhouse()
	g.baseURL = srv.URL

	q := Query{Limit: 10, Watchlist: []Watchlist{{SourceID: "greenhouse", BoardSlug: "vercel", Company: "Vercel"}}}
	got, rep, err := g.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "greenhouse", "ok")
	if rep.Fetched != 3 || rep.Accepted != 3 {
		t.Fatalf("counters: fetched=%d accepted=%d", rep.Fetched, rep.Accepted)
	}

	by := indexByJobID(got)
	first, ok := by["6136160004"]
	if !ok {
		t.Fatalf("missing listing 6136160004")
	}
	if first.Title != "Account Executive, Commercial" {
		t.Errorf("title: %q", first.Title)
	}
	if first.Company != "Vercel" {
		t.Errorf("company: %q", first.Company)
	}
	if first.LocationRaw != "Hybrid - London" {
		t.Errorf("location: %q", first.LocationRaw)
	}
	if first.WorkArrangement != "hybrid" || !first.Has(AttrWorkArrangement) {
		t.Errorf("arrangement: %q verified=%v", first.WorkArrangement, first.Has(AttrWorkArrangement))
	}
	if first.PublishedAt == nil || !first.Has(AttrPublishedAt) {
		t.Errorf("published_at not captured")
	}
	if first.Has(AttrSalary) {
		t.Errorf("greenhouse payload has no salary")
	}
	// content is HTML-escaped in the feed; the mapping must yield plain text.
	if strings.Contains(first.Description, "<") || strings.Contains(first.Description, "&lt;") {
		short := first.Description
		if len(short) > 60 {
			short = short[:60]
		}
		t.Errorf("description should be plain text: %q", short)
	}
}

func TestGreenhousePartialBoardFailure(t *testing.T) {
	srv := serveFixtureFailing(t, "greenhouse.json", "bad")
	g := NewGreenhouse()
	g.baseURL = srv.URL

	q := Query{Limit: 10, Watchlist: []Watchlist{
		{SourceID: "greenhouse", BoardSlug: "vercel", Company: "Vercel"},
		{SourceID: "greenhouse", BoardSlug: "bad", Company: "Bad Co"},
	}}
	got, rep, err := g.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("one bad board must not fail the source: %v", err)
	}
	assertStatus(t, rep, "greenhouse", "partial")
	if rep.Accepted != 3 || len(got) != 3 {
		t.Fatalf("want 3 listings, got %d (accepted=%d)", len(got), rep.Accepted)
	}
	if rep.Error == "" {
		t.Errorf("partial report should carry the failure text")
	}
}

func TestGreenhouseAllBoardsFail(t *testing.T) {
	srv := serveFixtureFailing(t, "greenhouse.json", "bad")
	g := NewGreenhouse()
	g.baseURL = srv.URL

	q := Query{Watchlist: []Watchlist{{SourceID: "greenhouse", BoardSlug: "bad", Company: "Bad Co"}}}
	got, rep, err := g.Search(context.Background(), q)
	if err == nil {
		t.Fatalf("want error when every board fails")
	}
	if len(got) != 0 {
		t.Fatalf("want no listings, got %d", len(got))
	}
	assertStatus(t, rep, "greenhouse", "error")
	if rep.Error == "" {
		t.Errorf("error report should carry the failure text")
	}
}

// ---- Lever -----------------------------------------------------------------

func TestLeverSkippedWithoutWatchlist(t *testing.T) {
	l := NewLever()
	got, rep, err := l.Search(context.Background(), Query{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want no listings, got %d", len(got))
	}
	assertStatus(t, rep, "lever", "skipped")
	if !l.Caps().RequiresWatchlist {
		t.Errorf("lever must require a watchlist")
	}
	if l.ID() != "lever" {
		t.Errorf("id: %q", l.ID())
	}
}

func TestLeverMapping(t *testing.T) {
	srv := serveFixture(t, "lever.json")
	l := NewLever()
	l.baseURL = srv.URL

	q := Query{Limit: 10, Watchlist: []Watchlist{{SourceID: "lever", BoardSlug: "leverdemo", Company: "Lever Demo"}}}
	got, rep, err := l.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "lever", "ok")
	if rep.Fetched != 3 || rep.Accepted != 3 {
		t.Fatalf("counters: fetched=%d accepted=%d", rep.Fetched, rep.Accepted)
	}

	by := indexByJobID(got)
	paid := by["ad208490-4052-4f91-a57e-433f2d1e484b"]
	if paid.SourceJobID == "" {
		t.Fatalf("missing lever listing, got %d", len(got))
	}
	if paid.WorkArrangement != "hybrid" || !paid.Has(AttrWorkArrangement) {
		t.Errorf("arrangement: %q", paid.WorkArrangement)
	}
	if paid.EmploymentType != "permanent" || !paid.Has(AttrEmploymentType) {
		t.Errorf("employment: %q", paid.EmploymentType)
	}
	assertPtr(t, "lever min", paid.SalaryMin, 85000)
	assertPtr(t, "lever max", paid.SalaryMax, 175000)
	if paid.SalaryCurrency != "USD" || paid.SalaryPeriod != "year" || !paid.Has(AttrSalary) {
		t.Errorf("salary: %s/%s verified=%v", paid.SalaryCurrency, paid.SalaryPeriod, paid.Has(AttrSalary))
	}
	if paid.Company != "Lever Demo" {
		t.Errorf("company: %q", paid.Company)
	}
	if paid.LocationRaw != "Atlanta, Georgia" || !paid.Has(AttrLocation) {
		t.Errorf("location: %q", paid.LocationRaw)
	}
	if paid.PublishedAt == nil || !paid.Has(AttrPublishedAt) {
		t.Errorf("published_at not captured")
	}

	// Third posting: workplaceType "remote", no salary, commitment "Remote"
	// which is not an employment type.
	rem := by["7e10ea46-98be-4bd1-a3ca-57d153b25c61"]
	if rem.WorkArrangement != "remote" || !rem.Has(AttrWorkArrangement) {
		t.Errorf("arrangement: %q", rem.WorkArrangement)
	}
	if rem.SalaryMin != nil || rem.Has(AttrSalary) {
		t.Errorf("salary should be absent")
	}
	if rem.EmploymentType != "" || rem.Has(AttrEmploymentType) {
		t.Errorf("employment should be unknown, got %q", rem.EmploymentType)
	}
}

func TestLeverPartialBoardFailure(t *testing.T) {
	srv := serveFixtureFailing(t, "lever.json", "bad")
	l := NewLever()
	l.baseURL = srv.URL

	q := Query{Limit: 10, Watchlist: []Watchlist{
		{SourceID: "lever", BoardSlug: "leverdemo", Company: "Lever Demo"},
		{SourceID: "lever", BoardSlug: "bad", Company: "Bad Co"},
	}}
	got, rep, err := l.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("one bad board must not fail the source: %v", err)
	}
	assertStatus(t, rep, "lever", "partial")
	if len(got) != 3 || rep.Accepted != 3 {
		t.Fatalf("want 3 listings, got %d", len(got))
	}
}

// ---- Ashby -----------------------------------------------------------------

func TestAshbySkippedWithoutWatchlist(t *testing.T) {
	a := NewAshby()
	got, rep, err := a.Search(context.Background(), Query{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want no listings, got %d", len(got))
	}
	assertStatus(t, rep, "ashby", "skipped")
	if !a.Caps().RequiresWatchlist {
		t.Errorf("ashby must require a watchlist")
	}
	if a.ID() != "ashby" {
		t.Errorf("id: %q", a.ID())
	}
}

func TestAshbyMapping(t *testing.T) {
	srv := serveFixture(t, "ashby.json")
	a := NewAshby()
	a.baseURL = srv.URL

	q := Query{Limit: 10, Watchlist: []Watchlist{{SourceID: "ashby", BoardSlug: "ramp", Company: "Ramp"}}}
	got, rep, err := a.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "ashby", "ok")
	if rep.Fetched != 3 || rep.Accepted != 3 {
		t.Fatalf("counters: fetched=%d accepted=%d", rep.Fetched, rep.Accepted)
	}

	by := indexByJobID(got)
	eng := by["34413f8d-26bf-4bbc-8ade-eb309a0e2245"]
	if eng.SourceJobID == "" {
		t.Fatalf("missing ashby listing, got %d", len(got))
	}
	if eng.Title != "Security Engineer, Cloud" {
		t.Errorf("title should be trimmed: %q", eng.Title)
	}
	if eng.Company != "Ramp" {
		t.Errorf("company: %q", eng.Company)
	}
	if eng.WorkArrangement != "hybrid" || !eng.Has(AttrWorkArrangement) {
		t.Errorf("arrangement: %q", eng.WorkArrangement)
	}
	if eng.EmploymentType != "permanent" || !eng.Has(AttrEmploymentType) {
		t.Errorf("employment: %q", eng.EmploymentType)
	}
	// "$211.4K - $290.6K" must expand the magnitude and keep the decimal.
	assertPtr(t, "ashby min", eng.SalaryMin, 211400)
	assertPtr(t, "ashby max", eng.SalaryMax, 290600)
	if eng.SalaryCurrency != "USD" || !eng.Has(AttrSalary) {
		t.Errorf("salary: %s verified=%v", eng.SalaryCurrency, eng.Has(AttrSalary))
	}
	if eng.City != "New York City" || eng.Region != "NY" {
		t.Errorf("address: city=%q region=%q", eng.City, eng.Region)
	}
	// "USA" is not an ISO alpha-2 code, so Country must remain empty.
	if eng.Country != "" {
		t.Errorf("country should be empty for non-alpha-2 input, got %q", eng.Country)
	}

	// Second posting: no compensation object -> salary unknown; OnSite.
	noComp := by["1515fe6d-1d8e-475b-a5ee-cefe43e78cb7"]
	if noComp.SourceJobID == "" {
		t.Fatalf("missing second ashby listing")
	}
	if noComp.SalaryMin != nil || noComp.SalaryMax != nil || noComp.Has(AttrSalary) {
		t.Errorf("salary should be absent when compensation is absent")
	}
	if noComp.WorkArrangement != "onsite" || !noComp.Has(AttrWorkArrangement) {
		t.Errorf("arrangement: %q", noComp.WorkArrangement)
	}
}

func TestAshbyPartialBoardFailure(t *testing.T) {
	srv := serveFixtureFailing(t, "ashby.json", "bad")
	a := NewAshby()
	a.baseURL = srv.URL

	q := Query{Limit: 10, Watchlist: []Watchlist{
		{SourceID: "ashby", BoardSlug: "ramp", Company: "Ramp"},
		{SourceID: "ashby", BoardSlug: "bad", Company: "Bad Co"},
	}}
	got, rep, err := a.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("one bad board must not fail the source: %v", err)
	}
	assertStatus(t, rep, "ashby", "partial")
	if len(got) != 3 || rep.Accepted != 3 {
		t.Fatalf("want 3 listings, got %d", len(got))
	}
}

func TestAshbySkipsUnlistedPosting(t *testing.T) {
	// Synthetic edge case: no captured Ashby board currently exposes an
	// isListed:false posting, so the skip path is exercised with a minimal
	// inline payload carrying the real field shape.
	body := `{"jobs":[
		{"id":"visible","title":"Visible Role","isListed":true,"workplaceType":"Remote","employmentType":"FullTime","jobUrl":"https://ex/1","applyUrl":"https://ex/1","publishedAt":"2026-01-01T00:00:00Z"},
		{"id":"hidden","title":"Hidden Role","isListed":false,"workplaceType":"Remote","employmentType":"FullTime","jobUrl":"https://ex/2","applyUrl":"https://ex/2"}
	]}`
	srv := serveBody(t, body)
	a := NewAshby()
	a.baseURL = srv.URL

	q := Query{Limit: 10, Watchlist: []Watchlist{{SourceID: "ashby", BoardSlug: "x", Company: "X"}}}
	got, rep, err := a.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if rep.Fetched != 1 || rep.Accepted != 1 {
		t.Fatalf("isListed:false posting must be skipped: fetched=%d accepted=%d", rep.Fetched, rep.Accepted)
	}
	if len(got) != 1 || got[0].SourceJobID != "visible" {
		t.Fatalf("want only the listed posting, got %+v", got)
	}
}
