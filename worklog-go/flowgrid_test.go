package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Tiles must use the whole row. A fixed cell width leaves a strip of dead space
// on the right at every window size that is not an exact multiple of it, which
// is the thing this layout exists to remove.
func TestFlowGridFillsTheWidth(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	g := newFlowGrid(bubbleMinWidth, bubbleMaxWidth, bubbleHeight)
	// 1000px fits two 340px tiles with 320 to spare; both should absorb it.
	cols, cellW := g.columns(1000, 6)
	if cols != 2 {
		t.Fatalf("1000px should hold 2 tiles of %g, got %d", float32(bubbleMinWidth), cols)
	}
	used := cellW*float32(cols) + g.gap()*float32(cols-1)
	if used < 1000-1 {
		t.Fatalf("tiles used %g of 1000px — the rest is dead space", used)
	}
	if cellW < bubbleMinWidth {
		t.Fatalf("cell %g is under the %d minimum", cellW, bubbleMinWidth)
	}

	// Narrow window: one per row, still full width.
	if cols, cellW := g.columns(500, 6); cols != 1 || cellW < 499 {
		t.Fatalf("a narrow window should hold one full-width tile, got %d × %g", cols, cellW)
	}

	// Wide window: more per row, and the count follows the width rather than a
	// hard-coded density.
	if cols, _ := g.columns(1800, 9); cols != 5 {
		t.Fatalf("1800px should hold 5 tiles, got %d", cols)
	}
}

// A lone tile shares nothing, so it would otherwise stretch across a maximised
// window with a line of text lost in the middle of it.
func TestFlowGridCapsLonelyTiles(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	g := newFlowGrid(bubbleMinWidth, bubbleMaxWidth, bubbleHeight)
	cols, cellW := g.columns(1800, 1)
	if cols != 1 {
		t.Fatalf("one tile needs one column, got %d", cols)
	}
	if cellW != bubbleMaxWidth {
		t.Fatalf("a lone tile should stop at %d, got %g", bubbleMaxWidth, cellW)
	}
	// Two tiles on a wide window split it rather than sitting at the minimum.
	if cols, cellW := g.columns(1200, 2); cols != 2 || cellW < bubbleMinWidth {
		t.Fatalf("two tiles should share the row, got %d × %g", cols, cellW)
	}
}

// MinSize reports the height of every row, or the scroll above it clips the
// tiles that wrapped.
func TestFlowGridReportsEveryRow(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	g := newFlowGrid(bubbleMinWidth, bubbleMaxWidth, bubbleHeight)
	var objs []fyne.CanvasObject
	for i := 0; i < 5; i++ {
		objs = append(objs, canvas.NewRectangle(nil))
	}
	g.Layout(objs, fyne.NewSize(1000, 400)) // 2 per row, so 3 rows
	if g.rows != 3 {
		t.Fatalf("5 tiles at 2 per row is 3 rows, got %d", g.rows)
	}
	want := bubbleHeight*3 + g.gap()*2
	if got := g.MinSize(objs).Height; got != want {
		t.Fatalf("grid asks for %g of height, needs %g", got, want)
	}

	// Tiles are placed inside the width they were given, side by side, no
	// overlap: the second sits one cell plus one gap along.
	_, cellW := g.columns(1000, 5)
	if p := objs[1].Position(); p.X != cellW+g.gap() || p.Y != 0 {
		t.Fatalf("second tile sits at %v, want x=%g y=0", p, cellW+g.gap())
	}
	if p := objs[2].Position(); p.X != 0 || p.Y != bubbleHeight+g.gap() {
		t.Fatalf("third tile should start row two, sits at %v", p)
	}
	if w := objs[0].Size().Width; w != cellW {
		t.Fatalf("tile was sized %g, cell is %g", w, cellW)
	}
}

// An invisible child takes no cell — otherwise a hidden tile leaves a hole and
// the count that decides the row breaks is wrong.
func TestFlowGridSkipsHiddenTiles(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	g := newFlowGrid(bubbleMinWidth, bubbleMaxWidth, bubbleHeight)
	objs := []fyne.CanvasObject{
		canvas.NewRectangle(nil), canvas.NewRectangle(nil), canvas.NewRectangle(nil),
	}
	objs[1].Hide()
	g.Layout(objs, fyne.NewSize(1000, 400))
	if g.rows != 1 {
		t.Fatalf("two visible tiles fit one row, got %d", g.rows)
	}
	if p := objs[2].Position(); p.Y != 0 {
		t.Fatalf("the visible tiles should share a row, third sits at %v", p)
	}
}

