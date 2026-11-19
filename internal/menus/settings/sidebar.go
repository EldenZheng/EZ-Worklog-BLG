package settings

import (
	"fyne.io/fyne/v2"

	"worklog/internal/shared/shell"
)

// The scroll is painted first and extends behind half of Updates. Its bottom
// inset keeps the final control reachable above the floating capsule, matching
// the header/scroll relationship in Kanban's columns.
type sidebarFooterLayout struct {
	inset *shell.ColumnInset
	gap   float32
}

func (l *sidebarFooterLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	list, footer := objects[0].MinSize(), objects[1].MinSize()
	return fyne.NewSize(max(list.Width, footer.Width), max(list.Height, footer.Height+l.gap))
}

func (l *sidebarFooterLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	footer := objects[1]
	footer.Resize(fyne.NewSize(size.Width, footer.MinSize().Height))
	height := footer.MinSize().Height
	footer.Resize(fyne.NewSize(size.Width, height))
	footer.Move(fyne.NewPos(0, max(size.Height-height, 0)))
	clip := height / 2
	list := objects[0]
	if bottom := height - clip + l.gap; bottom != l.inset.Bottom {
		l.inset.Bottom = bottom
		list.Refresh()
	}
	list.Move(fyne.Position{})
	list.Resize(fyne.NewSize(size.Width, max(size.Height-clip, 0)))
}
