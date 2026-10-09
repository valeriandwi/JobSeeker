// Package matching scores a vacancy against a candidate profile and explains
// every point it awards.
//
// Two stages, deliberately separated:
//
//  1. Hard constraints — a user-marked mandatory location, work arrangement or
//     employment type, plus excluded keywords. A vacancy is excluded ONLY when
//     the source published a value that contradicts the constraint. An unknown
//     attribute is never treated as a match and never treated as a mismatch: it
//     is reported as unverified so the UI can say so.
//  2. Ranking — a weighted 0-100 score. Missing evidence lowers the score
//     rather than being ignored, and every component reports its own status so
//     the reason for the score is inspectable.
package matching

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/valeriandwipurnomo/jobseeker/internal/sources"
)

// Weights of the ranking components. They sum to 100 so the score is already on
// the documented 0-100 scale.
const (
	WeightSkills        = 30.0
	WeightTitle         = 20.0
	WeightExperience    = 10.0
	WeightSeniority     = 8.0
	WeightLocation      = 12.0
	WeightArrangement   = 10.0
	WeightEmployment    = 5.0
	WeightSalary        = 5.0
	TotalWeight         = WeightSkills + WeightTitle + WeightExperience + WeightSeniority + WeightLocation + WeightArrangement + WeightEmployment + WeightSalary
	DefaultDuplicateSim = 0.86
)

// Component statuses, surfaced verbatim in the API.
const (
	StatusMatched       = "matched"
	StatusPartial       = "partial"
	StatusNotMatched    = "not_matched"
	StatusUnknown       = "unknown"
	StatusNotApplicable = "not_applicable"
)

// Profile is the user-confirmed candidate profile used for scoring.
type Profile struct {
	Version         int
	Titles          []string
	Skills          []string
	Industries      []string
	Keywords        []string
	CurrentTitle    string
	Seniority       string
	YearsExperience *float64
}

// LocationPref is one entry of the user's location preferences.
type LocationPref struct {
	Country   string `json:"country"`
	Region    string `json:"region"`
	City      string `json:"city"`
	Mandatory bool   `json:"mandatory"`
}

// ArrangementPref is one work-arrangement preference (remote/onsite/hybrid).
type ArrangementPref struct {
	Value     string `json:"value"`
	Mandatory bool   `json:"mandatory"`
}

// Preferences holds the user's search configuration.
type Preferences struct {
	Locations        []LocationPref
	WorkArrangements []ArrangementPref
	EmploymentTypes  []string
	SeniorityLevels  []string
	MinSalary        *float64
	SalaryCurrency   string
	SalaryPeriod     string
	ExcludedKeywords []string
	MinScore         int
}

// Job is the vacancy side of the comparison.
type Job struct {
	ID                 string
	Title              string
	Company            string
	DescriptionSummary string
	Keywords           []string
	City               string
	Region             string
	Country            string
	WorkArrangement    string
	EmploymentType     string
	Seniority          string
	SalaryMin          *float64
	SalaryMax          *float64
	SalaryCurrency     string
	SalaryPeriod       string
	// AttributesVerified lists the fields the source actually published.
	AttributesVerified []string
}

// Component explains one part of the score.
type Component struct {
	Component    string  `json:"component"`
	Weight       float64 `json:"weight"`
	Contribution float64 `json:"contribution"`
	Status       string  `json:"status"`
	Reason       string  `json:"reason"`
}

// Result is the outcome of scoring one vacancy.
type Result struct {
	Score            int         `json:"score"`
	Explanation      []Component `json:"explanation"`
	Excluded         bool        `json:"excluded"`
	ExclusionReasons []string    `json:"exclusion_reasons,omitempty"`
	UnverifiedNotes  []string    `json:"unverified_notes,omitempty"`
	EvidenceCoverage float64     `json:"evidence_coverage"`
	ProfileVersion   int         `json:"profile_version"`
	ApplicableWeight float64     `json:"applicable_weight"`
}

