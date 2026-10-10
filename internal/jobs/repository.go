package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/valeriandwipurnomo/jobseeker/internal/matching"
	"github.com/valeriandwipurnomo/jobseeker/internal/sources"
	"github.com/valeriandwipurnomo/jobseeker/internal/storage"
)

// Repository is the persistence layer for canonical vacancies and listings.
type Repository struct {
	db *storage.DB
}

// NewRepository wires the repository to the connection pool.
func NewRepository(db *storage.DB) *Repository { return &Repository{db: db} }

// UpsertOutcome describes what happened to an incoming listing.
type UpsertOutcome string

// Upsert outcomes, counted per source on the Search History page.
const (
	OutcomeNew       UpsertOutcome = "new"
	OutcomeUpdated   UpsertOutcome = "updated"
	OutcomeDuplicate UpsertOutcome = "duplicate"
	OutcomeSkipped   UpsertOutcome = "skipped"
)

// UpsertInput is one normalized listing on its way into storage.
type UpsertInput struct {
	Listing  sources.RawListing
	Verified []string
	Now      time.Time
}

// UpsertResult reports the outcome and how a duplicate was decided.
type UpsertResult struct {
	JobID    uuid.UUID
	Outcome  UpsertOutcome
	Decision DuplicateDecision
}

// Candidate is an existing canonical vacancy considered during dedup.
type Candidate struct {
	ID        uuid.UUID
	Identity  Identity
	Status    string
	Verified  map[string]bool
	City      string
	Country   string
	ExpiresAt *time.Time
}

// UpsertListing stores a listing, linking it to an existing canonical vacancy
// when the duplicate policy says it is the same job and creating one otherwise.
//
// The whole operation runs in a transaction: a partially-linked listing would
// corrupt the dedup invariants that later runs depend on.
func (r *Repository) UpsertListing(ctx context.Context, in UpsertInput) (UpsertResult, error) {
	listing := in.Listing
	if strings.TrimSpace(listing.Title) == "" || strings.TrimSpace(listing.SourceID) == "" {
		return UpsertResult{Outcome: OutcomeSkipped, Decision: DuplicateDecision{Reason: "listing has no title or source"}}, nil
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	incoming := NewIdentity(
		listing.SourceID, listing.SourceJobID, listing.URL,
		listing.Title, listing.Company, firstNonEmpty(listing.City, listing.LocationRaw), listing.Country,
	)

	var out UpsertResult
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		// tier 1 + 2: exact matches enforced by unique constraints.
		if id, found, err := findListingJobTx(ctx, tx, listing.SourceID, listing.SourceJobID, incoming.URLHash); err != nil {
			return err
		} else if found {
			out = UpsertResult{JobID: id, Outcome: OutcomeUpdated, Decision: DuplicateDecision{Same: true, Tier: "exact", Similarity: 1, Reason: "already stored for this source"}}
			return updateListingTx(ctx, tx, id, listing, in.Verified, now)
		}

		// tier 3: fuzzy match against existing canonical vacancies.
		candidates, err := fuzzyCandidatesTx(ctx, tx, incoming, 200)
		if err != nil {
			return err
		}
		bestSim := -1.0
		var best *Candidate
		var bestDecision DuplicateDecision
		for i := range candidates {
			d := SameVacancy(incoming, candidates[i].Identity)
			if d.Same && d.Similarity > bestSim {
				bestSim = d.Similarity
				best = &candidates[i]
				bestDecision = d
			}
		}
		if best != nil {
			out = UpsertResult{JobID: best.ID, Outcome: OutcomeDuplicate, Decision: bestDecision}
			if err := mergeIntoJobTx(ctx, tx, best, listing, in.Verified, now); err != nil {
				return err
			}
			return insertListingTx(ctx, tx, best.ID, listing, now)
		}

		// No match: create the canonical vacancy.
		jobID, err := insertJobTx(ctx, tx, incoming, listing, in.Verified, now)
		if err != nil {
			return err
		}
		if err := insertListingTx(ctx, tx, jobID, listing, now); err != nil {
			return err
		}
		out = UpsertResult{JobID: jobID, Outcome: OutcomeNew, Decision: DuplicateDecision{Tier: "none", Reason: "no existing vacancy matched"}}
		return nil
	})
	if err != nil {
		return UpsertResult{}, err
	}
	return out, nil
}

