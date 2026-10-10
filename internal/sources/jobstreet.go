package sources

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// JobStreet (SEEK) fetches the JSON search API that the JobStreet/SEEK front end
// itself calls:
//
//	GET https://{host}/api/jobsearch/v5/search?siteKey=ID-Main&keywords=…
//
// ACCESS DECISION (operator): this adapter exists because the operator asked for
// it with the restrictions below in full view. Anyone re-enabling this source
// inherits that decision knowingly.
//
//   - robots.txt (id.jobstreet.com, `User-agent: *`) disallows `*/job/`, `*?`,
//     `/graphql` and — exactly the endpoint used here — `/api/jobsearch/`.
//   - The Website Terms and Conditions (SEEK Limited, covering "SEEK, JobStreet
//     by SEEK or JobsDB by SEEK", last updated 20 July 2026) state: "You may not
//     use data mining, robots, screen scraping, or similar automated data
//     gathering, extraction or publication tools on our websites and apps …
//     except with our prior written consent", and "our websites and apps are
//     designed for individual human use. Except where we have provided a
//     specific technical interface (such as an API) … you must not use … any
//     automated process, script, tool, bot, scraper or other technical means
//     to … access, query, search, copy, collect, mine or harvest information".
//     That exception is SEEK's developer API, a hirer/ATS surface needing
//     partner onboarding — not a seeker-side feed.
//   - Individual job pages sit behind a Cloudflare JS challenge ("Just a
//     moment…", HTTP 403 to non-browser clients). This adapter does NOT try to
//     pass that challenge: it only calls the JSON search endpoint. The listing
//     URL it emits is therefore INFERRED from the board's own path shape
//     (/job/{id}) and could not be confirmed by fetch — it is deliberately not
//     listed in AttributesVerified.
//   - The list payload carries a `teaser`, not the full advertisement, and no
//     seniority or expiry, so those stay unknown rather than guessed.
//
// Verified endpoint behaviour (probed live 2026-10-10):
//
//   - siteKey selects the market: ID-Main / MY-Main / SG-Main (hosts
//     id./my./sg.jobstreet.com). "software engineer" -> 1419 (ID), 2262 (MY),
//     5420 (SG).
//   - `keywords` performs a server-side search; `where` is a place filter that
//     genuinely narrows results (Jakarta 804, Bandung 55).
//   - pageSize ceiling is 100: 200 is rejected with 400. `page` is 1-based and
//     pages do not overlap.
type JobStreet struct {
	Base
	// hostOverride points every market at one server (tests only).
	hostOverride string
}

// jobStreetMarket is one market SEEK serves under the JobStreet brand.
type jobStreetMarket struct {
	iso     string
	host    string
	siteKey string
	locale  string
}

var jobStreetMarkets = map[string]jobStreetMarket{
	"ID": {iso: "ID", host: "id.jobstreet.com", siteKey: "ID-Main", locale: "en-ID"},
	"MY": {iso: "MY", host: "my.jobstreet.com", siteKey: "MY-Main", locale: "en-MY"},
	"SG": {iso: "SG", host: "sg.jobstreet.com", siteKey: "SG-Main", locale: "en-SG"},
}

const (
	// jobStreetPageSize is the largest page the API accepts (200 -> HTTP 400).
	jobStreetPageSize = 100
	jobStreetPath     = "/api/jobsearch/v5/search"
)

// NewJobStreet builds the JobStreet adapter.
func NewJobStreet(opts ...Option) *JobStreet {
	return &JobStreet{Base: NewBase(opts...)}
}

func (j *JobStreet) ID() string { return "jobstreet" }

func (j *JobStreet) Caps() Caps {
	return Caps{
		// Individual job pages answer a Cloudflare challenge, so a listing
		// cannot be re-checked.
		SupportsVerification: false,
		ProvidesSalary:       true,  // salaryLabel
		ProvidesExpiry:       false, // not published by the list endpoint
		ProvidesSeniority:    false, // not published
	}
}

type jobStreetResponse struct {
	Data       []jobStreetJob `json:"data"`
	TotalCount int            `json:"totalCount"`
}