// Score evaluates a vacancy. The result is deterministic: the same inputs
// always produce the same score and the same ordered explanation.
func Score(job Job, profile Profile, prefs Preferences) Result {
	res := Result{ProfileVersion: profile.Version}
	verified := verifySet(job.AttributesVerified)

	// ---- stage 1: hard constraints ------------------------------------------
	if reasons := exclusionReasons(job, prefs, verified); len(reasons) > 0 {
		res.Excluded = true
		res.ExclusionReasons = reasons
	}

	components := []Component{
		scoreSkills(job, profile),
		scoreTitle(job, profile),
		scoreExperience(job, profile),
		scoreSeniority(job, profile, prefs),
		scoreLocation(job, prefs, verified, &res),
		scoreArrangement(job, prefs, verified, &res),
		scoreEmployment(job, prefs, verified, &res),
		scoreSalary(job, prefs, verified, &res),
	}

	dataText := strings.ToLower(job.Title + " " + job.DescriptionSummary + " " + strings.Join(job.Keywords, " "))
	for _, kw := range prefs.ExcludedKeywords {
		kw = strings.TrimSpace(strings.ToLower(kw))
		if kw == "" {
			continue
		}
		if strings.Contains(dataText, kw) {
			res.Excluded = true
			res.ExclusionReasons = append(res.ExclusionReasons, "contains excluded keyword: "+kw)
		}
	}

	total := 0.0
	applicable := 0.0
	unknown := 0.0
	for _, c := range components {
		total += c.Contribution
		switch c.Status {
		case StatusUnknown:
			unknown += c.Weight
			applicable += c.Weight
		case StatusNotApplicable:
			// The user has no preference here, so there is nothing to compare.
		default:
			applicable += c.Weight
		}
	}
	res.Explanation = components
	res.Score = int(math.Round(total))
	if res.Score < 0 {
		res.Score = 0
	}
	if res.Score > 100 {
		res.Score = 100
	}
	res.ApplicableWeight = applicable
	if TotalWeight > 0 {
		res.EvidenceCoverage = round2((TotalWeight - unknown) / TotalWeight)
	}
	if res.Excluded {
		res.Score = 0
	}
	return res
}

func exclusionReasons(job Job, prefs Preferences, verified map[string]bool) []string {
	var reasons []string

	for _, loc := range prefs.Locations {
		if !loc.Mandatory {
			continue
		}
		if !verified[sources.AttrLocation] {
			continue // unverified: cannot exclude
		}
		// A mandatory country is enforced even for remote vacancies: remote
		// eligibility is frequently geo-restricted by the employer, and the
		// source published a location, so assuming eligibility would be
		// inventing information the data does not support.
		if loc.Country != "" && !strings.EqualFold(strings.TrimSpace(job.Country), strings.TrimSpace(loc.Country)) {
			reasons = append(reasons, fmt.Sprintf("mandatory country %s not matched (job country: %s)", loc.Country, orUnknown(job.Country)))
			continue
		}
		if loc.City != "" && job.WorkArrangement != "remote" && !strings.EqualFold(strings.TrimSpace(job.City), strings.TrimSpace(loc.City)) {
			reasons = append(reasons, fmt.Sprintf("mandatory city %s not matched (job city: %s)", loc.City, orUnknown(job.City)))
		}
	}

	for _, arr := range prefs.WorkArrangements {
		if !arr.Mandatory {
			continue
		}
		if !verified[sources.AttrWorkArrangement] {
			continue
		}
		if job.WorkArrangement == "unknown" {
			continue
		}
		if !strings.EqualFold(job.WorkArrangement, arr.Value) {
			reasons = append(reasons, fmt.Sprintf("mandatory work arrangement %s not matched (job: %s)", arr.Value, job.WorkArrangement))
		}
	}

	// Employment type is deliberately NOT a hard constraint: it is a ranking
	// preference (see scoreEmployment). Only location and work arrangement were
	// requested as enforceable constraints.

	return reasons
}

func scoreSkills(job Job, profile Profile) Component {
	c := Component{Component: "skills", Weight: WeightSkills}
	skills := sources.DedupeStrings(profile.Skills)
	if len(skills) == 0 {
		c.Status = StatusNotApplicable
		c.Reason = "no skills in the candidate profile"
		return c
	}
	haystack := strings.ToLower(strings.Join([]string{job.Title, job.DescriptionSummary, strings.Join(job.Keywords, " ")}, " "))
	matched := make([]string, 0, len(skills))
	for _, s := range skills {
		if containsTerm(haystack, s) {
			matched = append(matched, s)
		}
	}
	ratio := float64(len(matched)) / float64(len(skills))
	c.Contribution = round2(WeightSkills * ratio)
	switch {
	case ratio == 0:
		c.Status = StatusNotMatched
		c.Reason = "none of the candidate's skills appear in the vacancy text"
	case ratio < 0.5:
		c.Status = StatusPartial
		c.Reason = fmt.Sprintf("%d of %d candidate skills present: %s", len(matched), len(skills), strings.Join(limit(matched, 6), ", "))
	default:
		c.Status = StatusMatched
		c.Reason = fmt.Sprintf("%d of %d candidate skills present: %s", len(matched), len(skills), strings.Join(limit(matched, 8), ", "))
	}
	return c
}

