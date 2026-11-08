package tests

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestSummaryTitleRendererFollowsScreenResize(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	card := shell.IssueSummaryCard(backend.Config{}, "org/repo#1", strings.Repeat("Long title ", 20), "120m")
	var title *shell.SummaryTitle
	walk(card, func(o fyne.CanvasObject) {
		if v, ok := o.(*shell.SummaryTitle); ok {
			title = v
		}
	})
	if title == nil {
		t.Fatal("missing summary title")
	}
	renderer := test.WidgetRenderer(title)
	for _, width := range []float32{500, 120, 350, 120, 500} {
		title.Resize(fyne.NewSize(width, 52))
		for _, child := range renderer.Objects() {
			if child.Size() != title.Size() {
				t.Fatalf("title kept an old renderer size after resize: title=%v text=%v", title.Size(), child.Size())
			}
		}
	}
}

func TestSummaryBubbleReflowsAfterMovingBetweenScreens(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	card := shell.IssueSummaryCard(backend.Config{}, "owner/long-repository-name#12345", strings.Repeat("A long title ", 15), "180m +3c")
	grid := shell.IssueSummaryGrid(card)
	for _, width := range []float32{600, 120, 400, 120, 600} {
		grid.Resize(fyne.NewSize(width, 800))
		if card.Size().Height < card.MinSize().Height {
			t.Fatalf("%g wide: card is shorter than its reflowed content", width)
		}
		walk(card, func(o fyne.CanvasObject) {
			if c, ok := o.(*fyne.Container); ok {
				if _, ok := c.Layout.(*widgets.SidesLayout); ok {
					for _, child := range c.Objects {
						if child.Position().X+child.Size().Width > c.Size().Width+0.1 || child.Position().Y+child.Size().Height > c.Size().Height+0.1 {
							t.Fatalf("%g wide: footer content escapes the bubble", width)
						}
					}
				}
			}
		})
	}
}

