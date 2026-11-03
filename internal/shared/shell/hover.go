package shell

import (
	"fmt"
	"image/color"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// hoverFade is how long a hover change takes to blend in or out.
const hoverFade = 180 * time.Millisecond

// mixColor is a between a and b by share (0 is a, 1 is b), alpha included.
func MixColor(a, b color.Color, share float32) color.Color {
	x := color.NRGBAModel.Convert(a).(color.NRGBA)
	y := color.NRGBAModel.Convert(b).(color.NRGBA)
	mix := func(p, q uint8) uint8 { return uint8(float32(p) + (float32(q)-float32(p))*share + 0.5) }
	return color.NRGBA{R: mix(x.R, y.R), G: mix(x.G, y.G), B: mix(x.B, y.B), A: mix(x.A, y.A)}
}

// Glow eases a 0..1 hover level towards a target, calling paint on every
// frame.
type Glow struct {
	level float32
	anim  *fyne.Animation
}

// Level is how far lit it is now, 0 to 1.
func (g *Glow) Level() float32 { return g.level }

// To eases the level to target, calling paint with the level on every frame.
func (g *Glow) To(target float32, paint func(level float32)) { g.to(target, paint) }

func (g *Glow) to(target float32, paint func(level float32)) {
	if g.anim != nil {
		g.anim.Stop()
	}
	from := g.level
	var anim *fyne.Animation
	anim = fyne.NewAnimation(hoverFade, func(done float32) {
		if g.anim != anim {
			return
		}
		g.level = from + (target-from)*done
		paint(g.level)
	})
	anim.Curve = fyne.AnimationEaseInOut
	g.anim = anim
	anim.Start()
}

// Colours of the gold tag: a muted gold on a faint gold wash at rest, dark
// text on bright gold under the pointer.
var (
	TagRestFill  = color.NRGBA{R: 0xcd, G: 0xa2, B: 0x36, A: 0x38}
	TagRestInk   = color.NRGBA{R: 0xc9, G: 0xa9, B: 0x62, A: 0xff}
	TagHoverFill = widgets.LogoGold
	TagHoverInk  = color.NRGBA{R: 0x1c, G: 0x18, B: 0x0d, A: 0xff}
)

// HoverTag is a rounded tag of text whose fill and ink blend to a second pair
// of colours while the pointer is over it. Wide tags fill the width they are
// given, text centred; the others hug their text, cut short with an ellipsis
// when the width given is less (place them with HugTag). A tag with OnTapped
// set is clickable and shows the pointer cursor.
type HoverTag struct {
	widget.BaseWidget
	Text                string
	OnTapped            func()
	RestFill, HoverFill color.Color
	RestInk, HoverInk   color.Color
	Wide                bool
	bg                  *canvas.Rectangle
	label               *canvas.Text
	hover               Glow
	parent              func(on bool)
}

// SetHoverParent tells the tag which bubble to keep lit while the pointer is
// on the tag.
func (t *HoverTag) SetHoverParent(f func(on bool)) { t.parent = f }

const tagInset = 8

// NewGoldTag is the gold HoverTag, for a parent issue and the like.
func NewGoldTag(text string, onTapped func()) *HoverTag {
	return NewHoverTag(text, TagRestFill, TagHoverFill, TagRestInk, TagHoverInk, onTapped)
}

func NewHoverTag(text string, restFill, hoverFill, restInk, hoverInk color.Color, onTapped func()) *HoverTag {
	t := &HoverTag{Text: text, OnTapped: onTapped, RestFill: restFill, HoverFill: hoverFill, RestInk: restInk, HoverInk: hoverInk}
	t.bg = canvas.NewRectangle(restFill)
	t.bg.CornerRadius = 10
	t.label = canvas.NewText(text, restInk)
	t.label.TextSize = theme.CaptionTextSize()
	t.label.TextStyle = fyne.TextStyle{Bold: true}
	t.ExtendBaseWidget(t)
	return t
}

// NaturalWidth is the width the tag takes with its whole text showing.
func (t *HoverTag) NaturalWidth() float32 {
	return fyne.MeasureText(t.Text, t.label.TextSize, t.label.TextStyle).Width + 2*tagInset
}

// Shown is the text the tag shows now, after any ellipsis.
func (t *HoverTag) Shown() string { return t.label.Text }

func (t *HoverTag) paint(level float32) {
	t.bg.FillColor = MixColor(t.RestFill, t.HoverFill, level)
	t.label.Color = MixColor(t.RestInk, t.HoverInk, level)
	t.bg.Refresh()
	t.label.Refresh()
}

func (t *HoverTag) MouseIn(*desktop.MouseEvent) {
	t.hover.to(1, t.paint)
	if t.parent != nil {
		t.parent(true)
	}
}

func (t *HoverTag) MouseMoved(*desktop.MouseEvent) {}

func (t *HoverTag) MouseOut() {
	t.hover.to(0, t.paint)
	if t.parent != nil {
		t.parent(false)
	}
}

func (t *HoverTag) Tapped(*fyne.PointEvent) {
	if t.OnTapped != nil {
		t.OnTapped()
	}
}

func (t *HoverTag) Cursor() desktop.Cursor {
	if t.OnTapped != nil {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

func (t *HoverTag) CreateRenderer() fyne.WidgetRenderer { return &hoverTagRenderer{t: t} }

type hoverTagRenderer struct{ t *HoverTag }

func (r *hoverTagRenderer) Layout(size fyne.Size) {
	t := r.t
	t.bg.Move(fyne.NewPos(0, 0))
	t.bg.Resize(size)
	text := t.Text
	if !t.Wide && size.Width > 0 {
		text = elideToWidth(t.Text, size.Width-2*tagInset, t.label.TextSize, t.label.TextStyle)
	}
	if text != t.label.Text {
		t.label.Text = text
		t.label.Refresh()
	}
	m := t.label.MinSize()
	x := float32(tagInset)
	if t.Wide {
		x = (size.Width - m.Width) / 2
	}
	t.label.Move(fyne.NewPos(x, (size.Height-m.Height)/2))
	t.label.Resize(m)
}

func (r *hoverTagRenderer) MinSize() fyne.Size {
	t := r.t
	text := fyne.MeasureText(t.Text, t.label.TextSize, t.label.TextStyle)
	if t.Wide {
		return fyne.NewSize(text.Width+2*tagInset, text.Height+2*theme.Padding()+4)
	}
	ellipsis := fyne.MeasureText("…", t.label.TextSize, t.label.TextStyle)
	return fyne.NewSize(ellipsis.Width+2*tagInset, text.Height+4)
}

func (r *hoverTagRenderer) Refresh() {
	r.t.paint(r.t.hover.level)
	r.Layout(r.t.Size())
}

func (r *hoverTagRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.t.bg, r.t.label}
}
func (r *hoverTagRenderer) Destroy() {}

// HugTag places a tag at its text's width, never wider than the row it sits
// in, so the tag (and its hover) covers only itself.
func HugTag(t *HoverTag) fyne.CanvasObject {
	return container.New(&hugLayout{t: t}, t)
}

type hugLayout struct{ t *HoverTag }

func (l *hugLayout) MinSize([]fyne.CanvasObject) fyne.Size { return l.t.MinSize() }

func (l *hugLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	l.t.Move(fyne.NewPos(0, 0))
	width := l.t.NaturalWidth()
	if size.Width > 0 {
		width = min(width, size.Width)
	}
	l.t.Resize(fyne.NewSize(width, size.Height))
}

// elideToWidth returns s, or as much of it as fits width with an ellipsis.
func elideToWidth(s string, width, size float32, style fyne.TextStyle) string {
	if fyne.MeasureText(s, size, style).Width <= width {
		return s
	}
	r := []rune(s)
	lo, hi := 0, len(r)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fyne.MeasureText(string(r[:mid])+"…", size, style).Width <= width {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return strings.TrimRight(string(r[:lo]), " ") + "…"
}

// StripeBubble is the card look every worklog and issue bubble shares: a dark
// org tint at rest that lightens under the pointer, the org bar down the left
// sliding in as it does. It does not watch the pointer itself; whatever holds
// it calls SetHover, or wraps it with HoverBubble.
type StripeBubble struct {
	widget.BaseWidget
	Body    fyne.CanvasObject
	accent  color.Color
	bg      *canvas.Rectangle
	stripe  *canvas.Rectangle
	hover   Glow
	on      bool // lit now
	over    bool // the pointer is on the bubble itself
	inside  int  // links and tags inside it the pointer is on
	present bool // the pointer is on the bubble or something in it
	parent  func(on bool)
}

const stripeWidth = 4

// BubbleFill and BubbleStroke are a bubble's tint and edge at rest and under
// the pointer. The lighter pair is the Sprint Calendar's bubble.
func BubbleFill(accent color.Color, hovered bool) color.Color {
	if hovered {
		return BlendColor(theme.Color(theme.ColorNameInputBackground), accent, 0.24)
	}
	return BlendColor(theme.Color(theme.ColorNameInputBackground), accent, 0.10)
}

func BubbleStroke(accent color.Color, hovered bool) color.Color {
	if hovered {
		return BlendColor(theme.Color(theme.ColorNameInputBorder), accent, 0.65)
	}
	return BlendColor(theme.Color(theme.ColorNameInputBorder), accent, 0.5)
}

func NewStripeBubble(accent color.Color, body fyne.CanvasObject) *StripeBubble {
	b := &StripeBubble{Body: body, accent: accent}
	b.bg = canvas.NewRectangle(BubbleFill(accent, false))
	b.bg.StrokeColor = BubbleStroke(accent, false)
	b.bg.StrokeWidth = 1
	b.bg.CornerRadius = 10
	b.stripe = canvas.NewRectangle(color.Transparent)
	b.stripe.CornerRadius = 2
	b.ExtendBaseWidget(b)
	AttachHoverChildren(body, b.ChildHover)
	return b
}

func (b *StripeBubble) paint(level float32) {
	b.bg.FillColor = MixColor(BubbleFill(b.accent, false), BubbleFill(b.accent, true), level)
	b.bg.StrokeColor = MixColor(BubbleStroke(b.accent, false), BubbleStroke(b.accent, true), level)
	b.stripe.FillColor = MixColor(color.Transparent, b.accent, level)
	b.placeStripe(b.Size())
	b.bg.Refresh()
	b.stripe.Refresh()
}

func (b *StripeBubble) placeStripe(size fyne.Size) {
	pad := theme.Padding()
	// Slides in from just outside the bubble's left edge to its slot.
	x := pad - (pad+stripeWidth+8)*(1-b.hover.level)
	b.stripe.Move(fyne.NewPos(x, 2*pad))
	b.stripe.Resize(fyne.NewSize(stripeWidth, max(size.Height-4*pad, 0)))
}

// Lightable is anything drawn as a bubble that lights up under the pointer:
// a StripeBubble, or the Sprint Calendar's timed blocks.
type Lightable interface {
	SetHover(on bool)
}

// held is the bubble being dragged. While one is, it keeps its hover and no
// other bubble takes one, so only the card in hand is lit.
var held Lightable

// HoverAllowed reports whether a bubble may change its hover now: while one
// is held, only it is lit, and it stays lit until it is let go.
func HoverAllowed(l Lightable, on bool) bool {
	return !(held != nil && held != l || held == l && !on)
}

// Lit reports whether the bubble is lit, or lighting up, now.
func (b *StripeBubble) Lit() bool { return b.on }

// SetHover says whether the pointer is on the bubble itself.
func (b *StripeBubble) SetHover(on bool) {
	b.over = on
	b.light()
}

// ChildHover says the pointer came onto or left a link or tag inside the
// bubble. Fyne tells only the innermost thing under the pointer, so the
// bubble hears "left" as the pointer moves onto a link; this keeps it lit.
func (b *StripeBubble) ChildHover(on bool) {
	if on {
		b.inside++
	} else if b.inside > 0 {
		b.inside--
	}
	b.light()
}

func (b *StripeBubble) light() {
	on := b.over || b.inside > 0
	if on != b.present {
		b.present = on
		if b.parent != nil {
			b.parent(on)
		}
	}
	if !HoverAllowed(b, on) || on == b.on {
		return
	}
	b.on = on
	target := float32(0)
	if on {
		target = 1
	}
	b.hover.to(target, b.paint)
}

// HoverChild is a link or tag inside a bubble that tells the bubble when the
// pointer is on it.
type HoverChild interface {
	SetHoverParent(func(on bool))
}

// AttachHoverChildren hooks every HoverChild inside obj up to hover.
func AttachHoverChildren(obj fyne.CanvasObject, hover func(on bool)) {
	switch o := obj.(type) {
	case HoverChild:
		o.SetHoverParent(hover)
	case *container.ThemeOverride:
		AttachHoverChildren(o.Content, hover)
	case *Tappable:
		AttachHoverChildren(o.Content, hover)
	case *container.Scroll:
		AttachHoverChildren(o.Content, hover)
	case *Wipe:
		AttachHoverChildren(o.Content, hover)
	case *SprintDragCard:
		AttachHoverChildren(o.Content, hover)
	case *fyne.Container:
		for _, child := range o.Objects {
			AttachHoverChildren(child, hover)
		}
	}
}

// HoverLink is a hyperlink that keeps the bubble it sits in lit while the
// pointer is on it.
type HoverLink struct {
	widget.Hyperlink
	parent func(on bool)
}

// NewHoverLink links text to rawURL, or is plain text when the address does
// not parse.
func NewHoverLink(text, rawURL string) fyne.CanvasObject {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return widget.NewLabel(text)
	}
	l := &HoverLink{}
	l.Text = text
	l.URL = u
	l.ExtendBaseWidget(l)
	return l
}

func (l *HoverLink) SetHoverParent(f func(on bool)) { l.parent = f }

func (l *HoverLink) MouseIn(e *desktop.MouseEvent) {
	l.Hyperlink.MouseIn(e)
	if l.parent != nil {
		l.parent(true)
	}
}

func (l *HoverLink) MouseOut() {
	l.Hyperlink.MouseOut()
	if l.parent != nil {
		l.parent(false)
	}
}

// HoldBubble keeps the bubble in obj lit while it is dragged, and keeps every
// other bubble dark. ReleaseBubble ends it.
func HoldBubble(obj fyne.CanvasObject) {
	l := BubbleIn(obj)
	if l == nil {
		return
	}
	l.SetHover(true)
	held = l
}

// ReleaseBubble lets the dragged bubble settle back once it is dropped: the
// pointer is no longer over it, so its hover would otherwise stay on.
func ReleaseBubble() {
	l := held
	held = nil
	if l != nil {
		l.SetHover(false)
	}
}

// BubbleIn finds the bubble a card is drawn with, if any.
func BubbleIn(obj fyne.CanvasObject) Lightable {
	switch o := obj.(type) {
	case Lightable:
		return o
	case *HoverArea:
		return BubbleIn(o.Content)
	case *fyne.Container:
		for _, child := range o.Objects {
			if l := BubbleIn(child); l != nil {
				return l
			}
		}
	}
	return nil
}

func (b *StripeBubble) CreateRenderer() fyne.WidgetRenderer { return &stripeBubbleRenderer{b: b} }

type stripeBubbleRenderer struct{ b *StripeBubble }

func (r *stripeBubbleRenderer) insets() (left, top, right float32) {
	pad := theme.Padding()
	return 3*pad + stripeWidth, 2 * pad, 2 * pad
}

func (r *stripeBubbleRenderer) Layout(size fyne.Size) {
	b := r.b
	b.bg.Move(fyne.NewPos(0, 0))
	b.bg.Resize(size)
	b.placeStripe(size)
	left, top, right := r.insets()
	b.Body.Move(fyne.NewPos(left, top))
	b.Body.Resize(fyne.NewSize(max(size.Width-left-right, 0), max(size.Height-2*top, 0)))
}

func (r *stripeBubbleRenderer) MinSize() fyne.Size {
	left, top, right := r.insets()
	m := r.b.Body.MinSize()
	return fyne.NewSize(m.Width+left+right, m.Height+2*top)
}

func (r *stripeBubbleRenderer) Refresh() {
	r.b.paint(r.b.hover.level)
	r.Layout(r.b.Size())
	r.b.Body.Refresh()
}

func (r *stripeBubbleRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.b.bg, r.b.stripe, r.b.Body}
}
func (r *stripeBubbleRenderer) Destroy() {}

// HoverArea passes the pointer coming and going to the bubble inside it. It
// is for a bubble nothing else watches the pointer for; a card that is itself
// hoverable calls the bubble's SetHover instead, since only the innermost
// hoverable hears the pointer.
type HoverArea struct {
	widget.BaseWidget
	Content fyne.CanvasObject
	bubble  *StripeBubble
}

// HoverBubble is a StripeBubble that lights up under the pointer on its own.
func HoverBubble(accent color.Color, body fyne.CanvasObject) *HoverArea {
	b := NewStripeBubble(accent, body)
	a := &HoverArea{Content: b, bubble: b}
	a.ExtendBaseWidget(a)
	return a
}

// SetHoverParent tells the bubble what it sits in, such as a Kanban column,
// to keep that lit while the pointer is on the bubble.
func (a *HoverArea) SetHoverParent(f func(on bool)) { a.bubble.parent = f }

func (a *HoverArea) MouseIn(*desktop.MouseEvent)    { a.bubble.SetHover(true) }
func (a *HoverArea) MouseMoved(*desktop.MouseEvent) {}
func (a *HoverArea) MouseOut()                      { a.bubble.SetHover(false) }

func (a *HoverArea) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(a.Content)
}

