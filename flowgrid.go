package main

import (
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// flowGrid wraps children into rows like fyne's GridWrap, but the cells stretch
// to share the width instead of leaving the remainder as dead space on the
// right. A fixed cell width means the tiles only ever look right at the window
// sizes it was picked for; here the width is whatever the division leaves.
//
// minWidth decides how many fit per row and nothing else. maxWidth stops one or
// two tiles from spanning a maximised window — past that the row stays left
// aligned and the slack is real. height is a floor: a cell grows if a child
// needs more, and every cell in the grid grows with it so the rows stay level.
//
// rows is remembered from the last Layout because MinSize is asked for a height
// before anything knows the width. Fyne's own GridWrap does the same, and it is
// why a grid reports one row on its first pass and settles on the next.
type flowGrid struct {
	minWidth float32
	maxWidth float32
	height   float32
	rows     int
}

func newFlowGrid(minWidth, maxWidth, height float32) *flowGrid {
	return &flowGrid{minWidth: minWidth, maxWidth: maxWidth, height: height, rows: 1}
}

// gap is the space between cells. Cells no longer inset themselves, so this is
// the whole of the breathing room between one tile and the next.
func (g *flowGrid) gap() float32 { return theme.Padding() * 2 }

// columns reports how many cells fit across w and how wide each one comes out.
func (g *flowGrid) columns(w float32, count int) (cols int, cellW float32) {
	gap := g.gap()
	cols = 1
	if w > g.minWidth {
		cols = int(math.Floor(float64(w+gap) / float64(g.minWidth+gap)))
	}
	// Fewer tiles than fit across: let them share the row rather than sit at
	// their minimum with a hole beside them.
	if count > 0 && cols > count {
		cols = count
	}
	if cols < 1 {
		cols = 1
	}
	cellW = (w - gap*float32(cols-1)) / float32(cols)
	if g.maxWidth > 0 && cellW > g.maxWidth {
		cellW = g.maxWidth
	}
	if cellW < 1 {
		cellW = 1
	}
	return cols, cellW
}

// cellHeight is the tallest a child needs, floored at the configured height so
// every tile in the grid is the same height whatever its text does.
func (g *flowGrid) cellHeight(objects []fyne.CanvasObject) float32 {
	h := g.height
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		if mh := o.MinSize().Height; mh > h {
			h = mh
		}
	}
	return h
}

func (g *flowGrid) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	visible := 0
	for _, o := range objects {
		if o.Visible() {
			visible++
		}
	}
	gap := g.gap()
	cols, cellW := g.columns(size.Width, visible)
	cellH := g.cellHeight(objects)

	i, rows := 0, 0
	x, y := float32(0), float32(0)
	for _, child := range objects {
		if !child.Visible() {
			continue
		}
		if i%cols == 0 {
			rows++
			x = 0
		}
		child.Move(fyne.NewPos(x, y))
		child.Resize(fyne.NewSize(cellW, cellH))
		x += cellW + gap
		i++
		if i%cols == 0 {
			y += cellH + gap
		}
	}
	if rows < 1 {
		rows = 1
	}
	g.rows = rows
}

func (g *flowGrid) MinSize(objects []fyne.CanvasObject) fyne.Size {
	rows := g.rows
	if rows < 1 {
		rows = 1
	}
	h := g.cellHeight(objects)
	return fyne.NewSize(g.minWidth, h*float32(rows)+g.gap()*float32(rows-1))
}

// topFill stacks two children: the first takes the height it asks for, and the
// second takes every pixel left, down to the bottom edge.
//
// A Border with the first child on top would do the same until the first child
// is tall — a group with a dozen commits — at which point it would push the
// second off the bottom. So the top is capped at a share of the height and the
// overflow goes to whatever scroller it is wrapped in. That wrapper is also why
// content is passed separately: a scroller asks for almost no height of its
// own, so the height wanted has to be measured on what is inside it.
type topFill struct {
	share   float32 // most of the height the top child may take
	content fyne.CanvasObject
}

func newTopFill(share float32, content fyne.CanvasObject) *topFill {
	return &topFill{share: share, content: content}
}

// topHeight is what the first child gets: what it wants, capped by its share,
// and never so much that the second child is squeezed under its own minimum.
func (t *topFill) topHeight(objects []fyne.CanvasObject, size fyne.Size) float32 {
	want := t.content.MinSize().Height
	if ceiling := size.Height * t.share; want > ceiling {
		want = ceiling
	}
	if room := size.Height - objects[1].MinSize().Height - theme.Padding(); want > room {
		want = room
	}
	if want < 0 {
		want = 0
	}
	return want
}

func (t *topFill) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 2 {
		return
	}
	gap := theme.Padding()
	top := t.topHeight(objects, size)
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(size.Width, top))
	rest := size.Height - top - gap
	if rest < 0 {
		rest = 0
	}
	objects[1].Move(fyne.NewPos(0, top+gap))
	objects[1].Resize(fyne.NewSize(size.Width, rest))
}

