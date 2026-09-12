package main

import "testing"

func TestCalendarDaysProgressIncludesWeekendsAndOnlyFinishedDays(t *testing.T) {
	for _, tc := range []struct {
		from, to, today string
		gone, total     int
	}{
		{"2026-08-21", "2026-09-20", "2026-09-11", 21, 31},
		{"2026-08-21", "2026-09-20", "2026-08-20", 0, 31},
		{"2026-08-21", "2026-09-20", "2026-08-21", 0, 31},
		{"2026-08-21", "2026-09-20", "2026-09-20", 30, 31},
		{"2026-08-21", "2026-09-20", "2026-09-21", 31, 31},
		{"2028-02-21", "2028-03-20", "2028-03-01", 9, 29},
		{"2026-09-20", "2026-08-21", "2026-09-11", 0, 0},
		{"bad", "2026-09-20", "2026-09-11", 0, 0},
	} {
		gone, total := calendarDaysProgress(tc.from, tc.to, tc.today)
		if gone != tc.gone || total != tc.total {
			t.Errorf("%s to %s as of %s: got %d/%d, want %d/%d", tc.from, tc.to, tc.today, gone, total, tc.gone, tc.total)
		}
	}
}
