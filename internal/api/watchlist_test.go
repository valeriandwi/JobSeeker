package api

import (
	"testing"

	"github.com/valeriandwipurnomo/jobseeker/internal/sources"
)

// The watchlist endpoint used to hardcode "greenhouse, lever, ashby", so a new
// ATS adapter could not be given a watchlist without editing the HTTP layer.
// Capability now comes from the adapter itself; these tests pin that contract.
func TestSupportsWatchlistIsDerivedFromAdapterCaps(t *testing.T) {
	srv := &Server{deps: Deps{Registry: sources.NewRegistry(
		sources.NewGreenhouse(),
		sources.NewLever(),
		sources.NewAshby(),
		sources.NewSmartRecruiters(),
		sources.NewRecruitee(),
		sources.NewPersonio(),
		sources.NewRemotive(),
		sources.NewArbeitnow(),
	)}}

	// Every company-board adapter must accept a watchlist.
	for _, id := range []string{
		"greenhouse", "lever", "ashby", "smartrecruiters", "recruitee", "personio",
	} {
		if !srv.supportsWatchlist(id) {
			t.Errorf("%s should accept a watchlist", id)
		}
	}

	// Feeds and aggregators must keep rejecting one.
	for _, id := range []string{"remotive", "arbeitnow", "adzuna", "usajobs", "nonexistent"} {
		if srv.supportsWatchlist(id) {
			t.Errorf("%s should not accept a watchlist", id)
		}
	}
}

func TestSupportsWatchlistWithoutRegistryIsClosed(t *testing.T) {
	srv := &Server{}
	if srv.supportsWatchlist("greenhouse") {
		t.Error("an empty registry must not report support")
	}
}
