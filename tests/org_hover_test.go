package tests

import (
	"strconv"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/test"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestOrgLegendSlidesGoldHoverAcrossOrgsAndSpecialKeys(t *testing.T) {
	base := test.NewApp()
	d := &collectDriver{Driver: base.Driver()}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: d})
	defer func() { fyne.SetCurrentApp(base); base.Quit() }()
	w := fyne.CurrentApp().NewWindow("test")
	var toggled string
	row := shell.OrgLegendWith(backend.Config{}, map[string]int{"a": 2, "b": 1}, nil, strconv.Itoa,
		func(key string) { toggled = key })
	w.SetContent(row)
	w.Resize(fyne.NewSize(800, 50))
	var chips []*shell.Tappable
	walk(row, func(o fyne.CanvasObject) {
		if t, ok := o.(*shell.Tappable); ok {
			chips = append(chips, t)
		}
	})
	if len(chips) != 4 {
		t.Fatal("both orgs and the appended draft/plan keys must share the group")
	}
	host := row.(*fyne.Container).Objects[0].(fyne.Widget)
	pill := test.WidgetRenderer(host).Objects()[0].(*canvas.Rectangle)
	chips[0].MouseIn(nil)
	first := d.anims[len(d.anims)-1]
	if first.Duration != 600*time.Millisecond {
		t.Fatal("org hover must use 600ms")
	}
	first.Tick(1)
	if !sameColor(pill.FillColor, widgets.WithAlpha(widgets.LogoGold, .16)) {
		t.Fatal("org filters must use the same gold fill as parent issues")
	}
	from := pill.Position().X
	chips[0].MouseOut()
	stale := d.anims[len(d.anims)-1]
	chips[2].MouseIn(nil)
	slide := d.anims[len(d.anims)-1]
	slide.Tick(.5)
	x := pill.Position().X
	if x <= from || x >= chips[2].Position().X {
		t.Fatal("the highlight must slide rather than jump to the next key")
	}
	stale.Tick(1)
	if pill.Position().X != x {
		t.Fatal("a cancelled exit must not interrupt the slide")
	}
	slide.Tick(1)
	chips[2].Tapped(nil)
	if toggled != shell.DraftsKey {
		t.Fatal("hover must preserve the draft filter action")
	}
	chips[2].MouseOut()
	d.anims[len(d.anims)-1].Tick(1)
	if _, _, _, alpha := pill.FillColor.RGBA(); alpha != 0 {
		t.Fatal("leaving must fade the shared highlight out")
	}
}

func TestNumberBubblesShareTravellingHoverOverTheirSurfaces(t *testing.T) {
	base := test.NewApp()
	d := &collectDriver{Driver: base.Driver()}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: d})
	defer func() { fyne.SetCurrentApp(base); base.Quit() }()
	w := fyne.CurrentApp().NewWindow("test")
	first := canvas.NewRectangle(widgets.LogoGold)
	first.SetMinSize(fyne.NewSize(160, 100))
	second := canvas.NewRectangle(shell.PlannerPlanColor)
	second.SetMinSize(fyne.NewSize(160, 100))
	row := shell.HoverCards(layout.NewHBoxLayout(), first, second)
	w.SetContent(row)
	w.Resize(fyne.NewSize(400, 120))
	// Opaque tile fills are below the shared highlight, not covering it up.
	host := row.Objects[len(row.Objects)-1].(fyne.Widget)
	pill := test.WidgetRenderer(host).Objects()[0].(*canvas.Rectangle)
	cards := row.Objects[0].(*fyne.Container)
	at := func(i int) fyne.Position {
		return d.AbsolutePositionForObject(cards.Objects[i]).Add(fyne.NewPos(30, 30))
	}
	test.MoveMouse(w.Canvas(), at(0))
	d.anims[len(d.anims)-1].Tick(1)
	from := pill.Position().X
	test.MoveMouse(w.Canvas(), at(1))
	slide := d.anims[len(d.anims)-1]
	if slide.Duration != 600*time.Millisecond {
		t.Fatal("number bubble hover must also take 600ms")
	}
	slide.Tick(.5)
	if x := pill.Position().X; x <= from || x >= cards.Objects[1].Position().X {
		t.Fatal("number bubbles must share a moving highlight")
	}
	if !sameColor(first.FillColor, widgets.LogoGold) || !sameColor(second.FillColor, shell.PlannerPlanColor) {
		t.Fatal("hover must preserve each metric's semantic colour")
	}
}

// Exercise real hit testing, including the gap between adjacent filter chips.
func TestFilterLegendsSlideThroughGaps(t *testing.T) {
	for _, kind := range []string{"org", "kanban projects", "calendar projects"} {
		t.Run(kind, func(t *testing.T) {
			ui, w := twoProjectFixture(t)
			base := fyne.CurrentApp()
			defer base.Quit()
			var content fyne.CanvasObject
			if kind == "org" {
				content = container.NewHScroll(shell.OrgFilter(backend.Config{}, map[string]int{"a": 1, "b": 1}, nil, nil, func(string) {}))
			} else {
				content = ui.Kanban.ProjectLegend(kind == "calendar projects")
			}
			w.SetContent(content)
			w.Resize(fyne.NewSize(800, 60))
			row := content.(*container.Scroll).Content.(*fyne.Container)
			host := row.Objects[0].(fyne.Widget)
			pill, ok := test.WidgetRenderer(host).Objects()[0].(*canvas.Rectangle)
			if !ok {
				t.Fatal("the legend must have one shared travelling highlight")
			}
			var chips []fyne.CanvasObject
			for _, object := range row.Objects[1:] {
				switch object.(type) {
				case *shell.Tappable, *shell.ProjectChip:
					chips = append(chips, object)
				}
			}
			if len(chips) != 2 {
				t.Fatal("expected two filter chips")
			}
			d := &collectDriver{Driver: base.Driver()}
			fyne.SetCurrentApp(&motionTestApp{App: base, driver: d})
			defer fyne.SetCurrentApp(base)
			at := func(o fyne.CanvasObject) fyne.Position {
				return d.AbsolutePositionForObject(o).Add(fyne.NewPos(o.Size().Width/2, o.Size().Height/2))
			}
			test.MoveMouse(w.Canvas(), at(chips[0]))
			if len(d.anims) == 0 {
				t.Fatal("pointer entry must start the shared hover")
			}
			d.anims[len(d.anims)-1].Tick(1)
			from := pill.Position().X
			left := d.AbsolutePositionForObject(chips[0])
			right := d.AbsolutePositionForObject(chips[1])
			gap := fyne.NewPos((left.X+chips[0].Size().Width+right.X)/2, at(chips[0]).Y)
			test.MoveMouse(w.Canvas(), gap)
			for _, animation := range d.anims {
				animation.Tick(1)
			}
			if _, _, _, alpha := pill.FillColor.RGBA(); alpha == 0 {
				t.Fatal("crossing a gap must keep the travelling highlight visible")
			}
			test.MoveMouse(w.Canvas(), at(chips[1]))
			slide := d.anims[len(d.anims)-1]
			slide.Tick(.5)
			if x := pill.Position().X; x <= from || x >= chips[1].Position().X {
				t.Fatal("the highlight must travel between chips instead of restarting in place")
			}
		})
	}
}