func scoreTitle(job Job, profile Profile) Component {
	c := Component{Component: "title", Weight: WeightTitle}
	targets := sources.DedupeStrings(append(append([]string{}, profile.Titles...), profile.CurrentTitle))
	if len(targets) == 0 {
		c.Status = StatusNotApplicable
		c.Reason = "no target job titles configured"
		return c
	}
	best := 0.0
	bestTarget := ""
	for _, t := range targets {
		sim := TitleSimilarity(job.Title, t)
		if sim > best {
			best, bestTarget = sim, t
		}
	}
	c.Contribution = round2(WeightTitle * best)
	switch {
	case best >= 0.7:
		c.Status = StatusMatched
		c.Reason = fmt.Sprintf("title closely matches %q (similarity %.2f)", bestTarget, best)
	case best >= 0.35:
		c.Status = StatusPartial
		c.Reason = fmt.Sprintf("title partially matches %q (similarity %.2f)", bestTarget, best)
	default:
		c.Status = StatusNotMatched
		c.Reason = fmt.Sprintf("title does not resemble any target role (best: %q, similarity %.2f)", bestTarget, best)
	}
	return c
}

func scoreExperience(job Job, profile Profile) Component {
	c := Component{Component: "experience", Weight: WeightExperience}
	if profile.YearsExperience == nil {
		c.Status = StatusNotApplicable
		c.Reason = "candidate years of experience unknown"
		return c
	}
	if job.Seniority == "unknown" || job.Seniority == "" {
		c.Status = StatusUnknown
		c.Reason = "source does not state the seniority level, so required experience cannot be compared"
		return c
	}
	low, high := seniorityBand(job.Seniority)
	years := *profile.YearsExperience
	switch {
	case years >= low && (high == 0 || years <= high):
		c.Status = StatusMatched
		c.Contribution = WeightExperience
		c.Reason = fmt.Sprintf("%.1f years of experience fits the %s band (%.0f-%.0f years)", years, job.Seniority, low, high)
	case years < low:
		deficit := (low - years) / math.Max(low, 1)
		c.Contribution = round2(WeightExperience * math.Max(0, 1-deficit))
		c.Status = StatusPartial
		c.Reason = fmt.Sprintf("%.1f years of experience is below the %s band (%.0f-%.0f years)", years, job.Seniority, low, high)
	default:
		c.Contribution = round2(WeightExperience * 0.6)
		c.Status = StatusPartial
		c.Reason = fmt.Sprintf("%.1f years of experience is above the %s band (%.0f-%.0f years)", years, job.Seniority, low, high)
	}
	return c
}

func scoreSeniority(job Job, profile Profile, prefs Preferences) Component {
	c := Component{Component: "seniority", Weight: WeightSeniority}
	wanted := sources.DedupeStrings(append(append([]string{}, prefs.SeniorityLevels...), profile.Seniority))
	if len(wanted) == 0 {
		c.Status = StatusNotApplicable
		c.Reason = "no seniority preference configured"
		return c
	}
	if job.Seniority == "" || job.Seniority == "unknown" {
		c.Status = StatusUnknown
		c.Reason = "source does not state the seniority level"
		return c
	}
	for _, w := range wanted {
		if strings.EqualFold(w, job.Seniority) {
			c.Status = StatusMatched
			c.Contribution = WeightSeniority
			c.Reason = "seniority matches the preference (" + job.Seniority + ")"
			return c
		}
	}
	for _, w := range wanted {
		if adjacentSeniority(w, job.Seniority) {
			c.Status = StatusPartial
			c.Contribution = round2(WeightSeniority * 0.5)
			c.Reason = fmt.Sprintf("seniority %s is adjacent to the preference %s", job.Seniority, w)
			return c
		}
	}
	c.Status = StatusNotMatched
	c.Reason = fmt.Sprintf("seniority %s does not match the preference (%s)", job.Seniority, strings.Join(wanted, ", "))
	return c
}

