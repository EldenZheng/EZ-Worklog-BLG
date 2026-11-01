package widgets

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type StatusHeaderTheme struct {
	fyne.Theme
	Ink color.Color
}

func (t StatusHeaderTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground {
		return t.Ink
	}
	return t.Theme.Color(name, v)
}

var LogoGold = color.NRGBA{R: 0xcd, G: 0xa2, B: 0x36, A: 0xff}

// WithAlpha re-issues a colour at a fraction of full opacity.
func WithAlpha(c color.Color, f float32) color.NRGBA {
	r, g, b, _ := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(f * 255)}
}
