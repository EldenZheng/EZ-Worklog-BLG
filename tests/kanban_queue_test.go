package tests

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"worklog/internal/menus/kanban"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// queueFixture is a board with three cards in Created and a status field
// with three columns.
func queueFixture(t *testing.T) *shell.KanbanUI {
	k := kanbanSavingFixture(t)
	b := &k.Plan.Boards[0]
	b.Fields = append(b.Fields[:0:0], b.Fields...)
	for i := range b.Fields {
		if b.Fields[i].Name == "Status" {
			b.Fields[i].Options = append(b.Fields[i].Options, backend.FieldOption{Name: "Done"})
		}
	}
	b.Items = append(b.Items,
		backend.SprintItem{ID: "j", Title: "Second", Status: "Created", SprintID: "s"},
		backend.SprintItem{ID: "k", Title: "Third", Status: "Created", SprintID: "s"})
	kanban.Draw(k)
	return k
}

// stubQueuedPush records each status/sprint write and holds it until the test
// answers it, one answer per write, in order.
func stubQueuedPush(t *testing.T) (calls func() []string, answer func(error)) {
	t.Helper()
	answers := make(chan error)
	var mu sync.Mutex
	var got []string
	old := backend.PushSprintItem
	backend.PushSprintItem = func(_ backend.SprintBoard, it backend.SprintItem, kind, value string) error {
		mu.Lock()
		got = append(got, it.ID+":"+kind+"="+value)
		mu.Unlock()
		return <-answers
	}
	t.Cleanup(func() { backend.PushSprintItem = old })
	calls = func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string{}, got...)
	}
	answer = func(err error) {
		select {
		case answers <- err:
		case <-time.After(5 * time.Second):
			t.Fatal("no write was waiting for an answer")
		}
	}
	return calls, answer
}

func cardStatus(k *shell.KanbanUI, id string) string {
	for _, it := range k.Plan.Boards[0].Items {
		if it.ID == id {
			return it.Status
		}
	}
	return ""
}

func wait(t *testing.T, ch chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("change never finished")
		return nil
	}
}

func doneChan() (chan error, func(error)) {
	ch := make(chan error, 1)
	return ch, func(err error) { ch <- err }
}

// Two cards moved back to back: the second waits its turn instead of being
// refused, shows as queued, and both reach GitHub in order.
func TestKanbanBackToBackMovesAreQueuedInOrder(t *testing.T) {
	k := queueFixture(t)
	calls, answer := stubQueuedPush(t)
	b := k.Plan.Boards[0]
	first, firstDone := doneChan()
	second, secondDone := doneChan()
	if !kanban.MoveThen(k, b, b.Items[0], "status", "In Progress", nil, firstDone) {
		t.Fatal("first move refused")
	}
	if !kanban.MoveThen(k, b, b.Items[1], "status", "Done", nil, secondDone) {
		t.Fatal("second move refused while the first saves")
	}
	if len(k.Queue) != 1 || k.Running == nil {
		t.Fatalf("expected one running and one queued, got running=%v queued=%d", k.Running != nil, len(k.Queue))
	}
	if kanban.PendingStatus(k, b, b.Items[1]) != "Done" || !k.Pending["p/j"].Queued {
		t.Fatal("queued card not drawn where it is headed, marked queued", k.Pending["p/j"])
	}
	if !strings.Contains(strings.Join(columnLabels(k), "|"), "Queued: to Done") {
		t.Fatal("queued card does not say so", columnLabels(k))
	}
	answer(nil)
	if err := wait(t, first); err != nil {
		t.Fatal(err)
	}
	answer(nil)
	if err := wait(t, second); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls(), ","); got != "i:status=In Progress,j:status=Done" {
		t.Fatal("writes out of order:", got)
	}
	if cardStatus(k, "i") != "In Progress" || cardStatus(k, "j") != "Done" || k.Busy || len(k.Queue) != 0 || len(k.Pending) != 0 {
		t.Fatal("queue did not drain cleanly", cardStatus(k, "i"), cardStatus(k, "j"), k.Busy, len(k.Queue), k.Pending)
	}
}

// Moving the same card again before its queued change is sent replaces that
// change: one write, with the newest value.
func TestKanbanRepeatedMovesOfOneCardCoalesce(t *testing.T) {
	k := queueFixture(t)
	calls, answer := stubQueuedPush(t)
	b := k.Plan.Boards[0]
	first, firstDone := doneChan()
	last, lastDone := doneChan()
	kanban.MoveThen(k, b, b.Items[0], "status", "In Progress", nil, firstDone)
	kanban.MoveThen(k, b, b.Items[1], "status", "In Progress", nil, nil)
	kanban.MoveThen(k, b, b.Items[1], "status", "Done", nil, lastDone)
	if len(k.Queue) != 1 || k.Queue[0].Value != "Done" {
		t.Fatal("second move of the same card should replace the first", len(k.Queue))
	}
	answer(nil)
	wait(t, first)
	answer(nil)
	wait(t, last)
	if got := strings.Join(calls(), ","); got != "i:status=In Progress,j:status=Done" {
		t.Fatal("expected two writes, the latest value for the card:", got)
	}
}

