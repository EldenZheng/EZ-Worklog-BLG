package backend

import (
	"strings"
	"time"
)

// Keep range traversal finite if a malformed persisted date reaches the UI.
func ValidPlannerDate(date string) bool { _, err := time.Parse("2006-01-02", date); return err == nil }

const (
	// workDayStart is the hour at which the app rolls over to a new day. Work
	// that runs past midnight belongs to the day it started on, not to the one
	// the clock has just turned over into: at 1am the day still owing minutes is
	// yesterday, and that is the day a new entry should default to.
	workDayStart = 8

	WeekColMinHeight = 168 // a floor for an empty day, not a ceiling for a full one
	weekCardChars    = 44  // the card's own title, which wraps rather than elides

	// How much green a finished day takes. A day finished on GitHub is stated;
	// one only finished by counting drafts is a paler claim, because it is one.
	WeekDoneTint  = 0.22
	WeekDraftTint = 0.10

	// How long a Copy button says "Copied" before going back to its own label.
	CopiedFlash = 1500 * time.Millisecond

	// supportPlaceholderMins is the weekday support shift the placeholder pencils
	// in — three hours, the length of a rota slot.
	SupportPlaceholderMins = 180
)

// WorkDate is the working day a moment belongs to. See workDayStart.
func WorkDate(t time.Time) string {
	if t.Hour() < workDayStart {
		t = t.AddDate(0, 0, -1)
	}
	return t.Format("2006-01-02")
}

// WeekStartOf is the Monday of the week a date falls in.
func WeekStartOf(date string) string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(date))
	if err != nil {
		t = time.Now()
	}
	off := (int(t.Weekday()) + 6) % 7 // Sunday is 0 in Go, and last here
	return t.AddDate(0, 0, -off).Format("2006-01-02")
}
