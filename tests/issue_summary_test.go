package tests

import (
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/loglist"
	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestLogListIssuesScrollIndependentlyInsideThePage(t *testing.T) {
	ui := logListUI(t)
	defer fyne.CurrentApp().Quit()
	key := shell.StatusCacheKey("2026-08")
	for i := 1; i <= 40; i++ {
		ui.ProjItems[key] = append(ui.ProjItems[key], backend.WorklogItem{
			Date: "2026-08-25", Minutes: 10, Title: fmt.Sprintf("Issue %d", i),
			URL: fmt.Sprintf("https://github.com/exampleorg/repo/issues/%d", i),
		})
	}
	ui.ProjLoaded[key] = true
	ui.Win.Resize(fyne.NewSize(1500, 900))
	loglist.DrawLogList(ui)
	var scrolls []*container.Scroll
	walk(ui.ListBox, func(o fyne.CanvasObject) {
		if s, ok := o.(*container.Scroll); ok {
			if s.Direction == container.ScrollHorizontalOnly {
				return // the org key has its own sideways scroll
			}
			if _, page := s.Content.(*fyne.Container); page && contains(labels(s.Content), "Boards for this week") {
				return
			}
			scrolls = append(scrolls, s)
		}
	})
	if len(scrolls) != 2 {
		t.Fatalf("want two independent issue scroll areas, got %d", len(scrolls))
	}
	for _, s := range scrolls {
		if s.Size().Height <= 0 || s.Size().Height >= ui.Win.Canvas().Size().Height {
			t.Fatalf("issue list must fit inside the page: %v", s.Size())
		}
	}
	scrolls[0].ScrollToBottom()
	if scrolls[0].Offset.Y <= 0 || scrolls[1].Offset.Y != 0 {
		t.Fatalf("scrolling By issue moved the wrong area: %v / %v", scrolls[0].Offset, scrolls[1].Offset)
	}
	var boards fyne.CanvasObject
	walk(ui.ListBox, func(o fyne.CanvasObject) {
		if label, ok := o.(*widget.Label); ok && label.Text == "Boards for this week" {
			boards = label
		}
	})
	if boards == nil {
		t.Fatal("board links bubble missing")
	}
	y := fyne.CurrentApp().Driver().AbsolutePositionForObject(boards).Y
	if y+boards.Size().Height > ui.Win.Canvas().Size().Height {
		t.Fatal("board links were pushed below the visible page")
	}
	if y >= fyne.CurrentApp().Driver().AbsolutePositionForObject(scrolls[0]).Y {
		t.Fatal("board links should remain above the issue lists")
	}
}

func TestIssueBubblesFitSideBySideAndRevealFullTitlesOnHover(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	title := strings.Repeat("A long issue title ", 12)
	cards := []fyne.CanvasObject{
		shell.IssueSummaryCard(backend.Config{}, "org/repo#1", title, "120m"),
		shell.IssueSummaryCard(backend.Config{}, "org/repo#2", "Second", "60m"),
		shell.IssueSummaryCard(backend.Config{}, "Unfiled work", "Third", "30m"),
	}
	grid := shell.IssueSummaryGrid(cards...)
	w.SetContent(container.NewVScroll(grid))
	w.Resize(fyne.NewSize(900, 300))
	if cards[1].Position().X <= 0 || cards[1].Position().Y != cards[0].Position().Y {
		t.Fatal("wide issue lists should fit multiple bubbles per row")
	}
	var peek fyne.CanvasObject
	walk(cards[0], func(o fyne.CanvasObject) {
		if _, ok := o.(desktop.Hoverable); ok && o != cards[0] {
			peek = o
		}
	})
	if peek == nil {
		t.Fatal("title peek missing")
	}
	at := a.Driver().AbsolutePositionForObject(peek).Add(fyne.NewPos(8, 8))
	test.MoveMouse(w.Canvas(), at)
	firstCount := len(w.Canvas().Overlays().List())
	test.MoveMouse(w.Canvas(), at)
	overlays := w.Canvas().Overlays().List()
	fullTitle := false
	if len(overlays) == 1 {
		walkRendered(overlays[0], func(o fyne.CanvasObject) {
			if label, ok := o.(*widget.Label); ok && label.Text == title {
				fullTitle = true
			}
		})
	}
	if len(overlays) != 1 || !fullTitle {
		t.Fatalf("hover should retain a tooltip with the complete title: first=%d, now=%d, full title=%v", firstCount, len(overlays), fullTitle)
	}
	for _, obj := range []fyne.CanvasObject{cards[0], peek, overlays[0]} {
		cursor, ok := obj.(desktop.Cursorable)
		if !ok || cursor.Cursor() != desktop.PointerCursor {
			t.Errorf("%T should keep the pointer cursor, including while the tooltip is visible", obj)
		}
	}
	test.MoveMouse(w.Canvas(), fyne.NewPos(0, 290))
	if len(w.Canvas().Overlays().List()) != 0 {
		t.Fatal("title tooltip remained after leaving the button")
	}
	test.MoveMouse(w.Canvas(), at)
	shell.DismissSummaryTooltip()
	if len(w.Canvas().Overlays().List()) != 0 {
		t.Fatal("navigation should dismiss the title tooltip")
	}
	// The native wheel handler dismisses previews before hit testing, then
	// sends the same event to the scroller underneath (no lost first notch).
	w.Resize(fyne.NewSize(350, 120))
	at = a.Driver().AbsolutePositionForObject(peek).Add(fyne.NewPos(8, 8))
	test.MoveMouse(w.Canvas(), fyne.NewPos(0, 0))
	test.MoveMouse(w.Canvas(), at)
	preview, ok := w.Canvas().Overlays().Top().(interface{ DismissForScroll() })
	if !ok {
		t.Fatal("title preview cannot yield to the wheel")
	}
	preview.DismissForScroll()
	test.Scroll(w.Canvas(), at, 0, -40)
	if w.Content().(*container.Scroll).Offset.Y <= 0 {
		t.Fatal("first wheel event over a title did not move its list")
	}
}

func TestBoardCopyControlsShareGoldHoverAndCopyTheSelectedLinks(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := &shell.UI{Cfg: sampleExportConfig()}
	ui.Cfg.WorklogOwner = "team-alex"
	panel := logwork.WeekBoardsPanel(ui, "2026-08-24", "2026-08-30")
	w := a.NewWindow("boards for this week")
	defer w.Close()
	w.SetContent(panel)
	w.Resize(fyne.NewSize(500, 260))
	var buttons []*widget.Button
	var pills []*canvas.Rectangle
	walkRendered(panel, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && (b.Text == "Copy all" || b.Text == "Copy") {
			buttons = append(buttons, b)
		}
		if r, ok := o.(*canvas.Rectangle); ok && r.CornerRadius == 8 && r.StrokeWidth == 1 {
			pills = append(pills, r)
		}
	})
	links := backend.WeekBoardLinks(ui.Cfg, "team-alex", "2026-08-24", "2026-08-30")
	if len(buttons) != len(links)+1 || len(pills) != len(buttons) {
		t.Fatal("each copy control needs a gold hover pill")
	}
	mail := "I have attached the worklog combined for last week in this email\nhere is the github project link:"
	for _, link := range links {
		mail += "\n- " + link
	}
	want := append([]string{mail}, links...)
	driver := collectMotion(t)
	fill, outline := widgets.WithAlpha(widgets.LogoGold, 0.36), widgets.WithAlpha(widgets.LogoGold, 0.9)
	for i, button := range buttons {
		if button.Icon == nil {
			t.Fatalf("copy control %d is missing its icon", i)
		}
		pos := a.Driver().AbsolutePositionForObject(button)
		size := button.Size()
		at := pos.Add(fyne.NewPos(size.Width/2, size.Height/2))
		pill := pills[i]
		rest := pill.FillColor
		driver.anims = nil
		test.MoveMouse(w.Canvas(), at)
		if len(driver.anims) != 1 || driver.anims[0].Duration != widgets.HoverDuration || !sameColor(pill.FillColor, rest) {
			t.Fatalf("%q should start the Kanban fade when hovered", button.Text)
		}
		driver.anims[0].Tick(0.5)
		if sameColor(pill.FillColor, rest) || sameColor(pill.FillColor, fill) {
			t.Fatalf("%q should show a partial gold fill during the fade", button.Text)
		}
		driver.anims[0].Tick(1)
		if pill.Size() != size || !sameColor(pill.FillColor, fill) || !sameColor(pill.StrokeColor, outline) {
			t.Fatalf("%q should settle on the stronger gold fill and outline around the button", button.Text)
		}
		test.MoveMouse(w.Canvas(), fyne.Position{})
		if len(driver.anims) != 2 || driver.anims[1].Duration != widgets.HoverDuration {
			t.Fatalf("%q should fade out on exit", button.Text)
		}
		driver.anims[1].Tick(1)
		if !sameColor(pill.FillColor, rest) {
			t.Fatalf("%q should return to a clear highlight after exiting", button.Text)
		}
		test.TapCanvas(w.Canvas(), at)
		if got := a.Clipboard().Content(); got != want[i] {
			t.Fatalf("copy control %d put %q on the clipboard, want %q", i, got, want[i])
		}
	}
}
