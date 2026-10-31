package main

import (
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestSprintKanbanColumnsFillRightViewportAndKeepSidebarWidth(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	k := ui.kanban
	k.plan.Boards[0].Fields = append(k.plan.Boards[0].Fields, sprintField{Name: "Status", Options: []fieldOption{{Name: "Created"}, {Name: "In Progress"}}})
	w.SetContent(k.root)
	k.draw()
	sideWidth := float32(0)
	for _, width := range []float32{1400, 1800} {
		w.Resize(fyne.NewSize(width, 950))
		w.Content().Refresh()
		var cols []*kanbanTarget
		for _, target := range k.targets {
			if target.kind == "status" {
				cols = append(cols, target)
			}
		}
		if len(cols) != 2 {
			t.Fatal("fixture status columns missing")
		}
		last := cols[len(cols)-1].object
		end := fyne.CurrentApp().Driver().AbsolutePositionForObject(last).X + last.Size().Width
		clipEnd := fyne.CurrentApp().Driver().AbsolutePositionForObject(k.boardClip).X + k.boardClip.Size().Width
		if clipEnd-end > 3 || end > clipEnd {
			t.Fatal("status columns do not fill the right viewport", end, clipEnd)
		}
		if width == 1400 {
			sideWidth = k.sideClip.Size().Width
			continue
		}
		// Sidebar sizing comes from the same existing container at either width.
		if k.sideClip.Size().Width != sideWidth {
			t.Fatal("columns stretched the sidebar")
		}
	}
	objects := []fyne.CanvasObject{canvas.NewRectangle(nil), canvas.NewRectangle(nil), canvas.NewRectangle(nil)}
	l := kanbanColumnsLayout{}
	l.Layout(objects, fyne.NewSize(500, 600))
	if objects[0].Size().Width < 260 || l.MinSize(objects).Width <= 500 {
		t.Fatal("narrow boards lost horizontal overflow")
	}
}

func TestSprintCalendarBubbleEdgeHitTestResizesTheBubble(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.kanban
	k.plan.Tasks[c.Key] = sprintTask{Blocks: []planBlock{{k.week, 540, 60}, {k.week, 720, 30}}}
	k.draw()
	w.Content().Refresh()
	var cards []*plannerDragCard
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*plannerDragCard); ok && card.index >= 0 {
			cards = append(cards, card)
		}
	})
	if len(cards) != 2 {
		t.Fatal("scheduled bubbles missing")
	}
	card := cards[0]
	pixels := card.content.(*fyne.Container).Layout.(*plannerDurationLayout).pixels()
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(card)
	if card.Size().Height != 60*pixels {
		t.Fatal("bubble height does not match the ruler")
	}
	oldNextY := fyne.CurrentApp().Driver().AbsolutePositionForObject(cards[1]).Y
	test.Drag(w.Canvas(), pos.Add(fyne.NewPos(card.Size().Width/2, card.Size().Height-2)), 0, 30*pixels)
	if k.plan.Tasks[c.Key].Blocks[0].Minutes != 90 || len(k.plan.Tasks[c.Key].Blocks) != 2 {
		t.Fatal("dragging the bubble's bottom edge did not resize it")
	}
	var resized, next *plannerDragCard
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*plannerDragCard); ok {
			if card.index == 0 {
				resized = card
			}
			if card.index == 1 {
				next = card
			}
		}
	})
	if resized.Size().Height != 90*pixels || fyne.CurrentApp().Driver().AbsolutePositionForObject(next).Y != oldNextY+30*pixels {
		t.Fatal("resized bubble and following block did not reflow against the ruler")
	}
	// The card owns only its face and an overlaid edge, with no added panel/ruler.
	content := resized.content.(*fyne.Container)
	if len(content.Objects) != 2 {
		t.Fatal("extra resize panel remains")
	}
	edge := content.Objects[1]
	if edge.Position().Y+edge.Size().Height != resized.Size().Height || edge.Size().Height > 6 {
		t.Fatal("resize edge extends outside bubble")
	}
}

