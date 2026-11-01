package kanban

import (
	"fmt"

	"fyne.io/fyne/v2"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// GitHub writes from the board go through one queue, so changes made back to
// back are all kept and reach GitHub in the order they were made:
//
//   - One write runs at a time. The next starts when it answers, whether it
//     succeeded or failed, so one failure never holds up the rest.
//   - A card is drawn where its queued changes put it, marked "Queued" until
//     its turn and "Saving" while it is sent.
//   - Moving a card again before its last change was sent replaces that
//     change instead of queueing a second one. Moving it back to where GitHub
//     has it cancels the change.
//   - A failed write puts only that card back, and says so. Later writes
//     still go.
//   - A board refresh waits until the queue is empty, then runs.
//   - A redraw while a card is being dragged waits for the drop.
//   - Every request has a time limit (backend.GhTimeout), so a hung
//     connection fails the write rather than freezing the queue.

// enqueue adds a write. It reports false when there is nothing to do: the
// card already is, or is already headed, where it was moved.
func enqueue(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem, kind, value, afterID string, start func(), done func(error)) bool {
	b, it, ok := latestItem(k, b.ID, it.ID)
	if !ok {
		k.SetMessage(fmt.Sprintf("%q is no longer on the board.", it.Title))
		return false
	}
	key := it.Key(b.ID)
	addCallbacks := func(j *shell.KanbanJob) {
		if start != nil {
			j.Start = append(j.Start, start)
		}
		if done != nil {
			j.Done = append(j.Done, done)
		}
	}
	// The card's latest queued write, if it is the same kind, absorbs this
	// one: only the newest value matters, and order is kept because nothing
	// for this card comes after it.
	last := -1
	for i, j := range k.Queue {
		if j.Key() == key {
			last = i
		}
	}
	if last >= 0 && k.Queue[last].Kind == kind {
		j := k.Queue[last]
		before := expectedState(k, key, j)
		j.Value = value
		if kind == "place" {
			j.AfterID = afterID
			applyReorder(k, b.ID, it.ID, afterID)
		}
		addCallbacks(j)
		if kind != "place" && before.value(kind) == value {
			// Moved back to where it will already be: drop the write.
			k.Queue = append(k.Queue[:last], k.Queue[last+1:]...)
			refreshPending(k)
			k.SetMessage(fmt.Sprintf("Cancelled the queued change to %q.", it.Title))
			redraw(k)
			for _, d := range j.Done {
				d(nil)
			}
			return true
		}
		refreshPending(k)
		k.SetMessage(queuedMessage(k, it))
		redraw(k)
		return true
	}
	if kind != "place" && expectedState(k, key, nil).value(kind) == value {
		return false
	}
	j := &shell.KanbanJob{Board: b, Item: it, Kind: kind, Value: value, AfterID: afterID}
	if kind == "place" {
		j.PrevAfter = itemBefore(k, b.ID, it.ID)
		applyReorder(k, b.ID, it.ID, afterID)
	}
	addCallbacks(j)
	k.Queue = append(k.Queue, j)
	refreshPending(k)
	if k.Running != nil || k.Loading {
		k.SetMessage(queuedMessage(k, it))
	}
	redraw(k)
	Kick(k)
	return true
}

// Kick starts the next queued write if nothing else is talking to GitHub.
// When the queue is empty it runs a refresh that was asked for meanwhile.
func Kick(k *shell.KanbanUI) {
	if k.Running != nil || k.Busy || k.Loading {
		return
	}
	if len(k.Queue) == 0 {
		if k.ReloadAfterQueue {
			k.ReloadAfterQueue = false
			k.Load(true)
		}
		return
	}
	j := k.Queue[0]
	k.Queue = k.Queue[1:]
	b, it, ok := latestItem(k, j.Board.ID, j.Item.ID)
	if !ok {
		// The card left the board (a source was removed) before its turn.
		k.Running = j
		finishJob(k, j, false, fmt.Errorf("%q is no longer on the board", j.Item.Title), nil)
		return
	}
	j.Board, j.Item = b, it
	statusChange := j.Kind == "place" && it.Status != j.Value
	k.Running = j
	k.Busy = true
	refreshPending(k)
	k.SetMessage(savingMessage(k, j, statusChange))
	redraw(k)
	for _, s := range j.Start {
		s()
	}
	go func() {
		var err, posErr error
		switch j.Kind {
		case "status", "sprint":
			err = backend.PushSprintItem(b, it, j.Kind, j.Value)
		case "place":
			if statusChange {
				err = backend.PushSprintItem(b, it, "status", j.Value)
			}
			if err == nil {
				posErr = PushItemPosition(b, it, j.AfterID)
			}
		}
		fyne.Do(func() { finishJob(k, j, statusChange, err, posErr) })
	}()
}

// finishJob records a write's outcome, then starts the next one. err is the
// status or sprint write; posErr the position write of a drop.
func finishJob(k *shell.KanbanUI, j *shell.KanbanJob, statusChange bool, err, posErr error) {
	k.Running = nil
	k.Busy = false
	saved := false
	for bi := range k.Plan.Boards {
		board := &k.Plan.Boards[bi]
		if board.ID != j.Board.ID {
			continue
		}
		for ii := range board.Items {
			i := &board.Items[ii]
			if i.ID != j.Item.ID || err != nil {
				continue
			}
			switch {
			case j.Kind == "sprint":
				i.SprintID = j.Value
				saved = true
			case j.Kind == "status" || statusChange:
				i.Status = j.Value
				saved = true
			}
		}
		if j.Kind == "place" {
			if err != nil || posErr != nil {
				// Back to where GitHub still has it; other cards keep their
				// queued places.
				board.Items = ReorderItems(board.Items, j.Item.ID, j.PrevAfter)
			} else {
				// Again, in case a refresh replaced the order meanwhile.
				board.Items = ReorderItems(board.Items, j.Item.ID, j.AfterID)
				saved = true
			}
		}
	}
	if saved {
		k.Save(k.Plan)
	}
	p := shell.KanbanPending{Kind: j.Kind, Value: j.Value, Label: backend.FieldValueLabel(j.Board, kindForLabel(j.Kind), j.Value)}
	if j.Kind == "place" {
		p.Kind = "status"
	}
	var msg string
	switch {
	case err != nil:
		msg = moveResult(j.Item, p, err)
	case posErr != nil && statusChange:
		msg = fmt.Sprintf("Saved %q: status is now %s, but the new order failed: %v", j.Item.Title, p.Label, posErr)
	case posErr != nil:
		msg = fmt.Sprintf("Could not reorder %q on GitHub: %v", j.Item.Title, posErr)
	case j.Kind == "place" && !statusChange:
		msg = fmt.Sprintf("Saved %q: new order is on GitHub.", j.Item.Title)
	default:
		msg = moveResult(j.Item, p, nil)
	}
	if n := len(k.Queue); n > 0 {
		msg += fmt.Sprintf(" %d more change%s queued.", n, plural(n))
	}
	k.SetMessage(msg)
	refreshPending(k)
	redraw(k)
	if e := firstErr(err, posErr); e != nil {
		k.Ui.Errf(e)
	}
	Kick(k)
	for _, d := range j.Done {
		d(firstErr(err, posErr))
	}
}

func kindForLabel(kind string) string {
	if kind == "place" {
		return "status"
	}
	return kind
}

// redraw draws the board now, or after the drag in progress ends: a redraw
// mid-drag would drop the column the card is hovering over.
func redraw(k *shell.KanbanUI) {
	if k.Dragging || k.CalendarDrag != nil {
		k.RedrawPending = true
		return
	}
	Draw(k)
}

type cardState struct{ status, sprint string }

func (s cardState) value(kind string) string {
	if kind == "sprint" {
		return s.sprint
	}
	return s.status
}

// expectedState is where a card will be once the write in flight and every
// queued write for it land, leaving out skip.
func expectedState(k *shell.KanbanUI, key string, skip *shell.KanbanJob) cardState {
	var s cardState
	for _, b := range k.Plan.Boards {
		for _, it := range b.Items {
			if it.Key(b.ID) == key {
				s = cardState{it.Status, it.SprintID}
			}
		}
	}
	for _, j := range pendingJobs(k) {
		if j == skip || j.Key() != key {
			continue
		}
		if j.Kind == "sprint" {
			s.sprint = j.Value
		} else {
			s.status = j.Value
		}
	}
	return s
}

func pendingJobs(k *shell.KanbanUI) []*shell.KanbanJob {
	jobs := make([]*shell.KanbanJob, 0, len(k.Queue)+1)
	if k.Running != nil {
		jobs = append(jobs, k.Running)
	}
	return append(jobs, k.Queue...)
}

// refreshPending rebuilds what each card shows from the queue.
func refreshPending(k *shell.KanbanUI) {
	k.Pending = map[string]shell.KanbanPending{}
	confirmed := map[string]string{}
	for _, b := range k.Plan.Boards {
		for _, it := range b.Items {
			confirmed[it.Key(b.ID)] = it.Status
		}
	}
	status := map[string]string{}
	for _, j := range pendingJobs(k) {
		key := j.Key()
		before, seen := status[key]
		if !seen {
			before = confirmed[key]
		}
		p := k.Pending[key]
		p.Value = j.Value
		switch {
		case j.Kind == "sprint":
			p.Kind = "sprint"
			p.Label = backend.FieldValueLabel(j.Board, "sprint", j.Value)
		case j.Kind == "place" && before == j.Value:
			p.Kind = "position"
			p.Label = ""
		default:
			p.Kind = "status"
			p.Label = backend.FieldValueLabel(j.Board, "status", j.Value)
		}
		if j.Kind != "sprint" {
			p.Status, p.HasStatus = j.Value, true
			status[key] = j.Value
		}
		p.Queued = k.Running == nil || k.Running.Key() != key
		k.Pending[key] = p
	}
}

func latestItem(k *shell.KanbanUI, boardID, itemID string) (backend.SprintBoard, backend.SprintItem, bool) {
	for _, b := range k.Plan.Boards {
		if b.ID != boardID {
			continue
		}
		for _, it := range b.Items {
			if it.ID == itemID {
				return b, it, true
			}
		}
	}
	return backend.SprintBoard{}, backend.SprintItem{ID: itemID}, false
}

func itemBefore(k *shell.KanbanUI, boardID, itemID string) string {
	for _, b := range k.Plan.Boards {
		if b.ID != boardID {
			continue
		}
		prev := ""
		for _, it := range b.Items {
			if it.ID == itemID {
				return prev
			}
			prev = it.ID
		}
	}
	return ""
}

func applyReorder(k *shell.KanbanUI, boardID, itemID, afterID string) {
	for i := range k.Plan.Boards {
		if k.Plan.Boards[i].ID == boardID {
			k.Plan.Boards[i].Items = ReorderItems(k.Plan.Boards[i].Items, itemID, afterID)
		}
	}
}

func queuedMessage(k *shell.KanbanUI, it backend.SprintItem) string {
	return fmt.Sprintf("Queued the change to %q; %d change%s waiting for GitHub.", it.Title, k.PendingWrites(), plural(k.PendingWrites()))
}

func savingMessage(k *shell.KanbanUI, j *shell.KanbanJob, statusChange bool) string {
	var what string
	switch {
	case j.Kind == "sprint":
		what = "sprint " + backend.FieldValueLabel(j.Board, "sprint", j.Value)
	case j.Kind == "status":
		what = "status " + backend.FieldValueLabel(j.Board, "status", j.Value)
	case statusChange:
		what = "status " + backend.FieldValueLabel(j.Board, "status", j.Value) + " and order"
	default:
		what = "order"
	}
	msg := fmt.Sprintf("Saving %q to GitHub: %s…", j.Item.Title, what)
	if n := len(k.Queue); n > 0 {
		msg += fmt.Sprintf(" %d more queued.", n)
	}
	return msg
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