type jobStreetJob struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	CompanyName string `json:"companyName"`
	Teaser      string `json:"teaser"`
	SalaryLabel string `json:"salaryLabel"`
	ListingDate string `json:"listingDate"`
	RoleID      string `json:"roleId"`
	Advertiser  struct {
		Description string `json:"description"`
	} `json:"advertiser"`
	Employer struct {
		Name string `json:"name"`
	} `json:"employer"`
	Locations []struct {
		Label       string `json:"label"`
		CountryCode string `json:"countryCode"`
	} `json:"locations"`
	WorkTypes []string `json:"workTypes"`
	WorkArrangements struct {
		Data []struct {
			Label struct {
				Text string `json:"text"`
			} `json:"label"`
		} `json:"data"`
	} `json:"workArrangements"`
	Classifications []struct {
		Classification struct {
			Description string `json:"description"`
		} `json:"classification"`
		Subclassification struct {
			Description string `json:"description"`
		} `json:"subclassification"`
	} `json:"classifications"`
	BulletPoints []string `json:"bulletPoints"`
}

// jobStreetTargets resolves the markets a query should search, plus any
// requested country this source does not serve.
//
// One market per configured location: the Search Config can list Jakarta,
// Singapore and Malaysia, and each becomes its own siteKey request. An
// unserved country (anything but ID/MY/SG) is reported rather than silently
// answered with the home market's listings.
func jobStreetTargets(q Query) ([]jobStreetMarket, []string) {
	requested := q.Countries
	if len(requested) == 0 && strings.TrimSpace(q.Country) != "" {
		requested = []string{q.Country}
	}

	seen := make(map[string]struct{}, len(requested))
	markets := make([]jobStreetMarket, 0, len(requested))
	var unsupported []string

	for _, iso := range requested {
		key := strings.ToUpper(strings.TrimSpace(iso))
		if key == "" {
			continue
		}
		m, ok := jobStreetMarkets[key]
		if !ok {
			if _, dup := seen["!"+key]; !dup {
				seen["!"+key] = struct{}{}
				unsupported = append(unsupported, key)
			}
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		markets = append(markets, m)
	}

	// No location configured at all: search the home market rather than nothing.
	if len(markets) == 0 && len(requested) == 0 {
		markets = append(markets, jobStreetMarkets["ID"])
	}
	return markets, unsupported
}

func (j *JobStreet) Search(ctx context.Context, q Query) ([]RawListing, Report, error) {
	start := time.Now()
	rep := Report{SourceID: j.ID()}

	// The feed takes a single text term.
	text := strings.TrimSpace(strings.Join(q.Titles, " "))
	if text == "" {
		text = strings.TrimSpace(strings.Join(q.Keywords, " "))
	}

	markets, unsupported := jobStreetTargets(q)
	if len(markets) == 0 {
		rep.Status = "skipped"
		rep.Error = fmt.Sprintf(
			"jobstreet serves Indonesia (ID), Malaysia (MY) and Singapore (SG) only; requested %s",
			strings.Join(unsupported, ", "))
		rep.Duration = time.Since(start)
		return nil, rep, nil
	}

	// Split the per-run budget across markets so one run stays inside the
	// source timeout and the configured rate limit.
	budget := feedLimit(q.Limit)
	perMarket := budget / len(markets)
	if perMarket < 1 {
		perMarket = 1
	}

	all := make([]RawListing, 0, budget)
	var failures []string
	for _, m := range markets {
		if ctx.Err() != nil {
			break
		}
		listings, err := j.fetchMarket(ctx, m, text, q, perMarket)
		if err != nil {
			// One market failing must not discard the others.
			failures = append(failures, fmt.Sprintf("%s: %v", m.siteKey, err))
			continue
		}
		all = append(all, listings...)
	}
	if len(all) == 0 && len(failures) > 0 {
		rep.Status = "error"
		rep.Error = strings.Join(failures, "; ")
		rep.Duration = time.Since(start)
		return nil, rep, fmt.Errorf("jobstreet: %s", rep.Error)
	}

	kept, rejected := filterAndLimit(all, q, true)
	rep.Fetched = len(all)
	rep.Accepted = len(kept)
	rep.Rejected = rejected
	rep.Status = "ok"
	if len(failures) > 0 {
		rep.Status = "partial"
		rep.Error = strings.Join(failures, "; ")
	}
	if len(unsupported) > 0 {
		rep.Status = "partial"
		note := fmt.Sprintf("unserved markets requested: %s", strings.Join(unsupported, ", "))
		if rep.Error == "" {
			rep.Error = note
		} else {
			rep.Error += "; " + note
		}
	}
	rep.Duration = time.Since(start)
	return kept, rep, nil
}

// fetchMarket walks one market's pages until the per-market budget is met, the
// feed runs out, or the page ceiling is reached.
func (j *JobStreet) fetchMarket(ctx context.Context, m jobStreetMarket, text string, q Query, budget int) ([]RawListing, error) {
	pageSize := jobStreetPageSize
	if budget < pageSize {
		pageSize = budget
	}

	out := make([]RawListing, 0, pageSize)
	total := -1

	for page := 1; page <= maxPages; page++ {
		if len(out) >= budget {
			break
		}

		u := j.baseFor(m) + jobStreetPath
		u = appendQuery(u, "siteKey", m.siteKey)
		u = appendQuery(u, "sourcesystem", "houston")
		u = appendQuery(u, "locale", m.locale)
		u = appendQuery(u, "page", strconv.Itoa(page))
		u = appendQuery(u, "pageSize", strconv.Itoa(pageSize))
		if text != "" {
			u = appendQuery(u, "keywords", url.QueryEscape(text))
		}
		// `where` is only meaningful in the market the configured city belongs
		// to; sending it to another market would filter it by a foreign place.
		if where := j.whereFor(m, q); where != "" {
			u = appendQuery(u, "where", url.QueryEscape(where))
		}

		var resp jobStreetResponse
		if err := j.Fetcher.GetJSON(ctx, j.ID(), u, &resp); err != nil {
			return out, err
		}
		if len(resp.Data) == 0 {
			break
		}
		if total < 0 {
			total = resp.TotalCount
		}
		for _, raw := range resp.Data {
			out = append(out, j.mapJob(m, raw))
		}
		if len(resp.Data) < pageSize {
			break
		}
		if total > 0 && page*pageSize >= total {
			break
		}
	}
	return out, nil
}

func (j *JobStreet) whereFor(m jobStreetMarket, q Query) string {
	city := strings.TrimSpace(q.City)
	if city == "" {
		return ""
	}
	if iso := strings.ToUpper(strings.TrimSpace(q.Country)); iso != "" && iso != m.iso {
		return ""
	}
	return city
}

// baseFor returns the origin to call for a market. hostOverride (tests) points
// every market at one server.
func (j *JobStreet) baseFor(m jobStreetMarket) string {
	if j.hostOverride != "" {
		return j.hostOverride
	}
	return "https://" + m.host
}

func (j *JobStreet) mapJob(m jobStreetMarket, raw jobStreetJob) RawListing {
	rl := RawListing{
		SourceID:    j.ID(),
		SourceJobID: strings.TrimSpace(raw.ID),
		// The board's own path shape, built from the fields the payload carries
		// (id, roleId). NOT verified — /job/{id} answers a Cloudflare challenge
		// to non-browser clients — so it stays out of AttributesVerified.
		URL:         fmt.Sprintf("https://%s/job/%s", m.host, url.PathEscape(strings.TrimSpace(raw.ID))),
		Title:       strings.TrimSpace(raw.Title),
		Company:     firstNonEmpty(raw.CompanyName, raw.Advertiser.Description, raw.Employer.Name),
		Description: summarize(raw.Teaser),
		Raw:         mustRaw(raw),
	}
	if rl.Title != "" {
		rl.Verify(AttrTitle)
	}
	if rl.Company != "" {
		rl.Verify(AttrCompany)
	}
	if rl.Description != "" {
		rl.Verify(AttrDescription)
	}

	if len(raw.Locations) > 0 {
		loc := raw.Locations[0]
		rl.LocationRaw = strings.TrimSpace(loc.Label)
		if rl.LocationRaw != "" {
			// SEEK labels the place as "City, Region".
			parts := strings.SplitN(rl.LocationRaw, ",", 2)
			rl.City = strings.TrimSpace(parts[0])
			if len(parts) > 1 {
				rl.Region = strings.TrimSpace(parts[1])
			}
			rl.Verify(AttrLocation)
		}
		rl.Country = strings.ToUpper(strings.TrimSpace(loc.CountryCode))
	}

	if arr := jobStreetWorkArrangement(jobStreetArrangementText(raw)); arr != "" {
		rl.WorkArrangement = arr
		rl.Verify(AttrWorkArrangement)
	}
	if et := jobStreetEmploymentType(raw.WorkTypes); et != "" {
		rl.EmploymentType = et
		rl.Verify(AttrEmploymentType)
	}
	if min, max, currency, period := parseJobStreetSalary(raw.SalaryLabel); min != nil || max != nil {
		rl.SalaryMin, rl.SalaryMax = min, max
		rl.SalaryCurrency, rl.SalaryPeriod = currency, period
		rl.Verify(AttrSalary)
	}
	if ts := parseTimeAny(raw.ListingDate); ts != nil {
		rl.PublishedAt = ts
		rl.Verify(AttrPublishedAt)
	}

	// SEEK classifications carry the industry and the specialisation.
	for _, c := range raw.Classifications {
		if d := strings.TrimSpace(c.Classification.Description); d != "" {
			rl.Tags = append(rl.Tags, d)
		}
		if d := strings.TrimSpace(c.Subclassification.Description); d != "" {
			rl.Tags = append(rl.Tags, d)
		}
	}
	rl.Tags = DedupeStrings(rl.Tags)

	// Seniority and expiry are not published by the list endpoint: left unknown
	// and unverified rather than inferred.
	return rl
}

func jobStreetArrangementText(raw jobStreetJob) string {
	for _, a := range raw.WorkArrangements.Data {
		if t := strings.TrimSpace(a.Label.Text); t != "" {
			return t
		}
	}
	return ""
}

func jobStreetWorkArrangement(text string) string {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "on-site", "onsite", "on site", "office":
		return "onsite"
	case "remote":
		return "remote"
	case "hybrid":
		return "hybrid"
	default:
		return ""
	}
}

