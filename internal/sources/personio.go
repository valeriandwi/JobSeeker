package sources

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Personio fetches the public XML feed of a Personio-hosted careers site.
//
// The feed is the XML document Personio publishes for embedding on a company's
// own careers page, so no key is needed. One feed per company subdomain makes
// this a watchlist source.
type Personio struct {
	Base
	baseURLTemplate string
}

// NewPersonio builds the Personio adapter.
func NewPersonio(opts ...Option) *Personio {
	return &Personio{
		Base:            NewBase(opts...),
		baseURLTemplate: "https://%s.jobs.personio.de/xml",
	}
}

func (p *Personio) ID() string { return "personio" }

func (p *Personio) Caps() Caps {
	return Caps{
		SupportsVerification: true,
		ProvidesSeniority:    true,
		RequiresWatchlist:    true,
	}
}

type personioFeed struct {
	XMLName   xml.Name           `xml:"workzag-jobs"`
	Positions []personioPosition `xml:"position"`
}

type personioPosition struct {
	ID                 string `xml:"id"`
	Subcompany         string `xml:"subcompany"`
	Office             string `xml:"office"`
	AdditionalOffices  struct {
		Office []string `xml:"office"`
	} `xml:"additionalOffices"`
	Department         string `xml:"department"`
	RecruitingCategory string `xml:"recruitingCategory"`
	Name               string `xml:"name"`
	JobDescriptions    struct {
		JobDescription []struct {
			Name  string `xml:"name"`
			Value string `xml:"value"`
		} `xml:"jobDescription"`
	} `xml:"jobDescriptions"`
	EmploymentType    string `xml:"employmentType"`
	Seniority         string `xml:"seniority"`
	Schedule          string `xml:"schedule"`
	YearsOfExperience string `xml:"yearsOfExperience"`
	Occupation        string `xml:"occupation"`
	OccupationCategory string `xml:"occupationCategory"`
	CreatedAt         string `xml:"createdAt"`
}

func (p *Personio) Search(ctx context.Context, q Query) ([]RawListing, Report, error) {
	start := time.Now()
	rep := Report{SourceID: p.ID()}

	boards := watchlistFor(q.Watchlist, p.ID())
	if len(boards) == 0 {
		rep.Status = "skipped"
		rep.Duration = time.Since(start)
		return nil, rep, nil
	}

	var all []RawListing
	failures := 0
	var lastErr error
	for _, board := range boards {
		feedURL := fmt.Sprintf(p.baseURLTemplate, url.PathEscape(board.BoardSlug))
		body, _, err := p.Fetcher.GetBytes(ctx, p.ID(), feedURL)
		if err != nil {
			failures++
			lastErr = err
			continue
		}
		var feed personioFeed
		if err := xml.Unmarshal(body, &feed); err != nil {
			// An unknown subdomain answers with an HTML error page, not XML.
			// That is a broken board, reported as such, never silently empty.
			failures++
			lastErr = fmt.Errorf("personio: feed for %q is not XML: %w", board.BoardSlug, err)
			continue
		}
		for _, pos := range feed.Positions {
			all = append(all, p.mapPosition(pos, board))
		}
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

func (p *Personio) mapPosition(pos personioPosition, board Watchlist) RawListing {
	id := strings.TrimSpace(pos.ID)
	offices := append([]string{strings.TrimSpace(pos.Office)}, pos.AdditionalOffices.Office...)
	offices = DedupeStrings(cleanNonEmpty(offices))

	rl := RawListing{
		SourceID:    p.ID(),
		SourceJobID: id,
		// The feed carries no URL element; this is the board's own job path,
		// verified against a live board (200) rather than guessed.
		URL:         fmt.Sprintf("https://%s.jobs.personio.de/job/%s", board.BoardSlug, url.PathEscape(id)),
		Title:       strings.TrimSpace(pos.Name),
		Company:     FirstNonEmpty(strings.TrimSpace(pos.Subcompany), board.Company),
		Description: summarize(joinDescriptions(pos)),
		LocationRaw: strings.Join(offices, ", "),
		Tags: DedupeStrings(cleanNonEmpty([]string{
			pos.Department, pos.RecruitingCategory, pos.Occupation, pos.OccupationCategory,
		})),
		Raw: mustRaw(pos),
	}
	rl.ApplyURL = rl.URL
	rl.Verify(AttrTitle, AttrCompany)
	if len(offices) > 0 {
		rl.City = offices[0]
		rl.Verify(AttrLocation)
	}
	if rl.Description != "" {
		rl.Verify(AttrDescription)
	}

	if et := NormalizeEmploymentType(FirstNonEmpty(pos.EmploymentType, pos.Schedule)); et != "unknown" {
		rl.EmploymentType = et
		rl.Verify(AttrEmploymentType)
	}
	if sen := NormalizeSeniority(pos.Seniority); sen != "unknown" {
		rl.Seniority = sen
		rl.Verify(AttrSeniority)
	}
	if t := parseTimeAny(pos.CreatedAt); t != nil {
		rl.PublishedAt = t
		rl.Verify(AttrPublishedAt)
	}
	return rl
}

// joinDescriptions flattens the feed's named description sections into text.
func joinDescriptions(pos personioPosition) string {
	parts := make([]string, 0, len(pos.JobDescriptions.JobDescription))
	for _, d := range pos.JobDescriptions.JobDescription {
		value := strings.TrimSpace(d.Value)
		if value == "" {
			continue
		}
		if name := strings.TrimSpace(d.Name); name != "" {
			parts = append(parts, name+": "+value)
		} else {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "\n\n")
}

func cleanNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
