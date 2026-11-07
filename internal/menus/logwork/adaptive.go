package logwork

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/shared/shell"
)

// ---- Log Work on a small screen ----
//
// As with the layouts in widgets/adaptive.go, these only change anything once
// their content stops fitting, and remember what they decided from the last
// Layout.

// cardActionsLayout is a day card's bottom row: Push and any tags at the left,
// the icon buttons at the right. A tag that does not fit moves to its own line
// above, so tags never widen the column. Objects: push, tags, actions.
type cardActionsLayout struct{ stacked bool }

func (l *cardActionsLayout) fits(objects []fyne.CanvasObject, width float32) bool {
	total := float32(len(objects)-1) * theme.Padding()
	for _, o := range objects {
		total += o.MinSize().Width
	}
	return total <= width
}

func (l *cardActionsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	last := len(objects) - 1
	push, tags, actions := objects[0], objects[1:last], objects[last]
	am := actions.MinSize()
	l.stacked = len(tags) > 0 && !l.fits(objects, size.Width)
	pad := theme.Padding()
	if !l.stacked {
		x := float32(0)
		for _, o := range objects[:last] {
			m := o.MinSize()
			o.Move(fyne.NewPos(x, 0))
			o.Resize(fyne.NewSize(m.Width, size.Height))
			x += m.Width + pad
		}
		actions.Move(fyne.NewPos(size.Width-am.Width, 0))
		actions.Resize(fyne.NewSize(am.Width, size.Height))
		return
	}
	y := float32(0)
	for _, tag := range tags {
		m := tag.MinSize()
		tag.Move(fyne.NewPos(0, y))
		tag.Resize(m)
		y += m.Height + pad
	}
	row := max(push.MinSize().Height, am.Height)
	push.Move(fyne.NewPos(0, y))
	push.Resize(fyne.NewSize(push.MinSize().Width, row))
	actions.Move(fyne.NewPos(size.Width-am.Width, y))
	actions.Resize(fyne.NewSize(am.Width, row))
}

func (l *cardActionsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	last := len(objects) - 1
	push, tags, actions := objects[0].MinSize(), objects[1:last], objects[last].MinSize()
	size := fyne.NewSize(push.Width+theme.Padding()+actions.Width, max(push.Height, actions.Height))
	for _, tag := range tags {
		m := tag.MinSize()
		size.Width = max(size.Width, m.Width)
		if l.stacked {
			size.Height += m.Height + theme.Padding()
		} else {
			size.Height = max(size.Height, m.Height)
		}
	}
	return size
}

// dayCaptionLayout is shell.SplitCaption for a day with work waiting on it. The
// first caption carries the score and the waiting minutes on one line, too long
// for a narrow column, so there it gives way to the two pieces on separate lines.
// Objects: whole, projected, score, waiting.
type dayCaptionLayout struct{ split bool }

func (l *dayCaptionLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	whole, score, waiting := objects[0], objects[2], objects[3]
	l.split = whole.MinSize().Width > size.Width
	if !l.split {
		whole.Show()
		score.Hide()
		waiting.Hide()
		shell.SplitCaption{}.Layout(objects[:2], size)
		return
	}
	whole.Hide()
	score.Show()
	waiting.Show()
	y := float32(0)
	for _, o := range objects[1:] {
		o.Resize(o.MinSize())
	}
	for _, o := range []fyne.CanvasObject{score, waiting, objects[1]} {
		o.Move(fyne.NewPos(0, y))
		y += o.MinSize().Height + theme.Padding()
	}
}

func (l *dayCaptionLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	size := shell.SplitCaption{}.MinSize(objects[:2])
	size.Width = 0
	for _, o := range objects[1:] {
		size.Width = max(size.Width, o.MinSize().Width)
	}
	if l.split {
		line := objects[2].MinSize().Height
		size.Height = line*3 + theme.Padding()*2
	}
	return size
}

// fitCaption is a caption that cuts itself off with an ellipsis at the width it
// is given, rather than printing past the edge of its tile.
func fitCaption(text string, ink color.Color) fyne.CanvasObject {
	t := canvas.NewText(text, ink)
	t.TextSize = theme.CaptionTextSize()
	return container.New(&fitTextLayout{full: text}, t)
}

type fitTextLayout struct{ full string }

func (l *fitTextLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	t := objects[0].(*canvas.Text)
	if size.Width > 0 {
		if fitted := shell.FitText(l.full, t.TextSize, t.TextStyle, size.Width); fitted != t.Text {
			t.Text = fitted
			t.Refresh()
		}
	}
	t.Move(fyne.NewPos(0, 0))
	t.Resize(size)
}

// MinSize asks for no width: the tile decides how much there is.
func (l *fitTextLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, objects[0].MinSize().Height)
}
