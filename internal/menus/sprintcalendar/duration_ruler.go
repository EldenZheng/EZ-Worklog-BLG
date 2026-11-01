package sprintcalendar

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// One elapsed-minutes ruler beside each calendar week. Minor lines match the
// quarter-hour resize increments; major labels show minutes, never clock time.
type DurationRuler struct {
	widget.BaseWidget
	minutes int
	Scale   *plannerScale
}

func newDurationRuler(minutes int) *DurationRuler {
	r := &DurationRuler{minutes: minutes}
	r.ExtendBaseWidget(r)
	return r
}

func (r *DurationRuler) SetMinutes(minutes int) {
	if r.minutes == minutes {
		return
	}
	r.minutes = minutes
	r.Refresh()
}

func rulerMinutes(minutes int) []int {
	marks := []int{0}
	for m := 15; m < minutes; m += 15 {
		marks = append(marks, m)
	}
	if minutes > 0 {
		marks = append(marks, minutes)
	}
	return marks
}

func (r *DurationRuler) CreateRenderer() fyne.WidgetRenderer {
	renderer := &DurationRulerRenderer{ruler: r, built: -1}
	renderer.rebuild()
	return renderer
}

type DurationRulerRenderer struct {
	ruler        *DurationRuler
	Marks        []int
	Lines        []*canvas.Line
	Labels       []*canvas.Text
	labelMinutes []int
	objects      []fyne.CanvasObject
	built        int
}

func (r *DurationRulerRenderer) rebuild() {
	r.built = r.ruler.minutes
	r.Marks = rulerMinutes(r.built)
	r.Lines = nil
	r.Labels = nil
	r.labelMinutes = nil
	r.objects = nil
	title := canvas.NewText("Minutes", theme.Color(theme.ColorNamePlaceHolder))
	title.TextSize = 10
	title.Move(fyne.NewPos(2, 20))
	r.objects = append(r.objects, title)
	for _, m := range r.Marks {
		alpha := float32(0.06)
		if m%60 == 0 {
			alpha = 0.18
		}
		line := canvas.NewLine(widgets.WithAlpha(theme.Color(theme.ColorNameForeground), alpha))
		line.StrokeWidth = 1
		r.Lines = append(r.Lines, line)
		r.objects = append(r.objects, line)
		if m%60 == 0 || m == r.built {
			label := canvas.NewText(fmt.Sprintf("%dm", m), theme.Color(theme.ColorNamePlaceHolder))
			label.TextSize = 10
			label.TextStyle.Monospace = true
			r.Labels = append(r.Labels, label)
			r.labelMinutes = append(r.labelMinutes, m)
			r.objects = append(r.objects, label)
		}
	}
}

func (r *DurationRulerRenderer) Layout(size fyne.Size) {
	for i, m := range r.Marks {
		y := PlannerDayHeaderHeight + float32(m)*r.ruler.Scale.Pixels()
		r.Lines[i].Position1 = fyne.NewPos(plannerRulerWidth, y)
		r.Lines[i].Position2 = fyne.NewPos(size.Width, y)
	}
	for i, label := range r.Labels {
		y := PlannerDayHeaderHeight + float32(r.labelMinutes[i])*r.ruler.Scale.Pixels()
		label.Resize(label.MinSize())
		label.Move(fyne.NewPos(plannerRulerWidth-6-label.MinSize().Width, y-label.MinSize().Height/2))
	}
}

func (r *DurationRulerRenderer) MinSize() fyne.Size {
	return fyne.NewSize(plannerRulerWidth, PlannerDayHeaderHeight+float32(r.ruler.minutes)*r.ruler.Scale.Pixels()+8)
}

func (r *DurationRulerRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *DurationRulerRenderer) Refresh() {
	if r.built != r.ruler.minutes {
		r.rebuild()
	}
	r.Layout(r.ruler.Size())
	canvas.Refresh(r.ruler)
}

func (*DurationRulerRenderer) Destroy() {}