// topFillMin is the strip the top child keeps in the reported minimum. Asking
// for its full height would make a long commit list the popup's minimum size,
// when the whole point of the cap is that it scrolls instead.
const topFillMin = 140

func (t *topFill) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) != 2 {
		return fyne.Size{}
	}
	rest := objects[1].MinSize()
	top := t.content.MinSize()
	if top.Height > topFillMin {
		top.Height = topFillMin
	}
	w := rest.Width
	if top.Width > w {
		w = top.Width
	}
	return fyne.NewSize(w, top.Height+rest.Height+theme.Padding())
}

// vMeter stacks children from the bottom, each taking a share of the height.
//
// A calendar cell reads better as a glass filling up than as a hairline bar at
// its foot: the fill is the day, so it has to be the whole cell. Weights are
// fractions of the full height and are expected to sum to one or less — what is
// left over stays empty, which is exactly the part of the day still owed.
//
// MinSize is zero because this is a backdrop: it takes the size of whatever it
// is stacked behind rather than forcing one of its own.
// overlap is how far each segment reaches up behind the one above it. The
// segments are rounded rectangles so the fill matches the rounded cell it sits
// in; left to meet edge to edge, the two rounded ends facing each other would
// notch the seam. Reaching up by the corner radius puts the segment below
// behind that notch, so the join reads solid while the outer corners stay
// round.
type vMeter struct {
	weights []float32
	overlap float32
}

func newVMeter(overlap float32, weights ...float32) *vMeter {
	return &vMeter{weights: weights, overlap: overlap}
}

func (v *vMeter) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	y := size.Height
	for i, o := range objects {
		share := float32(0)
		if i < len(v.weights) {
			share = v.weights[i]
		}
		h := size.Height * share
		y -= h
		grown := h
		if i < len(objects)-1 { // everything but the topmost reaches up
			grown += v.overlap
		}
		o.Move(fyne.NewPos(0, y-(grown-h)))
		o.Resize(fyne.NewSize(size.Width, grown))
	}
}

func (v *vMeter) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.Size{} }

// ratioRow spreads children across one row by weight. An equal-column grid
// hands a two-digit minutes field the same width as the description beside it;
// weights give the text the room the short columns never use. Widen the window
// and the shares stay proportional instead of every column growing equally.
//
// Rows built with the same weights line up, so a table's header and its body
// share one ratioRow spec rather than agreeing by accident.
type ratioRow struct {
	weights []float32
	tight   bool // segments of a meter meet; columns of a table do not
}

func newRatioRow(weights ...float32) *ratioRow { return &ratioRow{weights: weights} }

// newTightRow is the same split with no gaps, for bars whose parts are one
// continuous shape rather than separate columns.
func newTightRow(weights ...float32) *ratioRow {
	return &ratioRow{weights: weights, tight: true}
}

func (r *ratioRow) gap() float32 {
	if r.tight {
		return 0
	}
	return theme.Padding()
}

// weight is the share for column i. Columns past the end of the spec count as
// one, so a row can be extended without the layout silently zeroing it.
func (r *ratioRow) weight(i int) float32 {
	if i < len(r.weights) && r.weights[i] > 0 {
		return r.weights[i]
	}
	return 1
}

// visibleTotal sums the weights actually on screen, so hiding a column hands
// its share to the others instead of leaving a gap.
func (r *ratioRow) visibleTotal(objects []fyne.CanvasObject) (float32, int) {
	total, count := float32(0), 0
	for i, o := range objects {
		if o.Visible() {
			total += r.weight(i)
			count++
		}
	}
	return total, count
}

func (r *ratioRow) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	total, count := r.visibleTotal(objects)
	if count == 0 || total <= 0 {
		return
	}
	gap := r.gap()
	avail := size.Width - gap*float32(count-1)
	if avail < 0 {
		avail = 0
	}
	x := float32(0)
	for i, o := range objects {
		if !o.Visible() {
			continue
		}
		w := avail * r.weight(i) / total
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(w, size.Height))
		x += w + gap
	}
}

func (r *ratioRow) MinSize(objects []fyne.CanvasObject) fyne.Size {
	total, count := r.visibleTotal(objects)
	if count == 0 || total <= 0 {
		return fyne.Size{}
	}
	// The row is only wide enough when its tightest column fits: a child owning
	// a tenth of the row needs the row to be ten times its own minimum.
	width, height := float32(0), float32(0)
	for i, o := range objects {
		if !o.Visible() {
			continue
		}
		m := o.MinSize()
		if m.Height > height {
			height = m.Height
		}
		if need := m.Width * total / r.weight(i); need > width {
			width = need
		}
	}
	return fyne.NewSize(width+r.gap()*float32(count-1), height)
}
