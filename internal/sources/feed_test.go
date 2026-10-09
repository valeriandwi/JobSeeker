package sources

import (
	"context"
	"strings"
	"testing"
)

// ---- Arbeitnow -------------------------------------------------------------

func TestArbeitnowMapping(t *testing.T) {
	srv := serveFixture(t, "arbeitnow.json")
	a := NewArbeitnow()
	a.baseURL = srv.URL

	got, rep, err := a.Search(context.Background(), Query{Limit: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "arbeitnow", "ok")
	if rep.Fetched != 3 || rep.Accepted != 3 || rep.Rejected != 0 {
		t.Fatalf("counters: fetched=%d accepted=%d rejected=%d", rep.Fetched, rep.Accepted, rep.Rejected)
	}

	by := indexByJobID(got)
	remote, ok := by["software-engineer-product-full-stack-angular-kotlin-mongodb-berlin-365981"]
	if !ok {
		t.Fatalf("missing remote listing, got %d", len(got))
	}
	if remote.Title != "Software Engineer, Product (Full-Stack: Angular, Kotlin, MongoDB)" {
		t.Errorf("title: %q", remote.Title)
	}
	if remote.Company != "Vestlane" {
		t.Errorf("company: %q", remote.Company)
	}
	if remote.WorkArrangement != "remote" || !remote.Has(AttrWorkArrangement) {
		t.Errorf("arrangement: %q verified=%v", remote.WorkArrangement, remote.Has(AttrWorkArrangement))
	}
	if !remote.Has(AttrPublishedAt) || remote.PublishedAt == nil {
		t.Errorf("published_at not captured")
	}
	if remote.SalaryMin != nil || remote.SalaryMax != nil {
		t.Errorf("salary should be absent, got %v/%v", remote.SalaryMin, remote.SalaryMax)
	}
	if remote.Has(AttrSalary) {
		t.Errorf("AttrSalary must not be verified when the source has no salary")
	}
	if remote.Description == "" {
		t.Errorf("description empty")
	}

	onsite := by["ausbildung-als-fachinformatiker-systemintegration-metzingen-270502"]
	// A bare city name ("Metzingen") states no arrangement.
	if onsite.WorkArrangement != "" || onsite.Has(AttrWorkArrangement) {
		t.Errorf("arrangement: %q verified=%v (want unknown)", onsite.WorkArrangement, onsite.Has(AttrWorkArrangement))
	}
	// job_types are present but do not normalize to a canonical value.
	if onsite.EmploymentType != "" || onsite.Has(AttrEmploymentType) {
		t.Errorf("employment: %q verified=%v (want unknown)", onsite.EmploymentType, onsite.Has(AttrEmploymentType))
	}
	if onsite.LocationRaw != "Metzingen" || !onsite.Has(AttrLocation) {
		t.Errorf("location: %q", onsite.LocationRaw)
	}
}

func TestArbeitnowLimitAndFilters(t *testing.T) {
	srv := serveFixture(t, "arbeitnow.json")
	a := NewArbeitnow()
	a.baseURL = srv.URL

	got, rep, err := a.Search(context.Background(), Query{Limit: 2})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 2 || rep.Accepted != 2 {
		t.Fatalf("limit: got=%d accepted=%d", len(got), rep.Accepted)
	}

	_, rep, err = a.Search(context.Background(), Query{Limit: 3, ExcludeKeywords: []string{"Vestlane"}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if rep.Accepted != 2 || rep.Rejected != 1 {
		t.Fatalf("exclude counters: accepted=%d rejected=%d", rep.Accepted, rep.Rejected)
	}

	got, rep, err = a.Search(context.Background(), Query{Limit: 3, Titles: []string{"Software Engineer"}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || rep.Accepted != 1 {
		t.Fatalf("client-side title filter: got=%d accepted=%d", len(got), rep.Accepted)
	}
}

// ---- Remotive --------------------------------------------------------------

func TestRemotiveMapping(t *testing.T) {
	srv := serveFixture(t, "remotive.json")
	r := NewRemotive()
	r.baseURL = srv.URL

	got, rep, err := r.Search(context.Background(), Query{Limit: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "remotive", "ok")
	if rep.Fetched != 3 || rep.Accepted != 3 {
		t.Fatalf("counters: fetched=%d accepted=%d", rep.Fetched, rep.Accepted)
	}

	by := indexByJobID(got)

	noSalary := by["2091045"]
	if noSalary.SalaryMin != nil || noSalary.Has(AttrSalary) {
		t.Errorf("salary should be absent for %s", noSalary.SourceJobID)
	}
	if noSalary.WorkArrangement != "remote" || !noSalary.Has(AttrWorkArrangement) {
		t.Errorf("arrangement: %q", noSalary.WorkArrangement)
	}
	if noSalary.EmploymentType != "permanent" || !noSalary.Has(AttrEmploymentType) {
		t.Errorf("employment: %q", noSalary.EmploymentType)
	}
	if noSalary.LocationRaw != "Worldwide" || !noSalary.Has(AttrLocation) {
		t.Errorf("location: %q", noSalary.LocationRaw)
	}
	if noSalary.PublishedAt == nil || !noSalary.PublishedAt.Equal(mustTime(t, "2026-10-07T01:11:09Z")) {
		t.Errorf("published_at: %v", noSalary.PublishedAt)
	}

	hourly := by["2091149"]
	assertPtr(t, "hourly min", hourly.SalaryMin, 45)
	assertPtr(t, "hourly max", hourly.SalaryMax, 120)
	if hourly.SalaryCurrency != "USD" || hourly.SalaryPeriod != "hour" || !hourly.Has(AttrSalary) {
		t.Errorf("hourly salary: %s/%s verified=%v", hourly.SalaryCurrency, hourly.SalaryPeriod, hourly.Has(AttrSalary))
	}

	annual := by["2091141"]
	// "$90k - $105k" must expand the magnitude suffix; parsing it as 90/105
	// would misstate the salary by three orders of magnitude.
	assertPtr(t, "annual min", annual.SalaryMin, 90000)
	assertPtr(t, "annual max", annual.SalaryMax, 105000)
	if annual.SalaryCurrency != "USD" || !annual.Has(AttrSalary) {
		t.Errorf("annual salary: %s verified=%v", annual.SalaryCurrency, annual.Has(AttrSalary))
	}
}

func TestRemotiveExclude(t *testing.T) {
	srv := serveFixture(t, "remotive.json")
	r := NewRemotive()
	r.baseURL = srv.URL

	_, rep, err := r.Search(context.Background(), Query{Limit: 3, ExcludeKeywords: []string{"CodeForAI"}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if rep.Accepted != 2 || rep.Rejected != 1 {
		t.Fatalf("exclude counters: accepted=%d rejected=%d", rep.Accepted, rep.Rejected)
	}
}

// ---- RemoteOK --------------------------------------------------------------

func TestRemoteOKMapping(t *testing.T) {
	srv := serveFixture(t, "remoteok.json")
	r := NewRemoteOK()
	r.baseURL = srv.URL

	got, rep, err := r.Search(context.Background(), Query{Limit: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "remoteok", "ok")
	// The fixture carries one metadata element plus three jobs.
	if rep.Fetched != 3 || rep.Accepted != 3 {
		t.Fatalf("counters: fetched=%d accepted=%d", rep.Fetched, rep.Accepted)
	}

	by := indexByJobID(got)
	first, ok := by["1137469"]
	if !ok {
		t.Fatalf("missing listing 1137469")
	}
	if !strings.HasPrefix(first.Title, "Telefondienst") {
		t.Errorf("title: %q", first.Title)
	}
	if first.WorkArrangement != "remote" || !first.Has(AttrWorkArrangement) {
		t.Errorf("arrangement: %q", first.WorkArrangement)
	}
	// salary_min/max are 0 in the payload: no salary was published.
	if first.SalaryMin != nil || first.Has(AttrSalary) {
		t.Errorf("salary should be absent")
	}
	if first.PublishedAt == nil || !first.PublishedAt.Equal(mustTime(t, "2026-10-07T16:00:05Z")) {
		t.Errorf("published_at: %v", first.PublishedAt)
	}

	// A listing with an empty location must not claim AttrLocation.
	emptyLoc := by["1137465"]
	if emptyLoc.Has(AttrLocation) {
		t.Errorf("AttrLocation must not be verified for an empty location")
	}
	if emptyLoc.LocationRaw != "" {
		t.Errorf("location: %q", emptyLoc.LocationRaw)
	}
}

// ---- Jobicy ----------------------------------------------------------------

func TestJobicyMapping(t *testing.T) {
	srv := serveFixture(t, "jobicy.json")
	j := NewJobicy()
	j.baseURL = srv.URL

	got, rep, err := j.Search(context.Background(), Query{Limit: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "jobicy", "ok")
	if rep.Fetched != 3 || rep.Accepted != 3 {
		t.Fatalf("counters: fetched=%d accepted=%d", rep.Fetched, rep.Accepted)
	}

	by := indexByJobID(got)

	paid := by["152748"]
	assertPtr(t, "jobicy min", paid.SalaryMin, 150000)
	assertPtr(t, "jobicy max", paid.SalaryMax, 165000)
	if paid.SalaryCurrency != "USD" || paid.SalaryPeriod != "year" || !paid.Has(AttrSalary) {
		t.Errorf("salary: %s/%s verified=%v", paid.SalaryCurrency, paid.SalaryPeriod, paid.Has(AttrSalary))
	}
	if paid.Seniority != "lead" || !paid.Has(AttrSeniority) {
		t.Errorf("seniority: %q verified=%v", paid.Seniority, paid.Has(AttrSeniority))
	}
	if paid.EmploymentType != "permanent" || !paid.Has(AttrEmploymentType) {
		t.Errorf("employment: %q", paid.EmploymentType)
	}
	if paid.WorkArrangement != "remote" || !paid.Has(AttrWorkArrangement) {
		t.Errorf("arrangement: %q", paid.WorkArrangement)
	}

	// No salary keys at all: salary must stay unknown.
	unpaid := by["152836"]
	if unpaid.SalaryMin != nil || unpaid.SalaryMax != nil || unpaid.Has(AttrSalary) {
		t.Errorf("salary should be absent for %s", unpaid.SourceJobID)
	}
}

// ---- Himalayas -------------------------------------------------------------

func TestHimalayasMapping(t *testing.T) {
	srv := serveFixture(t, "himalayas.json")
	h := NewHimalayas()
	h.baseURL = srv.URL

	got, rep, err := h.Search(context.Background(), Query{Limit: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertStatus(t, rep, "himalayas", "ok")
	if rep.Fetched != 3 || rep.Accepted != 3 {
		t.Fatalf("counters: fetched=%d accepted=%d", rep.Fetched, rep.Accepted)
	}

	var paid, unpaid RawListing
	for _, l := range got {
		if l.Title == "Business Development Manager - Global, Remote (UNCAPPED COMMISSIONS)" {
			paid = l
		}
		if l.Title == "Senior QA Engineer" {
			unpaid = l
		}
	}

	assertPtr(t, "himalayas min", paid.SalaryMin, 1500)
	assertPtr(t, "himalayas max", paid.SalaryMax, 2500)
	if paid.SalaryCurrency != "USD" || paid.SalaryPeriod != "month" || !paid.Has(AttrSalary) {
		t.Errorf("salary: %s/%s verified=%v", paid.SalaryCurrency, paid.SalaryPeriod, paid.Has(AttrSalary))
	}
	if paid.Seniority != "lead" || !paid.Has(AttrSeniority) {
		t.Errorf("seniority: %q", paid.Seniority)
	}
	if paid.ExpiresAt == nil || !paid.Has(AttrExpiresAt) {
		t.Errorf("expires_at not captured")
	}
	if paid.WorkArrangement != "remote" || !paid.Has(AttrWorkArrangement) {
		t.Errorf("arrangement: %q", paid.WorkArrangement)
	}

	// min/max are null even though a currency+period string is present.
	if unpaid.SalaryMin != nil || unpaid.SalaryMax != nil || unpaid.Has(AttrSalary) {
		t.Errorf("salary should be absent when min/max are null")
	}
	if unpaid.EmploymentType != "contract" || !unpaid.Has(AttrEmploymentType) {
		t.Errorf("employment: %q", unpaid.EmploymentType)
	}
}
