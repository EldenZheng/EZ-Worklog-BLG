package sprintcalendar

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func newPlannerDayClear(k *shell.KanbanUI, date string, day *shell.PlannerDayHover) *shell.PlannerDayClearButton {
	b := &shell.PlannerDayClearButton{Day: day}
	b.Text = "Delete"
	b.Icon = theme.DeleteIcon()
	b.OnTapped = func() { confirmClearDay(k, date) }
	b.ExtendBaseWidget(b)
	day.ClearFade = shell.SmallDeleteButton(b, &b.Button)
	day.ClearView = day.ClearFade.View
	return b
}

func confirmClearDay(k *shell.KanbanUI, date string) {
	// The day's Delete clears what the day shows: every visible issue's blocks,
	// whichever sprint the issue belongs to.
	_, count := backend.ClearPlannerDay(k.Plan, ScheduledItems(k), date)
	if count == 0 {
		return
	}
	ConfirmRemoval(k, "Delete "+date, fmt.Sprintf("Remove %d local time blocks from %s for the visible project issues?", count, date), "Delete", func() {
		p, _ := backend.ClearPlannerDay(k.Plan, ScheduledItems(k), date)
		if k.Save(p) {
			k.SetMessage("Local schedule cleared for " + date + ".")
			shell.Hooks.KanbanUIDraw(k)
		}
	})
}

// PlannerBlockDelete is the Delete on a block, shown while the pointer is on the
// block. Like the block's GH link it keeps the block, and its day, lit while the
// pointer is on it.
type PlannerBlockDelete struct {
	widget.Button
	face *PlannerTimedFace
}

func newPlannerBlockDelete(face *PlannerTimedFace) (*PlannerBlockDelete, *shell.SmallDelete) {
	b := &PlannerBlockDelete{face: face}
	b.Text = "Delete"
	b.Icon = theme.DeleteIcon()
	b.OnTapped = face.onDelete
	b.ExtendBaseWidget(b)
	return b, shell.SmallDeleteButton(b, &b.Button)
}

func (b *PlannerBlockDelete) MouseIn(e *desktop.MouseEvent) {
	if b.face.day != nil {
		b.face.day.Enter(b)
	}
	b.face.childHover(true)
	b.Button.MouseIn(e)
}

func (b *PlannerBlockDelete) MouseOut() {
	b.Button.MouseOut()
	b.face.childHover(false)
	if b.face.day != nil {
		b.face.day.Leave(b)
	}
}

// confirmDeleteBlock asks before removing one time block of an issue. The issue
// keeps its other blocks, estimate and notes.
func confirmDeleteBlock(k *shell.KanbanUI, c backend.SprintCandidate, index int) {
	blocks := k.Plan.Tasks[c.Key].Blocks
	if index < 0 || index >= len(blocks) {
		return
	}
	b := blocks[index]
	when := fmt.Sprintf("%s, %s–%s", b.Date, backend.PlanClock(b.Start), backend.PlanClock(b.Start+b.Minutes))
	ConfirmRemoval(k, "Delete time block", fmt.Sprintf("Remove %s (%s) from the calendar? Its other blocks, estimate and notes are kept; GitHub is unchanged.", c.Item.Title, when), "Delete", func() {
		p, err := backend.RemovePlannerBlock(k.Plan, c.Key, index, b)
		if err != nil {
			k.Ui.Errf(err)
			return
		}
		if k.Save(p) {
			k.SetMessage("Time block removed · saved locally.")
			shell.Hooks.KanbanUIDrawKeepingCalendarScroll(k)
		}
	})
}

// ConfirmClear asks before clearing time blocks, with a red Clear.
func ConfirmClear(k *shell.KanbanUI, title, message string, apply func()) {
	ConfirmRemoval(k, title, message, "Clear", apply)
}

// ConfirmRemoval is the confirmation for removing planned time: Sprint Calendar
// and Log Work Delete share red actions and gold rounded Cancel buttons. action
// names the red button.
func ConfirmRemoval(k *shell.KanbanUI, title, message, action string, apply func()) {
	body := widget.NewLabel(message)
	body.Wrapping = fyne.TextWrapWord
	var popup *widget.PopUp
	cancel := widget.NewButton("Cancel", func() { shell.ClosePopUp(popup) })
	confirm := widget.NewButton(action, func() { shell.ClosePopUp(popup); apply() })
	buttons := shell.RemovalButtons(cancel, confirm)
	content := shell.RemovalPopupContent(container.NewBorder(container.NewVBox(widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), body), buttons, nil, nil, nil))
	popup = widget.NewModalPopUp(content, k.Ui.Win.Canvas())
	size := k.Ui.RemovalPopupSize(content)
	popup.Resize(size)
	popup.Show()
	viewport := k.Ui.Win.Canvas().Size()
	popup.Move(fyne.NewPos((viewport.Width-size.Width)/2, (viewport.Height-size.Height)/2))
	shell.AnimateOpening(k.Ui.Win.Canvas())
}

// plannerDayBarBottom is the foot of a day's progress bar: the pointer above
// it brings the day's Delete in.
const plannerDayBarBottom = float32(108)

type plannerDayHeaderLayout struct{}

func (plannerDayHeaderLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(80, PlannerDayHeaderHeight)
}

// Objects: date, percentage, bar, Delete, reading, planned. Delete floats over
// the right of the date's line, drawn above it, so the date never moves for it.
func (plannerDayHeaderLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	del := objects[3].MinSize()
	objects[0].Move(fyne.Position{})
	objects[0].Resize(fyne.NewSize(fyne.Max(1, size.Width), 32))
	objects[1].Move(fyne.NewPos(4, 35))
	objects[1].Resize(fyne.NewSize(fyne.Max(1, size.Width-8), 16))
	objects[2].Move(fyne.NewPos(4, plannerDayBarBottom-6))
	objects[2].Resize(fyne.NewSize(fyne.Max(1, size.Width-8), 6))
	objects[3].Move(fyne.NewPos(fyne.Max(0, size.Width-del.Width-2), (32-del.Height)/2))
	objects[3].Resize(del)
	objects[4].Move(fyne.NewPos(4, 51))
	objects[4].Resize(fyne.NewSize(fyne.Max(1, size.Width-8), 28))
	objects[5].Move(fyne.NewPos(4, 83))
	objects[5].Resize(fyne.NewSize(fyne.Max(1, size.Width-8), 14))
}
