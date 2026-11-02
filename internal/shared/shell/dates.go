package shell

import (
	"fmt"
	"strings"
	"time"

	"worklog/internal/shared/backend"
)

// The Meeting tab is the same commit sweep as Commit List, but on a Thursday-
// through-Thursday window — the reporting span for the Thursday weekly update,
// with the previous meeting's own Thursday included so the report reads as
// "everything since we last met, through today". Left 90% is the calendar of
// commits; right 10% is the local worklog entries filed against those same
// eight days.

// MeetingWeekStartOf returns the previous Thursday that begins the meeting
// window covering date. Thursday itself still belongs to the just-closed
// window — the meeting on Thursday reports on the week ending that day — so
// the roll-over to a new window happens the next morning, on Friday.
func MeetingWeekStartOf(date string) string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(date))
	if err != nil {
		t = time.Now()
	}
	// Shift a day back so Thursday reports on the just-finished window instead
	// of opening a fresh one; the offset math then finds the Thursday on or
	// before yesterday, and the 8-day span meetingWeekDates renders lands on
	// Thu → Thu inclusive of today.
	prior := t.AddDate(0, 0, -1)
	off := (int(prior.Weekday()) - int(time.Thursday) + 7) % 7
	return prior.AddDate(0, 0, -off).Format("2006-01-02")
}

// HasWeeklySupport reports whether a day's board items already include the
// weekly support shift with time logged on it. Matched on the parent issue as
// well as the item's own title, because the worklog under it is called
// "Worklog: <date>" like any other and says nothing about what it was for.
//
// Only items with minutes count. The rota creates a slot for every weekday of
// the week up front, at 0 minutes; counting those as logged made the current
// week look fully covered, so ticking the placeholder was undone at once and
// the +180 never showed.
func HasWeeklySupport(cfg backend.Config, items []backend.WorklogItem) bool {
	prefix := strings.ToLower(strings.TrimSpace(backend.SupportTitlePrefix(cfg)))
	if prefix == ":: weekly support" || strings.HasPrefix(prefix, ": ") {
		return false // no display name configured; nothing to match on
	}
	for _, it := range items {
		if it.Minutes <= 0 {
			continue // an empty rota slot, not a logged shift
		}
		for _, title := range []string{it.ParentTitle, it.Title} {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(title)), prefix) {
				return true
			}
		}
	}
	return false
}

// SupportPlaceholderFor is the minutes to pencil onto one day: a weekday shift
// where the placeholder is switched on and the board does not already hold one.
//
// Nothing is written anywhere. It is a sketch of a rota that has been worked but
// not yet logged, so the week can be read as it will stand rather than as it
// stands before a known piece of work goes on — and switching it off, or walking
// to another week, takes it straight back off again.
func SupportPlaceholderFor(cfg backend.Config, on bool, ds string, items []backend.WorklogItem) int {
	if !on {
		return 0
	}
	t, err := time.Parse("2006-01-02", ds)
	if err != nil || t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return 0
	}
	if HasWeeklySupport(cfg, items) {
		return 0
	}
	return backend.SupportPlaceholderMins
}

// WeekDates lists the seven days of a week, Monday first.
func WeekDates(start string) []string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(start))
	if err != nil {
		return nil
	}
	out := make([]string, 7)
	for i := range out {
		out[i] = t.AddDate(0, 0, i).Format("2006-01-02")
	}
	return out
}

func ShiftWeek(start string, delta int) string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(start))
	if err != nil {
		return start
	}
	return t.AddDate(0, 0, 7*delta).Format("2006-01-02")
}

// WeekRangeLabel names a week the way a person would say it, only spelling out
// the month or the year twice when the week actually crosses one.
func WeekRangeLabel(start string) string {
	a, err := time.Parse("2006-01-02", strings.TrimSpace(start))
	if err != nil {
		return start
	}
	b := a.AddDate(0, 0, 6)
	switch {
	case a.Year() != b.Year():
		return fmt.Sprintf("%s – %s", a.Format("2 Jan 2006"), b.Format("2 Jan 2006"))
	case a.Month() != b.Month():
		return fmt.Sprintf("%d %s – %d %s %d", a.Day(), a.Format("Jan"), b.Day(), b.Format("Jan"), b.Year())
	default:
		return fmt.Sprintf("%d – %d %s %d", a.Day(), b.Day(), b.Format("Jan"), b.Year())
	}
}

// weekWorklogs buckets a week's pushed worklogs by date. A week can straddle two
// months, and the cache is a month at a time, so both are asked for.
// weekMonths is the one or two calendar months a week falls across — a week
// straddling the 1st is served by two of the month-sized caches.
func WeekMonths(weekStart string) []string {
	if weekStart == "" {
		weekStart = backend.WeekStartOf(Today())
	}
	var months []string
	seen := map[string]bool{}
	for _, d := range WeekDates(weekStart) {
		if len(d) >= 7 && !seen[d[:7]] {
			seen[d[:7]] = true
			months = append(months, d[:7])
		}
	}
	return months
}