// DragGhost is the small card that follows the pointer while something is
// dragged: its title and minutes on the org's tint, edged in gold. Log Work,
// Kanban and the Sprint Calendar all carry this one.
func DragGhost(title string, minutes int, accent color.Color) *fyne.Container {
	text := Truncate(title, 24)
	if minutes > 0 {
		text = fmt.Sprintf("%s · %dm", text, minutes)
	}
	label := canvas.NewText(text, theme.Color(theme.ColorNameForeground))
	label.TextSize = theme.CaptionTextSize()
	bg := canvas.NewRectangle(BlendColor(theme.Color(theme.ColorNameInputBackground), accent, 0.35))
	bg.CornerRadius = 8
	bg.StrokeColor = theme.Color(theme.ColorNamePrimary)
	bg.StrokeWidth = 1
	ghost := container.NewStack(bg, container.NewPadded(label))
	ghost.Resize(ghost.MinSize()) // nothing lays this out; it sizes itself
	return ghost
}

// CountBadge is a count, in gold, in a small rounded badge; a long count
// widens it rather than touching its edge.
func CountBadge(count int) fyne.CanvasObject {
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameOverlayBackground))
	bg.CornerRadius = 12
	bg.SetMinSize(fyne.NewSize(26, 26))
	label := canvas.NewText(strconv.Itoa(count), widgets.LogoGold)
	label.TextSize = theme.CaptionTextSize()
	label.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(0, 0, 7, 7), container.NewCenter(label)))
}

