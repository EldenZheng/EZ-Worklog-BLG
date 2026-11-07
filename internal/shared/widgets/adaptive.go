package widgets

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
)

// Layouts for small screens. Each only changes anything once its content stops
// fitting; until then it places things exactly as the plain row or grid it
// replaces did.
//
// As with FlowGrid, what was decided is remembered from the last Layout, because
// MinSize is asked for a height before anything knows the width.

// dayGridSteps are the columns a week can fall back to: never six days and a
// lonely Sunday.
var dayGridSteps = []int{7, 4, 2, 1}

// DayGridLayout lays a week's days out in as many columns as they fit.
//
// A plain seven-column grid is seven times as wide as its widest day, and Fyne
// cannot shrink a window below its content, so one wide card pushed the right of
// the page off a small screen.
type DayGridLayout struct{ Cols int }

// NewDayGridLayout starts from the width the week already has, so a redraw does
// not flash one row before settling.
func NewDayGridLayout(days []fyne.CanvasObject, width float32) *DayGridLayout {
	g := &DayGridLayout{Cols: dayGridSteps[0]}
	if width > 0 {
		g.Cols = g.columnsFor(days, width)
	}
	return g
}

func (g *DayGridLayout) columnsFor(objects []fyne.CanvasObject, width float32) int {
	need := float32(0)
	for _, o := range objects {
		need = max(need, o.MinSize().Width)
	}
	pad := theme.Padding()
	for _, cols := range dayGridSteps {
		if float32(cols)*need+float32(cols-1)*pad <= width {
			return cols
		}
	}
	return 1
}

// rowHeights is how tall each row needs to be for cols columns.
func rowHeights(objects []fyne.CanvasObject, cols int) []float32 {
	var rows []float32
	for i, o := range objects {
		if i%cols == 0 {
			rows = append(rows, 0)
		}
		rows[len(rows)-1] = max(rows[len(rows)-1], o.MinSize().Height)
	}
	return rows
}

func (g *DayGridLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	g.Cols = g.columnsFor(objects, size.Width)
	if g.Cols == dayGridSteps[0] {
		layout.NewGridLayoutWithColumns(g.Cols).Layout(objects, size)
		return
	}
	pad := theme.Padding()
	cellW := (size.Width - float32(g.Cols-1)*pad) / float32(g.Cols)
	heights := rowHeights(objects, g.Cols)
	y := float32(0)
	for i, o := range objects {
		row := i / g.Cols
		o.Move(fyne.NewPos(float32(i%g.Cols)*(cellW+pad), y))
		o.Resize(fyne.NewSize(cellW, heights[row]))
		if (i+1)%g.Cols == 0 {
			y += heights[row] + pad
		}
	}
}

// MinSize is one day wide, however many columns are showing: the width only
// decides how many fit.
func (g *DayGridLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for _, o := range objects {
		size.Width = max(size.Width, o.MinSize().Width)
	}
	heights := rowHeights(objects, max(g.Cols, 1))
	for _, h := range heights {
		size.Height += h
	}
	if len(heights) > 1 {
		size.Height += float32(len(heights)-1) * theme.Padding()
	}
	return size
}

// SidesLayout holds one group at each end of a row, or stacks them when they do
// not both fit.
type SidesLayout struct{ stacked bool }

// NewSidesLayout starts from the width the row already has, if any.
func NewSidesLayout(left, right fyne.CanvasObject, width float32) *SidesLayout {
	return &SidesLayout{stacked: width > 0 && sidesStack(left, right, width)}
}

func sidesStack(left, right fyne.CanvasObject, width float32) bool {
	return left.MinSize().Width+theme.Padding()+right.MinSize().Width > width
}

func (l *SidesLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	left, right := objects[0], objects[1]
	lm, rm := left.MinSize(), right.MinSize()
	l.stacked = sidesStack(left, right, size.Width)
	left.Move(fyne.NewPos(0, 0))
	if l.stacked {
		left.Resize(fyne.NewSize(size.Width, lm.Height))
		right.Move(fyne.NewPos(0, lm.Height+theme.Padding()))
		right.Resize(rm)
		return
	}
	// The left group gets all the room the right leaves it, so one that wraps
	// (WrapRowLayout) stays on one line while it can.
	left.Resize(fyne.NewSize(size.Width-rm.Width-theme.Padding(), size.Height))
	right.Move(fyne.NewPos(size.Width-rm.Width, 0))
	right.Resize(fyne.NewSize(rm.Width, size.Height))
}

