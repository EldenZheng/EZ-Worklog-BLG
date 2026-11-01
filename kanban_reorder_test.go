package main

import (
	"errors"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
)

func itemIDs(items []sprintItem) string {
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return strings.Join(ids, ",")
}

func TestReorderItemsMovesAfterOrToTop(t *testing.T) {
	items := []sprintItem{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	for _, tc := range []struct{ id, after, want string }{
		{"d", "a", "a,d,b,c"},
		{"a", "c", "b,c,a,d"},
		{"c", "", "c,a,b,d"},
		{"b", "missing", "a,c,d,b"},
		{"missing", "a", "a,b,c,d"},
	} {
		if got := itemIDs(reorderItems(items, tc.id, tc.after)); got != tc.want {
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
	cards := []slotCard{
		{"p", "a", 0, 40}, {"q", "x", 44, 84}, {"p", "b", 88, 128}, {"p", "c", 132, 172},
	}
	s := dropSlotFor(cards, "p", "b", 10, 4)
	if !s.ok || s.afterID != "" || !s.inColumn || s.currentAfter != "a" || s.unchanged() {
		t.Fatal("top slot", s)
	}
	s = dropSlotFor(cards, "p", "b", 100, 4)
	if s.afterID != "a" || !s.unchanged() {
		t.Fatal("own gap should be unchanged", s)
	}
	s = dropSlotFor(cards, "p", "b", 160, 4)
	if s.afterID != "c" || s.lineY != 174 {
		t.Fatal("bottom slot", s)
	}
	if s = dropSlotFor(cards, "r", "z", 10, 4); s.ok {
		t.Fatal("no cards of the project: no slot", s)
	}
}

func TestItemPositionMutationOmitsAfterForTop(t *testing.T) {
	b := sprintBoard{ID: "P"}
	top := strings.Join(itemPositionMutation(b, sprintItem{ID: "I"}, ""), " ")
	after := strings.Join(itemPositionMutation(b, sprintItem{ID: "I"}, "A"), " ")
	if !strings.Contains(top, "updateProjectV2ItemPosition") || strings.Contains(top, "after=") || !strings.Contains(after, "after=A") || !strings.Contains(after, "project=P") {
		t.Fatal(top, after)
	}
}

func reorderFixture(t *testing.T) *kanbanUI {
	k := kanbanSavingFixture(t)
	b := &k.plan.Boards[0]
	b.Items = []sprintItem{
		{ID: "a", Title: "A", Status: "Created", SprintID: "s"},
		{ID: "b", Title: "B", Status: "Created", SprintID: "s"},
		{ID: "c", Title: "C", Status: "In Progress", SprintID: "s"},
	}
	k.draw()
	return k
}

func stubPosition(t *testing.T, result error) *[]string {
	t.Helper()
	var calls []string
	old := pushItemPosition
	pushItemPosition = func(_ sprintBoard, it sprintItem, after string) error {
		calls = append(calls, it.ID+">"+after)
		return result
	}
	t.Cleanup(func() { pushItemPosition = old })
	return &calls
}

// Dropping into another column between cards changes the status and the order.
func TestPlaceChangesStatusThenPosition(t *testing.T) {
	k := reorderFixture(t)
	release := stubPush(t, nil)
	calls := stubPosition(t, nil)
	b := k.plan.Boards[0]
	finished := make(chan struct{})
	k.placeThen(b, b.Items[0], "In Progress", kanbanSlot{ok: true, afterID: "c"}, func(error) { close(finished) })
	if itemIDs(k.plan.Boards[0].Items) != "b,c,a" || k.pendingStatus(b, b.Items[0]) != "In Progress" {
		t.Fatal("card not shown in its new place while saving", itemIDs(k.plan.Boards[0].Items))
	}
	release()
	<-finished
	if len(*calls) != 1 || (*calls)[0] != "a>c" || k.plan.Boards[0].Items[2].Status != "In Progress" {
		t.Fatal("status/position not saved", *calls, k.plan.Boards[0].Items)
	}
}

// A failed reorder puts the card back in GitHub's order.
func TestPlaceFailureRestoresOrder(t *testing.T) {
	k := reorderFixture(t)
	release := stubPush(t, nil)
	stubPosition(t, errors.New("boom"))
	b := k.plan.Boards[0]
	finished := make(chan struct{})
	k.placeThen(b, b.Items[1], "Created", kanbanSlot{ok: true, afterID: "", inColumn: true, currentAfter: "a"}, func(error) { close(finished) })
	release()
	<-finished
	if itemIDs(k.plan.Boards[0].Items) != "a,b,c" || !strings.Contains(k.message.Text, "Could not reorder") {
		t.Fatal("order not restored", itemIDs(k.plan.Boards[0].Items), k.message.Text)
	}
}

// Dropped back into its own gap: nothing is sent.
func TestPlaceSameGapDoesNothing(t *testing.T) {
	k := reorderFixture(t)
	calls := stubPosition(t, nil)
	b := k.plan.Boards[0]
	k.place(b, b.Items[1], "Created", kanbanSlot{ok: true, afterID: "a", inColumn: true, currentAfter: "a"})
	if k.busy || len(*calls) != 0 {
		t.Fatal("no-op drop sent a request")
	}
}

// Dragging a card over the top half of another card of its project shows the
// gold line above it and aims the drop there.
func TestDraggingOverCardShowsDropLine(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	k := ui.kanban
	b := &k.plan.Boards[0]
	b.Fields = append(b.Fields, sprintField{Name: "Status", Options: []fieldOption{{Name: "Created"}, {Name: "In Progress"}}})
	b.Items = []sprintItem{{ID: "a", Title: "A", Status: "Created", SprintID: "s"}, {ID: "b", Title: "B", Status: "Created", SprintID: "s"}}
	k.filter = "all"
	w.SetContent(k.root)
	w.Resize(fyne.NewSize(1400, 950))
	k.draw()
	w.Content().Refresh()
	cards := k.columnCards["Created"]
	if len(cards) != 2 {
		t.Fatal("cards not recorded", len(cards))
	}
	top := fyne.CurrentApp().Driver().AbsolutePositionForObject(cards[0])
	cards[1].Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: top.Add(fyne.NewPos(20, 4))}})
	if k.hover == nil || k.hover.value != "Created" || !k.hoverSlot.ok || k.hoverSlot.afterID != "" || k.hoverSlot.currentAfter != "a" {
		t.Fatal("slot not found", k.hoverSlot)
	}
	if k.dropLine == nil || !k.dropLine.Visible() || k.dropLine.FillColor != logoGold {
		t.Fatal("drop line not shown")
	}
	// Over its own gap the line goes away: nothing would change.
	bottom := fyne.CurrentApp().Driver().AbsolutePositionForObject(cards[1])
	cards[1].Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: bottom.Add(fyne.NewPos(20, cards[1].Size().Height-4))}})
	if k.dropLine.Visible() {
		t.Fatal("line shown for a drop that changes nothing")
	}
}
