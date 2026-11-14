package shell

import (
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// foldSlideTime is how long a fold takes to open or shut: the time one piece of
// an arrival takes to uncover, so a section folding open moves as the Status
// day panel's pieces do.
const foldSlideTime = arriveEach

// Fold shows the top share of its content, cut off below, so a section can
// slide open and shut. It is a Scrollable only because Fyne clips nothing
// else; it never scrolls, and the patched wheel passes straight through it.
type Fold struct {
	widget.BaseWidget
	Content fyne.CanvasObject
	share   float32 // 0 shut, 1 fully open
	open    bool
	slide   *fyne.Animation
	// Duration and Curve, when set, replace the default slide's.
	Duration time.Duration
	Curve    fyne.AnimationCurve
}

// NewFold wraps content, starting open or shut.
func NewFold(content fyne.CanvasObject, open bool) *Fold {
	f := &Fold{Content: content, open: open}
	if open {
		f.share = 1
	} else {
		f.Hide()
	}
	f.ExtendBaseWidget(f)
	return f
}

// Open reports whether the fold is open or opening.
func (f *Fold) Open() bool { return f.open }

// Slide opens or shuts the fold over a short animation. relayout runs on
// every frame, for the page around the fold to follow it; final is true on
// the last one. Build it with Reflow.
func (f *Fold) Slide(open bool, relayout func(final bool)) {
	f.open = open
	if f.slide != nil {
		f.slide.Stop()
	}
	from, to := f.share, float32(0)
	if open {
		to = 1
		f.Show()
	}
	// Eased at both ends: a card that starts growing at full speed lands its
	// biggest step on the first frame, the one that also draws the newly
	// uncovered content, and reads as a jump.
	duration, curve := foldSlideTime, fyne.AnimationCurve(EaseInOutCubic)
	if f.Duration > 0 {
		duration = f.Duration
	}
	if f.Curve != nil {
		curve = f.Curve
	}
	var slide *fyne.Animation
	slide = fyne.NewAnimation(duration, func(done float32) {
		if f.slide != slide {
			return
		}
		f.share = from + (to-from)*done
		final := done >= 1
		if final && !open {
			f.Hide()
		}
		// Only the geometry moves, the last frame included: refreshing the
		// section's widgets, on any frame, is what made the slide stutter.
		f.layoutContent(f.Size())
		Repaint(f)
		if relayout != nil {
			relayout(final)
		}
	})
	slide.Curve = curve
	f.slide = slide
	slide.Start()
}

func (f *Fold) Scrolled(*fyne.ScrollEvent)    {}
func (f *Fold) CanScrollBy(_, _ float32) bool { return false }

func (f *Fold) CreateRenderer() fyne.WidgetRenderer {
	return &foldRenderer{f: f}
}

type foldRenderer struct{ f *Fold }

func (f *Fold) layoutContent(size fyne.Size) {
	full := f.Content.MinSize().Height
	// The content stands still and is uncovered from the top down, as the
	// Status day panel's pieces are: moving it as well read as a judder.
	if f.Content.Position() != (fyne.Position{}) {
		f.Content.Move(fyne.Position{})
	}
	if want := fyne.NewSize(size.Width, max(full, size.Height)); f.Content.Size() != want {
		f.Content.Resize(want)
	}
}

func (r *foldRenderer) Layout(size fyne.Size) { r.f.layoutContent(size) }

func (r *foldRenderer) MinSize() fyne.Size {
	m := r.f.Content.MinSize()
	if r.f.share >= 1 {
		return m
	}
	// Whole pixels while moving, so text does not shimmer between them.
	return fyne.NewSize(m.Width, float32(math.Round(float64(m.Height*r.f.share))))
}

func (r *foldRenderer) Refresh() {
	r.Layout(r.f.Size())
	r.f.Content.Refresh()
}

func (r *foldRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.f.Content} }
func (r *foldRenderer) Destroy()                     {}

// CollapseToggle is label with a gold arrow after it; tapping either slides
// body open or shut. It returns the heading and the area to place where body
// would go. body starts open or shut as asked. relayout runs on every frame of
// the slide, for the page around it to follow (see Reflow); onToggle runs once
// per tap with the new state, to remember it.
func CollapseToggle(label, body fyne.CanvasObject, open bool, onToggle func(open bool), relayout func(final bool)) (head, area fyne.CanvasObject) {
	name, fold, toggle := collapseParts(label, body, open, onToggle, relayout)
	return container.NewHBox(NewTappable(name, toggle)), fold
}

