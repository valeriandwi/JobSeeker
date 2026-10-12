package jobs

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/valeriandwipurnomo/jobseeker/internal/matching"
	"github.com/valeriandwipurnomo/jobseeker/internal/sources"
	"github.com/valeriandwipurnomo/jobseeker/internal/storage"
	"github.com/valeriandwipurnomo/jobseeker/migrations"
)

// testDB connects to TEST_DATABASE_URL, applies migrations and resets the
// vacancy tables. The test is skipped (not failed) when no test database is
// configured, so `go test ./...` stays green on a machine without Postgres.
func testDB(t *testing.T) (*storage.DB, uuid.UUID) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping database integration test")
	}

	ctx := context.Background()
	if err := applyMigrations(url); err != nil {
		t.Fatalf("applying migrations: %v", err)
	}
	db, err := storage.Open(ctx, url)
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	t.Cleanup(db.Close)

	if _, err := db.Pool.Exec(ctx, `truncate table users, jobs, job_source_listings, job_matches, search_runs cascade`); err != nil {
		t.Fatalf("resetting tables: %v", err)
	}
	if err := ensureJobSources(ctx, db); err != nil {
		t.Fatalf("seeding source catalog: %v", err)
	}

	var userID uuid.UUID
	if err := db.Pool.QueryRow(ctx, `
		insert into users (email, password_hash, display_name)
		values ($1, 'x', 'Test User') returning id`, "repo-test-"+uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("seeding user: %v", err)
	}
	return db, userID
}

func applyMigrations(url string) error {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return err
	}
	defer db.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, ".")
}

// ensureJobSources seeds the catalog rows the fixtures reference.
//
// The catalog is DATA, not code: migrations retire sources (0005 deleted every
// row, 0007/0008 re-added only kalibrr and jobstreet). A test that assumes
// `arbeitnow` simply exists therefore fails for reasons that have nothing to do
// with what it is testing, so each fixture seeds what it needs.
func ensureJobSources(ctx context.Context, db *storage.DB) error {
	ids := []string{
		"arbeitnow", "remotive", "remoteok", "jobicy", "himalayas",
		"greenhouse", "lever", "ashby", "smartrecruiters", "recruitee", "personio",
		"kalibrr", "jobstreet",
	}
	for _, id := range ids {
		// Attribution text is part of the contract (the product stores and shows
		// it per source), so the fixture carries it too — a seed with an empty
		// attribution column would fail the very assertion that guards it.
		if _, err := db.Pool.Exec(ctx, `
			insert into job_sources (id, display_name, kind, attribution_text, attribution_url)
			values ($1, $1, 'feed', 'Listings from ' || $1, 'https://example.test/' || $1)
			on conflict (id) do update set
			    display_name = excluded.display_name,
			    kind = excluded.kind,
			    attribution_text = excluded.attribution_text,
			    attribution_url = excluded.attribution_url`, id); err != nil {
			return err
		}
	}
	return nil
}

func sampleListing(sourceID, sourceJobID, url, title, company, city, country string) sources.RawListing {
	l := sources.RawListing{
		SourceID:    sourceID,
		SourceJobID: sourceJobID,
		URL:         url,
		Title:       title,
		Company:     company,
		City:        city,
		Country:     country,
		Description: "We build React and TypeScript interfaces with Go services and PostgreSQL.",
	}
	l.Verify(sources.AttrTitle, sources.AttrCompany, sources.AttrDescription)
	return l
}

func countRows(t *testing.T, db *storage.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("counting (%s): %v", query, err)
	}
	return n
}