// ParentTag is a parent issue as a gold tag: muted at rest, bright under the
// pointer, and a click opens the parent on GitHub.
func ParentTag(title, link string) fyne.CanvasObject {
	var open func()
	if u, err := url.Parse(link); err == nil && link != "" {
		open = func() { _ = fyne.CurrentApp().OpenURL(u) }
	}
	return HugTag(NewGoldTag(title, open))
}

// clipBox shows only the part of its objects inside its own bounds. It is a
// Scrollable only because Fyne clips nothing else; it never scrolls, and the
// patched wheel passes straight through it.
type clipBox struct {
	widget.BaseWidget
	objects *fyne.Container
}

func newClipBox(objects ...fyne.CanvasObject) *clipBox {
	c := &clipBox{objects: container.NewWithoutLayout(objects...)}
	c.ExtendBaseWidget(c)
	return c
}

func (c *clipBox) Scrolled(*fyne.ScrollEvent)    {}
func (c *clipBox) CanScrollBy(_, _ float32) bool { return false }
func (c *clipBox) CreateRenderer() fyne.WidgetRenderer {
	return &clipRenderer{c: c}
}

type clipRenderer struct{ c *clipBox }

func (r *clipRenderer) Layout(size fyne.Size)        { r.c.objects.Resize(size) }
func (r *clipRenderer) MinSize() fyne.Size           { return fyne.Size{} }
func (r *clipRenderer) Refresh()                     { canvas.Refresh(r.c) }
func (r *clipRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.c.objects} }
func (r *clipRenderer) Destroy()                     {}

