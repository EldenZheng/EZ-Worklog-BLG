package shell

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// Fader shows a control only while what holds it is lit, such as a card's
// Delete icon while the pointer is on the card: it fades in and out, and takes
// no clicks while it is gone. View keeps the control's room either way, so
// nothing around it moves as it comes and goes.
type Fader struct {
	View    fyne.CanvasObject
	control fyne.CanvasObject
	over    *container.ThemeOverride
	alpha   float32
	shown   bool
	anim    *fyne.Animation
	// leaving is set while a fade-out waits out faderLinger; gen tells a
	// stale wait from the current one.
	leaving bool
	gen     int
}

// faderTime is how long a Fader takes to fade in or out.
const faderTime = 320 * time.Millisecond

// faderLinger is how long the control stays after the light goes before it
// starts to fade: crossing from the card onto the control itself puts the
// light out for a moment, and the control must not dip as it does.
const faderLinger = 150 * time.Millisecond

// NewFader holds control, hidden until FadeTo(true).
func NewFader(control fyne.CanvasObject) *Fader {
	f := &Fader{control: control}
	f.over = container.NewThemeOverride(control, FadedTheme{Theme: theme.Current(), Alpha: &f.alpha})
	control.Hide()
	f.View = container.New(&keepRoomLayout{control: control}, f.over)
	return f
}

// Shown says the control is showing or on its way in.
func (f *Fader) Shown() bool { return f.shown && !f.leaving }

// FadeTo fades the control in (show) or out, then hides it.
func (f *Fader) FadeTo(show bool) {
	if f.leaving {
		if !show {
			return
		}
		f.leaving = false
		f.gen++
	}
	if show == f.shown {
		return
	}
	if !show && animationsOn() {
		f.leaving = true
		gen := f.gen
		fyne.NewAnimation(faderLinger, func(p float32) {
			if p >= 1 && f.leaving && f.gen == gen {
				f.leaving = false
				f.fadeNow(false)
			}
		}).Start()
		return
	}
	f.fadeNow(show)
}

func (f *Fader) fadeNow(show bool) {
	f.shown = show
	if f.anim != nil {
		f.anim.Stop()
		f.anim = nil
	}
	if show {
		f.control.Show()
	}
	to := float32(0)
	if show {
		to = 1
	}
	if !animationsOn() {
		f.set(to)
		f.settle()
		return
	}
	from := f.alpha
	var anim *fyne.Animation
	anim = fyne.NewAnimation(time.Duration(float32(faderTime)*max(0.2, abs32(to-from))), func(p float32) {
		if f.anim != anim {
			return
		}
		f.set(from + (to-from)*p)
		if p >= 1 {
			f.anim = nil
			f.settle()
		}
	})
	anim.Curve = fyne.AnimationEaseInOut
	f.anim = anim
	anim.Start()
}

// set draws the control at alpha. The override is set afresh each step: Fyne
// keeps a drawn icon under its theme scope, so a new scope draws it anew.
func (f *Fader) set(alpha float32) {
	f.alpha = alpha
	f.over.Theme = FadedTheme{Theme: theme.Current(), Alpha: &f.alpha}
	f.over.Refresh()
}

// settle hides the control once it has faded out.
func (f *Fader) settle() {
	if !f.shown {
		f.control.Hide()
	}
}

// FadedTheme draws everything at Alpha of its strength.
type FadedTheme struct {
	fyne.Theme
	Alpha *float32
}

func (t FadedTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	c := t.Theme.Color(name, v)
	if t.Alpha == nil || *t.Alpha >= 1 {
		return c
	}
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	n.A = uint8(float32(n.A) * max(0, *t.Alpha))
	return n
}

// keepRoomLayout takes the room its control wants, shown or not.
type keepRoomLayout struct{ control fyne.CanvasObject }

func (l *keepRoomLayout) MinSize([]fyne.CanvasObject) fyne.Size { return l.control.MinSize() }

func (l *keepRoomLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		o.Move(fyne.Position{})
		o.Resize(size)
	}
}

