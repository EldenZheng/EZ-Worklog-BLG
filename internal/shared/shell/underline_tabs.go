package shell

import (
	"image/color"
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// UnderlineTabs picks one of a few options, drawn as a row of large labels:
// the picked one bright with a gold line under it, the rest dimmed. Picking
// another slides the line across to it. It keeps RadioGroup's Options,
// Selected, OnChanged and SetSelected, so it stands in for one.
type UnderlineTabs struct {
	widget.BaseWidget
	Options   []string
	Selected  string
	OnChanged func(string)

	items        []*underlineTab
	line, track  *canvas.Rectangle
	lineX, lineW float32
	slide        *fyne.Animation
}

const (
	tabTextGrowth   = 3 // points above the body text
	tabSidePadding  = float32(14)
	tabEndPadding   = float32(10)
	tabLineHeight   = float32(3)
	tabSlideTime    = 300 * time.Millisecond
	tabDimmedShare  = 0.45 // how far an unpicked label fades toward the background
	tabHoveredShare = 0.18
)

// NewUnderlineTabs is a row of options with nothing picked yet.
func NewUnderlineTabs(options []string, changed func(string)) *UnderlineTabs {
	t := &UnderlineTabs{Options: options, OnChanged: changed}
	t.ExtendBaseWidget(t)
	return t
}

// SetSelected picks option and tells OnChanged, unless it is already picked or
// is not one of the options.
func (t *UnderlineTabs) SetSelected(option string) {
	if option == t.Selected || !slices.Contains(t.Options, option) {
		return
	}
	t.Selected = option
	t.paint()
	t.slideLine()
	if t.OnChanged != nil {
		t.OnChanged(option)
	}
}

func (t *UnderlineTabs) CreateRenderer() fyne.WidgetRenderer {
	if t.slide != nil {
		t.slide.Stop()
		t.slide = nil
	}
	t.lineW = 0
	t.line = canvas.NewRectangle(widgets.LogoGold)
	t.line.CornerRadius = tabLineHeight / 2
	t.track = canvas.NewRectangle(theme.Color(theme.ColorNameInputBorder))
	t.items = nil
	objects := []fyne.CanvasObject{t.track}
	for _, option := range t.Options {
		item := newUnderlineTab(t, option)
		t.items = append(t.items, item)
		objects = append(objects, item)
	}
	objects = append(objects, t.line)
	t.paint()
	return &underlineTabsRenderer{t: t, objects: objects}
}

// paint colours each label by whether it is picked or under the pointer.
func (t *UnderlineTabs) paint() {
	ink := theme.Color(theme.ColorNameForeground)
	bg := theme.Color(theme.ColorNameBackground)
	for _, item := range t.items {
		share := float32(tabDimmedShare)
		switch {
		case item.option == t.Selected:
			share = 0
		case item.hovered:
			share = tabHoveredShare
		}
		item.text.Color = BlendColor(ink, bg, share)
		item.text.Refresh()
	}
}

// target is where the line belongs: under the picked label.
func (t *UnderlineTabs) target() (x, w float32, ok bool) {
	for _, item := range t.items {
		if item.option == t.Selected && !item.Size().IsZero() {
			return item.Position().X + tabSidePadding/2, item.Size().Width - tabSidePadding, true
		}
	}
	return 0, 0, false
}

func (t *UnderlineTabs) placeLine(x, w float32) {
	t.lineX, t.lineW = x, w
	if t.line == nil {
		return
	}
	h := t.Size().Height
	t.line.Move(fyne.NewPos(x, h-tabLineHeight))
	t.line.Resize(fyne.NewSize(w, tabLineHeight))
	t.line.Refresh()
}

// slideLine moves the line to the picked label, gliding when it is already
// somewhere on screen.
func (t *UnderlineTabs) slideLine() {
	x, w, ok := t.target()
	if !ok {
		return
	}
	if t.slide != nil {
		t.slide.Stop()
		t.slide = nil
	}
	if t.lineW == 0 || !animationsOn() {
		t.placeLine(x, w)
		return
	}
	fromX, fromW := t.lineX, t.lineW
	var slide *fyne.Animation
	slide = fyne.NewAnimation(tabSlideTime, func(p float32) {
		if t.slide != slide {
			return
		}
		t.placeLine(fromX+(x-fromX)*p, fromW+(w-fromW)*p)
		if p >= 1 {
			t.slide = nil
		}
	})
	slide.Curve = easeOutQuart
	t.slide = slide
	slide.Start()
}

type underlineTabsRenderer struct {
	t       *UnderlineTabs
	objects []fyne.CanvasObject
}

func (r *underlineTabsRenderer) Layout(size fyne.Size) {
	x := float32(0)
	for _, item := range r.t.items {
		m := item.MinSize()
		item.Move(fyne.NewPos(x, 0))
		item.Resize(fyne.NewSize(m.Width, size.Height))
		x += m.Width
	}
	r.t.track.Move(fyne.NewPos(0, size.Height-1))
	r.t.track.Resize(fyne.NewSize(size.Width, 1))
	if r.t.slide == nil {
		if x, w, ok := r.t.target(); ok {
			r.t.placeLine(x, w)
		}
	}
}

func (r *underlineTabsRenderer) MinSize() fyne.Size {
	size := fyne.Size{}
	for _, item := range r.t.items {
		m := item.MinSize()
		size.Width += m.Width
		size.Height = max(size.Height, m.Height)
	}
	return size
}

func (r *underlineTabsRenderer) Refresh() {
	r.t.line.FillColor = widgets.LogoGold
	r.t.track.FillColor = theme.Color(theme.ColorNameInputBorder)
	r.t.paint()
	r.Layout(r.t.Size())
	canvas.Refresh(r.t)
}

func (r *underlineTabsRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *underlineTabsRenderer) Destroy() {
	if r.t.slide != nil {
		r.t.slide.Stop()
		r.t.slide = nil
	}
}

// underlineTab is one label in the row.
type underlineTab struct {
	widget.BaseWidget
	owner   *UnderlineTabs
	option  string
	text    *canvas.Text
	hovered bool
}

func newUnderlineTab(owner *UnderlineTabs, option string) *underlineTab {
	text := canvas.NewText(option, color.Transparent)
	text.TextSize = theme.TextSize() + tabTextGrowth
	text.TextStyle = fyne.TextStyle{Bold: true}
	item := &underlineTab{owner: owner, option: option, text: text}
	item.ExtendBaseWidget(item)
	return item
}

func (i *underlineTab) Tapped(*fyne.PointEvent) { i.owner.SetSelected(i.option) }
func (i *underlineTab) Cursor() desktop.Cursor  { return desktop.PointerCursor }

func (i *underlineTab) MouseIn(*desktop.MouseEvent) {
	i.hovered = true
	i.owner.paint()
}

func (i *underlineTab) MouseMoved(*desktop.MouseEvent) {}

func (i *underlineTab) MouseOut() {
	i.hovered = false
	i.owner.paint()
}

func (i *underlineTab) CreateRenderer() fyne.WidgetRenderer {
	return &underlineTabRenderer{i: i}
}

type underlineTabRenderer struct{ i *underlineTab }

func (r *underlineTabRenderer) Layout(size fyne.Size) {
	m := r.i.text.MinSize()
	r.i.text.Move(fyne.NewPos((size.Width-m.Width)/2, (size.Height-tabLineHeight-m.Height)/2))
	r.i.text.Resize(m)
}

func (r *underlineTabRenderer) MinSize() fyne.Size {
	m := r.i.text.MinSize()
	return fyne.NewSize(m.Width+tabSidePadding*2, m.Height+tabEndPadding*2+tabLineHeight)
}

func (r *underlineTabRenderer) Refresh() {
	r.i.text.TextSize = theme.TextSize() + tabTextGrowth
	r.Layout(r.i.Size())
	canvas.Refresh(r.i)
}

func (r *underlineTabRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.i.text} }
func (r *underlineTabRenderer) Destroy()                     {}
