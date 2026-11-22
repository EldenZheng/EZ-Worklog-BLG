package shell

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/shared/widgets"
)

// A click on anything the gold hover travels over (a filter, a sidebar row, a
// list row, a check) ripples gold from where it landed, so the choice just made
// shows. The ripple plays on the window's top layer (see FitScreen): the click
// usually redraws what was clicked, and a ripple drawn inside it would be cut
// short with it.

func init() { widgets.RippleAt = RippleIn }

// RippleTime is how long a ripple takes to spread and fade: long enough to be
// seen after a click that redraws a page full of cards holds up the frames.
const RippleTime = 900 * time.Millisecond

// A click that opens a heavy page (Log Work's week, the Kanban board) holds up
// the frames that would have shown its ripple, and a motion timed by the clock
// was over before the page was drawn. A steady motion is timed by the frames
// actually drawn instead: one that comes late moves it on by an ordinary
// frame, not by the time lost, so the ripple is still seen once the page is up.
const (
	steadyStall = 50 * time.Millisecond
	steadyFrame = time.Second / 60
)

// steadyAnimation runs step from 0 to 1 over d of frames drawn (see above),
// with ease applied. It gives up waiting after four times d.
func steadyAnimation(d time.Duration, ease fyne.AnimationCurve, step func(p float32)) *fyne.Animation {
	var last time.Time
	var played time.Duration
	finished := false
	var anim *fyne.Animation
	anim = fyne.NewAnimation(4*d, func(p float32) {
		if finished {
			return
		}
		now := time.Now()
		if !last.IsZero() {
			gap := now.Sub(last)
			if gap > steadyStall {
				gap = steadyFrame
			}
			played += gap
		}
		last = now
		share := min(float32(played)/float32(d), 1)
		if p >= 1 {
			share = 1
		}
		step(ease(share))
		if share >= 1 {
			finished = true
			if anim != nil {
				anim.Stop()
			}
		}
	})
	anim.Curve = fyne.AnimationLinear
	return anim
}

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