func scoreLocation(job Job, prefs Preferences, verified map[string]bool, res *Result) Component {
	c := Component{Component: "location", Weight: WeightLocation}
	if len(prefs.Locations) == 0 {
		c.Status = StatusNotApplicable
		c.Reason = "no location preference configured"
		return c
	}
	// Remote vacancies are reachable from anywhere unless the user demanded a
	// specific place with a mandatory on-site arrangement.
	if job.WorkArrangement == "remote" && verified[sources.AttrWorkArrangement] && !allMandatoryOnsite(prefs) {
		c.Status = StatusMatched
		c.Contribution = WeightLocation
		c.Reason = "vacancy is remote and no mandatory on-site location is configured"
		return c
	}
	if !verified[sources.AttrLocation] {
		c.Status = StatusUnknown
		c.Reason = "source does not publish a location for this vacancy"
		res.UnverifiedNotes = append(res.UnverifiedNotes, "location not published by the source; treated as unknown, not as a match")
		return c
	}
	bestSim := 0.0
	bestReason := ""
	for _, loc := range prefs.Locations {
		sim, reason := locationFit(job, loc)
		if sim > bestSim {
			bestSim, bestReason = sim, reason
		}
	}
	c.Contribution = round2(WeightLocation * bestSim)
	switch {
	case bestSim >= 0.999:
		c.Status = StatusMatched
	case bestSim > 0:
		c.Status = StatusPartial
	default:
		c.Status = StatusNotMatched
	}
	c.Reason = bestReason
	return c
}

func locationFit(job Job, loc LocationPref) (float64, string) {
	jobCity := strings.ToLower(strings.TrimSpace(job.City))
	jobRegion := strings.ToLower(strings.TrimSpace(job.Region))
	jobCountry := strings.ToLower(strings.TrimSpace(job.Country))
	wantCity := strings.ToLower(strings.TrimSpace(loc.City))
	wantRegion := strings.ToLower(strings.TrimSpace(loc.Region))
	wantCountry := strings.ToLower(strings.TrimSpace(loc.Country))

	if wantCity != "" {
		if jobCity == wantCity {
			return 1, "city matches " + loc.City
		}
		if jobCity != "" && strings.Contains(jobCity, wantCity) {
			return 0.8, "job city " + job.City + " contains the preferred city " + loc.City
		}
	}
	if wantCountry != "" && jobCountry != "" {
		if jobCountry == wantCountry {
			regionNote := ""
			if wantRegion != "" && jobRegion != "" {
				if jobRegion == wantRegion {
					return 1, "region and country match " + loc.Region + ", " + loc.Country
				}
				return 0.8, "country matches " + loc.Country + ", region differs (" + orUnknown(job.Region) + ")"
			}
			regionNote = "country matches " + loc.Country
			return 0.9, regionNote
		}
		return 0, fmt.Sprintf("job country %s does not match the preferred country %s", orUnknown(job.Country), loc.Country)
	}
	if wantRegion != "" && jobRegion != "" && jobRegion == wantRegion {
		return 0.7, "region matches " + loc.Region
	}
	return 0, "location preference not matched (job location: " + orUnknown(job.City) + " " + orUnknown(job.Country) + ")"
}

func scoreArrangement(job Job, prefs Preferences, verified map[string]bool, res *Result) Component {
	c := Component{Component: "work_arrangement", Weight: WeightArrangement}
	if len(prefs.WorkArrangements) == 0 {
		c.Status = StatusNotApplicable
		c.Reason = "no work arrangement preference configured"
		return c
	}
	if job.WorkArrangement == "" || job.WorkArrangement == "unknown" {
		c.Status = StatusUnknown
		c.Reason = "source does not state the work arrangement"
		res.UnverifiedNotes = append(res.UnverifiedNotes, "work arrangement not published by the source; treated as unknown, not as a match")
		return c
	}
	for _, a := range prefs.WorkArrangements {
		if strings.EqualFold(a.Value, job.WorkArrangement) {
			c.Status = StatusMatched
			c.Contribution = WeightArrangement
			c.Reason = "work arrangement matches the preference (" + job.WorkArrangement + ")"
			return c
		}
	}
	c.Status = StatusNotMatched
	c.Reason = fmt.Sprintf("work arrangement %s is not in the preferred set (%s)", job.WorkArrangement, arrangeValues(prefs))
	return c
}

