package edition

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, loc *time.Location, s string) time.Time {
	t.Helper()
	tm, err := time.ParseInLocation("2006-01-02 15:04:05", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestCutoff(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		now        string
		wantCutoff string
		wantStart  string
	}{
		{"after cutoff", "2026-10-16 09:00:00", "2026-10-16 06:30:00", "2026-10-15 06:30:00"},
		{"exactly at cutoff", "2026-10-16 06:30:00", "2026-10-16 06:30:00", "2026-10-15 06:30:00"},
		{"just before cutoff", "2026-10-16 06:29:59", "2026-10-15 06:30:00", "2026-10-14 06:30:00"},
		{"after midnight", "2026-10-16 00:10:00", "2026-10-15 06:30:00", "2026-10-14 06:30:00"},
		{"DST ends", "2026-10-25 12:00:00", "2026-10-25 06:30:00", "2026-10-24 06:30:00"},
		{"DST starts", "2026-03-29 12:00:00", "2026-03-29 06:30:00", "2026-03-28 06:30:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := mustParse(t, loc, tt.now)
			cutoff := Cutoff(now, loc)
			if want := mustParse(t, loc, tt.wantCutoff); !cutoff.Equal(want) {
				t.Errorf("Cutoff(%s) = %s, want %s", tt.now, cutoff, want)
			}
			start := Start(cutoff)
			if want := mustParse(t, loc, tt.wantStart); !start.Equal(want) {
				t.Errorf("Start(%s) = %s, want %s", cutoff, start, want)
			}
		})
	}

	t.Run("DST ends spans 25 hours", func(t *testing.T) {
		cutoff := Cutoff(mustParse(t, loc, "2026-10-25 12:00:00"), loc)
		start := Start(cutoff)
		if got := cutoff.Sub(start); got != 25*time.Hour {
			t.Errorf("cutoff.Sub(start) = %s, want 25h", got)
		}
	})

	t.Run("DST starts spans 23 hours", func(t *testing.T) {
		cutoff := Cutoff(mustParse(t, loc, "2026-03-29 12:00:00"), loc)
		start := Start(cutoff)
		if got := cutoff.Sub(start); got != 23*time.Hour {
			t.Errorf("cutoff.Sub(start) = %s, want 23h", got)
		}
	})

	t.Run("UTC now yields cutoff in loc", func(t *testing.T) {
		now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
		cutoff := Cutoff(now, loc)
		if cutoff.Location() != loc {
			t.Errorf("Location() = %v, want %v", cutoff.Location(), loc)
		}
		if want := mustParse(t, loc, "2026-10-16 06:30:00"); !cutoff.Equal(want) {
			t.Errorf("Cutoff(%s) = %s, want %s", now, cutoff, want)
		}
	})
}
