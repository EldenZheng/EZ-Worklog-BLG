package kanban

import (
	"fmt"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// ---- reordering cards, as on a GitHub project board ----
//
// GitHub keeps one order per project, not one per column: a card is placed
// after another item of the same project (or at the very top). A column can
// hold cards of two projects; a card can only be placed among its own
// project's cards, so the drop line only ever appears between those.

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

// SlotCard is a drawn card's place in a column, in absolute coordinates.
type SlotCard struct {
	BoardID, ItemID string
	Top, Bottom     float32
}

// DropSlotFor finds the gap nearest y among the column's cards of boardID.
func DropSlotFor(cards []SlotCard, boardID, dragID string, y, gap float32) shell.KanbanSlot {
	var s shell.KanbanSlot
	prev := ""
	var same []SlotCard
	for _, c := range cards {
		if c.BoardID != boardID {
			continue
		}
		if c.ItemID == dragID {
			s.InColumn, s.CurrentAfter = true, prev
			continue
		}
		prev = c.ItemID
		same = append(same, c)
	}
	if len(same) == 0 {
		return s
	}
	s.Ok = true
	for _, c := range same {
		if y < (c.Top+c.Bottom)/2 {
			s.LineY = c.Top - gap/2
			return s
		}
		s.AfterID = c.ItemID
	}
	s.LineY = same[len(same)-1].Bottom + gap/2
	return s
}

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
	if k.Busy || k.Loading {
		k.SetMessage("Wait for the GitHub request to finish before moving an issue.")
		return
	}
	bi := -1
	for i := range k.Plan.Boards {
		if k.Plan.Boards[i].ID == b.ID {
			bi = i
			for _, latest := range k.Plan.Boards[i].Items {
				if latest.ID == it.ID {
					it = latest
				}
			}
		}
	}
	if bi < 0 {
		return
	}
	statusChange := it.Status != status
	if !statusChange && slot.Unchanged() {
		return
	}
	k.Busy = true
	before := k.Plan.Boards[bi].Items
	k.Plan.Boards[bi].Items = ReorderItems(before, it.ID, slot.AfterID)
	if statusChange {
		k.SetPending(b, it, "status", status)
	} else {
		k.SetPending(b, it, "position", "")
	}
	p := k.Pending[it.Key(b.ID)]
	k.SetMessage(fmt.Sprintf("Saving %q to GitHub: %s…", it.Title, placeWhat(p)))
	Draw(k)
	go func() {
		var statusErr, posErr error
		if statusChange {
			statusErr = backend.PushSprintItem(b, it, "status", status)
		}
		if statusErr == nil {
			posErr = PushItemPosition(b, it, slot.AfterID)
		}
		fyne.Do(func() {
			k.Busy = false
			k.ClearPending(b, it)
			if done != nil {
				defer func() { done(firstErr(statusErr, posErr)) }()
			}
			for i := range k.Plan.Boards {
				if k.Plan.Boards[i].ID != b.ID {
					continue
				}
				if statusErr != nil || posErr != nil {
					// Back to the order GitHub still has.
					k.Plan.Boards[i].Items = before
				}
				if statusErr == nil && statusChange {
					for ii := range k.Plan.Boards[i].Items {
						if k.Plan.Boards[i].Items[ii].ID == it.ID {
							k.Plan.Boards[i].Items[ii].Status = status
						}
					}
				}
			}
			switch {
			case statusErr != nil:
				k.SetMessage(moveResult(it, p, statusErr))
			case posErr != nil:
				msg := fmt.Sprintf("Could not reorder %q on GitHub: %v", it.Title, posErr)
				if statusChange {
					k.Save(k.Plan)
					msg = fmt.Sprintf("Saved %q: status is now %s, but the new order failed: %v", it.Title, p.Label, posErr)
				}
				k.SetMessage(msg)
			default:
				k.Save(k.Plan)
				if statusChange {
					k.SetMessage(moveResult(it, p, nil))
				} else {
					k.SetMessage(fmt.Sprintf("Saved %q: new order is on GitHub.", it.Title))
				}
			}
			Draw(k)
			if err := firstErr(statusErr, posErr); err != nil {
				k.Ui.Errf(err)
			}
		})
	}()
}

func placeWhat(p shell.KanbanPending) string {
	if p.Kind == "status" {
		return "status " + p.Label + " and order"
	}
	return "order"
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
