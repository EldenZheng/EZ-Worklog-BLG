package tests

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/settings"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestSettingsOrgColorAdvancedHeadingIsPainted(t *testing.T) {
	ui, view := storageSettingsFixture(t)
	fyne.CurrentApp().Settings().SetTheme(shell.GoldTheme{Theme: theme.DefaultTheme()})
	ui.Win.Resize(fyne.NewSize(1500, 1000))
	more := buttonNamed(view, "...")
	if more == nil {
		t.Fatal("org colour control missing")
	}
	more.OnTapped()
	popup := overlayField(ui.Win.Canvas().Overlays().Top(), "Content").(*widget.PopUp)
	_, advanced := colorPickerRows(t, popup)
	var heading *canvas.Text
	walk(advanced, func(o fyne.CanvasObject) {
		if text, ok := o.(*canvas.Text); ok && text.Text == "Advanced" {
			heading = text
		}
	})
	if heading == nil {
		t.Fatal("Advanced text missing")
	}
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(heading)
	rowAt := fyne.CurrentApp().Driver().AbsolutePositionForObject(advanced)
	if at.X-rowAt.X < theme.Padding() || at.Y-rowAt.Y < theme.Padding() || advanced.Size().Height-heading.Size().Height < 2*theme.Padding() {
		t.Fatal("Advanced text needs padding inside its hover target")
	}
	if out := os.Getenv("WORKLOG_COLOR_TEST_RENDER"); out != "" {
		captureSprintPNG(t, ui.Win, filepath.Join(out, "org-color.png"))
	}
	im := ui.Win.Canvas().Capture()
	gold := 0
	for y := int(at.Y); y < int(at.Y+heading.Size().Height); y++ {
		for x := int(at.X); x < int(at.X+heading.Size().Width); x++ {
			r, g, b, _ := im.At(x, y).RGBA()
			if r > 0x9000 && g > 0x6000 && b < 0x7000 && r > g && g > b {
				gold++
			}
		}
	}
	if gold < 10 {
		t.Fatalf("Advanced exists in the tree but is not painted (%d gold pixels)", gold)
	}
	advanced.Tapped(nil)
	var fold *shell.Fold
	walk(popup.Content, func(o fyne.CanvasObject) {
		if f, ok := o.(*shell.Fold); ok {
			fold = f
		}
	})
	if fold == nil || !fold.Content.Visible() {
		t.Fatal("expanded Advanced controls are hidden")
	}
	fields := 0
	walkRendered(fold.Content, func(o fyne.CanvasObject) {
		if _, ok := o.(interface {
			fyne.Focusable
			SetText(string)
		}); ok && o.Visible() && o.Size().Height > 0 {
			fields++
		}
	})
	if fields == 0 {
		t.Fatal("Advanced has no visible colour fields")
	}
	if out := os.Getenv("WORKLOG_COLOR_TEST_RENDER"); out != "" {
		captureSprintPNG(t, ui.Win, filepath.Join(out, "org-color-advanced.png"))
	}
}

func colorPickerRows(t *testing.T, popup *widget.PopUp) (choices []*shell.Tappable, advanced *shell.Tappable) {
	t.Helper()
	walk(popup.Content, func(o fyne.CanvasObject) {
		if row, ok := o.(*shell.Tappable); ok {
			if hasRenderedText(row, "Advanced") {
				advanced = row
			} else {
				choices = append(choices, row)
			}
		}
	})
	if advanced == nil || len(choices) < 14 {
		t.Fatalf("colour choices and Advanced must share gold hover: %d choices, Advanced=%v", len(choices), advanced != nil)
	}
	return
}

func TestOrgColorPickerHoverTravelsAcrossSwatchesAndAdvanced(t *testing.T) {
	ui := editorUI(t)
	base := fyne.CurrentApp()
	defer base.Quit()
	ui.Win.Resize(fyne.NewSize(1000, 900))
	settings.OrgColorPicker(ui, "exampleorg", shell.GitHubGreen, nil)
	popup := overlayField(ui.Win.Canvas().Overlays().Top(), "Content").(*widget.PopUp)
	choices, advanced := colorPickerRows(t, popup)
	var pill *canvas.Rectangle
	walkRendered(popup.Content, func(o fyne.CanvasObject) {
		if rect, ok := o.(*canvas.Rectangle); ok && rect.CornerRadius == 8 && rect.StrokeWidth == 1 {
			pill = rect
		}
	})
	if pill == nil {
		t.Fatal("shared hover pill missing")
	}
	driver := collectMotion(t)
	point := func(o fyne.CanvasObject) fyne.Position {
		return base.Driver().AbsolutePositionForObject(o).Add(fyne.NewPos(o.Size().Width/2, o.Size().Height/2))
	}
	test.MoveMouse(ui.Win.Canvas(), point(choices[0]))
	if len(driver.anims) == 0 {
		t.Fatal("pointer did not reach the colour hover group")
	}
	driver.anims[len(driver.anims)-1].Tick(1)
	if !sameColor(pill.FillColor, widgets.WithAlpha(widgets.LogoGold, .16)) {
		t.Fatal("swatch must use the shared gold highlight")
	}
	start := pill.Position()
	test.MoveMouse(ui.Win.Canvas(), point(choices[5]))
	slide := driver.anims[len(driver.anims)-1]
	if slide.Duration != 600*time.Millisecond {
		t.Fatal("swatch hover must use 600 ms")
	}
	slide.Tick(.5)
	middle := pill.Position()
	slide.Tick(1)
	if middle == start || middle == pill.Position() {
		t.Fatal("swatch highlight jumped instead of travelling")
	}
	test.MoveMouse(ui.Win.Canvas(), point(advanced))
	slide = driver.anims[len(driver.anims)-1]
	slide.Tick(.5)
	middle = pill.Position()
	slide.Tick(1)
	if middle == pill.Position() {
		t.Fatal("the same highlight must travel to Advanced")
	}
	test.MoveMouse(ui.Win.Canvas(), fyne.NewPos(0, 0))
	driver.anims[len(driver.anims)-1].Tick(1)
	if _, _, _, alpha := pill.FillColor.RGBA(); alpha != 0 {
		t.Fatal("leaving must fade the highlight out")
	}
}