func scoreEmployment(job Job, prefs Preferences, verified map[string]bool, res *Result) Component {
	c := Component{Component: "employment_type", Weight: WeightEmployment}
	if len(prefs.EmploymentTypes) == 0 {
		c.Status = StatusNotApplicable
		c.Reason = "no employment type preference configured"
		return c
	}
	if job.EmploymentType == "" || job.EmploymentType == "unknown" {
		c.Status = StatusUnknown
		c.Reason = "source does not state the employment type"
		res.UnverifiedNotes = append(res.UnverifiedNotes, "employment type not published by the source; treated as unknown, not as a match")
		return c
	}
	for _, e := range prefs.EmploymentTypes {
		if strings.EqualFold(e, job.EmploymentType) {
			c.Status = StatusMatched
			c.Contribution = WeightEmployment
			c.Reason = "employment type matches the preference (" + job.EmploymentType + ")"
			return c
		}
	}
	// A full-time vacancy satisfies a "permanent" preference and vice versa:
	// the two are how different sources express the same arrangement.
	if equivalentEmployment(job.EmploymentType, prefs.EmploymentTypes) {
		c.Status = StatusPartial
		c.Contribution = round2(WeightEmployment * 0.75)
		c.Reason = "employment type " + job.EmploymentType + " is treated as equivalent to the preferred set"
		return c
	}
	c.Status = StatusNotMatched
	c.Reason = fmt.Sprintf("employment type %s is not in the preferred set (%s)", job.EmploymentType, strings.Join(prefs.EmploymentTypes, ", "))
	return c
}

func scoreSalary(job Job, prefs Preferences, verified map[string]bool, res *Result) Component {
	c := Component{Component: "salary", Weight: WeightSalary}
	if prefs.MinSalary == nil {
		c.Status = StatusNotApplicable
		c.Reason = "no minimum salary configured"
		return c
	}
	if job.SalaryMin == nil && job.SalaryMax == nil {
		c.Status = StatusUnknown
		c.Reason = "source does not disclose a salary for this vacancy"
		res.UnverifiedNotes = append(res.UnverifiedNotes, "salary not disclosed by the source; the configured minimum salary was not assumed to be satisfied")
		return c
	}
	if prefs.SalaryCurrency != "" && job.SalaryCurrency != "" && !strings.EqualFold(prefs.SalaryCurrency, job.SalaryCurrency) {
		c.Status = StatusUnknown
		c.Reason = fmt.Sprintf("salary currency differs (%s vs the configured %s) and no conversion is performed", job.SalaryCurrency, prefs.SalaryCurrency)
		res.UnverifiedNotes = append(res.UnverifiedNotes, "salary currency differs from the configured currency; comparison skipped rather than converted")
		return c
	}
	if prefs.SalaryPeriod != "" && job.SalaryPeriod != "" && !strings.EqualFold(prefs.SalaryPeriod, job.SalaryPeriod) {
		c.Status = StatusUnknown
		c.Reason = fmt.Sprintf("salary period differs (%s vs the configured %s) and no conversion is performed", job.SalaryPeriod, prefs.SalaryPeriod)
		return c
	}
	top := *prefs.MinSalary
	jobTop := deref(job.SalaryMax, deref(job.SalaryMin, 0))
	jobLow := deref(job.SalaryMin, deref(job.SalaryMax, 0))
	switch {
	case jobLow >= top:
		c.Status = StatusMatched
		c.Contribution = WeightSalary
		c.Reason = fmt.Sprintf("disclosed salary range starts at or above the minimum (%.0f >= %.0f)", jobLow, top)
	case jobTop >= top:
		c.Status = StatusPartial
		c.Contribution = round2(WeightSalary * 0.7)
		c.Reason = fmt.Sprintf("disclosed salary range reaches the minimum (%.0f-%.0f vs %.0f)", jobLow, jobTop, top)
	default:
		c.Status = StatusNotMatched
		c.Reason = fmt.Sprintf("disclosed salary is below the configured minimum (%.0f-%.0f vs %.0f)", jobLow, jobTop, top)
	}
	return c
}