func TestSprintCalendarClearPreservesOtherSprintsAndMetadata(t *testing.T) {
	s := sprintIteration{StartDate: "2026-11-02", Duration: 14}
	inside := planBlock{s.StartDate, 540, 60}
	outside := planBlock{"2026-12-01", 540, 60}
	original := sprintPlan{Tasks: map[string]sprintTask{
		"mine":   {Estimate: 90, Notes: "keep me", Complexity: "M", Blocks: []planBlock{inside, outside}},
		"hidden": {Blocks: []planBlock{inside}},
	}}
	p, count := clearSprintSchedule(original, []sprintCandidate{{Key: "mine", Sprint: s}})
	if count != 1 || !reflect.DeepEqual(p.Tasks["mine"].Blocks, []planBlock{outside}) {
		t.Fatal("clear did not limit itself to selected sprint")
	}
	if p.Tasks["mine"].Estimate != 90 || p.Tasks["mine"].Notes != "keep me" || p.Tasks["mine"].Complexity != "M" || !reflect.DeepEqual(p.Tasks["hidden"], original.Tasks["hidden"]) || len(original.Tasks["mine"].Blocks) != 2 {
		t.Fatal("clear lost other data or mutated original")
	}
}

func TestSprintCalendarSidebarAndDropColorsWithTopProgressMeters(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	var tray *plannerDragCard
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*plannerDragCard); ok && card.index < 0 {
			tray = card
		}
		if _, ok := o.(*widget.ProgressBar); ok {
			t.Fatal("date progress bar remains")
		}
	})
	if tray == nil {
		t.Fatal("sprint issues sidebar missing")
	}
	trayPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(tray)
	gridPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(k.calendarClip)
	if trayPos.X >= gridPos.X || tray.Size().Width > 300 {
		t.Fatal("issues are not in a left sidebar")
	}
	tray.MouseIn(nil)
	var target *plannerDateTarget
	for _, date := range k.dates {
		if date.date == c.Sprint.StartDate {
			target = date
		}
	}
	if target == nil || target.frame.FillColor != withAlpha(plannerAvailableBlue, 0.13) {
		t.Fatal("eligible date is not translucent light blue")
	}
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(target.object).Add(fyne.NewPos(10, 80))
	tray.highlightDates(&at)
	if target.frame.StrokeColor != logoGold || target.frame.FillColor != withAlpha(plannerAvailableBlue, 0.25) {
		t.Fatal("active target needs gold outline over light blue")
	}
	tray.MouseOut()
	if target.frame.FillColor != target.base {
		t.Fatal("hover wash did not reset")
	}
	foundMeter := false
	for key := range ui.meters {
		if strings.HasPrefix(key, "planner/") {
			foundMeter = true
		}
	}
	if !foundMeter {
		t.Fatal("calendar day progress meters missing")
	}
	k.calendarMode = "Month"
	k.draw()
	scroll := plannerScroll(k.calendarClip)
	scroll.ScrollToOffset(fyne.NewPos(0, 300))
	k.drawKeepingCalendarScroll()
	if plannerScroll(k.calendarClip).Offset.Y != 300 {
		t.Fatal("calendar jumped to the top after a planning change")
	}
}

func TestSprintCalendarClearButtonConfirmsAndSavesLocally(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.kanban
	k.plan.Tasks[c.Key] = sprintTask{Estimate: 90, Notes: "keep", Blocks: []planBlock{{c.Sprint.StartDate, 540, 60}}}
	k.draw()
	var clear *widget.Button
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Clear" {
			clear = b
		}
	})
	if clear == nil {
		t.Fatal("Clear action missing")
	}
	test.Tap(clear)
	if w.Canvas().Overlays().Top() == nil || len(k.plan.Tasks[c.Key].Blocks) != 1 {
		t.Fatal("clear must wait for confirmation")
	}
	var confirm *widget.Button
	walkPopup(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Clear" {
			confirm = b
		}
	})
	if confirm == nil {
		t.Fatal("clear confirmation missing")
	}
	test.Tap(confirm)
	p, err := ui.store.loadSprintPlan()
	if err != nil || len(p.Tasks[c.Key].Blocks) != 0 || p.Tasks[c.Key].Notes != "keep" || p.Boards[0].Items[0].Effort != 90 {
		t.Fatal("clear did not preserve local metadata and GitHub fields", err)
	}
}