// CollapseLine is CollapseToggle across a whole line: the label and its
// arrow at the left, end (when set) at the right, and a click anywhere along
// the line slides body open or shut, over slide when it is above zero.
func CollapseLine(label, end, body fyne.CanvasObject, open bool, slide time.Duration, onToggle func(open bool), relayout func(final bool)) (head, area fyne.CanvasObject) {
	name, fold, toggle := collapseParts(label, body, open, onToggle, relayout)
	fold.Duration = slide
	var right fyne.CanvasObject
	if end != nil {
		right = container.NewCenter(end)
	}
	return NewTappable(container.NewBorder(nil, nil, name, right), toggle), fold
}

// collapseParts is a collapsible heading's label with its gold arrow, the
// fold for body, and the toggle that slides it.
func collapseParts(label, body fyne.CanvasObject, open bool, onToggle func(open bool), relayout func(final bool)) (fyne.CanvasObject, *Fold, func()) {
	arrow := widget.NewIcon(theme.MenuExpandIcon())
	if open {
		arrow.SetResource(theme.MenuDropDownIcon())
	}
	goldArrow := container.NewThemeOverride(arrow, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	fold := NewFold(body, open)
	toggle := func() {
		open := !fold.Open()
		if open {
			arrow.SetResource(theme.MenuDropDownIcon())
		} else {
			arrow.SetResource(theme.MenuExpandIcon())
		}
		fold.Slide(open, relayout)
		if onToggle != nil {
			onToggle(open)
		}
	}
	return container.NewHBox(label, goldArrow), fold, toggle
}

// CollapsibleTitle is a SectionTitle's gold heading as a CollapseToggle.
func CollapsibleTitle(title string, body fyne.CanvasObject, open bool, onToggle func(open bool), relayout func(final bool)) (head, area fyne.CanvasObject) {
	t := canvas.NewText(title, widgets.LogoGold)
	t.TextSize = theme.TextSize() + 4
	t.TextStyle = fyne.TextStyle{Bold: true}
	return CollapseToggle(container.NewCenter(t), body, open, onToggle, relayout)
}

// Reflow lays a page out again around something that changed size, for one
// frame of an animation. chain runs from the outside in: the page's scroll,
// then each container down to the one holding the change. Only geometry is
// redone, so a frame stays cheap however much the page holds. A scroll keeps
// its view inside the content, so when a section above shuts and the page
// gets shorter, the view rides up with it instead of being left over blank
// space. On the final frame the outermost object is refreshed in full, which
// also settles the scroll bars.
func Reflow(final bool, chain ...fyne.CanvasObject) {
	for _, o := range chain {
		switch x := o.(type) {
		case *container.Scroll:
			reflowScroll(x)
		case *fyne.Container:
			if x.Layout != nil {
				x.Layout.Layout(x.Objects, x.Size())
			}
		}
	}
	if len(chain) == 0 {
		return
	}
	if final {
		settleFrame(chain[0])
	} else {
		Repaint(chain[0])
	}
}

// settleFrame ends a move: a page scroll's bars catch up with the page's new
// length. Refreshing the scroll would do it too, and redraw everything on the
// page with it, which was a stutter at the end of every slide.
func settleFrame(o fyne.CanvasObject) {
	s, ok := o.(*container.Scroll)
	if !ok {
		o.Refresh()
		return
	}
	// ScrollToOffset re-measures the bars, but does nothing for the offset the
	// scroll already has: step off it and back.
	at := s.Offset
	s.Offset.Y++
	s.ScrollToOffset(at)
	Repaint(s)
}

// Repaint asks for the next frame to be drawn, keeping everything already
// drawn. canvas.Refresh throws away the GPU's copy of every letter and shape
// under the object it is given, so calling it on a page on every frame of an
// animation drew the whole page again from scratch sixty times a second: the
// card juddered as it grew while the Status day panel, which refreshes only
// the piece arriving, stayed smooth. Between frames of a move only positions
// and sizes change, and none of that needs anything redrawn. Refresh in full
// on the last frame.
func Repaint(o fyne.CanvasObject) {
	app := fyne.CurrentApp()
	if app == nil || app.Driver() == nil {
		return
	}
	if c, ok := app.Driver().CanvasForObject(o).(interface{ SetDirty() }); ok {
		c.SetDirty()
	}
}

func reflowScroll(s *container.Scroll) {
	size := s.Size()
	s.Content.Resize(s.Content.MinSize().Max(size))
	limit := fyne.NewPos(max(s.Content.Size().Width-size.Width, 0), max(s.Content.Size().Height-size.Height, 0))
	if s.Offset.X > limit.X || s.Offset.Y > limit.Y {
		s.ScrollToOffset(fyne.NewPos(min(s.Offset.X, limit.X), min(s.Offset.Y, limit.Y)))
	}
}

// Morph eases its content from one height to the next and slides it across,
// so swapping what it shows (a different form for a different choice) reads as
// a move rather than a jump: the old form slides out one side while the new one
// slides in from the other. Like Fold it clips, and only for that is it a
// Scrollable.
type Morph struct {
	widget.BaseWidget
	Content fyne.CanvasObject
	// Duration overrides the default when a view needs a longer slide.
	Duration time.Duration
	height   float32 // shown height while moving; below zero, the content's own
	shift    float32 // sideways offset while sliding in
	move     *fyne.Animation
	// old is a picture of what it showed before the change, sliding out by
	// oldShift; nil when there is none.
	old      *canvas.Image
	oldShift float32
}

// morphSlide is how far the content slides in from when there is no picture of
// the old content to slide out of the way; with one, it slides the full width.
const morphSlide = 120

// MorphTime is how long a change takes.
const MorphTime = 440 * time.Millisecond

// Snapshot keeps a picture of what the Morph shows now, to slide out of the way
// at the next Change. Call it just before changing the content.
func (m *Morph) Snapshot() {
	// Hold the current height before fields are shown or hidden. Their refresh
	// can otherwise resize the parent before Change has captured the old size.
	if m.height < 0 {
		m.height = m.Size().Height
	}
	m.old = nil
	c := fyne.CurrentApp().Driver().CanvasForObject(m)
	if c == nil || !animationsOn() || !m.Visible() {
		return
	}
	picture := captureArea(c, fyne.CurrentApp().Driver().AbsolutePositionForObject(m), m.Size())
	if picture == nil {
		return
	}
	m.old = canvas.NewImageFromImage(picture)
	m.old.FillMode = canvas.ImageFillStretch
	m.old.Resize(m.Size())
}

func NewMorph(content fyne.CanvasObject) *Morph {
	m := &Morph{Content: content, height: -1}
	m.ExtendBaseWidget(m)
	return m
}

// Change animates from the height shown now to the content's new height, with
// the content sliding in from the right (dir > 0) or the left (dir < 0).
// Call it right after changing what the content shows. relayout is as for
// Fold.Slide.
func (m *Morph) Change(dir int, relayout func(final bool)) {
	if m.move != nil {
		m.move.Stop()
	}
	from := m.Size().Height
	if m.height >= 0 {
		from = m.height
	}
	slide := float32(morphSlide)
	if m.old != nil {
		slide = m.Size().Width // the new form pushes the old one out
	}
	if dir == 0 {
		m.old = nil
	}
	start := float32(0)
	switch {
	case dir > 0:
		start = slide
	case dir < 0:
		start = -slide
	}
	// Hold the old height from now, not from the first frame: until that
	// frame the panel would otherwise lay out at the new form's height, which
	// for Commits is still empty, and flash shut for an instant.
	// The same for the slide: the new form starts off to the side.
	m.height, m.shift, m.oldShift = from, start, 0
	m.layoutContent(m.Size())
	var move *fyne.Animation
	duration := MorphTime
	if m.Duration > 0 {
		duration = m.Duration
	}
	move = fyne.NewAnimation(duration, func(done float32) {
		if m.move != move {
			return
		}
		// Read every frame: the new form may still be filling in (Commits
		// redraws its bubbles after the switch), and a height taken at the
		// start would shrink the panel to that empty form.
		to := m.Content.MinSize().Height
		final := done >= 1
		if final {
			m.height, m.shift, m.old = -1, 0, nil
		} else {
			// Whole pixels, so text does not shimmer between them as it slides.
			m.height = float32(math.Round(float64(from + (to-from)*done)))
			m.shift = float32(math.Round(float64(start * (1 - done))))
			m.oldShift = float32(math.Round(float64(-start * done)))
		}
		m.layoutContent(m.Size())
		if final {
			canvas.Refresh(m)
		} else {
			Repaint(m)
		}
		if relayout != nil {
			relayout(final)
		}
	})
	// Eased at both ends: starting at full speed read as a jump while the
	// first frames were still being laid out.
	move.Curve = EaseInOutCubic
	m.move = move
	move.Start()
}

func (m *Morph) layoutContent(size fyne.Size) {
	full := m.Content.MinSize().Height
	m.Content.Move(fyne.NewPos(m.shift, 0))
	m.Content.Resize(fyne.NewSize(size.Width, max(full, size.Height)))
	if m.old != nil {
		m.old.Move(fyne.NewPos(m.oldShift, 0))
	}
}

func (m *Morph) Scrolled(*fyne.ScrollEvent)    {}
func (m *Morph) CanScrollBy(_, _ float32) bool { return false }

func (m *Morph) CreateRenderer() fyne.WidgetRenderer { return &morphRenderer{m: m} }

type morphRenderer struct{ m *Morph }

func (r *morphRenderer) Layout(size fyne.Size) { r.m.layoutContent(size) }

func (r *morphRenderer) MinSize() fyne.Size {
	c := r.m.Content.MinSize()
	if r.m.height >= 0 {
		c.Height = r.m.height
	}
	return c
}

func (r *morphRenderer) Refresh() {
	r.m.layoutContent(r.m.Size())
	r.m.Content.Refresh()
}

func (r *morphRenderer) Objects() []fyne.CanvasObject {
	if r.m.old != nil {
		return []fyne.CanvasObject{r.m.Content, r.m.old}
	}
	return []fyne.CanvasObject{r.m.Content}
}
func (r *morphRenderer) Destroy() {}

// pageScroll is a tab's page scroll that pans the view up when the page gets
// shorter under it, rather than jumping. A transparent floor at the foot of
// the page holds the old height, then eases away over pageSettle with the view
// following it up, as a camera would pan. It watches the page's height itself,
// so whatever shortens the page (a list emptied, a form swapped for a shorter
// one, a section folded) is caught, not only a Relayout.
type pageScroll struct {
	scroll *container.Scroll
	root   fyne.CanvasObject
	floor  *canvas.Rectangle
	chain  []fyne.CanvasObject
	ease   *fyne.Animation
	// inner is the page's height without the floor, as last measured; busy
	// stops a measure taken while one is being made from starting another.
	inner float32
	busy  bool
}

// pageSettle is how long the view takes to pan up to a page's new foot.
const pageSettle = 600 * time.Millisecond

var pageScrolls = map[*fyne.Container]*pageScroll{}

// NewPageScroll scrolls root, a tab's whole page, vertically. page is the
// container inside it that lists the page's parts; it gets the floor. column
// is the container the tab passes to Relayout.
func NewPageScroll(root fyne.CanvasObject, page, column *fyne.Container) *container.Scroll {
	floor := canvas.NewRectangle(color.Transparent)
	floor.Hide()
	page.Add(floor)
	p := &pageScroll{root: root, floor: floor}
	watched := container.New(&pageWatch{p: p}, root)
	s := container.NewVScroll(watched)
	p.scroll = s
	p.chain = []fyne.CanvasObject{s, watched, root}
	if page != root {
		p.chain = append(p.chain, page)
	}
	pageScrolls[column] = p
	return s
}

// pageWatch lays the page out at the scroll's size and, each time the
// scroll measures it, checks whether it got shorter.
type pageWatch struct{ p *pageScroll }

func (w *pageWatch) MinSize(objects []fyne.CanvasObject) fyne.Size {
	w.p.watch()
	return objects[0].MinSize()
}

func (w *pageWatch) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
}

