package tests

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/kanban"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// everything visits o and all it draws, through containers and widgets.
func everything(o fyne.CanvasObject, visit func(fyne.CanvasObject)) {
	visit(o)
	switch x := o.(type) {
	case *fyne.Container:
		for _, child := range x.Objects {
			everything(child, visit)
		}
	case fyne.Widget:
		for _, child := range test.WidgetRenderer(x).Objects() {
			everything(child, visit)
		}
	}
}

func longBoardUI(t *testing.T, cards int) *shell.KanbanUI {
	t.Helper()
	a := test.NewApp()
	t.Cleanup(a.Quit)
	// The cards arrive one after another, each started by a timer. Let the
	// last start before the next test lays text out: Fyne's text shaping is
	// not safe to run from two goroutines at once.
	t.Cleanup(func() { time.Sleep(600 * time.Millisecond) })
	w := a.NewWindow("kanban")
	w.Resize(fyne.NewSize(1600, 1000))
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w}
	b := doneHistoryBoard()
	for i := 0; i < cards; i++ {
		b.Items = append(b.Items, backend.SprintItem{ID: fmt.Sprint("I", i), Title: fmt.Sprint("Issue ", i),
			URL: fmt.Sprintf("https://github.com/exampleorg/repo/issues/%d", i), Status: "Created", SprintID: "current"})
	}
	w.SetContent(kanban.BuildKanbanTab(ui))
	k := ui.Kanban
	k.Plan.Boards = []backend.SprintBoard{b}
	kanban.Draw(k)
	return k
}

func boardCards(k *shell.KanbanUI) (cards int, more string) {
	everything(k.Body, func(o fyne.CanvasObject) {
		switch x := o.(type) {
		case *shell.SprintDragCard:
			cards++
		case *shell.HoverTag:
			if strings.HasPrefix(x.Text, "Show ") && x.Text != "Show More…" {
				more = x.Text
			}
		}
	})
	return cards, more
}

// A long column draws its first cards and says how many more there are; the
// rest come on request and fold away again when the selection changes.
func TestLongKanbanColumnShowsItsFirstCards(t *testing.T) {
	k := longBoardUI(t, kanban.ColumnCardsShown+25)
	if cards, more := boardCards(k); cards != kanban.ColumnCardsShown || more != "Show 25 more" {
		t.Fatalf("want %d cards and Show 25 more, got %d and %q", kanban.ColumnCardsShown, cards, more)
	}
	var toggle *shell.HoverTag
	everything(k.Body, func(o fyne.CanvasObject) {
		if x, ok := o.(*shell.HoverTag); ok && x.Text == "Show 25 more" {
			toggle = x
		}
	})
	toggle.Tapped(nil)
	if cards, more := boardCards(k); cards != kanban.ColumnCardsShown+25 || more != "Show less" {
		t.Fatalf("opened, want every card and Show less, got %d and %q", cards, more)
	}
	k.Filter = "all"
	kanban.Draw(k)
	if cards, _ := boardCards(k); cards != kanban.ColumnCardsShown {
		t.Fatalf("a new selection should fold the column back, got %d cards", cards)
	}
}

// The sprint that ended last stays above the current one; older ones fold
// under Completed.
func TestPreviousSprintStaysAboveCurrent(t *testing.T) {
	k := longBoardUI(t, 0)
	b := k.Plan.Boards[0]
	for _, id := range []string{"older", "previous", "current", "future"} {
		b.Items = append(b.Items, backend.SprintItem{ID: id, Title: id, Status: "Created", SprintID: id})
	}
	k.Plan.Boards[0] = b
	kanban.Draw(k)
	var order []string
	everything(k.Body, func(o fyne.CanvasObject) {
		if l, ok := o.(*widget.Label); ok && strings.HasSuffix(l.Text, " sprint") {
			order = append(order, l.Text)
		}
	})
	want := "Previous sprint|Current sprint|Future sprint|Older sprint"
	if got := strings.Join(order, "|"); got != want {
		t.Fatalf("sidebar order %q, want %q", got, want)
	}
	var capsules []string
	everything(k.Body, func(o fyne.CanvasObject) {
		if c, ok := o.(*canvas.Text); ok && (c.Text == "Previous" || c.Text == "Current") {
			capsules = append(capsules, c.Text)
		}
	})
	if got := strings.Join(capsules, "|"); got != "Previous|Current" {
		t.Fatalf("the previous and current sprints should each be labelled, got %q", got)
	}
}

// A redraw skips Kanban while it is out of sight, and opening it catches up.
func TestKanbanRedrawWaitsUntilShown(t *testing.T) {
	k := longBoardUI(t, 3)
	before := k.Body.Objects[0]
	k.Root.Hide()
	kanban.Draw(k)
	if k.Body.Objects[0] != before || !k.BoardStale {
		t.Fatal("a hidden board should not be rebuilt")
	}
	k.Root.Show()
	kanban.CatchUp(k)
	if k.Body.Objects[0] == before || k.BoardStale {
		t.Fatal("showing the board should draw what it missed")
	}
}

// Typing in Kanban's search redraws once the typing pauses, not per key;
// clearing the field redraws at once.
func TestKanbanSearchWaitsForAPause(t *testing.T) {
	k := longBoardUI(t, 3)
	// The real driver queues the debounce callback on the UI thread. Fyne's
	// test driver calls it on the timer goroutine instead; observing Body while
	// that callback was still drawing raced the text shaper with the clear.
	base := fyne.CurrentApp()
	driver := &kanbanSearchUIDriver{Driver: base.Driver(), queued: make(chan func(), 256)}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: driver})
	defer fyne.SetCurrentApp(base)
	var search *widget.Entry
	everything(k.Root, func(o fyne.CanvasObject) {
		if e, ok := o.(*widget.Entry); ok && e.PlaceHolder == "Search issues or project" {
			search = e
		}
	})
	if search == nil {
		t.Fatal("no Kanban search")
	}
	before := k.Body.Objects[0]
	search.SetText("Issue 1")
	if k.Body.Objects[0] != before {
		t.Fatal("a key should not redraw the board straight away")
	}
	deadline := time.NewTimer(5 * kanban.SearchPause)
	defer deadline.Stop()
	for k.Body.Objects[0] == before {
		select {
		case apply := <-driver.queued:
			apply()
		case <-deadline.C:
			t.Fatal("search did not redraw after the pause")
		}
	}
	if cards, _ := boardCards(k); cards != 1 {
		t.Fatalf("after the pause the board should show the one match, got %d cards", cards)
	}
	search.SetText("")
	if cards, _ := boardCards(k); cards != 3 {
		t.Fatalf("clearing should redraw at once, got %d cards", cards)
	}
}

type kanbanSearchUIDriver struct {
	fyne.Driver
	queued chan func()
}

func (d *kanbanSearchUIDriver) DoFromGoroutine(apply func(), _ bool) { d.queued <- apply }
