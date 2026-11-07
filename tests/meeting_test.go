package tests

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/menus/meeting"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestMeetingLoggedPaddingScrollsWithCards(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w, MeetingStart: "2026-08-20", Cfg: backend.Config{Repos: []string{"exampleorg"}}}
	var rows []backend.Row
	for i := 1; i <= 25; i++ {
		rows = append(rows, backend.Row{"date": "2026-08-26", "minutes": "60", "issue": fmt.Sprintf("exampleorg/repo#%d", i)})
	}
	if _, err := ui.Store.AppendRows(rows); err != nil {
		t.Fatal(err)
	}
	w.SetContent(meeting.BuildMeetingTab(ui))
	w.Resize(fyne.NewSize(1500, 900))
	meeting.DrawMeeting(ui)
	var scroll *container.Scroll
	var cards []*shell.Tappable
	var topRule, bottomRule *canvas.Rectangle
	walk(ui.MeetingRowsHolder, func(o fyne.CanvasObject) {
		if s, ok := o.(*container.Scroll); ok {
			scroll = s
		}
		if c, ok := o.(*shell.Tappable); ok {
			cards = append(cards, c)
		}
		if r, ok := o.(*canvas.Rectangle); ok && r.FillColor == widgets.LogoGold && r.CornerRadius == 0 {
			if topRule == nil {
				topRule = r
			} else {
				bottomRule = r
			}
		}
	})
	if scroll == nil || len(cards) != len(rows) || topRule == nil || bottomRule == nil {
		t.Fatal("logged issue list missing")
	}
	if scroll.Size().Width != ui.MeetingRowsHolder.Size().Width {
		t.Fatal("padding narrowed the sidebar scroll viewport")
	}
	at := a.Driver().AbsolutePositionForObject
	if got, want := at(scroll).Y, at(topRule).Y+topRule.Size().Height; got != want {
		t.Fatalf("scroll top = %g, want header line bottom %g", got, want)
	}
	if got, want := at(scroll).Y+scroll.Size().Height, at(bottomRule).Y; got != want {
		t.Fatalf("scroll bottom = %g, want footer line top %g", got, want)
	}
	first, last := cards[0], cards[len(cards)-1]
	side, end := theme.InnerPadding(), theme.InnerPadding()*1.5
	if got := at(first).X - at(scroll).X; got != side {
		t.Fatalf("left card padding = %g, want %g", got, side)
	}
	if got := at(scroll).X + scroll.Size().Width - at(first).X - first.Size().Width; got != side {
		t.Fatalf("right card padding = %g, want %g", got, side)
	}
	if got := at(first).Y - at(scroll).Y; got != end {
		t.Fatalf("initial top padding = %g, want %g", got, end)
	}
	scroll.ScrollToBottom()
	if scroll.Offset.Y <= 0 {
		t.Fatal("logged issues should scroll when they overflow")
	}
	if at(first).Y >= at(scroll).Y {
		t.Fatal("top padding should scroll out of the viewport with the cards")
	}
	if got := at(scroll).Y + scroll.Size().Height - at(last).Y - last.Size().Height; got != end {
		t.Fatalf("bottom padding at the end = %g, want %g", got, end)
	}
	scroll.ScrollToTop()
	if at(last).Y+last.Size().Height <= at(scroll).Y+scroll.Size().Height {
		t.Fatal("overflowing cards should continue past the viewport's bottom edge")
	}
}

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

func TestMeetingSidebarIsFixedAt15Percent(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w}
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
	// The calendar stops a gap short of the sidebar so the two panels never touch.
	if got, want := ui.MeetingCalendarHolder.Size().Width, 850-theme.InnerPadding(); got != want {
		t.Fatalf("calendar width = %g, want %g", got, want)
	}
	if got := ui.MeetingRowsHolder.Size().Width; got != 150 {
		t.Fatalf("sidebar width = %g, want 150", got)
	}
	if got := ui.MeetingRowsHolder.Position().X; got != 850 {
		t.Fatalf("sidebar x = %g, want 850", got)
	}
	if !contains(labels(ui.MeetingRowsHolder), "Logged this week") {
		t.Fatal("fixed sidebar should always show Logged this week")
	}
}