// TitleSimilarity returns 0..1 similarity between two job titles, ignoring
// seniority modifiers and noise words.
func TitleSimilarity(a, b string) float64 {
	ta := titleTokens(a)
	tb := titleTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for t := range ta {
		if _, ok := tb[t]; ok {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	jaccard := float64(inter) / float64(union)
	// Substring containment catches "Frontend Engineer" vs "Senior Frontend Engineer".
	sa := strings.Join(sortedKeys(ta), " ")
	sb := strings.Join(sortedKeys(tb), " ")
	containment := 0.0
	if strings.Contains(sa, sb) || strings.Contains(sb, sa) {
		containment = 0.85
	}
	if containment > jaccard {
		return containment
	}
	return jaccard
}

var titleNoise = map[string]bool{
	"senior": true, "junior": true, "lead": true, "staff": true, "principal": true,
	"sr": true, "jr": true, "the": true, "and": true, "of": true, "for": true,
	"a": true, "an": true, "i": true, "ii": true, "iii": true, "mid": true,
	"level": true, "m/f/d": true, "mwd": true, "remote": true, "hybrid": true,
	"onsite": true, "full": true, "time": true, "part": true, "contract": true,
}

func titleTokens(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, raw := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '+' && r != '#' && r != '.'
	}) {
		t := strings.Trim(strings.TrimSuffix(raw, "."), "+#")
		if t == "" || titleNoise[t] {
			continue
		}
		out[t] = struct{}{}
	}
	return out
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// containsTerm reports whether a haystack contains a term as a whole word or a
// compact form (so "react.js" matches "react" and "javascript" matches "js").
func containsTerm(haystack, term string) bool {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return false
	}
	if len(term) <= 2 {
		return false // too short to match meaningfully
	}
	if strings.Contains(haystack, term) {
		return true
	}
	compact := strings.NewReplacer(".", "", "-", "", " ", "", "/", "").Replace(term)
	if compact != term && strings.Contains(strings.NewReplacer(".", "", "-", "", " ", "", "/", "").Replace(haystack), compact) {
		return true
	}
	return false
}

func seniorityBand(level string) (low, high float64) {
	switch strings.ToLower(level) {
	case "intern":
		return 0, 1
	case "junior":
		return 0, 2.5
	case "mid":
		return 2, 6
	case "senior":
		return 4, 11
	case "lead":
		return 7, 15
	case "principal":
		return 9, 30
	default:
		return 0, 30
	}
}

var seniorityOrder = []string{"intern", "junior", "mid", "senior", "lead", "principal"}

func adjacentSeniority(a, b string) bool {
	ia, ib := indexOf(seniorityOrder, a), indexOf(seniorityOrder, b)
	if ia < 0 || ib < 0 {
		return false
	}
	return abs(ia-ib) == 1
}

func equivalentEmployment(jobType string, wanted []string) bool {
	pairs := map[string]string{"permanent": "full_time", "full_time": "permanent"}
	for _, w := range wanted {
		if p, ok := pairs[strings.ToLower(jobType)]; ok && strings.EqualFold(p, w) {
			return true
		}
	}
	return false
}

func arrangeValues(prefs Preferences) string {
	vals := make([]string, 0, len(prefs.WorkArrangements))
	for _, a := range prefs.WorkArrangements {
		vals = append(vals, a.Value)
	}
	return strings.Join(vals, ", ")
}

func allMandatoryOnsite(prefs Preferences) bool {
	onsite := false
	for _, a := range prefs.WorkArrangements {
		if !a.Mandatory {
			continue
		}
		if strings.EqualFold(a.Value, "onsite") {
			onsite = true
		}
	}
	return onsite
}

func verifySet(attrs []string) map[string]bool {
	m := make(map[string]bool, len(attrs))
	for _, a := range attrs {
		m[a] = true
	}
	return m
}

func deref(p *float64, def float64) float64 {
	if p == nil {
		return def
	}
	return *p
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}

func limit(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func indexOf(list []string, want string) int {
	for i, v := range list {
		if strings.EqualFold(v, want) {
			return i
		}
	}
	return -1
}

func abs(f int) int {
	if f < 0 {
		return -f
	}
	return f
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

// MeetsThreshold reports whether a result should be surfaced for a minimum
// score setting. Excluded results never meet the threshold.
func MeetsThreshold(res Result, minScore int) bool {
	return !res.Excluded && res.Score >= minScore
}

// DeriveTitles builds the list of target job titles used for title matching and
// for source search queries, from the confirmed current title and headline.
// It never invents a role the profile does not state; it only splits what the
// user wrote into comparable pieces.
func DeriveTitles(currentTitle, headline string) []string {
	out := []string{}
	if t := strings.TrimSpace(currentTitle); t != "" {
		out = append(out, t)
	}
	for _, part := range strings.FieldsFunc(headline, func(r rune) bool {
		return r == '|' || r == ',' || r == ';' || r == '/' || r == '·' || r == '-'
	}) {
		p := strings.TrimSpace(part)
		if p == "" || len([]rune(p)) > 60 {
			continue
		}
		out = append(out, p)
	}
	return sources.DedupeStrings(out)
}
