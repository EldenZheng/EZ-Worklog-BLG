package tests

import (
	"testing"
	"worklog/internal/menus/meeting"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"

	"fyne.io/fyne/v2/test"
)

// A long owner/repo#number used to set the width of its whole day column, so
// one busy repository pushed the week past the window's edge (and, on the
// meeting update, under the sidebar).
func TestCommitWeekFitsWindowWithLongIssueReferences(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := &shell.UI{Store: backend.NewStore(t.TempDir()), Win: a.NewWindow("t")}
	long := "someorganisation/a-very-long-repository-name-for-services#123456"
	byDay := map[string][]backend.Commit{}
	days := shell.WeekDates(backend.WeekStartOf(shell.Today()))
	for _, d := range days {
		byDay[d] = []backend.Commit{{Date: d, Repo: "someorganisation/a-very-long-repository-name-for-services", Sha: "0a1b2c3", Issue: long, Message: "Change"}}
	}
	if w := ui.CommitDaysPanel(days, byDay).MinSize().Width; w > 1280 {
		t.Fatalf("Commit List week needs %gpx, wider than the window", w)
	}
	meetingDays := meeting.MeetingWeekDates(shell.MeetingWeekStartOf(shell.Today()))
	if w := ui.CommitDaysPanelWithIssueLabel(meetingDays, byDay, meeting.MeetingIssueLabel).MinSize().Width; w > 1280*(1-meeting.MeetingRowsFraction) {
		t.Fatalf("meeting week needs %gpx, wider than its column", w)
	}
}
