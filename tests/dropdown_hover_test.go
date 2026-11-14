package tests

import (
	"math"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"worklog/internal/shared/shell"
)

func TestDropdownGoldHoverTravelsAndKeepsSearchSelection(t *testing.T) {
	base := test.NewApp()
	defer base.Quit()
	w := base.NewWindow("dropdown")
	selected := ""
	pick := shell.NewGoldSelect([]string{"Alpha", "Beta", "Gamma"}, func(value string) { selected = value })
	pick.Searchable = true
	w.SetContent(pick)
	w.Resize(fyne.NewSize(500, 400))
	pick.Tapped(nil)
	defer shell.FinishDropdownClosings()
	host := pick.Rows.Objects[len(pick.Rows.Objects)-1].(fyne.Widget)
	pill := test.WidgetRenderer(host).Objects()[0].(*canvas.Rectangle)
	d := &collectDriver{Driver: base.Driver()}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: d})
	defer fyne.SetCurrentApp(base)
	at := func(i int) fyne.Position {
		row := pick.Rows.Objects[i]
		return d.AbsolutePositionForObject(row).Add(fyne.NewPos(20, row.Size().Height/2))
	}
	test.MoveMouse(w.Canvas(), at(0))
	if len(d.anims) == 0 {
		t.Fatal("entering a dropdown option must start the gold hover")
	}
	d.anims[len(d.anims)-1].Tick(1)
	from := pill.Position().Y
	test.MoveMouse(w.Canvas(), at(2))
	slide := d.anims[len(d.anims)-1]
	if slide.Duration != 600*time.Millisecond {
		t.Fatal("dropdown hover must use the shared duration")
	}
	slide.Tick(.5)
	middle := pill.Position().Y
	slide.Tick(1)
	if math.Abs(float64(middle-(from+pill.Position().Y)/2)) > .1 || pill.Position().Y == from {
		t.Fatal("the gold highlight must travel between options")
	}
	pick.Find.SetText("gam")
	if got := choiceTexts(pick); len(got) != 1 || got[0] != "Gamma" {
		t.Fatal("hover must preserve dropdown search", got)
	}
	pick.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if selected != "Gamma" {
		t.Fatal("keyboard selection must still use the filtered option")
	}
}
