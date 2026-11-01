package tests

import (
	"testing"
	"worklog/internal/menus/meeting"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func TestMeetingIssueLabelOmitsOrgOnlyForMeeting(t *testing.T) {
	issue := "exampleorg/sample-intranet#42"
	if got := meeting.MeetingIssueLabel(issue); got != "sample-intranet#42" {
		t.Fatalf("meeting issue label = %q, want repo and number only", got)
	}
	if got := shell.IssueTag(issue); got != issue {
		t.Fatalf("shared commit-list label changed to %q", got)
	}

	a := test.NewApp()
	defer a.Quit()
	ui := &shell.UI{Win: a.NewWindow("t")}
	commits := map[string][]backend.Commit{"2026-09-17": {{Date: "2026-09-17", Issue: issue}}}
	meetingLabels := labels(ui.CommitDaysPanelWithIssueLabel(
		[]string{"2026-09-17"}, commits, meeting.MeetingIssueLabel))
	if !contains(meetingLabels, "sample-intranet#42") || contains(meetingLabels, issue) {
		t.Fatalf("meeting labels should omit the org: %v", meetingLabels)
	}
	commitListLabels := labels(ui.CommitDaysPanel([]string{"2026-09-17"}, commits))
	if !contains(commitListLabels, issue) {
		t.Fatalf("commit-list labels should keep the org: %v", commitListLabels)
	}
}

func TestMeetingSidebarIsFixedAtTenPercent(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &shell.UI{Store: backend.NewStore(t.TempDir()), Win: w}
	ui.Cfg = backend.Config{Repos: []string{"exampleorg"}}
	w.SetContent(meeting.BuildMeetingTab(ui))
	meeting.DrawMeeting(ui)

	columns, ok := ui.MeetingBox.Objects[0].(*fyne.Container)
	if !ok || len(columns.Objects) != 2 {
		t.Fatalf("meeting columns missing: %T", ui.MeetingBox.Objects[0])
	}
	if _, fixed := columns.Layout.(meeting.MeetingColumnsLayout); !fixed {
		t.Fatalf("meeting uses resizable layout %T", columns.Layout)
	}
	if columns.Objects[0] != ui.MeetingCalendarHolder || columns.Objects[1] != ui.MeetingRowsHolder {
		t.Fatal("meeting columns should contain calendar then logged rows")
	}

	columns.Resize(fyne.NewSize(1000, 600))
	// The calendar stops a padding short of the sidebar so the two never touch.
	if got, want := ui.MeetingCalendarHolder.Size().Width, 900-theme.Padding(); got != want {
		t.Fatalf("calendar width = %g, want %g", got, want)
	}
	if got := ui.MeetingRowsHolder.Size().Width; got != 100 {
		t.Fatalf("sidebar width = %g, want 100", got)
	}
	if got := ui.MeetingRowsHolder.Position().X; got != 900 {
		t.Fatalf("sidebar x = %g, want 900", got)
	}
	if !contains(labels(ui.MeetingRowsHolder), "Logged this week") {
		t.Fatal("fixed sidebar should always show Logged this week")
	}
}
