package main

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// One elapsed-minutes ruler beside each calendar week. Minor lines match the
// quarter-hour resize increments; major labels show minutes, never clock time.
type durationRuler struct {
	widget.BaseWidget
	minutes int
	scale   *plannerScale
}

func newDurationRuler(minutes int) *durationRuler {
	r := &durationRuler{minutes: minutes}
	r.ExtendBaseWidget(r)
	return r
}
func (r *durationRuler) SetMinutes(minutes int) {
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
func (r *durationRuler) CreateRenderer() fyne.WidgetRenderer {
	renderer := &durationRulerRenderer{ruler: r, built: -1}
	renderer.rebuild()
	return renderer
}

type durationRulerRenderer struct {
	ruler        *durationRuler
	marks        []int
	lines        []*canvas.Line
	labels       []*canvas.Text
	labelMinutes []int
	objects      []fyne.CanvasObject
	built        int
}

func (r *durationRulerRenderer) rebuild() {
	r.built = r.ruler.minutes
	r.marks = rulerMinutes(r.built)
	r.lines = nil
	r.labels = nil
	r.labelMinutes = nil
	r.objects = nil
	title := canvas.NewText("Minutes", theme.Color(theme.ColorNamePlaceHolder))
	title.TextSize = 10
	title.Move(fyne.NewPos(2, 20))
	r.objects = append(r.objects, title)
	for _, m := range r.marks {
		alpha := float32(0.06)
		if m%60 == 0 {
			alpha = 0.18
		}
		line := canvas.NewLine(withAlpha(theme.Color(theme.ColorNameForeground), alpha))
		line.StrokeWidth = 1
		r.lines = append(r.lines, line)
		r.objects = append(r.objects, line)
		if m%60 == 0 || m == r.built {
			label := canvas.NewText(fmt.Sprintf("%dm", m), theme.Color(theme.ColorNamePlaceHolder))
			label.TextSize = 10
			label.TextStyle.Monospace = true
			r.labels = append(r.labels, label)
			r.labelMinutes = append(r.labelMinutes, m)
			r.objects = append(r.objects, label)
		}
	}
}
func (r *durationRulerRenderer) Layout(size fyne.Size) {
	for i, m := range r.marks {
		y := plannerDayHeaderHeight + float32(m)*r.ruler.scale.pixels()
		r.lines[i].Position1 = fyne.NewPos(plannerRulerWidth, y)
		r.lines[i].Position2 = fyne.NewPos(size.Width, y)
	}
	for i, label := range r.labels {
		y := plannerDayHeaderHeight + float32(r.labelMinutes[i])*r.ruler.scale.pixels()
		label.Resize(label.MinSize())
		label.Move(fyne.NewPos(plannerRulerWidth-6-label.MinSize().Width, y-label.MinSize().Height/2))
	}
}
func (r *durationRulerRenderer) MinSize() fyne.Size {
	return fyne.NewSize(plannerRulerWidth, plannerDayHeaderHeight+float32(r.ruler.minutes)*r.ruler.scale.pixels()+8)
}
func (r *durationRulerRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *durationRulerRenderer) Refresh() {
	if r.built != r.ruler.minutes {
		r.rebuild()
	}
	r.Layout(r.ruler.Size())
	canvas.Refresh(r.ruler)
}
func (*durationRulerRenderer) Destroy() {}
