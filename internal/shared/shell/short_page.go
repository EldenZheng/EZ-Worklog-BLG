package shell

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// ShortPage preserves the fixed summary on tall windows. When the remaining
// list would be too short to use, the entire page can scroll instead.
func ShortPage(top, body fyne.CanvasObject, minimumBodyHeight float32) *container.Scroll {
	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(0, minimumBodyHeight))
	page := container.NewBorder(top, nil, nil, nil, container.NewStack(floor, body))
	return container.NewVScroll(page)
}
