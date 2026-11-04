package shell

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"worklog/internal/shared/widgets"
)

// GoldSection uses the Report panel shape: rounded surface and a header capsule.
func GoldSection(title string, action, content fyne.CanvasObject) fyne.CanvasObject {
	base := theme.Color(theme.ColorNameBackground)
	bg := canvas.NewRectangle(BlendColor(base, widgets.LogoGold, 0.18))
	bg.CornerRadius, bg.StrokeWidth = 10, 1
	bg.StrokeColor = BlendColor(base, widgets.LogoGold, 0.70)
	band := canvas.NewRectangle(BlendColor(base, widgets.LogoGold, 0.34))
	band.CornerRadius = 6
	head := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	heading := container.NewThemeOverride(head, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	return container.NewStack(bg, container.NewPadded(container.NewVBox(
		container.NewStack(band, container.NewBorder(nil, nil, nil, action, heading)), container.NewPadded(content))))
}
