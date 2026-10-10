package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/valeriandwipurnomo/jobseeker/internal/auth"
	"github.com/valeriandwipurnomo/jobseeker/internal/candidate"
	"github.com/valeriandwipurnomo/jobseeker/internal/jobs"
	"github.com/valeriandwipurnomo/jobseeker/internal/matching"
	"github.com/valeriandwipurnomo/jobseeker/internal/platform/httpx"
	"github.com/valeriandwipurnomo/jobseeker/internal/resume"
	"github.com/valeriandwipurnomo/jobseeker/internal/search"
	"github.com/valeriandwipurnomo/jobseeker/internal/storage"
	"github.com/valeriandwipurnomo/jobseeker/internal/tracker"
)

// --- shared helpers --------------------------------------------------------

func (s *Server) user(r *http.Request) auth.User {
	u, ok := auth.UserFromContext(r.Context())
	if !ok {
		panic("api: handler registered without the auth middleware")
	}
	return u
}

// fail maps domain errors onto the correct HTTP status. Domain packages stay
// free of HTTP concepts; the translation lives here.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	switch {
	case errors.Is(err, jobs.ErrNotFound),
		errors.Is(err, candidate.ErrNotFound),
		errors.Is(err, search.ErrNotFound),
		errors.Is(err, tracker.ErrNotFound),
		errors.Is(err, resume.ErrInvalidDocument),
		errors.Is(err, storage.ErrInvalidDocumentPath):
		httpx.WriteError(w, r, s.deps.Logger, httpx.ErrNotFound)
	case errors.Is(err, search.ErrRunInProgress):
		httpx.WriteError(w, r, s.deps.Logger, &httpx.ValidationError{Message: "a search is already running for this account"})
	case errors.Is(err, resume.ErrUnsupportedFormat), errors.Is(err, resume.ErrNoTextExtracted):
		httpx.WriteError(w, r, s.deps.Logger, &httpx.ValidationError{Message: err.Error()})
	case errors.Is(err, tracker.ErrInvalidStatus):
		httpx.WriteError(w, r, s.deps.Logger, &httpx.ValidationError{Message: err.Error()})
	default:
		httpx.WriteError(w, r, s.deps.Logger, err)
	}
}

// orEmpty guarantees a JSON array where the contract promises one. A Go nil
// slice marshals to `null`, which crashes clients that follow the documented
// contract and call .length/.map on it.
func orEmpty[T any](in []T) []T {
	if in == nil {
		return []T{}
	}
	return in
}

func uuidParam(r *http.Request, name string) (uuid.UUID, error) {
	raw := r.PathValue(name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, httpx.NewValidationError(fmt.Sprintf("%s must be a UUID", name),
			httpx.FieldError{Field: name, Message: "not a valid UUID"})
	}
	return id, nil
}

func pagination(r *http.Request) (page, perPage int) {
	page = atoiDefault(r.URL.Query().Get("page"), 1)
	perPage = atoiDefault(r.URL.Query().Get("per_page"), 25)
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 25
	}
	return page, perPage
}

func atoiDefault(raw string, def int) int {
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}

func inSet(value string, allowed ...string) bool {
	for _, a := range allowed {
		if strings.EqualFold(value, a) {
			return true
		}
	}
	return false
}

// --- candidate profile -----------------------------------------------------

type profileBody struct {
	FullName        string   `json:"full_name"`
	Headline        string   `json:"headline"`
	CurrentTitle    string   `json:"current_title"`
	YearsExperience *float64 `json:"years_experience"`
	Seniority       string   `json:"seniority"`
	Skills          []string `json:"skills"`
	Industries      []string `json:"industries"`
	Education       []string `json:"education"`
	Certifications  []string `json:"certifications"`
	Languages       []string `json:"languages"`
	Projects        []string `json:"projects"`
	SourceResumeID  *string  `json:"source_resume_id"`
}

var seniorityLevels = []string{"intern", "junior", "mid", "senior", "lead", "principal", "unknown"}

func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	profile, exists, err := s.deps.Candidate.GetProfile(r.Context(), user.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"profile":              profile,
		"confirmed":            exists && profile.ConfirmedAt != nil,
		"confirmed_field_keys": confirmedFieldKeys(profile),
	})
}

