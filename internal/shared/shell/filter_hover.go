package shell

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// FilterGroup keeps one gold highlight behind the filters in a row or column.
// The returned container can still have draft and plan keys added to it.
func FilterGroup(flow fyne.Layout, items ...fyne.CanvasObject) *fyne.Container {
	g := &filterHover{pill: widgets.NewHoverPill(), frames: map[*Tappable]fyne.CanvasObject{}}
	g.ExtendBaseWidget(g)
	return container.New(&filterGroupLayout{flow: flow, hover: g}, append([]fyne.CanvasObject{g}, items...)...)
}

// HoverCards draws the same travelling highlight over opaque metric bubbles.
// Existing card actions remain on their own tappables underneath the overlay.
func HoverCards(flow fyne.Layout, items ...fyne.CanvasObject) *fyne.Container {
	var rows []*Tappable
	for i, item := range items {
		row := hoverCard(item)
		if row == nil {
			row = NewCardTappable(item, nil)
			row.Look = desktop.DefaultCursor
			items[i] = row
		}
		rows = append(rows, row)
	}
	return HoverRows(container.New(flow, items...), rows)
}

// HoverRows shares a highlight across explicit rows in a folding sidebar.
// It leaves headings and controls alone and tracks rows inside open folds.
func HoverRows(body *fyne.Container, rows []*Tappable) *fyne.Container {
	g := &filterHover{pill: widgets.NewHoverPill(), frames: map[*Tappable]fyne.CanvasObject{}, overlay: true}
	g.ExtendBaseWidget(g)
	l := &hoverRowsLayout{body: body, hover: g, rows: map[*Tappable]bool{}}
	for _, row := range rows {
		l.rows[row] = true
		row.filterHover = g
		if row.Feedback != nil {
			row.Feedback.grouped = true
			row.Feedback.outline.Hide()
		}
	}
	return container.New(l, body, g)
}

type hoverRowsLayout struct {
	body  *fyne.Container
	hover *filterHover
	rows  map[*Tappable]bool
}

func (l *hoverRowsLayout) MinSize([]fyne.CanvasObject) fyne.Size { return l.body.MinSize() }
func (l *hoverRowsLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	l.body.Move(fyne.Position{})
	l.body.Resize(size)
	g := l.hover
	g.Move(fyne.Position{})
	g.Resize(size)
	previousFrames := g.frames
	g.frames = map[*Tappable]fyne.CanvasObject{}
	var visit func(fyne.CanvasObject, fyne.Position, fyne.Position, fyne.Size)
	visit = func(object fyne.CanvasObject, parent, clip fyne.Position, limit fyne.Size) {
		if object == nil || !object.Visible() {
			return
		}
		pos := parent.Add(object.Position())
		if row, ok := object.(*Tappable); ok && l.rows[row] {
			left, top := max(pos.X, clip.X), max(pos.Y, clip.Y)
			right := min(pos.X+object.Size().Width, clip.X+limit.Width)
			bottom := min(pos.Y+object.Size().Height, clip.Y+limit.Height)
			if right > left && bottom > top {
				frame := previousFrames[row]
				if frame == nil {
					frame = canvas.NewRectangle(nil)
				}
				frame.Move(fyne.NewPos(left, top))
				frame.Resize(fyne.NewSize(right-left, bottom-top))
				g.frames[row] = frame
			}
			return
		}
		switch o := object.(type) {
		case *fyne.Container:
			for _, child := range o.Objects {
				visit(child, pos, clip, limit)
			}
		case *Fold:
			left, top := max(pos.X, clip.X), max(pos.Y, clip.Y)
			right, bottom := min(pos.X+o.Size().Width, clip.X+limit.Width), min(pos.Y+o.Size().Height, clip.Y+limit.Height)
			if right > left && bottom > top {
				visit(o.Content, pos, fyne.NewPos(left, top), fyne.NewSize(right-left, bottom-top))
			}
		case *Reveal:
			visit(o.Content, pos, clip, limit)
		case *container.ThemeOverride:
			visit(o.Content, pos, clip, limit)
		}
	}
	visit(l.body, fyne.Position{}, fyne.Position{}, size)
	if g.last != nil && g.frames[g.last] == nil {
		g.last = nil
	}
	if g.current != nil {
		if frame := g.frames[g.current]; frame != nil {
			g.pill.Fit(frame.Position(), frame.Size())
		} else {
			g.hover(g.current, false)
		}
	}
}

