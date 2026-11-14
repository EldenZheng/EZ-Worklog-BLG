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
// the picked one gold with a gold line under it, the rest dimmed. Picking
// another slides the line across to it. It keeps RadioGroup's Options,
// Selected, OnChanged and SetSelected, so it stands in for one.
type UnderlineTabs struct {
	widget.BaseWidget
	Options   []string
	Selected  string
	OnChanged func(string)
	// Trailing is how many of the last options stand at the right end of the
	// row, apart from the rest, when the row is wider than they all need.
	Trailing int
	// Inks gives an option its own colour while it is not picked, in place of
	// the dimmed foreground.
	Inks map[string]color.Color
	// Action is an option that does something rather than being picked, such
	// as a + at the end that adds another: tapping it runs OnAction and leaves
	// the pick where it was.
	Action   string
	OnAction func()
	// OnMenu, when set, gives the picked option a drop-down arrow: tapping the
	// picked option again runs OnMenu with the option and its label, to drop
	// a menu from under it. Tapping another option picks it as usual.
	OnMenu func(option string, anchor fyne.CanvasObject)
	// OnReorder, when set, lets the options be dragged along the row into a
	// new order, which it is handed when one is dropped somewhere new. The
	// Action option stays where it is, at the end.
	OnReorder func(order []string)

	items       []*underlineTab
	line, track *canvas.Rectangle
	// drop marks where a dragged option would land; order is what dropping
	// it there makes, nil while that is the order already.
	drop         *canvas.Rectangle
	order        []string
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
	for _, item := range t.items {
		target := float32(0)
		if item.option == option {
			target = 1
		}
		item.ease(&item.pick, target)
	}
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
		if option == t.Selected {
			item.pick.level = 1 // already picked: no fade on first draw
		}
		t.items = append(t.items, item)
		objects = append(objects, item)
	}
	t.drop = canvas.NewRectangle(widgets.LogoGold)
	t.drop.CornerRadius = 1.5
	t.drop.Hide()
	objects = append(objects, t.drop, t.line)
	t.paint()
	return &underlineTabsRenderer{t: t, objects: objects}
}

// paint colours each label by how far it is picked and under the pointer.
func (t *UnderlineTabs) paint() {
	for _, item := range t.items {
		t.paintItem(item)
	}
}

