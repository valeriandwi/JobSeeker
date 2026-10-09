// Package jobs owns the canonical vacancy record: text canonicalization,
// cross-portal duplicate detection and the vacancy lifecycle.
//
// Two-layer storage drives this design: `jobs` holds one canonical row per real
// vacancy, while `job_source_listings` keeps one row per portal that advertised
// it. Duplicate detection therefore has to answer "is this incoming listing the
// same vacancy we already know?" without losing per-source attribution.
package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/valeriandwipurnomo/jobseeker/internal/matching"
	"github.com/valeriandwipurnomo/jobseeker/internal/sources"
)

var (
	reNonAlnum  = regexp.MustCompile(`[^a-z0-9+#. ]+`)
	reSpaces    = regexp.MustCompile(`\s+`)
	companyTail = map[string]bool{
		"inc": true, "incorporated": true, "ltd": true, "limited": true, "llc": true,
		"l.l.c": true, "plc": true, "gmbh": true, "ag": true, "bv": true, "b.v": true,
		"nv": true, "sa": true, "s.a": true, "srl": true, "s.r.l": true, "pte": true,
		"pty": true, "co": true, "corp": true, "corporation": true, "company": true,
		"holdings": true, "group": true, "technologies": true, "technology": true,
		"sdn": true, "bhd": true, "tbk": true, "oy": true, "ab": true, "as": true,
		"aps": true, "spa": true, "s.p.a": true, "kft": true, "d.o.o": true,
	}
)

// NormalizeText lowercases, strips punctuation that carries no meaning for
// comparison, and collapses whitespace.
func NormalizeText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' || r == '#' || r == '.' {
			return r
		}
		return ' '
	}, s)
	s = reNonAlnum.ReplaceAllString(s, " ")
	s = reSpaces.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// NormalizeCompany removes legal-entity suffixes and noise so that
// "Example Co., Ltd." and "Example" compare equal.
func NormalizeCompany(s string) string {
	norm := NormalizeText(s)
	if norm == "" {
		return ""
	}
	parts := strings.Fields(norm)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(p, ".,")
		if p == "" || companyTail[p] {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return norm
	}
	return strings.Join(out, " ")
}

// URLHash returns a stable digest of a canonical URL.
func URLHash(rawURL string) string {
	sum := sha256.Sum256([]byte(sources.SanitizeURL(rawURL)))
	return hex.EncodeToString(sum[:])
}

// CanonicalKey is the deterministic identity of a vacancy used as the unique
// constraint on jobs.canonical_key. It is intentionally built from normalized
// title+company+location only: the same vacancy syndicated to several portals
// produces the same key.
func CanonicalKey(title, company, city, country string) string {
	parts := []string{
		NormalizeText(title),
		NormalizeCompany(company),
		NormalizeText(city),
		strings.ToUpper(strings.TrimSpace(country)),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

// Identity holds the fields compared when deciding whether two listings are the
// same vacancy.
type Identity struct {
	SourceID    string
	SourceJobID string
	URLHash     string
	Title       string
	TitleNorm   string
	Company     string
	CompanyNorm string
	City        string
	Country     string
}

// NewIdentity builds an Identity from raw listing fields.
func NewIdentity(sourceID, sourceJobID, url, title, company, city, country string) Identity {
	return Identity{
		SourceID:    sourceID,
		SourceJobID: sourceJobID,
		URLHash:     URLHash(url),
		Title:       title,
		TitleNorm:   NormalizeText(title),
		Company:     company,
		CompanyNorm: NormalizeCompany(company),
		City:        NormalizeText(city),
		Country:     strings.ToUpper(strings.TrimSpace(country)),
	}
}

// IdentityFromCanonical builds an Identity from an existing jobs row.
func IdentityFromCanonical(titleNorm, companyNorm, city, country string) Identity {
	return Identity{
		TitleNorm:   titleNorm,
		CompanyNorm: companyNorm,
		City:        NormalizeText(city),
		Country:     strings.ToUpper(strings.TrimSpace(country)),
	}
}

// DuplicateDecision explains a duplicate verdict, for auditing and for the
// per-run statistics the Search History page displays.
type DuplicateDecision struct {
	Same       bool
	Tier       string  // exact_source_id|exact_url|fuzzy
	Similarity float64 // combined similarity for the fuzzy tier
	Reason     string
}

// SimThreshold is the combined similarity above which two listings are treated
// as the same vacancy. It is deliberately high: merging two different jobs is
// worse for the user than showing a duplicate.
const SimThreshold = matching.DefaultDuplicateSim

// SameVacancy applies the three-tier duplicate policy:
//
//	tier 1: same source + same source job id  (exact, handled by a DB constraint)
//	tier 2: same canonical URL hash           (exact, handled by a DB constraint)
//	tier 3: normalized title + company + city similarity
func SameVacancy(incoming, existing Identity) DuplicateDecision {
	if incoming.SourceID != "" && incoming.SourceID == existing.SourceID && incoming.SourceJobID != "" && incoming.SourceJobID == existing.SourceJobID {
		return DuplicateDecision{Same: true, Tier: "exact_source_id", Similarity: 1, Reason: "same source job id"}
	}
	if incoming.URLHash != "" && incoming.URLHash == existing.URLHash {
		return DuplicateDecision{Same: true, Tier: "exact_url", Similarity: 1, Reason: "same canonical application URL"}
	}
	sim, reason := CombinedSimilarity(incoming, existing)
	if sim >= SimThreshold {
		return DuplicateDecision{Same: true, Tier: "fuzzy", Similarity: sim, Reason: reason}
	}
	return DuplicateDecision{Tier: "fuzzy", Similarity: sim, Reason: reason}
}

// CombinedSimilarity weighs title most heavily, then company, then location.
// A company mismatch caps the similarity: the same title at a different company
// is a different job.
func CombinedSimilarity(a, b Identity) (float64, string) {
	if a.TitleNorm == "" || b.TitleNorm == "" {
		return 0, "missing title; cannot compare"
	}
	titleSim := matching.TitleSimilarity(a.TitleNorm, b.TitleNorm)
	companySim := 0.0
	switch {
	case a.CompanyNorm == "" || b.CompanyNorm == "":
		companySim = 0 // unknown company: do not assume
	case a.CompanyNorm == b.CompanyNorm:
		companySim = 1
	default:
		companySim = matching.TitleSimilarity(a.CompanyNorm, b.CompanyNorm)
	}
	locSim := 0.0
	if a.City != "" && b.City != "" {
		if a.City == b.City {
			locSim = 1
		} else {
			locSim = matching.TitleSimilarity(a.City, b.City) * 0.5
		}
	} else if a.Country != "" && b.Country != "" && a.Country == b.Country {
		locSim = 0.5
	}

	combined := 0.65*titleSim + 0.25*companySim + 0.10*locSim
	if companySim < 0.5 {
		combined = combined * 0.5
	}
	reason := "title similarity " + f2(titleSim) + ", company similarity " + f2(companySim) + ", location similarity " + f2(locSim)
	return combined, reason
}

func f2(f float64) string {
	return strconv.FormatFloat(f, 'f', 2, 64)
}
