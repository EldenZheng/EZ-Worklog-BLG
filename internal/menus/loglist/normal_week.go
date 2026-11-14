package loglist

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

const normalWeekMinutes = 40 * 60

// normalWeekProgress scores the week against 40 hours, in sections: what is on
// the board, then with drafts and support, then with plans as well.
func normalWeekProgress(logged, waiting, planned int) []shell.TextSection {
	left := max(normalWeekMinutes-logged, 0)
	sections := []shell.TextSection{{Text: fmt.Sprintf("Normal work week (40h): %d%% logged · %d%% remaining (%s)",
		logged*100/normalWeekMinutes, (left*100+normalWeekMinutes-1)/normalWeekMinutes, backend.HoursMins(left)),
		Ink: theme.Color(theme.ColorNameForeground)}}
	if waiting > 0 {
		sections = append(sections, shell.TextSection{Text: weekProjection("waiting", waiting, logged+waiting, normalWeekMinutes),
			Ink: theme.Color(theme.ColorNameWarning)})
	}
	if planned > 0 {
		sections = append(sections, shell.TextSection{Text: weekProjection("planned", planned, logged+waiting+planned, normalWeekMinutes),
			Ink: shell.PlannerPlanColor})
	}
	return sections
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
	return fyne.NewSize(objects[2].MinSize().Width, objects[2].MinSize().Height+14)
}

func (l normalWeekMeterLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	labelSize := objects[2].MinSize()
	x := size.Width * l.fraction
	y := labelSize.Height + 2
	objects[0].Move(fyne.NewPos(0, y))
	objects[0].Resize(fyne.NewSize(size.Width, 10))
	objects[1].Move(fyne.NewPos(max(min(x-1, size.Width-2), 0), y-2))
	objects[1].Resize(fyne.NewSize(2, 14))
	objects[2].Move(fyne.NewPos(max(min(x-labelSize.Width/2, size.Width-labelSize.Width), 0), 0))
	objects[2].Resize(labelSize)
}
