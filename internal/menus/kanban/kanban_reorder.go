package kanban

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// ---- reordering cards, as on a GitHub project board ----
//
// GitHub keeps one order per project, not one per column: a card is placed
// after another item of the same project (or at the very top). A column can
// hold cards of two projects, merged position by position (see ColumnOrder); a
// card can only be placed among its own project's cards, so the drop line only
// ever appears next to those.

// updateItemPosition moves an item to just after afterID in its project, or
// to the top when afterID is empty.
func updateItemPosition(b backend.SprintBoard, it backend.SprintItem, afterID string) error {
	_, err := backend.Gh(ItemPositionMutation(b, it, afterID))
	return err
}

// PushItemPosition is a variable so tests can stand in for the gh CLI.
var PushItemPosition = updateItemPosition

func ItemPositionMutation(b backend.SprintBoard, it backend.SprintItem, afterID string) []string {
	args := []string{"api", "graphql",
		"-f", "query=mutation($project:ID!,$item:ID!,$after:ID){updateProjectV2ItemPosition(input:{projectId:$project,itemId:$item,afterId:$after}){clientMutationId}}",
		"-f", "project=" + b.ID, "-f", "item=" + it.ID}
	if afterID != "" {
		args = append(args, "-f", "after="+afterID)
	}
	return args
}

// ReorderItems returns items with id moved to just after afterID, or to the
// front when afterID is empty. The input slice is left untouched.
func ReorderItems(items []backend.SprintItem, id, afterID string) []backend.SprintItem {
	var moved *backend.SprintItem
	out := make([]backend.SprintItem, 0, len(items))
	for i := range items {
		if items[i].ID == id {
			moved = &items[i]
			continue
		}
		out = append(out, items[i])
	}
	if moved == nil {
		return append([]backend.SprintItem{}, items...)
	}
	at := 0
	if afterID != "" {
		at = len(out)
		for i, x := range out {
			if x.ID == afterID {
				at = i + 1
				break
			}
		}
	}
	out = append(out, backend.SprintItem{})
	copy(out[at+1:], out[at:])
	out[at] = *moved
	return out
}

// SlotCard and DropSlotFor live in shell, for the Sprint Calendar's tray too.
type SlotCard = shell.SlotCard

var DropSlotFor = shell.DropSlotFor

// ShowDropSlot works out the slot under the pointer in the hovered status
// column and draws the gold line there.
func ShowDropSlot(k *shell.KanbanUI, c *shell.SprintDragCard, t *shell.KanbanTarget, at fyne.Position) {
	k.HoverSlot = shell.KanbanSlot{}
	if k.DropLine == nil {
		return
	}
	if t == nil || t.Kind != "status" {
		k.DropLine.Hide()
		return
	}
	driver := fyne.CurrentApp().Driver()
	var cards []SlotCard
	var left, width float32
	for _, d := range k.ColumnCards[t.Value] {
		pos := driver.AbsolutePositionForObject(d)
		cards = append(cards, SlotCard{d.Board.ID, d.Item.ID, pos.Y, pos.Y + d.Size().Height})
		if d.Board.ID == c.Board.ID {
			left, width = pos.X, d.Size().Width
		}
	}
	k.HoverSlot = DropSlotFor(cards, c.Board.ID, c.Item.ID, at.Y, theme.Padding())
	if !k.HoverSlot.Ok || k.HoverSlot.Unchanged() && t.Value == c.Item.Status {
		k.DropLine.Hide()
		return
	}
	origin := driver.AbsolutePositionForObject(k.GhostLayer)
	k.DropLine.Move(fyne.NewPos(left-origin.X, k.HoverSlot.LineY-origin.Y-1.5))
	k.DropLine.Resize(fyne.NewSize(width, 3))
	k.DropLine.Show()
}

// Place drops a card into a status column at a slot: the status changes if
// the column differs, then the card is positioned after its new neighbour.
// The card is drawn in its new place at once, marked as saving; a failure
// puts it back.
func Place(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem, status string, slot shell.KanbanSlot) {
	PlaceThen(k, b, it, status, slot, nil)
}

// PlaceThen is place with done, if given, called on the UI thread last.
func PlaceThen(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem, status string, slot shell.KanbanSlot, done func(error)) {
	if !slot.Ok {
		MoveThen(k, b, it, "status", status, nil, done)
		return
	}
	b, latest, ok := latestItem(k, b.ID, it.ID)
	if !ok {
		return
	}
	if expectedState(k, latest.Key(b.ID), nil).status == status && slot.Unchanged() {
		return
	}
	enqueue(k, b, latest, "place", status, slot.AfterID, nil, done)
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
