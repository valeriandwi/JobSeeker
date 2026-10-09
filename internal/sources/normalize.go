package sources

import (
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Normalization helpers shared by every adapter.
//
// The rule these implement: produce either a confident value or "unknown".
// Guessing is what makes a matching system lie to its user, so ambiguous input
// maps to "unknown" rather than to a plausible-looking default.

var (
	reWhitespace = regexp.MustCompile(`\s+`)
	reHTMLTag    = regexp.MustCompile(`(?s)<[^>]*>`)
	reScript     = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</\s*(script|style)\s*>`)
	reLineBreaks = regexp.MustCompile(`(?i)<(br|/p|/div|/li|/tr|/h[1-6])\s*/?>`)
	// A plain number token: digits with locale separators. Magnitude suffixes
	// (90k, 1.5M, 25jt) are detected by looking at the text immediately after
	// the match, because Go's regexp engine has no lookahead.
	reNumber = regexp.MustCompile(`\d[\d.,]*`)
)

var htmlEntities = strings.NewReplacer(
	"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`,
	"&#39;", "'", "&apos;", "'", "&mdash;", "—", "&ndash;", "–", "&hellip;", "…",
	"&rsquo;", "'", "&lsquo;", "'", "&ldquo;", `"`, "&rdquo;", `"`, "&euro;", "€",
	"&pound;", "£", "&deg;", "°", "&bull;", "•", "&middot;", "·",
)

// StripHTML converts an HTML fragment to plain text.
func StripHTML(s string) string {
	if s == "" {
		return ""
	}
	s = reScript.ReplaceAllString(s, " ")
	s = reLineBreaks.ReplaceAllString(s, "\n")
	s = reHTMLTag.ReplaceAllString(s, " ")
	s = htmlEntities.Replace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(reWhitespace.ReplaceAllString(l, " "))
	}
	// Collapse runs of blank lines.
	out := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		if l == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// CollapseWhitespace trims and collapses all whitespace runs to single spaces.
func CollapseWhitespace(s string) string {
	return strings.TrimSpace(reWhitespace.ReplaceAllString(s, " "))
}

// NormalizeArrangement maps free-form arrangement text to one of
// remote|onsite|hybrid|unknown. remoteHint is used by sources that expose a
// separate boolean (e.g. "remote": true).
func NormalizeArrangement(raw string, remoteHint bool) string {
	r := strings.ToLower(CollapseWhitespace(raw))
	hybrid := containsAny(r, "hybrid", "partially remote", "part remote", "flexible remote", "remote/hybrid")
	if hybrid {
		return "hybrid"
	}
	remote := remoteHint || containsAny(r,
		"remote", "work from home", "wfh", "work-from-home", "telecommute", "telework",
		"fully distributed", "distributed team", "anywhere", "home-based", "home based")
	if remote {
		return "remote"
	}
	if containsAny(r, "on-site", "onsite", "on site", "in-office", "in office", "in person", "in-person", "office-based", "on premise", "on-premise") {
		return "onsite"
	}
	// A bare city/country string says nothing about arrangement.
	return "unknown"
}

// NormalizeEmploymentType maps free-form employment text to the canonical set.
func NormalizeEmploymentType(raw string) string {
	r := strings.ToLower(CollapseWhitespace(raw))
	if r == "" {
		return "unknown"
	}
	switch {
	case containsAny(r, "intern", "internship", "praktik", "magang", "trainee"):
		return "internship"
	case containsAny(r, "part-time", "part time", "parttime", "freelance", "side job"):
		return "part_time"
	case containsAny(r, "contract", "contractor", "fixed term", "fixed-term", "b2b", "temporary", "temp ", "locum"):
		return "contract"
	case containsAny(r, "full-time", "full time", "fulltime", "permanent", "regular employee", "cdi"):
		return "permanent"
	case containsAny(r, "volunteer", "unpaid"):
		return "temporary"
	default:
		return "unknown"
	}
}

// NormalizeSeniority maps free-form level text to the canonical set.
func NormalizeSeniority(raw string) string {
	r := strings.ToLower(CollapseWhitespace(raw))
	if r == "" {
		return "unknown"
	}
	switch {
	case containsAny(r, "principal", "staff engineer", "distinguished", "fellow"):
		return "principal"
	case containsAny(r, "lead", "leader", "head of", "manager", "director", "vp ", "chief", "cto", "ceo"):
		return "lead"
	case containsAny(r, "senior", "sr.", "sr ", "experienced", "expert"):
		return "senior"
	case containsAny(r, "junior", "jr.", "jr ", "entry", "graduate", "internship", "apprentice", "associate"):
		return "junior"
	case containsAny(r, "mid", "intermediate", "middle", "medior"):
		return "mid"
	case containsAny(r, "intern"):
		return "intern"
	default:
		return "unknown"
	}
}

// ParseSalary extracts a salary range from free-form text. It is deliberately
// conservative: anything it cannot parse confidently yields nils and an empty
// currency, and the caller must mark salary as unknown rather than guess.
func ParseSalary(raw string) (min, max *float64, currency, period string) {
	text := CollapseWhitespace(raw)
	if text == "" {
		return nil, nil, "", ""
	}
	lower := strings.ToLower(text)
	if containsAny(lower, "undisclosed", "not disclosed", "competitive", "negotiable", "doe", "no salary", "n/a") {
		return nil, nil, "", ""
	}

	currency = detectCurrency(text)
	period = detectPeriod(lower)

	nums := reNumber.FindAllStringIndex(text, -1)
	values := make([]float64, 0, len(nums))
	for _, m := range nums {
		token := text[m[0]:m[1]]
		multiplier := magnitudeAfter(text, m[1])
		v, ok := parseAmount(token, multiplier)
		if !ok {
			continue
		}
		values = append(values, v)
	}
	if len(values) == 0 {
		return nil, nil, currency, period
	}
	lo, hi := values[0], values[len(values)-1]
	if hi < lo {
		lo, hi = hi, lo
	}
	// A single value is a point estimate: treat as both bounds.
	if len(values) == 1 {
		hi = lo
	}
	return &lo, &hi, currency, period
}

// magnitudeAfter inspects the text immediately following a numeric token for a
// magnitude suffix: k/K for thousands, m/M for millions, jt for millions
// (Indonesian "juta"). The suffix is only accepted when it is not part of a
// longer word, so "90 km" is not read as ninety thousand.
func magnitudeAfter(text string, pos int) float64 {
	j := pos
	for j < len(text) && (text[j] == ' ' || text[j] == '\u00a0') {
		j++
	}
	if j >= len(text) {
		return 1
	}
	suffixLen := 0
	multiplier := 1.0
	switch c := text[j]; {
	case c == 'k' || c == 'K':
		multiplier, suffixLen = 1e3, 1
	case c == 'm' || c == 'M':
		multiplier, suffixLen = 1e6, 1
	case (c == 'j' || c == 'J') && j+1 < len(text) && (text[j+1] == 't' || text[j+1] == 'T'):
		multiplier, suffixLen = 1e6, 2
	default:
		return 1
	}
	if end := j + suffixLen; end < len(text) {
		next := text[end]
		if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') {
			return 1 // part of a longer word
		}
	}
	return multiplier
}

