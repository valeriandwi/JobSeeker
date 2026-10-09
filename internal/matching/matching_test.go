package matching

import (
	"testing"

	"github.com/valeriandwipurnomo/jobseeker/internal/sources"
)

func floatPtr(f float64) *float64 { return &f }

func baseProfile() Profile {
	return Profile{
		Version:         3,
		Titles:          []string{"Frontend Engineer", "Senior Frontend Engineer"},
		Skills:          []string{"React", "TypeScript", "Go", "PostgreSQL"},
		CurrentTitle:    "Frontend Engineer",
		Seniority:       "senior",
		YearsExperience: floatPtr(7),
	}
}

func basePrefs() Preferences {
	return Preferences{
		Locations: []LocationPref{
			{Country: "MY", City: "Kuala Lumpur", Mandatory: true},
			{Country: "ID"},
		},
		WorkArrangements: []ArrangementPref{{Value: "hybrid", Mandatory: true}},
		EmploymentTypes:  []string{"permanent"},
		SeniorityLevels:  []string{"senior"},
		MinSalary:        floatPtr(120000),
		SalaryCurrency:   "USD",
		SalaryPeriod:     "year",
	}
}

func strongJob() Job {
	return Job{
		ID:                 "job-1",
		Title:              "Senior Frontend Engineer",
		Company:            "Example Co",
		DescriptionSummary: "We build React and TypeScript interfaces. Go services, PostgreSQL.",
		City:               "Kuala Lumpur",
		Country:            "MY",
		WorkArrangement:    "hybrid",
		EmploymentType:     "permanent",
		Seniority:          "senior",
		SalaryMin:          floatPtr(140000),
		SalaryMax:          floatPtr(180000),
		SalaryCurrency:     "USD",
		SalaryPeriod:       "year",
		AttributesVerified: []string{
			sources.AttrTitle, sources.AttrCompany, sources.AttrDescription,
			sources.AttrLocation, sources.AttrWorkArrangement, sources.AttrEmploymentType,
			sources.AttrSeniority, sources.AttrSalary,
		},
	}
}

func TestScoreIsDeterministic(t *testing.T) {
	first := Score(strongJob(), baseProfile(), basePrefs())
	for i := 0; i < 25; i++ {
		again := Score(strongJob(), baseProfile(), basePrefs())
		if again.Score != first.Score {
			t.Fatalf("score changed between runs: %d vs %d", first.Score, again.Score)
		}
		if len(again.Explanation) != len(first.Explanation) {
			t.Fatalf("explanation length changed")
		}
		for j := range again.Explanation {
			if again.Explanation[j] != first.Explanation[j] {
				t.Fatalf("component %d differs: %+v vs %+v", j, again.Explanation[j], first.Explanation[j])
			}
		}
	}
}

func TestStrongMatchScoresHigh(t *testing.T) {
	res := Score(strongJob(), baseProfile(), basePrefs())
	if res.Excluded {
		t.Fatalf("strong match must not be excluded: %v", res.ExclusionReasons)
	}
	if res.Score < 85 {
		t.Fatalf("expected a high score for an ideal vacancy, got %d (%+v)", res.Score, res.Explanation)
	}
	if res.EvidenceCoverage != 1 {
		t.Errorf("evidence coverage = %v, want 1 for fully published attributes", res.EvidenceCoverage)
	}
}

func TestExplanationAccountsForEveryComponent(t *testing.T) {
	res := Score(strongJob(), baseProfile(), basePrefs())
	if len(res.Explanation) != 8 {
		t.Fatalf("expected 8 components, got %d", len(res.Explanation))
	}
	for _, c := range res.Explanation {
		if c.Component == "" || c.Reason == "" || c.Status == "" {
			t.Errorf("component missing text: %+v", c)
		}
		if c.Contribution > c.Weight+0.01 {
			t.Errorf("component %s contributed more than its weight: %v > %v", c.Component, c.Contribution, c.Weight)
		}
		if c.Contribution < 0 {
			t.Errorf("negative contribution for %s", c.Component)
		}
	}
}

func TestScoreStaysWithinBounds(t *testing.T) {
	// A vacancy that matches nothing must floor at 0, and an ideal one at 100.
	empty := Job{ID: "x", Title: "Warehouse Operative", DescriptionSummary: "Forklift licence required."}
	res := Score(empty, baseProfile(), basePrefs())
	if res.Score < 0 || res.Score > 100 {
		t.Fatalf("score out of range: %d", res.Score)
	}
	if res.EvidenceCoverage >= 1 {
		t.Errorf("vacancy with no published attributes must not report full evidence coverage: %v", res.EvidenceCoverage)
	}
}

