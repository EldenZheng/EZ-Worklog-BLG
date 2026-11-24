package tests

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/menus/status"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// Each worklog bubble in the Status day panel keeps its heading line and
// parent issue in view and folds the rest away under a gold arrow. It starts
// shut, a tap on the heading slides it open, and the panel keeps it open when
// redrawn.
func TestStatusDayBubblesFold(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())
	w := a.NewWindow("t")
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w, CalMonth: "2026-09", RepMonth: "2026-09"}
	ui.Cfg = backend.Config{Repos: []string{"exampleorg"}}
	w.SetContent(status.BuildStatusTab(ui))

	ui.EnsureProjectCache()
	key := shell.StatusCacheKey("2026-09")
	ui.ProjItems[key] = []backend.WorklogItem{{
		Date: "2026-09-08", Minutes: 60, Title: "Worklog 2026-09-08",
		URL:         "https://github.com/exampleorg/sample-intranet/issues/901",
		ParentTitle: "EXPORT FIXES",
		ParentURL:   "https://github.com/exampleorg/sample-intranet/issues/43",
		Remarks:     "Fixed the export header",
	}}
	ui.ProjLoaded[key] = true
	ui.SelDay = "2026-09-08"
	status.DrawDayPanel(ui)

	find := func() (*shell.Fold, *shell.Tappable) {
		var fold *shell.Fold
		var head *shell.Tappable
		walk(ui.DaySwap, func(o fyne.CanvasObject) {
			switch v := o.(type) {
			case *shell.Fold:
				fold = v
			case *shell.Tappable:
				if head == nil {
					head = v
				}
			}
		})
		return fold, head
	}
	fold, head := find()
	if fold == nil || head == nil {
		t.Fatal("the worklog bubble should have a fold under a tappable heading")
	}
	if fold.Open() || fold.Visible() {
		t.Fatal("the bubble should start folded")
	}
	if !contains(labels(head), "60 min") {
		t.Fatalf("the heading should keep the minutes in view: %v", labels(head))
	}

	if !contains(labels(head), "EXPORT FIXES") || contains(labels(fold), "EXPORT FIXES") {
		t.Fatalf("the parent issue should stay in view, not fold away: %v", labels(head))
	}

	head.Tapped(&fyne.PointEvent{})
	if !fold.Open() || !fold.Visible() {
		t.Fatal("a tap on the heading should open the bubble")
	}

	status.DrawDayPanel(ui)
	if fold, _ = find(); fold == nil || !fold.Open() {
		t.Fatal("the bubble should stay open when the panel is redrawn")
	}
}
