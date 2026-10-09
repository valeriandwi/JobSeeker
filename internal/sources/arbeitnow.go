package sources

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Arbeitnow fetches the keyless Arbeitnow job board feed.
type Arbeitnow struct {
	Base
	baseURL string
}

// NewArbeitnow builds the Arbeitnow adapter.
func NewArbeitnow(opts ...Option) *Arbeitnow {
	return &Arbeitnow{
		Base:    NewBase(opts...),
		baseURL: "https://www.arbeitnow.com/api/job-board-api",
	}
}

func (a *Arbeitnow) ID() string { return "arbeitnow" }

func (a *Arbeitnow) Caps() Caps {
	return Caps{SupportsVerification: true, RequiresKey: false}
}

type arbeitnowResponse struct {
	Data  []arbeitnowJob `json:"data"`
	Links struct {
		Next *string `json:"next"`
	} `json:"links"`
}

type arbeitnowJob struct {
	Slug        string   `json:"slug"`
	CompanyName string   `json:"company_name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Remote      bool     `json:"remote"`
	URL         string   `json:"url"`
	Tags        []string `json:"tags"`
	JobTypes    []string `json:"job_types"`
	Location    string   `json:"location"`
	CreatedAt   int64    `json:"created_at"`
}

func (a *Arbeitnow) Search(ctx context.Context, q Query) ([]RawListing, Report, error) {
	start := time.Now()
	rep := Report{SourceID: a.ID()}

	var all []RawListing
	page := 1
	for {
		u := appendQuery(a.baseURL, "page", fmt.Sprintf("%d", page))
		var resp arbeitnowResponse
		if err := a.Fetcher.GetJSON(ctx, a.ID(), u, &resp); err != nil {
			rep.Fetched = len(all)
			if len(all) == 0 {
				rep.Status = "error"
				rep.Error = err.Error()
				rep.Duration = time.Since(start)
				return nil, rep, err
			}
			rep.Status = "partial"
			rep.Error = err.Error()
			break
		}
		for _, j := range resp.Data {
			all = append(all, a.mapJob(j))
		}
		if q.Limit > 0 && len(all) >= q.Limit {
			break
		}
		if resp.Links.Next == nil || strings.TrimSpace(*resp.Links.Next) == "" {
			break
		}
		page++
		if page > maxPages {
			break
		}
	}

	kept, rejected := filterAndLimit(all, q, true)
	rep.Fetched = len(all)
	rep.Accepted = len(kept)
	rep.Rejected = rejected
	if rep.Status == "" {
		rep.Status = "ok"
	}
	rep.Duration = time.Since(start)
	return kept, rep, nil
}

func (a *Arbeitnow) mapJob(j arbeitnowJob) RawListing {
	r := RawListing{
		SourceID:    a.ID(),
		SourceJobID: strings.TrimSpace(j.Slug),
		URL:         j.URL,
		ApplyURL:    j.URL,
		Title:       strings.TrimSpace(j.Title),
		Company:     strings.TrimSpace(j.CompanyName),
		Description: summarize(j.Description),
		LocationRaw: strings.TrimSpace(j.Location),
		City:        strings.TrimSpace(j.Location),
		Tags:        DedupeStrings(append(append([]string(nil), j.Tags...), j.JobTypes...)),
		Raw:         mustRaw(j),
	}
	r.Verify(AttrTitle, AttrCompany, AttrDescription, AttrLocation)

	if arr := NormalizeArrangement(j.Location, j.Remote); arr != "unknown" {
		r.WorkArrangement = arr
		r.Verify(AttrWorkArrangement)
	}
	if len(j.JobTypes) > 0 {
		if et := NormalizeEmploymentType(strings.Join(j.JobTypes, " ")); et != "unknown" {
			r.EmploymentType = et
			r.Verify(AttrEmploymentType)
		}
	}
	if j.CreatedAt > 0 {
		r.PublishedAt = unixSeconds(j.CreatedAt)
		r.Verify(AttrPublishedAt)
	}
	return r
}
