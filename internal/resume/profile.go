package resume

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/valeriandwipurnomo/jobseeker/internal/sources"
)

// Provenance values. Only "confirmed" values may be treated as user
// preferences; "inferred" values are a draft the user must accept or correct.
const (
	ProvenanceInferred  = "inferred"
	ProvenanceConfirmed = "confirmed"
)

// Field is one extracted profile field with its provenance.
type Field struct {
	Value      any     `json:"value"`
	Provenance string  `json:"provenance"`
	Confidence float64 `json:"confidence"`
}

// Draft is the machine-extracted profile awaiting user review.
type Draft struct {
	Fields    map[string]Field `json:"fields"`
	TextChars int              `json:"text_chars"`
	Warnings  []string         `json:"warnings"`
}

// Field keys used in the draft.
const (
	FieldFullName        = "full_name"
	FieldHeadline        = "headline"
	FieldCurrentTitle    = "current_title"
	FieldYearsExperience = "years_experience"
	FieldSeniority       = "seniority"
	FieldSkills          = "skills"
	FieldJobTitles       = "job_titles"
	FieldIndustries      = "industries"
	FieldEducation       = "education"
	FieldCertifications  = "certifications"
	FieldLanguages       = "languages"
	FieldProjects        = "projects"
)

// ExtractProfile builds a draft profile from CV text. It is deterministic: the
// same text always produces the same draft, which is what makes it testable.
func ExtractProfile(text string, now time.Time) Draft {
	text = NormalizeText(text)
	lines := nonEmptyLines(text)
	d := Draft{Fields: map[string]Field{}, TextChars: len([]rune(text))}

	if name := guessName(lines); name != "" {
		d.Fields[FieldFullName] = Field{Value: name, Provenance: ProvenanceInferred, Confidence: 0.6}
	}
	if title := guessCurrentTitle(lines); title != "" {
		d.Fields[FieldCurrentTitle] = Field{Value: title, Provenance: ProvenanceInferred, Confidence: 0.65}
		if s := sources.NormalizeSeniority(title); s != "unknown" {
			d.Fields[FieldSeniority] = Field{Value: s, Provenance: ProvenanceInferred, Confidence: 0.7}
		}
	}
	if yrs, ok := guessYearsExperience(text, now); ok {
		d.Fields[FieldYearsExperience] = Field{Value: yrs, Provenance: ProvenanceInferred, Confidence: 0.55}
	}
	if skills := guessSkills(text); len(skills) > 0 {
		d.Fields[FieldSkills] = Field{Value: skills, Provenance: ProvenanceInferred, Confidence: 0.75}
	}
	if titles := guessJobTitles(lines); len(titles) > 0 {
		d.Fields[FieldJobTitles] = Field{Value: titles, Provenance: ProvenanceInferred, Confidence: 0.6}
	}
	if inds := guessIndustries(text); len(inds) > 0 {
		d.Fields[FieldIndustries] = Field{Value: inds, Provenance: ProvenanceInferred, Confidence: 0.5}
	}
	if edu := guessEducation(lines); len(edu) > 0 {
		d.Fields[FieldEducation] = Field{Value: edu, Provenance: ProvenanceInferred, Confidence: 0.6}
	}
	if certs := guessCertifications(lines); len(certs) > 0 {
		d.Fields[FieldCertifications] = Field{Value: certs, Provenance: ProvenanceInferred, Confidence: 0.6}
	}
	if langs := guessLanguages(text); len(langs) > 0 {
		d.Fields[FieldLanguages] = Field{Value: langs, Provenance: ProvenanceInferred, Confidence: 0.5}
	}
	if projects := guessProjects(lines); len(projects) > 0 {
		d.Fields[FieldProjects] = Field{Value: projects, Provenance: ProvenanceInferred, Confidence: 0.45}
	}

	d.Warnings = append(d.Warnings,
		"All extracted values are inferred drafts. Review and confirm them before they are used for matching.")
	if len(d.Fields) == 0 {
		d.Warnings = append(d.Warnings, "No profile fields could be extracted; the CV text may use an unusual layout.")
	}
	if regexp.MustCompile(`(?i)[\w.+-]+@[\w-]+\.[\w.]+`).MatchString(text) {
		d.Warnings = append(d.Warnings, "Contact details were detected but are deliberately not stored in the profile.")
	}
	return d
}

