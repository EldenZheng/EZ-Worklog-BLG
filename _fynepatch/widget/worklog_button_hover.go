package widget

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
)

// WorklogEnableHoverFade opts a control into the app's gentle hover without
// changing its role, keyboard handling, press feedback or layout.
func (b *Button) WorklogEnableHoverFade() { b.worklogHoverFade = true }

type worklogHover struct {
	ready, hovered bool
	target         color.Color
	anim           *fyne.Animation
}

func (h *worklogHover) stop() {
	if h.anim != nil {
		h.anim.Stop()
		h.anim = nil
	}
}

func hoverColorEqual(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func (r *buttonRenderer) worklogSetBackground(target color.Color) {
	h := &r.hover
	hovered := r.button.hovered && !r.button.Disabled()
	changed := h.ready && h.hovered != hovered
	h.ready, h.hovered = true, hovered
	if !r.button.worklogHoverFade || r.button.Disabled() || !fyne.CurrentApp().Settings().ShowAnimations() || !changed {
		// A refresh in the middle of the same hover leaves its frame intact;
		// a new role, theme or disabled state replaces the old target at once.
		if h.anim != nil && r.button.worklogHoverFade && !r.button.Disabled() && fyne.CurrentApp().Settings().ShowAnimations() && hoverColorEqual(h.target, target) {
			return
		}
		h.stop()
		h.target = target
		r.background.FillColor = target
		return
	}
	h.stop()
	from := color.NRGBAModel.Convert(r.background.FillColor).(color.NRGBA)
	to := color.NRGBAModel.Convert(target).(color.NRGBA)
	// Quiet buttons fade gold's alpha, without drifting through black.
	if from.A == 0 {
		from.R, from.G, from.B = to.R, to.G, to.B
	}
	if to.A == 0 {
		to.R, to.G, to.B = from.R, from.G, from.B
	}
	h.target = target
	var anim *fyne.Animation
	anim = fyne.NewAnimation(300*time.Millisecond, func(p float32) {
		if h.anim != anim {
			return
		}
		mix := func(a, b uint8) uint8 { return uint8(float32(a) + (float32(b)-float32(a))*p + 0.5) }
		r.background.FillColor = color.NRGBA{R: mix(from.R, to.R), G: mix(from.G, to.G), B: mix(from.B, to.B), A: mix(from.A, to.A)}
		if p >= 1 {
			r.background.FillColor = target
			h.anim = nil
		}
		r.background.Refresh()
	})
	anim.Curve = fyne.AnimationEaseInOut
	h.anim = anim
	anim.Start()
}