func (s *Server) handlePutProfile(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	var body profileBody
	if err := httpx.DecodeJSON(w, r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	if body.Seniority == "" {
		body.Seniority = "unknown"
	}
	if !inSet(body.Seniority, seniorityLevels...) {
		httpx.WriteError(w, r, s.deps.Logger, httpx.NewValidationError(
			"seniority must be one of: "+strings.Join(seniorityLevels, ", "),
			httpx.FieldError{Field: "seniority", Message: "unsupported level"}))
		return
	}
	if body.YearsExperience != nil && (*body.YearsExperience < 0 || *body.YearsExperience > 60) {
		httpx.WriteError(w, r, s.deps.Logger, httpx.NewValidationError(
			"years_experience must be between 0 and 60",
			httpx.FieldError{Field: "years_experience", Message: "out of range"}))
		return
	}

	var sourceResumeID *uuid.UUID
	if body.SourceResumeID != nil && *body.SourceResumeID != "" {
		id, err := uuid.Parse(*body.SourceResumeID)
		if err != nil {
			httpx.WriteError(w, r, s.deps.Logger, httpx.NewValidationError("source_resume_id is not a UUID",
				httpx.FieldError{Field: "source_resume_id", Message: "not a valid UUID"}))
			return
		}
		sourceResumeID = &id
	}

	saved, err := s.deps.Candidate.SaveProfile(r.Context(), candidate.Profile{
		UserID:          user.ID,
		FullName:        strings.TrimSpace(body.FullName),
		Headline:        strings.TrimSpace(body.Headline),
		CurrentTitle:    strings.TrimSpace(body.CurrentTitle),
		YearsExperience: body.YearsExperience,
		Seniority:       strings.ToLower(body.Seniority),
		Skills:          cleanList(body.Skills),
		Industries:      cleanList(body.Industries),
		Education:       cleanList(body.Education),
		Certifications:  cleanList(body.Certifications),
		Languages:       cleanList(body.Languages),
		Projects:        cleanList(body.Projects),
		SourceResumeID:  sourceResumeID,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}

	// The profile drives every score, so confirming a change (typically the
	// fields just reviewed from an uploaded CV) must not leave the stored
	// matches describing the previous profile version. Best effort: a rescore
	// failure must never lose the save itself.
	rescored := 0
	if s.deps.Runner != nil {
		n, rerr := s.deps.Runner.Rescore(r.Context(), user.ID)
		if rerr != nil {
			s.deps.Logger.Warn("rescoring after profile save failed",
				"user_id", user.ID.String(), "error", rerr.Error())
		} else {
			rescored = n
		}
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"profile":              saved,
		"confirmed":            true,
		"confirmed_field_keys": confirmedFieldKeys(saved),
		"matches_rescored":     rescored,
	})
}

func confirmedFieldKeys(p candidate.Profile) []string {
	keys := []string{}
	if p.FullName != "" {
		keys = append(keys, "full_name")
	}
	if p.Headline != "" {
		keys = append(keys, "headline")
	}
	if p.CurrentTitle != "" {
		keys = append(keys, "current_title")
	}
	if p.YearsExperience != nil {
		keys = append(keys, "years_experience")
	}
	if p.Seniority != "" && p.Seniority != "unknown" {
		keys = append(keys, "seniority")
	}
	if len(p.Skills) > 0 {
		keys = append(keys, "skills")
	}
	if len(p.Industries) > 0 {
		keys = append(keys, "industries")
	}
	if len(p.Education) > 0 {
		keys = append(keys, "education")
	}
	if len(p.Certifications) > 0 {
		keys = append(keys, "certifications")
	}
	if len(p.Languages) > 0 {
		keys = append(keys, "languages")
	}
	if len(p.Projects) > 0 {
		keys = append(keys, "projects")
	}
	return keys
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, s := range in {
		t := strings.TrimSpace(s)
		if t == "" {
			continue
		}
		k := strings.ToLower(t)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, t)
	}
	return out
}

// --- resumes ---------------------------------------------------------------

func (s *Server) handleListResumes(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	list, err := s.deps.Candidate.ListResumes(r.Context(), user.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	draft, resumeID, err := s.deps.Candidate.LatestExtraction(r.Context(), user.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"resumes":         orEmpty(list),
		"latest_draft":    draft,
		"draft_resume_id": resumeID,
	})
}