// frameFade is a frame on its way to a new look.
type frameFade struct {
	anim             *fyne.Animation
	fill, stroke     color.Color
	width            float32
	toFill, toStroke color.Color
	toWidth          float32
	edgeOnly         bool
	// rawFill and rawStroke are the colours asked for, nil included, which
	// the frame is left holding once the fade ends.
	rawFill, rawStroke color.Color
}

var frameFades = map[*canvas.Rectangle]*frameFade{}

// FadeFrame blends frame's fill, edge and edge width to the ones given over
// SlowFade, so an outline lights and dims gently rather than snapping. Asked
// again for the look it is already heading to, it carries on undisturbed.
func FadeFrame(frame *canvas.Rectangle, fill, stroke color.Color, width float32) {
	fadeFrame(frame, fill, stroke, width, false)
}

// FadeEdge is FadeFrame for the edge alone, leaving the fill to whatever
// else paints it.
func FadeEdge(frame *canvas.Rectangle, stroke color.Color, width float32) {
	fadeFrame(frame, nil, stroke, width, true)
}

func fadeFrame(frame *canvas.Rectangle, fill, stroke color.Color, width float32, edgeOnly bool) {
	if frame == nil {
		return
	}
	rawFill, rawStroke := fill, stroke
	if fill == nil {
		fill = color.Transparent
	}
	if stroke == nil {
		stroke = color.Transparent
	}
	f := frameFades[frame]
	if f != nil && f.edgeOnly == edgeOnly && (edgeOnly || sameColor(f.toFill, fill)) && sameColor(f.toStroke, stroke) && f.toWidth == width {
		return
	}
	if f == nil && (edgeOnly || sameColor(frame.FillColor, fill)) && sameColor(frame.StrokeColor, stroke) && frame.StrokeWidth == width {
		return
	}
	if f != nil && f.anim != nil {
		f.anim.Stop()
	}
	from := &frameFade{fill: frame.FillColor, stroke: frame.StrokeColor, width: frame.StrokeWidth, toFill: fill, toStroke: stroke, toWidth: width, edgeOnly: edgeOnly,
		rawFill: rawFill, rawStroke: rawStroke}
	if from.fill == nil {
		from.fill = color.Transparent
	}
	if from.stroke == nil {
		from.stroke = color.Transparent
	}
	frameFades[frame] = from
	step := func(p float32) {
		if p >= 1 {
			from.land(frame)
			return
		}
		if !edgeOnly {
			frame.FillColor = MixColor(from.fill, fill, p)
		}
		frame.StrokeColor = MixColor(from.stroke, stroke, p)
		frame.StrokeWidth = from.width + (width-from.width)*p
		frame.Refresh()
	}
	if !animationsOn() {
		step(1)
		delete(frameFades, frame)
		return
	}
	var anim *fyne.Animation
	anim = fyne.NewAnimation(SlowFade, func(p float32) {
		if frameFades[frame] != from {
			return
		}
		step(p)
		if p >= 1 {
			delete(frameFades, frame)
		}
	})
	anim.Curve = fyne.AnimationEaseInOut
	from.anim = anim
	anim.Start()
}

// SetFrame puts frame straight into a look, ending any fade it was in.
func SetFrame(frame *canvas.Rectangle, fill, stroke color.Color, width float32) {
	if f := frameFades[frame]; f != nil {
		if f.anim != nil {
			f.anim.Stop()
		}
		delete(frameFades, frame)
	}
	frame.FillColor, frame.StrokeColor, frame.StrokeWidth = fill, stroke, width
	frame.Refresh()
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// SettleFades ends every outline fade under way at the look it was heading
// to, for a check made straight after it started.
func SettleFades() {
	for frame, f := range frameFades {
		if f.anim != nil {
			f.anim.Stop()
		}
		delete(frameFades, frame)
		f.land(frame)
	}
}

// land leaves frame in the look the fade was heading to.
func (f *frameFade) land(frame *canvas.Rectangle) {
	if !f.edgeOnly {
		frame.FillColor = f.rawFill
	}
	frame.StrokeColor, frame.StrokeWidth = f.rawStroke, f.toWidth
	frame.Refresh()
}
