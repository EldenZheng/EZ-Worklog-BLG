package widgets

import (
	"image/color"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Native CheckGroup gives every horizontal item the longest label's width.
// Keep native checks but pack them at their own widths in the column footer.
//
// Under the pointer a soft gold pill fades in behind a check, and slides from
// one check to the next as the pointer moves along the row. A click draws the
// tick in or out (see TickMotion) rather than snapping it.
//
// With OnReorder set, the options can also be dragged along the row into a
// new order: the check being carried dims where it stood, a gold line shows
// where it would land, and OnReorder is handed the new order when it is
// dropped somewhere new. OnLift and OnLand let the page draw a ghost of it
// under the pointer, above anything that would clip the row.
type CompactCheckGroup struct {
	widget.CheckGroup
	OnReorder func(order []string)
	// OnLift runs on every move of a drag with the option carried and where
	// the pointer is on the window; OnLand once when it is let go, saying
	// whether the order changed.
	OnLift func(option string, at fyne.Position)
	OnLand func(moved bool)

	drop     *canvas.Rectangle
	dragging string   // the option being dragged, "" when none
	order    []string // the order a drop here makes; nil when it changes nothing
	items    []fyne.CanvasObject

	// pill is the hover highlight and hovered the check it stands behind, -1
	// for none. pillMotion supplies the shared fade and slide.
	pill       *canvas.Rectangle
	hovered    int
	pillMotion *HoverPill
	// lift dims the check being carried.
	lift *canvas.Rectangle
	// ticks are the renderer's tick motions, one per check.
	ticks []*TickMotion
}

func NewCompactCheckGroup(options []string, changed func([]string)) *CompactCheckGroup {
	g := &CompactCheckGroup{hovered: -1}
	g.Options = options
	g.Horizontal = true
	g.OnChanged = changed
	g.ExtendBaseWidget(g)
	return g
}

func (g *CompactCheckGroup) CreateRenderer() fyne.WidgetRenderer {
	g.drop = canvas.NewRectangle(LogoGold)
	g.drop.CornerRadius = 1.5
	g.drop.Hide()
	g.pillMotion = NewHoverPill()
	g.pill = g.pillMotion.Rect
	g.lift = canvas.NewRectangle(color.Transparent)
	g.lift.CornerRadius = 8
	g.lift.Hide()
	return &compactChecksRenderer{WidgetRenderer: g.CheckGroup.CreateRenderer(), group: g}
}

// pillFrame is where the hover pill stands behind item i.
func (g *CompactCheckGroup) pillFrame(i int) (fyne.Position, fyne.Size) {
	item := g.items[i]
	pad := theme.Padding()
	return fyne.NewPos(item.Position().X-pad, 1), fyne.NewSize(item.Size().Width+pad*2, g.Size().Height-2)
}

// hover moves the pill to check i, or fades it out for -1. Coming from
// nowhere it fades in where it lands; from another check it slides across.
func (g *CompactCheckGroup) hover(i int) {
	if g.pill == nil || i >= len(g.items) {
		return
	}
	g.hovered = i
	var pos fyne.Position
	var size fyne.Size
	if i >= 0 {
		pos, size = g.pillFrame(i)
	}
	g.pillMotion.MoveTo(pos, size, i >= 0)
}

// Dragged moves the drop line to where the dragged option would land.
func (g *CompactCheckGroup) Dragged(e *fyne.DragEvent) {
	if g.OnReorder == nil || g.drop == nil || len(g.items) != len(g.Options) {
		return
	}
	if g.dragging == "" {
		start := e.Position.Subtract(e.Dragged).X
		for i, item := range g.items {
			if start >= item.Position().X && start < item.Position().X+item.Size().Width {
				g.dragging = g.Options[i]
				// The check lifts out of the row: dimmed where it stood.
				pos, size := g.pillFrame(i)
				g.lift.Move(pos)
				g.lift.Resize(size)
				g.lift.FillColor = WithAlpha(theme.Color(theme.ColorNameBackground), 0.6)
				g.lift.StrokeColor = WithAlpha(LogoGold, 0.7)
				g.lift.StrokeWidth = 1
				g.lift.Show()
				g.lift.Refresh()
			}
		}
		if g.dragging == "" {
			return
		}
	}
	if g.OnLift != nil {
		g.OnLift(g.dragging, e.AbsolutePosition)
	}
	// The others in their order, and the slot whose middle the pointer passed.
	var others []int
	for i, name := range g.Options {
		if name != g.dragging {
			others = append(others, i)
		}
	}
	slot := 0
	for slot < len(others) {
		item := g.items[others[slot]]
		if item.Position().X+item.Size().Width/2 >= e.Position.X {
			break
		}
		slot++
	}
	var order []string
	for k, i := range others {
		if k == slot {
			order = append(order, g.dragging)
		}
		order = append(order, g.Options[i])
	}
	if slot == len(others) {
		order = append(order, g.dragging)
	}
	g.order = nil
	g.drop.Hide()
	if !slices.Equal(order, g.Options) {
		g.order = order
		x := float32(0)
		if slot < len(others) {
			x = g.items[others[slot]].Position().X - theme.Padding()/2
		} else if len(others) > 0 {
			last := g.items[others[len(others)-1]]
			x = last.Position().X + last.Size().Width + theme.Padding()/2
		}
		g.drop.Move(fyne.NewPos(x-1.5, 0))
		g.drop.Resize(fyne.NewSize(3, g.Size().Height))
		g.drop.Show()
	}
	g.drop.Refresh()
}

// DragEnd hands over the new order, if the option was dropped somewhere new.
func (g *CompactCheckGroup) DragEnd() {
	carried := g.dragging != ""
	g.dragging = ""
	if g.drop != nil {
		g.drop.Hide()
		g.lift.Hide()
		canvas.Refresh(g)
	}
	order := g.order
	g.order = nil
	if carried && g.OnLand != nil {
		g.OnLand(order != nil)
	}
	if order != nil && g.OnReorder != nil {
		g.OnReorder(order)
	}
}

// checkCatcher lies over one check and takes the pointer for it: a click
// still ticks the check, while hover and drags go to the group's own motion.
// Fyne sends hover only to the topmost object, so the check under it would
// otherwise keep the pointer to itself.
type checkCatcher struct {
	widget.BaseWidget
	group *CompactCheckGroup
	index int
}

func newCheckCatcher(g *CompactCheckGroup, i int) *checkCatcher {
	c := &checkCatcher{group: g, index: i}
	c.ExtendBaseWidget(c)
	return c
}

func (c *checkCatcher) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

func (c *checkCatcher) check() *widget.Check {
	if c.index < len(c.group.items) {
		check, _ := c.group.items[c.index].(*widget.Check)
		return check
	}
	return nil
}

func (c *checkCatcher) Tapped(e *fyne.PointEvent) {
	if check := c.check(); check != nil && !check.Disabled() && !c.group.Disabled() {
		was := check.Checked
		check.Tapped(e)
		if ticks := c.group.ticks; check.Checked != was && c.index < len(ticks) {
			ticks[c.index].Play(check.Checked)
		}
	}
}

func (c *checkCatcher) MouseIn(*desktop.MouseEvent)    { c.group.hover(c.index) }
func (c *checkCatcher) MouseMoved(*desktop.MouseEvent) {}
func (c *checkCatcher) MouseOut() {
	if c.group.hovered == c.index && c.group.dragging == "" {
		c.group.hover(-1)
	}
}

// Cursor is the pointing hand: the check is clicked far more than dragged,
// though a drag still reorders.
func (c *checkCatcher) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (c *checkCatcher) Dragged(e *fyne.DragEvent) {
	moved := *e
	moved.Position = e.Position.Add(c.Position())
	c.group.Dragged(&moved)
}

func (c *checkCatcher) DragEnd() {
	c.group.DragEnd()
	c.group.hover(-1)
}

type compactChecksRenderer struct {
	fyne.WidgetRenderer
	group    *CompactCheckGroup
	catchers []fyne.CanvasObject
	// ticks draw each check's tick in and out over its icon.
	ticks []*TickMotion
}

func (r *compactChecksRenderer) MinSize() fyne.Size {
	s := fyne.Size{}
	objects := r.checks()
	for _, o := range objects {
		m := o.MinSize()
		s.Width += m.Width
		s.Height = fyne.Max(s.Height, m.Height)
	}
	if len(objects) > 1 {
		s.Width += float32(len(objects)-1) * theme.Padding()
	}
	return s
}

// Objects are the hover pill, the checks, the catchers over them, and the
// lift and drop line drawn over all.
func (r *compactChecksRenderer) Objects() []fyne.CanvasObject {
	objects := []fyne.CanvasObject{r.group.pill}
	objects = append(objects, r.checks()...)
	for _, t := range r.ticks {
		objects = append(objects, t.Objects()...)
	}
	objects = append(objects, r.catchers...)
	return append(objects, r.group.lift, r.group.drop)
}

func (r *compactChecksRenderer) checks() []fyne.CanvasObject {
	return r.WidgetRenderer.Objects()
}

func (r *compactChecksRenderer) Layout(size fyne.Size) {
	x := float32(0)
	checks := r.checks()
	r.group.items = checks
	for len(r.catchers) < len(checks) {
		r.catchers = append(r.catchers, newCheckCatcher(r.group, len(r.catchers)))
	}
	r.catchers = r.catchers[:len(checks)]
	for len(r.ticks) < len(checks) {
		r.ticks = append(r.ticks, NewTickMotion(LogoGold))
	}
	for _, t := range r.ticks[len(checks):] {
		t.Stop()
	}
	r.ticks = r.ticks[:len(checks)]
	r.group.ticks = r.ticks
	for i, o := range checks {
		m := o.MinSize()
		o.Move(fyne.NewPos(x, (size.Height-m.Height)/2))
		o.Resize(m)
		r.ticks[i].PlaceOnCheck(o.Position(), m)
		r.catchers[i].Move(o.Position())
		r.catchers[i].Resize(m)
		x += m.Width + theme.Padding()
	}
	// The pill keeps to its check when the row lays out again.
	if g := r.group; g.hovered >= 0 && g.hovered < len(checks) {
		pos, sz := g.pillFrame(g.hovered)
		g.pillMotion.Fit(pos, sz)
	}
}

func (r *compactChecksRenderer) Refresh() { r.WidgetRenderer.Refresh(); r.Layout(r.group.Size()) }

func (r *compactChecksRenderer) Destroy() {
	r.group.pillMotion.Stop()
	for _, tick := range r.ticks {
		tick.Stop()
	}
	r.WidgetRenderer.Destroy()
}
