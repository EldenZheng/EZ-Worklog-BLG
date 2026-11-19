package shell

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"

	"worklog/internal/shared/widgets"
)

// CardPress gives a card the buttons' press: it shrinks while the pointer is
// held down on it and springs back when let go. A card is labels, links and
// buttons laid out to its width, and shrinking those would wrap its text
// afresh every frame. So while pressed the card is hidden and a picture of it,
// taken as the pointer went down, is drawn shrunk about its centre instead.
// The card's layout, hit area and neighbours never move.
//
// Log Work's tiles, Kanban's cards and the Sprint Calendar's bubbles all press
// alike. A card wraps its renderer with Wrap and hands its mouse to Down, Up
// and Play; a drag lifting the card calls Cancel.
type CardPress struct {
	motion widgets.PressMotion
	shot   *canvas.Image
	// card is the widget pressed; content, what it draws (hidden while the
	// picture stands in for it).
	card    fyne.CanvasObject
	content func() fyne.CanvasObject
}

// NewCardPress is the press of card, which draws content.
func NewCardPress(card fyne.CanvasObject, content func() fyne.CanvasObject) *CardPress {
	p := &CardPress{card: card, content: content, shot: canvas.NewImageFromImage(nil)}
	p.shot.FillMode = canvas.ImageFillStretch
	p.shot.Hide()
	return p
}

// pressRenderer is a card's own renderer with the press picture on top.
type pressRenderer struct {
	fyne.WidgetRenderer
	p *CardPress
}

func (r *pressRenderer) Layout(size fyne.Size) {
	r.WidgetRenderer.Layout(size)
	r.p.place(size)
}

func (r *pressRenderer) Objects() []fyne.CanvasObject {
	inner := r.WidgetRenderer.Objects()
	objects := make([]fyne.CanvasObject, 0, len(inner)+1)
	return append(append(objects, inner...), r.p.shot)
}

func (r *pressRenderer) Destroy() {
	r.p.motion.Stop()
	r.p.end()
	r.WidgetRenderer.Destroy()
}

// Wrap is the card's renderer inner with the press drawn over it.
func (p *CardPress) Wrap(inner fyne.WidgetRenderer) fyne.WidgetRenderer {
	return &pressRenderer{WidgetRenderer: inner, p: p}
}

// on says whether the press plays at all.
func (p *CardPress) on() bool { return p != nil && animationsOn() }

// picture takes the picture the press shows, unless one is showing now.
func (p *CardPress) picture() bool {
	if p.shot.Visible() {
		return true
	}
	driver := fyne.CurrentApp().Driver()
	c := driver.CanvasForObject(p.card)
	if c == nil {
		return false
	}
	picture := captureArea(c, driver.AbsolutePositionForObject(p.card), p.card.Size())
	if picture == nil {
		return false // partly off screen: no press rather than a stretched one
	}
	_, radius := c.PixelCoordinateForPosition(fyne.NewPos(0, bubbleRadius))
	clearCorners(picture, radius)
	p.shot.Image = picture
	return true
}

// paint shows the press as it is now, and the card again once it is over.
func (p *CardPress) paint() {
	if !p.motion.Active() {
		p.end()
		return
	}
	if !p.shot.Visible() {
		if content := p.content(); content != nil {
			content.Hide()
		}
		p.shot.Show()
	}
	p.place(p.card.Size())
	p.shot.Refresh()
}

func (p *CardPress) place(size fyne.Size) {
	s := p.motion.Scale()
	shrunk := fyne.NewSize(size.Width*s, size.Height*s)
	p.shot.Resize(shrunk)
	p.shot.Move(fyne.NewPos((size.Width-shrunk.Width)/2, (size.Height-shrunk.Height)/2))
}

func (p *CardPress) end() {
	if !p.shot.Visible() {
		return
	}
	p.shot.Hide()
	p.shot.Image = nil
	if content := p.content(); content != nil {
		content.Show()
	}
	canvas.Refresh(p.card)
}

// Down shrinks the card as the primary button goes down on it.
func (p *CardPress) Down(e *desktop.MouseEvent) {
	if e == nil || e.Button != desktop.MouseButtonPrimary || !p.on() || !p.picture() {
		return
	}
	p.motion.Down(p.paint)
}

// Up lets the card spring back, wherever the pointer was let go.
func (p *CardPress) Up() {
	if p != nil {
		p.motion.Up()
	}
}

// Play is the whole press for a tap with no pointer going down first. A click
// already shrank the card and its release is playing the return.
func (p *CardPress) Play() {
	if !p.on() {
		return
	}
	if !p.motion.Active() && !p.picture() {
		return
	}
	p.motion.Play(p.paint)
}

// Cancel puts the card back at once, for a drag lifting it.
func (p *CardPress) Cancel() {
	if p == nil {
		return
	}
	p.motion.Stop()
	p.end()
}

// CardCursor is a card's pointer: the pointing hand, as anything clicked
// shows, and the grabbing hand while the card is held or carried.
func CardCursor(held bool) desktop.Cursor {
	if held {
		return widgets.GrabCursor{}
	}
	return desktop.PointerCursor
}

// ---- Tappable ----

func (t *Tappable) pressRenderer(inner fyne.WidgetRenderer) fyne.WidgetRenderer {
	if !t.Press {
		return inner
	}
	if t.press == nil {
		t.press = NewCardPress(t, func() fyne.CanvasObject { return t.Content })
	}
	return t.press.Wrap(inner)
}

// MouseDown shrinks a pressing card as the pointer goes down on it; MouseUp
// lets it spring back. PressReleased is the same for a pointer let go
// somewhere else.
func (t *Tappable) MouseDown(e *desktop.MouseEvent) {
	if e != nil && e.Button == desktop.MouseButtonPrimary {
		t.held = true
	}
	if t.Press {
		t.press.Down(e)
	}
}

func (t *Tappable) MouseUp(*desktop.MouseEvent) { t.PressReleased() }

func (t *Tappable) PressReleased() {
	t.held = false
	t.press.Up()
}

func (t *Tappable) playPress() {
	if t.Press {
		t.press.Play()
	}
}
