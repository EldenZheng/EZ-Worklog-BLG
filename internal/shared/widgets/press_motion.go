package widgets

import (
	"time"

	"fyne.io/fyne/v2"
)

// ButtonPressDuration is the complete compression-and-return time.
const ButtonPressDuration = 600 * time.Millisecond

// ButtonPressCompression is the share of a press spent shrinking; the rest is
// the return.
const ButtonPressCompression = 0.25

// pressTapWindow is how soon after a release its tap arrives: at once, in the
// same event.
const pressTapWindow = 150 * time.Millisecond

// PressMotion gives custom action tags and tab buttons the native buttons'
// feedback without changing the control's layout or hit area. As on a button,
// the control shrinks while the pointer is held down on it (Down) and springs
// back when it is let go (Up); Play is the whole press, for a tap that came
// with no pointer going down first.
type PressMotion struct {
	scale float32
	anim  *Steady
	down  bool
	upAt  time.Time
}

func (p *PressMotion) Scale() float32 {
	if p.scale == 0 {
		return 1
	}
	return p.scale
}

// Active says a press is under way: held down, or still playing.
func (p *PressMotion) Active() bool { return p.anim != nil || p.down }

func (p *PressMotion) Stop() {
	p.anim.Stop()
	p.anim = nil
	p.scale = 1
	p.down = false
}

// begin readies a press that starts from the control as it is painted now.
// Steady, because a tab or tag often redraws the page it is on: the press then
// waits for that frame rather than turning up half-way back.
func (p *PressMotion) begin(paint func()) *Steady {
	from := p.Scale()
	p.Stop()
	var anim *Steady
	anim = NewSteady(ButtonPressDuration, func(progress float32) {
		if p.anim != anim {
			return
		}
		p.scale = ButtonPressScale(from, progress)
		if progress >= 1 {
			p.scale, p.anim = 1, nil
		}
		paint()
	})
	p.anim = anim
	return anim
}

// Play is a whole press. After a click it does nothing more: the control
// shrank under the pointer and its release is already playing the return.
func (p *PressMotion) Play(paint func()) {
	if p.anim != nil && !p.down && time.Since(p.upAt) < pressTapWindow {
		return
	}
	if !fyne.CurrentApp().Settings().ShowAnimations() {
		p.Stop()
		paint()
		return
	}
	p.begin(paint).Start()
}

// Down shrinks the control and holds it there, for the pointer going down.
func (p *PressMotion) Down(paint func()) {
	if !fyne.CurrentApp().Settings().ShowAnimations() {
		return
	}
	anim := p.begin(paint)
	p.down = true
	anim.StartTo(ButtonPressCompression)
}

// Up lets a held control spring back, for the pointer being let go, on the
// control or anywhere else.
func (p *PressMotion) Up() {
	if !p.down {
		return
	}
	p.down, p.upAt = false, time.Now()
	if p.anim != nil {
		p.anim.PlayTo(1)
	}
}

// ButtonPressScale is shared by live custom controls and a popup's closing
// picture, so dismissing a button does not cut its feedback short.
func ButtonPressScale(from, progress float32) float32 {
	if progress <= ButtonPressCompression {
		t := progress / ButtonPressCompression
		return from + (0.94-from)*(1-(1-t)*(1-t))
	}
	t := (progress - ButtonPressCompression) / (1 - ButtonPressCompression)
	return 0.94 + 0.06*(1-(1-t)*(1-t)*(1-t))
}