var (
	reEmail      = regexp.MustCompile(`(?i)[\w.+-]+@[\w-]+\.[\w.]+`)
	rePhone      = regexp.MustCompile(`(?:\+?\d[\d\s().-]{7,}\d)`)
	reLinkedIn   = regexp.MustCompile(`(?i)linkedin\.com/\S+`)
	reMonthYear  = regexp.MustCompile(`(?i)\b(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+((?:19|20)\d{2})\b`)
	reYearRange  = regexp.MustCompile(`\b((?:19|20)\d{2})\s*(?:-|–|—|to)\s*((?:19|20)\d{2}|present|current|now)\b`)
	reBullet     = regexp.MustCompile(`^\s*(?:[-•*·▪◦]|\d+[.)])\s+`)
	reYear       = regexp.MustCompile(`\b((?:19|20)\d{2})\b`)
	reWhitespace = regexp.MustCompile(`\s+`)
)

// firstYear returns the first plausible year found in s.
func firstYear(s string) time.Time {
	m := reYear.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}
	}
	return parseYear(m[1])
}

// Section headings used to slice the document.
var sectionHeadings = map[string]string{
	"experience": "experience", "work experience": "experience", "employment": "experience",
	"professional experience": "experience", "work history": "experience",
	"education": "education", "academic background": "education", "qualifications": "education",
	"skills": "skills", "technical skills": "skills", "core competencies": "skills",
	"technologies": "skills", "tech stack": "skills",
	"projects": "projects", "selected projects": "projects", "personal projects": "projects",
	"certifications": "certifications", "certificates": "certifications", "licenses": "certifications",
	"languages": "languages", "summary": "summary", "profile": "summary", "about": "summary",
	"objective": "summary", "achievements": "achievements",
}

func nonEmptyLines(text string) []string {
	raw := strings.Split(text, "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func guessName(lines []string) string {
	for i, l := range lines {
		if i > 6 {
			break
		}
		if reEmail.MatchString(l) || rePhone.MatchString(l) || reLinkedIn.MatchString(l) {
			continue
		}
		clean := strings.TrimSpace(l)
		if len([]rune(clean)) < 3 || len([]rune(clean)) > 60 {
			continue
		}
		if sectionHeadings[strings.ToLower(clean)] != "" {
			continue
		}
		if strings.ContainsAny(clean, "0123456789@/|:") {
			continue
		}
		words := strings.Fields(clean)
		if len(words) < 2 || len(words) > 5 {
			continue
		}
		allCapsOrTitle := true
		for _, w := range words {
			if w == strings.ToLower(w) && !isAllCaps(w) {
				allCapsOrTitle = false
				break
			}
		}
		if !allCapsOrTitle {
			continue
		}
		return toTitleCase(clean)
	}
	return ""
}

func isAllCaps(s string) bool {
	hasLetter := false
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return false
		}
		if r >= 'A' && r <= 'Z' {
			hasLetter = true
		}
	}
	return hasLetter
}

func toTitleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// titleKeywords are role words that indicate a professional title line.
var titleKeywords = []string{
	"engineer", "developer", "programmer", "architect", "analyst", "scientist",
	"manager", "director", "lead", "consultant", "designer", "administrator",
	"specialist", "intern", "officer", "executive", "supervisor", "coordinator",
	"qa", "devops", "sre", "data", "product owner", "scrum master", "cto", "ceo",
}

