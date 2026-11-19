package tests

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// A saved entry on its day in the week strip is a Kanban-style bubble: a click
// on it opens the editor, it has no Edit of its own, and its Delete is there
// only while the card is lit.
func TestDayCardOpensOnAClickAndShowsDeleteOnHover(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	shell.ReleaseBubble()
	if _, err := ui.Store.AppendRows([]backend.Row{{
		"date": "2026-08-10", "minutes": "60", "description": "triage",
	}}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)

	var card *logwork.DragTile
	var area *shell.HoverArea
	var del *shell.FadeIconButton
	walk(ui.WeekCols["2026-08-10"], func(o fyne.CanvasObject) {
		switch x := o.(type) {
		case *logwork.DragTile:
			card = x
		case *shell.HoverArea:
			area = x
		}
	})
	if card == nil || area == nil || card.OnTap == nil {
		t.Fatal("the day's entry should be a card that opens on a click")
	}
	// The Delete is the icon it always was, inside what fades it.
	var holder *fyne.Container
	walkRendered(area, func(o fyne.CanvasObject) {
		if c, ok := o.(*fyne.Container); ok {
			for _, child := range c.Objects {
				if over, ok := child.(*container.ThemeOverride); ok {
					if b, ok := over.Content.(*shell.FadeIconButton); ok {
						del, holder = b, c
					}
				}
			}
		}
	})
	if del == nil {
		t.Fatal("the card should carry the Delete icon")
	}
	if del.Visible() {
		t.Fatal("the Delete should wait for the pointer")
	}
	area.MouseIn(&desktop.MouseEvent{})
	if !del.Visible() || !holder.Visible() {
		t.Fatal("the Delete should show while the card is lit")
	}

	card.Tapped(&fyne.PointEvent{})
	if ui.Win.Canvas().Overlays().Top() == nil {
		t.Fatal("a click on the card should open its editor")
	}
}

// Two entries on the same day going to the same issue, or two bulk reviews on
// the same day, are both tagged, so the double is seen before it is pushed.
func TestSameDayDuplicatesAreTagged(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	if _, err := ui.Store.AppendRows([]backend.Row{
		{"date": "2026-08-11", "minutes": "30", "issue": "exampleorg/repo#7", "description": "one"},
		{"date": "2026-08-11", "minutes": "45", "issue": "exampleorg/repo#7", "description": "two"},
		{"date": "2026-08-11", "minutes": "20", "issue": "exampleorg/repo#8", "description": "other"},
		{"date": "2026-08-12", "minutes": "60", "type": backend.KindBulkReview, "issue": "exampleorg/tasks#1"},
		{"date": "2026-08-12", "minutes": "60", "type": backend.KindBulkReview, "issue": "exampleorg/tasks#2"},
	}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)
	tagged := func(day string) int {
		n := 0
		walk(ui.WeekCols[day], func(o fyne.CanvasObject) {
			if card, ok := o.(*logwork.DragTile); ok && contains(labels(card.Content), "Duplicates same day") {
				n++
			}
		})
		return n
	}
	if n := tagged("2026-08-11"); n != 2 {
		t.Fatalf("the two entries for #7 should be tagged, and only they: %d", n)
	}
	if n := tagged("2026-08-12"); n != 2 {
		t.Fatalf("both bulk reviews of the day should be tagged: %d", n)
	}
}

// The Delete on a card washes gold under the pointer as a quiet button does,
// fading in over the slow fade rather than snapping on.
func TestCardDeleteHoverFadesToGold(t *testing.T) {
	a, driver := pressTestApp(t)
	a.Settings().SetTheme(shell.GoldTheme{Theme: theme.DefaultTheme()}) // the app's own theme
	del := shell.NewFadeIconButton(theme.DeleteIcon(), func() {})
	w := a.NewWindow("delete")
	w.SetContent(del)
	var wash *canvas.Rectangle
	for _, o := range test.WidgetRenderer(del).Objects() {
		if r, ok := o.(*canvas.Rectangle); ok {
			wash = r
		}
	}
	driver.anims = nil
	del.MouseIn(&desktop.MouseEvent{})
	if len(driver.anims) != 1 || driver.anims[0].Duration > shell.SlowFade/2 {
		t.Fatal("the hover should fade in, and quickly")
	}
	driver.anims[0].Tick(0.25)
	_, _, _, early := wash.FillColor.RGBA()
	driver.anims[0].Tick(1)
	r, g, b, settled := wash.FillColor.RGBA()
	if early >= settled {
		t.Fatalf("the wash should still be fading in a quarter of the way: %d then %d", early, settled)
	}
	if !(r > g && g > b) {
		t.Fatalf("the hover should be gold, got %v", wash.FillColor)
	}
	// Fading out it stays gold, only fainter, never dark.
	del.MouseOut()
	driver.anims[len(driver.anims)-1].Tick(0.5)
	fading := color.NRGBAModel.Convert(wash.FillColor).(color.NRGBA)
	gold := color.NRGBAModel.Convert(shell.QuietHoverColor()).(color.NRGBA)
	if fading.R != gold.R || fading.G != gold.G || fading.B != gold.B || fading.A >= gold.A {
		t.Fatalf("the wash should fade out as fainter gold, got %v", fading)
	}
}

