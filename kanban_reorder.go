package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
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
func updateItemPosition(b sprintBoard, it sprintItem, afterID string) error {
	_, err := gh(itemPositionMutation(b, it, afterID))
	return err
}

// pushItemPosition is a variable so tests can stand in for the gh CLI.
var pushItemPosition = updateItemPosition

func itemPositionMutation(b sprintBoard, it sprintItem, afterID string) []string {
	args := []string{"api", "graphql",
		"-f", "query=mutation($project:ID!,$item:ID!,$after:ID){updateProjectV2ItemPosition(input:{projectId:$project,itemId:$item,afterId:$after}){clientMutationId}}",
		"-f", "project=" + b.ID, "-f", "item=" + it.ID}
	if afterID != "" {
		args = append(args, "-f", "after="+afterID)
	}
	return args
}

// reorderItems returns items with id moved to just after afterID, or to the
// front when afterID is empty. The input slice is left untouched.
func reorderItems(items []sprintItem, id, afterID string) []sprintItem {
	var moved *sprintItem
	out := make([]sprintItem, 0, len(items))
	for i := range items {
		if items[i].ID == id {
			moved = &items[i]
			continue
		}
		out = append(out, items[i])
	}
	if moved == nil {
		return append([]sprintItem{}, items...)
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
	out = append(out, sprintItem{})
	copy(out[at+1:], out[at:])
	out[at] = *moved
	return out
}

// slotCard is a drawn card's place in a column, in absolute coordinates.
type slotCard struct {
	boardID, itemID string
	top, bottom     float32
}

// kanbanSlot is where a dragged card would land in a status column.
type kanbanSlot struct {
	ok           bool    // the column holds other cards of the same project
	afterID      string  // place after this item; "" is the top
	currentAfter string  // the card's own predecessor, when it is in this column
	inColumn     bool    // the dragged card is drawn in this column
	lineY        float32 // where to draw the drop line
}

// dropSlotFor finds the gap nearest y among the column's cards of boardID.
func dropSlotFor(cards []slotCard, boardID, dragID string, y, gap float32) kanbanSlot {
	var s kanbanSlot
	prev := ""
	var same []slotCard
	for _, c := range cards {
		if c.boardID != boardID {
			continue
		}
		if c.itemID == dragID {
			s.inColumn, s.currentAfter = true, prev
			continue
		}
		prev = c.itemID
		same = append(same, c)
	}
	if len(same) == 0 {
		return s
	}
	s.ok = true
	for _, c := range same {
		if y < (c.top+c.bottom)/2 {
			s.lineY = c.top - gap/2
			return s
		}
		s.afterID = c.itemID
	}
	s.lineY = same[len(same)-1].bottom + gap/2
	return s
}

// unchanged: dropped back into its own gap in its own column.
func (s kanbanSlot) unchanged() bool { return s.inColumn && s.afterID == s.currentAfter }

// showDropSlot works out the slot under the pointer in the hovered status
// column and draws the gold line there.
func (k *kanbanUI) showDropSlot(c *sprintDragCard, t *kanbanTarget, at fyne.Position) {
	k.hoverSlot = kanbanSlot{}
	if k.dropLine == nil {
		return
	}
	if t == nil || t.kind != "status" {
		k.dropLine.Hide()
		return
	}
	driver := fyne.CurrentApp().Driver()
	var cards []slotCard
	var left, width float32
	for _, d := range k.columnCards[t.value] {
		pos := driver.AbsolutePositionForObject(d)
		cards = append(cards, slotCard{d.board.ID, d.item.ID, pos.Y, pos.Y + d.Size().Height})
		if d.board.ID == c.board.ID {
			left, width = pos.X, d.Size().Width
		}
	}
	k.hoverSlot = dropSlotFor(cards, c.board.ID, c.item.ID, at.Y, theme.Padding())
	if !k.hoverSlot.ok || k.hoverSlot.unchanged() && t.value == c.item.Status {
		k.dropLine.Hide()
		return
	}
	origin := driver.AbsolutePositionForObject(k.ghostLayer)
	k.dropLine.Move(fyne.NewPos(left-origin.X, k.hoverSlot.lineY-origin.Y-1.5))
	k.dropLine.Resize(fyne.NewSize(width, 3))
	k.dropLine.Show()
}

func newDropLine() *canvas.Rectangle {
	line := canvas.NewRectangle(logoGold)
	line.CornerRadius = 1.5
	line.Hide()
	return line
}

// place drops a card into a status column at a slot: the status changes if
// the column differs, then the card is positioned after its new neighbour.
// The card is drawn in its new place at once, marked as saving; a failure
// puts it back.
func (k *kanbanUI) place(b sprintBoard, it sprintItem, status string, slot kanbanSlot) {
	k.placeThen(b, it, status, slot, nil)
}

// placeThen is place with done, if given, called on the UI thread last.
func (k *kanbanUI) placeThen(b sprintBoard, it sprintItem, status string, slot kanbanSlot, done func(error)) {
	if !slot.ok {
		k.moveThen(b, it, "status", status, nil, done)
		return
	}
	if k.busy || k.loading {
		k.setMessage("Wait for the GitHub request to finish before moving an issue.")
		return
	}
	bi := -1
	for i := range k.plan.Boards {
		if k.plan.Boards[i].ID == b.ID {
			bi = i
			for _, latest := range k.plan.Boards[i].Items {
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
	if !statusChange && slot.unchanged() {
		return
	}
	k.busy = true
	before := k.plan.Boards[bi].Items
	k.plan.Boards[bi].Items = reorderItems(before, it.ID, slot.afterID)
	if statusChange {
		k.setPending(b, it, "status", status)
	} else {
		k.setPending(b, it, "position", "")
	}
	p := k.pending[it.key(b.ID)]
	k.setMessage(fmt.Sprintf("Saving %q to GitHub: %s…", it.Title, placeWhat(p)))
	k.draw()
	go func() {
		var statusErr, posErr error
		if statusChange {
			statusErr = pushSprintItem(b, it, "status", status)
		}
		if statusErr == nil {
			posErr = pushItemPosition(b, it, slot.afterID)
		}
		fyne.Do(func() {
			k.busy = false
			k.clearPending(b, it)
			if done != nil {
				defer func() { done(firstErr(statusErr, posErr)) }()
			}
			for i := range k.plan.Boards {
				if k.plan.Boards[i].ID != b.ID {
					continue
				}
				if statusErr != nil || posErr != nil {
					// Back to the order GitHub still has.
					k.plan.Boards[i].Items = before
				}
				if statusErr == nil && statusChange {
					for ii := range k.plan.Boards[i].Items {
						if k.plan.Boards[i].Items[ii].ID == it.ID {
							k.plan.Boards[i].Items[ii].Status = status
						}
					}
				}
			}
			switch {
			case statusErr != nil:
				k.setMessage(moveResult(it, p, statusErr))
			case posErr != nil:
				msg := fmt.Sprintf("Could not reorder %q on GitHub: %v", it.Title, posErr)
				if statusChange {
					k.save(k.plan)
					msg = fmt.Sprintf("Saved %q: status is now %s, but the new order failed: %v", it.Title, p.label, posErr)
				}
				k.setMessage(msg)
			default:
				k.save(k.plan)
				if statusChange {
					k.setMessage(moveResult(it, p, nil))
				} else {
					k.setMessage(fmt.Sprintf("Saved %q: new order is on GitHub.", it.Title))
				}
			}
			k.draw()
			if err := firstErr(statusErr, posErr); err != nil {
				k.ui.errf(err)
			}
		})
	}()
}

func placeWhat(p kanbanPending) string {
	if p.kind == "status" {
		return "status " + p.label + " and order"
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