// Moving a queued card back to where GitHub has it cancels the change.
func TestKanbanMovingQueuedCardBackCancelsIt(t *testing.T) {
	k := queueFixture(t)
	calls, answer := stubQueuedPush(t)
	b := k.Plan.Boards[0]
	first, firstDone := doneChan()
	cancelled, cancelledDone := doneChan()
	kanban.MoveThen(k, b, b.Items[0], "status", "In Progress", nil, firstDone)
	kanban.MoveThen(k, b, b.Items[1], "status", "Done", nil, cancelledDone)
	kanban.MoveThen(k, b, b.Items[1], "status", "Created", nil, nil)
	if len(k.Queue) != 0 {
		t.Fatal("moving back should cancel the queued change")
	}
	if err := wait(t, cancelled); err != nil {
		t.Fatal("cancelled change should report done without error", err)
	}
	if _, ok := k.Pending["p/j"]; ok {
		t.Fatal("cancelled card still marked pending")
	}
	answer(nil)
	wait(t, first)
	if got := strings.Join(calls(), ","); got != "i:status=In Progress" {
		t.Fatal("cancelled change was still sent:", got)
	}
}

// A failed write puts only its own card back; the next one still goes.
func TestKanbanFailedWriteDoesNotBlockTheQueue(t *testing.T) {
	k := queueFixture(t)
	_, answer := stubQueuedPush(t)
	b := k.Plan.Boards[0]
	first, firstDone := doneChan()
	second, secondDone := doneChan()
	kanban.MoveThen(k, b, b.Items[0], "status", "In Progress", nil, firstDone)
	kanban.MoveThen(k, b, b.Items[1], "status", "Done", nil, secondDone)
	answer(errors.New("boom"))
	if err := wait(t, first); err == nil {
		t.Fatal("failure not reported to its caller")
	}
	answer(nil)
	if err := wait(t, second); err != nil {
		t.Fatal(err)
	}
	if cardStatus(k, "i") != "Created" || cardStatus(k, "j") != "Done" || k.Busy || len(k.Pending) != 0 {
		t.Fatal("failure should restore only its card", cardStatus(k, "i"), cardStatus(k, "j"))
	}
}

// Dropping into a new place while another card saves is queued too; if the
// drop fails, only that card returns to its old place.
func TestKanbanQueuedDropRestoresOnlyItsOwnPlace(t *testing.T) {
	k := queueFixture(t)
	_, answer := stubQueuedPush(t)
	positions := stubPosition(t, errors.New("no"))
	b := k.Plan.Boards[0]
	first, firstDone := doneChan()
	drop, dropDone := doneChan()
	kanban.MoveThen(k, b, b.Items[1], "status", "In Progress", nil, firstDone)
	// Third card to the top of Created, above the first.
	kanban.PlaceThen(k, b, b.Items[2], "Created", shell.KanbanSlot{Ok: true, AfterID: "", InColumn: true, CurrentAfter: "j"}, dropDone)
	if k.Plan.Boards[0].Items[0].ID != "k" {
		t.Fatal("queued drop not shown in its new place")
	}
	answer(nil)
	wait(t, first)
	if err := wait(t, drop); err == nil {
		t.Fatal("drop failure not reported")
	}
	if len(*positions) != 1 {
		t.Fatal("position write not attempted", *positions)
	}
	var order []string
	for _, it := range k.Plan.Boards[0].Items {
		order = append(order, it.ID)
	}
	if strings.Join(order, ",") != "i,j,k" || cardStatus(k, "j") != "In Progress" {
		t.Fatal("failed drop should put back only its card", order, cardStatus(k, "j"))
	}
}

// A refresh asked for while writes are queued waits for them.
func TestKanbanRefreshWaitsForQueuedWrites(t *testing.T) {
	k := queueFixture(t)
	_, answer := stubQueuedPush(t)
	b := k.Plan.Boards[0]
	first, firstDone := doneChan()
	kanban.MoveThen(k, b, b.Items[0], "status", "In Progress", nil, firstDone)
	k.Load(true)
	if k.Loading || !k.ReloadAfterQueue {
		t.Fatal("refresh should be deferred until the queue drains")
	}
	// The real refresh would reach GitHub; this test only checks the wait.
	k.ReloadAfterQueue = false
	answer(nil)
	wait(t, first)
}

// A save finishing mid-drag holds its redraw until the drop, so the column
// under the pointer is not swapped out.
func TestKanbanRedrawWaitsForDragToEnd(t *testing.T) {
	k := queueFixture(t)
	_, answer := stubQueuedPush(t)
	b := k.Plan.Boards[0]
	first, firstDone := doneChan()
	kanban.MoveThen(k, b, b.Items[0], "status", "In Progress", nil, firstDone)
	k.Dragging = true
	answer(nil)
	wait(t, first)
	if !k.RedrawPending {
		t.Fatal("redraw should wait for the drag")
	}
	k.Dragging = false
	kanban.Draw(k)
	if k.RedrawPending {
		t.Fatal("redraw flag not cleared")
	}
}

// A gh call that runs past the limit fails instead of hanging.
func TestGhGivesUpAfterTimeout(t *testing.T) {
	old := backend.GhTimeout
	backend.GhTimeout = time.Nanosecond
	t.Cleanup(func() { backend.GhTimeout = old })
	_, err := backend.RunGh([]string{"--version"}) // the real runner: TestMain keeps Gh offline
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Fatal("expected a timeout error, got", err)
	}
}
