package sources

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// RemoteOK fetches the keyless RemoteOK feed. Its legal notice requires a
// dofollow backlink, so the untouched URL is preserved for attribution.
type RemoteOK struct {
	Base
	baseURL string
}

// NewRemoteOK builds the RemoteOK adapter.
func NewRemoteOK(opts ...Option) *RemoteOK {
	return &RemoteOK{
		Base:    NewBase(opts...),
		baseURL: "https://remoteok.com/api",
	}
}

func (r *RemoteOK) ID() string { return "remoteok" }

func (r *RemoteOK) Caps() Caps {
	return Caps{SupportsVerification: true, ProvidesSalary: true}
}

type remoteokJob struct {
	Slug        string   `json:"slug"`
	ID          string   `json:"id"`
	Epoch       int64    `json:"epoch"`
	Date        string   `json:"date"`
	Company     string   `json:"company"`
	Position    string   `json:"position"`
	Tags        []string `json:"tags"`
	Description string   `json:"description"`
	Location    string   `json:"location"`
	ApplyURL    string   `json:"apply_url"`
	SalaryMin   float64  `json:"salary_min"`
	SalaryMax   float64  `json:"salary_max"`
	URL         string   `json:"url"`
}

func (r *RemoteOK) Search(ctx context.Context, q Query) ([]RawListing, Report, error) {
	start := time.Now()
	rep := Report{SourceID: r.ID()}

	body, _, err := r.Fetcher.GetBytes(ctx, r.ID(), r.baseURL)
	if err != nil {
		rep.Status = "error"
		rep.Error = err.Error()
		rep.Duration = time.Since(start)
		return nil, rep, err
	}

	// The response is a JSON array whose element 0 is source metadata
	// ({last_updated, legal}); the remaining elements are jobs.
	var elements []json.RawMessage
	if err := json.Unmarshal(body, &elements); err != nil {
		rep.Status = "error"
		rep.Error = err.Error()
		rep.Duration = time.Since(start)
		return nil, rep, err
	}

	all := make([]RawListing, 0, len(elements))
	for i, el := range elements {
		if i == 0 {
			continue
		}
		var j remoteokJob
		if err := json.Unmarshal(el, &j); err != nil {
			continue
		}
		all = append(all, r.mapJob(j, el))
	}
	kept, rejected := filterAndLimit(all, q, true)
	rep.Fetched = len(all)
	rep.Accepted = len(kept)
	rep.Rejected = rejected
	rep.Status = "ok"
	rep.Duration = time.Since(start)
	return kept, rep, nil
}

func (r *RemoteOK) mapJob(j remoteokJob, raw json.RawMessage) RawListing {
	rl := RawListing{
		SourceID:    r.ID(),
		SourceJobID: strings.TrimSpace(j.ID),
		URL:         j.URL,
		ApplyURL:    FirstNonEmpty(j.ApplyURL, j.URL),
		Title:       strings.TrimSpace(j.Position),
		Company:     strings.TrimSpace(j.Company),
		Description: summarize(j.Description),
		LocationRaw: strings.TrimSpace(j.Location),
		Tags:        DedupeStrings(j.Tags),
		Raw:         raw,
	}
	rl.Verify(AttrTitle, AttrCompany, AttrDescription)
	if rl.LocationRaw != "" {
		rl.Verify(AttrLocation)
	}

	// RemoteOK is a remote-only board; the arrangement is a property of the source.
	if arr := NormalizeArrangement(j.Location, true); arr != "unknown" {
		rl.WorkArrangement = arr
		rl.Verify(AttrWorkArrangement)
	}
	// The feed separates min/max but states no currency or period; we record
	// exactly what was published and nothing more.
	if j.SalaryMin > 0 || j.SalaryMax > 0 {
		if j.SalaryMin > 0 {
			v := j.SalaryMin
			rl.SalaryMin = &v
		}
		if j.SalaryMax > 0 {
			v := j.SalaryMax
			rl.SalaryMax = &v
		}
		rl.Verify(AttrSalary)
	}
	if t := parseTimeAny(j.Date); t != nil {
		rl.PublishedAt = t
		rl.Verify(AttrPublishedAt)
	} else if t := unixSeconds(j.Epoch); t != nil {
		rl.PublishedAt = t
		rl.Verify(AttrPublishedAt)
	}
	return rl
}