func TestUnknownAttributesAreNotAssumedToMatch(t *testing.T) {
	job := strongJob()
	job.AttributesVerified = []string{sources.AttrTitle, sources.AttrCompany}
	job.WorkArrangement = "unknown"
	job.SalaryMin, job.SalaryMax = nil, nil
	job.City, job.Country = "", ""

	res := Score(job, baseProfile(), basePrefs())
	if res.Excluded {
		t.Fatalf("unknown attributes must never trigger exclusion: %v", res.ExclusionReasons)
	}
	statuses := map[string]string{}
	for _, c := range res.Explanation {
		statuses[c.Component] = c.Status
	}
	if statuses["work_arrangement"] != StatusUnknown {
		t.Errorf("work_arrangement status = %q, want unknown", statuses["work_arrangement"])
	}
	if statuses["salary"] != StatusUnknown {
		t.Errorf("salary status = %q, want unknown", statuses["salary"])
	}
	if statuses["location"] != StatusUnknown {
		t.Errorf("location status = %q, want unknown", statuses["location"])
	}
	if len(res.UnverifiedNotes) < 3 {
		t.Errorf("expected unverified notes for location, arrangement and salary, got %v", res.UnverifiedNotes)
	}
	if res.EvidenceCoverage > 0.75 {
		t.Errorf("evidence coverage too high for a mostly-unknown vacancy: %v", res.EvidenceCoverage)
	}
	// Fewer known facts must not beat a fully published strong match.
	if res.Score >= Score(strongJob(), baseProfile(), basePrefs()).Score {
		t.Error("a vacancy with unknown attributes must not outscore a fully verified match")
	}
}

func TestMandatoryConstraintsExcludeOnlyWhenVerified(t *testing.T) {
	prefs := basePrefs()

	t.Run("verified mismatch excludes", func(t *testing.T) {
		job := strongJob()
		job.WorkArrangement = "onsite" // verified, contradicts mandatory hybrid
		res := Score(job, baseProfile(), prefs)
		if !res.Excluded {
			t.Fatal("verified contradictory work arrangement must exclude")
		}
		if res.Score != 0 {
			t.Errorf("excluded result must score 0, got %d", res.Score)
		}
		if len(res.ExclusionReasons) == 0 {
			t.Error("exclusion reasons must be reported")
		}
	})

	t.Run("unverified mismatch does not exclude", func(t *testing.T) {
		job := strongJob()
		job.WorkArrangement = "onsite"
		job.AttributesVerified = []string{sources.AttrTitle, sources.AttrCompany, sources.AttrLocation}
		res := Score(job, baseProfile(), prefs)
		if res.Excluded {
			t.Fatalf("unverified arrangement must not exclude: %v", res.ExclusionReasons)
		}
	})

	t.Run("mandatory country mismatch excludes", func(t *testing.T) {
		job := strongJob()
		job.Country = "US"
		job.City = "Austin"
		res := Score(job, baseProfile(), prefs)
		if !res.Excluded {
			t.Fatal("verified mandatory country mismatch must exclude")
		}
	})

	t.Run("non-mandatory location does not exclude", func(t *testing.T) {
		prefs := basePrefs()
		prefs.Locations = []LocationPref{{Country: "MY", Mandatory: false}}
		prefs.WorkArrangements = []ArrangementPref{{Value: "hybrid", Mandatory: false}}
		job := strongJob()
		job.Country = "DE"
		job.City = "Berlin"
		res := Score(job, baseProfile(), prefs)
		if res.Excluded {
			t.Fatalf("non-mandatory preferences must not exclude: %v", res.ExclusionReasons)
		}
	})
}

func TestExcludedKeywordsExclude(t *testing.T) {
	prefs := basePrefs()
	prefs.ExcludedKeywords = []string{"unreal"}
	job := strongJob()
	job.DescriptionSummary = "React, TypeScript and Unreal Engine"
	res := Score(job, baseProfile(), prefs)
	if !res.Excluded {
		t.Fatal("an excluded keyword must exclude the vacancy")
	}
	found := false
	for _, r := range res.ExclusionReasons {
		if r == "contains excluded keyword: unreal" {
			found = true
		}
	}
	if !found {
		t.Errorf("reason missing: %v", res.ExclusionReasons)
	}
}

func TestRemoteLocationHandling(t *testing.T) {
	t.Run("remote satisfies a non-mandatory country preference", func(t *testing.T) {
		prefs := basePrefs()
		prefs.Locations = []LocationPref{{Country: "ID"}}
		prefs.WorkArrangements = []ArrangementPref{{Value: "remote"}}
		job := strongJob()
		job.Country, job.City = "US", "New York"
		job.WorkArrangement = "remote"
		res := Score(job, baseProfile(), prefs)
		if res.Excluded {
			t.Fatalf("non-mandatory preferences must not exclude: %v", res.ExclusionReasons)
		}
		for _, c := range res.Explanation {
			if c.Component == "location" && c.Status != StatusMatched {
				t.Errorf("location status = %q, want matched", c.Status)
			}
		}
	})

	t.Run("remote does not bypass a mandatory country", func(t *testing.T) {
		prefs := basePrefs()
		prefs.Locations = []LocationPref{{Country: "ID", Mandatory: true}}
		prefs.WorkArrangements = []ArrangementPref{{Value: "remote"}}
		job := strongJob()
		job.Country, job.City = "US", "New York"
		job.WorkArrangement = "remote"
		res := Score(job, baseProfile(), prefs)
		// Remote eligibility is often geo-restricted, so a verified country that
		// contradicts a mandatory requirement excludes rather than assumes the
		// user is eligible.
		if !res.Excluded {
			t.Fatal("a verified mandatory country mismatch must exclude even for remote vacancies")
		}
	})

	t.Run("mandatory city is waived for a remote vacancy", func(t *testing.T) {
		prefs := basePrefs()
		prefs.Locations = []LocationPref{{Country: "MY", City: "Kuala Lumpur", Mandatory: true}}
		prefs.WorkArrangements = []ArrangementPref{{Value: "remote"}}
		job := strongJob()
		job.Country, job.City = "MY", "Penang"
		job.WorkArrangement = "remote"
		res := Score(job, baseProfile(), prefs)
		if res.Excluded {
			t.Fatalf("a remote vacancy in the right country must not be excluded for the wrong city: %v", res.ExclusionReasons)
		}
	})
}

