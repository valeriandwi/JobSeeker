package sources

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Kalibrr fetches the public Kalibrr job-board JSON feed.
//
// ACCESS DECISION (operator): Kalibrr's Terms of Use require written permission
// to reuse their materials ("copy, distribute ... or disseminate in any manner
// ... materials from our website or Application without written permission from
// Kalibrr") and name "automated extraction, scraping" under Account Monitoring
// and Acceptable Use. This adapter was requested explicitly by the operator with
// that in view; see docs/sources.md and the job_sources row notes. Anyone
// re-enabling this source takes that decision knowingly.
//
// Verified behaviour of the endpoint (see docs/sources.md):
//   - `text` performs a server-side keyword search ("Software Engineer" -> 272).
//   - `location` filters by place name ("Jakarta" -> 665).
//   - `country` is honoured ONLY for Indonesia and Philippines. Any other value
//     is ignored and the FULL feed is returned, so unsupported values are never
//     sent: doing so would silently mislabel listings.
//   - Coverage is Indonesia and the Philippines only; Singapore and Malaysia
//     have no listings at all.
type Kalibrr struct {
	Base
	baseURL string
}

// NewKalibrr builds the Kalibrr adapter.
func NewKalibrr(opts ...Option) *Kalibrr {
	return &Kalibrr{
		Base:    NewBase(opts...),
		baseURL: "https://www.kalibrr.com/kjs/job_board/search",
	}
}

func (k *Kalibrr) ID() string { return "kalibrr" }

func (k *Kalibrr) Caps() Caps {
	return Caps{
		SupportsVerification: true,
		ProvidesSalary:       true,
		ProvidesExpiry:       true,
		// Seniority is NOT mapped: the feed exposes work_experience as an
		// undocumented numeric code (100/200/300/400/500). Guessing a band from
		// it would invent data, so it stays unknown.
		ProvidesSeniority: false,
	}
}

type kalibrrResponse struct {
	Count      int          `json:"count"`
	SearchText string       `json:"search_text"`
	Jobs       []kalibrrJob `json:"jobs"`
}

