package shell

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// supportCheckTheme is GoldChoice's theme, with the label turning gold while
// *gold is set: the week shown is the user's support week.
type supportCheckTheme struct {
	fyne.Theme
	gold *bool
}

func (t supportCheckTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground && *t.gold {
		return widgets.LogoGold
	}
	return t.Theme.Color(name, variant)
}

// SupportChoice shows the weekday support checkbox like GoldChoice does, its
// label in gold whenever *gold is set. Refresh the checkbox after changing it.
func SupportChoice(check *widget.Check, gold *bool) fyne.CanvasObject {
	return container.NewThemeOverride(widgets.AnimatedChoice(check),
		supportCheckTheme{Theme: RadioTheme{Theme: theme.Current()}, gold: gold})
}
