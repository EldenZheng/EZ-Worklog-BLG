//go:build cursors

package tests

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// rippleWindow shows content in a window built as the app builds it, and
// returns the window's top layer, where ripples play.
func rippleWindow(t *testing.T, content fyne.CanvasObject) (fyne.Window, *fyne.Container, *collectDriver) {
	t.Helper()
	a, driver := pressTestApp(t)
	w := a.NewWindow("ripple")
	t.Cleanup(w.Close)
	shell.FitScreen(w, content)
	w.Resize(fyne.NewSize(600, 400))
	// FitScreen measures the page again on a timer; let it run now, not in
	// the middle of the next test (text shaping is not safe to run at once).
	time.Sleep(150 * time.Millisecond)
	return w, w.Content().(*fyne.Container).Objects[1].(*fyne.Container), driver
}

// rippleAnim is the ripple's motion among those the driver started.
func rippleAnim(t *testing.T, driver *collectDriver) *fyne.Animation {
	t.Helper()
	for _, a := range driver.anims {
		if a.Duration == shell.RippleTime {
			return a
		}
	}
	t.Fatal("no ripple started")
	return nil
}

// A click on a filter the gold hover travels over ripples from where it
// landed, over the page, and the ripple is gone once it has spread.
func TestClickingAFilterRipples(t *testing.T) {
	tapped := 0
	filter := shell.NewTappable(widget.NewLabel("Open"), func() { tapped++ })
	quiet := shell.NewTappable(widget.NewLabel("Nothing"), nil)
	_, layer, driver := rippleWindow(t, shell.FilterGroup(layout.NewHBoxLayout(), filter, quiet))
	test.Tap(quiet)
	if len(layer.Objects) != 0 {
		t.Fatal("a filter that does nothing should not ripple")
	}
	test.Tap(filter)
	if tapped != 1 || len(layer.Objects) != 1 {
		t.Fatalf("the click should run once and ripple: ran %d, %d ripples", tapped, len(layer.Objects))
	}
	rippleAnim(t, driver).Tick(1)
	if len(layer.Objects) != 0 {
		t.Fatal("the ripple should be gone once it has spread")
	}
}

// A row with a check ticks it from anywhere on the row, and a click on the
// box itself ripples the row too.
func TestATickedRowRipplesFromTheBox(t *testing.T) {
	var changes []bool
	tick := widgets.NewTickCheck(false, nil, func(on bool) { changes = append(changes, on) })
	row := shell.NewTappable(container.NewBorder(nil, nil, tick, nil, widget.NewLabel("Parent issue")), tick.Toggle)
	row.Ripple = true
	row.RippleClear = tick
	tick.OnTapAt = func(at fyne.Position) { shell.RippleBeside(row, shell.RippleClearOf(row, tick), at) }
	w, layer, _ := rippleWindow(t, container.NewVBox(row))

	test.Tap(row)
	if !tick.Checked() || len(layer.Objects) != 1 {
		t.Fatalf("a click on the row should tick the box and ripple: ticked %v, %d ripples", tick.Checked(), len(layer.Objects))
	}
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(tick).Add(fyne.NewPos(tick.Size().Width/2, tick.Size().Height/2))
	test.TapCanvas(w.Canvas(), at)
	if tick.Checked() || len(layer.Objects) != 2 {
		t.Fatalf("a click on the box should untick it and ripple the row: ticked %v, %d ripples", tick.Checked(), len(layer.Objects))
	}
	if len(changes) != 2 {
		t.Fatalf("each click should be heard once, got %v", changes)
	}
	// Neither ripple reaches over the box, where the tick draws itself.
	boxRight := fyne.CurrentApp().Driver().AbsolutePositionForObject(tick).X + tick.Size().Width
	layerX := fyne.CurrentApp().Driver().AbsolutePositionForObject(layer).X
	for _, o := range layer.Objects {
		if layerX+o.Position().X < boxRight {
			t.Fatalf("a ripple starts at %v, over the box ending at %v", layerX+o.Position().X, boxRight)
		}
	}
}

// A check in a compact check group ripples as it is clicked.
func TestACompactCheckRipples(t *testing.T) {
	var picked []string
	group := widgets.NewCompactCheckGroup([]string{"Todo", "Done"}, func(selected []string) { picked = selected })
	w, layer, _ := rippleWindow(t, container.NewVBox(group))
	test.TapCanvas(w.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(group).Add(fyne.NewPos(12, 8)))
	if len(picked) != 1 || len(layer.Objects) != 1 {
		t.Fatalf("the click should tick the check and ripple: picked %v, %d ripples", picked, len(layer.Objects))
	}
	if w.Canvas().Focused() != nil {
		t.Fatal("the check should not take the focus, which lights its box")
	}
	// The ripple keeps off the box: it starts right of it.
	box := fyne.CurrentApp().Driver().AbsolutePositionForObject(group).X + 12
	if at := fyne.CurrentApp().Driver().AbsolutePositionForObject(layer).X + layer.Objects[0].Position().X; at <= box {
		t.Fatalf("the ripple should start beside the box, not at %v", at)
	}
}
