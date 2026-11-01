package shell

import (
	_ "embed"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/shared/backend"
)

// dayCellMinHeight keeps a cell legible when the window is too short for the
// calendar to take its full height. It carries three lines now — the date, the
// day's score against the target, and its share of it — so the floor is the
// height those need rather than the two lines it used to hold.
//
// cellCornerRadius is shared by the cell's frame and the fill inside it — two
// different radii would leave the fill showing at the corners of the frame it is
// meant to sit in.
const (
	DayCellMinHeight = 96
	CellCornerRadius = 8
)

// dayStateStyle is how one day is painted: the wash behind it and the colour its
// score is written in.
type dayStateStyle struct {
	Fill      color.Color
	TextColor fyne.ThemeColorName
}

// DayState sorts a day into one of four, which is what the month is read by.
//
// Done on the board is stated in full-strength green. Done only once the drafts
// are counted is the same green at the draft weight — the work is finished, the
// sending is not, and those are different claims. Worked and still short is the
// warning colour, the same one the chart's target line and the report's
// short-day rows use. A day with nothing on it is not short, it is untouched,
// and is left unpainted so a month not yet worked does not read as a failed one.
//
// Draft-complete is tested before short because both are true of such a day, and
// yellow there would send you looking for hours already worked.
func DayState(mins, draft int) dayStateStyle {
	switch {
	case mins >= backend.Target:
		return dayStateStyle{
			Fill:      BlendColor(theme.Color(theme.ColorNameBackground), theme.Color(theme.ColorNameSuccess), backend.WeekDoneTint),
			TextColor: theme.ColorNameSuccess,
		}
	case IsDraftComplete(mins, draft):
		return dayStateStyle{Fill: DraftDoneFill(), TextColor: theme.ColorNameSuccess}
	case mins > 0 || draft > 0:
		return dayStateStyle{Fill: ShortFill(), TextColor: theme.ColorNameWarning}
	default:
		return dayStateStyle{Fill: color.Transparent, TextColor: theme.ColorNameForeground}
	}
}

// SplitCaption puts one caption at each end of a line, and drops the second
// onto a line of its own when the two will not fit side by side.
//
// A day column is a seventh of the window. Now that each caption carries what
// the day still owes as well as its score, two of them side by side overflow the
// column on any window short of very wide — and fyne does not clip, so the
// overflow prints over the neighbouring day rather than being cut off.
//
// Its width is the wider of the two rather than their sum, so a long caption
// never widens the strip, the tab, and the window's smallest usable size with
// it. Its height reserves both rows whenever there are two captions: the layout
// cannot ask how wide it will be before it is measured, so the row it might need
// has to be there already.
type SplitCaption struct{}

func (SplitCaption) MinSize(objs []fyne.CanvasObject) fyne.Size {
	live := captionsIn(objs)
	size := fyne.NewSize(0, 0)
	for _, o := range live {
		m := o.MinSize()
		size.Width = fyne.Max(size.Width, m.Width)
		size.Height = fyne.Max(size.Height, m.Height)
	}
	if len(live) > 1 {
		size.Height = size.Height*float32(len(live)) + theme.Padding()*float32(len(live)-1)
	}
	return size
}

func (SplitCaption) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	live := captionsIn(objs)
	for _, o := range objs {
		o.Resize(o.MinSize())
	}
	if len(live) == 0 {
		return
	}
	if len(live) == 1 {
		m := live[0].MinSize()
		live[0].Move(fyne.NewPos(0, (size.Height-m.Height)/2))
		return
	}

	// Side by side only while they both fit with a gap between them; otherwise
	// stacked, which is what a narrow window gets.
	wide := float32(0)
	for _, o := range live {
		wide += o.MinSize().Width
	}
	if wide+theme.Padding() <= size.Width {
		for i, o := range live {
			m := o.MinSize()
			x := float32(0)
			if i > 0 {
				x = size.Width - m.Width
			}
			o.Move(fyne.NewPos(x, (size.Height-m.Height)/2))
		}
		return
	}
	y := float32(0)
	for _, o := range live {
		m := o.MinSize()
		o.Move(fyne.NewPos(0, y))
		y += m.Height + theme.Padding()
	}
}