func guessCurrentTitle(lines []string) string {
	for i, l := range lines {
		if i > 15 {
			break
		}
		clean := strings.TrimSpace(reBullet.ReplaceAllString(l, ""))
		if len([]rune(clean)) < 4 || len([]rune(clean)) > 90 {
			continue
		}
		if sectionHeadings[strings.ToLower(clean)] != "" {
			continue
		}
		if reEmail.MatchString(clean) || rePhone.MatchString(clean) {
			continue
		}
		lower := strings.ToLower(clean)
		for _, kw := range titleKeywords {
			if strings.Contains(lower, kw) {
				// Strip a trailing "at Company" / "| Company" qualifier.
				if idx := strings.Index(lower, " at "); idx > 0 {
					clean = clean[:idx]
				}
				if idx := strings.Index(clean, "|"); idx > 0 {
					clean = strings.TrimSpace(clean[:idx])
				}
				return strings.TrimSpace(clean)
			}
		}
	}
	return ""
}

// guessYearsExperience estimates total professional experience from employment
// date ranges. Overlapping ranges are unioned so contract overlaps do not
// double-count.
func guessYearsExperience(text string, now time.Time) (float64, bool) {
	type span struct{ start, end time.Time }
	var spans []span

	for _, m := range reYearRange.FindAllStringSubmatch(text, -1) {
		start := parseYear(m[1])
		if start.IsZero() {
			continue
		}
		var end time.Time
		switch strings.ToLower(m[2]) {
		case "present", "current", "now":
			end = now
		default:
			end = parseYear(m[2])
		}
		if end.IsZero() || end.Before(start) {
			continue
		}
		spans = append(spans, span{start: start, end: end})
	}

	// Month-precision ranges, e.g. "Jan 2019 - Mar 2022" or "Mar 2020 - Present".
	// Each month-year match is examined to see whether a range separator follows
	// it, which is what distinguishes an employment period from a stray date.
	for _, m := range reMonthYear.FindAllStringSubmatchIndex(text, -1) {
		start := parseMonthYear(text[m[0]:m[1]])
		if start.IsZero() {
			continue
		}
		rest := text[m[1]:]
		if len(rest) > 40 {
			rest = rest[:40]
		}
		lowerRest := strings.ToLower(rest)
		sepEnd := -1
		for _, sep := range []string{" - ", " – ", " — ", " to ", " until ", " s/d ", " until ", "-", "–", "—"} {
			if i := strings.Index(lowerRest, sep); i >= 0 && i <= 3 {
				sepEnd = i + len(sep)
				break
			}
		}
		if sepEnd < 0 {
			continue
		}
		tail := strings.TrimSpace(rest[sepEnd:])
		lowerTail := strings.ToLower(tail)

		var end time.Time
		switch {
		case strings.HasPrefix(lowerTail, "present"), strings.HasPrefix(lowerTail, "current"), strings.HasPrefix(lowerTail, "now"):
			end = now
		default:
			end = parseMonthYear(tail)
			if end.IsZero() {
				end = firstYear(tail)
			}
		}
		if end.IsZero() || end.Before(start) {
			continue
		}
		spans = append(spans, span{start: start, end: end})
	}

	if len(spans) == 0 {
		return 0, false
	}

	sort.Slice(spans, func(i, j int) bool { return spans[i].start.Before(spans[j].start) })
	total := time.Duration(0)
	cur := spans[0]
	for _, s := range spans[1:] {
		if s.start.After(cur.end) {
			total += cur.end.Sub(cur.start)
			cur = s
			continue
		}
		if s.end.After(cur.end) {
			cur.end = s.end
		}
	}
	total += cur.end.Sub(cur.start)

	years := total.Hours() / (24 * 365.25)
	if years <= 0 {
		return 0, false
	}
	if years > 60 {
		years = 60
	}
	return round1(years), true
}

func parseYear(s string) time.Time {
	y, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || y < 1950 || y > 2100 {
		return time.Time{}
	}
	return time.Date(y, time.January, 1, 0, 0, 0, 0, time.UTC)
}

var monthNames = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March, "apr": time.April,
	"may": time.May, "jun": time.June, "jul": time.July, "aug": time.August,
	"sep": time.September, "oct": time.October, "nov": time.November, "dec": time.December,
}

