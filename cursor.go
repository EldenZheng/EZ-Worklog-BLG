package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
)

// This transparent surface supplies only the cursor. The existing control
// still receives hover, click, focus and keyboard events unchanged.
type pointerRegion struct {
	canvas.Rectangle
	disabled func() bool
}

func (p *pointerRegion) Cursor() desktop.Cursor {
	if p.disabled() {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

func withPointerCursor(control interface {
	fyne.CanvasObject
	Disabled() bool
}) fyne.CanvasObject {
	region := &pointerRegion{disabled: control.Disabled}
	region.FillColor = color.Transparent
	return container.NewStack(control, region)
}
