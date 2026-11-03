package tests

import (
	"testing"

	"worklog/internal/shared/backend"
)

// The support rota's issues stay off the Sprint Calendar; ordinary issues
// that only mention support do not.
func TestIsSupportIssue(t *testing.T) {
	for title, want := range map[string]bool{
		"Team A Support":         true,
		"Group A Support":        true,
		"Team A/Group A Support": true,
		"Elden Zheng: Weekly Support (28th August, 12PM - 3PM)": true,
		"Weekend Support 2026-08-15":                            true,
		"Team A / Group A - Support":                            true,
		"Support - Group A":                                     true,
		"GROUP A SUPPORT (weekday)":                             true,
		"Fix invoice rounding":                                  false,
		"Add support for CSV export":                            false,
		"Support ticket follow-up for ACME":                     false,
	} {
		if got := backend.IsSupportIssue(title); got != want {
			t.Errorf("IsSupportIssue(%q) = %v, want %v", title, got, want)
		}
	}
}
