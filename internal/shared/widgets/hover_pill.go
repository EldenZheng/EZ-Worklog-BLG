package widgets

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// HoverDuration is shared by gold hover transitions throughout the app.
const HoverDuration = 600 * time.Millisecond

// HoverPill fades in at the first item, slides between items, and fades out
// on leaving. Lists, filters and charts share its gold fill and timing.
type HoverPill struct {
	Rect      *canvas.Rectangle
	Animation *fyne.Animation
	shown     float32
	on        bool
	pos       fyne.Position
	size      fyne.Size
}

func NewHoverPill() *HoverPill {
	r := canvas.NewRectangle(color.Transparent)
	r.CornerRadius = 8
	r.StrokeWidth = 1
	r.StrokeColor = color.Transparent
	return &HoverPill{Rect: r}
}

func (p *HoverPill) Stop() {
	if p.Animation != nil {
		p.Animation.Stop()
		p.Animation = nil
	}
}

// ShowAt preserves a hover when a filter redraw replaces the item under it.
func (p *HoverPill) ShowAt(pos fyne.Position, size fyne.Size) {
	p.Stop()
	p.on, p.shown, p.pos, p.size = true, 1, pos, size
	p.Rect.Move(pos)
	p.Rect.Resize(size)
	p.paint()
}

// Fit keeps a settled highlight aligned when its content is resized.
func (p *HoverPill) Fit(pos fyne.Position, size fyne.Size) {
	if p.on && (pos != p.pos || size != p.size) {
		p.Stop()
		p.pos, p.size = pos, size
		p.Rect.Move(pos)
		p.Rect.Resize(size)
		p.shown = 1
		p.paint()
	}
}

func (p *HoverPill) paint() {
	p.Rect.FillColor = WithAlpha(LogoGold, 0.16*p.shown)
	p.Rect.StrokeColor = WithAlpha(LogoGold, 0.45*p.shown)
	p.Rect.Refresh()
}

func (p *HoverPill) MoveTo(pos fyne.Position, size fyne.Size, on bool) {
	p.Stop()
	p.on = on
	p.pos, p.size = pos, size
	fromPos, fromSize, fromShown := p.Rect.Position(), p.Rect.Size(), p.shown
	toShown := float32(1)
	if !on {
		pos, size, toShown = fromPos, fromSize, 0
	} else if fromShown < 0.05 {
		fromPos, fromSize = pos, size
	}
	step := func(done float32) {
		mix := func(a, b float32) float32 { return a + (b-a)*done }
		p.Rect.Move(fyne.NewPos(mix(fromPos.X, pos.X), mix(fromPos.Y, pos.Y)))
		p.Rect.Resize(fyne.NewSize(mix(fromSize.Width, size.Width), mix(fromSize.Height, size.Height)))
		p.shown = mix(fromShown, toShown)
		p.paint()
	}
	if !fyne.CurrentApp().Settings().ShowAnimations() {
		step(1)
		return
	}
	var anim *fyne.Animation
	anim = fyne.NewAnimation(HoverDuration, func(done float32) {
		if p.Animation != anim {
			return
		}
		step(done)
		if done >= 1 {
			p.Animation = nil
		}
	})
	p.Animation = anim
	anim.Curve = fyne.AnimationEaseOut
	anim.Start()
}
