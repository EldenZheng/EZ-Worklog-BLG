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

// PlannedLine is one block in Log Work's planned tooltip: its repo#number (or meeting description)
// in brackets, then its title and minutes.
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
		if l.Detail != "" {
			ink := l.DetailColor
			if ink == nil {
				ink = fg
			}
			row.Add(text("(", fg, false))
			row.Add(text(Truncate(l.Detail, 40), ink, false))
			row.Add(text(") ", fg, false))
		}
		row.Add(text(Truncate(l.Title, 60), widgets.LogoGold, true))
		row.Add(text(fmt.Sprintf(" · %dm", l.Minutes), fg, false))
		rows.Add(row)
	}
	return rows
}

// PlannedTipWithFree is PlannedTipBody over a centred purple line saying what
// the day still has free once its plans are counted. A full day has nothing to
// say.
func PlannedTipWithFree(free int, lines []PlannedLine) fyne.CanvasObject {
	body := PlannedTipBody(lines)
	if free <= 0 {
		return body
	}
	left := canvas.NewText(fmt.Sprintf("%dm left without planned", free), PlannerPlanColor)
	left.Alignment = fyne.TextAlignCenter
	return container.NewVBox(body, left)
}
