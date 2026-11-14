package tests

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"

	"worklog/internal/menus/loglist"
	"worklog/internal/menus/logwork"
	"worklog/internal/menus/meeting"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestReviewDayCardsKeepTheSameHeight(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	for _, width := range []float32{180, 240, 360} {
		var height float32
		for _, kind := range []string{backend.KindOther, backend.KindCodeReview, backend.KindBulkReview} {
			row := backend.Row{"type": kind, "date": "2026-08-25", "minutes": "30", "issue": "org/repo#1"}
			if kind == backend.KindBulkReview {
				row["issue"] = ""
			}
			card := logwork.DayRowCard(ui, row, func() {})
			card.Resize(fyne.NewSize(width, 200))
			if height == 0 {
				height = card.MinSize().Height
			} else if card.MinSize().Height != height {
				t.Fatalf("width %g: %s height %g, want %g", width, kind, card.MinSize().Height, height)
			}
			if kind == backend.KindBulkReview && contains(labels(card), "no issue ref") {
				t.Fatal("bulk review must retain spacing without the missing-ref text")
			}
		}
	}
}

func assertSummaryReview(t *testing.T, root fyne.CanvasObject, title string, want bool) {
	t.Helper()
	found := false
	walk(root, func(o fyne.CanvasObject) {
		if card, ok := o.(*shell.StripeBubble); ok && contains(labels(card), title) {
			found = true
			if got := contains(labels(card), "Code review"); got != want {
				t.Fatalf("%q review badge=%v, want %v", title, got, want)
			}
			if want {
				assertOutlineTag(t, card, "Code review", shell.PlannerPlanColor)
				var refTag *fyne.Container
				walk(card, func(o fyne.CanvasObject) {
					if line, ok := o.(*fyne.Container); ok {
						if _, ok := line.Layout.(*widgets.RefTagLayout); ok {
							refTag = line
						}
					}
				})
				if refTag == nil || !contains(labels(refTag.Objects[0]), "repo#") || !contains(labels(refTag.Objects[1]), "Code review") {
					t.Fatalf("%q: review badge should be beside repo#number", title)
				}
			}
		}
	})
	if !found {
		t.Fatalf("missing summary card %q", title)
	}
}

func TestLogListReviewBadgesCoverPendingAndLoggedWork(t *testing.T) {
	ui := logListUI(t)
	defer fyne.CurrentApp().Quit()
	_, err := ui.Store.AppendRows([]backend.Row{
		{"type": backend.KindCodeReview, "date": "2026-08-25", "minutes": "30", "issue": "exampleorg/repo#1", "issue_title": "Pending review"},
		{"type": backend.KindOther, "date": "2026-08-25", "minutes": "30", "issue": "exampleorg/repo#2", "issue_title": "Older review", "description": "Code Review: 2026-08-25"},
		{"type": backend.KindCodeReview, "date": "2026-08-25", "minutes": "30", "issue": "exampleorg/repo#3", "pushed_at": "2026-08-25T12:00:00"},
		{"type": backend.KindCodeReview, "date": "2026-08-01", "minutes": "30", "issue": "exampleorg/repo#4", "pushed_at": "2026-08-01T12:00:00"},
		{"type": backend.KindBulkReview, "date": "2026-08-25", "minutes": "30", "issue_title": "Pending bulk"},
	})
	if err != nil {
		t.Fatal(err)
	}
	key := shell.StatusCacheKey("2026-08")
	ui.ProjLoaded[key] = true
	ui.ProjItems[key] = []backend.WorklogItem{
		{Date: "2026-08-25", Minutes: 30, URL: "https://github.com/exampleorg/repo/issues/3", Title: "Saved review on board"},
		{Date: "2026-08-25", Minutes: 30, URL: "https://github.com/exampleorg/repo/issues/4", Title: "Ordinary work this week"},
		{Date: "2026-08-25", Minutes: 30, URL: "https://github.com/exampleorg/repo/issues/5", Title: "Code Review: 2026-08-25", ParentURL: "https://github.com/exampleorg/repo/issues/6", ParentTitle: "Review from another device"},
		{Date: "2026-08-25", Minutes: 30, URL: "https://github.com/exampleorg/repo/issues/7", Title: "Alex Code Review: 2026-08-25"},
	}
	ui.PendingLoaded = true
	loglist.DrawLogList(ui)
	for _, title := range []string{"Pending review", "Older review", "Saved review on board", "Review from another device"} {
		assertSummaryReview(t, ui.ListBox, title, true)
	}
	for _, title := range []string{"Pending bulk", "Alex Code Review: 2026-08-25", "Ordinary work this week"} {
		assertSummaryReview(t, ui.ListBox, title, false)
	}
}

