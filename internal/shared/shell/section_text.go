package shell

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
)

// TextSection is one coloured run of a line of sections.
type TextSection struct {
	Text string
	Ink  color.Color
}

// SectionTexts draws sections dotted apart, as the children of a
// widgets.WrapRowLayout row, which moves a section that does not fit to the next
// line whole.
func SectionTexts(style fyne.TextStyle, sections []TextSection) []fyne.CanvasObject {
	var out []fyne.CanvasObject
	add := func(s string, ink color.Color) {
		t := canvas.NewText(s, ink)
		t.TextStyle = style
		out = append(out, t)
	}
	for i, s := range sections {
		if i > 0 {
			add("  ·  ", theme.Color(theme.ColorNameForeground))
		}
		add(s.Text, s.Ink)
	}
	return out
}
