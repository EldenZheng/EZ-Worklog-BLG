package backend

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"
)

func ProjectShortName(b SprintBoard) string {
	if ref, err := ParseProjectRef(b.URL); err == nil {
		return ref.Login
	}
	return b.Title
}

// FieldValueLabel names a status or sprint value the way the board shows it.
func FieldValueLabel(b SprintBoard, kind, value string) string {
	if kind == "position" {
		return ""
	}
	if kind == "status" {
		if value == "" {
			return "No status"
		}
		return value
	}
	if value == "" {
		return "No sprint"
	}
	for _, s := range b.Iterations() {
		if s.ID == value {
			return s.Title
		}
	}
	return value
}

func SprintKey(s SprintIteration) string { return fmt.Sprintf("%s/%d", s.StartDate, s.Duration) }

func OrDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// HoursMins renders minutes the way a timesheet reads them.
func HoursMins(m int) string {
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	if m%60 == 0 {
		return fmt.Sprintf("%dh", m/60)
	}
	return fmt.Sprintf("%dh %dm", m/60, m%60)
}

// SupportTitlePrefix is how a weekly support issue is named: "<Name>: Weekly
// Support (28th August, 12PM - 3PM)". A day already carrying one has its real
// shift on the board and must not have a second pencilled in on top.
func SupportTitlePrefix(cfg Config) string {
	return DisplayName(cfg) + ": Weekly Support"
}

// The support rota's issues: a person's weekly shift ("<Name>: Weekly Support
// (28th August, 12PM - 3PM)"), weekend support, and a team or group's support
// however it is written ("Team A Support", "Team A/Group A support",
// "Support - Group A").
var (
	rotaSupportTitle = regexp.MustCompile(`(?i):\s*weekly\s+support\b|\bweekend\s+support\b`)
	teamWord         = regexp.MustCompile(`(?i)\b(team|group)\b`)
	supportWord      = regexp.MustCompile(`(?i)\bsupport\b`)
)

// IsSupportIssue reports whether an issue title is one of the support rota's,
// which the Sprint Calendar leaves out: they are shifts, not planned work.
func IsSupportIssue(title string) bool {
	return rotaSupportTitle.MatchString(title) || teamWord.MatchString(title) && supportWord.MatchString(title)
}