// The bottom child reaches the bottom edge, and a long top child is capped
// there rather than pushing it off — the remarks stay on screen whatever the
// commit list above them does.
func TestTopFillGivesTheRestToTheBottom(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	fields := canvas.NewRectangle(nil)
	fields.SetMinSize(fyne.NewSize(300, 120))
	rest := canvas.NewRectangle(nil)
	rest.SetMinSize(fyne.NewSize(300, 100))
	objs := []fyne.CanvasObject{fields, rest}

	l := newTopFill(0.45, fields)
	size := fyne.NewSize(800, 600)
	l.Layout(objs, size)

	if h := fields.Size().Height; h != 120 {
		t.Fatalf("short fields should take only what they ask for, got %g", h)
	}
	if got := rest.Position().Y + rest.Size().Height; got != size.Height {
		t.Fatalf("the bottom child ends at %g, should reach %g", got, size.Height)
	}

	fields.SetMinSize(fyne.NewSize(300, 900)) // a group with a long commit list
	l.Layout(objs, size)
	if got, want := fields.Size().Height, size.Height*0.45; got != want {
		t.Fatalf("tall fields took %g, should stop at %g and scroll", got, want)
	}
	if h := rest.Size().Height; h < rest.MinSize().Height {
		t.Fatalf("the bottom child was squeezed to %g, under its %g minimum", h, rest.MinSize().Height)
	}
}

// Columns take their share of the row, not an equal slice, and they keep it as
// the window grows — that is the whole point over a GridWithColumns.
func TestRatioRowSharesByWeight(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	r := newRatioRow(0.2, 0.6, 0.2)
	objs := []fyne.CanvasObject{
		canvas.NewRectangle(nil), canvas.NewRectangle(nil), canvas.NewRectangle(nil),
	}
	for _, w := range []float32{600, 1400} {
		r.Layout(objs, fyne.NewSize(w, 40))
		avail := w - theme.Padding()*2
		wide, narrow := objs[1].Size().Width, objs[0].Size().Width
		if got, want := wide, avail*0.6; got < want-0.5 || got > want+0.5 {
			t.Fatalf("at %gpx the middle column got %g, want %g", w, got, want)
		}
		if wide < narrow*2 {
			t.Fatalf("at %gpx the weighting was lost: %g vs %g", w, wide, narrow)
		}
		if objs[0].Position().X != 0 {
			t.Fatalf("first column should start at the left edge, got %v", objs[0].Position())
		}
		if x := objs[2].Position().X; x <= objs[1].Position().X {
			t.Fatalf("columns overlap: %g then %g", objs[1].Position().X, x)
		}
	}
}

// A hidden column hands its share to the rest instead of leaving a hole.
func TestRatioRowSkipsHiddenColumns(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	r := newRatioRow(0.5, 0.5)
	objs := []fyne.CanvasObject{canvas.NewRectangle(nil), canvas.NewRectangle(nil)}
	objs[1].Hide()
	r.Layout(objs, fyne.NewSize(600, 40))
	if got := objs[0].Size().Width; got != 600 {
		t.Fatalf("the only visible column should take the row, got %g", got)
	}
}

// The row is wide enough only when its tightest column fits: a column owning a
// tenth of the width needs ten times its own minimum before it stops clipping.
func TestRatioRowMinSizeScalesTheTightestColumn(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	r := newRatioRow(0.9, 0.1)
	small := canvas.NewRectangle(nil)
	small.SetMinSize(fyne.NewSize(50, 20))
	big := canvas.NewRectangle(nil)
	big.SetMinSize(fyne.NewSize(90, 30))
	objs := []fyne.CanvasObject{big, small}

	min := r.MinSize(objs)
	if want := float32(500) + theme.Padding(); min.Width < want-0.5 || min.Width > want+0.5 {
		t.Fatalf("row min width %g, want %g so the 10%% column fits", min.Width, want)
	}
	if min.Height != 30 {
		t.Fatalf("row must be as tall as its tallest cell, got %g", min.Height)
	}
}

// fyne sizes a Select from its placeholder, so "(Select one)" left "Worklog
// sub-issue" running out under the arrow. The wrapper must be wide enough for
// the longest option, whatever is currently picked.
func TestWideSelectFitsItsLongestOption(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	opts := []string{"Worklog sub-issue", "The issue itself"}
	sel := widget.NewSelect(opts, nil)
	sel.SetSelected(opts[1]) // the short one — the box must still fit the long one
	wrapped := wideSelect(sel)

	longest := float32(0)
	for _, o := range opts {
		if w := fyne.MeasureText(o, theme.TextSize(), fyne.TextStyle{}).Width; w > longest {
			longest = w
		}
	}
	got := wrapped.MinSize().Width
	if got < longest+theme.IconInlineSize() {
		t.Fatalf("dropdown asks for %g, needs %g for %q plus the arrow",
			got, longest+theme.IconInlineSize(), opts[0])
	}
	if got <= sel.MinSize().Width {
		t.Fatalf("wrapper %g is no wider than the placeholder-sized select %g",
			got, sel.MinSize().Width)
	}
}