func findListingJobTx(ctx context.Context, tx pgx.Tx, sourceID, sourceJobID, urlHash string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		select job_id from job_source_listings
		where source_id = $1 and (source_job_id = $2 or url_hash = $3)
		limit 1`, sourceID, sourceJobID, urlHash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("looking up existing listing: %w", err)
	}
	return id, true, nil
}

func updateListingTx(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, l sources.RawListing, verified []string, now time.Time) error {
	_, err := tx.Exec(ctx, `
		update job_source_listings
		set url = $3, apply_url = $4, raw_payload = $5, last_seen_at = $6,
		    last_status = 'active', is_active = true
		where job_id = $1 and source_id = $2`,
		jobID, l.SourceID, l.URL, nullIfEmpty(l.ApplyURL), rawJSON(l), now)
	if err != nil {
		return fmt.Errorf("updating listing: %w", err)
	}
	return touchJobTx(ctx, tx, jobID, l, verified, now)
}

func insertListingTx(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, l sources.RawListing, now time.Time) error {
	_, err := tx.Exec(ctx, `
		insert into job_source_listings
		    (job_id, source_id, source_job_id, url, url_hash, apply_url, raw_payload, first_seen_at, last_seen_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		on conflict (source_id, source_job_id) do update
		    set last_seen_at = excluded.last_seen_at, last_status = 'active', is_active = true`,
		jobID, l.SourceID, l.SourceJobID, l.URL, URLHash(l.URL), nullIfEmpty(l.ApplyURL), rawJSON(l), now)
	if err != nil {
		return fmt.Errorf("inserting listing: %w", err)
	}
	return nil
}

// touchJobTx propagates newly-published attributes onto the canonical vacancy.
// It never overwrites a value that is already verified, and it only fills a
// field when the incoming listing actually published it.
func touchJobTx(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, l sources.RawListing, verified []string, now time.Time) error {
	var currentVerified map[string]bool
	var raw []byte
	if err := tx.QueryRow(ctx, `select attributes_verified from jobs where id = $1`, jobID).Scan(&raw); err != nil {
		return fmt.Errorf("loading verified attributes: %w", err)
	}
	currentVerified = map[string]bool{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &currentVerified)
	}
	for _, v := range verified {
		currentVerified[v] = true
	}
	merged, _ := json.Marshal(currentVerified)

	_, err := tx.Exec(ctx, `
		update jobs set
		    work_arrangement = case when $2 then coalesce(nullif($3, ''), work_arrangement) else work_arrangement end,
		    employment_type  = case when $4 then coalesce(nullif($5, ''), employment_type) else employment_type end,
		    seniority        = case when $6 then coalesce(nullif($7, ''), seniority) else seniority end,
		    salary_min       = case when $8 then coalesce($9, salary_min) else salary_min end,
		    salary_max       = case when $8 then coalesce($10, salary_max) else salary_max end,
		    salary_currency  = case when $8 then coalesce(nullif($11, ''), salary_currency) else salary_currency end,
		    salary_period    = case when $8 then coalesce(nullif($12, ''), salary_period) else salary_period end,
		    expires_at       = case when $13 then coalesce($14, expires_at) else expires_at end,
		    description_summary = case when description_summary = '' then coalesce(nullif($15, ''), description_summary) else description_summary end,
		    published_at     = coalesce(published_at, $16),
		    attributes_verified = $17,
		    last_verified_at = $18,
		    verification_failures = 0,
		    status = case when status in ('expired', 'removed', 'unverified') then 'active' else status end,
		    updated_at = $18
		where id = $1`,
		jobID,
		hasAttr(verified, sources.AttrWorkArrangement), l.WorkArrangement,
		hasAttr(verified, sources.AttrEmploymentType), l.EmploymentType,
		hasAttr(verified, sources.AttrSeniority), l.Seniority,
		hasAttr(verified, sources.AttrSalary), l.SalaryMin, l.SalaryMax, l.SalaryCurrency, l.SalaryPeriod,
		hasAttr(verified, sources.AttrExpiresAt), l.ExpiresAt,
		l.Description, l.PublishedAt,
		merged, now)
	if err != nil {
		return fmt.Errorf("updating job attributes: %w", err)
	}
	return nil
}

func insertJobTx(ctx context.Context, tx pgx.Tx, identity Identity, l sources.RawListing, verified []string, now time.Time) (uuid.UUID, error) {
	attrs := map[string]bool{}
	for _, v := range verified {
		attrs[v] = true
	}
	merged, _ := json.Marshal(attrs)

	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		insert into jobs (
		    canonical_key, title, title_norm, company, company_norm,
		    location_city, location_region, location_country,
		    work_arrangement, employment_type, seniority,
		    salary_min, salary_max, salary_currency, salary_period,
		    description_summary, published_at, expires_at, status,
		    first_discovered_at, last_verified_at, attributes_verified
		) values (
		    $1, $2, $3, $4, $5,
		    $6, $7, $8,
		    $9, $10, $11,
		    $12, $13, $14, $15,
		    $16, $17, $18, 'new',
		    $19, $19, $20
		)
		on conflict (canonical_key) do update set updated_at = excluded.updated_at
		returning id`,
		CanonicalKey(l.Title, l.Company, l.City, l.Country),
		l.Title, identity.TitleNorm, l.Company, identity.CompanyNorm,
		nullIfEmpty(l.City), nullIfEmpty(l.Region), strings.ToUpper(strings.TrimSpace(l.Country)),
		orDefault(l.WorkArrangement, "unknown"), orDefault(l.EmploymentType, "unknown"), orDefault(l.Seniority, "unknown"),
		l.SalaryMin, l.SalaryMax, nullIfEmpty(l.SalaryCurrency), nullIfEmpty(l.SalaryPeriod),
		truncateRunes(l.Description, 4000), l.PublishedAt, l.ExpiresAt,
		now, merged).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("inserting job: %w", err)
	}
	return id, nil
}

