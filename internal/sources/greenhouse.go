package sources

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Greenhouse fetches job boards from the Greenhouse ATS (watchlist source).
type Greenhouse struct {
	Base
	baseURL string
}

// NewGreenhouse builds the Greenhouse adapter.
func NewGreenhouse(opts ...Option) *Greenhouse {
	return &Greenhouse{
		Base:    NewBase(opts...),
		baseURL: "https://boards-api.greenhouse.io/v1/boards",
	}
}

func (g *Greenhouse) ID() string { return "greenhouse" }

func (g *Greenhouse) Caps() Caps {
	return Caps{
		SupportsVerification: true,
		ProvidesExpiry:       true,
		RequiresWatchlist:    true,
	}
}

type greenhouseResponse struct {
	Jobs []greenhouseJob `json:"jobs"`
}

type greenhouseJob struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	AbsoluteURL string `json:"absolute_url"`
	Location    struct {
		Name string `json:"name"`
	} `json:"location"`
	Offices []struct {
		Name     string `json:"name"`
		Location string `json:"location"`
	} `json:"offices"`
	Content             string `json:"content"`
	UpdatedAt           string `json:"updated_at"`
	FirstPublished      string `json:"first_published"`
	ApplicationDeadline string `json:"application_deadline"`
	Departments         []struct {
		Name string `json:"name"`
	} `json:"departments"`
	CompanyName string `json:"company_name"`
}

func (g *Greenhouse) Search(ctx context.Context, q Query) ([]RawListing, Report, error) {
	start := time.Now()
	rep := Report{SourceID: g.ID()}

	boards := watchlistFor(q.Watchlist, g.ID())
	if len(boards) == 0 {
		rep.Status = "skipped"
		rep.Duration = time.Since(start)
		return nil, rep, nil
	}

	var all []RawListing
	failures := 0
	var lastErr error
	for _, board := range boards {
		url := fmt.Sprintf("%s/%s/jobs?content=true", strings.TrimRight(g.baseURL, "/"), board.BoardSlug)
		var resp greenhouseResponse
		if err := g.Fetcher.GetJSON(ctx, g.ID(), url, &resp); err != nil {
			failures++
			lastErr = err
			continue
		}
		for _, j := range resp.Jobs {
			all = append(all, g.mapJob(j, board))
		}
	}

	kept, rejected := filterAndLimit(all, q, true)
	rep.Fetched = len(all)
	rep.Accepted = len(kept)
	rep.Rejected = rejected
	rep.Duration = time.Since(start)

	if failures > 0 && failures == len(boards) {
		// Every configured board failed: the source failed entirely.
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

func (g *Greenhouse) mapJob(j greenhouseJob, board Watchlist) RawListing {
	tags := make([]string, 0, len(j.Departments)+len(j.Offices))
	for _, d := range j.Departments {
		tags = append(tags, d.Name)
	}
	for _, o := range j.Offices {
		tags = append(tags, o.Name)
	}

	rl := RawListing{
		SourceID:    g.ID(),
		SourceJobID: strconv.FormatInt(j.ID, 10),
		URL:         j.AbsoluteURL,
		ApplyURL:    j.AbsoluteURL,
		Title:       strings.TrimSpace(j.Title),
		Company:     FirstNonEmpty(j.CompanyName, board.Company),
		Description: summarize(j.Content),
		LocationRaw: strings.TrimSpace(j.Location.Name),
		Tags:        DedupeStrings(tags),
		Raw:         mustRaw(j),
	}
	rl.Verify(AttrTitle, AttrCompany, AttrDescription)
	if strings.TrimSpace(j.Location.Name) != "" {
		rl.Verify(AttrLocation)
	}

	if arr := NormalizeArrangement(j.Location.Name, false); arr != "unknown" {
		rl.WorkArrangement = arr
		rl.Verify(AttrWorkArrangement)
	}
	if t := parseTimeAny(j.FirstPublished); t != nil {
		rl.PublishedAt = t
		rl.Verify(AttrPublishedAt)
	}
	if t := parseTimeAny(j.ApplicationDeadline); t != nil {
		rl.ExpiresAt = t
		rl.Verify(AttrExpiresAt)
	}
	return rl
}

// watchlistFor returns the watchlist entries targeting the given source id.
func watchlistFor(wl []Watchlist, sourceID string) []Watchlist {
	out := make([]Watchlist, 0, len(wl))
	for _, w := range wl {
		if w.SourceID == sourceID && strings.TrimSpace(w.BoardSlug) != "" {
			out = append(out, w)
		}
	}
	return out
}
