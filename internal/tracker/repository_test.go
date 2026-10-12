package tracker

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/valeriandwipurnomo/jobseeker/internal/jobs"
	"github.com/valeriandwipurnomo/jobseeker/internal/sources"
	"github.com/valeriandwipurnomo/jobseeker/internal/storage"
	"github.com/valeriandwipurnomo/jobseeker/migrations"
)

// testDB mirrors the helper used by the jobs and candidate packages: it runs the
// real migrations against TEST_DATABASE_URL and skips when it is not configured
// (`make test-integration` sets it).
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

	if _, err := db.Pool.Exec(ctx, `truncate table users, jobs, job_source_listings, job_matches, applications, saved_jobs cascade`); err != nil {
		t.Fatalf("resetting tables: %v", err)
	}
	if err := ensureTrackerSources(ctx, db); err != nil {
		t.Fatalf("seeding source catalog: %v", err)
	}

	var userID uuid.UUID
	if err := db.Pool.QueryRow(ctx, `
		insert into users (email, password_hash, display_name)
		values ($1, 'x', 'Test User') returning id`, "tracker-test-"+uuid.NewString()+"@example.com").Scan(&userID); err != nil {
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

// ensureTrackerSources seeds the catalog row the fixture's listing references:
// the catalog is data, so migrations can retire a source (0005 removed every
// row) and a fixture must not depend on which rows happen to exist.
func ensureTrackerSources(ctx context.Context, db *storage.DB) error {
	// Upsert rather than "do nothing": the catalog is not truncated between runs,
	// so a row seeded by an earlier version of this fixture must be repaired.
	if _, err := db.Pool.Exec(ctx, `
		insert into job_sources (id, display_name, kind, attribution_text, attribution_url)
		values ('arbeitnow', 'Arbeitnow', 'feed', 'Listings from arbeitnow', 'https://example.test/arbeitnow')
		on conflict (id) do update set
		    display_name = excluded.display_name,
		    kind = excluded.kind,
		    attribution_text = excluded.attribution_text,
		    attribution_url = excluded.attribution_url`); err != nil {
		return err
	}
	return nil
}

// seedJob inserts one vacancy through the real jobs repository so the tracker is
// exercised against rows shaped exactly like production data.
func seedJob(t *testing.T, db *storage.DB) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	listing := sources.RawListing{
		SourceID:    "arbeitnow",
		SourceJobID: "a-" + uuid.NewString(),
		URL:         "https://portal.test/jobs/" + uuid.NewString(),
		Title:       "Senior Frontend Engineer",
		Company:     "Alpha Co",
		Description: "React and TypeScript.",
		City:        "Jakarta",
		Country:     "ID",
	}
	listing.Verify(sources.AttrTitle, sources.AttrCompany, sources.AttrLocation)

	res, err := jobs.NewRepository(db).UpsertListing(ctx, jobs.UpsertInput{
		Listing:  listing,
		Verified: listing.AttributesVerified,
		Now:      time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("seeding job: %v", err)
	}
	return res.JobID
}

func jobStatus(t *testing.T, db *storage.DB, jobID uuid.UUID) string {
	t.Helper()
	var status string
	if err := db.Pool.QueryRow(context.Background(),
		`select status from jobs where id = $1`, jobID).Scan(&status); err != nil {
		t.Fatalf("reading job status: %v", err)
	}
	return status
}

// Taking an application back must clear the send date: a status of "saved" says
// no application was sent, and a surviving applied_at would contradict it. The
// date is otherwise preserved when a later update omits it.
func TestApplicationStatusTransitions(t *testing.T) {
	db, userID := testDB(t)
	ctx := context.Background()
	repo := NewRepository(db)
	jobID := seedJob(t, db)

	sent := time.Date(2026, 10, 10, 9, 30, 0, 0, time.UTC)

	t.Run("marking applied stores the send date", func(t *testing.T) {
		app, err := repo.UpsertApplication(ctx, userID, jobID, "applied", &sent, nil, "")
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if app.Status != "applied" || app.AppliedAt == nil || !app.AppliedAt.Equal(sent) {
			t.Fatalf("application: status=%q applied_at=%v", app.Status, app.AppliedAt)
		}
		if got := jobStatus(t, db, jobID); got != "applied" {
			t.Errorf("job status: %q, want applied", got)
		}
	})

	t.Run("a later stage keeps the date when it is omitted", func(t *testing.T) {
		app, err := repo.UpsertApplication(ctx, userID, jobID, "interviewing", nil, nil, "")
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if app.AppliedAt == nil || !app.AppliedAt.Equal(sent) {
			t.Fatalf("applied_at should survive an update that omits it, got %v", app.AppliedAt)
		}
		if got := jobStatus(t, db, jobID); got != "interviewing" {
			t.Errorf("job status: %q, want interviewing", got)
		}
	})

	t.Run("taking the application back clears the date", func(t *testing.T) {
		app, err := repo.UpsertApplication(ctx, userID, jobID, "saved", nil, nil, "")
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if app.Status != "saved" {
			t.Fatalf("status: %q, want saved", app.Status)
		}
		if app.AppliedAt != nil {
			t.Errorf("applied_at must be cleared when the application is taken back, got %v", app.AppliedAt)
		}
		// The tracker maps pre-send stages onto the job status "saved".
		if got := jobStatus(t, db, jobID); got != "saved" {
			t.Errorf("job status: %q, want saved", got)
		}
	})

	t.Run("rejects an unknown status", func(t *testing.T) {
		if _, err := repo.UpsertApplication(ctx, userID, jobID, "ghosted", nil, nil, ""); !errors.Is(err, ErrInvalidStatus) {
			t.Fatalf("want ErrInvalidStatus, got %v", err)
		}
	})

	t.Run("rejects an unknown job", func(t *testing.T) {
		if _, err := repo.UpsertApplication(ctx, userID, uuid.New(), "applied", nil, nil, ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}

// Bookmarking is a separate fact from an application: saving must not create an
// application record, and unsaving must not destroy one.
func TestSaveIsIndependentOfApplications(t *testing.T) {
	db, userID := testDB(t)
	ctx := context.Background()
	repo := NewRepository(db)
	jobID := seedJob(t, db)

	if err := repo.Save(ctx, userID, jobID); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := repo.Save(ctx, userID, jobID); err != nil {
		t.Fatalf("saving twice should be idempotent: %v", err)
	}

	if _, err := repo.UpsertApplication(ctx, userID, jobID, "applied", nil, nil, "referred"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := repo.Unsave(ctx, userID, jobID); err != nil {
		t.Fatalf("unsave: %v", err)
	}

	apps, err := repo.ListApplications(ctx, userID)
	if err != nil {
		t.Fatalf("list applications: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("applications after unsave: %d, want 1 (unsaving must not delete it)", len(apps))
	}

	saved, err := repo.ListSaved(ctx, userID)
	if err != nil {
		t.Fatalf("list saved: %v", err)
	}
	// Unsaving removed the bookmark (the application record is what survives),
	// which is exactly what "two independent facts" means here.
	if len(saved) != 0 {
		t.Fatalf("saved jobs after unsave: %d, want 0", len(saved))
	}
}
