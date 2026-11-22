package shell

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// The top menu's hover is the Projects list's and the Kanban side list's: a
// soft gold pill fades in on the tab under the pointer, glides from tab to tab
// as the pointer moves along the bar, and fades out when it leaves. The gear
// takes the same hover and keeps its own click (see tab_look.go for a tab's).

// tabButton is a menu tab that tells the bar when the pointer is on it and
// where a click landed.
type tabButton struct {
	widget.Button
	hover func(on bool)
	tap   func(at fyne.Position)
}

func newTabButton(text string, icon fyne.Resource, tapped func()) *tabButton {
	b := &tabButton{}
	b.Text, b.Icon, b.OnTapped = text, icon, tapped
	b.ExtendBaseWidget(b)
	return b
}

func (b *tabButton) MouseIn(e *desktop.MouseEvent) {
	b.Button.MouseIn(e)
	if b.hover != nil {
		b.hover(true)
	}
}

func (b *tabButton) MouseOut() {
	b.Button.MouseOut()
	if b.hover != nil {
		b.hover(false)
	}
}

func (b *tabButton) Tapped(e *fyne.PointEvent) {
	// Measured before the click restyles the tab.
	if b.tap != nil && e != nil {
		b.tap(e.AbsolutePosition)
	}
	b.Button.Tapped(e)
}

// tabHover is the bar's travelling pill, drawn over the tabs.
type tabHover struct {
	pill    *widgets.HoverPill
	layer   fyne.CanvasObject
	hovered fyne.CanvasObject
}

// pillLayer holds the pill and keeps it on its tab when the bar is laid out
// again.
type pillLayer struct{ h *tabHover }

func (l pillLayer) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.Size{} }

func (l pillLayer) Layout([]fyne.CanvasObject, fyne.Size) {
	if l.h.hovered != nil {
		pos, size := l.h.frame(l.h.hovered)
		l.h.pill.Fit(pos, size)
	}
}

// frame is where the pill stands over tab, in the layer.
func (h *tabHover) frame(tab fyne.CanvasObject) (fyne.Position, fyne.Size) {
	d := fyne.CurrentApp().Driver()
	size := tab.Size()
	h.pill.Rect.CornerRadius = min(PillTheme{}.Size(theme.SizeNameButtonRadius), size.Height/2)
	return d.AbsolutePositionForObject(tab).Subtract(d.AbsolutePositionForObject(h.layer)), size
}

// over lights tab as the pointer arrives on it, and lets the pill fade as the
// pointer leaves the tab it is on.
func (h *tabHover) over(tab fyne.CanvasObject, on bool) {
	if !on {
		if h.hovered == tab {
			h.hovered = nil
			h.pill.MoveTo(fyne.Position{}, fyne.Size{}, false)
		}
		return
	}
	h.hovered = tab
	pos, size := h.frame(tab)
	h.pill.MoveTo(pos, size, true)
}

// tabBarTheme leaves the hover to the pill: the tabs' own hover wash is off.
type tabBarTheme struct{ fyne.Theme }

func (t tabBarTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameHover {
		return color.Transparent
	}
	return t.Theme.Color(name, variant)
}
