package tests

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/widgets"
)

func sizedBox(w, h float32) fyne.CanvasObject {
	r := canvas.NewRectangle(nil)
	r.SetMinSize(fyne.NewSize(w, h))
	return r
}

func weekOfBoxes(width, height float32) []fyne.CanvasObject {
	var days []fyne.CanvasObject
	for i := 0; i < 7; i++ {
		days = append(days, sizedBox(width, height))
	}
	return days
}

// A wide card must not make the week, and with it the window, seven times wider
// than the card: the days wrap instead.
func TestDayGridWrapsInsteadOfForcingTheWindowWide(t *testing.T) {
	test.NewApp()
	days := weekOfBoxes(180, 100)
	g := widgets.NewDayGridLayout(days, 0)
	grid := container.New(g, days...)
	if w := g.MinSize(days).Width; w != 180 {
		t.Fatalf("the week should ask for one day's width, got %g", w)
	}
	for _, tc := range []struct {
		width float32
		cols  int
	}{{2000, 7}, {1300, 7}, {1000, 4}, {500, 2}, {300, 1}} {
		grid.Resize(fyne.NewSize(tc.width, 800))
		if g.Cols != tc.cols {
			t.Fatalf("%g wide: %d columns, want %d", tc.width, g.Cols, tc.cols)
		}
	}
	// At four columns the days flow onto a second row, and the height follows.
	grid.Resize(fyne.NewSize(1000, 800))
	if days[4].Position().X != 0 || days[4].Position().Y <= days[0].Position().Y {
		t.Fatalf("Friday should start the next row: %v", days[4].Position())
	}
	if h := g.MinSize(days).Height; h <= 200 {
		t.Fatalf("two rows should need more than two days' height, got %g", h)
	}
}

// With room for all seven the grid is exactly the plain seven-column grid.
func TestDayGridMatchesPlainGridWhenAllFit(t *testing.T) {
	test.NewApp()
	size := fyne.NewSize(2500, 400)
	plain := weekOfBoxes(180, 100)
	container.NewGridWithColumns(7, plain...).Resize(size)
	days := weekOfBoxes(180, 100)
	container.New(widgets.NewDayGridLayout(days, 0), days...).Resize(size)
	for i := range days {
		if days[i].Position() != plain[i].Position() || days[i].Size() != plain[i].Size() {
			t.Fatalf("day %d moved: %v %v, plain %v %v", i, days[i].Position(), days[i].Size(), plain[i].Position(), plain[i].Size())
		}
	}
}

// Tiles pack tighter below the compact width and are untouched at or above it.
func TestFlowGridCompactOnlyBelowItsWidth(t *testing.T) {
	test.NewApp()
	plain := widgets.NewFlowGrid(logwork.BubbleMinWidth, logwork.BubbleMaxWidth, logwork.BubbleHeight)
	compact := widgets.NewFlowGrid(logwork.BubbleMinWidth, logwork.BubbleMaxWidth, logwork.BubbleHeight).
		Compact(logwork.BubbleCompactBelow, logwork.BubbleCompactMinWidth)
	for _, w := range []float32{logwork.BubbleCompactBelow, 2500} {
		pc, pw := plain.Columns(w, 20)
		cc, cw := compact.Columns(w, 20)
		if pc != cc || pw != cw {
			t.Fatalf("%g wide changed: %d/%g, plain %d/%g", w, cc, cw, pc, pw)
		}
	}
	if pc, _ := plain.Columns(1200, 20); pc != 3 {
		t.Fatalf("plain at 1200: %d columns", pc)
	}
	if cc, _ := compact.Columns(1200, 20); cc != 4 {
		t.Fatalf("compact at 1200: %d columns", cc)
	}
	tiles := []fyne.CanvasObject{sizedBox(10, 90), sizedBox(10, 90)}
	grid := container.New(compact, tiles...)
	grid.Resize(fyne.NewSize(1200, 400))
	if h := tiles[0].Size().Height; h != 90 {
		t.Fatalf("a compact tile should be as tall as its content, got %g", h)
	}
	grid.Resize(fyne.NewSize(2500, 400))
	if h := tiles[0].Size().Height; h != logwork.BubbleHeight {
		t.Fatalf("a wide tile keeps its height, got %g", h)
	}
}
