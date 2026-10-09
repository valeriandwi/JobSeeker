package search

import (
	"testing"
	"time"
)

func TestNextRunDailyMorningJakarta(t *testing.T) {
	// 08:00 Asia/Jakarta (UTC+7) is 01:00 UTC.
	now := time.Date(2026, 10, 9, 0, 30, 0, 0, time.UTC)
	next, err := NextRun("0 8 * * *", "Asia/Jakarta", now)
	if err != nil {
		t.Fatalf("NextRun: %v", err)
	}
	want := time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}

func TestNextRunRollsToNextDayAfterTheSlot(t *testing.T) {
	now := time.Date(2026, 10, 9, 2, 0, 0, 0, time.UTC) // 09:00 Jakarta, past 08:00
	next, err := NextRun("0 8 * * *", "Asia/Jakarta", now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}

func TestNextRunKeepsTimeOfDayAcrossDSTForUTCZone(t *testing.T) {
	// Singapore has no DST; the wall-clock time must not drift.
	now := time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC)
	next, err := NextRun("30 6 * * *", "Asia/Singapore", now)
	if err != nil {
		t.Fatal(err)
	}
	if next.Hour() != 22 || next.Minute() != 30 {
		t.Fatalf("expected 22:30 UTC (06:30 +08), got %s", next)
	}
	// And the following day at the same UTC offset.
	next2, err := NextRun("30 6 * * *", "Asia/Singapore", next)
	if err != nil {
		t.Fatal(err)
	}
	if next2.Sub(next) != 24*time.Hour {
		t.Fatalf("interval changed across days: %s -> %s", next, next2)
	}
}

func TestNextRunHonoursDSTTransition(t *testing.T) {
	// Europe/Lisbon shifts on 2026-10-25; a 06:30 local job must stay at 06:30
	// local, which means the UTC time moves by an hour.
	before := time.Date(2026, 10, 23, 6, 30, 0, 0, time.UTC)
	summer, err := NextRun("30 6 * * *", "Europe/Lisbon", before)
	if err != nil {
		t.Fatal(err)
	}
	if summer.Hour() != 5 {
		t.Fatalf("before the change, 06:30 Lisbon (UTC+1) should be 05:30 UTC, got %s", summer)
	}
	winter, err := NextRun("30 6 * * *", "Europe/Lisbon", time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if winter.Hour() != 6 {
		t.Fatalf("after the change, 06:30 Lisbon (UTC+0) should be 06:30 UTC, got %s", winter)
	}
}

func TestNextRunRejectsBadInput(t *testing.T) {
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	if _, err := NextRun("not a cron", "UTC", now); err == nil {
		t.Error("an invalid cron spec must be rejected")
	}
	if _, err := NextRun("0 8 * * *", "Mars/Olympus_Mons", now); err == nil {
		t.Error("an unknown timezone must be rejected")
	}
}

func TestIsDueFirstUseDoesNotBackfillHistory(t *testing.T) {
	now := time.Date(2026, 10, 9, 6, 0, 0, 0, time.UTC)
	due, next, err := IsDue("0 8 * * *", "Asia/Jakarta", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	// 08:00 Jakarta today is 01:00 UTC, which has already passed at 06:00 UTC,
	// so it is due once - not once per day since the beginning of time.
	if !due {
		t.Fatal("a schedule whose slot passed today should be due on first use")
	}
	want := time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}

func TestIsDueRespectsLastRun(t *testing.T) {
	last := time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC) // ran at today's slot
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	due, next, err := IsDue("0 8 * * *", "Asia/Jakarta", &last, now)
	if err != nil {
		t.Fatal(err)
	}
	if due {
		t.Fatal("a schedule that already ran today must not run again")
	}
	if !next.Equal(time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("next = %s", next)
	}
}

func TestIsDueLongOverdueRunsOnce(t *testing.T) {
	last := time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC) // over a month ago
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	due, next, err := IsDue("0 8 * * *", "Asia/Jakarta", &last, now)
	if err != nil {
		t.Fatal(err)
	}
	if !due {
		t.Fatal("a long-overdue schedule must be due exactly once, not once per missed day")
	}
	if next.Before(now) {
		t.Fatalf("the next run must be in the future, got %s", next)
	}
}
