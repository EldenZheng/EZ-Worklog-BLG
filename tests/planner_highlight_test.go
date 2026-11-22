package tests

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestSprintCalendarHoverMarksMatchingBlocksAndDays(t *testing.T) {
	ui, _, issue := plannerFixture(t)
	k := ui.Kanban
	first, later, other := k.Week, backend.AddDays(k.Week, 1), backend.AddDays(k.Week, 2)
	k.Plan.Boards[0].Items = append(k.Plan.Boards[0].Items, backend.SprintItem{ID: "other", Title: "Unrelated", SprintID: issue.Sprint.ID, Effort: 60})
	k.Plan.Tasks[issue.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: first, Minutes: 90}, {Date: later, Minutes: 480}}}
	k.Plan.Tasks["p/other"] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: other, Minutes: 60}}}
	kanban.Draw(k)
	for _, date := range []string{first, later, other} {
		if len(k.DayCards[date]) != 1 {
			t.Fatalf("expected a block on %s", date)
		}
	}
	marked := func(card *shell.PlannerDragCard) bool {
		var gold bool
		walkRendered(card.Content, func(o fyne.CanvasObject) {
			if r, ok := o.(*canvas.Rectangle); ok && r.StrokeWidth == 2 && sameColor(r.StrokeColor, widgets.LogoGold) {
				gold = true
			}
		})
		return gold
	}
	source, match := k.DayCards[first][0], k.DayCards[later][0]
	source.MouseIn(nil)
	shell.SettleFades()
	if marked(source) || !marked(match) || marked(k.DayCards[other][0]) {
		t.Fatal("only the other block for the hovered issue should be gold")
	}
	for _, date := range k.Dates {
		if date.Date == later {
			if !sameColor(date.Frame.StrokeColor, widgets.LogoGold) || !sameColor(date.Frame.FillColor, date.Base) {
				t.Fatal("the matching date should have a gold outline over its green background")
			}
		} else if !sameColor(date.Frame.StrokeColor, date.StrokeColor) {
			t.Fatalf("hovering a scheduled block changed the outline on %s", date.Date)
		}
	}
	source.MouseOut()
	shell.SettleFades()
	if marked(match) {
		t.Fatal("the block highlight should clear after leaving the source")
	}
	for _, date := range k.Dates {
		if !sameColor(date.Frame.StrokeColor, date.StrokeColor) {
			t.Fatalf("the outline on %s did not return to its resting color", date.Date)
		}
	}
}

func TestSprintHighlightsOnlySidebarHoverOrHeldMoveAndNeverResize(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: k.Week, Start: 540, Minutes: 90}}}
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
		shell.SettleFades() // the outlines fade; look at where they settle
		for _, d := range k.Dates {
			if d.Frame.FillColor != d.Base || d.Frame.StrokeColor != d.StrokeColor || d.Frame.StrokeWidth != d.StrokeWidth {
				t.Fatal("unexpected drop highlight on", d.Date)
			}
		}
	}
	highlighted := func() bool {
		shell.SettleFades() // the outlines fade; look at where they settle
		for _, d := range k.Dates {
			if d.Frame.FillColor == widgets.WithAlpha(widgets.LogoGold, 0.13) || d.Frame.FillColor == widgets.WithAlpha(widgets.LogoGold, 0.25) {
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
