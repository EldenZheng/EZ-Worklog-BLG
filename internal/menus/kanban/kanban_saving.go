package kanban

import (
	"fmt"
	"image/color"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// PendingStatus is the column a card is drawn in: where it is being moved
// while that save runs, otherwise its board status.
func PendingStatus(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem) string {
	if p, ok := k.Pending[it.Key(b.ID)]; ok && p.Kind == "status" {
		return p.Value
	}
	return it.Status
}

func savingText(p shell.KanbanPending) string {
	if p.Kind == "position" {
		return "Saving order…"
	}
	if p.Kind == "status" {
		return "Saving to " + p.Label + "…"
	}
	return "Moving to " + p.Label + "…"
}

// SavingOverlay dims a card and states the save in flight on top of it.
func SavingOverlay(card fyne.CanvasObject, p shell.KanbanPending) fyne.CanvasObject {
	r, g, b, _ := theme.Color(theme.ColorNameBackground).RGBA()
	shade := canvas.NewRectangle(color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 0xa8})
	shade.CornerRadius = 10
	shade.StrokeColor = widgets.LogoGold
	shade.StrokeWidth = 2
	// Gold like the outline, and wrapped to the card's width: the label sits
	// in a VBox, which hands it the full width, rather than in a Center, which
	// would let a long sprint name run past the card's edges.
	text := widget.NewLabelWithStyle(savingText(p), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	text.Wrapping = fyne.TextWrapWord
	gold := container.NewThemeOverride(text, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	return container.NewStack(card, shade, container.NewPadded(container.NewVBox(layout.NewSpacer(), gold, layout.NewSpacer())))
}

// moveResult is the line shown once a change has been saved or has failed.
func moveResult(it backend.SprintItem, p shell.KanbanPending, err error) string {
	if err != nil {
		return fmt.Sprintf("GitHub update failed for %q: %v", it.Title, err)
	}
	if p.Kind == "status" {
		return fmt.Sprintf("Saved %q: status is now %s.", it.Title, p.Label)
	}
	return fmt.Sprintf("Saved %q: sprint is now %s.", it.Title, p.Label)
}
