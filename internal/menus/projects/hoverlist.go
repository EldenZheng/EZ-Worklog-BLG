package projects

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
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
	// Held, when set, keeps the key of the row under the pointer, so the
	// list drawn again by a click on a row comes up with that row still lit.
	Held *string
	keys []string
}

func newHoverList(spacing float32) *HoverList {
	l := &HoverList{Rows: container.New(layout.NewCustomPaddedVBoxLayout(spacing)), hovered: -1}
	l.pill = widgets.NewHoverPill()
	l.ExtendBaseWidget(l)
	return l
}

// add puts content in the list as a row that lights under the pointer, and
// clicks to onTap when it is set, rippling gold from the click. Links and
// ticks inside it that tell a hover parent (see shell.AttachHoverChildren)
// keep it lit.
func (l *HoverList) add(content fyne.CanvasObject, onTap func()) *shell.Tappable {
	return l.addAs("", content, onTap)
}

// addAs is add for a row known by key (see Held).
func (l *HoverList) addAs(key string, content fyne.CanvasObject, onTap func()) *shell.Tappable {
	i := len(l.Rows.Objects)
	l.keys = append(l.keys, key)
	on := func(in bool) { l.hover(i, in) }
	// A little room round the content, so the pill does not crowd it.
	row := shell.NewTappable(container.New(layout.NewCustomPaddedLayout(2, 2, 4, 4), content), onTap)
	row.OnHover = on
	row.Ripple = true
	if onTap == nil {
		row.Look = desktop.DefaultCursor
	}
	shell.AttachHoverChildren(content, on)
	l.Rows.Add(row)
	return row
}

// hover moves the pill to row i as the pointer arrives on it, and fades it
// out as the pointer leaves the row it is on.
func (l *HoverList) hover(i int, in bool) {
	switch {
	case in:
		l.moveTo(i)
		if l.Held != nil {
			*l.Held = l.keys[i]
		}
	case l.hovered == i:
		l.moveTo(-1)
		if l.Held != nil {
			*l.Held = ""
		}
	}
}

// restoreHeld lights the row Held names, as it stands, once the rows are laid
// out: the pointer is still on it after the click that drew the list again.
func (l *HoverList) restoreHeld() {
	if l.hovered >= 0 || l.Held == nil || *l.Held == "" {
		return
	}
	for i, key := range l.keys {
		if key == *l.Held && i < len(l.Rows.Objects) {
			pos, size := l.frame(i)
			if size.Width <= 0 || size.Height <= 0 {
				return
			}
			l.hovered = i
			l.pill.ShowAt(pos, size)
			return
		}
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
	l.restoreHeld()
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

// hugLink keeps a link no wider than its words, so a click beside them lands
// on the row it sits in rather than on the link. A link too long for the row
// still shortens to fit.
func hugLink(link fyne.CanvasObject) fyne.CanvasObject {
	h, ok := link.(*shell.HoverLink)
	if !ok {
		return link
	}
	return container.New(&hugLinkLayout{link: h}, h)
}

type hugLinkLayout struct {
	link *shell.HoverLink
	// full is the link's width with its whole text showing, measured on an
	// untruncated copy of it.
	full float32
}

// natural is the link's width with its whole text showing.
func (l *hugLinkLayout) natural() float32 {
	if l.full == 0 {
		measure := widget.NewHyperlink(l.link.Text, l.link.URL)
		measure.SizeName = l.link.SizeName
		measure.TextStyle = l.link.TextStyle
		// A truncating link wants an inner padding more each side than the
		// width the copy asks for, or it shortens text that would fit.
		l.full = measure.MinSize().Width + 2*theme.InnerPadding()
	}
	return l.full
}

func (l *hugLinkLayout) MinSize([]fyne.CanvasObject) fyne.Size { return l.link.MinSize() }

func (l *hugLinkLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	l.link.Move(fyne.Position{})
	l.link.Resize(fyne.NewSize(min(size.Width, max(l.natural(), l.link.MinSize().Width)), size.Height))
}
