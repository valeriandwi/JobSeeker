package jobs

import (
	"fmt"
	"time"
)

// Vacancy statuses.
const (
	StatusNew          = "new"
	StatusActive       = "active"
	StatusSaved        = "saved"
	StatusApplied      = "applied"
	StatusInterviewing = "interviewing"
	StatusRejected     = "rejected"
	StatusExpired      = "expired"
	StatusRemoved      = "removed"
	StatusUnverified   = "unverified"
)

// Listing statuses (per portal).
const (
	ListingActive     = "active"
	ListingExpired    = "expired"
	ListingRemoved    = "removed"
	ListingUnverified = "unverified"
)

// statusTransitions describes the allowed user/system lifecycle. Anything not
// listed is rejected, so a stale client cannot resurrect a removed vacancy.
var statusTransitions = map[string]map[string]bool{
	StatusNew:          {StatusActive: true, StatusRemoved: true, StatusExpired: true, StatusUnverified: true, StatusSaved: true},
	StatusActive:       {StatusSaved: true, StatusExpired: true, StatusRemoved: true, StatusUnverified: true},
	StatusSaved:        {StatusApplied: true, StatusActive: true, StatusExpired: true, StatusRemoved: true},
	StatusApplied:      {StatusInterviewing: true, StatusRejected: true, StatusSaved: true},
	StatusInterviewing: {StatusRejected: true, StatusApplied: true},
	StatusRejected:     {StatusApplied: true},
	StatusExpired:      {StatusActive: true, StatusRemoved: true},
	StatusUnverified:   {StatusActive: true, StatusExpired: true, StatusRemoved: true},
	StatusRemoved:      {StatusActive: true},
}

// CanTransition reports whether a status change is allowed.
func CanTransition(from, to string) bool {
	if from == to {
		return true
	}
	allowed, ok := statusTransitions[from]
	if !ok {
		return false
	}
	return allowed[to]
}

// VerificationState captures what we know about a listing's current existence
// and the source's own statements about it.
type VerificationState struct {
	// SourceDeclaredActive is true when the listing appeared in the source's
	// latest feed (as opposed to merely not being re-checked).
	SourceDeclaredActive bool
	// ExpiresAt is the source-published application deadline, when provided.
	ExpiresAt *time.Time
	// LastSeenAt is the last time the listing appeared in the source feed.
	LastSeenAt time.Time
	// ConsecutiveFailures counts verification attempts that did not find the
	// listing.
	ConsecutiveFailures int
}

// ExpiryPolicy parameterises the expiry decision.
type ExpiryPolicy struct {
	// GraceWindow is how long a listing may be missing before expiry is even
	// considered. The default is generous on purpose: sources break often.
	GraceWindow time.Duration
	// FailureThreshold is the number of consecutive misses required.
	FailureThreshold int
}

// DefaultExpiryPolicy returns the policy used by the scheduler.
func DefaultExpiryPolicy() ExpiryPolicy {
	return ExpiryPolicy{GraceWindow: 72 * time.Hour, FailureThreshold: 3}
}

// DecideExpiry reports whether a vacancy should be marked expired, and why.
//
// Deliberate rule: a single failed search attempt never expires a vacancy.
// Only a source-published deadline passing, or repeated misses spread over the
// grace window, does.
func DecideExpiry(state VerificationState, now time.Time, policy ExpiryPolicy) (bool, string) {
	if policy.GraceWindow <= 0 {
		policy.GraceWindow = DefaultExpiryPolicy().GraceWindow
	}
	if policy.FailureThreshold <= 0 {
		policy.FailureThreshold = DefaultExpiryPolicy().FailureThreshold
	}

	if !state.SourceDeclaredActive && state.ExpiresAt != nil && now.After(*state.ExpiresAt) {
		return true, fmt.Sprintf("source-published application deadline passed on %s", state.ExpiresAt.UTC().Format(time.RFC3339))
	}

	if state.SourceDeclaredActive {
		return false, "listing is still present in the source feed"
	}

	if !state.LastSeenAt.IsZero() && now.Sub(state.LastSeenAt) < policy.GraceWindow {
		remaining := policy.GraceWindow - now.Sub(state.LastSeenAt)
		return false, fmt.Sprintf("within the grace window; %s remaining before expiry is considered", remaining.Round(time.Hour))
	}

	if state.ConsecutiveFailures >= policy.FailureThreshold {
		return true, fmt.Sprintf("listing absent from the source for %d consecutive verification attempts", state.ConsecutiveFailures)
	}

	return false, fmt.Sprintf("only %d consecutive misses (< %d required) and the grace window has passed; kept as unverified", state.ConsecutiveFailures, policy.FailureThreshold)
}

// VerificationOutcome maps a verification miss to the listing status to store,
// which is distinct from the vacancy status: a listing going unverified must not
// immediately expire the canonical vacancy.
func VerificationOutcome(state VerificationState, now time.Time, policy ExpiryPolicy) string {
	if expire, _ := DecideExpiry(state, now, policy); expire {
		return ListingExpired
	}
	if !state.SourceDeclaredActive {
		return ListingUnverified
	}
	return ListingActive
}
