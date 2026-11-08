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

const wheelResponse = 70 * time.Millisecond

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
	if wheel.target != target || !fyne.CurrentApp().Settings().ShowAnimations() || runtime.GOOS == "darwin" {
		if wheel.anim != nil {
			wheel.anim.Stop()
			wheel.anim = nil
		}
		// Never dump the remainder into a panel the pointer has already left.
		wheel.dx, wheel.dy = 0, 0
	}
	wheel.target, wheel.pos, wheel.abs = target, pos, abs
	if !fyne.CurrentApp().Settings().ShowAnimations() || runtime.GOOS == "darwin" {
		deliverWheel(target, pos, abs, dx, dy)
		return
	}
	// Reversing direction cancels that axis's remaining travel immediately.
	if dx*wheel.dx < 0 {
		wheel.dx = 0
	}
	if dy*wheel.dy < 0 {
		wheel.dy = 0
	}
	wheel.dx += dx
	wheel.dy += dy
	// Keep a single clock running across successive wheel notches. Restarting
	// an ease-out curve each time produces a fresh velocity jump on every notch.
	if wheel.anim != nil {
		return
	}
	last := time.Now()
	var anim *fyne.Animation
	anim = fyne.NewAnimation(time.Second, func(float32) {
		if wheel.anim != anim {
			return
		}
		now := time.Now()
		dt := min(now.Sub(last), 100*time.Millisecond)
		last = now
		fraction := float32(1 - math.Exp(-float64(dt)/float64(wheelResponse)))
		x, y := wheel.dx*fraction, wheel.dy*fraction
		if math.Abs(float64(wheel.dx)) < .1 && math.Abs(float64(wheel.dy)) < .1 {
			x, y = wheel.dx, wheel.dy
			wheel.anim = nil
			anim.Stop()
		}
		wheel.dx -= x
		wheel.dy -= y
		deliverWheel(target, wheel.pos, wheel.abs, x, y)
	})
	anim.Curve = fyne.AnimationLinear
	anim.RepeatCount = fyne.AnimationRepeatForever
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
