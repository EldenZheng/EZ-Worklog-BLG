package shell

import "fyne.io/fyne/v2"

// ColumnInset holds a column's cards (Kanban's, or the Sprint Calendar tray's)
// a set distance down from the top of their scroll, under the floating header.
type ColumnInset struct{ Top float32 }

func (c *ColumnInset) MinSize(objects []fyne.CanvasObject) fyne.Size {
	m := objects[0].MinSize()
	return fyne.NewSize(m.Width, m.Height+c.Top)
}

func (c *ColumnInset) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.NewPos(0, c.Top))
	objects[0].Resize(fyne.NewSize(size.Width, max(size.Height-c.Top, 0)))
}

// FloatingHeaderLayout lays a column's card scroll (first) from halfway down
// its header to the foot, and its header (second) across the top, over the
// cards. The cards' inset follows the header's height, which grows when a
// title wraps.
type FloatingHeaderLayout struct {
	Inset *ColumnInset
	Gap   float32
}

func (l *FloatingHeaderLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	list, head := objects[0].MinSize(), objects[1].MinSize()
	return fyne.NewSize(max(list.Width, head.Width), max(list.Height, head.Height+l.Gap))
}

func (l *FloatingHeaderLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	head := objects[1]
	head.Move(fyne.NewPos(0, 0))
	head.Resize(fyne.NewSize(size.Width, head.MinSize().Height))
	// A wrapped title knows its height only once it has its width.
	height := head.MinSize().Height
	head.Resize(fyne.NewSize(size.Width, height))
	// The cards' scroll starts halfway down the capsule, so a card scrolling
	// up slips under the capsule and is gone by its middle line.
	clip := height / 2
	list := objects[0]
	if top := height - clip + l.Gap; top != l.Inset.Top {
		l.Inset.Top = top
		list.Refresh()
	}
	list.Move(fyne.NewPos(0, clip))
	list.Resize(fyne.NewSize(size.Width, max(size.Height-clip, 0)))
}
