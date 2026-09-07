package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// hoverText is a wrapping label that lifts a floating tooltip while the mouse
// hovers it. Fyne 2.8 ships no tooltip widget, so this is the smallest one that
// does the job: a Hoverable that adds a PopUp overlay to the window canvas on
// enter and drops it on leave.
//
// The label itself is what shows in place; tip is the string the popup carries.
// Passing an empty tip disables the hover entirely, so callers do not have to
// guard the constructor.
type hoverText struct {
	widget.BaseWidget
	label   *widget.Label
	tip     string
	win     fyne.Window
	tooltip *widget.PopUp
}

func newHoverText(win fyne.Window, text, tip string, style fyne.TextStyle) *hoverText {
	label := widget.NewLabelWithStyle(text, fyne.TextAlignLeading, style)
	label.Wrapping = fyne.TextWrapWord
	h := &hoverText{label: label, tip: tip, win: win}
	h.ExtendBaseWidget(h)
	return h
}

func (h *hoverText) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(h.label)
}

func (h *hoverText) SetText(s string) { h.label.SetText(s) }
func (h *hoverText) SetTip(s string)  { h.tip = s }

func (h *hoverText) MouseIn(*desktop.MouseEvent) {
	if h.tip == "" || h.win == nil || h.win.Canvas() == nil {
		return
	}
	// PopUp already paints a themed background under whatever content it holds,
	// so a hand-rolled background rectangle here fought that one for pixels and
	// only half-covered the label. Use PopUp's own background by handing it the
	// label directly, and force a minimum width via a floor rectangle so the
	// wrapped title has room to wrap rather than being drawn as one long line
	// that the popup could not measure.
	body := widget.NewLabel(h.tip)
	body.Wrapping = fyne.TextWrapWord
	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(320, 0))
	content := container.NewStack(floor, container.NewPadded(body))
	h.tooltip = widget.NewPopUp(content, h.win.Canvas())
	// Place the tip just below the label so it does not sit under the cursor
	// and steal further hover events.
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(h)
	h.tooltip.ShowAtPosition(fyne.NewPos(pos.X, pos.Y+h.Size().Height+4))
}

func (h *hoverText) MouseMoved(*desktop.MouseEvent) {}

func (h *hoverText) MouseOut() {
	if h.tooltip != nil {
		h.tooltip.Hide()
		h.tooltip = nil
	}
}

func (h *hoverText) Cursor() desktop.Cursor { return desktop.DefaultCursor }
