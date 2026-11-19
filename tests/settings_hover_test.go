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
)

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