func (l *SidesLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	lm, rm := objects[0].MinSize(), objects[1].MinSize()
	if l.stacked {
		return fyne.NewSize(max(lm.Width, rm.Width), lm.Height+theme.Padding()+rm.Height)
	}
	return fyne.NewSize(max(lm.Width, rm.Width), max(lm.Height, rm.Height))
}

// WrapRowLayout runs its children left to right like an HBox, and starts a new
// line when the next one does not fit.
type WrapRowLayout struct{ height float32 }

func (l *WrapRowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	pad := theme.Padding()
	// Lines of children, as index ranges, and how tall each one is.
	var starts []int
	var heights []float32
	x := float32(0)
	for i, o := range objects {
		if !o.Visible() {
			continue
		}
		m := o.MinSize()
		if len(starts) == 0 || x+m.Width > size.Width {
			starts = append(starts, i)
			heights = append(heights, 0)
			x = 0
		}
		heights[len(heights)-1] = max(heights[len(heights)-1], m.Height)
		x += m.Width + pad
	}
	l.height = 0
	for _, h := range heights {
		l.height += h
	}
	l.height += pad * float32(max(len(heights)-1, 0))
	// A single line takes the row's full height, as an HBox's children do.
	if len(heights) == 1 {
		heights[0] = max(heights[0], size.Height)
	}
	y, line := float32(0), -1
	x = 0
	for i, o := range objects {
		if !o.Visible() {
			continue
		}
		if line+1 < len(starts) && starts[line+1] == i {
			if line >= 0 {
				y += heights[line] + pad
			}
			line++
			x = 0
		}
		m := o.MinSize()
		o.Move(fyne.NewPos(x, y))
		o.Resize(fyne.NewSize(m.Width, heights[line]))
		x += m.Width + pad
	}
}

func (l *WrapRowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for _, o := range objects {
		if o.Visible() {
			m := o.MinSize()
			size.Width = max(size.Width, m.Width)
			size.Height = max(size.Height, m.Height)
		}
	}
	size.Height = max(size.Height, l.height)
	return size
}

// FluidRow splits a row by weight like NewRatioRow, except that a column under
// its own minimum takes that minimum and the others share what is left.
//
// A ratio row's minimum is its tightest column scaled up: a sidebar needing 300
// in a fifth of the row demands a 1500 wide row. This one asks for the sum of its
// columns, so the row can get as narrow as they can.
type FluidRow struct{ weights []float32 }

func NewFluidRow(weights ...float32) *FluidRow { return &FluidRow{weights: weights} }

func (r *FluidRow) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	pad := theme.Padding()
	avail := max(size.Width-pad*float32(len(objects)-1), 0)
	total := float32(0)
	for i := range objects {
		total += r.weights[i]
	}
	widths := make([]float32, len(objects))
	floored := make([]bool, len(objects))
	// Pin any column its share would squeeze, then re-share what is left: pinning
	// one can squeeze another.
	for pinning := true; pinning; {
		pinning = false
		left, shares := avail, float32(0)
		for i, o := range objects {
			if floored[i] {
				left -= o.MinSize().Width
			} else {
				shares += r.weights[i]
			}
		}
		for i, o := range objects {
			if floored[i] {
				widths[i] = o.MinSize().Width
				continue
			}
			widths[i] = left * r.weights[i] / shares
			if widths[i] < o.MinSize().Width {
				floored[i] = true
				pinning = true
			}
		}
	}
	x := float32(0)
	for i, o := range objects {
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(widths[i], size.Height))
		x += widths[i] + pad
	}
}

func (r *FluidRow) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for _, o := range objects {
		m := o.MinSize()
		size.Width += m.Width
		size.Height = max(size.Height, m.Height)
	}
	size.Width += theme.Padding() * float32(max(len(objects)-1, 0))
	return size
}
