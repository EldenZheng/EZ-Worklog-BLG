package kanban

import (
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// A sprint row picked in the sidebar does not snap to its selected look: the
// gold fill ripples out from where it was clicked, its words darken as the
// gold reaches them, and its refresh button fades in. A row let go of drains
// the same way back, its refresh button fading out. The click redraws the
// whole board (and a sprint still to fetch redraws it again as it lands), so
// each motion is kept by row and started time, and a row drawn again part way
// picks the motion up where it stands.

// selectedInk is the words on a selected row, dark on the gold.
var selectedInk = color.NRGBA{R: 24, G: 24, B: 24, A: 255}

// noSprintRow names the No Sprint row among the sprint keys.
const noSprintRow = "\x00no sprint"

// rowMotion is a row on its way into its selected look, or out of it.
type rowMotion struct {
	selecting bool
	start     time.Time
	// centre is where the ripple spreads from, in the row; at its middle
	// when the row was not the one clicked.
	centre    fyne.Position
	hasCentre bool
}

var sprintMotions = map[*shell.KanbanUI]map[string]*rowMotion{}

// selectedRow is the row the sidebar shows selected: a sprint's key,
// noSprintRow, or "" for none.
func selectedRow(k *shell.KanbanUI) string {
	switch k.Filter {
	case "none":
		return noSprintRow
	case "sprint":
		return k.Sprint
	case "current":
		for _, g := range k.CombinedSprints() {
			if g.Sprint.Contains(shell.Today()) {
				return g.Key
			}
		}
	}
	return ""
}

// selectRow runs change (a click on row key, at in the row when known) and
// sets the rows it selects and lets go of moving.
func selectRow(k *shell.KanbanUI, key string, at *fyne.Position, change func()) {
	before := selectedRow(k)
	change()
	after := selectedRow(k)
	if before == after || !fyne.CurrentApp().Settings().ShowAnimations() {
		return
	}
	motions := sprintMotions[k]
	if motions == nil {
		motions = map[string]*rowMotion{}
		sprintMotions[k] = motions
	}
	now := time.Now()
	note := func(row string, selecting bool) {
		m := &rowMotion{selecting: selecting, start: now}
		if row == key && at != nil {
			m.centre, m.hasCentre = *at, true
		}
		motions[row] = m
	}
	if after != "" {
		note(after, true)
	}
	if before != "" {
		note(before, false)
	}
}

// motionFor is row key's motion still under way toward selected, if any.
func motionFor(k *shell.KanbanUI, key string, selected bool) *rowMotion {
	m := sprintMotions[k][key]
	if m == nil {
		return nil
	}
	if m.selecting != selected || time.Since(m.start) >= shell.RippleTime {
		delete(sprintMotions[k], key)
		return nil
	}
	return m
}

// tapSpot remembers where in row a click lands, for the ripple to spread
// from. Setting OnTapAt also keeps the plain gold ripple off the row.
func tapSpot(row *shell.Tappable) *fyne.Position {
	spot := new(fyne.Position)
	row.OnTapAt = func(at fyne.Position) {
		*spot = at.Subtract(fyne.CurrentApp().Driver().AbsolutePositionForObject(row))
	}
	return spot
}

// selectLook is a row's selected look drawn part way.
type selectLook struct {
	area    fyne.CanvasObject // the row's drop area, as large as the row
	frame   *canvas.Rectangle // the row's own frame: its gold edge
	fill    *canvas.Rectangle // the gold, spreading or draining
	stroke  float32           // the edge's width once selected
	ink     *container.ThemeOverride
	restInk color.Color
	body    []*fyne.Container // re-laid as the refresh button grows or shrinks
	refresh *refreshFade
	// icons are inked with the words. An icon draws itself again only for a
	// new resource, so each frame hands it its own once more.
	icons []*widget.Icon
}

// paint draws the look selected share of the way (0 at rest, 1 selected),
// with the gold reaching spread of its full reach from m's centre.
func (l *selectLook) paint(m *rowMotion, share, spread float32) {
	edge := widgets.WithAlpha(widgets.LogoGold, share)
	l.frame.FillColor = color.Transparent
	l.frame.StrokeColor, l.frame.StrokeWidth = edge, l.stroke
	l.frame.Refresh()
	l.placeFill(m, spread)
	inked := widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: shell.MixColor(l.restInk, selectedInk, share)}
	if l.ink != nil {
		l.ink.Theme = inked
		l.ink.Refresh()
	}
	for _, icon := range l.icons {
		res := icon.Resource
		icon.SetResource(nil)
		icon.SetResource(res)
	}
	if l.refresh != nil {
		l.refresh.set(share, inked)
		for _, c := range l.body {
			c.Refresh()
		}
	}
}

