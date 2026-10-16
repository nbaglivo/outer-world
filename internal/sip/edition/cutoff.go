// Package edition works out the daily sip edition window, bounded by 06:30 cutoffs.
package edition

import "time"

// Cutoff returns the latest 06:30 in loc at or before now.
func Cutoff(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	y, m, d := local.Date()
	cutoff := time.Date(y, m, d, 6, 30, 0, 0, loc)
	if local.Before(cutoff) {
		cutoff = time.Date(y, m, d-1, 6, 30, 0, 0, loc)
	}
	return cutoff
}

// Start returns the 06:30 on the calendar day before cutoff.
func Start(cutoff time.Time) time.Time {
	return Cutoff(cutoff.Add(-time.Nanosecond), cutoff.Location())
}
