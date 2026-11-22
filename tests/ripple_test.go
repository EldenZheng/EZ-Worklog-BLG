//go:build cursors

package tests

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
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
		// Timed by frames drawn, the ripple waits up to four times its span.
		if a.Duration == 4*shell.RippleTime {
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

// A click on a menu tab ripples its selected gold out from where it landed,
// as a sprint row does on the Kanban, while the tab let go of drains; the
// gear keeps its own click.
func TestClickingAMenuTabRipples(t *testing.T) {
	tabs := shell.NewPillTabs(
		container.NewTabItem(shell.LogListTabName, widget.NewLabel("log list")),
		container.NewTabItem(shell.KanbanTabName, widget.NewLabel("kanban")),
		container.NewTabItemWithIcon(shell.SettingsTabName, theme.SettingsIcon(), widget.NewLabel("settings")),
	)
	w, layer, driver := rippleWindow(t, tabs)
	gold := func() []*canvas.Rectangle {
		var found []*canvas.Rectangle
		walkRendered(tabs, func(o fyne.CanvasObject) {
			if r, ok := o.(*canvas.Rectangle); ok && r.Visible() && sameColor(r.FillColor, widgets.LogoGold) {
				found = append(found, r)
			}
		})
		return found
	}
	d := fyne.CurrentApp().Driver()
	logList, kanban := tabs.Views[0], tabs.Views[1]
	if g := gold(); len(g) != 1 || g[0].Size() != logList.Size() {
		t.Fatal("the open tab should stand in gold")
	}
	driver.anims = nil
	at := d.AbsolutePositionForObject(kanban).Add(fyne.NewPos(10, kanban.Size().Height/2))
	test.TapAt(kanban.(fyne.Tappable), at.Subtract(d.AbsolutePositionForObject(kanban)))
	_ = w
	if tabs.SelectedIndex() != 1 || len(layer.Objects) != 0 {
		t.Fatalf("a click on Kanban should open it with its own ripple, not the page's: open %d", tabs.SelectedIndex())
	}
	var motions []*fyne.Animation
	for _, a := range driver.anims {
		if a.Duration == 4*shell.RippleTime {
			motions = append(motions, a)
		}
	}
	if len(motions) < 2 {
		t.Fatalf("Log List should drain and Kanban fill, got %d motions", len(motions))
	}
	// The first frame starts the motion; the next, a frame's time later,
	// carries it on.
	for _, m := range motions {
		m.Tick(0)
	}
	time.Sleep(40 * time.Millisecond)
	for _, m := range motions {
		m.Tick(0.01)
	}
	var spreading *canvas.Rectangle
	for _, r := range gold() {
		if p := d.AbsolutePositionForObject(r); p.X >= d.AbsolutePositionForObject(kanban).X-1 {
			spreading = r
		}
	}
	if spreading == nil || spreading.Size().Width >= kanban.Size().Width ||
		d.AbsolutePositionForObject(spreading).X > at.X {
		t.Fatal("the gold should be spreading over Kanban from the click")
	}
	for _, m := range motions {
		m.Tick(1)
	}
	if g := gold(); len(g) != 1 || g[0].Size() != kanban.Size() || d.AbsolutePositionForObject(g[0]) != d.AbsolutePositionForObject(kanban) {
		t.Fatal("Kanban should end in gold and Log List back at rest")
	}
	driver.anims = nil
	test.Tap(tabs.Views[2].(fyne.Tappable))
	if tabs.SelectedIndex() != 2 || len(layer.Objects) != 0 {
		t.Fatalf("the gear should open Settings without the tabs' ripple: open %d", tabs.SelectedIndex())
	}
}

// A click that draws a heavy page holds up the frames; the ripple then carries
// on by one frame's time, not the time lost, so it is still seen.
func TestARippleOutlastsASlowFrame(t *testing.T) {
	tabs := shell.NewPillTabs(
		container.NewTabItem(shell.LogListTabName, widget.NewLabel("log list")),
		container.NewTabItem(shell.KanbanTabName, widget.NewLabel("kanban")),
	)
	_, _, driver := rippleWindow(t, tabs)
	driver.anims = nil
	test.Tap(tabs.Views[1].(fyne.Tappable))
	var motion *fyne.Animation
	for _, a := range driver.anims {
		if a.Duration == 4*shell.RippleTime {
			motion = a
		}
	}
	if motion == nil {
		t.Fatal("no motion for the tab")
	}
	motion.Tick(0)
	time.Sleep(300 * time.Millisecond) // the page takes its time to draw
	motion.Tick(0.1)
	var gold *canvas.Rectangle
	walkRendered(tabs, func(o fyne.CanvasObject) {
		if r, ok := o.(*canvas.Rectangle); ok && r.Visible() && sameColor(r.FillColor, widgets.LogoGold) &&
			fyne.CurrentApp().Driver().AbsolutePositionForObject(r).X >= fyne.CurrentApp().Driver().AbsolutePositionForObject(tabs.Views[1]).X {
			gold = r
		}
	})
	if gold != nil && gold.Size() == tabs.Views[1].Size() {
		t.Fatal("a slow frame should not carry the ripple to its end")
	}
	motion.Tick(1)
}

// A ripple that has spread over its whole box is rounded at the box's
// corners, as the hover pill under it is: no square corner lights round a
// rounded target.
func TestARippleKeepsToRoundedCorners(t *testing.T) {
	spot := canvas.NewRectangle(widgets.LogoGold)
	box := fyne.NewSize(120, 30)
	shell.SpreadIn(spot, fyne.NewPos(10, 20), box, fyne.NewPos(30, 15), 200, 8)
	if spot.Position() != fyne.NewPos(10, 20) || spot.Size() != box {
		t.Fatalf("a full ripple should cover its box: %v %v", spot.Position(), spot.Size())
	}
	for _, r := range []float32{spot.TopLeftCornerRadius, spot.TopRightCornerRadius, spot.BottomLeftCornerRadius, spot.BottomRightCornerRadius} {
		if r != 8 {
			t.Fatalf("each corner should be rounded as the pill is, got %v", r)
		}
	}
	shell.SpreadIn(spot, fyne.Position{}, box, fyne.NewPos(30, 15), 0, 8)
	if spot.Visible() {
		t.Fatal("a ripple with no size should not show")
	}
}