func (s *Server) handleGetDraft(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	if _, err := uuidParam(r, "id"); err != nil {
		s.fail(w, r, err)
		return
	}
	draft, resumeID, err := s.deps.Candidate.LatestExtraction(r.Context(), user.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"draft": draft, "resume_id": resumeID})
}

// handleUploadResume accepts a CV, stores it privately, extracts a draft profile
// and returns both. A document that parses poorly is still stored, with the
// reason recorded, so the user can see it and act instead of guessing.
func (s *Server) handleUploadResume(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	cfg := s.deps.Config

	r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxUploadBytes+1024)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		s.fail(w, r, fmt.Errorf("%w: %v", httpx.ErrTooLarge, err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, r, s.deps.Logger, httpx.NewValidationError(
			"a file field named \"file\" is required",
			httpx.FieldError{Field: "file", Message: "missing"}))
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, cfg.MaxUploadBytes+1))
	if err != nil {
		s.fail(w, r, fmt.Errorf("%w: %v", httpx.ErrBadRequest, err))
		return
	}
	if int64(len(data)) > cfg.MaxUploadBytes {
		s.fail(w, r, fmt.Errorf("%w: file exceeds the %d byte limit", httpx.ErrTooLarge, cfg.MaxUploadBytes))
		return
	}
	if len(data) == 0 {
		httpx.WriteError(w, r, s.deps.Logger, httpx.NewValidationError("the uploaded file is empty",
			httpx.FieldError{Field: "file", Message: "empty"}))
		return
	}

	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	kind, err := resume.DetectKind(header.Filename, head)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	storagePath, err := storage.SaveDocument(cfg.DataDir, data)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		if kind == resume.KindPDF {
			contentType = "application/pdf"
		} else {
			contentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		}
	}

	record, err := s.deps.Candidate.CreateResume(r.Context(), user.ID,
		header.Filename, contentType, int64(len(data)), resume.Hash(data), storagePath)
	if err != nil {
		_ = storage.RemoveDocument(cfg.DataDir, storagePath)
		s.fail(w, r, err)
		return
	}

	text, parseErr := resume.ExtractText(kind, data)
	warnings := []string{}
	var draft resume.Draft

	if parseErr != nil {
		_ = s.deps.Candidate.SetResumeStatus(r.Context(), user.ID, record.ID, "failed", parseErr.Error())
		record.Status = "failed"
		record.ParseError = parseErr.Error()
		warnings = append(warnings, "The document was stored but no profile could be extracted: "+parseErr.Error())
	} else {
		draft = resume.ExtractProfile(text, time.Now())
		if err := s.deps.Candidate.SaveExtraction(r.Context(), record.ID, user.ID, "deterministic-v1", "1", draft); err != nil {
			s.fail(w, r, err)
			return
		}
		if err := s.deps.Candidate.SetResumeStatus(r.Context(), user.ID, record.ID, "parsed", ""); err != nil {
			s.fail(w, r, err)
			return
		}
		record.Status = "parsed"
		warnings = append(warnings, draft.Warnings...)
	}

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"resume":   record,
		"draft":    draft,
		"warnings": warnings,
	})
}