func mergeIntoJobTx(ctx context.Context, tx pgx.Tx, existing *Candidate, l sources.RawListing, verified []string, now time.Time) error {
	return touchJobTx(ctx, tx, existing.ID, l, verified, now)
}

// fuzzyCandidatesTx narrows the comparison set with a trigram-indexed query.
// Postgres does the coarse filtering; the Go duplicate policy makes the verdict.
func fuzzyCandidatesTx(ctx context.Context, tx pgx.Tx, incoming Identity, limit int) ([]Candidate, error) {
	rows, err := tx.Query(ctx, `
		select id, title_norm, company_norm, location_city, location_country, status, expires_at
		from jobs
		where status <> 'removed'
		  and ( ($1 <> '' and company_norm = $1)
		     or ($2 <> '' and title_norm % $2)
		     or ($2 <> '' and title_norm = $2) )
		limit $3`, incoming.CompanyNorm, incoming.TitleNorm, limit)
	if err != nil {
		return nil, fmt.Errorf("querying duplicate candidates: %w", err)
	}
	defer rows.Close()

	var out []Candidate
	for rows.Next() {
		var c Candidate
		var city, country *string
		if err := rows.Scan(&c.ID, &c.Identity.TitleNorm, &c.Identity.CompanyNorm, &city, &country, &c.Status, &c.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scanning candidate: %w", err)
		}
		if city != nil {
			c.City = *city
			c.Identity.City = NormalizeText(*city)
		}
		if country != nil {
			c.Country = *country
			c.Identity.Country = strings.ToUpper(*country)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- reads -----------------------------------------------------------------

// JobFilter drives the Job Discovery list.
type JobFilter struct {
	Query          string
	Country        string
	City           string
	Arrangement    string
	EmploymentType string
	Seniority      string
	Source         string
	MinScore       int
	Status         string
	IncludeExpired bool
	// IncludeExcluded surfaces vacancies that violated a mandatory constraint.
	// Off by default: a vacancy the user's own hard rules rejected must not
	// quietly reappear in their list.
	IncludeExcluded bool
	Sort            string
	Page            int
	PerPage         int
}

// SourceRef is one portal listing attached to a vacancy.
type SourceRef struct {
	SourceID         string    `json:"source_id"`
	DisplayName      string    `json:"display_name"`
	URL              string    `json:"url"`
	ApplyURL         string    `json:"apply_url,omitempty"`
	AttributionText  string    `json:"attribution_text"`
	AttributionURL   string    `json:"attribution_url"`
	BacklinkRequired bool      `json:"backlink_required"`
	LastStatus       string    `json:"last_status"`
	FirstSeenAt      time.Time `json:"first_seen_at"`
	LastSeenAt       time.Time `json:"last_seen_at"`
}

// JobRecord is a vacancy as returned to the client.
type JobRecord struct {
	ID                 uuid.UUID            `json:"id"`
	Title              string               `json:"title"`
	Company            string               `json:"company"`
	City               string               `json:"location_city"`
	Region             string               `json:"location_region"`
	Country            string               `json:"location_country"`
	WorkArrangement    string               `json:"work_arrangement"`
	EmploymentType     string               `json:"employment_type"`
	Seniority          string               `json:"seniority"`
	SalaryMin          *float64             `json:"salary_min"`
	SalaryMax          *float64             `json:"salary_max"`
	SalaryCurrency     string               `json:"salary_currency"`
	SalaryPeriod       string               `json:"salary_period"`
	DescriptionSummary string               `json:"description_summary"`
	PublishedAt        *time.Time           `json:"published_at"`
	ExpiresAt          *time.Time           `json:"expires_at"`
	Status             string               `json:"status"`
	FirstDiscoveredAt  time.Time            `json:"first_discovered_at"`
	LastVerifiedAt     *time.Time           `json:"last_verified_at"`
	AttributesVerified []string             `json:"attributes_verified"`
	MatchScore         *int                 `json:"match_score"`
	MatchExplanation   []matching.Component `json:"match_explanation"`
	Excluded           bool                 `json:"excluded"`
	UnverifiedNotes    []string             `json:"unverified_notes,omitempty"`
	Sources            []SourceRef          `json:"sources"`
}

// ListJobs returns a filtered, sorted page of vacancies together with the total
// match count. Every filter is a bound parameter; sort columns are whitelisted.
func (r *Repository) ListJobs(ctx context.Context, userID uuid.UUID, f JobFilter) ([]JobRecord, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PerPage <= 0 || f.PerPage > 100 {
		f.PerPage = 25
	}

	args := []any{userID}
	where := []string{"true"}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.Replace(clause, "?", fmt.Sprintf("$%d", len(args)), 1))
	}

	if q := strings.TrimSpace(f.Query); q != "" {
		add("j.search_tsv @@ websearch_to_tsquery('english', ?)", q)
	}
	if f.Country != "" {
		add("upper(j.location_country) = upper(?)", f.Country)
	}
	if f.City != "" {
		add("lower(coalesce(j.location_city, '')) = lower(?)", f.City)
	}
	if f.Arrangement != "" {
		add("j.work_arrangement = ?", f.Arrangement)
	}
	if f.EmploymentType != "" {
		add("j.employment_type = ?", f.EmploymentType)
	}
	if f.Seniority != "" {
		add("j.seniority = ?", f.Seniority)
	}
	if f.Source != "" {
		add(`exists (select 1 from job_source_listings l where l.job_id = j.id and l.source_id = ?)`, f.Source)
	}
	if f.Status != "" {
		add("j.status = ?", f.Status)
	} else if !f.IncludeExpired {
		where = append(where, "j.status not in ('expired', 'removed')")
	}
	where = append(where, "not exists (select 1 from dismissed_jobs d where d.job_id = j.id and d.user_id = $1)")
	if !f.IncludeExcluded {
		where = append(where, "not coalesce(lm.hard_constraint_violation, false)")
	}
	if f.MinScore > 0 {
		add("coalesce(lm.score, 0) >= ?", f.MinScore)
	}

	// Newest discovery first is the default: the list is a "what's new" view, so
	// a freshly discovered vacancy should surface even when its score is lower
	// than older ones. Score ordering stays available via sort=score.
	orderBy := "order by j.first_discovered_at desc, coalesce(lm.score, 0) desc, j.published_at desc nulls last"
	switch f.Sort {
	case "first_discovered", "":
		// default
	case "score":
		orderBy = "order by coalesce(lm.score, 0) desc, j.published_at desc nulls last, j.first_discovered_at desc"
	case "published_at":
		orderBy = "order by j.published_at desc nulls last, coalesce(lm.score, 0) desc"
	case "salary":
		orderBy = "order by j.salary_max desc nulls last, coalesce(lm.score, 0) desc"
	case "company":
		orderBy = "order by j.company asc, coalesce(lm.score, 0) desc"
	}

	whereSQL := strings.Join(where, " and ")
	var total int
	countSQL := `
		with lm as (
		    select distinct on (job_id) job_id, score, hard_constraint_violation from job_matches
		    where user_id = $1 order by job_id, computed_at desc
		)
		select count(*) from jobs j left join lm on lm.job_id = j.id where ` + whereSQL
	if err := r.db.Pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting jobs: %w", err)
	}

	args = append(args, f.PerPage, (f.Page-1)*f.PerPage)
	limitIdx := len(args) - 1
	offsetIdx := len(args)
	listSQL := fmt.Sprintf(`
		with lm as (
		    select distinct on (job_id) job_id, score, explanation, hard_constraint_violation
		    from job_matches where user_id = $1 order by job_id, computed_at desc
		)
		select
		    j.id, j.title, j.company,
		    coalesce(j.location_city, ''), coalesce(j.location_region, ''), coalesce(j.location_country, ''),
		    j.work_arrangement, j.employment_type, j.seniority,
		    j.salary_min, j.salary_max, coalesce(j.salary_currency, ''), coalesce(j.salary_period, ''),
		    j.description_summary, j.published_at, j.expires_at, j.status,
		    j.first_discovered_at, j.last_verified_at, j.attributes_verified,
		    lm.score, lm.explanation, coalesce(lm.hard_constraint_violation, false),
		    coalesce((
		        select json_agg(json_build_object(
		            'source_id', l.source_id,
		            'display_name', s.display_name,
		            'url', l.url,
		            'apply_url', coalesce(l.apply_url, ''),
		            'attribution_text', s.attribution_text,
		            'attribution_url', s.attribution_url,
		            'backlink_required', s.backlink_required,
		            'last_status', l.last_status,
		            'first_seen_at', l.first_seen_at,
		            'last_seen_at', l.last_seen_at
		        ) order by l.first_seen_at)
		        from job_source_listings l
		        join job_sources s on s.id = l.source_id
		        where l.job_id = j.id
		    ), '[]'::json) as sources
		from jobs j
		left join lm on lm.job_id = j.id
		where %s
		%s
		limit $%d offset $%d`, whereSQL, orderBy, limitIdx, offsetIdx)

	rows, err := r.db.Pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing jobs: %w", err)
	}
	defer rows.Close()

	out := make([]JobRecord, 0, f.PerPage)
	for rows.Next() {
		rec, err := scanJob(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, rec)
	}
	return out, total, rows.Err()
}

