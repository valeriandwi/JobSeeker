package sources

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// Himalayas fetches the keyless Himalayas remote-jobs feed.
type Himalayas struct {
	Base
	baseURL string
}

// NewHimalayas builds the Himalayas adapter.
func NewHimalayas(opts ...Option) *Himalayas {
	return &Himalayas{
		Base:    NewBase(opts...),
		baseURL: "https://himalayas.app/jobs/api",
	}
}

func (h *Himalayas) ID() string { return "himalayas" }

func (h *Himalayas) Caps() Caps {
	return Caps{
		SupportsVerification: true,
		ProvidesSalary:       true,
		ProvidesExpiry:       true,
		ProvidesSeniority:    true,
	}
}

type himalayasResponse struct {
	Jobs []himalayasJob `json:"jobs"`
}

type himalayasJob struct {
	GUID                 string   `json:"guid"`
	Title                string   `json:"title"`
	CompanyName          string   `json:"companyName"`
	Description          string   `json:"description"`
	Excerpt              string   `json:"excerpt"`
	LocationRestrictions []string `json:"locationRestrictions"`
	Categories           []string `json:"categories"`
	ParentCategories     []string `json:"parentCategories"`
	Seniority            []string `json:"seniority"`
	EmploymentType       string   `json:"employmentType"`
	PubDate              int64    `json:"pubDate"`
	ExpiryDate           int64    `json:"expiryDate"`
	ApplicationLink      string   `json:"applicationLink"`
	MinSalary            *float64 `json:"minSalary"`
	MaxSalary            *float64 `json:"maxSalary"`
	Currency             string   `json:"currency"`
	SalaryPeriod         string   `json:"salaryPeriod"`
}

func (h *Himalayas) Search(ctx context.Context, q Query) ([]RawListing, Report, error) {
	start := time.Now()
	rep := Report{SourceID: h.ID()}

	u := appendQuery(h.baseURL, "limit", strconv.Itoa(feedLimit(q.Limit)))
	var resp himalayasResponse
	if err := h.Fetcher.GetJSON(ctx, h.ID(), u, &resp); err != nil {
		rep.Status = "error"
		rep.Error = err.Error()
		rep.Duration = time.Since(start)
		return nil, rep, err
	}

	all := make([]RawListing, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		all = append(all, h.mapJob(j))
	}
	kept, rejected := filterAndLimit(all, q, true)
	rep.Fetched = len(all)
	rep.Accepted = len(kept)
	rep.Rejected = rejected
	rep.Status = "ok"
	rep.Duration = time.Since(start)
	return kept, rep, nil
}

func (h *Himalayas) mapJob(j himalayasJob) RawListing {
	loc := strings.Join(j.LocationRestrictions, ", ")
	rl := RawListing{
		SourceID:    h.ID(),
		SourceJobID: strings.TrimSpace(j.GUID),
		URL:         j.ApplicationLink,
		ApplyURL:    j.ApplicationLink,
		Title:       strings.TrimSpace(j.Title),
		Company:     strings.TrimSpace(j.CompanyName),
		Description: summarize(FirstNonEmpty(j.Description, j.Excerpt)),
		LocationRaw: loc,
		Tags:        DedupeStrings(append(append([]string(nil), j.Categories...), j.ParentCategories...)),
		Raw:         mustRaw(j),
	}
	rl.Verify(AttrTitle, AttrCompany, AttrDescription)
	if loc != "" {
		rl.Verify(AttrLocation)
	}

	// Himalayas only lists remote roles; the arrangement is stated by the source.
	if arr := NormalizeArrangement(loc, true); arr != "unknown" {
		rl.WorkArrangement = arr
		rl.Verify(AttrWorkArrangement)
	}
	if et := NormalizeEmploymentType(j.EmploymentType); et != "unknown" {
		rl.EmploymentType = et
		rl.Verify(AttrEmploymentType)
	}
	if len(j.Seniority) > 0 {
		if sen := NormalizeSeniority(strings.Join(j.Seniority, " ")); sen != "unknown" {
			rl.Seniority = sen
			rl.Verify(AttrSeniority)
		}
	}
	if j.MinSalary != nil || j.MaxSalary != nil {
		rl.SalaryMin = j.MinSalary
		rl.SalaryMax = j.MaxSalary
		rl.SalaryCurrency = strings.ToUpper(strings.TrimSpace(j.Currency))
		rl.SalaryPeriod = normalizeSalaryPeriod(j.SalaryPeriod)
		rl.Verify(AttrSalary)
	}
	if t := unixSeconds(j.PubDate); t != nil {
		rl.PublishedAt = t
		rl.Verify(AttrPublishedAt)
	}
	if t := unixSeconds(j.ExpiryDate); t != nil {
		rl.ExpiresAt = t
		rl.Verify(AttrExpiresAt)
	}
	return rl
}
