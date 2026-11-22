//go:build cursors

package tests

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/settings"
	"worklog/internal/shared/widgets"
)

func TestSettingsOrgMenuGoldChasesHoveredControl(t *testing.T) {
	ui, _ := storageSettingsFixture(t)
	base := fyne.CurrentApp()
	ui.Cfg.Repos = []string{"exampleorg", "Example-Support"}
	view := settings.BuildSettingsTab(ui)
	ui.Win.SetContent(view)
	ui.Win.Resize(fyne.NewSize(1500, 1000))
	var buttons []*widget.Button
	walk(view, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "..." {
			buttons = append(buttons, b)
		}
	})
	if len(buttons) < 2 {
		t.Fatal("need two org colour controls")
	}
	driver := collectMotion(t)
	at := func(b *widget.Button) fyne.Position {
		return base.Driver().AbsolutePositionForObject(b).Add(fyne.NewPos(b.Size().Width/2, b.Size().Height/2))
	}
	test.MoveMouse(ui.Win.Canvas(), at(buttons[0]))
	if len(driver.anims) == 0 {
		t.Fatal("org menu has no shared gold hover")
	}
	enter := driver.anims[len(driver.anims)-1]
	if enter.Duration != widgets.HoverDuration {
		t.Fatal("org hover must use the shared timing")
	}
	enter.Tick(1)
	var pill *canvas.Rectangle
	walkRendered(view, func(o fyne.CanvasObject) {
		if rect, ok := o.(*canvas.Rectangle); ok && sameColor(rect.FillColor, widgets.WithAlpha(widgets.LogoGold, .42)) && rect.StrokeWidth == 1 {
			pill = rect
		}
	})
	if pill == nil {
		t.Fatal("gold hover pill missing")
	}
	if !sameColor(pill.StrokeColor, widgets.WithAlpha(widgets.LogoGold, .95)) {
		t.Fatal("org menu highlight must have a strong gold edge")
	}
	start := pill.Position()
	test.MoveMouse(ui.Win.Canvas(), at(buttons[1]))
	slide := driver.anims[len(driver.anims)-1]
	if slide == enter || slide.Duration != widgets.HoverDuration {
		t.Fatal("moving between menu controls must move one shared gold pill")
	}
	slide.Tick(.5)
	middle := pill.Position()
	enter.Tick(1)
	if pill.Position() != middle {
		t.Fatal("a stale hover frame interrupted the chase")
	}
	slide.Tick(1)
	if middle == start || middle == pill.Position() || pill.Position().Y == start.Y {
		t.Fatal("gold must travel between org menu controls instead of independently fading")
	}
	for _, b := range buttons {
		background := test.WidgetRenderer(b).Objects()[0].(*canvas.Rectangle)
		if _, _, _, a := background.FillColor.RGBA(); a != 0 {
			t.Fatal("the button's stock wash must stay clear under the shared gold pill")
		}
	}
	test.MoveMouse(ui.Win.Canvas(), fyne.NewPos(0, 0))
	driver.anims[len(driver.anims)-1].Tick(1)
	if _, _, _, a := pill.FillColor.RGBA(); a != 0 {
		t.Fatal("leaving org controls must fade the shared gold out")
	}
	buttons[1].OnTapped()
	if ui.Win.Canvas().Overlays().Top() == nil || waitForButton(t, ui.Win, "Confirm") == nil {
		t.Fatal("org menu no longer opens the colour picker")
	}
}

func TestNewSettingsButtonsFadeHoverInAndOut(t *testing.T) {
	for _, group := range []string{"sidebar", "menu", "reallocate", "clear"} {
		t.Run(group, func(t *testing.T) {
			ui, view := storageSettingsFixture(t)
			root := view
			names := []string{"Back up from GitHub", "…", "Check for updates", "Update"}
			ui.Update.InstallView.Show()
			ui.Update.Frame.Refresh()
			switch group {
			case "menu":
				tapButton(t, view, "…")
				root = ui.Win.Canvas().Overlays().Top()
				names = []string{"Reallocate backup", "Clear backup"}
			case "reallocate":
				openStorageAction(t, ui, view, "Reallocate backup")
				root = ui.Win.Canvas().Overlays().Top()
				names = []string{"Choose folder", "Cancel", "Reallocate"}
			case "clear":
				openStorageAction(t, ui, view, "Clear backup")
				root = ui.Win.Canvas().Overlays().Top()
				names = []string{"Cancel", "Clear backup"}
			}
			driver := collectMotion(t)
			for _, name := range names {
				var button *widget.Button
				walkRendered(root, func(o fyne.CanvasObject) {
					if b, ok := o.(*widget.Button); ok && b.Text == name {
						button = b
					}
				})
				if button == nil {
					t.Fatalf("button %q missing", name)
				}
				button.Refresh()
				background := test.WidgetRenderer(button).Objects()[0].(*canvas.Rectangle)
				rest := background.FillColor
				driver.anims = nil
				button.MouseIn(&desktop.MouseEvent{})
				if len(driver.anims) != 1 || driver.anims[0].Duration != 300*time.Millisecond {
					t.Fatalf("%q must fade on hover", name)
				}
				enter := driver.anims[0]
				if !sameColor(background.FillColor, rest) {
					t.Fatalf("%q hover snapped before its first frame", name)
				}
				enter.Tick(.5)
				if sameColor(background.FillColor, rest) {
					t.Fatalf("%q hover did not interpolate", name)
				}
				enter.Tick(1)
				hover := background.FillColor
				button.MouseOut()
				if !sameColor(background.FillColor, hover) || len(driver.anims) != 2 {
					t.Fatalf("%q must fade out from its current colour", name)
				}
				driver.anims[1].Tick(1)
				if !sameColor(background.FillColor, rest) {
					t.Fatalf("%q did not retain its button role", name)
				}
				button.MouseIn(nil)
				pending := driver.anims[len(driver.anims)-1]
				button.Disable()
				disabled := background.FillColor
				pending.Tick(1)
				if !sameColor(background.FillColor, disabled) {
					t.Fatalf("%q stale hover repainted a disabled button", name)
				}
			}
		})
	}
}
