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

// FadeIconButton is a quiet icon button whose hover wash fades in and out
// over iconHoverFade instead of snapping on, for an icon on a card such as a saved
// entry's Delete. It keeps the card it sits on lit while the pointer is on it
// (SetHoverParent), as a HoverButton does.
type FadeIconButton struct {
	widget.BaseWidget
	OnTapped func()
	icon     *canvas.Image
	wash     *canvas.Rectangle
	glow     Glow
	parent   func(on bool)
}

// NewFadeIconButton is an icon button running tapped on a click.
func NewFadeIconButton(icon fyne.Resource, tapped func()) *FadeIconButton {
	b := &FadeIconButton{OnTapped: tapped}
	b.glow.Duration = iconHoverFade
	b.icon = canvas.NewImageFromResource(icon)
	b.icon.FillMode = canvas.ImageFillContain
	b.wash = canvas.NewRectangle(color.Transparent)
	b.wash.CornerRadius = theme.Size(theme.SizeNameButtonRadius)
	b.ExtendBaseWidget(b)
	return b
}

// Disabled is never: the button is shown only while it can be used.
func (b *FadeIconButton) Disabled() bool { return false }

func (b *FadeIconButton) SetHoverParent(f func(on bool)) { b.parent = f }

// iconHoverFade is how long the Delete's gold takes to come and go: gentle,
// yet quick enough to follow the pointer.
const iconHoverFade = 300 * time.Millisecond

// paint washes the button level of the way to a quiet button's hover: the
// theme's gold button colour with the hover tint over it, as Fyne draws it.
// Only its strength fades: blending from clear (black with no alpha) dragged
// it through a dark brown on the way out.
func (b *FadeIconButton) paint(level float32) {
	c := color.NRGBAModel.Convert(QuietHoverColor()).(color.NRGBA)
	c.A = uint8(float32(c.A)*max(0, min(1, level)) + 0.5)
	b.wash.FillColor = c
	b.wash.Refresh()
}

// QuietHoverColor is the fill a quiet (low importance) button takes under the
// pointer: the button colour with the hover colour laid over it.
func QuietHoverColor() color.Color {
	under := color.NRGBAModel.Convert(theme.Color(theme.ColorNameButton)).(color.NRGBA)
	over := color.NRGBAModel.Convert(theme.Color(theme.ColorNameHover)).(color.NRGBA)
	a := float32(over.A) / 0xff
	mix := func(o, u uint8) uint8 { return uint8(float32(o)*a + float32(u)*(1-a) + 0.5) }
	return color.NRGBA{R: mix(over.R, under.R), G: mix(over.G, under.G), B: mix(over.B, under.B), A: under.A}
}

func (b *FadeIconButton) MouseIn(*desktop.MouseEvent) {
	b.glow.To(1, b.paint)
	if b.parent != nil {
		b.parent(true)
	}
}

func (b *FadeIconButton) MouseMoved(*desktop.MouseEvent) {}

func (b *FadeIconButton) MouseOut() {
	b.glow.To(0, b.paint)
	if b.parent != nil {
		b.parent(false)
	}
}

func (b *FadeIconButton) Tapped(*fyne.PointEvent) {
	if b.OnTapped != nil {
		b.OnTapped()
	}
}

func (b *FadeIconButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (b *FadeIconButton) CreateRenderer() fyne.WidgetRenderer { return &fadeIconRenderer{b: b} }

type fadeIconRenderer struct{ b *FadeIconButton }

// Layout matches an icon-only button: the icon with an inner padding round it.
func (r *fadeIconRenderer) Layout(size fyne.Size) {
	r.b.wash.Move(fyne.Position{})
	r.b.wash.Resize(size)
	side := theme.IconInlineSize()
	r.b.icon.Resize(fyne.NewSquareSize(side))
	r.b.icon.Move(fyne.NewPos((size.Width-side)/2, (size.Height-side)/2))
}

func (r *fadeIconRenderer) MinSize() fyne.Size {
	return fyne.NewSquareSize(theme.IconInlineSize() + 2*theme.InnerPadding())
}

func (r *fadeIconRenderer) Refresh() {
	r.b.icon.Refresh()
	r.b.wash.Refresh()
}

func (r *fadeIconRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.b.wash, r.b.icon}
}

func (r *fadeIconRenderer) Destroy() {}

// FadeHover gives a quiet text button the Delete icon's hover: the same gold
// wash, fading in and out over iconHoverFade instead of snapping on. The
// button draws no hover of its own; the wash behind it does.
func FadeHover(b *HoverButton) fyne.CanvasObject {
	wash := canvas.NewRectangle(color.Transparent)
	wash.CornerRadius = theme.Size(theme.SizeNameButtonRadius)
	glow := &Glow{Duration: iconHoverFade}
	paint := func(level float32) {
		c := color.NRGBAModel.Convert(QuietHoverColor()).(color.NRGBA)
		c.A = uint8(float32(c.A)*max(0, min(1, level)) + 0.5)
		wash.FillColor = c
		wash.Refresh()
	}
	b.hoverHook = func(on bool) {
		target := float32(0)
		if on {
			target = 1
		}
		glow.to(target, paint)
	}
	return container.NewStack(wash, container.NewThemeOverride(b, noHoverTheme{theme.Current()}))
}

// noHoverTheme leaves a quiet button clear under the pointer and in focus, for
// a button whose hover is drawn behind it.
type noHoverTheme struct{ fyne.Theme }

func (t noHoverTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameButton, theme.ColorNameHover, theme.ColorNameFocus:
		return color.Transparent
	}
	return t.Theme.Color(name, v)
}
