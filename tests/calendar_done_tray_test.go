package tests

import (
	"testing"

	"fyne.io/fyne/v2"

	"worklog/internal/menus/kanban"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// A closed issue counts as finished whatever its Status, and the tray keeps
// finished issues folded in a Done section at its foot.
func TestCalendarTrayFoldsClosedAndDoneIssues(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	k := ui.Kanban
	b := k.Plan.Boards[0]
	b.Items = append(b.Items,
		backend.SprintItem{ID: "closed", Title: "Closed one", SprintID: "s", Status: "In Progress", Closed: true},
		backend.SprintItem{ID: "done", Title: "Done one", SprintID: "s", Status: "Done"})
	k.Plan.Boards[0] = b
	kanban.Draw(k)
	w.Content().Refresh()
	if open := k.OpenCalendarItems(); len(open) != 1 || open[0].Item.ID != "i" {
		t.Fatalf("open tray items: %+v", open)
	}
	if done := k.DoneCalendarItems(); len(done) != 2 {
		t.Fatalf("done tray items: %+v", done)
	}
	// The tray's cards (index -1), by issue ID: the calendar's blocks are
	// cards too. A card in the shut Done section counts as hidden.
	shown := func(id string) bool {
		inShutFold := map[fyne.CanvasObject]bool{}
		walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
			if f, ok := o.(*shell.Fold); ok && !f.Open() {
				walkCanvas(f.Content, func(x fyne.CanvasObject) { inShutFold[x] = true })
			}
		})
		visible := false
		walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
			if card, ok := o.(*shell.PlannerDragCard); ok && card.Index < 0 && card.Candidate.Item.ID == id && card.Visible() && !inShutFold[o] {
				visible = true
			}
		})
		return visible
	}
	var toggle *shell.HoverTag
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if d, ok := o.(*shell.HoverTag); ok && d.Text == "Show Done Issues (2)" {
			toggle = d
		}
	})
	if toggle == nil {
		t.Fatal("Show Done Issues toggle missing")
	}
	if !shown("i") {
		t.Fatal("open issue missing from the tray")
	}
	if shown("closed") || shown("done") {
		t.Fatal("finished issues shown before the Done section is opened")
	}
	toggle.Tapped(nil)
	if !k.CalendarShowDone || toggle.Text != "Hide Done Issues (2)" || !shown("closed") || !shown("done") {
		t.Fatal("opening the Done section did not show finished issues")
	}
}

// A tray card dropped above another is placed before it, whatever project
// either is in, and the order is kept locally.
func TestCalendarTrayReordersLocally(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	k := ui.Kanban
	other := backend.SprintBoard{ID: "q", Title: "Other", Fields: k.Plan.Boards[0].Fields,
		Items: []backend.SprintItem{{ID: "j", Title: "Second", SprintID: "s", Status: "In Progress"}}}
	k.Plan.Boards = append(k.Plan.Boards, other)
	kanban.Draw(k)
	w.Content().Refresh()
	var first, second *shell.PlannerDragCard
	for _, c := range k.TrayCards {
		switch c.Candidate.Item.ID {
		case "i":
			first = c
		case "j":
			second = c
		}
	}
	if first == nil || second == nil {
		t.Fatalf("tray cards missing: %d", len(k.TrayCards))
	}
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(first).Add(fyne.NewPos(10, 2))
	second.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: at}})
	if k.TrayDropLine == nil || !k.TrayDropLine.Visible() {
		t.Fatal("no drop line over the tray")
	}
	second.DragEnd()
	open := k.OpenCalendarItems()
	if len(open) != 2 || open[0].Item.ID != "j" || open[1].Item.ID != "i" {
		t.Fatalf("tray order after the drop: %+v", open)
	}
	saved, err := ui.Store.LoadSprintPlan()
	if err != nil || len(saved.TrayOrder) != 2 || saved.TrayOrder[0] != open[0].Key {
		t.Fatalf("tray order not saved: %v %v", saved.TrayOrder, err)
	}
}
