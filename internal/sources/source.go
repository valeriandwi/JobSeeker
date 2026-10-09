// Package sources defines the job-source adapter contract and the shared HTTP
// fetcher every adapter uses.
//
// Design rules enforced by these types:
//   - Adapters never invent data. A field is either parsed from the source
//     payload or left zero, and every field the source actually published is
//     listed in RawListing.AttributesVerified.
//   - Adapters are pure transport + mapping. Normalization semantics live in
//     this package's helpers so all sources behave identically.
//   - A failing source never fails a run: Search returns a Report that records
//     the failure alongside whatever partial data arrived.
package sources

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"
)

// RawListing is one vacancy as retrieved from a source, before normalization
// into the canonical jobs table.
type RawListing struct {
	SourceID    string
	SourceJobID string
	URL         string
	ApplyURL    string

	Title       string
	Company     string
	Description string // plain text; adapters must strip HTML

	LocationRaw string
	City        string
	Region      string
	Country     string // ISO 3166-1 alpha-2 when the source states it

	WorkArrangement string // remote|onsite|hybrid|unknown
	EmploymentType  string // permanent|full_time|contract|part_time|internship|temporary|unknown
	Seniority       string // intern|junior|mid|senior|lead|principal|unknown

	SalaryMin      *float64
	SalaryMax      *float64
	SalaryCurrency string // ISO 4217
	SalaryPeriod   string // year|month|day|hour|""

	PublishedAt *time.Time
	ExpiresAt   *time.Time

	Tags []string

	// AttributesVerified lists the field names the source genuinely published
	// (e.g. "work_arrangement", "salary", "expires_at"). Downstream matching
	// uses this to decide between "does not match" and "unknown".
	AttributesVerified []string

	Raw json.RawMessage
}

// Attribute names used in RawListing.AttributesVerified.
const (
	AttrTitle           = "title"
	AttrCompany         = "company"
	AttrDescription     = "description"
	AttrLocation        = "location"
	AttrWorkArrangement = "work_arrangement"
	AttrEmploymentType  = "employment_type"
	AttrSeniority       = "seniority"
	AttrSalary          = "salary"
	AttrPublishedAt     = "published_at"
	AttrExpiresAt       = "expires_at"
)

// Verify marks an attribute as published by the source.
func (r *RawListing) Verify(attrs ...string) {
	seen := make(map[string]struct{}, len(r.AttributesVerified)+len(attrs))
	for _, a := range r.AttributesVerified {
		seen[a] = struct{}{}
	}
	for _, a := range attrs {
		if _, dup := seen[a]; dup {
			continue
		}
		seen[a] = struct{}{}
		r.AttributesVerified = append(r.AttributesVerified, a)
	}
	sort.Strings(r.AttributesVerified)
}

// Has reports whether an attribute was verified by the source.
func (r RawListing) Has(attr string) bool {
	for _, a := range r.AttributesVerified {
		if a == attr {
			return true
		}
	}
	return false
}

// Query is the search request handed to every adapter. Adapters must translate
// what they can and ignore the rest; unsupported filters must not cause errors.
type Query struct {
	Titles          []string
	Keywords        []string
	ExcludeKeywords []string
	Country         string // ISO alpha-2 preference, best effort
	City            string
	IncludeRemote   bool
	Limit           int // per-source cap; adapters must honour it
	Watchlist       []Watchlist
	Now             time.Time
}

// Watchlist is a company board to fetch for ATS-style adapters.
type Watchlist struct {
	SourceID  string
	BoardSlug string
	Company   string
}

// Caps describes what a source can actually provide.
type Caps struct {
	SupportsVerification bool // can we re-check a listing's existence?
	ProvidesSalary       bool
	ProvidesExpiry       bool
	ProvidesSeniority    bool
	RequiresWatchlist    bool // only returns results for configured boards
	RequiresKey          bool
}

// Report summarises one adapter's contribution to a run.
type Report struct {
	SourceID string
	Fetched  int
	Accepted int
	Rejected int
	Status   string // ok|partial|error|skipped
	Error    string
	Duration time.Duration
}

// Adapter fetches vacancies from one source.
type Adapter interface {
	// ID returns the stable source identifier matching job_sources.id.
	ID() string
	// Caps describes source capabilities.
	Caps() Caps
	// Search retrieves listings. Returning an error means the source failed
	// entirely; a partial success must be reported through Report instead.
	Search(ctx context.Context, q Query) ([]RawListing, Report, error)
}

// Registry holds the adapters available to the application.
type Registry struct {
	adapters map[string]Adapter
	order    []string
}

// NewRegistry builds a registry from adapters. Duplicate ids panic at startup:
// a duplicate source id is a programming error, not a runtime condition.
func NewRegistry(adapters ...Adapter) *Registry {
	r := &Registry{adapters: make(map[string]Adapter, len(adapters))}
	for _, a := range adapters {
		if a == nil {
			continue
		}
		id := a.ID()
		if _, dup := r.adapters[id]; dup {
			panic("sources: duplicate adapter id " + id)
		}
		r.adapters[id] = a
		r.order = append(r.order, id)
	}
	sort.Strings(r.order)
	return r
}

// Get returns an adapter by id.
func (r *Registry) Get(id string) (Adapter, bool) {
	a, ok := r.adapters[id]
	return a, ok
}

// IDs returns the registered ids in stable order.
func (r *Registry) IDs() []string { return append([]string(nil), r.order...) }

// All returns the adapters in stable id order.
func (r *Registry) All() []Adapter {
	out := make([]Adapter, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.adapters[id])
	}
	return out
}

// Len returns the number of registered adapters.
func (r *Registry) Len() int { return len(r.order) }

// Option configures an adapter's HTTP behaviour.
type Option func(*Base)

// Base provides the shared fields and helpers every adapter embeds.
type Base struct {
	Fetcher *Fetcher
}

// WithFetcher sets the shared fetcher.
func WithFetcher(f *Fetcher) Option { return func(b *Base) { b.Fetcher = f } }

// NewBase builds a Base honouring the options.
func NewBase(opts ...Option) Base {
	b := Base{Fetcher: NewFetcher(nil)}
	for _, o := range opts {
		o(&b)
	}
	return b
}

// HTTPClient exposes the fetcher's client (used by adapters overriding paths).
func (b *Base) HTTPClient() *http.Client { return b.Fetcher.Client }
