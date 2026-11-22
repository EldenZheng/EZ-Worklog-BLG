package tests

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func patchNotesCheck(w fyne.Window) *widget.Check {
	var found *widget.Check
	for _, o := range w.Canvas().Overlays().List() {
		walkRendered(o, func(o fyne.CanvasObject) {
			if c, ok := o.(*widget.Check); ok && c.Text == "Don't show this again" {
				found = c
			}
		})
	}
	return found
}

func TestPatchNotesReadingHeightAndSectionDividers(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	app.Settings().SetTheme(shell.GoldTheme{Theme: theme.DefaultTheme()})
	w := app.NewWindow("notes")
	defer w.Close()
	w.SetContent(sizedBox(1, 1))
	ui := &shell.UI{Win: w}
	for _, height := range []float32{900, 520} {
		w.Resize(fyne.NewSize(1200, height))
		shell.ShowPatchNotes(ui)
		popup := overlayField(w.Canvas().Overlays().Top(), "Content").(*widget.PopUp)
		size := popup.Size()
		if size.Height >= height || size.Height < 0.9*height {
			t.Fatalf("notes should use most of the window height and leave room around the frame: popup %v, window height %v", size, height)
		}
		dividers := 0
		walkRendered(popup.Content, func(o fyne.CanvasObject) {
			if divider, ok := o.(*widget.Separator); ok {
				dividers++
				line := test.WidgetRenderer(divider).Objects()[0].(*canvas.Rectangle)
				want := theme.Color(theme.ColorNameSeparator)
				r, g, b, _ := line.FillColor.RGBA()
				if !sameColor(line.FillColor, want) || !(r > g && g > b) || sameColor(line.FillColor, widgets.LogoGold) {
					t.Errorf("section divider should use the theme's muted gold, got %v", line.FillColor)
				}
			}
		})
		if dividers == 0 {
			t.Fatal("missing content section dividers")
		}
		shell.ClosePopUp(popup)
	}
}

// The patch notes open at start-up until "Don't show this again" is ticked
// before OK; OK alone brings them back next time.
func TestPatchNotesOpenUntilTurnedOff(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	ui.Win.Resize(fyne.NewSize(1200, 800))

	shell.ShowPatchNotes(ui)
	test.Tap(waitForButton(t, ui.Win, "OK, understood"))
	if cfg, _ := ui.Store.LoadConfig(); cfg.PatchNotesSeen != "" || ui.Cfg.PatchNotesSeen != "" {
		t.Fatal("OK without the tick must leave the notes to open again")
	}

	shell.ShowPatchNotes(ui)
	waitForButton(t, ui.Win, "OK, understood")
	check := patchNotesCheck(ui.Win)
	if check == nil {
		t.Fatal("the notes need a \"Don't show this again\" tick")
	}
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(check).AddXY(check.Size().Width/2, check.Size().Height/2)
	test.MoveMouse(ui.Win.Canvas(), at)
	var hover *canvas.Rectangle
	var heading *widget.Label
	walkRendered(ui.Win.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if r, ok := o.(*canvas.Rectangle); ok && r.FillColor == widgets.WithAlpha(widgets.LogoGold, 0.16) {
			hover = r
		}
		if l, ok := o.(*widget.Label); ok && l.Text == "What's new" {
			heading = l
		}
	})
	if hover == nil || hover.Size().Width != check.Size().Width {
		t.Fatal("hovering the checkbox label should light the full choice in gold")
	}
	if heading == nil || heading.SizeName != theme.SizeNameHeadingText ||
		heading.Theme().Color(theme.ColorNameForeground, fyne.CurrentApp().Settings().ThemeVariant()) != widgets.LogoGold {
		t.Fatal("the startup notice should use the gold confirmation heading")
	}
	test.TapCanvas(ui.Win.Canvas(), at)
	if !check.Checked {
		t.Fatal("tapping the highlighted label should tick the checkbox")
	}
	test.Tap(waitForButton(t, ui.Win, "OK, understood"))
	if cfg, _ := ui.Store.LoadConfig(); cfg.PatchNotesSeen != shell.PatchNotesID {
		t.Fatalf("ticked notes must be remembered, saved %q", cfg.PatchNotesSeen)
	}

	before := len(ui.Win.Canvas().Overlays().List())
	shell.ShowPatchNotes(ui)
	if len(ui.Win.Canvas().Overlays().List()) != before {
		t.Fatal("notes turned off must not open again")
	}
}
