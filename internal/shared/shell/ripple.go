package shell

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// A click on anything the gold hover travels over (a filter, a sidebar row, a
// list row, a check) ripples gold from where it landed, so the choice just made
// shows. The ripple plays on the window's top layer (see FitScreen): the click
// usually redraws what was clicked, and a ripple drawn inside it would be cut
// short with it.

func init() { widgets.RippleAt = RippleIn }

// RippleTime is how long a ripple takes to spread and fade.
const RippleTime = 600 * time.Millisecond

// RippleOnPage ripples area from at (window coordinates) on the window's top
// layer. Nothing plays while a popup is open: the layer lies under it.
func RippleOnPage(area fyne.CanvasObject, at fyne.Position) {
	RippleBeside(area, 0, at)
}

// prepareRippleOnPage is RippleOnPage measured now and started later (see
// prepareRipple), or nil when nothing would play.
func prepareRippleOnPage(area fyne.CanvasObject, clearLeft float32, at fyne.Position) func() {
	if area == nil || fyne.CurrentApp() == nil {
		return nil
	}
	c := fyne.CurrentApp().Driver().CanvasForObject(area)
	if c == nil || c.Overlays().Top() != nil {
		return nil
	}
	return prepareRippleBeside(closingLayers[c], area, clearLeft, at)
}

// RippleBeside is RippleOnPage keeping clear of the leftmost clearLeft of
// area, where a check box draws its tick: the ripple spreads beside it.
func RippleBeside(area fyne.CanvasObject, clearLeft float32, at fyne.Position) {
	if start := prepareRippleOnPage(area, clearLeft, at); start != nil {
		start()
	}
}

// RippleIn is RippleOnPage over only the part of area at pos (in area) of
// size, such as a check's hover pill beside its box.
func RippleIn(area fyne.CanvasObject, pos fyne.Position, size fyne.Size, at fyne.Position) {
	if area == nil || fyne.CurrentApp() == nil {
		return
	}
	c := fyne.CurrentApp().Driver().CanvasForObject(area)
	if c == nil || c.Overlays().Top() != nil {
		return
	}
	if start := prepareRippleIn(closingLayers[c], area, pos, size, at); start != nil {
		start()
	}
}

// RippleClearOf is how much of area's left a ripple keeps clear of so as to
// stay off box: up to its right edge, with a little room.
func RippleClearOf(area, box fyne.CanvasObject) float32 {
	if area == nil || box == nil {
		return 0
	}
	driver := fyne.CurrentApp().Driver()
	right := driver.AbsolutePositionForObject(box).X + box.Size().Width - driver.AbsolutePositionForObject(area).X
	return max(0, right+theme.Padding())
}

// ripples says a tap on t should ripple: it does something, and it is one of
// the gold hover's targets (a filter, a row under a travelling highlight, a
// card edged in gold under the pointer) or asked for it.
func (t *Tappable) ripples() bool {
	return t.onTap != nil && t.OnTapAt == nil && (t.Ripple || t.FadeOnTap || t.filterHover != nil || t.Feedback != nil)
}

// rippleClip shows only the part of its circle inside its own bounds. It is a
// clip by its renderer alone, not a Scrollable, so the wheel still scrolls the
// page under a ripple.
type rippleClip struct {
	widget.BaseWidget
	circle *canvas.Circle
}

func newRippleClip(circle *canvas.Circle) *rippleClip {
	c := &rippleClip{circle: circle}
	c.ExtendBaseWidget(c)
	return c
}

func (c *rippleClip) CreateRenderer() fyne.WidgetRenderer { return &rippleClipRenderer{c: c} }

type rippleClipRenderer struct{ c *rippleClip }

func (r *rippleClipRenderer) Layout(fyne.Size)             {}
func (r *rippleClipRenderer) MinSize() fyne.Size           { return fyne.Size{} }
func (r *rippleClipRenderer) Refresh()                     { canvas.Refresh(r.c.circle) }
func (r *rippleClipRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.c.circle} }
func (r *rippleClipRenderer) Destroy()                     {}

// IsClip tells Fyne's painter to clip the circle to the box.
func (r *rippleClipRenderer) IsClip() {}