// Ripple spreads a gold ring of light from where area was tapped, drawn in
// layer (an overlay without a layout, above the area) so it plays on even if
// the area is redrawn underneath it straight away.
func Ripple(layer *fyne.Container, area fyne.CanvasObject, at fyne.Position) {
	if layer == nil || area == nil {
		return
	}
	driver := fyne.CurrentApp().Driver()
	origin := driver.AbsolutePositionForObject(area)
	size := area.Size()
	circle := canvas.NewCircle(color.Transparent)
	box := newClipBox(circle)
	box.Move(origin.Subtract(driver.AbsolutePositionForObject(layer)))
	box.Resize(size)
	centre := at.Subtract(origin)
	reach := float32(math.Hypot(float64(max(centre.X, size.Width-centre.X)), float64(max(centre.Y, size.Height-centre.Y))))
	layer.Add(box)
	anim := fyne.NewAnimation(420*time.Millisecond, func(done float32) {
		radius := reach * done
		circle.Move(fyne.NewPos(centre.X-radius, centre.Y-radius))
		circle.Resize(fyne.NewSquareSize(2 * radius))
		circle.FillColor = widgets.WithAlpha(widgets.LogoGold, 0.30*(1-done))
		circle.Refresh()
		if done >= 1 {
			layer.Remove(box)
		}
	})
	anim.Curve = fyne.AnimationEaseOut
	anim.Start()
}

