package candidate

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/valeriandwipurnomo/jobseeker/internal/matching"
	"github.com/valeriandwipurnomo/jobseeker/internal/resume"
	"github.com/valeriandwipurnomo/jobseeker/internal/storage"
	"github.com/valeriandwipurnomo/jobseeker/migrations"
)

func testRepo(t *testing.T) (*Repository, uuid.UUID) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping database integration test")
	}
	ctx := context.Background()

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = db.Close()

	pool, err := storage.Open(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Pool.Exec(ctx, `truncate table users cascade`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	var userID uuid.UUID
	if err := pool.Pool.QueryRow(ctx, `
		insert into users (email, password_hash) values ($1, 'x') returning id`,
		"candidate-"+uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return NewRepository(pool), userID
}

func TestPreferencesRoundTrip(t *testing.T) {
	repo, userID := testRepo(t)
	ctx := context.Background()

	t.Run("unset preferences return usable defaults", func(t *testing.T) {
		prefs, exists, err := repo.GetPreferences(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatal("no preferences row should exist yet")
		}
		if prefs.JobTitles == nil || prefs.Locations == nil || prefs.WorkArrangements == nil {
			t.Fatalf("defaults must be non-nil slices for JSON: %+v", prefs)
		}
	})

	t.Run("save with no salary", func(t *testing.T) {
		saved, err := repo.SavePreferences(ctx, Preferences{
			UserID:    userID,
			JobTitles: []string{"Frontend Engineer", "Full Stack Engineer"},
			Keywords:  []string{"react", "typescript", "go"},
			Locations: []matching.LocationPref{
				{Country: "ID", Mandatory: false},
				{Country: "MY", City: "Kuala Lumpur", Mandatory: true},
			},
			WorkArrangements: []matching.ArrangementPref{
				{Value: "remote", Mandatory: false},
				{Value: "hybrid", Mandatory: false},
			},
			EmploymentTypes: []string{"permanent", "full_time", "contract"},
			SeniorityLevels: []string{"senior", "mid"},
			MinSalary:       nil,
			SalaryCurrency:  "",
			SalaryPeriod:    "",
			MinScore:        0,
		})
		if err != nil {
			t.Fatalf("SavePreferences: %v", err)
		}
		if len(saved.Locations) != 2 || saved.Locations[1].City != "Kuala Lumpur" || !saved.Locations[1].Mandatory {
			t.Fatalf("locations did not round-trip: %+v", saved.Locations)
		}
		if len(saved.WorkArrangements) != 2 || saved.WorkArrangements[0].Value != "remote" {
			t.Fatalf("arrangements did not round-trip: %+v", saved.WorkArrangements)
		}
		if saved.MinSalary != nil {
			t.Fatalf("min salary should stay null, got %v", *saved.MinSalary)
		}
	})

	t.Run("save with a salary then back to null", func(t *testing.T) {
		min := 120000.0
		saved, err := repo.SavePreferences(ctx, Preferences{
			UserID:         userID,
			JobTitles:      []string{"Frontend Engineer"},
			MinSalary:      &min,
			SalaryCurrency: "USD",
			SalaryPeriod:   "year",
			MinScore:       40,
		})
		if err != nil {
			t.Fatalf("SavePreferences with salary: %v", err)
		}
		if saved.MinSalary == nil || *saved.MinSalary != 120000 {
			t.Fatalf("salary did not round-trip: %+v", saved.MinSalary)
		}
		if saved.SalaryCurrency != "USD" || saved.MinScore != 40 {
			t.Fatalf("currency/min score: %q %d", saved.SalaryCurrency, saved.MinScore)
		}

		saved, err = repo.SavePreferences(ctx, Preferences{UserID: userID, JobTitles: []string{"Frontend Engineer"}})
		if err != nil {
			t.Fatalf("clearing the salary failed: %v", err)
		}
		if saved.MinSalary != nil {
			t.Fatal("clearing min_salary must be possible")
		}
	})

	t.Run("preferences are per user", func(t *testing.T) {
		var otherID uuid.UUID
		if err := repo.db.Pool.QueryRow(ctx, `
			insert into users (email, password_hash) values ($1, 'x') returning id`,
			"other-"+uuid.NewString()+"@example.com").Scan(&otherID); err != nil {
			t.Fatal(err)
		}
		other, exists, err := repo.GetPreferences(ctx, otherID)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("another user must not see these preferences: %+v", other)
		}
	})
}

func TestProfileRoundTripAndVersionBump(t *testing.T) {
	repo, userID := testRepo(t)
	ctx := context.Background()

	profile, exists, err := repo.GetProfile(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("no profile should exist yet")
	}
	if profile.Skills == nil {
		t.Fatal("default skills slice must be non-nil")
	}

	years := 10.2
	saved, err := repo.SaveProfile(ctx, Profile{
		UserID: userID, FullName: "Test Candidate", Headline: "Senior Full Stack Engineer",
		CurrentTitle: "Senior Full Stack Engineer", YearsExperience: &years, Seniority: "senior",
		Skills: []string{"React", "Go"}, Industries: []string{"Fintech"}, Languages: []string{"English"},
	})
	if err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	if saved.Version != 1 {
		t.Fatalf("first save version = %d, want 1", saved.Version)
	}
	if saved.ConfirmedAt == nil {
		t.Fatal("confirmed_at must be set on save")
	}

	// A PUT replaces the whole profile: a field omitted from the request is
	// cleared, which is why the client must always send the reviewed profile in
	// full. This is asserted in the repository tests.
	again, err := repo.SaveProfile(ctx, Profile{
		UserID: userID, FullName: "Test Candidate", Seniority: "senior",
		CurrentTitle: "Senior Full Stack Engineer", Skills: []string{"React", "Go", "PostgreSQL"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.Version != 2 {
		t.Fatalf("second save must bump the version, got %d", again.Version)
	}

	loaded, exists, err := repo.GetProfile(ctx, userID)
	if err != nil || !exists {
		t.Fatalf("GetProfile: exists=%v err=%v", exists, err)
	}
	if len(loaded.Skills) != 3 || loaded.Skills[0] != "React" {
		t.Fatalf("skills did not persist: %+v", loaded.Skills)
	}

	// Replace semantics: omitting skills clears them.
	cleared, err := repo.SaveProfile(ctx, Profile{UserID: userID, FullName: "Test Candidate", Seniority: "senior"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.Skills) != 0 {
		t.Fatalf("a profile PUT without skills must clear them (replace semantics), got %+v", cleared.Skills)
	}
	if cleared.Version != 3 {
		t.Fatalf("version = %d, want 3", cleared.Version)
	}

	// The matching input must carry the confirmed version so scores are
	// invalidated when the profile changes.
	if m := loaded.ToMatching(); m.Version != loaded.Version {
		t.Errorf("matching version = %d, want %d", m.Version, loaded.Version)
	}
	if titles := loaded.ToMatching().Titles; len(titles) == 0 {
		t.Error("target titles must be derived for scoring")
	}
}

func TestResumeLifecycle(t *testing.T) {
	repo, userID := testRepo(t)
	ctx := context.Background()

	record, err := repo.CreateResume(ctx, userID, "cv.pdf", "application/pdf", 1234, "abc123", "resumes/x.pdf")
	if err != nil {
		t.Fatalf("CreateResume: %v", err)
	}
	if record.Status != "uploaded" {
		t.Fatalf("status = %q", record.Status)
	}

	if err := repo.SetResumeStatus(ctx, userID, record.ID, "parsed", ""); err != nil {
		t.Fatal(err)
	}
	draft := resume.Draft{
		Fields: map[string]resume.Field{
			resume.FieldSkills: {Value: []string{"React"}, Provenance: resume.ProvenanceInferred, Confidence: 0.7},
		},
		TextChars: 500,
		Warnings:  []string{"inferred"},
	}
	if err := repo.SaveExtraction(ctx, record.ID, userID, "deterministic-v1", "1", draft); err != nil {
		t.Fatal(err)
	}

	got, path, err := repo.GetResume(ctx, userID, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if path != "resumes/x.pdf" {
		t.Fatalf("storage path = %q", path)
	}
	if got.Status != "parsed" {
		t.Fatalf("status = %q, want parsed", got.Status)
	}

	extracted, resumeID, err := repo.LatestExtraction(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if resumeID == nil || *resumeID != record.ID {
		t.Fatal("LatestExtraction must return the resume id")
	}
	if extracted.TextChars != 500 {
		t.Fatalf("draft did not round-trip: %+v", extracted)
	}

	list, err := repo.ListResumes(ctx, userID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListResumes: %v (%d)", err, len(list))
	}

	deletedPath, err := repo.DeleteResume(ctx, userID, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deletedPath != "resumes/x.pdf" {
		t.Fatalf("DeleteResume must return the file path, got %q", deletedPath)
	}
	list, err = repo.ListResumes(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatal("a deleted resume must disappear from the list")
	}
	if _, _, err := repo.GetResume(ctx, userID, record.ID); err != ErrNotFound {
		t.Fatalf("GetResume after delete = %v, want ErrNotFound", err)
	}
}

func TestResumesAreScopedToTheirOwner(t *testing.T) {
	repo, userID := testRepo(t)
	ctx := context.Background()

	record, err := repo.CreateResume(ctx, userID, "cv.pdf", "application/pdf", 10, "h", "resumes/a.pdf")
	if err != nil {
		t.Fatal(err)
	}
	var otherID uuid.UUID
	if err := repo.db.Pool.QueryRow(ctx, `
		insert into users (email, password_hash) values ($1, 'x') returning id`,
		"intruder-"+uuid.NewString()+"@example.com").Scan(&otherID); err != nil {
		t.Fatal(err)
	}

	if _, _, err := repo.GetResume(ctx, otherID, record.ID); err != ErrNotFound {
		t.Fatalf("another user must not read this resume: %v", err)
	}
	if _, err := repo.DeleteResume(ctx, otherID, record.ID); err != ErrNotFound {
		t.Fatalf("another user must not delete this resume: %v", err)
	}
	if _, _, err := repo.GetProfile(ctx, otherID); err != nil {
		t.Fatal(err)
	}
}
