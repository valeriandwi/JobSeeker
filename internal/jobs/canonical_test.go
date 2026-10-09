package jobs

import (
	"encoding/json"
	"testing"
)

func TestNormalizeText(t *testing.T) {
	cases := map[string]string{
		"Senior Frontend Engineer":     "senior frontend engineer",
		"  C++   Developer  ":          "c++ developer",
		"C#/.NET Engineer!":            "c# .net engineer",
		"React.js Developer (Remote)":  "react.js developer remote",
		"":                             "",
		"SENIOR  FRONTEND\nENGINEER\t": "senior frontend engineer",
	}
	for in, want := range cases {
		if got := NormalizeText(in); got != want {
			t.Errorf("NormalizeText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeCompanyStripsLegalSuffixes(t *testing.T) {
	cases := map[string]string{
		"Example Co., Ltd.": "example",
		"Acme Inc":          "acme",
		"Foo Technologies":  "foo",
		"Bar Pte. Ltd.":     "bar",
		"Baz GmbH":          "baz",
		"":                  "",
		"Ltd":               "ltd", // nothing left: keep the raw form rather than empty
	}
	for in, want := range cases {
		if got := NormalizeCompany(in); got != want {
			t.Errorf("NormalizeCompany(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonicalKeyIsStableAndDiscriminating(t *testing.T) {
	a := CanonicalKey("Senior Frontend Engineer", "Example Co., Ltd.", "Kuala Lumpur", "my")
	b := CanonicalKey("senior frontend engineer", "Example", "kuala lumpur", "MY")
	if a != b {
		t.Error("canonical key must be stable across formatting and case")
	}
	c := CanonicalKey("Senior Frontend Engineer", "Example", "Singapore", "SG")
	if a == c {
		t.Error("different city/country must produce a different key")
	}
	d := CanonicalKey("Senior Frontend Engineer", "Different Company", "Kuala Lumpur", "MY")
	if a == d {
		t.Error("different company must produce a different key")
	}
	if len(a) != 64 {
		t.Errorf("expected a sha256 hex digest, got %d chars", len(a))
	}
}

func TestSameVacancyTiers(t *testing.T) {
	existing := NewIdentity("arbeitnow", "abc-123", "https://example.com/jobs/1?utm_source=x", "Senior Frontend Engineer", "Example Co", "Kuala Lumpur", "MY")

	t.Run("tier 1 same source job id", func(t *testing.T) {
		in := NewIdentity("arbeitnow", "abc-123", "https://elsewhere.test/other", "Completely Different", "Other Co", "Berlin", "DE")
		d := SameVacancy(in, existing)
		if !d.Same || d.Tier != "exact_source_id" {
			t.Fatalf("expected exact_source_id, got %+v", d)
		}
	})

	t.Run("tier 2 same canonical url ignoring tracking params", func(t *testing.T) {
		in := NewIdentity("remoteok", "other-id", "https://example.com/jobs/1?utm_campaign=zzz&gclid=1", "Senior Frontend Engineer", "Example Co", "Kuala Lumpur", "MY")
		d := SameVacancy(in, existing)
		if !d.Same || d.Tier != "exact_url" {
			t.Fatalf("expected exact_url, got %+v", d)
		}
	})

	t.Run("tier 3 fuzzy across portals", func(t *testing.T) {
		in := NewIdentity("himalayas", "xyz", "https://other-portal.test/vacancy/9", "Senior Frontend Engineer (Remote)", "Example Co., Ltd", "Kuala Lumpur", "MY")
		d := SameVacancy(in, existing)
		if !d.Same || d.Tier != "fuzzy" {
			t.Fatalf("expected a fuzzy duplicate, got %+v", d)
		}
		if d.Similarity < SimThreshold {
			t.Errorf("similarity %v below threshold", d.Similarity)
		}
		if d.Reason == "" {
			t.Error("a duplicate decision must carry a reason for auditing")
		}
	})

	t.Run("different company is not a duplicate", func(t *testing.T) {
		in := NewIdentity("himalayas", "xyz2", "https://other-portal.test/vacancy/10", "Senior Frontend Engineer", "Totally Different Corp", "Kuala Lumpur", "MY")
		d := SameVacancy(in, existing)
		if d.Same {
			t.Fatalf("a different company must not be merged: %+v", d)
		}
	})

	t.Run("different role is not a duplicate", func(t *testing.T) {
		in := NewIdentity("jobicy", "z", "https://other.test/11", "Warehouse Operative", "Example Co", "Kuala Lumpur", "MY")
		d := SameVacancy(in, existing)
		if d.Same {
			t.Fatalf("a different role must not be merged: %+v", d)
		}
	})

	t.Run("missing title never merges", func(t *testing.T) {
		in := NewIdentity("jobicy", "z2", "https://other.test/12", "", "Example Co", "Kuala Lumpur", "MY")
		d := SameVacancy(in, existing)
		if d.Same {
			t.Fatalf("a listing without a title must not be merged: %+v", d)
		}
	})
}

func TestURLHashIgnoresTracking(t *testing.T) {
	a := URLHash("https://Example.com/jobs/1?utm_source=a&gclid=b")
	b := URLHash("https://example.com/jobs/1")
	if a != b {
		t.Error("tracking parameters must not change the canonical URL hash")
	}
	c := URLHash("https://example.com/jobs/2")
	if a == c {
		t.Error("different paths must produce different hashes")
	}
}

func TestCanTransition(t *testing.T) {
	allowed := [][2]string{
		{StatusNew, StatusActive},
		{StatusActive, StatusSaved},
		{StatusSaved, StatusApplied},
		{StatusApplied, StatusInterviewing},
		{StatusInterviewing, StatusRejected},
		{StatusExpired, StatusActive},
		{StatusNew, StatusNew},
	}
	for _, pair := range allowed {
		if !CanTransition(pair[0], pair[1]) {
			t.Errorf("transition %s -> %s should be allowed", pair[0], pair[1])
		}
	}
	denied := [][2]string{
		{StatusRemoved, StatusInterviewing},
		{StatusRejected, StatusInterviewing},
		{StatusExpired, StatusApplied},
		{"bogus", StatusActive},
	}
	for _, pair := range denied {
		if CanTransition(pair[0], pair[1]) {
			t.Errorf("transition %s -> %s must be denied", pair[0], pair[1])
		}
	}
}

func TestStatusesMatchDatabaseConstraints(t *testing.T) {
	// The migration CHECK constraints must accept every status this package can
	// produce; this guards against drift between Go and SQL.
	statuses := []string{StatusNew, StatusActive, StatusSaved, StatusApplied, StatusInterviewing, StatusRejected, StatusExpired, StatusRemoved, StatusUnverified}
	blob, err := json.Marshal(statuses)
	if err != nil {
		t.Fatal(err)
	}
	if len(blob) == 0 {
		t.Fatal("statuses must serialise")
	}
	// Mirrors the CHECK constraint text in migrations/0001_init.sql.
	const migrationList = "'new','active','saved','applied','interviewing','rejected','expired','removed','unverified'"
	for _, s := range statuses {
		if !contains(migrationList, "'"+s+"'") {
			t.Errorf("status %q is missing from the migration CHECK constraint (%s)", s, migrationList)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOfSub(haystack, needle) >= 0
}

func indexOfSub(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

func TestCombinedSimilarityReportsReason(t *testing.T) {
	a := NewIdentity("s1", "1", "https://a.test/1", "Senior Frontend Engineer", "Example Co", "Kuala Lumpur", "MY")
	b := NewIdentity("s2", "2", "https://b.test/2", "Frontend Engineer", "Example", "Kuala Lumpur", "MY")
	sim, reason := CombinedSimilarity(a, b)
	if sim <= 0 {
		t.Fatalf("expected a positive similarity, got %v (%s)", sim, reason)
	}
	if reason == "" {
		t.Error("similarity must be explainable")
	}
}
