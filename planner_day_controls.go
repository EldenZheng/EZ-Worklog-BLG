package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Share hover ownership across the date, its bubbles, links and Clear button.
type plannerDayHover struct {
	widget.BaseWidget
	body     fyne.CanvasObject
	clear    *plannerDayClearButton
	owner    fyne.CanvasObject
	canClear bool
	header   *fyne.Container
	view     fyne.Canvas
}

func (h *plannerDayHover) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(h.body)
}
func (h *plannerDayHover) enter(owner fyne.CanvasObject) {
	h.owner = owner
	if h.canClear {
		h.showClear(true)
	}
}
func (h *plannerDayHover) leave(owner fyne.CanvasObject) {
	if h.owner == owner {
		h.owner = nil
		h.showClear(false)
	}
}
func (h *plannerDayHover) showClear(show bool) {
	if h.clear.Visible() == show {
		return
	}
	if show {
		h.clear.Show()
	} else {
		h.clear.Hide()
	}
	if h.header != nil {
		h.header.Refresh()
		// Initially hidden widgets have not been associated with the native
		// canvas yet. Refresh their visible parent directly, without a click.
		if h.view != nil {
			h.view.Refresh(h.header)
		}
	}
}
func (h *plannerDayHover) MouseIn(*desktop.MouseEvent)    { h.enter(h) }
func (h *plannerDayHover) MouseMoved(*desktop.MouseEvent) { h.enter(h) }
func (h *plannerDayHover) MouseOut()                      { h.leave(h) }

type plannerDayClearButton struct {
	widget.Button
	day *plannerDayHover
}

func (b *plannerDayClearButton) MouseIn(e *desktop.MouseEvent) { b.day.enter(b); b.Button.MouseIn(e) }
func (b *plannerDayClearButton) MouseMoved(e *desktop.MouseEvent) {
	b.day.enter(b)
	b.Button.MouseMoved(e)
}
func (b *plannerDayClearButton) MouseOut() { b.Button.MouseOut(); b.day.leave(b) }
func newPlannerDayClear(k *kanbanUI, date string, day *plannerDayHover) *plannerDayClearButton {
	b := &plannerDayClearButton{day: day}
	b.Icon = theme.DeleteIcon()
	b.Importance = widget.LowImportance
	b.OnTapped = func() { k.confirmClearDay(date) }
	b.ExtendBaseWidget(b)
	b.Hide()
	return b
}
func (k *kanbanUI) confirmClearDay(date string) {
	// The day's bin clears what the day shows: every visible issue's blocks,
	// whichever sprint the issue belongs to.
	_, count := clearPlannerDay(k.plan, k.scheduledItems(), date)
	if count == 0 {
		return
	}
	k.confirmClear("Clear "+date, fmt.Sprintf("Remove %d local time blocks from %s for the visible project issues?", count, date), func() {
		p, _ := clearPlannerDay(k.plan, k.scheduledItems(), date)
		if k.save(p) {
			k.setMessage("Local schedule cleared for " + date + ".")
			k.draw()
		}
	})
}

// Clear and Log Work Delete share red actions and gold rounded Cancel buttons.
func (k *kanbanUI) confirmClear(title, message string, apply func()) {
	body := widget.NewLabel(message)
	body.Wrapping = fyne.TextWrapWord
	var popup *widget.PopUp
	cancel := widget.NewButton("Cancel", func() { popup.Hide() })
	confirm := widget.NewButton("Clear", func() { popup.Hide(); apply() })
	buttons := removalButtons(cancel, confirm)
	content := removalPopupContent(container.NewBorder(container.NewVBox(widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), body), buttons, nil, nil, nil))
	popup = widget.NewModalPopUp(content, k.ui.win.Canvas())
	size := k.ui.removalPopupSize(content)
	popup.Resize(size)
	popup.Show()
	viewport := k.ui.win.Canvas().Size()
	popup.Move(fyne.NewPos((viewport.Width-size.Width)/2, (viewport.Height-size.Height)/2))
}

type plannerDayHeaderLayout struct{}

func (plannerDayHeaderLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(80, plannerDayHeaderHeight)
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
