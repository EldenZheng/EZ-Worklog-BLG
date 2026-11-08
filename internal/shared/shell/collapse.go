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

const foldDuration = 260 * time.Millisecond

// foldDrop is how far a fold's content sinks into place as it opens.
const foldDrop = 28

// Fold shows the top share of its content, cut off below, so a section can
// slide open and shut. It is a Scrollable only because Fyne clips nothing
// else; it never scrolls, and the patched wheel passes straight through it.
type Fold struct {
	widget.BaseWidget
	Content fyne.CanvasObject
	share   float32 // 0 shut, 1 fully open
	open    bool
	slide   *fyne.Animation
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
	var slide *fyne.Animation
	slide = fyne.NewAnimation(foldDuration, func(done float32) {
		if f.slide != slide {
			return
		}
		f.share = from + (to-from)*done
		final := done >= 1
		if final && !open {
			f.Hide()
		}
		// Between frames only the geometry moves; refreshing every widget in
		// the section on each frame is what made the slide stutter.
		if final {
			f.Refresh()
		} else {
			f.layoutContent(f.Size())
			canvas.Refresh(f)
		}
		if relayout != nil {
			relayout(final)
		}
	})
	slide.Curve = fyne.AnimationEaseOut
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
	// The content drops in from a little above as the fold opens, and rises
	// back as it shuts, rather than only being uncovered.
	f.Content.Move(fyne.NewPos(0, -foldDrop*(1-f.share)))
	f.Content.Resize(fyne.NewSize(size.Width, max(full, size.Height)))
}

func (r *foldRenderer) Layout(size fyne.Size) { r.f.layoutContent(size) }

func (r *foldRenderer) MinSize() fyne.Size {
	m := r.f.Content.MinSize()
	return fyne.NewSize(m.Width, m.Height*r.f.share)
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
	arrow := widget.NewIcon(theme.MenuExpandIcon())
	if open {
		arrow.SetResource(theme.MenuDropDownIcon())
	}
	goldArrow := container.NewThemeOverride(arrow, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	fold := NewFold(body, open)
	tap := NewTappable(container.NewHBox(label, goldArrow), func() {
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
	})
	return container.NewHBox(tap), fold
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
		chain[0].Refresh()
	} else {
		canvas.Refresh(chain[0])
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
	height  float32 // shown height while moving; below zero, the content's own
	shift   float32 // sideways offset while sliding in
	move    *fyne.Animation
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
	move = fyne.NewAnimation(MorphTime, func(done float32) {
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
		canvas.Refresh(m)
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

// pageScroll is a tab's page scroll that rides the view up when the page gets
// shorter under it. A transparent floor at the foot of the page holds the old
// height for a moment and then eases away, the view following it up.
type pageScroll struct {
	scroll *container.Scroll
	floor  *canvas.Rectangle
	chain  []fyne.CanvasObject
	ease   *fyne.Animation
}

var pageScrolls = map[*fyne.Container]*pageScroll{}

// NewPageScroll scrolls root, a tab's whole page, vertically. page is the
// container inside it that lists the page's parts; it gets the floor. column
// is the container the tab passes to Relayout.
func NewPageScroll(root fyne.CanvasObject, page, column *fyne.Container) *container.Scroll {
	floor := canvas.NewRectangle(color.Transparent)
	floor.Hide()
	page.Add(floor)
	s := container.NewVScroll(root)
	chain := []fyne.CanvasObject{s, root}
	if page != root {
		chain = append(chain, page)
	}
	pageScrolls[column] = &pageScroll{scroll: s, floor: floor, chain: chain}
	return s
}

func (p *pageScroll) settle() {
	s := p.scroll
	view := s.Size().Height
	shown := s.Content.Size().Height // still the old height: the page relays later
	floor := float32(0)
	if p.floor.Visible() {
		floor = p.floor.MinSize().Height + theme.Padding()
	}
	needed := max(s.Content.MinSize().Height-floor, view)
	if view <= 0 || s.Offset.Y <= needed-view || shown <= needed {
		return
	}
	if p.ease != nil {
		p.ease.Stop()
	}
	from := shown - needed - theme.Padding()
	p.floor.SetMinSize(fyne.NewSize(0, max(from, 0)))
	p.floor.Show()
	var ease *fyne.Animation
	ease = fyne.NewAnimation(foldDuration, func(done float32) {
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
	ease.Curve = fyne.AnimationEaseOut
	p.ease = ease
	ease.Start()
}