// jobStreetEmploymentType maps SEEK workTypes ("Full time", "Part time",
// "Contract/Temp", "Casual", "Internship") to the canonical set.
func jobStreetEmploymentType(types []string) string {
	for _, t := range types {
		l := strings.ToLower(strings.TrimSpace(t))
		switch {
		case l == "":
			continue
		case strings.Contains(l, "full"):
			return "full_time"
		case strings.Contains(l, "part"):
			return "part_time"
		case strings.Contains(l, "contract"):
			return "contract"
		case strings.Contains(l, "casual"), strings.Contains(l, "temp"):
			return "temporary"
		case strings.Contains(l, "intern"):
			return "internship"
		}
	}
	return ""
}

var jobStreetAmountRe = regexp.MustCompile(`\d[\d.,]*`)

// parseJobStreetSalary reads the human-readable salaryLabel, e.g.
// "Rp 6.500.000 – Rp 8.000.000 per month". "Up to X" yields only a maximum and
// "From X" only a minimum, so an unstated bound is never invented.
func parseJobStreetSalary(label string) (min, max *float64, currency, period string) {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil, nil, "", ""
	}
	currency = jobStreetCurrency(label)
	period = normalizeSalaryPeriod(label)

	values := make([]float64, 0, 2)
	for _, raw := range jobStreetAmountRe.FindAllString(label, -1) {
		v, ok := jobStreetAmount(raw)
		if !ok {
			continue
		}
		values = append(values, v)
	}

	switch len(values) {
	case 0:
		return nil, nil, currency, period
	case 1:
		lower := strings.ToLower(label)
		if strings.Contains(lower, "up to") || strings.Contains(lower, "max") {
			return nil, &values[0], currency, period
		}
		return &values[0], nil, currency, period
	default:
		low, high := values[0], values[1]
		if high < low {
			low, high = high, low
		}
		return &low, &high, currency, period
	}
}

// jobStreetAmount turns a grouped figure into a number. SEEK groups thousands
// with "." in Indonesia and "," in Singapore/Malaysia; both separators are
// stripped, so 6.500.000 and 6,500,000 both read as 6500000.
func jobStreetAmount(raw string) (float64, bool) {
	cleaned := strings.NewReplacer(",", "", ".", "", "\u00a0", "", " ", "").Replace(raw)
	v, err := strconv.ParseFloat(cleaned, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

func jobStreetCurrency(label string) string {
	l := strings.ToLower(label)
	switch {
	case strings.Contains(l, "rp"), strings.Contains(l, "idr"):
		return "IDR"
	case strings.Contains(l, "s$"), strings.Contains(l, "sgd"):
		return "SGD"
	case strings.Contains(l, "rm"), strings.Contains(l, "myr"):
		return "MYR"
	case strings.Contains(l, "us$"), strings.Contains(l, "usd"):
		return "USD"
	case strings.Contains(l, "php"), strings.Contains(l, "₱"):
		return "PHP"
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}