// paintItem colours one label: dimmed at rest (or its own ink), brighter under
// the pointer, and the line's gold once picked. Both levels ease, so the
// colour fades between them rather than jumping.
func (t *UnderlineTabs) paintItem(item *underlineTab) {
	ink := theme.Color(theme.ColorNameForeground)
	bg := theme.Color(theme.ColorNameBackground)
	share := tabDimmedShare + (tabHoveredShare-tabDimmedShare)*item.hover.level
	if own, ok := t.Inks[item.option]; ok {
		ink = own // dimmed the same as the others, so it reads as unfocused
	}
	rest := BlendColor(ink, bg, share)
	item.text.Color = MixColor(rest, widgets.LogoGold, item.pick.level)
	item.text.Refresh()
	// The arrow fades in with the pick, so only the picked option shows one.
	if item.arrow != nil {
		item.arrow.Translucency = float64(1 - item.pick.level)
		item.arrow.Refresh()
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
	x, need := float32(0), r.MinSize().Width
	lead := len(r.t.items) - min(max(r.t.Trailing, 0), len(r.t.items))
	for i, item := range r.t.items {
		if i == lead && size.Width > need {
			x += size.Width - need // the trailing ones go to the right end
		}
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
	owner       *UnderlineTabs
	option      string
	text        *canvas.Text
	arrow       *canvas.Image // the drop-down arrow, gold like the picked label
	hover, pick Glow
}

// hasMenu says whether the option carries a drop-down arrow.
func (i *underlineTab) hasMenu() bool {
	return i.owner.OnMenu != nil && i.option != i.owner.Action
}

const (
	tabArrowSize = float32(18)
	tabArrowGap  = float32(2)
)

// tabArrow is the drop-down arrow beside a tab with a menu: the same filled
// triangle a drop-down field shows, in the logo's gold.
var tabArrow = fyne.NewStaticResource("tab-arrow.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#cda236" d="M7 10l5 5 5-5z"/></svg>`))

// ease fades one of the label's levels to target, or sets it at once with
// animations off.
func (i *underlineTab) ease(g *Glow, target float32) {
	if !animationsOn() {
		if g.anim != nil {
			g.anim.Stop()
			g.anim = nil
		}
		g.level = target
		i.owner.paintItem(i)
		return
	}
	g.To(target, func(float32) { i.owner.paintItem(i) })
}

func newUnderlineTab(owner *UnderlineTabs, option string) *underlineTab {
	text := canvas.NewText(option, color.Transparent)
	text.TextSize = theme.TextSize() + tabTextGrowth
	text.TextStyle = fyne.TextStyle{Bold: true}
	item := &underlineTab{owner: owner, option: option, text: text}
	item.arrow = canvas.NewImageFromResource(tabArrow)
	item.arrow.FillMode = canvas.ImageFillContain
	item.arrow.Translucency = 1
	item.ExtendBaseWidget(item)
	return item
}

func (i *underlineTab) Tapped(*fyne.PointEvent) {
	if o := i.owner; o.OnAction != nil && i.option == o.Action {
		o.OnAction()
		return
	}
	if i.hasMenu() && i.option == i.owner.Selected {
		i.owner.OnMenu(i.option, i)
		return
	}
	i.owner.SetSelected(i.option)
}
func (i *underlineTab) Cursor() desktop.Cursor { return desktop.PointerCursor }

// Dragged moves the drop mark to where the option would land.
func (i *underlineTab) Dragged(e *fyne.DragEvent) {
	t := i.owner
	if t.OnReorder == nil || i.option == t.Action || t.drop == nil {
		return
	}
	x := i.Position().X + e.Position.X
	var spans []ChipSpan
	var current []string
	for _, item := range t.items {
		if item.option == t.Action {
			continue
		}
		left := item.Position().X
		spans = append(spans, ChipSpan{item.option, left, left + item.Size().Width})
		current = append(current, item.option)
	}
	order, lineX := LegendSlot(spans, i.option, x, 0)
	t.order = nil
	t.drop.Hide()
	if !slices.Equal(order, current) {
		t.order = order
		t.drop.Move(fyne.NewPos(lineX-1.5, tabEndPadding/2))
		t.drop.Resize(fyne.NewSize(3, t.Size().Height-tabEndPadding))
		t.drop.Show()
	}
	t.drop.Refresh()
}

// DragEnd hands the new order over, the Action option back on the end.
func (i *underlineTab) DragEnd() {
	t := i.owner
	if t.drop != nil {
		t.drop.Hide()
	}
	order := t.order
	t.order = nil
	if order == nil || t.OnReorder == nil {
		return
	}
	if t.Action != "" && slices.Contains(t.Options, t.Action) {
		order = append(order, t.Action)
	}
	t.OnReorder(order)
}

func (i *underlineTab) MouseIn(*desktop.MouseEvent) { i.ease(&i.hover, 1) }

func (i *underlineTab) MouseMoved(*desktop.MouseEvent) {}

func (i *underlineTab) MouseOut() { i.ease(&i.hover, 0) }

func (i *underlineTab) CreateRenderer() fyne.WidgetRenderer {
	return &underlineTabRenderer{i: i}
}

type underlineTabRenderer struct{ i *underlineTab }

// arrowRoom is the width the drop-down arrow takes beside the label.
func (r *underlineTabRenderer) arrowRoom() float32 {
	if r.i.hasMenu() {
		return tabArrowGap + tabArrowSize
	}
	return 0
}

func (r *underlineTabRenderer) Layout(size fyne.Size) {
	m := r.i.text.MinSize()
	x := (size.Width - m.Width - r.arrowRoom()) / 2
	y := (size.Height - tabLineHeight - m.Height) / 2
	r.i.text.Move(fyne.NewPos(x, y))
	r.i.text.Resize(m)
	// The arrow sits just after the label, centred on its line.
	r.i.arrow.Move(fyne.NewPos(x+m.Width+tabArrowGap, y+(m.Height-tabArrowSize)/2))
	r.i.arrow.Resize(fyne.NewSquareSize(tabArrowSize))
	if r.i.hasMenu() {
		r.i.arrow.Show()
	} else {
		r.i.arrow.Hide()
	}
}

func (r *underlineTabRenderer) MinSize() fyne.Size {
	m := r.i.text.MinSize()
	return fyne.NewSize(m.Width+r.arrowRoom()+tabSidePadding*2, m.Height+tabEndPadding*2+tabLineHeight)
}

func (r *underlineTabRenderer) Refresh() {
	r.i.text.TextSize = theme.TextSize() + tabTextGrowth
	r.Layout(r.i.Size())
	canvas.Refresh(r.i)
}

func (r *underlineTabRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.i.text, r.i.arrow}
}
func (r *underlineTabRenderer) Destroy() {}
