package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Lever fetches job postings from the Lever ATS (watchlist source).
type Lever struct {
	Base
	baseURL string
}

// NewLever builds the Lever adapter.
func NewLever(opts ...Option) *Lever {
	return &Lever{
		Base:    NewBase(opts...),
		baseURL: "https://api.lever.co/v0/postings",
	}
}

func (l *Lever) ID() string { return "lever" }

func (l *Lever) Caps() Caps {
	return Caps{
		SupportsVerification: true,
		ProvidesSalary:       true,
		RequiresWatchlist:    true,
	}
}

type leverPosting struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	HostedURL  string `json:"hostedUrl"`
	ApplyURL   string `json:"applyUrl"`
	Categories struct {
		Location     string   `json:"location"`
		Team         string   `json:"team"`
		Department   string   `json:"department"`
		Commitment   string   `json:"commitment"`
		AllLocations []string `json:"allLocations"`
	} `json:"categories"`
	DescriptionPlain string `json:"descriptionPlain"`
	Description      string `json:"description"`
	AdditionalPlain  string `json:"additionalPlain"`
	CreatedAt        int64  `json:"createdAt"`
	WorkplaceType    string `json:"workplaceType"`
	SalaryRange      *struct {
		Min      float64 `json:"min"`
		Max      float64 `json:"max"`
		Currency string  `json:"currency"`
		Interval string  `json:"interval"`
	} `json:"salaryRange"`
}

func (l *Lever) Search(ctx context.Context, q Query) ([]RawListing, Report, error) {
	start := time.Now()
	rep := Report{SourceID: l.ID()}

	boards := watchlistFor(q.Watchlist, l.ID())
	if len(boards) == 0 {
		rep.Status = "skipped"
		rep.Duration = time.Since(start)
		return nil, rep, nil
	}

	var all []RawListing
	failures := 0
	var lastErr error
	for _, board := range boards {
		url := fmt.Sprintf("%s/%s?mode=json", strings.TrimRight(l.baseURL, "/"), board.BoardSlug)
		body, _, err := l.Fetcher.GetBytes(ctx, l.ID(), url)
		if err != nil {
			failures++
			lastErr = err
			continue
		}
		var postings []leverPosting
		if err := json.Unmarshal(body, &postings); err != nil {
			failures++
			lastErr = err
			continue
		}
		for _, p := range postings {
			all = append(all, l.mapJob(p, board))
		}
	}

	kept, rejected := filterAndLimit(all, q, true)
	rep.Fetched = len(all)
	rep.Accepted = len(kept)
	rep.Rejected = rejected
	rep.Duration = time.Since(start)

	if failures > 0 && failures == len(boards) {
		rep.Status = "error"
		if lastErr != nil {
			rep.Error = lastErr.Error()
		}
		return nil, rep, lastErr
	}
	if failures > 0 {
		rep.Status = "partial"
		if lastErr != nil {
			rep.Error = lastErr.Error()
		}
		return kept, rep, nil
	}
	rep.Status = "ok"
	return kept, rep, nil
}

func (l *Lever) mapJob(p leverPosting, board Watchlist) RawListing {
	desc := FirstNonEmpty(p.DescriptionPlain, p.Description, p.AdditionalPlain)
	rl := RawListing{
		SourceID:    l.ID(),
		SourceJobID: strings.TrimSpace(p.ID),
		URL:         FirstNonEmpty(p.HostedURL, p.ApplyURL),
		ApplyURL:    FirstNonEmpty(p.ApplyURL, p.HostedURL),
		Title:       strings.TrimSpace(p.Text),
		Company:     board.Company,
		Description: summarize(desc),
		LocationRaw: strings.TrimSpace(p.Categories.Location),
		Tags:        DedupeStrings([]string{p.Categories.Department, p.Categories.Team}),
		Raw:         mustRaw(p),
	}
	rl.Verify(AttrTitle, AttrCompany, AttrDescription)
	if rl.LocationRaw != "" {
		rl.Verify(AttrLocation)
	}

	if arr := NormalizeArrangement(p.WorkplaceType, false); arr != "unknown" {
		rl.WorkArrangement = arr
		rl.Verify(AttrWorkArrangement)
	}
	if et := NormalizeEmploymentType(p.Categories.Commitment); et != "unknown" {
		rl.EmploymentType = et
		rl.Verify(AttrEmploymentType)
	}
	if p.SalaryRange != nil && (p.SalaryRange.Min > 0 || p.SalaryRange.Max > 0) {
		if p.SalaryRange.Min > 0 {
			v := p.SalaryRange.Min
			rl.SalaryMin = &v
		}
		if p.SalaryRange.Max > 0 {
			v := p.SalaryRange.Max
			rl.SalaryMax = &v
		}
		rl.SalaryCurrency = strings.ToUpper(strings.TrimSpace(p.SalaryRange.Currency))
		rl.SalaryPeriod = normalizeSalaryPeriod(p.SalaryRange.Interval)
		rl.Verify(AttrSalary)
	}
	if t := unixMillis(p.CreatedAt); t != nil {
		rl.PublishedAt = t
		rl.Verify(AttrPublishedAt)
	}
	return rl
}
