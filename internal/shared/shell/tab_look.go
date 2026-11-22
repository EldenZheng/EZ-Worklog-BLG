package shell

import (
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/shared/widgets"
)

// A tab picked does not snap to its selected look: the gold ripples out from
// where it was clicked, its words darken as the gold reaches them, and the
// tab let go of drains back the same way, as a sprint row does on the
// Kanban's side list. The tabs' own fills are off; the bar paints them
// behind the tabs so the gold can spread under the words.

// tabSelectedInk is a selected tab's words, dark on the gold.
var tabSelectedInk = color.NRGBA{R: 0x1c, G: 0x18, B: 0x0d, A: 0xff}

// tabLook is one tab's surface: its resting fill and the gold spreading over
// it, share of the way to selected.
type tabLook struct {
	tab       fyne.CanvasObject
	bg, fill  *canvas.Rectangle
	over      *container.ThemeOverride
	base      fyne.Theme
	share     float32
	centre    fyne.Position
	hasCentre bool
	anim      *fyne.Animation
	// Dropdown choices share the menu's spread, with their own rounding and
	// a check mark that follows the selected share.
	radius  float32
	onShare func(float32)
	// at and size are where the tab stands in the layer behind the bar.
	at   fyne.Position
	size fyne.Size
}

func newTabLook(tab fyne.CanvasObject, base fyne.Theme) *tabLook {
	l := &tabLook{tab: tab, base: base}
	v := fyne.CurrentApp().Settings().ThemeVariant()
	l.bg = canvas.NewRectangle(base.Color(theme.ColorNameButton, v))
	l.fill = canvas.NewRectangle(widgets.LogoGold)
	l.fill.Hide()
	l.over = container.NewThemeOverride(tab, tabInkTheme{Theme: base, ink: base.Color(theme.ColorNameForeground, v)})
	return l
}

// tabInkTheme draws a tab with no fill of its own, its words in ink.
type tabInkTheme struct {
	fyne.Theme
	ink color.Color
}

func (t tabInkTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameButton, theme.ColorNamePrimary, theme.ColorNameHover:
		return color.Transparent
	case theme.ColorNameForeground, theme.ColorNameForegroundOnPrimary:
		return t.ink
	}
	return t.Theme.Color(name, variant)
}

// set puts the look straight at share, with no motion.
func (l *tabLook) set(share float32) {
	l.stop()
	l.share, l.hasCentre = share, false
	l.paint()
}

func (l *tabLook) stop() {
	if l.anim != nil {
		l.anim.Stop()
		l.anim = nil
	}
}

// to moves the look to selected (1) or rest (0) over a ripple's time, the gold
// spreading from centre (in the tab) when given, else from the middle.
func (l *tabLook) to(target float32, centre *fyne.Position) {
	l.stop()
	l.hasCentre = centre != nil
	if centre != nil {
		l.centre = *centre
	}
	if !fyne.CurrentApp().Settings().ShowAnimations() {
		l.share = target
		l.paint()
		return
	}
	from := l.share
	// Timed by the frames drawn, so the gold is still seen spreading after
	// the click has drawn a heavy page (see steadyAnimation).
	var anim *fyne.Animation
	anim = steadyAnimation(RippleTime, easeOutQuart, func(eased float32) {
		if l.anim != anim {
			return
		}
		l.share = from + (target-from)*eased
		l.paint()
		if eased >= 1 {
			l.anim = nil
		}
	})
	l.anim = anim
	anim.Start()
}

// place puts the look behind its tab, at pos of size in the layer.
func (l *tabLook) place(pos fyne.Position, size fyne.Size) {
	l.at, l.size = pos, size
	l.bg.Move(pos)
	l.bg.Resize(size)
	l.bg.CornerRadius = l.corner(size)
	l.bg.Refresh()
	l.placeFill()
	if l.onShare != nil {
		l.onShare(l.share)
	}
}

func (l *tabLook) corner(size fyne.Size) float32 {
	if l.radius > 0 {
		return min(l.radius, size.Height/2)
	}
	return tabRadius(size)
}

func tabRadius(size fyne.Size) float32 {
	return min(PillTheme{}.Size(theme.SizeNameButtonRadius), size.Height/2)
}

// paint inks the words share of the way to dark and spreads the gold.
func (l *tabLook) paint() {
	v := fyne.CurrentApp().Settings().ThemeVariant()
	l.over.Theme = tabInkTheme{Theme: l.base, ink: MixColor(l.base.Color(theme.ColorNameForeground, v), tabSelectedInk, l.share)}
	l.over.Refresh()
	l.placeFill()
	if l.onShare != nil {
		l.onShare(l.share)
	}
}

// placeFill spreads the gold over the tab: a circle from the centre, cut to
// the tab and rounded as the tab is where it meets the tab's ends.
func (l *tabLook) placeFill() {
	size := l.size
	if size.Width <= 0 || size.Height <= 0 {
		l.fill.Hide()
		return
	}
	c := fyne.NewPos(size.Width/2, size.Height/2)
	if l.hasCentre {
		c = fyne.NewPos(min(max(l.centre.X, 0), size.Width), min(max(l.centre.Y, 0), size.Height))
	}
	reach := float32(math.Hypot(float64(max(c.X, size.Width-c.X)), float64(max(c.Y, size.Height-c.Y))))
	SpreadIn(l.fill, l.at, size, c, reach*l.share, l.corner(size))
}

// lookLayer holds the tabs' surfaces behind the bar, each under its tab.
type lookLayer struct{ t *pillTabs }

func (l lookLayer) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.Size{} }

func (l lookLayer) Layout([]fyne.CanvasObject, fyne.Size) { l.t.placeLooks() }

// placeLooks stands each tab's surface under the tab.
func (t *pillTabs) placeLooks() {
	if t.back == nil || fyne.CurrentApp() == nil {
		return
	}
	d := fyne.CurrentApp().Driver()
	origin := d.AbsolutePositionForObject(t.back)
	for _, l := range t.looks {
		if l != nil {
			l.place(d.AbsolutePositionForObject(l.tab).Subtract(origin), l.tab.Size())
		}
	}
}

// barStack lays the bar out first, then the surfaces behind it and the hover
// over it, which both stand where the bar has just put the tabs.
type barStack struct{}

func (barStack) MinSize(objects []fyne.CanvasObject) fyne.Size { return objects[1].MinSize() }

func (barStack) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, i := range []int{1, 0, 2} {
		objects[i].Move(fyne.Position{})
		objects[i].Resize(size)
	}
}
