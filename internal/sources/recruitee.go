package sources

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Recruitee fetches public offers from a Recruitee company board.
//
// The offers endpoint is the public feed behind the company's own careers site
// (jobs.<company>.com), so it needs no key. Boards are per-company: this is a
// watchlist source and reports "skipped" when nothing is configured.
type Recruitee struct {
	Base
	baseURLTemplate string
}

// NewRecruitee builds the Recruitee adapter.
func NewRecruitee(opts ...Option) *Recruitee {
	return &Recruitee{
		Base:            NewBase(opts...),
		baseURLTemplate: "https://%s.recruitee.com/api/offers/",
	}
}

func (r *Recruitee) ID() string { return "recruitee" }

func (r *Recruitee) Caps() Caps {
	return Caps{
		SupportsVerification: true,
		ProvidesSalary:       true,
		ProvidesExpiry:       true,
		ProvidesSeniority:    true,
		RequiresWatchlist:    true,
	}
}

type recruiteeResponse struct {
	Offers []recruiteeOffer `json:"offers"`
}

type recruiteeOffer struct {
	ID           int64    `json:"id"`
	GUID         string   `json:"guid"`
	Title        string   `json:"title"`
	CompanyName  string   `json:"company_name"`
	Slug         string   `json:"slug"`
	Status       string   `json:"status"`
	Description  string   `json:"description"`
	Requirements string   `json:"requirements"`
	Location     string   `json:"location"`
	City         string   `json:"city"`
	StateName    string   `json:"state_name"`
	Country      string   `json:"country"`
	CountryCode  string   `json:"country_code"`
	Hybrid       bool     `json:"hybrid"`
	Remote       bool     `json:"remote"`
	OnSite       bool     `json:"on_site"`
	Employment   string   `json:"employment_type_code"`
	Experience   string   `json:"experience_code"`
	Category     string   `json:"category_code"`
	Tags         []string `json:"tags"`
	CareersURL   string   `json:"careers_url"`
	ApplyURL     string   `json:"careers_apply_url"`
	PublishedAt  string   `json:"published_at"`
	CloseAt      string   `json:"close_at"`
	Salary       *struct {
		Min      string `json:"min"`
		Max      string `json:"max"`
		Period   string `json:"period"`
		Currency string `json:"currency"`
	} `json:"salary"`
}

func (r *Recruitee) Search(ctx context.Context, q Query) ([]RawListing, Report, error) {
	start := time.Now()
	rep := Report{SourceID: r.ID()}

	boards := watchlistFor(q.Watchlist, r.ID())
	if len(boards) == 0 {
		rep.Status = "skipped"
		rep.Duration = time.Since(start)
		return nil, rep, nil
	}

	var all []RawListing
	rejected := 0
	failures := 0
	var lastErr error
	for _, board := range boards {
		boardURL := fmt.Sprintf(r.baseURLTemplate, url.PathEscape(board.BoardSlug))
		var resp recruiteeResponse
		if err := r.Fetcher.GetJSON(ctx, r.ID(), boardURL, &resp); err != nil {
			failures++
			lastErr = err
			continue
		}
		for _, o := range resp.Offers {
			// The feed carries draft and archived offers too; only published
			// ones are vacancies a candidate could act on.
			if o.Status != "" && !strings.EqualFold(o.Status, "published") {
				rejected++
				continue
			}
			all = append(all, r.mapOffer(o, board))
		}
	}

	kept, filtered := filterAndLimit(all, q, true)
	rep.Fetched = len(all)
	rep.Accepted = len(kept)
	rep.Rejected = rejected + filtered
	rep.Duration = time.Since(start)

	switch {
	case failures > 0 && failures == len(boards):
		rep.Status = "error"
		if lastErr != nil {
			rep.Error = lastErr.Error()
		}
		return nil, rep, lastErr
	case failures > 0:
		rep.Status = "partial"
		if lastErr != nil {
			rep.Error = lastErr.Error()
		}
		return kept, rep, nil
	default:
		rep.Status = "ok"
		return kept, rep, nil
	}
}

func (r *Recruitee) mapOffer(o recruiteeOffer, board Watchlist) RawListing {
	sourceID := strconv.FormatInt(o.ID, 10)
	if o.ID == 0 {
		sourceID = strings.TrimSpace(o.GUID)
	}

	rl := RawListing{
		SourceID:    r.ID(),
		SourceJobID: sourceID,
		URL:         strings.TrimSpace(o.CareersURL),
		ApplyURL:    FirstNonEmpty(o.ApplyURL, o.CareersURL),
		Title:       strings.TrimSpace(o.Title),
		Company:     FirstNonEmpty(o.CompanyName, board.Company),
		Description: summarize(FirstNonEmpty(o.Description, o.Requirements)),
		LocationRaw: strings.TrimSpace(o.Location),
		City:        strings.TrimSpace(o.City),
		Region:      strings.TrimSpace(o.StateName),
		Tags:        DedupeStrings(append(append([]string(nil), o.Tags...), o.Category)),
		Raw:         mustRaw(o),
	}
	rl.Verify(AttrTitle, AttrCompany)
	if rl.Description != "" {
		rl.Verify(AttrDescription)
	}
	if rl.LocationRaw != "" {
		rl.Verify(AttrLocation)
	}
	if c := strings.TrimSpace(o.CountryCode); len(c) == 2 {
		rl.Country = strings.ToUpper(c)
	}

	switch {
	case o.Remote:
		rl.WorkArrangement = "remote"
		rl.Verify(AttrWorkArrangement)
	case o.Hybrid:
		rl.WorkArrangement = "hybrid"
		rl.Verify(AttrWorkArrangement)
	case o.OnSite:
		rl.WorkArrangement = "onsite"
		rl.Verify(AttrWorkArrangement)
	default:
		if arr := NormalizeArrangement(o.Location, false); arr != "unknown" {
			rl.WorkArrangement = arr
			rl.Verify(AttrWorkArrangement)
		}
	}

	if et := NormalizeEmploymentType(o.Employment); et != "unknown" {
		rl.EmploymentType = et
		rl.Verify(AttrEmploymentType)
	}
	if sen := NormalizeSeniority(o.Experience); sen != "unknown" {
		rl.Seniority = sen
		rl.Verify(AttrSeniority)
	}
	if o.Salary != nil {
		rl.SalaryMin = parseAmountPtr(o.Salary.Min)
		rl.SalaryMax = parseAmountPtr(o.Salary.Max)
		if rl.SalaryMin != nil || rl.SalaryMax != nil {
			rl.SalaryCurrency = strings.ToUpper(strings.TrimSpace(o.Salary.Currency))
			rl.SalaryPeriod = normalizeSalaryPeriod(o.Salary.Period)
			rl.Verify(AttrSalary)
		}
	}
	if t := parseTimeAny(o.PublishedAt); t != nil {
		rl.PublishedAt = t
		rl.Verify(AttrPublishedAt)
	}
	if t := parseTimeAny(o.CloseAt); t != nil {
		rl.ExpiresAt = t
		rl.Verify(AttrExpiresAt)
	}
	return rl
}

// parseAmountPtr converts a source-formatted amount to a float, nil when the
// value is absent or unparseable (never a guessed number).
func parseAmountPtr(raw string) *float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &v
}
