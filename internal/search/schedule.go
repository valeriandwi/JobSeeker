// Package search: cron scheduling helpers shared by the runner and the
// scheduler process.
package search

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// cronParser accepts standard 5-field specs (minute hour dom month dow).
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// NextRun computes the next fire time for a cron spec in a given timezone,
// returned in UTC. Timezone handling happens here and only here: everything
// stored in the database is UTC.
func NextRun(spec, timezone string, after time.Time) (time.Time, error) {
	schedule, err := cronParser.Parse(spec)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron spec %q: %w", spec, err)
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}
	return schedule.Next(after.In(loc)).UTC(), nil
}

// IsDue reports whether a schedule should fire now, and computes the following
// fire time.
//
// When a schedule has never run, the search window starts 24 hours back so a
// newly created schedule does not immediately fire once for every day it did
// not exist.
func IsDue(spec, tz string, lastRun *time.Time, now time.Time) (bool, time.Time, error) {
	from := now.Add(-24 * time.Hour)
	if lastRun != nil {
		from = *lastRun
	}
	next, err := NextRun(spec, tz, from)
	if err != nil {
		return false, time.Time{}, err
	}
	if next.After(now) {
		return false, next, nil
	}
	following, err := NextRun(spec, tz, now)
	if err != nil {
		return true, time.Time{}, err
	}
	return true, following, nil
}
