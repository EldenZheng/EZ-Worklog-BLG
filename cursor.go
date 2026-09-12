package main

import (
	"image"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
)

// Fyne has no standard grab cursor. This cached open hand uses a white fill
// and black outline so it stays visible over every organisation colour.
type grabCursor struct{}

var grabImage = makeGrabImage()

func (grabCursor) Image() (image.Image, int, int) { return grabImage, 12, 12 }

func makeGrabImage() image.Image {
	mask := []string{
		"           XX           ",
		"       XX X..X XX       ",
		"      X..XX..XX..X      ",
		"      X..XX..XX..X XX   ",
		"      X..XX..XX..XX..X  ",
		"      X..XX..XX..XX..X  ",
		"      X..XX..XX..XX..X  ",
		"      X..XX..XX..XX..X  ",
		"  XX  X.............X  ",
		" X..X X.............X  ",
		" X...XX.............X  ",
		"  X.................X  ",
		"   X................X  ",
		"    X...............X  ",
		"     X.............X   ",
		"      X............X   ",
		"       X..........X    ",
		"       X..........X    ",
		"       XXXXXXXXXXXX    ",
	}
	img := image.NewNRGBA(image.Rect(0, 0, 24, 24))
	for y, row := range mask {
		for x, pixel := range row {
			switch pixel {
			case 'X':
				img.Set(x, y+2, color.Black)
			case '.':
				img.Set(x, y+2, color.White)
			}
		}
	}
	return img
}

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