func TestOrgColorPickerAdvancedResizesSmoothlyAndShrinksBack(t *testing.T) {
	ui := editorUI(t)
	base := fyne.CurrentApp()
	defer base.Quit()
	ui.Win.Resize(fyne.NewSize(1000, 1000))
	settings.OrgColorPicker(ui, "exampleorg", shell.GitHubGreen, nil)
	popup := overlayField(ui.Win.Canvas().Overlays().Top(), "Content").(*widget.PopUp)
	_, advanced := colorPickerRows(t, popup)
	closed := popup.Size()
	var fold *shell.Fold
	walk(popup.Content, func(o fyne.CanvasObject) {
		if f, ok := o.(*shell.Fold); ok {
			fold = f
		}
	})
	driver := collectMotion(t)
	advanced.Tapped(nil)
	if popup.Size() != closed {
		t.Fatal("opening must wait for its first animation frame")
	}
	if len(driver.anims) != 1 || driver.anims[0].Duration != widgets.HoverDuration {
		t.Fatalf("Advanced must use the ignored-list fold: %d animations", len(driver.anims))
	}
	opening := driver.anims[0]
	if opening.Curve(.25) != shell.EaseInOutCubic(.25) {
		t.Fatal("Advanced must use the ignored-list easing")
	}
	opening.Tick(.5)
	middle := popup.Size()
	opening.Tick(1)
	if fold == nil || !fold.Content.Visible() {
		t.Fatal("Advanced grew the popup but its colour controls are hidden")
	}
	full := popup.Size()
	if closed.Width != full.Width || middle.Width != full.Width {
		t.Fatalf("collapsed picker must keep Advanced's full width: closed %v, opening %v, expanded %v", closed, middle, full)
	}
	if middle.Height <= closed.Height || middle.Height >= full.Height {
		t.Fatalf("popup did not grow with the fold: %v, %v, %v", closed, middle, full)
	}
	advanced.Tapped(nil)
	closing := driver.anims[len(driver.anims)-1]
	closing.Tick(.5)
	if h := popup.Size().Height; h <= closed.Height || h >= full.Height {
		t.Fatal("closing must shrink the popup progressively")
	}
	closing.Tick(1)
	if popup.Size() != closed || popup.Content.Size() != closed {
		t.Fatalf("collapsed popup left blank space: %v, content %v; want %v", popup.Size(), popup.Content.Size(), closed)
	}
	// A stale closing frame cannot undo a new opening.
	advanced.Tapped(nil)
	driver.anims[len(driver.anims)-1].Tick(.4)
	before := popup.Size()
	closing.Tick(1)
	if popup.Size() != before {
		t.Fatal("stale fold changed the popup")
	}
}

func TestOrgColorPickerSwatchAndAdvancedConfirmStillSave(t *testing.T) {
	for _, mode := range []string{"swatch", "advanced"} {
		t.Run(mode, func(t *testing.T) {
			ui := editorUI(t)
			defer fyne.CurrentApp().Quit()
			ui.Win.Resize(fyne.NewSize(1000, 900))
			saved := 0
			picker := settings.OrgColorPicker(ui, "exampleorg", shell.GitHubGreen, func() { saved++ })
			popup := overlayField(ui.Win.Canvas().Overlays().Top(), "Content").(*widget.PopUp)
			choices, advanced := colorPickerRows(t, popup)
			want := "#f44336"
			if mode == "swatch" {
				choices[0].Tapped(nil)
			} else {
				advanced.Tapped(nil)
				picker.SetColor(color.NRGBA{R: 20, G: 70, B: 180, A: 255})
				want = "#1446b4"
				waitForButton(t, ui.Win, "Confirm").OnTapped()
			}
			if saved != 1 || ui.Cfg.OrgColors["exampleorg"] != want {
				t.Fatalf("colour not saved once: %d, %v", saved, ui.Cfg.OrgColors)
			}
			if ui.Win.Canvas().Overlays().Top() != nil {
				t.Fatal("choosing a colour must close the popup")
			}
			cfg, ok := ui.Store.LoadConfig()
			if !ok || cfg.OrgColors["exampleorg"] != want {
				t.Fatal("colour was not persisted")
			}
		})
	}
}
