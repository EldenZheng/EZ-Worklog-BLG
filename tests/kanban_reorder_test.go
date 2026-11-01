package tests

import (
	"errors"
	"strings"
	"testing"
	"worklog/internal/menus/kanban"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"

	"fyne.io/fyne/v2"
)

func itemIDs(items []backend.SprintItem) string {
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return strings.Join(ids, ",")
}

func TestReorderItemsMovesAfterOrToTop(t *testing.T) {
	items := []backend.SprintItem{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	for _, tc := range []struct{ id, after, want string }{
		{"d", "a", "a,d,b,c"},
		{"a", "c", "b,c,a,d"},
		{"c", "", "c,a,b,d"},
		{"b", "missing", "a,c,d,b"},
		{"missing", "a", "a,b,c,d"},
	} {
		if got := itemIDs(kanban.ReorderItems(items, tc.id, tc.after)); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	if itemIDs(items) != "a,b,c,d" {
		t.Fatal("input changed")
	}
}

// The slot only counts the dragged card's own project, skips the card itself,
// and knows when the drop would leave it where it is.
func TestDropSlotUsesOwnProjectCards(t *testing.T) {
	cards := []kanban.SlotCard{
		{"p", "a", 0, 40}, {"q", "x", 44, 84}, {"p", "b", 88, 128}, {"p", "c", 132, 172},
	}
	s := kanban.DropSlotFor(cards, "p", "b", 10, 4)
	if !s.Ok || s.AfterID != "" || !s.InColumn || s.CurrentAfter != "a" || s.Unchanged() {
		t.Fatal("top slot", s)
	}
	s = kanban.DropSlotFor(cards, "p", "b", 100, 4)
	if s.AfterID != "a" || !s.Unchanged() {
		t.Fatal("own gap should be unchanged", s)
	}
	s = kanban.DropSlotFor(cards, "p", "b", 160, 4)
	if s.AfterID != "c" || s.LineY != 174 {
		t.Fatal("bottom slot", s)
	}
	if s = kanban.DropSlotFor(cards, "r", "z", 10, 4); s.Ok {
		t.Fatal("no cards of the project: no slot", s)
	}
}

func TestItemPositionMutationOmitsAfterForTop(t *testing.T) {
	b := backend.SprintBoard{ID: "P"}
	top := strings.Join(kanban.ItemPositionMutation(b, backend.SprintItem{ID: "I"}, ""), " ")
	after := strings.Join(kanban.ItemPositionMutation(b, backend.SprintItem{ID: "I"}, "A"), " ")
	if !strings.Contains(top, "updateProjectV2ItemPosition") || strings.Contains(top, "after=") || !strings.Contains(after, "after=A") || !strings.Contains(after, "project=P") {
		t.Fatal(top, after)
	}
}

func reorderFixture(t *testing.T) *shell.KanbanUI {
	k := kanbanSavingFixture(t)
	b := &k.Plan.Boards[0]
	b.Items = []backend.SprintItem{
		{ID: "a", Title: "A", Status: "Created", SprintID: "s"},
		{ID: "b", Title: "B", Status: "Created", SprintID: "s"},
		{ID: "c", Title: "C", Status: "In Progress", SprintID: "s"},
	}
	kanban.Draw(k)
	return k
}

func stubPosition(t *testing.T, result error) *[]string {
	t.Helper()
	var calls []string
	old := kanban.PushItemPosition
	kanban.PushItemPosition = func(_ backend.SprintBoard, it backend.SprintItem, after string) error {
		calls = append(calls, it.ID+">"+after)
		return result
	}
	t.Cleanup(func() { kanban.PushItemPosition = old })
	return &calls
}

// Dropping into another column between cards changes the status and the order.
func TestPlaceChangesStatusThenPosition(t *testing.T) {
	k := reorderFixture(t)
	release := stubPush(t, nil)
	calls := stubPosition(t, nil)
	b := k.Plan.Boards[0]
	finished := make(chan struct{})
	kanban.PlaceThen(k, b, b.Items[0], "In Progress", shell.KanbanSlot{Ok: true, AfterID: "c"}, func(error) { close(finished) })
	if itemIDs(k.Plan.Boards[0].Items) != "b,c,a" || kanban.PendingStatus(k, b, b.Items[0]) != "In Progress" {
		t.Fatal("card not shown in its new place while saving", itemIDs(k.Plan.Boards[0].Items))
	}
	release()
	<-finished
	if len(*calls) != 1 || (*calls)[0] != "a>c" || k.Plan.Boards[0].Items[2].Status != "In Progress" {
		t.Fatal("status/position not saved", *calls, k.Plan.Boards[0].Items)
	}
}

// A failed reorder puts the card back in GitHub's order.
func TestPlaceFailureRestoresOrder(t *testing.T) {
	k := reorderFixture(t)
	release := stubPush(t, nil)
	stubPosition(t, errors.New("boom"))
	b := k.Plan.Boards[0]
	finished := make(chan struct{})
	kanban.PlaceThen(k, b, b.Items[1], "Created", shell.KanbanSlot{Ok: true, AfterID: "", InColumn: true, CurrentAfter: "a"}, func(error) { close(finished) })
	release()
	<-finished
	if itemIDs(k.Plan.Boards[0].Items) != "a,b,c" || !strings.Contains(k.Message.Text, "Could not reorder") {
		t.Fatal("order not restored", itemIDs(k.Plan.Boards[0].Items), k.Message.Text)
	}
}

// Dropped back into its own gap: nothing is sent.
func TestPlaceSameGapDoesNothing(t *testing.T) {
	k := reorderFixture(t)
	calls := stubPosition(t, nil)
	b := k.Plan.Boards[0]
	kanban.Place(k, b, b.Items[1], "Created", shell.KanbanSlot{Ok: true, AfterID: "a", InColumn: true, CurrentAfter: "a"})
	if k.Busy || len(*calls) != 0 {
		t.Fatal("no-op drop sent a request")
	}
}

// Dragging a card over the top half of another card of its project shows the
// gold line above it and aims the drop there.
func TestDraggingOverCardShowsDropLine(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	k := ui.Kanban
	b := &k.Plan.Boards[0]
	b.Fields = append(b.Fields, backend.SprintField{Name: "Status", Options: []backend.FieldOption{{Name: "Created"}, {Name: "In Progress"}}})
	b.Items = []backend.SprintItem{{ID: "a", Title: "A", Status: "Created", SprintID: "s"}, {ID: "b", Title: "B", Status: "Created", SprintID: "s"}}
	k.Filter = "all"
	w.SetContent(k.Root)
	w.Resize(fyne.NewSize(1400, 950))
	kanban.Draw(k)
	w.Content().Refresh()
	cards := k.ColumnCards["Created"]
	if len(cards) != 2 {
		t.Fatal("cards not recorded", len(cards))
	}
	top := fyne.CurrentApp().Driver().AbsolutePositionForObject(cards[0])
	cards[1].Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: top.Add(fyne.NewPos(20, 4))}})
	if k.Hover == nil || k.Hover.Value != "Created" || !k.HoverSlot.Ok || k.HoverSlot.AfterID != "" || k.HoverSlot.CurrentAfter != "a" {
		t.Fatal("slot not found", k.HoverSlot)
	}
	if k.DropLine == nil || !k.DropLine.Visible() || k.DropLine.FillColor != widgets.LogoGold {
		t.Fatal("drop line not shown")
	}
	// Over its own gap the line goes away: nothing would change.
	bottom := fyne.CurrentApp().Driver().AbsolutePositionForObject(cards[1])
	cards[1].Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: bottom.Add(fyne.NewPos(20, cards[1].Size().Height-4))}})
	if k.DropLine.Visible() {
		t.Fatal("line shown for a drop that changes nothing")
	}
}
