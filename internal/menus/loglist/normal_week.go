package loglist

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/widgets"
)

const normalWeekMinutes = 40 * 60

func normalWeekProgress(logged, waiting int) string {
	left := max(normalWeekMinutes-logged, 0)
	line := fmt.Sprintf("Normal work week (40h): %d%% logged · %d%% remaining (%s)",
		logged*100/normalWeekMinutes, (left*100+normalWeekMinutes-1)/normalWeekMinutes, backend.HoursMins(left))
	if waiting > 0 {
		line += fmt.Sprintf(" · %d%% with drafts/support", (logged+waiting)*100/normalWeekMinutes)
	}
	return line
}

func normalWeekMarker(bar fyne.CanvasObject, scale int) fyne.CanvasObject {
	mark := canvas.NewRectangle(widgets.LogoGold)
	label := canvas.NewText("40h", widgets.LogoGold)
	label.TextSize = theme.CaptionTextSize()
	label.TextStyle.Bold = true
	return container.New(normalWeekMeterLayout{fraction: float32(normalWeekMinutes) / float32(max(scale, normalWeekMinutes))}, bar, mark, label)
}

// Keep the marker aligned with the meter's scale, including over-target weeks.
type normalWeekMeterLayout struct{ fraction float32 }

func (l normalWeekMeterLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(objects[2].MinSize().Width, objects[2].MinSize().Height+18)
}

func (l normalWeekMeterLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	labelSize := objects[2].MinSize()
	x := size.Width * l.fraction
	y := labelSize.Height + 4
	objects[0].Move(fyne.NewPos(0, y))
	objects[0].Resize(fyne.NewSize(size.Width, 10))
	objects[1].Move(fyne.NewPos(max(min(x-1, size.Width-2), 0), y-2))
	objects[1].Resize(fyne.NewSize(2, 14))
	objects[2].Move(fyne.NewPos(max(min(x-labelSize.Width/2, size.Width-labelSize.Width), 0), 0))
	objects[2].Resize(labelSize)
}
