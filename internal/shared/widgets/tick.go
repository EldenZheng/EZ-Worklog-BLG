package widgets

import (
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// A check box ticks itself rather than snapping: its box fills with gold and
// the tick is drawn in, first the short stroke then the long one; unticking
// runs the same backwards. TickMotion draws that over a widget.Check's own
// icon while it plays and steps aside once it is over, leaving the check's
// icon, which by then shows the same, to stand alone.

// TickTime is how long a tick takes to draw in or out.
const TickTime = 600 * time.Millisecond

// The material check box icon's box and tick, as shares of its 24 point grid.
var (
	tickBoxFrom = float32(3.0 / 24)
	tickBoxSide = float32(18.0 / 24)
	tickPoints  = [3][2]float32{{6.0 / 24, 12.3 / 24}, {10.0 / 24, 16.2 / 24}, {18.0 / 24, 8.2 / 24}}
)

// TickMotion is the drawn check box. Its Objects go over the check, in the
// same container, after it.
type TickMotion struct {
	box         *canvas.Rectangle
	short, long *canvas.Line
	ink         color.Color
	// Border is the empty box's edge, the theme's input border when nil.
	Border         color.Color
	anim           *fyne.Animation
	shown          float32
	icon           fyne.Position
	side           float32
	playing, ready bool
}

// NewTickMotion is a hidden tick in ink, gold when nil.
func NewTickMotion(ink color.Color) *TickMotion {
	if ink == nil {
		ink = LogoGold
	}
	m := &TickMotion{ink: ink, box: canvas.NewRectangle(color.Transparent)}
	m.short, m.long = canvas.NewLine(color.Transparent), canvas.NewLine(color.Transparent)
	m.hide()
	return m
}

// Objects are the box and its two strokes.
func (m *TickMotion) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{m.box, m.short, m.long}
}

// Place puts the tick over a check box icon at pos, side points square.
func (m *TickMotion) Place(pos fyne.Position, side float32) {
	m.icon, m.side, m.ready = pos, side, side > 0
	if m.playing {
		m.Draw(m.shown)
	}
}

// PlaceOnCheck places the tick over the icon of a widget.Check laid out at
// pos with size, where Fyne's check renderer draws it.
func (m *TickMotion) PlaceOnCheck(pos fyne.Position, size fyne.Size) {
	inner := theme.InnerPadding()
	side := theme.Size(theme.SizeNameInlineIcon)
	at := fyne.NewPos(inner/2+theme.Size(theme.SizeNameInputBorder), (size.Height-side)/2)
	m.Place(pos.Add(at), side)
}

// Play draws the tick in (checked) or out from where it stands.
func (m *TickMotion) Play(checked bool) {
	from, to := float32(0), float32(1)
	if !checked {
		from, to = 1, 0
	}
	m.Stop()
	if !fyne.CurrentApp().Settings().ShowAnimations() {
		return
	}
	m.playing = true
	m.Draw(from)
	m.anim = fyne.NewAnimation(TickTime, func(done float32) {
		if done >= 1 {
			m.Stop()
			return
		}
		m.Draw(from + (to-from)*done)
	})
	// Even over the whole run; each phase eases itself (see Draw).
	m.anim.Curve = fyne.AnimationLinear
	m.anim.Start()
}

// Stop ends the tick's motion and steps aside for the check's own icon.
func (m *TickMotion) Stop() {
	if m.anim != nil {
		m.anim.Stop()
		m.anim = nil
	}
	m.playing = false
	m.hide()
}

// Show draws the tick part-way, ticked share 0 to 1, for an arrival to move
// it frame by frame; at 1 or more it steps aside.
func (m *TickMotion) Show(share float32) {
	if share >= 1 {
		m.Stop()
		return
	}
	m.playing = true
	m.Draw(share)
}

func (m *TickMotion) hide() {
	for _, o := range m.Objects() {
		o.Hide()
	}
}

// Draw paints the box ticked share of the way: the gold fills in over the
// first half and the tick is drawn over the second.
func (m *TickMotion) Draw(share float32) {
	m.shown = share
	if !m.ready {
		return
	}
	at := func(p [2]float32) fyne.Position {
		return m.icon.Add(fyne.NewPos(p[0]*m.side, p[1]*m.side))
	}
	// The gold fills in over the first 55%, and the tick starts drawing at 35%
	// so the two run into each other rather than one waiting on the other.
	fill := smoothStep(min(1, max(0, share/0.55)))
	m.box.Move(m.icon.Add(fyne.NewSquareOffsetPos(tickBoxFrom * m.side)))
	m.box.Resize(fyne.NewSquareSize(tickBoxSide * m.side))
	m.box.CornerRadius = m.side * 2 / 24
	m.box.StrokeWidth = m.side * 2 / 24
	border := m.Border
	if border == nil {
		border = theme.Color(theme.ColorNameInputBorder)
	}
	m.box.StrokeColor = mixColor(border, m.ink, fill)
	m.box.FillColor = mixColor(theme.Color(theme.ColorNameInputBackground), m.ink, fill)
	m.box.Show()
	m.box.Refresh()

	// The tick is cut out of the gold in the background's colour, as on the
	// icon, drawn along its length at an even speed: the short stroke, then
	// round the corner into the long one.
	drawn := smoothStep(min(1, max(0, (share-0.35)/0.65)))
	cut := theme.Color(theme.ColorNameBackground)
	length := func(a, b [2]float32) float32 {
		dx, dy := float64(b[0]-a[0]), float64(b[1]-a[1])
		return float32(math.Sqrt(dx*dx + dy*dy))
	}
	first, second := length(tickPoints[0], tickPoints[1]), length(tickPoints[1], tickPoints[2])
	along := drawn * (first + second)
	stroke := func(l *canvas.Line, from, to [2]float32, part float32) {
		if part <= 0 {
			l.Hide()
			return
		}
		a, b := at(from), at(to)
		l.Position1 = a
		l.Position2 = fyne.NewPos(a.X+(b.X-a.X)*part, a.Y+(b.Y-a.Y)*part)
		l.StrokeColor = cut
		l.StrokeWidth = m.side * 2.2 / 24
		l.Show()
		l.Refresh()
	}
	stroke(m.short, tickPoints[0], tickPoints[1], min(1, along/first))
	stroke(m.long, tickPoints[1], tickPoints[2], max(0, along-first)/second)
}

