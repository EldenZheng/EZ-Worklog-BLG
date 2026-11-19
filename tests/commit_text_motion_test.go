package tests

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestCommitTextRemarksSlideFromRightAndReverseWithoutLosingText(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	driver := collectMotion(t)
	long := strings.Repeat("- work completed\n", 80)
	form := logwork.RowEditor(ui, backend.Row{"remarks": long}, func() {}, func() {}).All()
	var pane *logwork.CommitTextPane
	walk(form, func(o fyne.CanvasObject) {
		if p, ok := o.(*logwork.CommitTextPane); ok {
			pane = p
		}
	})
	if pane == nil {
		t.Fatal("missing text pane")
	}
	pane.Resize(fyne.NewSize(1000, 300))
	check := draftSplitCheck(form)
	if check == nil {
		t.Fatal("missing split checkbox")
	}
	latestSlide := func() *fyne.Animation {
		for i := len(driver.anims) - 1; i >= 0; i-- {
			if a := driver.anims[i]; a.Duration == shell.MorphTime {
				return a
			}
		}
		t.Fatal("split must animate")
		return nil
	}
	check.SetChecked(true)
	open := latestSlide()
	if pane.Remarks.Position().X < 1000 || pane.Description.Size().Width != 1000 {
		t.Fatal("new field must start outside the right edge")
	}
	open.Tick(.5)
	if x := pane.Remarks.Position().X; x <= 500 || x >= 1000 {
		t.Fatalf("field did not slide through the right half: %v", x)
	}
	if w := pane.Description.Size().Width; w <= 500 || w >= 1000 {
		t.Fatalf("description did not narrow smoothly: %v", w)
	}
	open.Tick(1)
	remarks := multiLineEntry(pane.Remarks)
	if remarks == nil || remarks.Text == "" {
		t.Fatal("split lost seeded remarks")
	}
	remarks.SetText("- edited summary")
	check.SetChecked(false)
	close := latestSlide()
	close.Tick(.4)
	at := pane.Remarks.Position()
	check.SetChecked(true)
	reopen := latestSlide()
	close.Tick(1)
	if pane.Remarks.Position() != at {
		t.Fatal("stale closing frame interrupted reopen")
	}
	reopen.Tick(1)
	if remarks.Text != "- edited summary" {
		t.Fatal("toggling discarded edited remarks")
	}
	if desc := multiLineEntry(pane.Description); desc == nil || desc.Text != long {
		t.Fatal("split changed full description")
	}
	check.SetChecked(false)
	latestSlide().Tick(1)
	if pane.Remarks.Visible() || pane.Description.Size().Width != 1000 {
		t.Fatal("closing must restore a single full-width field")
	}
}

func TestSplitCheckboxUsesFilterHoverAndDrawnTick(t *testing.T) {
	base := test.NewApp()
	defer base.Quit()
	driver := collectMotion(t)
	check := widget.NewCheck("Separate worklog remarks", nil)
	feedback := widgets.AnimatedTickChoice(check).(*widgets.ChoiceFeedback)
	feedback.Resize(fyne.NewSize(310, 40))
	r := feedback.CreateRenderer()
	defer r.Destroy()
	r.Layout(feedback.Size())
	objects := r.Objects()
	catcher := objects[len(objects)-1]
	catcher.(desktop.Hoverable).MouseIn(nil)
	if len(driver.anims) != 1 || driver.anims[0].Duration != widgets.HoverDuration {
		t.Fatal("check must use filter hover timing")
	}
	driver.anims[0].Tick(1)
	if _, _, _, alpha := objects[0].(*canvas.Rectangle).FillColor.RGBA(); alpha == 0 {
		t.Fatal("hover pill did not fade in")
	}
	catcher.(fyne.Tappable).Tapped(&fyne.PointEvent{})
	if !check.Checked || feedback.Animation != nil {
		t.Fatal("click must toggle with a drawn tick")
	}
	var tick *fyne.Animation
	for _, a := range driver.anims {
		if a.Duration == widgets.TickTime {
			tick = a
		}
	}
	if tick == nil {
		t.Fatal("missing tick drawing animation")
	}
	tick.Tick(.5)
	if !objects[2].Visible() {
		t.Fatal("tick overlay must be visible during drawing")
	}
	tick.Tick(1)
	if objects[2].Visible() {
		t.Fatal("finished tick must return to native check")
	}
	check.Disable()
	catcher.(fyne.Tappable).Tapped(nil)
	if !check.Checked {
		t.Fatal("disabled check changed")
	}
}
