package sources

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// Remotive fetches the keyless Remotive remote-jobs feed.
type Remotive struct {
	Base
	baseURL string
}

// NewRemotive builds the Remotive adapter.
func NewRemotive(opts ...Option) *Remotive {
	return &Remotive{
		Base:    NewBase(opts...),
		baseURL: "https://remotive.com/api/remote-jobs",
	}
}

func (r *Remotive) ID() string { return "remotive" }

func (r *Remotive) Caps() Caps {
	return Caps{SupportsVerification: true, ProvidesSalary: true}
}

type remotiveResponse struct {
	Jobs []remotiveJob `json:"jobs"`
}

type remotiveJob struct {
	ID                        int64    `json:"id"`
	URL                       string   `json:"url"`
	Title                     string   `json:"title"`
	CompanyName               string   `json:"company_name"`
	Category                  string   `json:"category"`
	Tags                      []string `json:"tags"`
	JobType                   string   `json:"job_type"`
	PublicationDate           string   `json:"publication_date"`
	CandidateRequiredLocation string   `json:"candidate_required_location"`
	Salary                    string   `json:"salary"`
	Description               string   `json:"description"`
}

func (r *Remotive) Search(ctx context.Context, q Query) ([]RawListing, Report, error) {
	start := time.Now()
	rep := Report{SourceID: r.ID()}

	u := appendQuery(r.baseURL, "limit", strconv.Itoa(feedLimit(q.Limit)))
	var resp remotiveResponse
	if err := r.Fetcher.GetJSON(ctx, r.ID(), u, &resp); err != nil {
		rep.Status = "error"
		rep.Error = err.Error()
		rep.Duration = time.Since(start)
		return nil, rep, err
	}

	all := make([]RawListing, 0, len(resp.Jobs))
	for _, j := range resp.Jobs {
		all = append(all, r.mapJob(j))
	}
	kept, rejected := filterAndLimit(all, q, true)
	rep.Fetched = len(all)
	rep.Accepted = len(kept)
	rep.Rejected = rejected
	rep.Status = "ok"
	rep.Duration = time.Since(start)
	return kept, rep, nil
}

func (r *Remotive) mapJob(j remotiveJob) RawListing {
	rl := RawListing{
		SourceID:    r.ID(),
		SourceJobID: strconv.FormatInt(j.ID, 10),
		URL:         j.URL,
		ApplyURL:    j.URL,
		Title:       strings.TrimSpace(j.Title),
		Company:     strings.TrimSpace(j.CompanyName),
		Description: summarize(j.Description),
		LocationRaw: strings.TrimSpace(j.CandidateRequiredLocation),
		Tags:        DedupeStrings(append(append([]string(nil), j.Tags...), j.Category)),
		Raw:         mustRaw(j),
	}
	rl.Verify(AttrTitle, AttrCompany, AttrDescription, AttrLocation)

	// Remotive only publishes remote roles: the arrangement is stated, not guessed.
	if arr := NormalizeArrangement(j.CandidateRequiredLocation, true); arr != "unknown" {
		rl.WorkArrangement = arr
		rl.Verify(AttrWorkArrangement)
	}
	if et := NormalizeEmploymentType(strings.ReplaceAll(j.JobType, "_", " ")); et != "unknown" {
		rl.EmploymentType = et
		rl.Verify(AttrEmploymentType)
	}
	if min, max, cur, per := ParseSalary(j.Salary); min != nil || max != nil {
		rl.SalaryMin = min
		rl.SalaryMax = max
		rl.SalaryCurrency = cur
		rl.SalaryPeriod = per
		rl.Verify(AttrSalary)
	}
	if t := parseTimeAny(j.PublicationDate); t != nil {
		rl.PublishedAt = t
		rl.Verify(AttrPublishedAt)
	}
	return rl
}