func TestUpsertCreatesThenUpdatesSameListing(t *testing.T) {
	db, _ := testDB(t)
	ctx := context.Background()
	repo := NewRepository(db)
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)

	l := sampleListing("arbeitnow", "a-1", "https://example.test/jobs/1", "Senior Frontend Engineer", "Example Co", "Kuala Lumpur", "MY")
	l.Verify(sources.AttrLocation)

	first, err := repo.UpsertListing(ctx, UpsertInput{Listing: l, Verified: l.AttributesVerified, Now: now})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if first.Outcome != OutcomeNew {
		t.Fatalf("outcome = %q, want new", first.Outcome)
	}

	second, err := repo.UpsertListing(ctx, UpsertInput{Listing: l, Verified: l.AttributesVerified, Now: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if second.Outcome != OutcomeUpdated {
		t.Fatalf("outcome = %q, want updated", second.Outcome)
	}
	if second.JobID != first.JobID {
		t.Fatal("the same listing must map to the same canonical vacancy")
	}
	if got := countRows(t, db, `select count(*) from jobs`); got != 1 {
		t.Fatalf("jobs rows = %d, want 1", got)
	}
	if got := countRows(t, db, `select count(*) from job_source_listings`); got != 1 {
		t.Fatalf("listing rows = %d, want 1", got)
	}
}

func TestCrossPortalDuplicateMergesIntoOneVacancy(t *testing.T) {
	db, _ := testDB(t)
	ctx := context.Background()
	repo := NewRepository(db)
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)

	a := sampleListing("arbeitnow", "a-1", "https://portal-a.test/jobs/1", "Senior Frontend Engineer", "Example Co", "Kuala Lumpur", "MY")
	a.Verify(sources.AttrLocation)
	if _, err := repo.UpsertListing(ctx, UpsertInput{Listing: a, Verified: a.AttributesVerified, Now: now}); err != nil {
		t.Fatalf("first portal: %v", err)
	}

	b := sampleListing("himalayas", "h-9", "https://portal-b.test/vacancy/9", "Senior Frontend Engineer (Remote)", "Example Co., Ltd", "Kuala Lumpur", "MY")
	b.Verify(sources.AttrLocation, sources.AttrWorkArrangement)
	b.WorkArrangement = "remote"
	res, err := repo.UpsertListing(ctx, UpsertInput{Listing: b, Verified: b.AttributesVerified, Now: now})
	if err != nil {
		t.Fatalf("second portal: %v", err)
	}
	if res.Outcome != OutcomeDuplicate {
		t.Fatalf("outcome = %q, want duplicate (decision: %s)", res.Outcome, res.Decision.Reason)
	}
	if got := countRows(t, db, `select count(*) from jobs`); got != 1 {
		t.Fatalf("jobs rows = %d, want 1 (the same vacancy must not be duplicated)", got)
	}
	if got := countRows(t, db, `select count(*) from job_source_listings`); got != 2 {
		t.Fatalf("listing rows = %d, want 2 (per-portal attribution must be preserved)", got)
	}

	// The second source's newly published arrangement must be merged in.
	var arrangement string
	if err := db.Pool.QueryRow(ctx, `select work_arrangement from jobs`).Scan(&arrangement); err != nil {
		t.Fatal(err)
	}
	if arrangement != "remote" {
		t.Errorf("work_arrangement = %q, want remote (merged from the second source)", arrangement)
	}
}

func TestDifferentCompanyIsNotMerged(t *testing.T) {
	db, _ := testDB(t)
	ctx := context.Background()
	repo := NewRepository(db)
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)

	a := sampleListing("arbeitnow", "a-1", "https://portal-a.test/jobs/1", "Senior Frontend Engineer", "Example Co", "Kuala Lumpur", "MY")
	b := sampleListing("himalayas", "h-2", "https://portal-b.test/vacancy/2", "Senior Frontend Engineer", "Different Corp", "Kuala Lumpur", "MY")

	for _, l := range []sources.RawListing{a, b} {
		if _, err := repo.UpsertListing(ctx, UpsertInput{Listing: l, Verified: l.AttributesVerified, Now: now}); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	if got := countRows(t, db, `select count(*) from jobs`); got != 2 {
		t.Fatalf("jobs rows = %d, want 2", got)
	}
}

