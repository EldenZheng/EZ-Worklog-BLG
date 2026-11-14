package projects

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// HoverList is a column of rows with the Kanban filter's hover: a soft gold
// pill fades in behind the row under the pointer and slides from row to row
// as the pointer moves down the list. The parent issues and the milestones
// are drawn as one.
type HoverList struct {
	widget.BaseWidget
	// Rows holds the rows, in order; add them with add.
	Rows    *fyne.Container
	pill    *widgets.HoverPill
	hovered int
}

func newHoverList(spacing float32) *HoverList {
	l := &HoverList{Rows: container.New(layout.NewCustomPaddedVBoxLayout(spacing)), hovered: -1}
	l.pill = widgets.NewHoverPill()
	l.ExtendBaseWidget(l)
	return l
}

// add puts content in the list as a row that lights under the pointer, and
// clicks to onTap when it is set. Links and ticks inside it that tell a hover
// parent (see shell.AttachHoverChildren) keep it lit.
func (l *HoverList) add(content fyne.CanvasObject, onTap func()) {
	i := len(l.Rows.Objects)
	on := func(in bool) { l.hover(i, in) }
	// A little room round the content, so the pill does not crowd it.
	row := shell.NewTappable(container.New(layout.NewCustomPaddedLayout(2, 2, 4, 4), content), onTap)
	row.OnHover = on
	if onTap == nil {
		row.Look = desktop.DefaultCursor
	}
	shell.AttachHoverChildren(content, on)
	l.Rows.Add(row)
}

// hover moves the pill to row i as the pointer arrives on it, and fades it
// out as the pointer leaves the row it is on.
func (l *HoverList) hover(i int, in bool) {
	switch {
	case in:
		l.moveTo(i)
	case l.hovered == i:
		l.moveTo(-1)
	}
}

// frame is where the pill stands behind row i.
func (l *HoverList) frame(i int) (fyne.Position, fyne.Size) {
	row := l.Rows.Objects[i]
	return row.Position(), row.Size()
}

// moveTo slides the pill to row i, or fades it out for -1: coming from
// nowhere it fades in where it lands, as on the Kanban's filter.
func (l *HoverList) moveTo(i int) {
	if i >= len(l.Rows.Objects) {
		return
	}
	l.hovered = i
	var pos fyne.Position
	var size fyne.Size
	if i >= 0 {
		pos, size = l.frame(i)
	}
	l.pill.MoveTo(pos, size, i >= 0)
}

func (l *HoverList) CreateRenderer() fyne.WidgetRenderer { return &hoverListRenderer{l: l} }

type hoverListRenderer struct{ l *HoverList }

func (r *hoverListRenderer) Layout(size fyne.Size) {
	l := r.l
	l.Rows.Move(fyne.NewPos(0, 0))
	l.Rows.Resize(size)
	// The pill keeps to its row when the list lays out again.
	if l.hovered >= 0 && l.hovered < len(l.Rows.Objects) {
		pos, sz := l.frame(l.hovered)
		l.pill.Fit(pos, sz)
	}
}

func (r *hoverListRenderer) MinSize() fyne.Size { return r.l.Rows.MinSize() }
func (r *hoverListRenderer) Refresh()           { r.Layout(r.l.Size()); r.l.pill.Rect.Refresh() }
func (r *hoverListRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.l.pill.Rect, r.l.Rows}
}
func (r *hoverListRenderer) Destroy() { r.l.pill.Stop() }