type kalibrrJob struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	CompanyName string `json:"company_name"`
	Company     struct {
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"company"`
	Description    string `json:"description"`
	Qualifications string `json:"qualifications"`
	Tenure         string `json:"tenure"`
	Visibility     string `json:"visibility"`
	GoogleLocation struct {
		AddressComponents struct {
			AddressLine1 string `json:"address_line_1"`
			City         string `json:"city"`
			Region       string `json:"region"`
			Country      string `json:"country"`
		} `json:"address_components"`
	} `json:"google_location"`
	IsWorkFromHome bool   `json:"is_work_from_home"`
	IsHybrid       bool   `json:"is_hybrid"`
	BaseSalary     *int64 `json:"base_salary"`
	MaximumSalary  *int64 `json:"maximum_salary"`
	SalaryCurrency string `json:"salary_currency"`
	SalaryInterval string `json:"salary_interval"`
	SalaryShown    bool   `json:"salary_shown"`
	ActivationDate string `json:"activation_date"`
	EndDate        string `json:"application_end_date"`
	JobSkills      []struct {
		SdsSkill struct {
			Name string `json:"name"`
		} `json:"sds_skill"`
	} `json:"job_sds_skills"`
}

// kalibrrCountries maps the country names the feed publishes to ISO alpha-2.
// Only names verified against the live feed are listed; anything else is left
// unset rather than guessed.
var kalibrrCountries = map[string]string{
	"indonesia":     "ID",
	"philippines":   "PH",
	"singapore":     "SG",
	"malaysia":      "MY",
	"united states": "US",
}

// kalibrrMarkets maps an ISO alpha-2 preference onto the country values the
// feed actually honours. Everything else is not a Kalibrr market.
var kalibrrMarkets = map[string]string{
	"ID": "Indonesia",
	"PH": "Philippines",
}

// kalibrrVerifiedCities lists the place names the feed demonstrably filters by
// (probed: count < the unfiltered total). The API SILENTLY IGNORES unknown
// values and returns the entire marketplace instead — "location=Singapore"
// answers 1,514 listings, every one of them Indonesian — so a place that is not
// in this list must never be forwarded.
var kalibrrVerifiedCities = map[string]string{
	"jakarta":         "Jakarta",
	"jakarta pusat":   "Jakarta Pusat",
	"jakarta selatan": "Jakarta Selatan",
	"bandung":         "Bandung",
	"bekasi":          "Bekasi",
	"denpasar":        "Denpasar",
	"makassar":        "Makassar",
	"medan":           "Medan",
	"semarang":        "Semarang",
	"surabaya":        "Surabaya",
	"tangerang":       "Tangerang",
	"yogyakarta":      "Yogyakarta",
	"cebu":            "Cebu",
	"davao":           "Davao",
	"makati":          "Makati",
	"manila":          "Manila",
	"quezon city":     "Quezon City",
	"taguig":          "Taguig",
}

// kalibrrCountryParam maps an ISO alpha-2 preference onto the only two country
// values the API honours. Anything else returns an empty string, so the
// parameter is omitted and the caller's client-side filters still apply.
func kalibrrCountryParam(iso string) string {
	return kalibrrMarkets[strings.ToUpper(strings.TrimSpace(iso))]
}

func (k *Kalibrr) Search(ctx context.Context, q Query) ([]RawListing, Report, error) {
	start := time.Now()
	rep := Report{SourceID: k.ID()}

	want := feedLimit(q.Limit)
	pageSize := 100
	if want < pageSize {
		pageSize = want
	}

	// Server-side narrowing: one text term (the feed takes a single string) and,
	// when we have one, a place name.
	text := strings.TrimSpace(strings.Join(q.Titles, " "))
	if text == "" {
		text = strings.TrimSpace(strings.Join(q.Keywords, " "))
	}

	// A requested market Kalibrr does not serve cannot be answered truthfully:
	// the feed ignores the country value and returns every marketplace. Report
	// that instead of filing Indonesian vacancies under a Singapore search.
	if iso := strings.ToUpper(strings.TrimSpace(q.Country)); iso != "" {
		if _, ok := kalibrrMarkets[iso]; !ok {
			rep.Status = "skipped"
			rep.Error = fmt.Sprintf(
				"kalibrr covers Indonesia and the Philippines only (requested country %s): the feed ignores unsupported locations and returns the whole marketplace, so no trustworthy result exists",
				iso)
			rep.Duration = time.Since(start)
			return nil, rep, nil
		}
	}

	var all []RawListing
	for page := 0; page < maxPages; page++ {
		if q.Limit > 0 && len(all) >= q.Limit {
			break
		}
		u := appendQuery(k.baseURL, "limit", strconv.Itoa(pageSize))
		u = appendQuery(u, "offset", strconv.Itoa(page*pageSize))
		if text != "" {
			u = appendQuery(u, "text", url.QueryEscape(text))
		}
		// Only verified place names are forwarded; an unverified one would be
		// silently ignored and return the full marketplace.
		if city, ok := kalibrrVerifiedCities[strings.ToLower(strings.TrimSpace(q.City))]; ok {
			u = appendQuery(u, "location", url.QueryEscape(city))
		}
		if country := kalibrrCountryParam(q.Country); country != "" {
			u = appendQuery(u, "country", url.QueryEscape(country))
		}

		var resp kalibrrResponse
		if err := k.Fetcher.GetJSON(ctx, k.ID(), u, &resp); err != nil {
			rep.Status = "error"
			rep.Error = err.Error()
			rep.Duration = time.Since(start)
			return nil, rep, err
		}
		if len(resp.Jobs) == 0 {
			break
		}
		for _, j := range resp.Jobs {
			if j.Visibility != "" && !strings.EqualFold(j.Visibility, "public") {
				continue
			}
			all = append(all, k.mapJob(j))
		}
		if len(resp.Jobs) < pageSize || len(all) >= resp.Count {
			break
		}
	}

	kept, rejected := filterAndLimit(all, q, true)
	rep.Fetched = len(all)
	rep.Accepted = len(kept)
	rep.Rejected = rejected
	rep.Status = "ok"
	rep.Duration = time.Since(start)
	return kept, rep, nil
}

func (k *Kalibrr) mapJob(j kalibrrJob) RawListing {
	ac := j.GoogleLocation.AddressComponents
	locationRaw := strings.Join(cleanNonEmpty([]string{ac.City, ac.Region, ac.Country}), ", ")

	tags := make([]string, 0, len(j.JobSkills))
	for _, s := range j.JobSkills {
		tags = append(tags, s.SdsSkill.Name)
	}

	rl := RawListing{
		SourceID:    k.ID(),
		SourceJobID: strconv.FormatInt(j.ID, 10),
		URL: fmt.Sprintf("https://www.kalibrr.com/c/%s/jobs/%d/%s",
			url.PathEscape(j.Company.Code), j.ID, url.PathEscape(j.Slug)),
		Title:       strings.TrimSpace(j.Name),
		Company:     FirstNonEmpty(strings.TrimSpace(j.CompanyName), strings.TrimSpace(j.Company.Name)),
		Description: summarize(FirstNonEmpty(j.Description, "") + "\n\n" + FirstNonEmpty(j.Qualifications, "")),
		LocationRaw: locationRaw,
		City:        strings.TrimSpace(ac.City),
		Region:      strings.TrimSpace(ac.Region),
		Tags:        DedupeStrings(tags),
		Raw:         mustRaw(j),
	}
	rl.ApplyURL = rl.URL
	rl.Verify(AttrTitle, AttrCompany)
	if rl.Description != "" {
		rl.Verify(AttrDescription)
	}
	if locationRaw != "" {
		rl.Verify(AttrLocation)
	}
	if iso, ok := kalibrrCountries[strings.ToLower(strings.TrimSpace(ac.Country))]; ok {
		rl.Country = iso
	}

	switch {
	case j.IsHybrid:
		rl.WorkArrangement = "hybrid"
		rl.Verify(AttrWorkArrangement)
	case j.IsWorkFromHome:
		rl.WorkArrangement = "remote"
		rl.Verify(AttrWorkArrangement)
	default:
		if arr := NormalizeArrangement(locationRaw, false); arr != "unknown" {
			rl.WorkArrangement = arr
			rl.Verify(AttrWorkArrangement)
		}
	}

	if et := NormalizeEmploymentType(j.Tenure); et != "unknown" {
		rl.EmploymentType = et
		rl.Verify(AttrEmploymentType)
	}

	// Amounts are only claimed when the feed actually published them.
	if j.BaseSalary != nil || j.MaximumSalary != nil {
		if j.BaseSalary != nil {
			v := float64(*j.BaseSalary)
			rl.SalaryMin = &v
		}
		if j.MaximumSalary != nil {
			v := float64(*j.MaximumSalary)
			rl.SalaryMax = &v
		}
		rl.SalaryCurrency = strings.ToUpper(strings.TrimSpace(j.SalaryCurrency))
		rl.SalaryPeriod = normalizeSalaryPeriod(j.SalaryInterval)
		rl.Verify(AttrSalary)
	}

	if t := parseTimeAny(j.ActivationDate); t != nil {
		rl.PublishedAt = t
		rl.Verify(AttrPublishedAt)
	}
	if t := parseTimeAny(j.EndDate); t != nil {
		rl.ExpiresAt = t
		rl.Verify(AttrExpiresAt)
	}
	return rl
}
