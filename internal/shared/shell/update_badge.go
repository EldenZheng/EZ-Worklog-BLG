package shell

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// UpdateBadgeText is what the gear says while a newer release is waiting.
const UpdateBadgeText = "Update Available"

// How the badge comes and goes: the gold lights behind the gear first, then
// the capsule grows to the left to show its text.
const (
	updateBadgeDuration = 620 * time.Millisecond
	updateBadgeLight    = 0.3 // share of the way spent lighting the gear
	updateBadgeLead     = 14  // space before the text, inside the capsule
)

// gearButton is the Settings gear: a plain button that also tells the badge
// around it when the pointer comes and goes, which Fyne tells only the button.
type gearButton struct {
	widget.Button
	hover func(on bool)
	tap   func(at fyne.Position)
}

func newGearButton(icon fyne.Resource, tapped func()) *gearButton {
	b := &gearButton{}
	b.Icon = icon
	b.OnTapped = tapped
	b.ExtendBaseWidget(b)
	return b
}

func (b *gearButton) MouseIn(e *desktop.MouseEvent) {
	b.Button.MouseIn(e)
	if b.hover != nil {
		b.hover(true)
	}
}

func (b *gearButton) Tapped(e *fyne.PointEvent) {
	b.Button.Tapped(e)
	if b.tap != nil {
		b.tap(e.Position)
	}
}

func (b *gearButton) MouseOut() {
	b.Button.MouseOut()
	if b.hover != nil {
		b.hover(false)
	}
}

// gearTheme drops the button's own hover wash and tap flash while the badge
// is up: the whole capsule brightens and ripples instead.
type gearTheme struct {
	fyne.Theme
	badge *updateBadge
}

func (t gearTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if (name == theme.ColorNameHover || name == theme.ColorNamePressed) && t.badge.shown {
		return color.Transparent
	}
	return t.Theme.Color(name, variant)
}

// updateBadge wraps the gear. With no update it is just the gear. When one
// arrives a gold capsule lights behind the gear and grows to the left with
// "Update Available", pushing the tabs beside it along; it looks like the
// parent tag, muted at rest and bright gold under the pointer or while
// Settings is open. A click anywhere on it ripples and opens Settings.
type updateBadge struct {
	widget.BaseWidget
	gear     *gearButton
	gearView fyne.CanvasObject
	bg       *canvas.Rectangle
	ripple   *canvas.Rectangle
	rippling *fyne.Animation
	label    *canvas.Text
	clip     *clipBox
	shown    bool    // an update is waiting
	progress float32 // 0 is just the gear, 1 is the capsule fully open
	anim     *fyne.Animation
	hover    Glow
	overSelf bool
	overGear bool
	selected bool // Settings is the open page
	inked    bool // the gear is drawn dark, on bright gold
	onTap    func()
	onInk    func()           // the gear's colour has to change
	relayout func(final bool) // the badge's width changed
}

func newUpdateBadge(gear *gearButton, base fyne.Theme) *updateBadge {
	b := &updateBadge{gear: gear}
	b.gearView = container.NewThemeOverride(gear, gearTheme{Theme: base, badge: b})
	gear.hover = func(on bool) { b.overGear = on; b.pointer() }
	gear.tap = func(at fyne.Position) { b.spread(at.Add(b.gearView.Position())) }
	b.bg = canvas.NewRectangle(color.Transparent)
	b.ripple = canvas.NewRectangle(color.Transparent)
	b.ripple.Hide()
	b.label = canvas.NewText(UpdateBadgeText, color.Transparent)
	b.label.TextStyle = fyne.TextStyle{Bold: true}
	b.clip = newClipBox(b.label)
	b.ExtendBaseWidget(b)
	return b
}

// Shown says whether the badge is up (or on its way up).
func (b *updateBadge) Shown() bool { return b.shown }

// Set raises or lowers the badge, animated.
func (b *updateBadge) Set(show bool) {
	if show == b.shown {
		return
	}
	b.shown = show
	b.pointer() // lit at once if Settings is already open, dark when going
	if b.anim != nil {
		b.anim.Stop()
	}
	from, to := b.progress, float32(0)
	if show {
		to = 1
	}
	span := to - from
	if span < 0 {
		span = -span
	}
	var anim *fyne.Animation
	anim = fyne.NewAnimation(time.Duration(float32(updateBadgeDuration)*span), func(done float32) {
		if b.anim != anim {
			return
		}
		b.progress = from + (to-from)*done
		b.paint()
		if b.relayout != nil {
			b.relayout(done == 1)
		}
	})
	anim.Curve = fyne.AnimationLinear
	b.anim = anim
	anim.Start()
}

// lit is how far the gold behind the gear has come on; open is how far the
// capsule has grown (eased).
func (b *updateBadge) lit() float32 {
	return min(b.progress/updateBadgeLight, 1)
}