func hoverCard(item fyne.CanvasObject) *Tappable {
	switch o := item.(type) {
	case *Tappable:
		return o
	case *ProjectChip:
		return o.Chip
	case *Reveal:
		return hoverCard(o.Content)
	case *container.ThemeOverride:
		return hoverCard(o.Content)
	}
	return nil
}

type filterHover struct {
	widget.BaseWidget
	pill    *widgets.HoverPill
	current *Tappable
	last    *Tappable
	// Project chips wrap their tappables to handle dragging. Use the wrapper's
	// bounds so the highlight travels in the legend's coordinates.
	frames  map[*Tappable]fyne.CanvasObject
	overlay bool
}

func (g *filterHover) frame(t *Tappable) fyne.CanvasObject {
	if frame := g.frames[t]; frame != nil {
		return frame
	}
	return t
}

func (g *filterHover) hover(t *Tappable, on bool) {
	if on {
		if g.current == t {
			return
		}
		g.current = t
		g.last = t
		frame := g.frame(t)
		g.pill.MoveTo(frame.Position(), frame.Size(), true)
	} else if g.current == t {
		g.current = nil
		g.pill.MoveTo(fyne.Position{}, fyne.Size{}, false)
	}
}

func (g *filterHover) restore(t *Tappable) {
	g.current, g.last = t, t
	frame := g.frame(t)
	g.pill.ShowAt(frame.Position(), frame.Size())
}

// Fyne sends the row its own hover only in the gaps between chips. Keep the
// last chip lit there, so moving slowly across a gap still slides to the next
// chip instead of starting a fresh fade at that chip.
func (g *filterHover) MouseIn(e *desktop.MouseEvent) { g.MouseMoved(e) }
func (g *filterHover) MouseMoved(e *desktop.MouseEvent) {
	if len(g.frames) == 0 {
		return
	}
	var first bool
	var left, top, right, bottom float32
	for _, frame := range g.frames {
		pos, size := frame.Position(), frame.Size()
		if !first {
			left, top, right, bottom = pos.X, pos.Y, pos.X+size.Width, pos.Y+size.Height
			first = true
		} else {
			left, top = min(left, pos.X), min(top, pos.Y)
			right, bottom = max(right, pos.X+size.Width), max(bottom, pos.Y+size.Height)
		}
	}
	if g.overlay {
		for t, frame := range g.frames {
			pos, size := frame.Position(), frame.Size()
			if e.Position.X >= pos.X && e.Position.X < pos.X+size.Width && e.Position.Y >= pos.Y && e.Position.Y < pos.Y+size.Height {
				g.hover(t, true)
				return
			}
		}
	}
	if e.Position.X >= left && e.Position.X < right && e.Position.Y >= top && e.Position.Y < bottom {
		if g.last != nil {
			g.hover(g.last, true)
		}
	} else if g.current != nil {
		g.hover(g.current, false)
	}
}
func (g *filterHover) MouseOut() {
	if g.current != nil {
		g.hover(g.current, false)
	}
}

func (g *filterHover) CreateRenderer() fyne.WidgetRenderer { return &filterHoverRenderer{g: g} }

type filterHoverRenderer struct{ g *filterHover }

func (r *filterHoverRenderer) Layout(fyne.Size)   {}
func (r *filterHoverRenderer) MinSize() fyne.Size { return fyne.Size{} }
func (r *filterHoverRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.g.pill.Rect}
}
func (r *filterHoverRenderer) Refresh() { canvas.Refresh(r.g) }
func (r *filterHoverRenderer) Destroy() { r.g.pill.Stop() }

type filterGroupLayout struct {
	flow    fyne.Layout
	hover   *filterHover
	overlay bool
}

func (l *filterGroupLayout) items(objects []fyne.CanvasObject) []fyne.CanvasObject {
	if l.overlay {
		return objects[:len(objects)-1]
	}
	return objects[1:]
}

func (l *filterGroupLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return l.flow.MinSize(l.items(objects))
}

func (l *filterGroupLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	l.flow.Layout(l.items(objects), size)
	g := l.hover
	g.Move(fyne.Position{})
	g.Resize(size)
	g.frames = map[*Tappable]fyne.CanvasObject{}
	for _, object := range l.items(objects) {
		t := hoverCard(object)
		if t != nil {
			t.filterHover = g
			g.frames[t] = object
			if t.Feedback != nil {
				t.Feedback.grouped = true
				t.Feedback.outline.Hide()
			}
		}
	}
	if g.last != nil && g.frames[g.last] == nil {
		g.last = nil
	}
	if g.current != nil {
		if frame := g.frames[g.current]; frame != nil {
			g.pill.Fit(frame.Position(), frame.Size())
		} else {
			g.hover(g.current, false)
		}
	}
}
