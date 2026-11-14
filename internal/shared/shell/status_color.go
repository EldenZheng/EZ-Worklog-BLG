package shell

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// StatusHue is the colour a board Status is drawn in, as the Kanban's columns
// are: Created grey, In Progress gold, In Review purple, Done green and so on.
// ok is false for a status the Kanban has no colour for.
func StatusHue(name string) (hue color.Color, ok bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "created":
		return OrgUnknown, true
	case "analysis - investigation":
		return color.NRGBA{R: 22, G: 52, B: 88, A: 255}, true
	case "in progress":
		return color.NRGBA{R: 220, G: 183, B: 40, A: 255}, true
	case "pending customer feedback":
		return color.NRGBA{R: 58, G: 30, B: 82, A: 255}, true
	case "pending test":
		return color.NRGBA{R: 202, G: 104, B: 31, A: 255}, true
	case "server update required":
		return color.NRGBA{R: 205, G: 112, B: 161, A: 255}, true
	case "in review":
		return color.NRGBA{R: 126, G: 76, B: 177, A: 255}, true
	case "done":
		return color.NRGBA{R: 42, G: 127, B: 74, A: 255}, true
	}
	return nil, false
}

// TintTheme paints a field (a drop-down, an entry) washed with Hue: its
// background at rest and, a little stronger, under the pointer.
type TintTheme struct {
	fyne.Theme
	Hue color.Color
}

func (t TintTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameInputBackground:
		return BlendColor(t.Theme.Color(name, v), t.Hue, 0.32)
	case theme.ColorNameHover:
		return BlendColor(t.Theme.Color(theme.ColorNameInputBackground, v), t.Hue, 0.46)
	}
	return t.Theme.Color(name, v)
}
