package shell

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// goldTextTheme paints text in the logo's gold.
type goldTextTheme struct{ fyne.Theme }

func (t goldTextTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground {
		return widgets.LogoGold
	}
	return t.Theme.Color(name, variant)
}

// SupportView is the weekday support checkbox or, in the user's own support
// week, a bold gold "Support Week" in its place: the rota already puts the
// shift on every weekday, so there is nothing to tick (the +180 is pencilled
// on by itself; see SupportPlaceholderFor). Show View.
type SupportView struct {
	View  *fyne.Container
	check fyne.CanvasObject
	label fyne.CanvasObject
}

// SupportChoice wraps the support checkbox like GoldChoice does, swapped for
// the "Support Week" label while rota is set.
func SupportChoice(check *widget.Check, rota bool) *SupportView {
	label := widget.NewLabelWithStyle(SupportCheckText(true), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	v := &SupportView{
		check: GoldChoice(check),
		label: container.NewThemeOverride(label, goldTextTheme{Theme: theme.Current()}),
	}
	v.View = container.NewStack(v.check, v.label)
	v.Set(rota)
	return v
}

// Set shows the label in a support week and the checkbox in any other.
func (v *SupportView) Set(rota bool) {
	ShowIf(v.check, !rota)
	ShowIf(v.label, rota)
	v.View.Refresh()
}

// Rota says whether the label is showing.
func (v *SupportView) Rota() bool { return v.label.Visible() }
