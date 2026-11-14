package shell

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"worklog/internal/shared/widgets"
)

// Keep a picture of just the tapped filter while its action redraws the real
// button underneath. Fading the picture blends its previous and new states
// without fading the page or delaying the action.
type filterFade struct {
	canvas    fyne.Canvas
	layer     *fyne.Container
	picture   *canvas.Image
	animation *fyne.Animation
	size      fyne.Size
}

var filterFades = map[fyne.Canvas]*filterFade{}

// A redraw creates new filter widgets under a pointer that has not moved.
// Find the replacement in the live layout: the desktop driver's canvas cache
// does not know about the new widgets until its next paint.
func keepFilterHover(button *Tappable, e *fyne.PointEvent) func() {
	if button.Feedback == nil || !button.Feedback.hovered {
		return func() {}
	}
	driver := fyne.CurrentApp().Driver()
	c := driver.CanvasForObject(button)
	if c == nil {
		return func() {}
	}
	at := driver.AbsolutePositionForObject(button).Add(fyne.NewPos(button.Size().Width/2, button.Size().Height/2))
	if e != nil && pointIn(button, e.AbsolutePosition) {
		at = e.AbsolutePosition
	}
	return func() {
		if c.Overlays().Top() != nil {
			return
		}
		if next := filterAt(c.Content(), at, fyne.Position{}); next != nil && next != button {
			next.Feedback.stop()
			next.Feedback.hovered = true
			next.Feedback.paint(0.24)
			if next.filterHover != nil {
				next.filterHover.restore(next)
			}
			button.filterReplacement = next
		}
	}
}

// filterAt follows the app's filter hosts without relying on rendered-widget
// cache entries. Positions already include scrolling offsets after layout.
func filterAt(obj fyne.CanvasObject, at, parent fyne.Position) *Tappable {
	if obj == nil || !obj.Visible() {
		return nil
	}
	pos := parent.Add(obj.Position())
	inside := at.X >= pos.X && at.Y >= pos.Y && at.X < pos.X+obj.Size().Width && at.Y < pos.Y+obj.Size().Height
	var children []fyne.CanvasObject
	switch o := obj.(type) {
	case *Tappable:
		if o.FadeOnTap && inside {
			return o
		}
		children = []fyne.CanvasObject{o.Content}
	case *fyne.Container:
		children = o.Objects
	case *container.Scroll:
		if !inside {
			return nil
		}
		children = []fyne.CanvasObject{o.Content}
	case *PlannerFastScroll:
		if !inside {
			return nil
		}
		children = []fyne.CanvasObject{o.Content}
	case *container.ThemeOverride:
		children = []fyne.CanvasObject{o.Content}
	case *pillTabs:
		children = []fyne.CanvasObject{o.body}
	case *ProjectChip:
		children = []fyne.CanvasObject{o.Chip}
	}
	for i := len(children) - 1; i >= 0; i-- {
		if next := filterAt(children[i], at, pos); next != nil {
			return next
		}
	}
	return nil
}

func beginFilterFade(button fyne.CanvasObject) *filterFade {
	driver := fyne.CurrentApp().Driver()
	c := driver.CanvasForObject(button)
	if c == nil {
		return nil
	}
	previous := filterFades[c]
	if previous != nil {
		defer previous.stop()
	}
	if !animationsOn() {
		return nil
	}
	layer := closingLayers[c]
	if layer == nil || c.Overlays().Top() != nil {
		return nil
	}
	pos := driver.AbsolutePositionForObject(button)
	end := pos.Add(button.Size())
	pos = fyne.NewPos(max(0, pos.X), max(0, pos.Y))
	size := fyne.NewSize(min(c.Size().Width, end.X)-pos.X, min(c.Size().Height, end.Y)-pos.Y)
	if size.Width <= 0 || size.Height <= 0 {
		return nil
	}
	picture := captureArea(c, pos, size)
	if picture == nil {
		return nil
	}
	ghost := canvas.NewImageFromImage(picture)
	ghost.FillMode = canvas.ImageFillStretch
	ghost.Move(pos)
	ghost.Resize(size)
	return &filterFade{canvas: c, layer: layer, picture: ghost, size: c.Size()}
}

func (f *filterFade) play() {
	if f == nil {
		return
	}
	filterFades[f.canvas] = f
	f.layer.Add(f.picture)
	f.animation = fyne.NewAnimation(widgets.HoverDuration, func(done float32) {
		if filterFades[f.canvas] != f {
			return
		}
		if done >= 1 || f.canvas.Size() != f.size {
			f.stop()
			return
		}
		f.picture.Translucency = float64(done)
		f.picture.Refresh()
	})
	f.animation.Curve = EaseInOutCubic
	f.animation.Start()
}

func (f *filterFade) stop() {
	if f.animation != nil {
		f.animation.Stop()
	}
	f.layer.Remove(f.picture)
	if filterFades[f.canvas] == f {
		delete(filterFades, f.canvas)
	}
}

func stopFilterFade(c fyne.Canvas) {
	if f := filterFades[c]; f != nil {
		f.stop()
	}
}