func parseMonthYear(s string) time.Time {
	m := reMonthYear.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return time.Time{}
	}
	mon, ok := monthNames[strings.ToLower(m[1][:3])]
	if !ok {
		return time.Time{}
	}
	y, err := strconv.Atoi(m[2])
	if err != nil {
		return time.Time{}
	}
	return time.Date(y, mon, 1, 0, 0, 0, 0, time.UTC)
}

// skillDictionary is a curated, extendable list. Matching against a fixed
// dictionary is what makes skill extraction deterministic and auditable.
var skillDictionary = []string{
	"React", "React Native", "Next.js", "Vue", "Angular", "Svelte", "TypeScript", "JavaScript",
	"HTML", "CSS", "Sass", "Tailwind CSS", "Redux", "Zustand", "React Query", "Vite", "Webpack",
	"Node.js", "Express", "NestJS", "Go", "Golang", "Python", "Django", "Flask", "FastAPI",
	"Java", "Spring Boot", "Kotlin", "Swift", "Objective-C", "C#", "C++", ".NET", "PHP", "Laravel",
	"Ruby", "Ruby on Rails", "Rust", "Elixir", "Scala", "Perl", "Dart", "Flutter",
	"PostgreSQL", "MySQL", "MariaDB", "SQLite", "MongoDB", "Redis", "Elasticsearch", "Cassandra",
	"DynamoDB", "Oracle", "SQL Server", "Snowflake", "BigQuery", "ClickHouse", "Neo4j",
	"Kafka", "RabbitMQ", "NATS", "gRPC", "GraphQL", "REST", "WebSocket", "OpenAPI", "Swagger",
	"Docker", "Kubernetes", "Helm", "Terraform", "Ansible", "Pulumi", "Jenkins", "GitHub Actions",
	"GitLab CI", "CircleCI", "ArgoCD", "AWS", "GCP", "Azure", "Cloudflare", "Vercel", "Heroku",
	"Linux", "Unix", "Bash", "Shell", "Git", "GitHub", "GitLab", "Bitbucket", "Jira", "Confluence",
	"Prometheus", "Grafana", "Datadog", "OpenTelemetry", "Sentry", "ELK", "Loki",
	"CI/CD", "TDD", "BDD", "Unit Testing", "Integration Testing", "Jest", "Vitest", "Playwright",
	"Cypress", "Selenium", "GoTest", "Pytest", "JUnit",
	"Microservices", "Event-Driven Architecture", "Domain-Driven Design", "Clean Architecture",
	"SOLID", "Design Patterns", "System Design", "Distributed Systems", "High Availability",
	"Performance Optimization", "Caching", "Load Balancing", "Message Queues", "Serverless",
	"Machine Learning", "Deep Learning", "TensorFlow", "PyTorch", "Pandas", "NumPy", "scikit-learn",
	"LLM", "NLP", "Computer Vision", "Data Engineering", "ETL", "Airflow", "dbt", "Spark", "Hadoop",
	"Tableau", "Power BI", "Looker", "Metabase", "Excel", "Google Analytics", "Amplitude", "Mixpanel",
	"Figma", "Sketch", "Adobe XD", "Photoshop", "Illustrator", "Accessibility", "WCAG",
	"Agile", "Scrum", "Kanban", "SAFe", "Product Management", "Stakeholder Management",
	"Project Management", "Team Leadership", "Mentoring", "Code Review", "Technical Documentation",
	"Open Source", "OWASP", "Penetration Testing", "Cryptography", "OAuth", "JWT", "SAML", "SSO",
	"Payment Gateway", "Stripe", "Midtrans", "Xendit", "Shopify", "WooCommerce", "Salesforce",
	"SAP", "Oracle ERP", "ERP", "CRM", "HRIS", "Payroll", "Accounting", "Financial Modeling",
	"Customer Support", "Sales", "Marketing", "SEO", "SEM", "Content Writing", "Copywriting",
	"UI/UX", "User Research", "Wireframing", "Prototyping", "Design System", "Responsive Design",
	"Mobile Development", "iOS", "Android", "Firebase", "Supabase", "App Store", "Play Store",
}

