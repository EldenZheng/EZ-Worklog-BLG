package tests

import (
	"testing"
	"worklog/internal/menus/kanban"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"

	"fyne.io/fyne/v2"
)

func TestSprintHighlightsOnlySidebarHoverOrHeldMoveAndNeverResize(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{k.Week, 540, 90}}}
	kanban.Draw(k)
	var scheduled, sidebar *shell.PlannerDragCard
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*shell.PlannerDragCard); ok {
			if card.Index >= 0 {
				scheduled = card
			} else {
				sidebar = card
			}
		}
	})
	if scheduled == nil || sidebar == nil {
		t.Fatal("calendar bubbles missing")
	}
	reset := func() {
		t.Helper()
		for _, d := range k.Dates {
			if d.Frame.FillColor != d.Base || d.Frame.StrokeColor != d.StrokeColor || d.Frame.StrokeWidth != d.StrokeWidth {
				t.Fatal("unexpected drop highlight on", d.Date)
			}
		}
	}
	highlighted := func() bool {
		for _, d := range k.Dates {
			if d.Frame.FillColor == widgets.WithAlpha(shell.PlannerAvailableBlue, 0.13) || d.Frame.FillColor == widgets.WithAlpha(shell.PlannerAvailableBlue, 0.25) {
				return true
			}
		}
		return false
	}
	scheduled.MouseIn(nil)
	reset()
	scheduled.MouseOut()
	sidebar.MouseIn(nil)
	if !highlighted() {
		t.Fatal("sidebar hover should preview eligible dates")
	}
	sidebar.MouseOut()
	reset()
	var grip *sprintcalendar.PlannerResizeGrip
	walkCanvas(scheduled.Content, func(o fyne.CanvasObject) {
		if g, ok := o.(*sprintcalendar.PlannerResizeGrip); ok {
			grip = g
		}
	})
	if grip == nil {
		t.Fatal("resize edge missing")
	}
	grip.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DY: 15 * grip.Layout.Pixels()}})
	sidebar.MouseIn(nil)
	scheduled.MouseIn(nil)
	reset()
	grip.DragEnd()
	if k.CalendarResizing {
		t.Fatal("resize state remained active after release")
	}
	reset()
	scheduled = sprintcalendar.PlannerCard(k, c, 0).(*shell.PlannerDragCard)
	var target *shell.PlannerDateTarget
	for _, d := range k.Dates {
		if d.Date == backend.AddDays(k.Week, 1) {
			target = d
		}
	}
	if target == nil {
		t.Fatal("destination missing")
	}
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(target.Object).Add(fyne.NewPos(20, 100))
	scheduled.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: at}})
	if !highlighted() || scheduled.Hover != target || k.CalendarDrag != scheduled {
		t.Fatal("held move should highlight eligible dates and its destination")
	}
	sidebar.MouseIn(nil)
	sidebar.MouseOut()
	if !highlighted() {
		t.Fatal("other hover events cleared the held move's highlight")
	}
	scheduled.DragEnd()
	if k.CalendarDrag != nil || k.Plan.Tasks[c.Key].Blocks[0].Date != target.Date {
		t.Fatal("move was not committed or held state remained")
	}
	reset()
}