func TestSalaryUnknownIsNotTreatedAsSatisfied(t *testing.T) {
	prefs := basePrefs()
	job := strongJob()
	job.SalaryMin, job.SalaryMax = nil, nil
	job.AttributesVerified = []string{sources.AttrTitle, sources.AttrCompany, sources.AttrDescription, sources.AttrLocation, sources.AttrWorkArrangement, sources.AttrEmploymentType, sources.AttrSeniority}
	res := Score(job, baseProfile(), prefs)
	for _, c := range res.Explanation {
		if c.Component != "salary" {
			continue
		}
		if c.Status != StatusUnknown {
			t.Fatalf("salary status = %q, want unknown", c.Status)
		}
		if c.Contribution != 0 {
			t.Fatalf("undisclosed salary must contribute 0, got %v", c.Contribution)
		}
	}
}

func TestSalaryCurrencyMismatchSkipsComparison(t *testing.T) {
	prefs := basePrefs()
	job := strongJob()
	job.SalaryCurrency = "IDR"
	res := Score(job, baseProfile(), prefs)
	for _, c := range res.Explanation {
		if c.Component == "salary" && c.Status != StatusUnknown {
			t.Fatalf("currency mismatch must not be compared: %+v", c)
		}
	}
}

func TestSeniorityAdjacency(t *testing.T) {
	prefs := basePrefs()
	prefs.SeniorityLevels = []string{"senior"}
	job := strongJob()
	job.Seniority = "lead"
	res := Score(job, baseProfile(), prefs)
	for _, c := range res.Explanation {
		if c.Component == "seniority" {
			if c.Status != StatusPartial {
				t.Fatalf("adjacent seniority must be partial, got %q", c.Status)
			}
			if c.Contribution <= 0 {
				t.Fatal("adjacent seniority must contribute something")
			}
		}
	}
}

func TestUnrelatedVacancyScoresLow(t *testing.T) {
	prefs := Preferences{} // no preferences: only content matters
	job := Job{
		ID:                 "j",
		Title:              "Warehouse Operative",
		DescriptionSummary: "Forklift licence, night shifts, physical work.",
		AttributesVerified: []string{sources.AttrTitle, sources.AttrDescription},
	}
	res := Score(job, baseProfile(), prefs)
	if res.Score > 35 {
		t.Fatalf("unrelated vacancy scored too high: %d", res.Score)
	}
}

func TestTitleSimilarity(t *testing.T) {
	cases := []struct {
		a, b string
		min  float64
		max  float64
	}{
		{"Senior Frontend Engineer", "Frontend Engineer", 0.8, 1},
		{"Frontend Engineer", "frontend engineer", 0.9, 1},
		{"Backend Developer", "Frontend Engineer", 0, 0.25},
		{"React Developer", "React.js Engineer", 0, 0.6},
		{"", "Frontend Engineer", 0, 0},
	}
	for _, tc := range cases {
		got := TitleSimilarity(tc.a, tc.b)
		if got < tc.min || got > tc.max {
			t.Errorf("TitleSimilarity(%q,%q) = %.2f, want between %.2f and %.2f", tc.a, tc.b, got, tc.min, tc.max)
		}
	}
}

func TestMeetsThreshold(t *testing.T) {
	if MeetsThreshold(Result{Score: 70}, 70) != true {
		t.Error("70 must meet a 70 threshold")
	}
	if MeetsThreshold(Result{Score: 69}, 70) != false {
		t.Error("69 must not meet a 70 threshold")
	}
	if MeetsThreshold(Result{Score: 95, Excluded: true}, 10) != false {
		t.Error("excluded results must never meet the threshold")
	}
}

func TestNoPreferencesYieldsNoPenaltyForAbsentPrefs(t *testing.T) {
	res := Score(strongJob(), baseProfile(), Preferences{})
	for _, c := range res.Explanation {
		switch c.Component {
		case "location", "work_arrangement", "employment_type", "salary", "seniority":
			if c.Status != StatusNotApplicable && c.Status != StatusMatched && c.Status != StatusPartial {
				t.Errorf("%s status = %q with no preference configured", c.Component, c.Status)
			}
		}
	}
}