// parseAmount interprets one numeric token, applying a magnitude multiplier.
//
// Separator rules:
//   - with both "." and "," the rightmost separator is the decimal separator
//   - repeated separators are grouping: 15.000.000 is fifteen million
//   - a single separator followed by exactly three digits is grouping, so
//     1,000 is one thousand while 1.5 and 211.4 are decimals
func parseAmount(raw string, multiplier float64) (float64, bool) {
	s := strings.NewReplacer(" ", "", "_", "", "\u00a0", "", "	", "").Replace(strings.TrimSpace(raw))
	if s == "" {
		return 0, false
	}
	if multiplier <= 0 {
		multiplier = 1
	}

	value, ok := normalizeNumeric(s)
	if !ok {
		return 0, false
	}
	v := value * multiplier
	if v <= 0 || v > 1e12 {
		return 0, false
	}
	return v, true
}

// normalizeNumeric resolves separator ambiguity into a plain float.
func normalizeNumeric(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	dots := strings.Count(s, ".")
	commas := strings.Count(s, ",")

	switch {
	case dots > 0 && commas > 0:
		if strings.LastIndex(s, ",") > strings.LastIndex(s, ".") {
			// 1.234,56 -> the comma is the decimal separator.
			s = strings.ReplaceAll(s, ".", "")
			li := strings.LastIndex(s, ",")
			s = s[:li] + "." + s[li+1:]
			s = strings.ReplaceAll(s, ",", "")
		} else {
			// 1,234.56 -> the dot is the decimal separator.
			s = strings.ReplaceAll(s, ",", "")
		}
	case dots > 1:
		s = strings.ReplaceAll(s, ".", "")
	case commas > 1:
		s = strings.ReplaceAll(s, ",", "")
	case dots == 1 || commas == 1:
		sep := "."
		if commas == 1 {
			sep = ","
		}
		idx := strings.Index(s, sep)
		after := s[idx+1:]
		if len(after) == 3 && len(after) == len(strings.TrimRight(after, "0123456789"))+3 {
			s = strings.ReplaceAll(s, sep, "") // 15.000 / 1,000 -> grouping
		} else {
			s = s[:idx] + "." + after // 211.4 / 1,5 -> decimal
		}
	}

	if strings.Count(s, ".") > 1 {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func detectCurrency(text string) string {
	upper := strings.ToUpper(text)
	for _, code := range []string{"USD", "EUR", "GBP", "IDR", "SGD", "MYR", "AUD", "CAD", "JPY", "INR", "CHF", "SEK", "NOK", "DKK", "PLN", "PHP", "THB", "VND", "HKD", "NZD", "BRL", "MXN", "ZAR", "AED", "SAR", "CZK", "HUF", "RON", "TRY", "KRW", "TWD", "CNY", "RMB", "ILS"} {
		if containsAny(upper, code) {
			if code == "RMB" {
				return "CNY"
			}
			return code
		}
	}
	switch {
	case strings.Contains(text, "$"):
		return "USD"
	case strings.Contains(text, "€"):
		return "EUR"
	case strings.Contains(text, "£"):
		return "GBP"
	case strings.Contains(text, "Rp") || strings.Contains(strings.ToLower(text), "idr"):
		return "IDR"
	case strings.Contains(text, "RM"):
		return "MYR"
	case strings.Contains(text, "S$"):
		return "SGD"
	case strings.Contains(text, "¥"):
		return "JPY"
	case strings.Contains(text, "₹"):
		return "INR"
	}
	return ""
}

func detectPeriod(lower string) string {
	switch {
	case containsAny(lower, "per year", "/year", "/yr", "yearly", "annual", "per annum", "p.a.", "annum", "year"):
		return "year"
	case containsAny(lower, "per month", "/month", "/mo", "monthly", "per bulan", "bulan", "month"):
		return "month"
	case containsAny(lower, "per day", "/day", "daily", "day"):
		return "day"
	case containsAny(lower, "per hour", "/hour", "/hr", "hourly", "hour"):
		return "hour"
	}
	return ""
}

// SanitizeURL returns a canonical form of a job URL with tracking parameters
// removed, used for duplicate detection. The original URL is always preserved
// separately for display and attribution.
func SanitizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	q := u.Query()
	for key := range q {
		lk := strings.ToLower(key)
		if strings.HasPrefix(lk, "utm_") || trackingParams[lk] {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	p := u.Path
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		p = strings.TrimSuffix(p, "/")
	}
	u.Path = p
	return u.String()
}

var trackingParams = map[string]bool{
	"gclid": true, "fbclid": true, "msclkid": true, "ref": true, "referrer": true,
	"source": true, "src": true, "trk": true, "trackingid": true, "gh_src": true,
	"lever-origin": true, "lever-source": true, "yclid": true, "mc_cid": true,
	"mc_eid": true, "_ga": true, "igshid": true, "s_kwcid": true,
}

// FirstNonEmpty returns the first non-blank trimmed string.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

// SplitList splits comma/semicolon/pipe separated text into trimmed parts.
func SplitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || r == '/'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if t := strings.TrimSpace(f); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// DedupeStrings returns the input with duplicates (case-insensitive) removed,
// preserving first-seen order.
func DedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		t := strings.TrimSpace(s)
		if t == "" {
			continue
		}
		k := strings.ToLower(t)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, t)
	}
	return out
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