func (b *updateBadge) open() float32 {
	x := (b.progress - updateBadgeLight) / (1 - updateBadgeLight)
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	return 1 - (1-x)*(1-x)*(1-x)
}

func (b *updateBadge) labelWidth() float32 {
	return b.label.MinSize().Width + updateBadgeLead
}

func (b *updateBadge) pointer() {
	on := b.shown && (b.overSelf || b.overGear || b.selected)
	target := float32(0)
	if on {
		target = 1
	}
	b.hover.To(target, b.paintHover)
}

// setSelected keeps the capsule lit while Settings is the open page.
func (b *updateBadge) setSelected(on bool) {
	if on != b.selected {
		b.selected = on
		b.pointer()
	}
}

// spread runs a ripple across the capsule from where it was clicked: a pill of
// light that widens from that point to both ends, fading as it goes, so it
// keeps the capsule's round ends.
func (b *updateBadge) spread(at fyne.Position) {
	if !b.shown {
		return
	}
	if b.rippling != nil {
		b.rippling.Stop()
	}
	size := b.Size()
	reach := max(at.X, size.Width-at.X)
	var anim *fyne.Animation
	anim = fyne.NewAnimation(420*time.Millisecond, func(done float32) {
		if b.rippling != anim {
			return
		}
		size := b.Size()
		left := max(at.X-reach*done, 0)
		right := min(at.X+reach*done, size.Width)
		b.ripple.Move(fyne.NewPos(left, 0))
		b.ripple.Resize(fyne.NewSize(right-left, size.Height))
		b.ripple.CornerRadius = size.Height / 2
		b.ripple.FillColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: uint8(0x70 * (1 - done))}
		b.ripple.Show()
		if done >= 1 {
			b.ripple.Hide()
		}
		b.ripple.Refresh()
	})
	anim.Curve = easeOutQuart
	b.rippling = anim
	anim.Start()
}

func (b *updateBadge) paintHover(float32) {
	b.paint()
	inked := b.hover.Level() > 0.5
	if inked != b.inked {
		b.inked = inked
		if b.onInk != nil {
			b.onInk()
		}
	}
}

func scaleAlpha(c color.Color, share float32) color.Color {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	n.A = uint8(float32(n.A)*share + 0.5)
	return n
}

func (b *updateBadge) paint() {
	level := b.hover.Level()
	b.bg.FillColor = scaleAlpha(MixColor(TagRestFill, TagHoverFill, level), b.lit())
	b.label.Color = scaleAlpha(MixColor(TagRestInk, TagHoverInk, level), b.open())
	b.bg.Refresh()
	b.label.Refresh()
}

func (b *updateBadge) MouseIn(*desktop.MouseEvent)    { b.overSelf = true; b.pointer() }
func (b *updateBadge) MouseMoved(*desktop.MouseEvent) {}
func (b *updateBadge) MouseOut()                      { b.overSelf = false; b.pointer() }

func (b *updateBadge) Tapped(e *fyne.PointEvent) {
	if b.shown && b.onTap != nil {
		b.spread(e.Position)
		b.onTap()
	}
}

func (b *updateBadge) Cursor() desktop.Cursor {
	if b.shown {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

func (b *updateBadge) CreateRenderer() fyne.WidgetRenderer { return &updateBadgeRenderer{b: b} }

type updateBadgeRenderer struct{ b *updateBadge }

func (r *updateBadgeRenderer) MinSize() fyne.Size {
	b := r.b
	gear := b.gearView.MinSize()
	return fyne.NewSize(gear.Width+b.labelWidth()*b.open(), gear.Height)
}

func (r *updateBadgeRenderer) Layout(size fyne.Size) {
	b := r.b
	gearWidth := b.gearView.MinSize().Width
	b.gearView.Move(fyne.NewPos(size.Width-gearWidth, 0))
	b.gearView.Resize(fyne.NewSize(gearWidth, size.Height))
	b.bg.Move(fyne.NewPos(0, 0))
	b.bg.Resize(size)
	b.bg.CornerRadius = size.Height / 2
	// The text rides against the gear, so the capsule's growing left edge
	// uncovers it from its end.
	room := max(size.Width-gearWidth, 0)
	b.clip.Move(fyne.NewPos(0, 0))
	b.clip.Resize(fyne.NewSize(room, size.Height))
	text := b.label.MinSize()
	b.label.Resize(text)
	b.label.Move(fyne.NewPos(room-text.Width, (size.Height-text.Height)/2))
}

func (r *updateBadgeRenderer) Refresh() {
	r.b.paint()
	r.Layout(r.b.Size())
}

func (r *updateBadgeRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.b.bg, r.b.ripple, r.b.clip, r.b.gearView}
}

func (r *updateBadgeRenderer) Destroy() {}
