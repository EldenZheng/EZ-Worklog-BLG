package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestMeetingIssueLabelOmitsOrgOnlyForMeeting(t *testing.T) {
	issue := "bigledger/blg-intranet#42"
	if got := meetingIssueLabel(issue); got != "blg-intranet#42" {
		t.Fatalf("meeting issue label = %q, want repo and number only", got)
	}
	if got := issueTag(issue); got != issue {
		t.Fatalf("shared commit-list label changed to %q", got)
	}

	a := test.NewApp()
	defer a.Quit()
	ui := &UI{win: a.NewWindow("t")}
	commits := map[string][]Commit{"2026-09-17": {{Date: "2026-09-17", Issue: issue}}}
	meetingLabels := labels(ui.commitDaysPanelWithIssueLabel(
		[]string{"2026-09-17"}, commits, meetingIssueLabel))
	if !contains(meetingLabels, "blg-intranet#42") || contains(meetingLabels, issue) {
		t.Fatalf("meeting labels should omit the org: %v", meetingLabels)
	}
	commitListLabels := labels(ui.commitDaysPanel([]string{"2026-09-17"}, commits))
	if !contains(commitListLabels, issue) {
		t.Fatalf("commit-list labels should keep the org: %v", commitListLabels)
	}
}

func TestMeetingSidebarIsFixedAtTenPercent(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w}
	ui.cfg = Config{Repos: []string{"bigledger"}}
	w.SetContent(ui.buildMeetingTab())
	ui.drawMeeting()

	columns, ok := ui.meetingBox.Objects[0].(*fyne.Container)
	if !ok || len(columns.Objects) != 2 {
		t.Fatalf("meeting columns missing: %T", ui.meetingBox.Objects[0])
	}
	if _, fixed := columns.Layout.(meetingColumnsLayout); !fixed {
		t.Fatalf("meeting uses resizable layout %T", columns.Layout)
	}
	if columns.Objects[0] != ui.meetingCalendarHolder || columns.Objects[1] != ui.meetingRowsHolder {
		t.Fatal("meeting columns should contain calendar then logged rows")
	}

	columns.Resize(fyne.NewSize(1000, 600))
	if got := ui.meetingCalendarHolder.Size().Width; got != 900 {
		t.Fatalf("calendar width = %g, want 900", got)
	}
	if got := ui.meetingRowsHolder.Size().Width; got != 100 {
		t.Fatalf("sidebar width = %g, want 100", got)
	}
	if got := ui.meetingRowsHolder.Position().X; got != 900 {
		t.Fatalf("sidebar x = %g, want 900", got)
	}
	if !contains(labels(ui.meetingRowsHolder), "Logged this week") {
		t.Fatal("fixed sidebar should always show Logged this week")
	}
}
