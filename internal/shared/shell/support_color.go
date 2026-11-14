package shell

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// SupportPendingKey keeps pencilled-in support separate from local drafts.
const SupportPendingKey = "@support"

// WeekdaySupportColor identifies the +180m weekday shift throughout the app.
func WeekdaySupportColor() color.NRGBA {
	if DarkSurface(theme.Current(), fyne.CurrentApp().Settings().ThemeVariant()) {
		return color.NRGBA{R: 0x42, G: 0xd6, B: 0xc4, A: 0xff}
	}
	return color.NRGBA{R: 0x00, G: 0x80, B: 0x76, A: 0xff}
}

func SupportFill(tint float32) color.NRGBA {
	return BlendColor(theme.Color(theme.ColorNameBackground), WeekdaySupportColor(), tint)
}
