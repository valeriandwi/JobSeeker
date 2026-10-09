// Package candidate owns the user-confirmed candidate profile, search
// preferences and CV resume metadata.
//
// The profile stored here contains ONLY values the user has confirmed. Machine
// extraction output lives in resume_extractions and is never promoted silently:
// that separation is what stops an inferred keyword from becoming a filter.
package candidate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/valeriandwipurnomo/jobseeker/internal/matching"
	"github.com/valeriandwipurnomo/jobseeker/internal/resume"
	"github.com/valeriandwipurnomo/jobseeker/internal/storage"
)

// ErrNotFound reports a missing profile, preference set or resume.
var ErrNotFound = errors.New("candidate: not found")

// Profile is the user-confirmed candidate profile.
type Profile struct {
	UserID          uuid.UUID  `json:"user_id"`
	FullName        string     `json:"full_name"`
	Headline        string     `json:"headline"`
	CurrentTitle    string     `json:"current_title"`
	YearsExperience *float64   `json:"years_experience"`
	Seniority       string     `json:"seniority"`
	Skills          []string   `json:"skills"`
	Industries      []string   `json:"industries"`
	Education       []string   `json:"education"`
	Certifications  []string   `json:"certifications"`
	Languages       []string   `json:"languages"`
	Projects        []string   `json:"projects"`
	SourceResumeID  *uuid.UUID `json:"source_resume_id,omitempty"`
	Version         int        `json:"version"`
	ConfirmedAt     *time.Time `json:"confirmed_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ToMatching converts the profile for the scoring engine.
func (p Profile) ToMatching() matching.Profile {
	return matching.Profile{
		Version:         p.Version,
		Titles:          matching.DeriveTitles(p.CurrentTitle, p.Headline),
		Skills:          p.Skills,
		Industries:      p.Industries,
		CurrentTitle:    p.CurrentTitle,
		Seniority:       p.Seniority,
		YearsExperience: p.YearsExperience,
	}
}

// Preferences is the user's search configuration.
type Preferences struct {
	UserID           uuid.UUID                  `json:"user_id"`
	JobTitles        []string                   `json:"job_titles"`
	Keywords         []string                   `json:"keywords"`
	ExcludedKeywords []string                   `json:"excluded_keywords"`
	Locations        []matching.LocationPref    `json:"locations"`
	WorkArrangements []matching.ArrangementPref `json:"work_arrangements"`
	EmploymentTypes  []string                   `json:"employment_types"`
	SeniorityLevels  []string                   `json:"seniority_levels"`
	MinSalary        *float64                   `json:"min_salary"`
	SalaryCurrency   string                     `json:"salary_currency"`
	SalaryPeriod     string                     `json:"salary_period"`
	MinScore         int                        `json:"min_score"`
	UpdatedAt        time.Time                  `json:"updated_at"`
}

// ToMatching converts the preferences for the scoring engine.
func (p Preferences) ToMatching() matching.Preferences {
	return matching.Preferences{
		Locations:        p.Locations,
		WorkArrangements: p.WorkArrangements,
		EmploymentTypes:  p.EmploymentTypes,
		SeniorityLevels:  p.SeniorityLevels,
		MinSalary:        p.MinSalary,
		SalaryCurrency:   p.SalaryCurrency,
		SalaryPeriod:     p.SalaryPeriod,
		ExcludedKeywords: p.ExcludedKeywords,
		MinScore:         p.MinScore,
	}
}

// Repository persists profiles, preferences and resume metadata.
type Repository struct {
	db *storage.DB
}

// NewRepository wires the repository to the connection pool.
func NewRepository(db *storage.DB) *Repository { return &Repository{db: db} }

// GetProfile loads the confirmed profile.
func (r *Repository) GetProfile(ctx context.Context, userID uuid.UUID) (Profile, bool, error) {
	var p Profile
	var skills, industries, education, certs, langs, projects []byte
	err := r.db.Pool.QueryRow(ctx, `
		select user_id, full_name, headline, current_title, years_experience, seniority,
		       skills, industries, education, certifications, languages, projects,
		       source_resume_id, version, confirmed_at, updated_at
		from candidate_profiles where user_id = $1`, userID).
		Scan(&p.UserID, &p.FullName, &p.Headline, &p.CurrentTitle, &p.YearsExperience, &p.Seniority,
			&skills, &industries, &education, &certs, &langs, &projects,
			&p.SourceResumeID, &p.Version, &p.ConfirmedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{UserID: userID, Seniority: "unknown", Skills: []string{}}, false, nil
	}
	if err != nil {
		return Profile{}, false, fmt.Errorf("loading profile: %w", err)
	}
	p.Skills = decodeStrings(skills)
	p.Industries = decodeStrings(industries)
	p.Education = decodeStrings(education)
	p.Certifications = decodeStrings(certs)
	p.Languages = decodeStrings(langs)
	p.Projects = decodeStrings(projects)
	return p, true, nil
}

// SaveProfile upserts the confirmed profile and bumps its version, which
// invalidates stored match scores for the previous version.
func (r *Repository) SaveProfile(ctx context.Context, p Profile) (Profile, error) {
	if p.Seniority == "" {
		p.Seniority = "unknown"
	}
	confirmedAt := time.Now().UTC()
	var out Profile
	var skills, industries, education, certs, langs, projects []byte
	err := r.db.Pool.QueryRow(ctx, `
		insert into candidate_profiles (
		    user_id, full_name, headline, current_title, years_experience, seniority,
		    skills, industries, education, certifications, languages, projects,
		    source_resume_id, version, confirmed_at, updated_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,1,$14,$14)
		on conflict (user_id) do update set
		    full_name = excluded.full_name,
		    headline = excluded.headline,
		    current_title = excluded.current_title,
		    years_experience = excluded.years_experience,
		    seniority = excluded.seniority,
		    skills = excluded.skills,
		    industries = excluded.industries,
		    education = excluded.education,
		    certifications = excluded.certifications,
		    languages = excluded.languages,
		    projects = excluded.projects,
		    source_resume_id = excluded.source_resume_id,
		    version = candidate_profiles.version + 1,
		    confirmed_at = excluded.confirmed_at,
		    updated_at = excluded.updated_at
		returning user_id, full_name, headline, current_title, years_experience, seniority,
		          skills, industries, education, certifications, languages, projects,
		          source_resume_id, version, confirmed_at, updated_at`,
		p.UserID, p.FullName, p.Headline, p.CurrentTitle, p.YearsExperience, p.Seniority,
		encodeStrings(p.Skills), encodeStrings(p.Industries), encodeStrings(p.Education),
		encodeStrings(p.Certifications), encodeStrings(p.Languages), encodeStrings(p.Projects),
		p.SourceResumeID, confirmedAt).
		Scan(&out.UserID, &out.FullName, &out.Headline, &out.CurrentTitle, &out.YearsExperience, &out.Seniority,
			&skills, &industries, &education, &certs, &langs, &projects,
			&out.SourceResumeID, &out.Version, &out.ConfirmedAt, &out.UpdatedAt)
	if err != nil {
		return Profile{}, fmt.Errorf("saving profile: %w", err)
	}
	out.Skills = decodeStrings(skills)
	out.Industries = decodeStrings(industries)
	out.Education = decodeStrings(education)
	out.Certifications = decodeStrings(certs)
	out.Languages = decodeStrings(langs)
	out.Projects = decodeStrings(projects)
	return out, nil
}

// GetPreferences loads preferences, returning usable defaults when unset.
func (r *Repository) GetPreferences(ctx context.Context, userID uuid.UUID) (Preferences, bool, error) {
	var p Preferences
	var titles, keywords, excluded, locations, arrangements, employment, seniority []byte
	err := r.db.Pool.QueryRow(ctx, `
		select user_id, job_titles, keywords, excluded_keywords, locations, work_arrangements,
		       employment_types, seniority_levels, min_salary,
		       coalesce(salary_currency, ''), coalesce(salary_period, ''), min_score, updated_at
		from search_preferences where user_id = $1`, userID).
		Scan(&p.UserID, &titles, &keywords, &excluded, &locations, &arrangements,
			&employment, &seniority, &p.MinSalary, &p.SalaryCurrency, &p.SalaryPeriod, &p.MinScore, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return emptyPreferences(userID), false, nil
	}
	if err != nil {
		return Preferences{}, false, fmt.Errorf("loading preferences: %w", err)
	}
	p.JobTitles = decodeStrings(titles)
	p.Keywords = decodeStrings(keywords)
	p.ExcludedKeywords = decodeStrings(excluded)
	p.EmploymentTypes = decodeStrings(employment)
	p.SeniorityLevels = decodeStrings(seniority)
	_ = json.Unmarshal(locations, &p.Locations)
	_ = json.Unmarshal(arrangements, &p.WorkArrangements)
	return p, true, nil
}

func emptyPreferences(userID uuid.UUID) Preferences {
	return Preferences{
		UserID:           userID,
		JobTitles:        []string{},
		Keywords:         []string{},
		ExcludedKeywords: []string{},
		Locations:        []matching.LocationPref{},
		WorkArrangements: []matching.ArrangementPref{},
		EmploymentTypes:  []string{},
		SeniorityLevels:  []string{},
	}
}

// SavePreferences upserts the search configuration.
func (r *Repository) SavePreferences(ctx context.Context, p Preferences) (Preferences, error) {
	var out Preferences
	var titles, keywords, excluded, locations, arrangements, employment, seniority []byte
	err := r.db.Pool.QueryRow(ctx, `
		insert into search_preferences (
		    user_id, job_titles, keywords, excluded_keywords, locations, work_arrangements,
		    employment_types, seniority_levels, min_salary, salary_currency, salary_period, min_score
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,nullif($10,''),nullif($11,''),$12)
		on conflict (user_id) do update set
		    job_titles = excluded.job_titles,
		    keywords = excluded.keywords,
		    excluded_keywords = excluded.excluded_keywords,
		    locations = excluded.locations,
		    work_arrangements = excluded.work_arrangements,
		    employment_types = excluded.employment_types,
		    seniority_levels = excluded.seniority_levels,
		    min_salary = excluded.min_salary,
		    salary_currency = excluded.salary_currency,
		    salary_period = excluded.salary_period,
		    min_score = excluded.min_score,
		    updated_at = now()
		returning user_id, job_titles, keywords, excluded_keywords, locations, work_arrangements,
		          employment_types, seniority_levels, min_salary,
		          coalesce(salary_currency, ''), coalesce(salary_period, ''), min_score, updated_at`,
		p.UserID, encodeStrings(p.JobTitles), encodeStrings(p.Keywords), encodeStrings(p.ExcludedKeywords),
		encodeJSON(p.Locations), encodeJSON(p.WorkArrangements),
		encodeStrings(p.EmploymentTypes), encodeStrings(p.SeniorityLevels),
		p.MinSalary, p.SalaryCurrency, p.SalaryPeriod, clampScore(p.MinScore)).
		Scan(&out.UserID, &titles, &keywords, &excluded, &locations, &arrangements,
			&employment, &seniority, &out.MinSalary, &out.SalaryCurrency, &out.SalaryPeriod, &out.MinScore, &out.UpdatedAt)
	if err != nil {
		return Preferences{}, fmt.Errorf("saving preferences: %w", err)
	}
	out.JobTitles = decodeStrings(titles)
	out.Keywords = decodeStrings(keywords)
	out.ExcludedKeywords = decodeStrings(excluded)
	out.EmploymentTypes = decodeStrings(employment)
	out.SeniorityLevels = decodeStrings(seniority)
	_ = json.Unmarshal(locations, &out.Locations)
	_ = json.Unmarshal(arrangements, &out.WorkArrangements)
	return out, nil
}

// Resume is the stored metadata of an uploaded CV. The file itself lives outside
// the database, in a private directory, referenced only by StoragePath.
type Resume struct {
	ID               uuid.UUID `json:"id"`
	UserID           uuid.UUID `json:"user_id"`
	OriginalFilename string    `json:"original_filename"`
	ContentType      string    `json:"content_type"`
	SizeBytes        int64     `json:"size_bytes"`
	SHA256           string    `json:"sha256"`
	Status           string    `json:"status"`
	ParseError       string    `json:"parse_error"`
	UploadedAt       time.Time `json:"uploaded_at"`
}

// CreateResume records an uploaded CV.
func (r *Repository) CreateResume(ctx context.Context, userID uuid.UUID, filename, contentType string, size int64, sha, storagePath string) (Resume, error) {
	var out Resume
	err := r.db.Pool.QueryRow(ctx, `
		insert into resumes (user_id, original_filename, content_type, size_bytes, sha256, storage_path, status)
		values ($1, $2, $3, $4, $5, $6, 'uploaded')
		returning id, user_id, original_filename, content_type, size_bytes, sha256, status, parse_error, uploaded_at`,
		userID, filename, contentType, size, sha, storagePath).
		Scan(&out.ID, &out.UserID, &out.OriginalFilename, &out.ContentType, &out.SizeBytes, &out.SHA256, &out.Status, &out.ParseError, &out.UploadedAt)
	if err != nil {
		return Resume{}, fmt.Errorf("creating resume: %w", err)
	}
	return out, nil
}

// SetResumeStatus records parse success or failure.
func (r *Repository) SetResumeStatus(ctx context.Context, userID, resumeID uuid.UUID, status, parseError string) error {
	_, err := r.db.Pool.Exec(ctx, `
		update resumes set status = $3, parse_error = $4 where id = $1 and user_id = $2`,
		resumeID, userID, status, parseError)
	if err != nil {
		return fmt.Errorf("updating resume status: %w", err)
	}
	return nil
}

// ListResumes returns the user's active CVs, newest first.
func (r *Repository) ListResumes(ctx context.Context, userID uuid.UUID) ([]Resume, error) {
	rows, err := r.db.Pool.Query(ctx, `
		select id, user_id, original_filename, content_type, size_bytes, sha256, status, parse_error, uploaded_at
		from resumes where user_id = $1 and deleted_at is null
		order by uploaded_at desc`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing resumes: %w", err)
	}
	defer rows.Close()
	out := []Resume{}
	for rows.Next() {
		var x Resume
		if err := rows.Scan(&x.ID, &x.UserID, &x.OriginalFilename, &x.ContentType, &x.SizeBytes, &x.SHA256, &x.Status, &x.ParseError, &x.UploadedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// GetResume returns a resume with its private storage path.
func (r *Repository) GetResume(ctx context.Context, userID, resumeID uuid.UUID) (Resume, string, error) {
	var x Resume
	var path string
	err := r.db.Pool.QueryRow(ctx, `
		select id, user_id, original_filename, content_type, size_bytes, sha256, status, parse_error, uploaded_at, storage_path
		from resumes where id = $1 and user_id = $2 and deleted_at is null`, resumeID, userID).
		Scan(&x.ID, &x.UserID, &x.OriginalFilename, &x.ContentType, &x.SizeBytes, &x.SHA256, &x.Status, &x.ParseError, &x.UploadedAt, &path)
	if errors.Is(err, pgx.ErrNoRows) {
		return Resume{}, "", ErrNotFound
	}
	if err != nil {
		return Resume{}, "", fmt.Errorf("loading resume: %w", err)
	}
	return x, path, nil
}

// DeleteResume soft-deletes the row and returns the storage path so the caller
// can remove the file from disk.
func (r *Repository) DeleteResume(ctx context.Context, userID, resumeID uuid.UUID) (string, error) {
	var path string
	err := r.db.Pool.QueryRow(ctx, `
		update resumes set deleted_at = now()
		where id = $1 and user_id = $2 and deleted_at is null
		returning storage_path`, resumeID, userID).Scan(&path)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("deleting resume: %w", err)
	}
	return path, nil
}

// SaveExtraction stores the machine draft for a resume.
func (r *Repository) SaveExtraction(ctx context.Context, resumeID, userID uuid.UUID, extractor, version string, draft resume.Draft) error {
	encoded, err := json.Marshal(draft)
	if err != nil {
		return fmt.Errorf("encoding draft: %w", err)
	}
	_, err = r.db.Pool.Exec(ctx, `
		insert into resume_extractions (resume_id, user_id, extractor, extractor_version, draft)
		values ($1, $2, $3, $4, $5)`, resumeID, userID, extractor, version, encoded)
	if err != nil {
		return fmt.Errorf("saving extraction: %w", err)
	}
	return nil
}

// LatestExtraction returns the most recent draft for a user (used to populate
// the review screen after a page reload).
func (r *Repository) LatestExtraction(ctx context.Context, userID uuid.UUID) (resume.Draft, *uuid.UUID, error) {
	var raw []byte
	var resumeID uuid.UUID
	err := r.db.Pool.QueryRow(ctx, `
		select draft, resume_id from resume_extractions
		where user_id = $1 order by created_at desc limit 1`, userID).Scan(&raw, &resumeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return resume.Draft{}, nil, nil
	}
	if err != nil {
		return resume.Draft{}, nil, fmt.Errorf("loading extraction: %w", err)
	}
	var draft resume.Draft
	if err := json.Unmarshal(raw, &draft); err != nil {
		return resume.Draft{}, nil, fmt.Errorf("decoding extraction: %w", err)
	}
	return draft, &resumeID, nil
}

func encodeStrings(in []string) []byte {
	if in == nil {
		in = []string{}
	}
	b, err := json.Marshal(in)
	if err != nil {
		return []byte("[]")
	}
	return b
}

func decodeStrings(raw []byte) []string {
	out := []string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func encodeJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("[]")
	}
	return b
}

func clampScore(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
