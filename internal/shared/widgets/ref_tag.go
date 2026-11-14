package widgets

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// RefTagLayout keeps a badge immediately beside an issue reference. The ref
// takes Full pixels when possible, shrinking before the badge drops below it.
type RefTagLayout struct {
	Full    float32
	stacked bool
}

func (l *RefTagLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	ref, tag := objects[0], objects[1].MinSize()
	width := min(l.Full, size.Width-tag.Width)
	l.stacked = width < l.Full/2
	ref.Move(fyne.NewPos(0, 0))
	if l.stacked {
		ref.Resize(fyne.NewSize(size.Width, ref.MinSize().Height))
		height := ref.MinSize().Height
		ref.Resize(fyne.NewSize(size.Width, height))
		objects[1].Move(fyne.NewPos(theme.InnerPadding(), height))
		objects[1].Resize(fyne.NewSize(min(tag.Width, size.Width), tag.Height))
		return
	}
	ref.Resize(fyne.NewSize(width, size.Height))
	objects[1].Move(fyne.NewPos(width, (size.Height-tag.Height)/2))
	objects[1].Resize(tag)
}

func (l *RefTagLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	ref, tag := objects[0].MinSize(), objects[1].MinSize()
	if l.stacked {
		return fyne.NewSize(max(ref.Width, tag.Width), ref.Height+tag.Height)
	}
	return fyne.NewSize(ref.Width+tag.Width, max(ref.Height, tag.Height))
}
