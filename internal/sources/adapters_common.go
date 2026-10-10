package sources

import (
	"encoding/json"
	"html"
	"strings"
	"time"
)

// Shared behaviour for every adapter. These helpers keep mapping identical
// across sources and enforce the "never invent data" rule: a value is only set
// (and only marked verified) when the source payload actually carried it.

const (
	// descriptionSummaryLimit caps how much plain-text description is stored.
	descriptionSummaryLimit = 4000
	// defaultFeedLimit is used when Query.Limit is unset for paginated feeds.
	defaultFeedLimit = 50
	// maxPages bounds how many pages a paginated feed adapter will walk. It is a
	// ceiling, not a target: every paging adapter stops as soon as it has
	// collected the requested limit, so this only has to be big enough for the
	// largest per-run limit — 1000 listings at 100 per page needs 10 pages, and
	// 20 leaves headroom without letting a bad limit loop forever.
	maxPages = 20
)

// summarize converts a (possibly HTML-escaped) description to plain text and
// truncates it to a storage-friendly summary length.
func summarize(raw string) string {
	txt := StripHTML(html.UnescapeString(raw))
	r := []rune(txt)
	if len(r) > descriptionSummaryLimit {
		txt = strings.TrimSpace(string(r[:descriptionSummaryLimit]))
	}
	return txt
}

// mustRaw marshals a typed payload for RawListing.Raw. Marshal of a decoded
// struct cannot fail for our payload shapes, so errors collapse to nil.
func mustRaw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// parseTimeAny parses the common timestamp layouts job sources use. A value it
// cannot parse yields nil rather than a guessed time.
func parseTimeAny(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return &t
		}
	}
	return nil
}

// unixSeconds converts a unix-seconds timestamp to UTC, nil when non-positive.
func unixSeconds(v int64) *time.Time {
	if v <= 0 {
		return nil
	}
	t := time.Unix(v, 0).UTC()
	return &t
}

// unixMillis converts a unix-milliseconds timestamp to UTC, nil when
// non-positive.
func unixMillis(v int64) *time.Time {
	if v <= 0 {
		return nil
	}
	t := time.UnixMilli(v).UTC()
	return &t
}

// matchesAnyKeyword reports whether hay contains any needle (case-insensitive).
func matchesAnyKeyword(hay string, needles []string) bool {
	for _, n := range needles {
		n = strings.ToLower(strings.TrimSpace(n))
		if n != "" && strings.Contains(hay, n) {
			return true
		}
	}
	return false
}

// normalizeSalaryPeriod maps a source's period text to the canonical set
// (year|month|day|hour); anything unrecognised maps to "".
func normalizeSalaryPeriod(raw string) string {
	l := strings.ToLower(strings.TrimSpace(raw))
	if l == "" {
		return ""
	}
	switch {
	case containsAny(l, "year", "annual", "annum", "p.a.", "yr"):
		return "year"
	case containsAny(l, "month", "mo"):
		return "month"
	case containsAny(l, "day", "daily"):
		return "day"
	case containsAny(l, "hour", "hourly", "hr"):
		return "hour"
	default:
		return ""
	}
}

// filterAndLimit applies the client-side Query filters every adapter honours:
// canonical-URL de-duplication, ExcludeKeywords, and — for sources with no
// server-side search — a best-effort title/keyword include filter. It returns
// the surviving listings and the number rejected.
func filterAndLimit(items []RawListing, q Query, clientSideSearch bool) ([]RawListing, int) {
	include := make([]string, 0, len(q.Titles)+len(q.Keywords))
	include = append(include, q.Titles...)
	include = append(include, q.Keywords...)

	rejected := 0
	seen := make(map[string]struct{}, len(items))
	kept := make([]RawListing, 0, len(items))
	for _, it := range items {
		// Canonical-URL check: the untouched URL stays in RawListing.URL for
		// attribution; the sanitized form is only used for duplicate detection.
		key := SanitizeURL(it.URL)
		if key == "" {
			key = SanitizeURL(it.ApplyURL)
		}
		if key != "" {
			if _, dup := seen[key]; dup {
				rejected++
				continue
			}
			seen[key] = struct{}{}
		}

		hay := strings.ToLower(it.Title + " " + it.Company + " " + it.Description)
		if matchesAnyKeyword(hay, q.ExcludeKeywords) {
			rejected++
			continue
		}
		if clientSideSearch && len(include) > 0 {
			hit := strings.ToLower(it.Title + " " + it.Description)
			if !matchesAnyKeyword(hit, include) {
				rejected++
				continue
			}
		}
		kept = append(kept, it)
	}
	if q.Limit > 0 && len(kept) > q.Limit {
		kept = kept[:q.Limit]
	}
	return kept, rejected
}

// feedLimit returns the page size to request from a paginated feed.
func feedLimit(limit int) int {
	if limit > 0 {
		return limit
	}
	return defaultFeedLimit
}

// appendQuery appends a "?k=v" (or "&k=v") pair to a URL.
func appendQuery(u, key, val string) string {
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	return u + sep + key + "=" + val
}
