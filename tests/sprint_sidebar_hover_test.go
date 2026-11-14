package tests

import (
	"fmt"
	"math"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"worklog/internal/menus/kanban"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestSprintSidebarHighlightTravelsIntoOpenFolds(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	k := ui.Kanban
	k.Filter = "all"
	k.SprintsShowAll, k.CompletedOpen = true, true
	b := &k.Plan.Boards[0]
	for i := 1; i <= 11; i++ {
		s := backend.SprintIteration{ID: fmt.Sprint(i), Title: fmt.Sprintf("Hover sprint %02d", i), StartDate: backend.AddDays(shell.Today(), i*14), Duration: 14}
		b.Fields[0].Configuration.Iterations = append(b.Fields[0].Configuration.Iterations, s)
		b.Items = append(b.Items, backend.SprintItem{ID: "item" + s.ID, Title: s.Title, SprintID: s.ID})
	}
	past := backend.SprintIteration{ID: "past", Title: "Hover completed", StartDate: backend.AddDays(shell.Today(), -28), Duration: 14}
	b.Fields[0].Configuration.Iterations = append(b.Fields[0].Configuration.Iterations, past)
	b.Items = append(b.Items, backend.SprintItem{ID: "past-item", Title: "Done", SprintID: "past"}, backend.SprintItem{ID: "no-sprint", Title: "Unscheduled"})
	w.SetContent(k.Root)
	w.Resize(fyne.NewSize(1400, 3000))
	kanban.Draw(k)
	var pill *canvas.Rectangle
	walk(k.SideClip, func(o fyne.CanvasObject) {
		if box, ok := o.(*fyne.Container); ok && len(box.Objects) == 2 && contains(labels(box.Objects[0]), "Hover sprint 11") {
			if host, ok := box.Objects[1].(fyne.Widget); ok {
				if objects := test.WidgetRenderer(host).Objects(); len(objects) == 1 {
					if rect, ok := objects[0].(*canvas.Rectangle); ok && rect.CornerRadius == 8 && rect.StrokeWidth == 1 {
						pill = rect
					}
				}
			}
		}
	})
	if pill == nil {
		t.Fatalf("sprint sidebar needs a shared overlay highlight: %T %v", k.SideClip, labels(k.SideClip))
	}
	base := fyne.CurrentApp()
	d := &collectDriver{Driver: base.Driver()}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: d})
	defer fyne.SetCurrentApp(base)
	var previous float32
	for i, name := range []string{"Hover sprint 01", "Hover sprint 11", "Hover completed", "No Sprint"} {
		row := tappableWith(k.SideClip, name)
		if row == nil {
			t.Fatalf("missing sidebar row %s", name)
		}
		at := d.AbsolutePositionForObject(row).Add(fyne.NewPos(24, row.Size().Height/2))
		test.MoveMouse(w.Canvas(), at)
		if len(d.anims) == 0 {
			t.Fatal("moving onto a sprint must animate its highlight")
		}
		animation := d.anims[len(d.anims)-1]
		if animation.Duration != 600*time.Millisecond {
			t.Fatal("sidebar running hover should keep the 600ms duration")
		}
		animation.Tick(.5)
		middle := pill.Position().Y
		animation.Tick(1)
		end := pill.Position().Y
		if i > 0 && (math.Abs(float64(middle-(previous+end)/2)) > .1 || previous == end) {
			t.Fatalf("highlight must travel into %s, including nested fold offsets: %g -> %g -> %g", name, previous, middle, end)
		}
		previous = end
	}
	// Clicking still filters the board through the original row action.
	row := tappableWith(k.SideClip, "Hover sprint 01")
	row.Tapped(nil)
	if k.Filter != "sprint" || k.Sprint == "" {
		t.Fatal("sidebar hover must preserve sprint selection")
	}
}
