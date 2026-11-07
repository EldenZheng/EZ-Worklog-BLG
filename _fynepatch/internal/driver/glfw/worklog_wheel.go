package glfw

// Worklog patch, copied into the private Fyne build by tools/patchfyne, which also
// renames Fyne's own processMouseScrolled so this one replaces it. The go tool
// skips folders that start with "_", so this file only builds there.
//
// Two changes to the mouse wheel:
//   - It goes to the innermost scroller under the pointer that can still move
//     that way. Stock Fyne always picks the innermost one, so hovering a text
//     field or a list that had reached its end stopped the page from scrolling.
//   - Each notch glides over a few frames instead of jumping.

import (
	"math"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
)

const wheelGlide = 160 * time.Millisecond

// Match Status's day sidebar throughout the app. Explicitly boosted scrollers
// retain their own speed (including Sprint Calendar's faster month view). A Mac
// trackpad already sends a stream of small movements, so it keeps Fyne's speed.
func defaultWheelBoost() float32 {
	if runtime.GOOS == "darwin" {
		return 1
	}
	return 3
}

type wheelReach interface {
	CanScrollBy(dx, dy float32) bool
}

// wheel is the glide in progress. Wheel events and animation ticks both run
// on the main thread, so it needs no lock.
var wheel struct {
	target   fyne.Scrollable
	pos, abs fyne.Position
	dx, dy   float32 // not yet delivered to target
	anim     *fyne.Animation
}

func (w *window) processMouseScrolled(xoff float64, yoff float64) {
	// Hover previews must not capture the wheel. Remove them before hit testing
	// so this very event reaches the real scroller under the pointer.
	if preview, ok := w.canvas.Overlays().Top().(interface{ DismissForScroll() }); ok {
		preview.DismissForScroll()
	}
	if math.Abs(xoff) >= scrollAccelerateCutoff {
		xoff *= scrollAccelerateRate
	}
	if math.Abs(yoff) >= scrollAccelerateCutoff {
		yoff *= scrollAccelerateRate
	}
	dx, dy := float32(xoff)*scrollSpeed, float32(yoff)*scrollSpeed

	mousePos := w.mousePos
	var under []fyne.CanvasObject // outermost first
	w.findObjectAtPositionMatching(w.canvas, mousePos, func(object fyne.CanvasObject) bool {
		_, ok := object.(fyne.Scrollable)
		if ok {
			under = append(under, object)
		}
		return ok
	})
	if len(under) == 0 {
		return
	}
	// When nothing can move, the innermost keeps the event as in stock Fyne:
	// a sideways scroller then turns the wheel into a sideways scroll.
	target := under[len(under)-1]
	for i := len(under) - 1; i >= 0; i-- {
		reach, ok := under[i].(wheelReach)
		if !ok || reach.CanScrollBy(dx, dy) {
			target = under[i]
			break
		}
	}
	_, pos, _ := w.findObjectAtPositionMatching(w.canvas, mousePos, func(object fyne.CanvasObject) bool {
		return object == target
	})
	if _, custom := target.(interface{ WheelSpeed() float32 }); !custom {
		dx *= defaultWheelBoost()
		dy *= defaultWheelBoost()
	}
	glideWheel(target.(fyne.Scrollable), pos, mousePos, dx, dy)
}

func glideWheel(target fyne.Scrollable, pos, abs fyne.Position, dx, dy float32) {
	if wheel.anim != nil {
		wheel.anim.Stop()
		wheel.anim = nil
	}
	if wheel.target != nil && wheel.target != target {
		deliverWheel(wheel.target, wheel.pos, wheel.abs, wheel.dx, wheel.dy)
		wheel.dx, wheel.dy = 0, 0
	}
	wheel.target, wheel.pos, wheel.abs = target, pos, abs
	// A notch during a glide adds to what is left, so fast wheeling speeds up.
	wheel.dx += dx
	wheel.dy += dy
	totalX, totalY := wheel.dx, wheel.dy
	var sentX, sentY float32
	var anim *fyne.Animation
	anim = fyne.NewAnimation(wheelGlide, func(done float32) {
		if wheel.anim != anim {
			return
		}
		x, y := totalX*done-sentX, totalY*done-sentY
		if done >= 1 {
			x, y = wheel.dx, wheel.dy
			wheel.anim = nil
		}
		sentX += x
		sentY += y
		wheel.dx -= x
		wheel.dy -= y
		deliverWheel(target, pos, abs, x, y)
	})
	anim.Curve = fyne.AnimationEaseOut
	wheel.anim = anim
	anim.Start()
}

func deliverWheel(target fyne.Scrollable, pos, abs fyne.Position, dx, dy float32) {
	if dx == 0 && dy == 0 {
		return
	}
	ev := &fyne.ScrollEvent{}
	ev.Scrolled = fyne.NewDelta(dx, dy)
	ev.Position = pos
	ev.AbsolutePosition = abs
	target.Scrolled(ev)
}
