package main

import (
	"testing"

	"fyne.io/fyne/v2"
)

func TestSprintHighlightsOnlySidebarHoverOrHeldMoveAndNeverResize(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	k.plan.Tasks[c.Key] = sprintTask{Blocks: []planBlock{{k.week, 540, 90}}}
	k.draw()
	var scheduled, sidebar *plannerDragCard
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*plannerDragCard); ok {
			if card.index >= 0 {
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
		for _, d := range k.dates {
			if d.frame.FillColor != d.base || d.frame.StrokeColor != d.strokeColor || d.frame.StrokeWidth != d.strokeWidth {
				t.Fatal("unexpected drop highlight on", d.date)
			}
		}
	}
	highlighted := func() bool {
		for _, d := range k.dates {
			if d.frame.FillColor == withAlpha(plannerAvailableBlue, 0.13) || d.frame.FillColor == withAlpha(plannerAvailableBlue, 0.25) {
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
	var grip *plannerResizeGrip
	walkCanvas(scheduled.content, func(o fyne.CanvasObject) {
		if g, ok := o.(*plannerResizeGrip); ok {
			grip = g
		}
	})
	if grip == nil {
		t.Fatal("resize edge missing")
	}
	grip.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DY: 15 * grip.layout.pixels()}})
	sidebar.MouseIn(nil)
	scheduled.MouseIn(nil)
	reset()
	grip.DragEnd()
	if k.calendarResizing {
		t.Fatal("resize state remained active after release")
	}
	reset()
	scheduled = k.plannerCard(c, 0).(*plannerDragCard)
	var target *plannerDateTarget
	for _, d := range k.dates {
		if d.date == addDays(k.week, 1) {
			target = d
		}
	}
	if target == nil {
		t.Fatal("destination missing")
	}
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(target.object).Add(fyne.NewPos(20, 100))
	scheduled.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: at}})
	if !highlighted() || scheduled.hover != target || k.calendarDrag != scheduled {
		t.Fatal("held move should highlight eligible dates and its destination")
	}
	sidebar.MouseIn(nil)
	sidebar.MouseOut()
	if !highlighted() {
		t.Fatal("other hover events cleared the held move's highlight")
	}
	scheduled.DragEnd()
	if k.calendarDrag != nil || k.plan.Tasks[c.Key].Blocks[0].Date != target.date {
		t.Fatal("move was not committed or held state remained")
	}
	reset()
}