// Holding a card lights in gold every other card going to the same issue,
// on whatever day it stands, and lets them go once the card is dropped.
func TestCarryingACardMarksItsMatches(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	if _, err := ui.Store.AppendRows([]backend.Row{
		{"date": "2026-08-11", "minutes": "30", "issue": "exampleorg/repo#7", "description": "carried"},
		{"date": "2026-08-12", "minutes": "45", "issue": "exampleorg/repo#7", "description": "already there"},
		{"date": "2026-08-12", "minutes": "20", "issue": "exampleorg/repo#8", "description": "other"},
		{"date": "2026-08-13", "minutes": "20", "issue": "exampleorg/repo#7", "description": "later"},
	}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)
	bubbles := map[string]*shell.StripeBubble{}
	var carried backend.Row
	for _, day := range []string{"2026-08-11", "2026-08-12", "2026-08-13"} {
		walk(ui.WeekCols[day], func(o fyne.CanvasObject) {
			if card, ok := o.(*logwork.DragTile); ok {
				bubbles[card.Row["description"]] = card.Content.(*shell.HoverArea).Bubble()
				if card.Row["description"] == "carried" {
					carried = card.Row
				}
			}
		})
	}
	ui.DragRow, ui.HoverDay = carried, "2026-08-11"
	logwork.MarkDropTargets(ui)
	if !bubbles["already there"].Matched() || !bubbles["later"].Matched() || bubbles["other"].Matched() {
		t.Fatal("every card for the same issue, and only those, should light in gold")
	}
	if bubbles["carried"].Matched() {
		t.Fatal("the card in hand should not mark itself")
	}
	ui.DragRow, ui.HoverDay = nil, ""
	logwork.MarkDropTargets(ui)
	if bubbles["already there"].Matched() || bubbles["later"].Matched() {
		t.Fatal("dropping the card should let the gold go")
	}
}

// The date picker edges in gold the days already holding an unpushed entry
// for the issue being edited, besides the day picked.
func TestDatePickerMarksDaysWithTheSameIssue(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	if _, err := ui.Store.AppendRows([]backend.Row{
		{"date": "2026-08-11", "minutes": "30", "issue": "exampleorg/repo#7"},
		{"date": "2026-08-14", "minutes": "30", "issue": "exampleorg/repo#8"},
	}); err != nil {
		t.Fatal(err)
	}
	gold := func(p *logwork.WorklogDatePicker) int {
		n := 0
		walk(p.Grid, func(o fyne.CanvasObject) {
			if r, ok := o.(*canvas.Rectangle); ok && r.StrokeWidth == 2 && sameColor(r.StrokeColor, widgets.LogoGold) {
				n++
			}
		})
		return n
	}
	p := logwork.NewWorklogDatePicker(ui, "2026-08-20")
	if n := gold(p); n != 1 {
		t.Fatalf("with nothing to match only the day picked is gold: %d", n)
	}
	p.Matches = func() (string, string) { return "issue exampleorg/repo#7", "" }
	p.Refresh()
	if n := gold(p); n != 2 {
		t.Fatalf("the day holding #7 should be gold too: %d", n)
	}
}

// A pending tile's Ignore fades its gold in and out as the Delete does, and
// draws no hover of its own on top.
func TestIgnoreHoverFadesLikeDelete(t *testing.T) {
	a, driver := pressTestApp(t)
	a.Settings().SetTheme(shell.GoldTheme{Theme: theme.DefaultTheme()})
	b := shell.NewHoverButton("Ignore", nil, func() {})
	view := shell.FadeHover(b).(*fyne.Container)
	w := a.NewWindow("ignore")
	w.SetContent(view)
	wash := view.Objects[0].(*canvas.Rectangle)
	driver.anims = nil
	b.MouseIn(&desktop.MouseEvent{})
	if len(driver.anims) != 1 || driver.anims[0].Duration > shell.SlowFade/2 {
		t.Fatal("the hover should fade in, and quickly")
	}
	driver.anims[0].Tick(1)
	gold := color.NRGBAModel.Convert(shell.QuietHoverColor()).(color.NRGBA)
	if got := color.NRGBAModel.Convert(wash.FillColor).(color.NRGBA); got != gold {
		t.Fatalf("the wash should settle on the Delete's gold, got %v", got)
	}
	for _, o := range test.WidgetRenderer(&b.Button).Objects() {
		if r, ok := o.(*canvas.Rectangle); ok {
			if _, _, _, alpha := r.FillColor.RGBA(); alpha != 0 {
				t.Fatalf("the button should leave its hover to the wash, drew %v", r.FillColor)
			}
			break
		}
	}
	b.MouseOut()
	driver.anims[len(driver.anims)-1].Tick(0.5)
	if got := color.NRGBAModel.Convert(wash.FillColor).(color.NRGBA); got.R != gold.R || got.A >= gold.A {
		t.Fatalf("fading out it should stay gold, only fainter: %v", got)
	}
}
