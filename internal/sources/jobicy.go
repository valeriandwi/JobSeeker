package sources

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// Jobicy fetches the keyless Jobicy v2 remote-jobs feed.
type Jobicy struct {
	Base
	baseURL string
}

// NewJobicy builds the Jobicy adapter.
func NewJobicy(opts ...Option) *Jobicy {
	return &Jobicy{
		Base:    NewBase(opts...),
		baseURL: "https://jobicy.com/api/v2/remote-jobs",
	}
}

func (j *Jobicy) ID() string { return "jobicy" }

func (j *Jobicy) Caps() Caps {
	return Caps{SupportsVerification: true, ProvidesSalary: true, ProvidesSeniority: true}
}

type jobicyResponse struct {
	Jobs []jobicyJob `json:"jobs"`
}

type jobicyJob struct {
	ID             int64    `json:"id"`
	URL            string   `json:"url"`
	JobTitle       string   `json:"jobTitle"`
	CompanyName    string   `json:"companyName"`
	JobGeo         string   `json:"jobGeo"`
	JobIndustry    []string `json:"jobIndustry"`
	JobLevel       string   `json:"jobLevel"`
	JobType        []string `json:"jobType"`
	JobExcerpt     string   `json:"jobExcerpt"`
	JobDescription string   `json:"jobDescription"`
	PubDate        string   `json:"pubDate"`
	SalaryMin      *float64 `json:"salaryMin"`
	SalaryMax      *float64 `json:"salaryMax"`
	SalaryCurrency string   `json:"salaryCurrency"`
	SalaryPeriod   string   `json:"salaryPeriod"`
}

func (j *Jobicy) Search(ctx context.Context, q Query) ([]RawListing, Report, error) {
	start := time.Now()
	rep := Report{SourceID: j.ID()}

	u := appendQuery(j.baseURL, "count", strconv.Itoa(feedLimit(q.Limit)))
	var resp jobicyResponse
	if err := j.Fetcher.GetJSON(ctx, j.ID(), u, &resp); err != nil {
		rep.Status = "error"
		rep.Error = err.Error()
		rep.Duration = time.Since(start)
		return nil, rep, err
	}

	all := make([]RawListing, 0, len(resp.Jobs))
	for _, item := range resp.Jobs {
		all = append(all, j.mapJob(item))
	}
	kept, rejected := filterAndLimit(all, q, true)
	rep.Fetched = len(all)
	rep.Accepted = len(kept)
	rep.Rejected = rejected
	rep.Status = "ok"
	rep.Duration = time.Since(start)
	return kept, rep, nil
}

func (j *Jobicy) mapJob(item jobicyJob) RawListing {
	desc := item.JobDescription
	if strings.TrimSpace(desc) == "" {
		desc = item.JobExcerpt
	}
	rl := RawListing{
		SourceID:    j.ID(),
		SourceJobID: strconv.FormatInt(item.ID, 10),
		URL:         item.URL,
		ApplyURL:    item.URL,
		Title:       strings.TrimSpace(item.JobTitle),
		Company:     strings.TrimSpace(item.CompanyName),
		Description: summarize(desc),
		LocationRaw: strings.TrimSpace(item.JobGeo),
		Tags:        DedupeStrings(item.JobIndustry),
		Raw:         mustRaw(item),
	}
	rl.Verify(AttrTitle, AttrCompany, AttrDescription, AttrLocation)

	if arr := NormalizeArrangement(item.JobGeo, true); arr != "unknown" {
		rl.WorkArrangement = arr
		rl.Verify(AttrWorkArrangement)
	}
	if len(item.JobType) > 0 {
		if et := NormalizeEmploymentType(strings.Join(item.JobType, " ")); et != "unknown" {
			rl.EmploymentType = et
			rl.Verify(AttrEmploymentType)
		}
	}
	if s := strings.TrimSpace(item.JobLevel); s != "" {
		if sen := NormalizeSeniority(s); sen != "unknown" {
			rl.Seniority = sen
			rl.Verify(AttrSeniority)
		}
	}
	if item.SalaryMin != nil || item.SalaryMax != nil {
		rl.SalaryMin = item.SalaryMin
		rl.SalaryMax = item.SalaryMax
		rl.SalaryCurrency = strings.ToUpper(strings.TrimSpace(item.SalaryCurrency))
		rl.SalaryPeriod = normalizeSalaryPeriod(item.SalaryPeriod)
		rl.Verify(AttrSalary)
	}
	if t := parseTimeAny(item.PubDate); t != nil {
		rl.PublishedAt = t
		rl.Verify(AttrPublishedAt)
	}
	return rl
}
