package sprintcalendar

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func newPlannerDayClear(k *shell.KanbanUI, date string, day *shell.PlannerDayHover) *shell.PlannerDayClearButton {
	b := &shell.PlannerDayClearButton{Day: day}
	b.Icon = theme.DeleteIcon()
	b.Importance = widget.LowImportance
	b.OnTapped = func() { confirmClearDay(k, date) }
	b.ExtendBaseWidget(b)
	b.Hide()
	return b
}

func confirmClearDay(k *shell.KanbanUI, date string) {
	// The day's bin clears what the day shows: every visible issue's blocks,
	// whichever sprint the issue belongs to.
	_, count := backend.ClearPlannerDay(k.Plan, ScheduledItems(k), date)
	if count == 0 {
		return
	}
	ConfirmClear(k, "Clear "+date, fmt.Sprintf("Remove %d local time blocks from %s for the visible project issues?", count, date), func() {
		p, _ := backend.ClearPlannerDay(k.Plan, ScheduledItems(k), date)
		if k.Save(p) {
			k.SetMessage("Local schedule cleared for " + date + ".")
			shell.Hooks.KanbanUIDraw(k)
		}
	})
}

// Clear and Log Work Delete share red actions and gold rounded Cancel buttons.
func ConfirmClear(k *shell.KanbanUI, title, message string, apply func()) {
	body := widget.NewLabel(message)
	body.Wrapping = fyne.TextWrapWord
	var popup *widget.PopUp
	cancel := widget.NewButton("Cancel", func() { popup.Hide() })
	confirm := widget.NewButton("Clear", func() { popup.Hide(); apply() })
	buttons := shell.RemovalButtons(cancel, confirm)
	content := shell.RemovalPopupContent(container.NewBorder(container.NewVBox(widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), body), buttons, nil, nil, nil))
	popup = widget.NewModalPopUp(content, k.Ui.Win.Canvas())
	size := k.Ui.RemovalPopupSize(content)
	popup.Resize(size)
	popup.Show()
	viewport := k.Ui.Win.Canvas().Size()
	popup.Move(fyne.NewPos((viewport.Width-size.Width)/2, (viewport.Height-size.Height)/2))
}

type plannerDayHeaderLayout struct{}

func (plannerDayHeaderLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(80, PlannerDayHeaderHeight)
}

func (plannerDayHeaderLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	width := size.Width
	if objects[3].Visible() {
		width -= 28
	}
	objects[0].Move(fyne.Position{})
	objects[0].Resize(fyne.NewSize(width, 32))
	objects[1].Move(fyne.NewPos(4, 35))
	objects[1].Resize(fyne.NewSize(fyne.Max(1, size.Width-8), 16))
	objects[2].Move(fyne.NewPos(4, 102))
	objects[2].Resize(fyne.NewSize(fyne.Max(1, size.Width-8), 6))
	objects[3].Move(fyne.NewPos(fyne.Max(0, size.Width-28), 2))
	objects[3].Resize(fyne.NewSize(26, 26))
	objects[4].Move(fyne.NewPos(4, 51))
	objects[4].Resize(fyne.NewSize(fyne.Max(1, size.Width-8), 28))
	objects[5].Move(fyne.NewPos(4, 83))
	objects[5].Resize(fyne.NewSize(fyne.Max(1, size.Width-8), 14))
}