// GetJob loads one vacancy with every portal listing attached.
func (r *Repository) GetJob(ctx context.Context, userID, jobID uuid.UUID) (JobRecord, error) {
	sql := `
		with lm as (
		    select distinct on (job_id) job_id, score, explanation, hard_constraint_violation
		    from job_matches where user_id = $1 order by job_id, computed_at desc
		)
		select
		    j.id, j.title, j.company,
		    coalesce(j.location_city, ''), coalesce(j.location_region, ''), coalesce(j.location_country, ''),
		    j.work_arrangement, j.employment_type, j.seniority,
		    j.salary_min, j.salary_max, coalesce(j.salary_currency, ''), coalesce(j.salary_period, ''),
		    j.description_summary, j.published_at, j.expires_at, j.status,
		    j.first_discovered_at, j.last_verified_at, j.attributes_verified,
		    lm.score, lm.explanation, coalesce(lm.hard_constraint_violation, false),
		    coalesce((
		        select json_agg(json_build_object(
		            'source_id', l.source_id,
		            'display_name', s.display_name,
		            'url', l.url,
		            'apply_url', coalesce(l.apply_url, ''),
		            'attribution_text', s.attribution_text,
		            'attribution_url', s.attribution_url,
		            'backlink_required', s.backlink_required,
		            'last_status', l.last_status,
		            'first_seen_at', l.first_seen_at,
		            'last_seen_at', l.last_seen_at
		        ) order by l.first_seen_at)
		        from job_source_listings l
		        join job_sources s on s.id = l.source_id
		        where l.job_id = j.id
		    ), '[]'::json) as sources
		from jobs j
		left join lm on lm.job_id = j.id
		where j.id = $2`

	rows, err := r.db.Pool.Query(ctx, sql, userID, jobID)
	if err != nil {
		return JobRecord{}, fmt.Errorf("loading job: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return JobRecord{}, ErrNotFound
	}
	rec, err := scanJob(rows)
	if err != nil {
		return JobRecord{}, err
	}
	return rec, rows.Err()
}

func scanJob(rows pgx.Rows) (JobRecord, error) {
	var rec JobRecord
	var explanationJSON, sourcesJSON, verifiedJSON []byte
	var score *int
	if err := rows.Scan(
		&rec.ID, &rec.Title, &rec.Company,
		&rec.City, &rec.Region, &rec.Country,
		&rec.WorkArrangement, &rec.EmploymentType, &rec.Seniority,
		&rec.SalaryMin, &rec.SalaryMax, &rec.SalaryCurrency, &rec.SalaryPeriod,
		&rec.DescriptionSummary, &rec.PublishedAt, &rec.ExpiresAt, &rec.Status,
		&rec.FirstDiscoveredAt, &rec.LastVerifiedAt, &verifiedJSON,
		&score, &explanationJSON, &rec.Excluded,
		&sourcesJSON,
	); err != nil {
		return JobRecord{}, fmt.Errorf("scanning job row: %w", err)
	}
	rec.MatchScore = score
	if len(explanationJSON) > 0 {
		_ = json.Unmarshal(explanationJSON, &rec.MatchExplanation)
	}
	if len(verifiedJSON) > 0 {
		var m map[string]bool
		if json.Unmarshal(verifiedJSON, &m) == nil {
			for k, v := range m {
				if v {
					rec.AttributesVerified = append(rec.AttributesVerified, k)
				}
			}
		}
	}
	if len(sourcesJSON) > 0 {
		_ = json.Unmarshal(sourcesJSON, &rec.Sources)
	}
	if rec.MatchExplanation == nil {
		rec.MatchExplanation = []matching.Component{}
	}
	if rec.Sources == nil {
		rec.Sources = []SourceRef{}
	}
	if rec.AttributesVerified == nil {
		rec.AttributesVerified = []string{}
	}
	return rec, nil
}

// UpsertMatch stores a scoring result for a (job, user, profile version) triple.
func (r *Repository) UpsertMatch(ctx context.Context, jobID, userID uuid.UUID, profileVersion int, res matching.Result) error {
	explanation, err := json.Marshal(res.Explanation)
	if err != nil {
		return fmt.Errorf("encoding explanation: %w", err)
	}
	_, err = r.db.Pool.Exec(ctx, `
		insert into job_matches (job_id, user_id, profile_version, score, explanation, hard_constraint_violation, computed_at)
		values ($1, $2, $3, $4, $5, $6, now())
		on conflict (job_id, user_id, profile_version) do update
		    set score = excluded.score,
		        explanation = excluded.explanation,
		        hard_constraint_violation = excluded.hard_constraint_violation,
		        computed_at = now()`,
		jobID, userID, profileVersion, res.Score, explanation, res.Excluded)
	if err != nil {
		return fmt.Errorf("upserting match: %w", err)
	}
	return nil
}

// Counts is the dashboard summary.
type Counts struct {
	TotalJobs       int
	NewSinceLastRun int
	HighMatchCount  int
	UnverifiedCount int
}

// DashboardCounts computes the numbers shown on the dashboard.
func (r *Repository) DashboardCounts(ctx context.Context, userID uuid.UUID, since time.Time, highMatchMin int) (Counts, error) {
	var c Counts
	err := r.db.Pool.QueryRow(ctx, `
		with lm as (
		    select distinct on (job_id) job_id, score from job_matches
		    where user_id = $1 order by job_id, computed_at desc
		)
		select
		    count(*) filter (where j.status not in ('expired', 'removed')),
		    count(*) filter (where j.first_discovered_at >= $2),
		    count(*) filter (where coalesce(lm.score, 0) >= $3 and j.status not in ('expired', 'removed')),
		    count(*) filter (where j.status = 'unverified')
		from jobs j left join lm on lm.job_id = j.id`,
		userID, since, highMatchMin).Scan(&c.TotalJobs, &c.NewSinceLastRun, &c.HighMatchCount, &c.UnverifiedCount)
	if err != nil {
		return Counts{}, fmt.Errorf("computing dashboard counts: %w", err)
	}
	return c, nil
}

// ApplyVerificationUpdates records a verification sweep: listings that came back
// active, and listings that did not (which may lead to expiry under policy).
// An empty active set must be passed as an empty array, not NULL: `x = any(NULL)`
// is NULL, which would silently skip every row and leave stale listings looking
// active forever.
func (r *Repository) ApplyVerificationUpdates(ctx context.Context, sourceID string, activeSourceJobIDs []string, now time.Time) (verified, missing int, err error) {
	ids := activeSourceJobIDs
	if ids == nil {
		ids = []string{}
	}

	err = r.db.WithTx(ctx, func(tx pgx.Tx) error {
		tag, uerr := tx.Exec(ctx, `
			update job_source_listings
			set last_status = 'active', is_active = true, last_seen_at = $2
			where source_id = $1 and source_job_id = any($3)`,
			sourceID, now, ids)
		if uerr != nil {
			return fmt.Errorf("marking listings active: %w", uerr)
		}
		verified = int(tag.RowsAffected())

		// Listings absent from this sweep become unverified, and every affected
		// vacancy accumulates a failure count. A vacancy is never expired here:
		// that decision belongs to ExpireVacancies and its grace window.
		tag, uerr = tx.Exec(ctx, `
			with missed as (
			    update job_source_listings
			    set last_status = 'unverified', is_active = false
			    where source_id = $1 and not (source_job_id = any($2)) and last_status = 'active'
			    returning job_id
			), bumped as (
			    update jobs j
			    set verification_failures = j.verification_failures + 1, updated_at = $3
			    where j.id in (select job_id from missed)
			    returning j.id
			)
			select count(*) from missed`,
			sourceID, ids, now)
		if uerr != nil {
			return fmt.Errorf("marking listings unverified: %w", uerr)
		}
		missing = int(tag.RowsAffected())
		return nil
	})
	return verified, missing, err
}

// ExpireVacancies applies the lifecycle policy to listings that have gone
// missing repeatedly, and to vacancies whose published deadline has passed.
func (r *Repository) ExpireVacancies(ctx context.Context, now time.Time, policy ExpiryPolicy) (int, error) {
	sql := `
		update jobs j set status = 'expired', updated_at = $1
		where j.status not in ('expired', 'removed', 'saved', 'applied', 'interviewing')
		  and (
		      (j.expires_at is not null and j.expires_at < $1)
		      or exists (
		          select 1 from job_source_listings l
		          where l.job_id = j.id
		          group by l.job_id
		          having count(*) filter (where l.last_status = 'active') = 0
		             and max(l.last_seen_at) < $1 - $2::interval
		      )
		  )`
	interval := fmt.Sprintf("%d seconds", int(policy.GraceWindow.Seconds()))
	tag, err := r.db.Pool.Exec(ctx, sql, now, interval)
	if err != nil {
		return 0, fmt.Errorf("expiring vacancies: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ErrNotFound reports a missing vacancy.
var ErrNotFound = errors.New("job not found")

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func hasAttr(attrs []string, key string) bool {
	for _, a := range attrs {
		if a == key {
			return true
		}
	}
	return false
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func rawJSON(l sources.RawListing) []byte {
	if len(l.Raw) == 0 {
		return []byte("{}")
	}
	return []byte(l.Raw)
}