// smoothStep eases t (0 to 1) in and out, with no jolt at either end.
func smoothStep(t float32) float32 { return t * t * (3 - 2*t) }

func mixColor(a, b color.Color, t float32) color.Color {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	mix := func(x, y uint32) uint8 { return uint8((float32(x) + (float32(y)-float32(x))*t) / 257) }
	return color.NRGBA{R: mix(ar, br), G: mix(ag, bg), B: mix(ab, bb), A: mix(aa, ba)}
}

// TickCheck is a check box with no label whose tick draws itself in and out.
// The pointer on it is passed to a hover parent (SetHoverParent), so a row
// it sits in can stay lit.
type TickCheck struct {
	widget.BaseWidget
	check  *hoverCheck
	motion *TickMotion
	// OnChanged hears the new state as soon as the box is clicked; the tick
	// goes on drawing after it.
	OnChanged func(bool)
}

// NewTickCheck is a TickCheck ticked or not, drawn in ink (gold when nil)
// with its empty box edged in ink too.
func NewTickCheck(checked bool, ink color.Color, changed func(bool)) *TickCheck {
	t := &TickCheck{motion: NewTickMotion(ink), OnChanged: changed}
	t.motion.Border = t.motion.ink
	t.check = &hoverCheck{}
	t.check.Checked = checked
	t.check.ExtendBaseWidget(t.check)
	t.check.OnChanged = func(on bool) {
		t.motion.Play(on)
		if t.OnChanged != nil {
			t.OnChanged(on)
		}
	}
	t.ExtendBaseWidget(t)
	return t
}

// Checked is whether the box is ticked.
func (t *TickCheck) Checked() bool { return t.check.Checked }

// SetChecked ticks or unticks the box quietly: no motion, and OnChanged is
// not called. A tick still drawing keeps drawing toward the same state.
func (t *TickCheck) SetChecked(on bool) {
	if t.check.Checked == on {
		return
	}
	t.motion.Stop()
	t.check.Checked = on
	t.check.Refresh()
}

// ShowTick draws the tick part-way, for an arrival: share 0 to 1 of the way
// to how the box stands. A ticked box is drawn in from empty; an unticked one
// empties from ticked, for a box just unticked.
func (t *TickCheck) ShowTick(share float32) {
	if !t.check.Checked {
		share = 1 - share
		if share <= 0 {
			t.motion.Stop()
			return
		}
		t.motion.playing = true
		t.motion.Draw(share)
		return
	}
	t.motion.Show(share)
}

// SetHoverParent hands the pointer arriving and leaving to f.
func (t *TickCheck) SetHoverParent(f func(on bool)) { t.check.parent = f }

func (t *TickCheck) CreateRenderer() fyne.WidgetRenderer {
	return &tickCheckRenderer{t: t, objects: append([]fyne.CanvasObject{t.check}, t.motion.Objects()...)}
}

type tickCheckRenderer struct {
	t       *TickCheck
	objects []fyne.CanvasObject
}

func (r *tickCheckRenderer) Layout(size fyne.Size) {
	r.t.check.Move(fyne.NewPos(0, 0))
	r.t.check.Resize(size)
	r.t.motion.PlaceOnCheck(fyne.NewPos(0, 0), size)
}

func (r *tickCheckRenderer) MinSize() fyne.Size           { return r.t.check.MinSize() }
func (r *tickCheckRenderer) Refresh()                     { r.t.check.Refresh(); r.Layout(r.t.Size()) }
func (r *tickCheckRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *tickCheckRenderer) Destroy()                     { r.t.motion.Stop() }

// hoverCheck is a widget.Check that tells a parent when the pointer comes and
// goes.
type hoverCheck struct {
	widget.Check
	parent func(on bool)
}

func (c *hoverCheck) MouseIn(e *desktop.MouseEvent) {
	c.Check.MouseIn(e)
	if c.parent != nil {
		c.parent(true)
	}
}

func (c *hoverCheck) MouseOut() {
	c.Check.MouseOut()
	if c.parent != nil {
		c.parent(false)
	}
}
