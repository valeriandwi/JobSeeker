package api

import "testing"

// Sort keys are whitelisted because they are interpolated into ORDER BY, so an
// unknown value must be rejected rather than silently falling through to a
// default. "Newest discovery first" is the documented default and has to stay a
// valid request value: the UI shows it as the selected option while omitting the
// parameter, so an explicit request for it must work too.
func TestJobSortWhitelist(t *testing.T) {
	if !inSet("first_discovered", allowedSorts...) {
		t.Errorf("first_discovered must be sortable; whitelist=%v", allowedSorts)
	}
	for _, want := range []string{"score", "published_at", "salary", "company"} {
		if !inSet(want, allowedSorts...) {
			t.Errorf("%s should remain sortable; whitelist=%v", want, allowedSorts)
		}
	}
	// A column name is not a sort key, and neither is an invented one.
	for _, bad := range []string{"first_discovered_at", "lm.score", "bogus"} {
		if inSet(bad, allowedSorts...) {
			t.Errorf("%q must not be an accepted sort value", bad)
		}
	}
}