// placeFill spreads the gold over the row: a circle from the centre, cut to
// the row and rounded as the row is where it meets the row's corners.
func (l *selectLook) placeFill(m *rowMotion, spread float32) {
	size := l.area.Size()
	if size.Width <= 0 || size.Height <= 0 {
		l.fill.Hide()
		return
	}
	c := fyne.NewPos(size.Width/2, size.Height/2)
	if m.hasCentre {
		c = fyne.NewPos(min(max(m.centre.X, 0), size.Width), min(max(m.centre.Y, 0), size.Height))
	}
	reach := float32(math.Hypot(float64(max(c.X, size.Width-c.X)), float64(max(c.Y, size.Height-c.Y))))
	r := reach * spread
	if r <= 0.5 {
		l.fill.Hide()
		return
	}
	left, top := max(c.X-r, 0), max(c.Y-r, 0)
	right, bottom := min(c.X+r, size.Width), min(c.Y+r, size.Height)
	l.fill.Move(fyne.NewPos(left, top))
	l.fill.Resize(fyne.NewSize(right-left, bottom-top))
	most := min(right-left, bottom-top) / 2
	corner := func(atX, atY bool) float32 {
		if atX && atY {
			return min(8, most) // the row's own corner
		}
		return min(r, most)
	}
	l.fill.TopLeftCornerRadius = corner(left == 0, top == 0)
	l.fill.TopRightCornerRadius = corner(right == size.Width, top == 0)
	l.fill.BottomLeftCornerRadius = corner(left == 0, bottom == size.Height)
	l.fill.BottomRightCornerRadius = corner(right == size.Width, bottom == size.Height)
	l.fill.Show()
	l.fill.Refresh()
}

// play runs m from where it stands to its end.
func (l *selectLook) play(m *rowMotion) {
	step := func() bool {
		p := min(float32(time.Since(m.start))/float32(shell.RippleTime), 1)
		eased := 1 - (1-p)*(1-p)*(1-p)*(1-p)
		if m.selecting {
			l.paint(m, eased, eased)
		} else {
			l.paint(m, 1-eased, 1-eased)
		}
		return p >= 1
	}
	if step() {
		return
	}
	anim := fyne.NewAnimation(shell.RippleTime-time.Since(m.start), func(float32) { step() })
	anim.Curve = fyne.AnimationLinear
	anim.Start()
}

// refreshFade holds a row's refresh button as it fades in or out, its room
// opening and closing with it so what stands beside it slides over.
type refreshFade struct {
	box   *fyne.Container
	over  *container.ThemeOverride
	share float32
}

func newRefreshFade(control fyne.CanvasObject) *refreshFade {
	f := &refreshFade{}
	f.over = container.NewThemeOverride(control, shell.FadedTheme{Theme: theme.Current(), Alpha: &f.share})
	f.box = container.New(&shareWidth{share: &f.share}, f.over)
	return f
}

// set shows the button share of the way in, inked as base inks the row.
func (f *refreshFade) set(share float32, base fyne.Theme) {
	f.share = share
	f.over.Theme = shell.FadedTheme{Theme: base, Alpha: &f.share}
	f.over.Refresh()
	f.box.Refresh()
}

// shareWidth takes share of its content's width, and draws the content at
// its full width centred in that.
type shareWidth struct{ share *float32 }

func (l *shareWidth) MinSize(objects []fyne.CanvasObject) fyne.Size {
	m := objects[0].MinSize()
	return fyne.NewSize(m.Width**l.share, m.Height)
}

func (l *shareWidth) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	m := objects[0].MinSize()
	objects[0].Move(fyne.NewPos((size.Width-m.Width)/2, 0))
	objects[0].Resize(fyne.NewSize(m.Width, size.Height))
}