// Wipe uncovers its content from the top down after a delay, so a column of
// cards can arrive one after another. It keeps its full size throughout, so
// nothing around it moves.
type Wipe struct {
	widget.BaseWidget
	Content fyne.CanvasObject
	clip    *clipBox
	share   float32
}

// NewWipe wraps content to be uncovered after delay.
func NewWipe(content fyne.CanvasObject, delay time.Duration) *Wipe {
	w := &Wipe{Content: content, clip: newClipBox(content)}
	w.ExtendBaseWidget(w)
	anim := fyne.NewAnimation(280*time.Millisecond, func(done float32) {
		w.share = done
		w.layout(w.Size())
		canvas.Refresh(w)
	})
	anim.Curve = fyne.AnimationEaseOut
	if delay <= 0 {
		anim.Start()
	} else {
		time.AfterFunc(delay, func() { fyne.Do(anim.Start) })
	}
	return w
}

func (w *Wipe) layout(size fyne.Size) {
	w.Content.Move(fyne.NewPos(0, 0))
	w.Content.Resize(size)
	w.clip.Resize(fyne.NewSize(size.Width, size.Height*w.share))
}

func (w *Wipe) CreateRenderer() fyne.WidgetRenderer { return &wipeRenderer{w: w} }

type wipeRenderer struct{ w *Wipe }