func (s *Server) handleDeleteResume(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	id, err := uuidParam(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	path, err := s.deps.Candidate.DeleteResume(r.Context(), user.ID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := storage.RemoveDocument(s.deps.Config.DataDir, path); err != nil {
		// The row is already hidden from the user; report the file problem in
		// the log rather than failing the request.
		s.deps.Logger.ErrorContext(r.Context(), "removing resume file failed", "error", err.Error())
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- preferences -----------------------------------------------------------

type preferencesBody struct {
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
}

var (
	allowedArrangements = []string{"remote", "onsite", "hybrid"}
	allowedEmployment   = []string{"permanent", "full_time", "contract", "part_time", "internship", "temporary"}
	allowedPeriods      = []string{"year", "month", "day", "hour"}
)

func (s *Server) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	prefs, exists, err := s.deps.Candidate.GetPreferences(r.Context(), user.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"preferences": prefs, "configured": exists})
}

func (s *Server) handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	var body preferencesBody
	if err := httpx.DecodeJSON(w, r, &body); err != nil {
		s.fail(w, r, err)
		return
	}

	var problems []httpx.FieldError
	for _, a := range body.WorkArrangements {
		if !inSet(a.Value, allowedArrangements...) {
			problems = append(problems, httpx.FieldError{Field: "work_arrangements", Message: "unsupported value " + a.Value})
		}
	}
	for _, e := range body.EmploymentTypes {
		if !inSet(e, allowedEmployment...) {
			problems = append(problems, httpx.FieldError{Field: "employment_types", Message: "unsupported value " + e})
		}
	}
	for _, sl := range body.SeniorityLevels {
		if !inSet(sl, seniorityLevels...) {
			problems = append(problems, httpx.FieldError{Field: "seniority_levels", Message: "unsupported value " + sl})
		}
	}
	if body.SalaryPeriod != "" && !inSet(body.SalaryPeriod, allowedPeriods...) {
		problems = append(problems, httpx.FieldError{Field: "salary_period", Message: "unsupported value " + body.SalaryPeriod})
	}
	if body.SalaryCurrency != "" && (len(body.SalaryCurrency) != 3 || strings.ToUpper(body.SalaryCurrency) != body.SalaryCurrency) {
		problems = append(problems, httpx.FieldError{Field: "salary_currency", Message: "must be a 3-letter ISO code such as USD"})
	}
	if body.MinSalary != nil && *body.MinSalary < 0 {
		problems = append(problems, httpx.FieldError{Field: "min_salary", Message: "must not be negative"})
	}
	if body.MinScore < 0 || body.MinScore > 100 {
		problems = append(problems, httpx.FieldError{Field: "min_score", Message: "must be between 0 and 100"})
	}
	if body.MinSalary != nil && body.SalaryCurrency == "" {
		problems = append(problems, httpx.FieldError{Field: "salary_currency", Message: "required when a minimum salary is set"})
	}
	if len(problems) > 0 {
		httpx.WriteError(w, r, s.deps.Logger, &httpx.ValidationError{Message: "invalid search configuration", Details: problems})
		return
	}

	saved, err := s.deps.Candidate.SavePreferences(r.Context(), candidate.Preferences{
		UserID:           user.ID,
		JobTitles:        cleanList(body.JobTitles),
		Keywords:         cleanList(body.Keywords),
		ExcludedKeywords: cleanList(body.ExcludedKeywords),
		Locations:        cleanLocations(body.Locations),
		WorkArrangements: body.WorkArrangements,
		EmploymentTypes:  cleanList(body.EmploymentTypes),
		SeniorityLevels:  cleanList(body.SeniorityLevels),
		MinSalary:        body.MinSalary,
		SalaryCurrency:   body.SalaryCurrency,
		SalaryPeriod:     body.SalaryPeriod,
		MinScore:         body.MinScore,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"preferences": saved, "configured": true})
}

func cleanLocations(in []matching.LocationPref) []matching.LocationPref {
	out := make([]matching.LocationPref, 0, len(in))
	for _, l := range in {
		l.Country = strings.TrimSpace(l.Country)
		l.City = strings.TrimSpace(l.City)
		l.Region = strings.TrimSpace(l.Region)
		if l.Country == "" && l.City == "" && l.Region == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

// --- schedule --------------------------------------------------------------

func (s *Server) handleGetSchedule(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	schedule, err := s.deps.Search.GetSchedule(r.Context(), user.ID, s.deps.Config.SchedulerDefaultCron, s.deps.Config.SchedulerDefaultTZ)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	next, err := search.NextRun(schedule.CronSpec, schedule.Timezone, time.Now())
	if err != nil {
		// A stored schedule that no longer parses must not break the endpoint;
		// report it instead of inventing a time.
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"schedule": schedule,
			"warning":  "stored cron spec could not be parsed: " + err.Error(),
		})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"schedule": schedule, "next_run_at": next})
}

