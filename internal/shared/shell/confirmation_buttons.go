package shell

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// Explicit action colours keep confirmations independent of the system's
// primary colour and light/dark neutral button styling.
type removalButtonTheme struct {
	fyne.Theme
	destructive bool
}

func (t removalButtonTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		if t.destructive {
			return t.Theme.Color(theme.ColorNameError, variant)
		}
		return widgets.LogoGold
	case theme.ColorNameForegroundOnPrimary:
		if t.destructive {
			return color.White
		}
		return color.NRGBA{R: 0x1c, G: 0x18, B: 0x0d, A: 255}
	}
	return t.Theme.Color(name, variant)
}

func (t removalButtonTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameButtonRadius {
		if t.destructive {
			return 100
		}
		return 8
	}
	return t.Theme.Size(name)
}

func removalButton(button *widget.Button, destructive bool) fyne.CanvasObject {
	button.Importance = widget.HighImportance
	return container.NewThemeOverride(widgets.WithPointerCursor(button), removalButtonTheme{Theme: theme.Current(), destructive: destructive})
}

// RemovalButtons is the shared Cancel / destructive row for confirmations.
func RemovalButtons(cancel, confirm *widget.Button) *fyne.Container {
	return DialogButtons(removalButton(cancel, false), removalButton(confirm, true))
}
