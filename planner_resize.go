package main

import (
	"fmt"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// The resize hit area is inside the bubble's bottom edge.
const plannerPixelsPerMinute = float32(1.2)

type plannerScale struct{ pixelsPerMinute float32 }

func (s *plannerScale) pixels() float32 {
	if s == nil || s.pixelsPerMinute <= 0 {
		return plannerPixelsPerMinute
	}
	return s.pixelsPerMinute
}

type plannerDurationLayout struct {
	minutes int
	scale   *plannerScale
}

func (l *plannerDurationLayout) pixels() float32 { return l.scale.pixels() }

func (l *plannerDurationLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(80, float32(l.minutes)*l.pixels())
}
func (*plannerDurationLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.Position{})
	objects[0].Resize(size)
	if len(objects) > 1 {
		h := fyne.Min(6, size.Height)
		objects[1].Move(fyne.NewPos(0, size.Height-h))
		objects[1].Resize(fyne.NewSize(size.Width, h))
	}
}

type plannerResizeGrip struct {
	widget.BaseWidget
	k                 *kanbanUI
	key               string
	index             int
	layout            *plannerDurationLayout
	card              *plannerDragCard
	face              *plannerTimedFace
	started           bool
	original, preview int
	delta             float32
	pixelsPerMinute   float32
}

func (g *plannerResizeGrip) CreateRenderer() fyne.WidgetRenderer {
	// This transparent hit area overlays the card, rather than adding a handle
	// panel below it. The small line is painted by the bubble itself.
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}
func (*plannerResizeGrip) Cursor() desktop.Cursor { return desktop.VResizeCursor }
func (g *plannerResizeGrip) Dragged(e *fyne.DragEvent) {
	t := g.k.plan.Tasks[g.key]
	if g.index < 0 || g.index >= len(t.Blocks) || t.Done {
		return
	}
	if !g.started {
		g.started = true
		g.k.calendarResizing = true
		g.original = t.Blocks[g.index].Minutes
		g.delta = 0
		g.pixelsPerMinute = g.layout.pixels()
		g.card.resetDates()
	}
	g.delta += e.Dragged.DY
	max := 1440 - t.Blocks[g.index].Start
	minutes := snapPlannerDuration(float64(g.original)+float64(g.delta/g.pixelsPerMinute), max)
	g.preview = minutes
	g.layout.minutes = minutes
	text := fmt.Sprintf("%dm", minutes)
	if _, err := resizePlannerBlock(g.k.plan, g.key, g.index, minutes); err != nil {
		text = fmt.Sprintf("%dm · time unavailable", minutes)
	}
	g.face.duration = text
	g.face.Refresh()
	g.k.calendarBody.Refresh()
}
func (g *plannerResizeGrip) DragEnd() {
	if !g.started {
		return
	}
	g.started = false
	g.k.calendarResizing = false
	p, err := resizePlannerBlock(g.k.plan, g.key, g.index, g.preview)
	if err != nil {
		g.layout.minutes = g.original
		g.face.duration = fmt.Sprintf("%dm", g.original)
		g.face.Refresh()
		g.k.setMessage(err.Error())
		g.k.calendarBody.Refresh()
		return
	}
	if g.k.save(p) {
		g.k.setMessage("Duration saved locally; GitHub estimate unchanged.")
		g.k.drawKeepingCalendarScroll()
	} else {
		g.layout.minutes = g.original
		g.face.duration = fmt.Sprintf("%dm", g.original)
		g.face.Refresh()
		g.k.calendarBody.Refresh()
	}
}

func snapPlannerDuration(minutes float64, max int) int {
	limit := max / plannerDurationStep * plannerDurationStep
	if limit < plannerDurationStep {
		return 0
	}
	snapped := int(math.Round(minutes/plannerDurationStep)) * plannerDurationStep
	if snapped < plannerDurationStep {
		snapped = plannerDurationStep
	}
	if snapped > limit {
		snapped = limit
	}
	return snapped
}