// floorHeight is how much height the floor adds to the page now.
func (p *pageScroll) floorHeight() float32 {
	if !p.floor.Visible() {
		return 0
	}
	return p.floor.MinSize().Height + theme.Padding()
}

// settle checks the page's height now, for a column that has just changed.
func (p *pageScroll) settle() {
	p.watch()
}

// watch notices the page getting shorter while the view stands past its new
// foot, and holds the old height with the floor while the view pans up.
func (p *pageScroll) watch() {
	if p.busy {
		return
	}
	p.busy = true
	defer func() { p.busy = false }()
	held := p.floorHeight()
	inner := p.root.MinSize().Height - held
	before := p.inner
	p.inner = inner
	view := p.scroll.Size().Height
	if view <= 0 || before <= 0 || inner >= before-0.5 {
		return
	}
	// Still in view at the new height: nothing to pan.
	if p.scroll.Offset.Y <= max(inner-view, 0)+0.5 {
		return
	}
	p.panFrom(before + held - inner)
}

// panFrom holds extra height at the foot of the page and eases it away, the
// view riding up with it.
func (p *pageScroll) panFrom(extra float32) {
	if p.ease != nil {
		p.ease.Stop()
	}
	from := max(extra-theme.Padding(), 0)
	p.floor.SetMinSize(fyne.NewSize(0, from))
	p.floor.Show()
	if !fyne.CurrentApp().Settings().ShowAnimations() {
		from = 0
	}
	var ease *fyne.Animation
	ease = fyne.NewAnimation(pageSettle, func(done float32) {
		if p.ease != ease {
			return
		}
		final := done >= 1
		if final {
			p.floor.SetMinSize(fyne.NewSize(0, 0))
			p.floor.Hide()
		} else {
			p.floor.SetMinSize(fyne.NewSize(0, max(from*(1-done), 0)))
		}
		Reflow(final, p.chain...)
	})
	ease.Curve = EaseInOutCubic
	p.ease = ease
	ease.Start()
}
