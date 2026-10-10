package sources

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SmartRecruiters fetches public postings from a SmartRecruiters company board.
//
// Access is the vendor's own public postings API — the same surface their
// hosted careers pages use — so no key is needed. Boards are per-company, so
// this is a watchlist source: with no watchlist it reports "skipped" rather
// than failing.
type SmartRecruiters struct {
	Base
	baseURL string
}

// NewSmartRecruiters builds the SmartRecruiters adapter.
func NewSmartRecruiters(opts ...Option) *SmartRecruiters {
	return &SmartRecruiters{
		Base:    NewBase(opts...),
		baseURL: "https://api.smartrecruiters.com/v1/companies",
	}
}

func (s *SmartRecruiters) ID() string { return "smartrecruiters" }

func (s *SmartRecruiters) Caps() Caps {
	return Caps{
		SupportsVerification: true,
		ProvidesSeniority:    true,
		RequiresWatchlist:    true,
	}
}

type smartRecruitersResponse struct {
	TotalFound int                      `json:"totalFound"`
	Content    []smartRecruitersPosting `json:"content"`
}

type smartRecruitersPosting struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ReleasedDate string `json:"releasedDate"`
	Company      struct {
		Identifier string `json:"identifier"`
		Name       string `json:"name"`
	} `json:"company"`
	Location struct {
		City         string `json:"city"`
		Region       string `json:"region"`
		Country      string `json:"country"`
		FullLocation string `json:"fullLocation"`
		Remote       bool   `json:"remote"`
		Hybrid       bool   `json:"hybrid"`
	} `json:"location"`
	Department struct {
		Label string `json:"label"`
	} `json:"department"`
	Function struct {
		Label string `json:"label"`
	} `json:"function"`
	Industry struct {
		Label string `json:"label"`
	} `json:"industry"`
	Employment struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"typeOfEmployment"`
	Level struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"experienceLevel"`
}

func (s *SmartRecruiters) Search(ctx context.Context, q Query) ([]RawListing, Report, error) {
	start := time.Now()
	rep := Report{SourceID: s.ID()}

	boards := watchlistFor(q.Watchlist, s.ID())
	if len(boards) == 0 {
		rep.Status = "skipped"
		rep.Duration = time.Since(start)
		return nil, rep, nil
	}

	var all []RawListing
	failures := 0
	var lastErr error
	for _, board := range boards {
		got, err := s.searchBoard(ctx, board, q)
		if err != nil {
			failures++
			lastErr = err
			continue
		}
		all = append(all, got...)
	}

	kept, rejected := filterAndLimit(all, q, true)
	rep.Fetched = len(all)
	rep.Accepted = len(kept)
	rep.Rejected = rejected
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

// searchBoard walks the board's pages until the requested limit is covered.
func (s *SmartRecruiters) searchBoard(ctx context.Context, board Watchlist, q Query) ([]RawListing, error) {
	boardURL := fmt.Sprintf("%s/%s/postings", strings.TrimRight(s.baseURL, "/"), url.PathEscape(board.BoardSlug))

	want := feedLimit(q.Limit)
	pageSize := 100
	if want < pageSize {
		pageSize = want
	}

	out := make([]RawListing, 0, pageSize)
	for page := 0; page < maxPages; page++ {
		offset := page * pageSize
		if q.Limit > 0 && len(out) >= q.Limit {
			break
		}
		u := appendQuery(boardURL, "limit", strconv.Itoa(pageSize))
		u = appendQuery(u, "offset", strconv.Itoa(offset))

		var resp smartRecruitersResponse
		if err := s.Fetcher.GetJSON(ctx, s.ID(), u, &resp); err != nil {
			return out, err
		}
		if len(resp.Content) == 0 {
			break
		}
		for _, p := range resp.Content {
			out = append(out, s.mapPosting(p, board))
		}
		if len(resp.Content) < pageSize || len(out) >= resp.TotalFound {
			break
		}
	}
	return out, nil
}

func (s *SmartRecruiters) mapPosting(p smartRecruitersPosting, board Watchlist) RawListing {
	identifier := FirstNonEmpty(p.Company.Identifier, board.BoardSlug)
	sourceID := strings.TrimSpace(p.ID)

	rl := RawListing{
		SourceID:    s.ID(),
		SourceJobID: sourceID,
		URL:         fmt.Sprintf("https://jobs.smartrecruiters.com/%s/%s", url.PathEscape(identifier), url.PathEscape(sourceID)),
		Title:       strings.TrimSpace(p.Name),
		Company:     FirstNonEmpty(p.Company.Name, board.Company, identifier),
		LocationRaw: strings.TrimSpace(p.Location.FullLocation),
		City:        strings.TrimSpace(p.Location.City),
		Region:      strings.TrimSpace(p.Location.Region),
		Tags: DedupeStrings([]string{
			p.Department.Label, p.Function.Label, p.Industry.Label,
		}),
		Raw: mustRaw(p),
	}
	rl.ApplyURL = rl.URL
	rl.Verify(AttrTitle, AttrCompany)

	// The postings list endpoint publishes no description (jobAd is null); the
	// per-posting detail endpoint would, at one extra request each. Absent data
	// stays absent rather than being invented.
	if rl.LocationRaw != "" {
		rl.Verify(AttrLocation)
	}
	if c := strings.TrimSpace(p.Location.Country); len(c) == 2 {
		rl.Country = strings.ToUpper(c)
	}

	switch {
	case p.Location.Remote:
		rl.WorkArrangement = "remote"
		rl.Verify(AttrWorkArrangement)
	case p.Location.Hybrid:
		rl.WorkArrangement = "hybrid"
		rl.Verify(AttrWorkArrangement)
	default:
		if arr := NormalizeArrangement(p.Location.FullLocation, false); arr != "unknown" {
			rl.WorkArrangement = arr
			rl.Verify(AttrWorkArrangement)
		}
	}

	if et := NormalizeEmploymentType(p.Employment.Label); et != "unknown" {
		rl.EmploymentType = et
		rl.Verify(AttrEmploymentType)
	}
	if sen := NormalizeSeniority(FirstNonEmpty(p.Level.Label, p.Level.ID)); sen != "unknown" {
		rl.Seniority = sen
		rl.Verify(AttrSeniority)
	}
	if t := parseTimeAny(p.ReleasedDate); t != nil {
		rl.PublishedAt = t
		rl.Verify(AttrPublishedAt)
	}
	return rl
}
