package jobs

import (
	"strings"
	"testing"
	"time"
)

func TestSingleFailedAttemptNeverExpires(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	state := VerificationState{
		SourceDeclaredActive: false,
		LastSeenAt:           now.Add(-2 * time.Hour),
		ConsecutiveFailures:  1,
	}
	expire, reason := DecideExpiry(state, now, DefaultExpiryPolicy())
	if expire {
		t.Fatalf("a single failed verification must not expire a vacancy (reason: %s)", reason)
	}
	if reason == "" {
		t.Error("the decision must be explained")
	}
}

func TestGraceWindowProtectsRecentlySeenListings(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	policy := ExpiryPolicy{GraceWindow: 72 * time.Hour, FailureThreshold: 3}

	// Three failures, but only 24h since last seen: still inside the window.
	state := VerificationState{LastSeenAt: now.Add(-24 * time.Hour), ConsecutiveFailures: 3}
	if expire, reason := DecideExpiry(state, now, policy); expire {
		t.Fatalf("must stay inside the grace window: %s", reason)
	}

	// Three failures and 96h since last seen: expire.
	state = VerificationState{LastSeenAt: now.Add(-96 * time.Hour), ConsecutiveFailures: 3}
	expire, reason := DecideExpiry(state, now, policy)
	if !expire {
		t.Fatal("repeated misses past the grace window must expire the vacancy")
	}
	if !strings.Contains(reason, "consecutive") {
		t.Errorf("reason should mention consecutive misses: %s", reason)
	}
}

func TestBelowFailureThresholdStaysUnverified(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	policy := ExpiryPolicy{GraceWindow: time.Hour, FailureThreshold: 3}
	state := VerificationState{LastSeenAt: now.Add(-48 * time.Hour), ConsecutiveFailures: 2}
	expire, reason := DecideExpiry(state, now, policy)
	if expire {
		t.Fatalf("2 misses with a threshold of 3 must not expire: %s", reason)
	}
	if !strings.Contains(reason, "kept as unverified") {
		t.Errorf("reason should state the listing stays unverified: %s", reason)
	}
	if got := VerificationOutcome(state, now, policy); got != ListingUnverified {
		t.Errorf("listing outcome = %q, want %q", got, ListingUnverified)
	}
}

func TestPublishedDeadlineExpiresImmediately(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	deadline := now.Add(-time.Minute)
	state := VerificationState{ExpiresAt: &deadline, LastSeenAt: now, SourceDeclaredActive: false}
	expire, reason := DecideExpiry(state, now, DefaultExpiryPolicy())
	if !expire {
		t.Fatal("a passed source-published deadline must expire the vacancy")
	}
	if !strings.Contains(reason, "deadline") {
		t.Errorf("reason = %s", reason)
	}
}

func TestActiveListingNeverExpiresEvenPastDeadline(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	deadline := now.Add(-time.Hour)
	state := VerificationState{ExpiresAt: &deadline, LastSeenAt: now, SourceDeclaredActive: true}
	expire, reason := DecideExpiry(state, now, DefaultExpiryPolicy())
	if expire {
		t.Fatalf("a listing still present in the feed must stay active: %s", reason)
	}
	if got := VerificationOutcome(state, now, DefaultExpiryPolicy()); got != ListingActive {
		t.Errorf("listing outcome = %q, want %q", got, ListingActive)
	}
}

func TestFutureDeadlineDoesNotExpire(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	deadline := now.Add(48 * time.Hour)
	state := VerificationState{ExpiresAt: &deadline, LastSeenAt: now}
	if expire, reason := DecideExpiry(state, now, DefaultExpiryPolicy()); expire {
		t.Fatalf("a future deadline must not expire: %s", reason)
	}
}

func TestVerificationOutcomeStates(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	policy := DefaultExpiryPolicy()

	active := VerificationState{SourceDeclaredActive: true, LastSeenAt: now}
	if got := VerificationOutcome(active, now, policy); got != ListingActive {
		t.Errorf("active listing = %q", got)
	}

	gone := VerificationState{SourceDeclaredActive: false, LastSeenAt: now.Add(-time.Hour), ConsecutiveFailures: 1}
	if got := VerificationOutcome(gone, now, policy); got != ListingUnverified {
		t.Errorf("recently missing listing = %q, want unverified", got)
	}

	expired := VerificationState{SourceDeclaredActive: false, LastSeenAt: now.Add(-200 * time.Hour), ConsecutiveFailures: 5}
	if got := VerificationOutcome(expired, now, policy); got != ListingExpired {
		t.Errorf("long-missing listing = %q, want expired", got)
	}
}
