package tests

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/kanban"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// collectMotion holds every animation started from here on, unplayed, so a
// test can look at the first frame of a motion and nothing runs behind it.
func collectMotion(t *testing.T) *collectDriver {
	t.Helper()
	base := fyne.CurrentApp()
	driver := &collectDriver{Driver: base.Driver()}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: driver})
	t.Cleanup(func() { fyne.SetCurrentApp(base) })
	return driver
}

// sprintRowNamed is the sidebar row whose title is name.
func sprintRowNamed(t *testing.T, k *shell.KanbanUI, name string) *shell.Tappable {
	t.Helper()
	var row *shell.Tappable
	everything(k.Body, func(o fyne.CanvasObject) {
		if r, ok := o.(*shell.Tappable); ok {
			everything(r.Content, func(inner fyne.CanvasObject) {
				if l, ok := inner.(*widget.Label); ok && l.Text == name {
					row = r
				}
			})
		}
	})
	if row == nil {
		t.Fatalf("no sidebar row for %q", name)
	}
	return row
}

// sprintFrame is the frame of the sidebar row for sprint key.
func sprintFrame(t *testing.T, k *shell.KanbanUI, key string) *canvas.Rectangle {
	t.Helper()
	for _, target := range k.Targets {
		if target.Kind == "sprint" && target.Value == key {
			return target.Frame
		}
	}
	t.Fatalf("no sidebar frame for %q", key)
	return nil
}

// A sprint picked in the sidebar does not snap gold: the gold spreads from
// the click while the row last picked drains, and both settle as the motion
// ends.
func TestPickingASprintSpreadsItsGold(t *testing.T) {
	k := longBoardUI(t, 2)
	driver := collectMotion(t)
	b := doneHistoryBoard()
	k.Plan.Boards[0].Items = append(k.Plan.Boards[0].Items, backend.SprintItem{ID: "later", Title: "Later work",
		URL: "https://github.com/exampleorg/repo/issues/99", Status: "Created", SprintID: "future"})
	var future, current string
	for _, s := range b.Iterations() {
		switch s.ID {
		case "future":
			future = backend.SprintKey(s)
		case "current":
			current = backend.SprintKey(s)
		}
	}
	k.Filter, k.Sprint = "sprint", current
	kanban.Draw(k)
	if !sameColor(sprintFrame(t, k, current).FillColor, widgets.LogoGold) {
		t.Fatal("the picked sprint should be gold at rest")
	}

	test.Tap(sprintRowNamed(t, k, "Future sprint"))
	if k.Sprint != future {
		t.Fatalf("the click should pick the future sprint, got %q", k.Sprint)
	}
	if sameColor(sprintFrame(t, k, future).FillColor, widgets.LogoGold) {
		t.Fatal("the gold should spread in, not snap")
	}
	spreading := 0
	everything(sprintRowNamed(t, k, "Future sprint"), func(o fyne.CanvasObject) {
		if r, ok := o.(*canvas.Rectangle); ok && sameColor(r.FillColor, widgets.LogoGold) && r.Size().Width < 290 {
			spreading++
		}
	})
	if spreading == 0 {
		t.Fatal("the gold should be spreading from the click")
	}

	// Timed by the frames drawn, the motion ends with its last frame, not
	// with the clock.
	for _, a := range driver.anims {
		a.Tick(1)
	}
	kanban.Draw(k)
	if !sameColor(sprintFrame(t, k, future).FillColor, widgets.LogoGold) {
		t.Fatal("once the motion ends the picked sprint should be gold")
	}
	if sameColor(sprintFrame(t, k, current).FillColor, widgets.LogoGold) {
		t.Fatal("the sprint let go of should be gold no more")
	}
}

// A Kanban card presses as Log Work's tiles do: drawn shrunk while held,
// and put back at once when a drag lifts it.
func TestKanbanCardsPress(t *testing.T) {
	k := longBoardUI(t, 1)
	driver := collectMotion(t)
	var card *shell.SprintDragCard
	everything(k.Body, func(o fyne.CanvasObject) {
		if c, ok := o.(*shell.SprintDragCard); ok {
			card = c
		}
	})
	if card == nil {
		t.Fatal("no card on the board")
	}
	objects := test.WidgetRenderer(card).Objects()
	shot := objects[len(objects)-1].(*canvas.Image)
	if card.Cursor() != desktop.PointerCursor {
		t.Fatal("a card at rest should show the pointing hand")
	}
	card.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	if card.Cursor() == desktop.PointerCursor {
		t.Fatal("a card held should show the grabbing hand")
	}
	if len(driver.anims) == 0 {
		t.Fatal("holding the card should start its press")
	}
	driver.anims[len(driver.anims)-1].Tick(1)
	if card.Content.Visible() || !shot.Visible() || shot.Size().Width >= card.Size().Width {
		t.Fatalf("the held card should be drawn shrunk: card %v, picture %v at %v", card.Content.Visible(), shot.Visible(), shot.Size())
	}
	card.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: fyne.NewPos(40, 40)}})
	card.DragEnd()
	if !card.Content.Visible() || shot.Visible() {
		t.Fatal("a drag should put the card back as it lifts it")
	}
	card.MouseUp(&desktop.MouseEvent{})
	if card.Cursor() != desktop.PointerCursor {
		t.Fatal("a card let go should show the pointing hand again")
	}
}