func (s *Server) handlePutSchedule(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	var body struct {
		CronSpec string `json:"cron_spec"`
		Timezone string `json:"timezone"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := httpx.DecodeJSON(w, r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := search.NextRun(body.CronSpec, body.Timezone, time.Now()); err != nil {
		httpx.WriteError(w, r, s.deps.Logger, httpx.NewValidationError(err.Error(),
			httpx.FieldError{Field: "cron_spec", Message: "invalid cron spec or timezone"}))
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	// Create the row first so the update always has a target.
	if _, err := s.deps.Search.GetSchedule(r.Context(), user.ID, s.deps.Config.SchedulerDefaultCron, s.deps.Config.SchedulerDefaultTZ); err != nil {
		s.fail(w, r, err)
		return
	}
	schedule, err := s.deps.Search.UpdateSchedule(r.Context(), user.ID, body.CronSpec, body.Timezone, enabled)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	next, _ := search.NextRun(schedule.CronSpec, schedule.Timezone, time.Now())
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"schedule": schedule, "next_run_at": next})
}

// --- sources ---------------------------------------------------------------

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	list, err := s.deps.Search.ListSourceSettings(r.Context(), user.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	cfg := s.deps.Config
	for i := range list {
		switch list[i].ID {
		case "adzuna":
			list[i].KeyConfigured = cfg.AdzunaAppID != "" && cfg.AdzunaAppKey != ""
		case "usajobs":
			list[i].KeyConfigured = cfg.USAJobsAPIKey != "" && cfg.USAJobsEmail != ""
		case "jooble":
			list[i].KeyConfigured = cfg.JoobleAPIKey != ""
		default:
			list[i].KeyConfigured = true
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sources": orEmpty(list)})
}

func (s *Server) handlePatchSource(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	sourceID := r.PathValue("id")
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := httpx.DecodeJSON(w, r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	if body.Enabled == nil {
		httpx.WriteError(w, r, s.deps.Logger, httpx.NewValidationError("enabled is required",
			httpx.FieldError{Field: "enabled", Message: "required"}))
		return
	}
	known, err := s.knownSourceIDs(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !known[sourceID] {
		s.fail(w, r, jobs.ErrNotFound)
		return
	}
	if err := s.deps.Search.SetSourceEnabled(r.Context(), user.ID, sourceID, *body.Enabled); err != nil {
		s.fail(w, r, err)
		return
	}
	list, err := s.deps.Search.ListSourceSettings(r.Context(), user.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, src := range list {
		if src.ID == sourceID {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{"source": src})
			return
		}
	}
	s.fail(w, r, jobs.ErrNotFound)
}

// handleRescoreJobs recomputes every stored match for the caller against the
// current confirmed profile — the "my CV changed, re-score what I already have"
// action. A run only scores the jobs it fetched, so matches computed against an
// earlier profile version otherwise stay stale indefinitely.
func (s *Server) handleRescoreJobs(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	scored, err := s.deps.Runner.Rescore(r.Context(), user.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"rescored": scored})
}

func (s *Server) knownSourceIDs(r *http.Request) (map[string]bool, error) {
	rows, err := s.deps.Search.ListAllSourceIDs(r.Context())
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, id := range rows {
		out[id] = true
	}
	return out, nil
}

func (s *Server) handleListWatchlist(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	entries, err := s.deps.Search.ListWatchlist(r.Context(), user.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"watchlist": orEmpty(entries)})
}

// supportsWatchlist reports whether a source accepts a per-company watchlist.
//
// Derived from the adapter's own capabilities rather than a hardcoded list: a
// new ATS adapter becomes watchlist-capable by declaring RequiresWatchlist in
// its Caps(), with no change here.
func (s *Server) supportsWatchlist(sourceID string) bool {
	if s.deps.Registry == nil {
		return false
	}
	adapter, ok := s.deps.Registry.Get(sourceID)
	return ok && adapter.Caps().RequiresWatchlist
}

var boardSlugAllowed = func(slug string) bool {
	if len(slug) < 1 || len(slug) > 64 {
		return false
	}
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func (s *Server) handleAddWatchlist(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	sourceID := r.PathValue("id")
	var body struct {
		BoardSlug string `json:"board_slug"`
		Company   string `json:"company"`
	}
	if err := httpx.DecodeJSON(w, r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	if !boardSlugAllowed(body.BoardSlug) {
		httpx.WriteError(w, r, s.deps.Logger, httpx.NewValidationError(
			"board_slug may only contain letters, digits, hyphens and underscores",
			httpx.FieldError{Field: "board_slug", Message: "invalid characters"}))
		return
	}
	// The catalog row must exist before a watchlist can reference it: an adapter
	// can be compiled in while its master row is absent (e.g. after a source-set
	// decision), and inserting against a missing row is a foreign-key violation,
	// not a 500 the caller can act on.
	known, err := s.knownSourceIDs(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !known[sourceID] {
		s.fail(w, r, jobs.ErrNotFound)
		return
	}
	if !s.supportsWatchlist(sourceID) {
		httpx.WriteError(w, r, s.deps.Logger, httpx.NewValidationError(
			"watchlists are only supported for company-board (ATS) sources",
			httpx.FieldError{Field: "id", Message: "source does not accept a watchlist"}))
		return
	}
	entry, err := s.deps.Search.AddWatchlistEntry(r.Context(), user.ID, sourceID, body.BoardSlug, strings.TrimSpace(body.Company))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"entry": entry})
}

func (s *Server) handleDeleteWatchlist(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	if err := s.deps.Search.DeleteWatchlistEntry(r.Context(), user.ID, r.PathValue("id"), r.PathValue("slug")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- searches --------------------------------------------------------------

func (s *Server) handleRunSearch(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var run search.Run
	err := search.WithUserLock(user.ID, func() error {
		var runErr error
		run, runErr = s.deps.Runner.RunNow(r.Context(), user.ID, key)
		return runErr
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"run": run})
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	page, perPage := pagination(r)
	runs, total, err := s.deps.Search.ListRuns(r.Context(), user.ID, page, perPage)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"runs": orEmpty(runs), "total": total, "page": page, "per_page": perPage,
	})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	id, err := uuidParam(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	run, err := s.deps.Search.GetRun(r.Context(), user.ID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"run": run})
}

// --- jobs ------------------------------------------------------------------

var allowedSorts = []string{"score", "published_at", "salary", "company"}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	q := r.URL.Query()
	page, perPage := pagination(r)

	filter := jobs.JobFilter{
		Query:          strings.TrimSpace(q.Get("q")),
		Country:        strings.ToUpper(strings.TrimSpace(q.Get("country"))),
		City:           strings.TrimSpace(q.Get("city")),
		Arrangement:    strings.ToLower(strings.TrimSpace(q.Get("arrangement"))),
		EmploymentType: strings.ToLower(strings.TrimSpace(q.Get("employment_type"))),
		Seniority:      strings.ToLower(strings.TrimSpace(q.Get("seniority"))),
		Source:         strings.TrimSpace(q.Get("source")),
		Status:         strings.TrimSpace(q.Get("status")),
		MinScore:       atoiDefault(q.Get("min_score"), 0),
		Sort:           strings.TrimSpace(q.Get("sort")),
		Page:           page,
		PerPage:        perPage,
	}

	var problems []httpx.FieldError
	if filter.Arrangement != "" && !inSet(filter.Arrangement, allowedArrangements...) {
		problems = append(problems, httpx.FieldError{Field: "arrangement", Message: "unsupported value"})
	}
	if filter.EmploymentType != "" && !inSet(filter.EmploymentType, allowedEmployment...) {
		problems = append(problems, httpx.FieldError{Field: "employment_type", Message: "unsupported value"})
	}
	if filter.Seniority != "" && !inSet(filter.Seniority, seniorityLevels...) {
		problems = append(problems, httpx.FieldError{Field: "seniority", Message: "unsupported value"})
	}
	if filter.MinScore < 0 || filter.MinScore > 100 {
		problems = append(problems, httpx.FieldError{Field: "min_score", Message: "must be between 0 and 100"})
	}
	if filter.Sort != "" && !inSet(filter.Sort, allowedSorts...) {
		problems = append(problems, httpx.FieldError{Field: "sort", Message: "unsupported value"})
	}
	if len(problems) > 0 {
		httpx.WriteError(w, r, s.deps.Logger, &httpx.ValidationError{Message: "invalid filter", Details: problems})
		return
	}

	list, total, err := s.deps.Jobs.ListJobs(r.Context(), user.ID, filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"jobs": orEmpty(list), "total": total, "page": filter.Page, "per_page": filter.PerPage,
	})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	id, err := uuidParam(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	record, err := s.deps.Jobs.GetJob(r.Context(), user.ID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"job": record})
}

func (s *Server) handleSaveJob(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	id, err := uuidParam(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.deps.Jobs.GetJob(r.Context(), user.ID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.deps.Tracker.Save(r.Context(), user.ID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUnsaveJob(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	id, err := uuidParam(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.deps.Tracker.Unsave(r.Context(), user.ID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDismissJob(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	id, err := uuidParam(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.deps.Tracker.Dismiss(r.Context(), user.ID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListSaved(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	list, err := s.deps.Tracker.ListSaved(r.Context(), user.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"saved_jobs": orEmpty(list)})
}

// --- applications ----------------------------------------------------------

func (s *Server) handleListApplications(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	list, err := s.deps.Tracker.ListApplications(r.Context(), user.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"applications": orEmpty(list)})
}

func (s *Server) handlePutApplication(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	jobID, err := uuidParam(r, "jobID")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var body struct {
		Status     string  `json:"status"`
		AppliedAt  *string `json:"applied_at"`
		FollowUpAt *string `json:"follow_up_at"`
		Notes      string  `json:"notes"`
	}
	if err := httpx.DecodeJSON(w, r, &body); err != nil {
		s.fail(w, r, err)
		return
	}
	if !tracker.IsValidStatus(body.Status) {
		httpx.WriteError(w, r, s.deps.Logger, httpx.NewValidationError(
			"unsupported application status",
			httpx.FieldError{Field: "status", Message: "must be one of saved, applied, interviewing, rejected, accepted, withdrawn"}))
		return
	}
	if len([]rune(body.Notes)) > 4000 {
		httpx.WriteError(w, r, s.deps.Logger, httpx.NewValidationError("notes are too long",
			httpx.FieldError{Field: "notes", Message: "maximum 4000 characters"}))
		return
	}
	appliedAt, err := parseOptionalTime(body.AppliedAt)
	if err != nil {
		httpx.WriteError(w, r, s.deps.Logger, httpx.NewValidationError("applied_at must be an RFC3339 timestamp",
			httpx.FieldError{Field: "applied_at", Message: "invalid timestamp"}))
		return
	}
	followUpAt, err := parseOptionalTime(body.FollowUpAt)
	if err != nil {
		httpx.WriteError(w, r, s.deps.Logger, httpx.NewValidationError("follow_up_at must be an RFC3339 timestamp",
			httpx.FieldError{Field: "follow_up_at", Message: "invalid timestamp"}))
		return
	}

	app, err := s.deps.Tracker.UpsertApplication(r.Context(), user.ID, jobID, body.Status, appliedAt, followUpAt, strings.TrimSpace(body.Notes))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"application": app})
}

func parseOptionalTime(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*raw))
	if err != nil {
		return nil, err
	}
	utc := t.UTC()
	return &utc, nil
}

// --- dashboard -------------------------------------------------------------

func (s *Server) handleDashboardSummary(w http.ResponseWriter, r *http.Request) {
	user := s.user(r)
	ctx := r.Context()

	since := time.Now().Add(-7 * 24 * time.Hour)
	if last, err := s.deps.Search.LastRunStartedAt(ctx, user.ID); err != nil {
		s.fail(w, r, err)
		return
	} else if last != nil {
		since = *last
	}

	counts, err := s.deps.Jobs.DashboardCounts(ctx, user.ID, since, 70)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	schedule, err := s.deps.Search.GetSchedule(ctx, user.ID, s.deps.Config.SchedulerDefaultCron, s.deps.Config.SchedulerDefaultTZ)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	next, _ := search.NextRun(schedule.CronSpec, schedule.Timezone, time.Now())

	runs, _, err := s.deps.Search.ListRuns(ctx, user.ID, 1, 5)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	var lastRun any
	if len(runs) > 0 {
		lastRun = runs[0]
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"total_jobs":         counts.TotalJobs,
		"new_since_last_run": counts.NewSinceLastRun,
		"high_match_count":   counts.HighMatchCount,
		"unverified_count":   counts.UnverifiedCount,
		"last_run":           lastRun,
		"next_run_at":        next,
		"schedule":           schedule,
		"recent_runs":        orEmpty(runs),
	})
}
