package sources

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Ashby fetches job boards from the Ashby ATS (watchlist source).
type Ashby struct {
	Base
	baseURL string
}

// NewAshby builds the Ashby adapter.
func NewAshby(opts ...Option) *Ashby {
	return &Ashby{
		Base:    NewBase(opts...),
		baseURL: "https://api.ashbyhq.com/posting-api/job-board",
	}
}

func (a *Ashby) ID() string { return "ashby" }

func (a *Ashby) Caps() Caps {
	return Caps{
		SupportsVerification: true,
		ProvidesSalary:       true,
		RequiresWatchlist:    true,
	}
}

type ashbyResponse struct {
	Jobs []ashbyJob `json:"jobs"`
}

type ashbyJob struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	Location           string `json:"location"`
	SecondaryLocations []struct {
		Location string `json:"location"`
	} `json:"secondaryLocations"`
	Department       string `json:"department"`
	Team             string `json:"team"`
	EmploymentType   string `json:"employmentType"`
	IsListed         *bool  `json:"isListed"`
	IsRemote         bool   `json:"isRemote"`
	WorkplaceType    string `json:"workplaceType"`
	DescriptionPlain string `json:"descriptionPlain"`
	DescriptionHTML  string `json:"descriptionHtml"`
	PublishedAt      string `json:"publishedAt"`
	JobURL           string `json:"jobUrl"`
	ApplyURL         string `json:"applyUrl"`
	Compensation     *struct {
		ScrapeableCompensationSalarySummary string `json:"scrapeableCompensationSalarySummary"`
		CompensationTierSummary             string `json:"compensationTierSummary"`
	} `json:"compensation"`
	Address struct {
		PostalAddress struct {
			AddressLocality string `json:"addressLocality"`
			AddressRegion   string `json:"addressRegion"`
			AddressCountry  string `json:"addressCountry"`
		} `json:"postalAddress"`
	} `json:"address"`
}

func (a *Ashby) Search(ctx context.Context, q Query) ([]RawListing, Report, error) {
	start := time.Now()
	rep := Report{SourceID: a.ID()}

	boards := watchlistFor(q.Watchlist, a.ID())
	if len(boards) == 0 {
		rep.Status = "skipped"
		rep.Duration = time.Since(start)
		return nil, rep, nil
	}

	var all []RawListing
	failures := 0
	var lastErr error
	for _, board := range boards {
		url := fmt.Sprintf("%s/%s?includeCompensation=true", strings.TrimRight(a.baseURL, "/"), board.BoardSlug)
		var resp ashbyResponse
		if err := a.Fetcher.GetJSON(ctx, a.ID(), url, &resp); err != nil {
			failures++
			lastErr = err
			continue
		}
		for _, j := range resp.Jobs {
			// The board explicitly hides unlisted postings; honour that.
			if j.IsListed != nil && !*j.IsListed {
				continue
			}
			all = append(all, a.mapJob(j, board))
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

func (a *Ashby) mapJob(j ashbyJob, board Watchlist) RawListing {
	loc := strings.TrimSpace(j.Location)
	rl := RawListing{
		SourceID:    a.ID(),
		SourceJobID: strings.TrimSpace(j.ID),
		URL:         FirstNonEmpty(j.JobURL, j.ApplyURL),
		ApplyURL:    FirstNonEmpty(j.ApplyURL, j.JobURL),
		Title:       strings.TrimSpace(j.Title),
		Company:     board.Company,
		Description: summarize(FirstNonEmpty(j.DescriptionPlain, j.DescriptionHTML)),
		LocationRaw: loc,
		City:        strings.TrimSpace(j.Address.PostalAddress.AddressLocality),
		Region:      strings.TrimSpace(j.Address.PostalAddress.AddressRegion),
		Tags:        DedupeStrings(append([]string{j.Department, j.Team}, secondaryLocationNames(j)...)),
		Raw:         mustRaw(j),
	}
	// Country is only stored when it is a genuine ISO alpha-2 code; the feed
	// publishes names like "USA", which we do not guess at.
	if c := strings.ToUpper(strings.TrimSpace(j.Address.PostalAddress.AddressCountry)); len(c) == 2 {
		rl.Country = c
	}
	rl.Verify(AttrTitle, AttrCompany, AttrDescription)
	if loc != "" {
		rl.Verify(AttrLocation)
	}

	if arr := NormalizeArrangement(j.WorkplaceType, j.IsRemote); arr != "unknown" {
		rl.WorkArrangement = arr
		rl.Verify(AttrWorkArrangement)
	}
	if et := NormalizeEmploymentType(strings.ReplaceAll(j.EmploymentType, "_", " ")); et != "unknown" {
		rl.EmploymentType = et
		rl.Verify(AttrEmploymentType)
	}
	if j.Compensation != nil {
		salaryText := FirstNonEmpty(j.Compensation.ScrapeableCompensationSalarySummary, j.Compensation.CompensationTierSummary)
		if min, max, cur, per := ParseSalary(salaryText); min != nil || max != nil {
			rl.SalaryMin = min
			rl.SalaryMax = max
			rl.SalaryCurrency = cur
			rl.SalaryPeriod = per
			rl.Verify(AttrSalary)
		}
	}
	if t := parseTimeAny(j.PublishedAt); t != nil {
		rl.PublishedAt = t
		rl.Verify(AttrPublishedAt)
	}
	return rl
}

// secondaryLocationNames extracts the location labels from Ashby's secondary
// location objects (each {"location": "...", "address": {...}}).
func secondaryLocationNames(j ashbyJob) []string {
	out := make([]string, 0, len(j.SecondaryLocations))
	for _, s := range j.SecondaryLocations {
		out = append(out, s.Location)
	}
	return out
}