func (r *wipeRenderer) Layout(size fyne.Size) { r.w.layout(size) }
func (r *wipeRenderer) MinSize() fyne.Size    { return r.w.Content.MinSize() }
func (r *wipeRenderer) Refresh() {
	r.w.layout(r.w.Size())
	r.w.Content.Refresh()
}
func (r *wipeRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.w.clip} }
func (r *wipeRenderer) Destroy()                     {}

// HoverRegion hears the pointer anywhere over its content, the cards and links
// in it included, which Fyne alone does not tell it: only the innermost thing
// under the pointer hears it. onChange runs when the pointer comes or goes.
type HoverRegion struct {
	widget.BaseWidget
	Content  fyne.CanvasObject
	over     bool
	inside   int
	on       bool
	onChange func(on bool)
}

func NewHoverRegion(content fyne.CanvasObject, onChange func(on bool)) *HoverRegion {
	r := &HoverRegion{Content: content, onChange: onChange}
	r.ExtendBaseWidget(r)
	AttachHoverChildren(content, r.child)
	return r
}

func (r *HoverRegion) child(on bool) {
	if on {
		r.inside++
	} else if r.inside > 0 {
		r.inside--
	}
	r.update()
}

func (r *HoverRegion) update() {
	on := r.over || r.inside > 0
	if on == r.on {
		return
	}
	r.on = on
	if r.onChange != nil {
		r.onChange(on)
	}
}

func (r *HoverRegion) MouseIn(*desktop.MouseEvent)    { r.over = true; r.update() }
func (r *HoverRegion) MouseMoved(*desktop.MouseEvent) {}
func (r *HoverRegion) MouseOut()                      { r.over = false; r.update() }

func (r *HoverRegion) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.Content)
}