func TestAttributesAreNeverInventedOrOverwritten(t *testing.T) {
	db, _ := testDB(t)
	ctx := context.Background()
	repo := NewRepository(db)
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)

	// A listing that publishes neither salary nor arrangement.
	l := sampleListing("jobicy", "j-1", "https://portal.test/jobs/1", "Backend Engineer", "Example Co", "Jakarta", "ID")
	if _, err := repo.UpsertListing(ctx, UpsertInput{Listing: l, Verified: l.AttributesVerified, Now: now}); err != nil {
		t.Fatal(err)
	}

	var salaryMin *float64
	var arrangement, verifiedJSON string
	if err := db.Pool.QueryRow(ctx, `select salary_min, work_arrangement, attributes_verified::text from jobs`).Scan(&salaryMin, &arrangement, &verifiedJSON); err != nil {
		t.Fatal(err)
	}
	if salaryMin != nil {
		t.Errorf("salary must stay null when the source did not disclose it, got %v", *salaryMin)
	}
	if arrangement != "unknown" {
		t.Errorf("arrangement = %q, want unknown", arrangement)
	}
	if contains(verifiedJSON, `"salary"`) {
		t.Errorf("salary must not be marked as verified: %s", verifiedJSON)
	}

	// Now a second portal publishes the salary: it must be filled in.
	withSalary := sampleListing("himalayas", "h-1", "https://other.test/v/1", "Backend Engineer", "Example Co", "Jakarta", "ID")
	withSalary.Verify(sources.AttrLocation)
	min, max := 150000.0, 190000.0
	withSalary.SalaryMin, withSalary.SalaryMax = &min, &max
	withSalary.SalaryCurrency = "USD"
	withSalary.SalaryPeriod = "year"
	withSalary.Verify(sources.AttrSalary)
	if _, err := repo.UpsertListing(ctx, UpsertInput{Listing: withSalary, Verified: withSalary.AttributesVerified, Now: now}); err != nil {
		t.Fatal(err)
	}
	var filled *float64
	if err := db.Pool.QueryRow(ctx, `select salary_min from jobs`).Scan(&filled); err != nil {
		t.Fatal(err)
	}
	if filled == nil || *filled != 150000 {
		t.Fatalf("salary should be filled from the source that published it, got %v", filled)
	}

	// A later listing without a salary must not erase the known value.
	noSalary := sampleListing("jobicy", "j-1", "https://portal.test/jobs/1", "Backend Engineer", "Example Co", "Jakarta", "ID")
	if _, err := repo.UpsertListing(ctx, UpsertInput{Listing: noSalary, Verified: noSalary.AttributesVerified, Now: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var stillThere *float64
	if err := db.Pool.QueryRow(ctx, `select salary_min from jobs`).Scan(&stillThere); err != nil {
		t.Fatal(err)
	}
	if stillThere == nil {
		t.Fatal("an unverified update must not clear a previously published salary")
	}
}

func TestListJobsFiltersSortsAndScores(t *testing.T) {
	db, userID := testDB(t)
	ctx := context.Background()
	repo := NewRepository(db)
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)

	strong := sampleListing("arbeitnow", "a-1", "https://a.test/1", "Senior Frontend Engineer", "Alpha Co", "Kuala Lumpur", "MY")
	strong.Verify(sources.AttrLocation, sources.AttrWorkArrangement)
	strong.WorkArrangement = "hybrid"
	weak := sampleListing("jobicy", "j-1", "https://b.test/2", "Warehouse Operative", "Beta Co", "Jakarta", "ID")
	weak.Verify(sources.AttrLocation)

	r1, err := repo.UpsertListing(ctx, UpsertInput{Listing: strong, Verified: strong.AttributesVerified, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := repo.UpsertListing(ctx, UpsertInput{Listing: weak, Verified: weak.AttributesVerified, Now: now})
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.UpsertMatch(ctx, r1.JobID, userID, 1, matching.Result{
		Score: 91, ProfileVersion: 1,
		Explanation: []matching.Component{{Component: "skills", Weight: 30, Contribution: 30, Status: "matched", Reason: "React and TypeScript present"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMatch(ctx, r2.JobID, userID, 1, matching.Result{Score: 12, ProfileVersion: 1}); err != nil {
		t.Fatal(err)
	}

	// Give the weaker match a LATER discovery time: the default sort must follow
	// discovery, not score, and an equal timestamp would only prove the tiebreak.
	if _, err := db.Pool.Exec(ctx,
		`update jobs set first_discovered_at = $1 where id = $2`, now.Add(time.Hour), r2.JobID); err != nil {
		t.Fatal(err)
	}

	t.Run("sorts by first discovered by default", func(t *testing.T) {
		rows, total, err := repo.ListJobs(ctx, userID, JobFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if total != 2 || len(rows) != 2 {
			t.Fatalf("total=%d rows=%d", total, len(rows))
		}
		// Discovered later and scored lower: it must still come first.
		if rows[0].Title != "Warehouse Operative" {
			t.Fatalf("expected the most recently discovered vacancy first, got %q", rows[0].Title)
		}
	})

	t.Run("sorted by score on request", func(t *testing.T) {
		rows, total, err := repo.ListJobs(ctx, userID, JobFilter{Sort: "score"})
		if err != nil {
			t.Fatal(err)
		}
		if total != 2 || len(rows) != 2 {
			t.Fatalf("total=%d rows=%d", total, len(rows))
		}
		if rows[0].MatchScore == nil || *rows[0].MatchScore != 91 {
			t.Fatalf("expected the 91-score vacancy first, got %+v", rows[0].MatchScore)
		}
		if rows[0].Title != "Senior Frontend Engineer" {
			t.Errorf("unexpected first row: %s", rows[0].Title)
		}
		if len(rows[0].MatchExplanation) != 1 || rows[0].MatchExplanation[0].Reason == "" {
			t.Errorf("match explanation missing: %+v", rows[0].MatchExplanation)
		}
		if len(rows[0].Sources) != 1 || rows[0].Sources[0].SourceID != "arbeitnow" {
			t.Errorf("source attribution missing: %+v", rows[0].Sources)
		}
		if rows[0].Sources[0].AttributionText == "" {
			t.Error("attribution text must be present for every source")
		}
	})

	t.Run("min_score filter", func(t *testing.T) {
		rows, total, err := repo.ListJobs(ctx, userID, JobFilter{MinScore: 50})
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || len(rows) != 1 {
			t.Fatalf("expected 1 result, got total=%d rows=%d", total, len(rows))
		}
	})

	t.Run("source filter", func(t *testing.T) {
		rows, _, err := repo.ListJobs(ctx, userID, JobFilter{Source: "jobicy"})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Company != "Beta Co" {
			t.Fatalf("source filter failed: %+v", rows)
		}
	})

	t.Run("full text search", func(t *testing.T) {
		rows, _, err := repo.ListJobs(ctx, userID, JobFilter{Query: "frontend"})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Company != "Alpha Co" {
			t.Fatalf("search failed: %+v", rows)
		}
	})

	t.Run("pagination", func(t *testing.T) {
		rows, total, err := repo.ListJobs(ctx, userID, JobFilter{PerPage: 1, Page: 2, Sort: "company"})
		if err != nil {
			t.Fatal(err)
		}
		if total != 2 || len(rows) != 1 {
			t.Fatalf("total=%d rows=%d", total, len(rows))
		}
		if rows[0].Company != "Beta Co" {
			t.Errorf("expected Beta Co on page 2 sorted by company, got %s", rows[0].Company)
		}
	})

	t.Run("get by id", func(t *testing.T) {
		rec, err := repo.GetJob(ctx, userID, r1.JobID)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Title != "Senior Frontend Engineer" {
			t.Errorf("title = %q", rec.Title)
		}
		if _, err := repo.GetJob(ctx, userID, uuid.New()); err != ErrNotFound {
			t.Errorf("missing job should return ErrNotFound, got %v", err)
		}
	})

	t.Run("hard-constraint violations are hidden unless requested", func(t *testing.T) {
		excludedJob := sampleListing("jobicy", "ex-1", "https://c.test/3", "DevOps Engineer", "Gamma Co", "Berlin", "DE")
		res, err := repo.UpsertListing(ctx, UpsertInput{Listing: excludedJob, Verified: excludedJob.AttributesVerified, Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.UpsertMatch(ctx, res.JobID, userID, 1, matching.Result{
			Score: 0, ProfileVersion: 1, Excluded: true,
			Explanation: []matching.Component{{Component: "location", Weight: 12, Status: "not_matched", Reason: "mandatory country mismatch"}},
		}); err != nil {
			t.Fatal(err)
		}

		rows, total, err := repo.ListJobs(ctx, userID, JobFilter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.ID == res.JobID {
				t.Fatal("a vacancy that violated a mandatory constraint must not appear by default")
			}
		}
		if total != 2 {
			t.Fatalf("total = %d, want 2 (the excluded vacancy is not counted)", total)
		}

		rows, total, err = repo.ListJobs(ctx, userID, JobFilter{IncludeExcluded: true})
		if err != nil {
			t.Fatal(err)
		}
		if total != 3 {
			t.Fatalf("total with IncludeExcluded = %d, want 3", total)
		}
		found := false
		for _, row := range rows {
			if row.ID == res.JobID {
				found = true
				if !row.Excluded {
					t.Error("the row must be flagged as excluded so the UI can explain why")
				}
			}
		}
		if !found {
			t.Fatal("IncludeExcluded must surface the vacancy")
		}
	})
}

func TestSingleFailedVerificationDoesNotExpire(t *testing.T) {
	db, _ := testDB(t)
	ctx := context.Background()
	repo := NewRepository(db)
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)

	l := sampleListing("arbeitnow", "a-1", "https://a.test/1", "Backend Engineer", "Alpha Co", "Jakarta", "ID")
	if _, err := repo.UpsertListing(ctx, UpsertInput{Listing: l, Verified: l.AttributesVerified, Now: now}); err != nil {
		t.Fatal(err)
	}

	// The source feed comes back empty once.
	if _, _, err := repo.ApplyVerificationUpdates(ctx, "arbeitnow", nil, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	expired, err := repo.ExpireVacancies(ctx, now.Add(time.Hour), DefaultExpiryPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if expired != 0 {
		t.Fatalf("a single failed verification must not expire anything, expired=%d", expired)
	}
	var status string
	if err := db.Pool.QueryRow(ctx, `select status from jobs`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == "expired" {
		t.Fatal("vacancy must not be expired after one miss")
	}

	// The listing reappears: it must be active again and failures reset.
	if _, _, err := repo.ApplyVerificationUpdates(ctx, "arbeitnow", []string{"a-1"}, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, `select last_status from job_source_listings`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "active" {
		t.Fatalf("listing status = %q, want active", status)
	}
}

func TestRepeatedMissesExpireAfterGraceWindow(t *testing.T) {
	db, _ := testDB(t)
	ctx := context.Background()
	repo := NewRepository(db)
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)

	l := sampleListing("arbeitnow", "a-1", "https://a.test/1", "Backend Engineer", "Alpha Co", "Jakarta", "ID")
	if _, err := repo.UpsertListing(ctx, UpsertInput{Listing: l, Verified: l.AttributesVerified, Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.ApplyVerificationUpdates(ctx, "arbeitnow", nil, now); err != nil {
		t.Fatal(err)
	}
	// Past the grace window.
	later := now.Add(96 * time.Hour)
	expired, err := repo.ExpireVacancies(ctx, later, DefaultExpiryPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if expired != 1 {
		t.Fatalf("expected 1 expiry after the grace window, got %d", expired)
	}
	var status string
	if err := db.Pool.QueryRow(ctx, `select status from jobs`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "expired" {
		t.Fatalf("status = %q, want expired", status)
	}
}

func TestPublishedDeadlineExpiresVacancy(t *testing.T) {
	db, _ := testDB(t)
	ctx := context.Background()
	repo := NewRepository(db)
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)

	l := sampleListing("greenhouse", "g-1", "https://boards.test/1", "Backend Engineer", "Alpha Co", "Jakarta", "ID")
	deadline := now.Add(-time.Hour)
	l.ExpiresAt = &deadline
	l.Verify(sources.AttrExpiresAt)
	if _, err := repo.UpsertListing(ctx, UpsertInput{Listing: l, Verified: l.AttributesVerified, Now: now}); err != nil {
		t.Fatal(err)
	}
	expired, err := repo.ExpireVacancies(ctx, now, DefaultExpiryPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if expired != 1 {
		t.Fatalf("a passed published deadline must expire the vacancy, expired=%d", expired)
	}
}

func TestDashboardCounts(t *testing.T) {
	db, userID := testDB(t)
	ctx := context.Background()
	repo := NewRepository(db)
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)

	l := sampleListing("arbeitnow", "a-1", "https://a.test/1", "Backend Engineer", "Alpha Co", "Jakarta", "ID")
	res, err := repo.UpsertListing(ctx, UpsertInput{Listing: l, Verified: l.AttributesVerified, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMatch(ctx, res.JobID, userID, 1, matching.Result{Score: 88, ProfileVersion: 1}); err != nil {
		t.Fatal(err)
	}

	counts, err := repo.DashboardCounts(ctx, userID, now.Add(-time.Hour), 70)
	if err != nil {
		t.Fatal(err)
	}
	if counts.TotalJobs != 1 {
		t.Errorf("TotalJobs = %d", counts.TotalJobs)
	}
	if counts.NewSinceLastRun != 1 {
		t.Errorf("NewSinceLastRun = %d", counts.NewSinceLastRun)
	}
	if counts.HighMatchCount != 1 {
		t.Errorf("HighMatchCount = %d", counts.HighMatchCount)
	}
}

func TestUpsertListingSkipsIncompleteListings(t *testing.T) {
	db, _ := testDB(t)
	ctx := context.Background()
	repo := NewRepository(db)

	t.Run("no title", func(t *testing.T) {
		res, err := repo.UpsertListing(ctx, UpsertInput{Listing: sources.RawListing{SourceID: "arbeitnow", SourceJobID: "x"}})
		if err != nil {
			t.Fatal(err)
		}
		if res.Outcome != OutcomeSkipped {
			t.Fatalf("outcome = %q, want skipped", res.Outcome)
		}
	})

	t.Run("no source", func(t *testing.T) {
		res, err := repo.UpsertListing(ctx, UpsertInput{Listing: sources.RawListing{Title: "Engineer"}})
		if err != nil {
			t.Fatal(err)
		}
		if res.Outcome != OutcomeSkipped {
			t.Fatalf("outcome = %q, want skipped", res.Outcome)
		}
	})

	if got := countRows(t, db, `select count(*) from jobs`); got != 0 {
		t.Fatalf("incomplete listings must not create vacancies, found %d", got)
	}
}
