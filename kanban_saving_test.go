package main

import (
	"errors"
	"image/color"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// stubPush replaces the GitHub write with one that waits for release.
func stubPush(t *testing.T, result error) (release func()) {
	t.Helper()
	gate := make(chan struct{})
	old := pushSprintItem
	pushSprintItem = func(sprintBoard, sprintItem, string, string) error {
		<-gate
		return result
	}
	t.Cleanup(func() { pushSprintItem = old })
	return func() { close(gate) }
}

func columnLabels(k *kanbanUI) []string {
	var out []string
	walkCanvas(k.body, func(o fyne.CanvasObject) {
		if l, ok := o.(*widget.Label); ok {
			out = append(out, l.Text)
		}
	})
	return out
}

func kanbanSavingFixture(t *testing.T) *kanbanUI {
	ui, _, _ := plannerFixture(t)
	k := ui.kanban
	b := &k.plan.Boards[0]
	b.Fields = append(b.Fields, sprintField{Name: "Status", Options: []fieldOption{{Name: "Created"}, {Name: "In Progress"}}})
	b.Items[0].Status = "Created"
	k.filter = "all"
	k.draw()
	return k
}

// While a status save runs the card sits in the column it is headed for,
// marked as saving, with the activity bar running and Refresh off.
func TestKanbanMoveShowsSavingUntilGitHubAnswers(t *testing.T) {
	k := kanbanSavingFixture(t)
	release := stubPush(t, nil)
	b := k.plan.Boards[0]
	finished := make(chan struct{})
	k.moveThen(b, b.Items[0], "status", "In Progress", nil, func(error) { close(finished) })
	if !k.busy || !k.activity.view.Visible() || !k.calendarActivity.view.Visible() || k.activity.caption.Text != "Saving to GitHub…" || !k.refreshButtons[0].Disabled() {
		t.Fatal("activity not shown while saving")
	}
	if k.pendingStatus(b, b.Items[0]) != "In Progress" {
		t.Fatal("card not drawn in its target column while saving")
	}
	if !strings.Contains(strings.Join(columnLabels(k), "|"), "Saving to In Progress…") {
		t.Fatal("card does not say it is saving", columnLabels(k))
	}
	release()
	// The UI-thread callback runs on its own goroutine in tests; done is
	// called last, after the redraw.
	<-finished
	if k.activity.view.Visible() || k.refreshButtons[0].Disabled() || len(k.pending) != 0 {
		t.Fatal("saving state left behind")
	}
	if k.plan.Boards[0].Items[0].Status != "In Progress" || !strings.Contains(k.message.Text, "status is now In Progress") {
		t.Fatal("result not applied or reported", k.message.Text)
	}
}

// A failed save puts the card back and says so.
func TestKanbanMoveFailureRestoresCard(t *testing.T) {
	k := kanbanSavingFixture(t)
	release := stubPush(t, errors.New("boom"))
	b := k.plan.Boards[0]
	finished := make(chan struct{})
	k.moveThen(b, b.Items[0], "status", "In Progress", nil, func(error) { close(finished) })
	release()
	<-finished
	if k.plan.Boards[0].Items[0].Status != "Created" || len(k.pending) != 0 || !strings.Contains(k.message.Text, "failed") {
		t.Fatal("failure not restored or reported", k.message.Text)
	}
}

func TestFieldValueLabelNamesSprintAndEmptyValues(t *testing.T) {
	b := sprintBoard{Fields: []sprintField{{Name: "Sprint"}}}
	b.Fields[0].Configuration.Iterations = []sprintIteration{{ID: "s5", Title: "Sprint 5"}}
	if fieldValueLabel(b, "sprint", "s5") != "Sprint 5" || fieldValueLabel(b, "sprint", "") != "No sprint" || fieldValueLabel(b, "status", "") != "No status" {
		t.Fatal("labels wrong")
	}
}

// The saving text is gold and wraps inside the card, however long the name.
func TestSavingOverlayTextIsGoldAndStaysInsideCard(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("overlay")
	defer w.Close()
	card := canvas.NewRectangle(color.Transparent)
	card.SetMinSize(fyne.NewSize(260, 90))
	overlay := savingOverlay(card, kanbanPending{kind: "sprint", label: strings.Repeat("Very long sprint name ", 6)})
	w.SetContent(container.NewWithoutLayout(overlay))
	overlay.Resize(fyne.NewSize(260, 90))
	overlay.Refresh()
	var label *widget.Label
	var ink color.Color
	walkCanvas(overlay, func(o fyne.CanvasObject) {
		if l, ok := o.(*widget.Label); ok {
			label = l
		}
		if th, ok := o.(*container.ThemeOverride); ok {
			ink = th.Theme.(statusHeaderTheme).ink
		}
	})
	if label == nil || label.Wrapping != fyne.TextWrapWord || ink != logoGold {
		t.Fatal("saving text not wrapped gold")
	}
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(label)
	start := fyne.CurrentApp().Driver().AbsolutePositionForObject(overlay)
	if pos.X < start.X || pos.X+label.Size().Width > start.X+260 {
		t.Fatal("saving text runs past the card", pos, label.Size())
	}
}