// skillEntry indexes one dictionary skill. allowCompact is set only for skills
// whose canonical spelling contains punctuation or spaces (Node.js, CI/CD,
// Ruby on Rails): for those we also match the compacted form. Single-word
// skills are matched on word boundaries only, so "Java" never matches
// "JavaScript" and "Go" never matches "logo".
type skillEntry struct {
	canonical    string
	allowCompact bool
}

var skillIndex map[string]skillEntry

func init() {
	skillIndex = make(map[string]skillEntry, len(skillDictionary))
	for _, s := range skillDictionary {
		key := strings.ToLower(s)
		allowCompact := strings.ContainsAny(s, ".-/ ") && len(s) >= 4
		skillIndex[key] = skillEntry{canonical: s, allowCompact: allowCompact}
		if allowCompact {
			compact := strings.NewReplacer(".", "", "-", "", " ", "", "/", "").Replace(key)
			if len(compact) >= 4 {
				skillIndex[compact] = skillEntry{canonical: s, allowCompact: false}
			}
		}
	}
}

func guessSkills(text string) []string {
	lower := strings.ToLower(text)
	compactText := strings.NewReplacer(".", "", "-", "", " ", "", "/", "").Replace(lower)
	found := map[string]struct{}{}
	for key, entry := range skillIndex {
		if len(key) < 2 {
			continue
		}
		matched := wordMatch(lower, key)
		if !matched && entry.allowCompact && len(key) >= 4 {
			matched = strings.Contains(compactText, key)
		}
		if matched {
			found[entry.canonical] = struct{}{}
		}
	}
	out := make([]string, 0, len(found))
	for s := range found {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// wordMatch reports whether needle appears in haystack bounded by non-alphanumeric
// characters, so "go" does not match "logo" and "java" does not match "javascript".
func wordMatch(haystack, needle string) bool {
	idx := 0
	for {
		i := strings.Index(haystack[idx:], needle)
		if i < 0 {
			return false
		}
		pos := idx + i
		before := byte(' ')
		if pos > 0 {
			before = haystack[pos-1]
		}
		after := byte(' ')
		if pos+len(needle) < len(haystack) {
			after = haystack[pos+len(needle)]
		}
		if !isWordByte(before) && !isWordByte(after) {
			return true
		}
		idx = pos + 1
		if idx >= len(haystack) {
			return false
		}
	}
}

func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func guessJobTitles(lines []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, l := range lines {
		clean := strings.TrimSpace(reBullet.ReplaceAllString(l, ""))
		if idx := strings.Index(clean, "|"); idx > 0 {
			clean = strings.TrimSpace(clean[:idx])
		}
		if idx := strings.Index(strings.ToLower(clean), " at "); idx > 0 {
			clean = strings.TrimSpace(clean[:idx])
		}
		lower := strings.ToLower(clean)
		if len([]rune(clean)) > 60 {
			continue
		}
		hasDate := reYearRange.MatchString(clean) || reMonthYear.MatchString(clean)
		matched := false
		for _, kw := range titleKeywords {
			if strings.Contains(lower, kw) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if !hasDate && len([]rune(clean)) > 45 {
			continue
		}
		if _, dup := seen[strings.ToLower(clean)]; dup {
			continue
		}
		seen[strings.ToLower(clean)] = struct{}{}
		out = append(out, clean)
		if len(out) >= 12 {
			break
		}
	}
	return out
}

var industryKeywords = map[string][]string{
	"Fintech":            {"fintech", "financial technology", "payment", "banking", "lending"},
	"Banking":            {"bank", "banking"},
	"E-commerce":         {"e-commerce", "ecommerce", "marketplace", "online retail"},
	"Logistics":          {"logistics", "supply chain", "shipping", "freight", "fulfillment"},
	"Healthcare":         {"healthcare", "health tech", "hospital", "clinic", "medical"},
	"Education":          {"education", "edtech", "school", "university", "learning platform"},
	"SaaS":               {"saas", "b2b software", "cloud platform"},
	"Telecommunications": {"telco", "telecommunications", "telecom"},
	"Insurance":          {"insurance", "insurtech"},
	"Travel":             {"travel", "hospitality", "tourism", "airline"},
	"Manufacturing":      {"manufacturing", "factory", "industrial"},
	"Energy":             {"energy", "oil and gas", "renewable", "utility"},
	"Retail":             {"retail", "supermarket", "consumer goods", "fmcg"},
	"Media":              {"media", "entertainment", "streaming", "publishing"},
	"Government":         {"government", "public sector", "ministry", "municipal"},
	"Consulting":         {"consulting", "consultancy", "advisory"},
	"Real Estate":        {"real estate", "property", "proptech"},
	"Mining":             {"mining", "mineral", "smelter"},
	"Agriculture":        {"agriculture", "agritech", "farming", "plantation"},
	"Automotive":         {"automotive", "vehicle", "mobility"},
}

func guessIndustries(text string) []string {
	lower := strings.ToLower(text)
	var out []string
	for industry, keys := range industryKeywords {
		for _, k := range keys {
			if strings.Contains(lower, k) {
				out = append(out, industry)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

func guessEducation(lines []string) []string {
	var out []string
	for _, l := range lines {
		lower := strings.ToLower(l)
		if containsAny(lower, "bachelor", "master", "doctorate", "phd", "b.sc", "m.sc", "bsc", "msc",
			"b.eng", "m.eng", "beng", "meng", "s1", "s2", "s3", "sarjana", "diploma", "associate degree") ||
			(containsAny(lower, "university", "institute of technology", "college", "universitas") && len([]rune(l)) < 120) {
			out = append(out, strings.TrimSpace(reBullet.ReplaceAllString(l, "")))
		}
		if len(out) >= 8 {
			break
		}
	}
	return dedupe(out)
}

func guessCertifications(lines []string) []string {
	var out []string
	for _, l := range lines {
		lower := strings.ToLower(l)
		if containsAny(lower, "certified", "certificate", "certification", "aws certified",
			"scrum master", "pmp", "itil", "comptia", "ccna", "cissp", "cfa", "acca") {
			out = append(out, strings.TrimSpace(reBullet.ReplaceAllString(l, "")))
		}
		if len(out) >= 10 {
			break
		}
	}
	return dedupe(out)
}

var languageNames = []string{"English", "Indonesian", "Bahasa Indonesia", "Mandarin", "Chinese", "Japanese",
	"Korean", "Spanish", "French", "German", "Dutch", "Arabic", "Hindi", "Malay", "Thai", "Vietnamese",
	"Portuguese", "Italian", "Russian", "Turkish", "Tagalog", "Javanese", "Sundanese"}

func guessLanguages(text string) []string {
	lower := strings.ToLower(text)
	found := map[string]struct{}{}
	for _, name := range languageNames {
		if wordMatch(lower, strings.ToLower(name)) {
			found[name] = struct{}{}
		}
	}
	if _, ok := found["Bahasa Indonesia"]; ok {
		delete(found, "Indonesian")
	}
	out := make([]string, 0, len(found))
	for l := range found {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

func guessProjects(lines []string) []string {
	var out []string
	inProjects := false
	for _, l := range lines {
		key := strings.TrimSpace(strings.TrimRight(strings.ToLower(l), ": "))
		if section, ok := sectionHeadings[key]; ok {
			inProjects = section == "projects"
			continue
		}
		if !inProjects {
			continue
		}
		clean := strings.TrimSpace(reBullet.ReplaceAllString(l, ""))
		if clean == "" || len([]rune(clean)) > 160 {
			continue
		}
		out = append(out, clean)
		if len(out) >= 10 {
			break
		}
	}
	return dedupe(out)
}

func dedupe(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(reWhitespace.ReplaceAllString(s, " "))
		if s == "" {
			continue
		}
		k := strings.ToLower(s)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, s)
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

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}