func TestMeetingUpdateSummaryShowsSingleReviewBadges(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w, MeetingStart: "2026-08-20", Cfg: backend.Config{Repos: []string{"exampleorg"}, WorklogOwner: "team-alex"}}
	ui.IssueInfo = map[string]backend.IssueInfo{
		"exampleorg/repo#1": {Title: "Single review"},
		"exampleorg/repo#2": {Title: "Older review"},
		"exampleorg/repo#3": {Title: "Ordinary work"},
	}
	_, err := ui.Store.AppendRows([]backend.Row{
		{"type": backend.KindCodeReview, "date": "2026-08-25", "minutes": "30", "issue": "exampleorg/repo#1"},
		{"type": backend.KindOther, "description": "Code Review: 2026-08-25", "date": "2026-08-25", "minutes": "30", "issue": "exampleorg/repo#2"},
		{"type": backend.KindOther, "date": "2026-08-25", "minutes": "30", "issue": "exampleorg/repo#3"},
		{"type": backend.KindCodeReview, "date": "2026-08-01", "minutes": "30", "issue": "exampleorg/repo#3"},
		{"type": backend.KindBulkReview, "date": "2026-08-25", "minutes": "30"},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.SetContent(meeting.BuildMeetingTab(ui))
	meeting.DrawMeeting(ui)
	assertSummaryReview(t, ui.MeetingRowsHolder, "Single review", true)
	assertSummaryReview(t, ui.MeetingRowsHolder, "Older review", true)
	assertSummaryReview(t, ui.MeetingRowsHolder, "Ordinary work", false)
	assertSummaryReview(t, ui.MeetingRowsHolder, backend.DailyCodeReviewTitle(ui.Cfg, "2026-08-25"), false)
}

// On a Log Work day card the tag follows the repo#number, not the minutes.
func TestDayCardReviewTagStandsBesideTheRef(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	card := logwork.DayRowCard(ui, backend.Row{"type": backend.KindCodeReview, "date": "2026-08-25", "minutes": "30", "issue": "org/repo#1"}, func() {})
	var line *fyne.Container
	walk(card, func(o fyne.CanvasObject) {
		if c, ok := o.(*fyne.Container); ok {
			if _, ok := c.Layout.(*logwork.RefTagLayout); ok {
				line = c
			}
		}
	})
	if line == nil || !contains(labels(line.Objects[0]), "repo#1") || !contains(labels(line.Objects[1]), "Code review") {
		t.Fatalf("the tag should sit with the reference: %+v", line)
	}

	ref, tag := sizedBox(10, 20), sizedBox(80, 20)
	row := container.New(&logwork.RefTagLayout{Full: 100}, ref, tag)
	row.Resize(fyne.NewSize(400, 20))
	if ref.Size().Width != 100 || tag.Position().X != 100 {
		t.Fatalf("room to spare: ref %v tag %v", ref.Size(), tag.Position())
	}
	row.Resize(fyne.NewSize(150, 20))
	if ref.Size().Width != 70 || tag.Position().X != 70 {
		t.Fatalf("the reference gives way first: ref %v tag %v", ref.Size(), tag.Position())
	}
	row.Resize(fyne.NewSize(100, 60))
	if tag.Position().Y <= 0 {
		t.Fatalf("too narrow to share a line, the tag drops below: %v", tag.Position())
	}
}

func TestReviewSummaryBadgeFitsNarrowCards(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	card := shell.IssueSummaryCardForRows(backend.Config{}, "owner/repo#1", "Reviewed changes", "120m", nil, true)
	grid := shell.IssueSummaryGrid(card)
	for _, width := range []float32{500, 120, 200, 500} {
		grid.Resize(fyne.NewSize(width, 800))
		grid.Refresh()
		walk(card, func(o fyne.CanvasObject) {
			if c, ok := o.(*fyne.Container); ok {
				switch c.Layout.(type) {
				case *widgets.SidesLayout, *widgets.WrapRowLayout:
					for _, child := range c.Objects {
						if child.Position().X+child.Size().Width > c.Size().Width+.1 || child.Position().Y+child.Size().Height > c.Size().Height+.1 {
							t.Fatalf("%g wide: badge/footer escapes card (%T)", width, c.Layout)
						}
					}
				}
			}
		})
	}
}