// settlesTo waits for the reflow that follows a window resize.
func settlesTo(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Parents ask for a wrapped child's height before the child has its new width,
// so moving the window between screens needs a second measure. Without it the
// last rows of cards fall off the end of the scroll (or leave a hole behind).
func TestSummaryPanelRemeasuresCardsWhenTheWindowChangesWidth(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")
	var cards []fyne.CanvasObject
	for i := 1; i <= 9; i++ {
		cards = append(cards, shell.IssueSummaryCard(backend.Config{}, fmt.Sprintf("owner/repo#%d", i), strings.Repeat("A long title ", 8), "120m"))
	}
	grid := shell.IssueSummaryGrid(cards...)
	shell.FitScreen(w, shell.IssueSummaryPanel("Pending", "9 issues", grid))
	for _, width := range []float32{1800, 500, 1800, 300, 900} {
		w.Resize(fyne.NewSize(width, 300))
		last := cards[len(cards)-1]
		settlesTo(t, fmt.Sprintf("%g wide: grid never matched its cards", width), func() bool {
			return grid.Size().Height == last.Position().Y+last.Size().Height
		})
	}
}

func TestWrappedLabelInColumnGetsItsHeightWhenTheWindowChangesWidth(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")
	label := widget.NewLabel(strings.Repeat("A sentence that has to wrap. ", 30))
	label.Wrapping = fyne.TextWrapWord
	below := sizedBox(0, 20)
	// Inside the tab bar, as every page is: the second measure has to get through it.
	tabs := shell.NewPillTabs(container.NewTabItem("Page", container.NewVBox(label, below)))
	shell.FitScreen(w, tabs)
	// Tall enough that the page always fits: a scroller only re-measures content
	// that outgrows it, and this is the case it leaves alone.
	for _, width := range []float32{1800, 900, 1800, 700} {
		w.Resize(fyne.NewSize(width, 1200))
		settlesTo(t, fmt.Sprintf("%g wide: label kept the height of its previous width", width), func() bool {
			return label.Size().Height == label.MinSize().Height && below.Position().Y >= label.Size().Height
		})
	}
}

func TestFlowGridMeasuresWrappedCardsAtTheirNewWidth(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	label := widget.NewLabel(strings.Repeat("A wrapping card title ", 12))
	label.Wrapping = fyne.TextWrapWord
	grid := container.New(widgets.NewFlowGrid(100, 0, 0), label)
	for _, width := range []float32{700, 150, 600, 150} {
		grid.Resize(fyne.NewSize(width, 1000))
		if label.Size().Height != label.MinSize().Height {
			t.Fatalf("%g wide: card height %g still uses previous wrapping, need %g", width, label.Size().Height, label.MinSize().Height)
		}
	}
}

func TestWeekBarCarriesPlansInPurpleAgainstTheWeeksGoal(t *testing.T) {
	parts := shell.PlannedBarPartsOf(backend.Config{}, map[string]int{"org": 1680}, map[string]int{"org": 336}, 336, 3360)
	last := parts[len(parts)-1]
	if last.Key != "@plan" || last.Fill != shell.PlannerPlanColor || last.Weight != 0.1 {
		t.Fatalf("end of the week bar: %+v", last)
	}
}

func TestPlannedTipEndsWithTimeLeftCentredInPurple(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	lines := []shell.PlannedLine{{Title: "Fix login", Minutes: 60}}
	var last *canvas.Text
	walk(shell.PlannedTipWithFree(120, lines), func(o fyne.CanvasObject) {
		if line, ok := o.(*canvas.Text); ok {
			last = line
		}
	})
	if last == nil || last.Text != "120m left without planned" || last.Color != shell.PlannerPlanColor || last.Alignment != fyne.TextAlignCenter {
		t.Fatalf("foot of the tooltip: %+v", last)
	}
	walk(shell.PlannedTipWithFree(0, lines), func(o fyne.CanvasObject) {
		if line, ok := o.(*canvas.Text); ok && strings.Contains(line.Text, "left") {
			t.Fatalf("a full day should not say what is left: %q", line.Text)
		}
	})
}

func TestLogWorkCalendarFitsRealNarrowWindow(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	for _, date := range shell.WeekDates(ui.WeekStart) {
		_, err := ui.Store.AppendRows([]backend.Row{{"date": date, "minutes": "90", "issue": "owner/long-repository-name#42", "description": "Saved work"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	ui.Win.Resize(fyne.NewSize(1100, 720))
	logwork.DrawWeekStrip(ui)
	var grid *fyne.Container
	walk(ui.WeekBox, func(o fyne.CanvasObject) {
		if c, ok := o.(*fyne.Container); ok {
			if _, ok := c.Layout.(*logwork.WeekGrid); ok {
				grid = c
			}
		}
	})
	if grid == nil || len(grid.Objects) != 7 {
		t.Fatal("seven-day calendar missing")
	}
	for _, day := range grid.Objects {
		if day.Position().Y != 0 || day.Position().X+day.Size().Width > grid.Size().Width+0.1 {
			t.Fatal("calendar day escaped the available width")
		}
	}
	if ui.Win.Canvas().Size().Width > 1100 {
		t.Fatalf("calendar forced the window wider: %v", ui.Win.Canvas().Size())
	}
}

func TestWeekGridKeepsSevenDaysWithinSmallScreen(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	days := weekOfBoxes(300, 220)
	grid := container.New(&logwork.WeekGrid{}, days...)
	for _, width := range []float32{2400, 1200, 900} {
		grid.Resize(fyne.NewSize(width, 220))
		for i, day := range days {
			if day.Position().Y != 0 || day.Position().X+day.Size().Width > width+0.1 {
				t.Fatalf("day %d outside %g-wide week: %v %v", i, width, day.Position(), day.Size())
			}
		}
	}
}

func TestSummaryGridRetainsThreeColumnsOnSmallScreen(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	cards := []fyne.CanvasObject{sizedBox(0, 100), sizedBox(0, 100), sizedBox(0, 100), sizedBox(0, 100)}
	grid := shell.IssueSummaryGrid(cards...)
	for _, width := range []float32{900, 600, 400} {
		grid.Resize(fyne.NewSize(width, 400))
		if cards[2].Position().Y != 0 || cards[3].Position().Y <= 0 {
			t.Fatalf("%g-wide summary should have three cards across", width)
		}
	}
}

func TestShortPageOnlyScrollsWhenListWouldBeTooSmall(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")
	top, body := sizedBox(0, 400), sizedBox(0, 0)
	scroll := shell.ShortPage(top, body, 260)
	w.SetContent(scroll)
	w.Resize(fyne.NewSize(1000, 900))
	scroll.ScrollToBottom()
	if scroll.Offset.Y != 0 {
		t.Fatal("large page should keep summary fixed")
	}
	w.Resize(fyne.NewSize(1000, 500))
	scroll.ScrollToBottom()
	if scroll.Offset.Y <= 0 || body.Size().Height < 260 {
		t.Fatalf("small page must scroll while retaining usable list: offset=%v body=%v", scroll.Offset, body.Size())
	}
	w.Resize(fyne.NewSize(1000, 900))
	if scroll.Offset.Y != 0 {
		t.Fatal("growing the page should clear the scroll offset")
	}
}
