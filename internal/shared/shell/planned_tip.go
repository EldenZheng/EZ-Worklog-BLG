package shell

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/shared/widgets"
)

// PlannedLine is one block in Log Work's planned tooltip: its title, then
// in brackets its repo#number (or a meeting's description) in Detail's
// colour, then its minutes.
type PlannedLine struct {
	Title, Detail string
	DetailColor   color.Color
	Minutes       int
}

// PlannedTipBody draws lines for the tooltip: the title in gold, the detail
// in its own colour. Several lines read as bullet points.
func PlannedTipBody(lines []PlannedLine) fyne.CanvasObject {
	text := func(s string, ink color.Color, bold bool) *canvas.Text {
		t := canvas.NewText(s, ink)
		t.TextStyle.Bold = bold
		return t
	}
	fg := theme.Color(theme.ColorNameForeground)
	rows := container.NewVBox()
	for _, l := range lines {
		row := container.New(layout.NewCustomPaddedHBoxLayout(0))
		if len(lines) > 1 {
			row.Add(text("• ", fg, false))
		}
		row.Add(text(Truncate(l.Title, 60), widgets.LogoGold, true))
		if l.Detail != "" {
			ink := l.DetailColor
			if ink == nil {
				ink = fg
			}
			row.Add(text(" (", fg, false))
			row.Add(text(Truncate(l.Detail, 40), ink, false))
			row.Add(text(")", fg, false))
		}
		row.Add(text(fmt.Sprintf(" · %dm", l.Minutes), fg, false))
		rows.Add(row)
	}
	return rows
}
