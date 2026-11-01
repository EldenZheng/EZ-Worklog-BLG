package sprintcalendar

import (
	"fmt"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// The resize hit area is inside the bubble's bottom edge.
const PlannerPixelsPerMinute = float32(1.2)

type plannerScale struct{ pixelsPerMinute float32 }

func (s *plannerScale) Pixels() float32 {
	if s == nil || s.pixelsPerMinute <= 0 {
		return PlannerPixelsPerMinute
	}
	return s.pixelsPerMinute
}

type PlannerDurationLayout struct {
	minutes int
	scale   *plannerScale
}

func (l *PlannerDurationLayout) Pixels() float32 { return l.scale.Pixels() }

func (l *PlannerDurationLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(80, float32(l.minutes)*l.Pixels())
}

func (*PlannerDurationLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.Position{})
	objects[0].Resize(size)
	if len(objects) > 1 {
		h := fyne.Min(6, size.Height)
		objects[1].Move(fyne.NewPos(0, size.Height-h))
		objects[1].Resize(fyne.NewSize(size.Width, h))
	}
}

type PlannerResizeGrip struct {
	widget.BaseWidget
	k                 *shell.KanbanUI
	Key               string
	Index             int
	Layout            *PlannerDurationLayout
	card              *shell.PlannerDragCard
	Face              *PlannerTimedFace
	Started           bool
	original, Preview int
	delta             float32
	pixelsPerMinute   float32
}

func (g *PlannerResizeGrip) CreateRenderer() fyne.WidgetRenderer {
	// This transparent hit area overlays the card, rather than adding a handle
	// panel below it. The small line is painted by the bubble itself.
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

func (*PlannerResizeGrip) Cursor() desktop.Cursor { return desktop.VResizeCursor }

func (g *PlannerResizeGrip) Dragged(e *fyne.DragEvent) {
	t := g.k.Plan.Tasks[g.Key]
	if g.Index < 0 || g.Index >= len(t.Blocks) || t.Done {
		return
	}
	if !g.Started {
		g.Started = true
		g.k.CalendarResizing = true
		g.original = t.Blocks[g.Index].Minutes
		g.delta = 0
		g.pixelsPerMinute = g.Layout.Pixels()
		g.card.ResetDates()
	}
	g.delta += e.Dragged.DY
	max := 1440 - t.Blocks[g.Index].Start
	minutes := SnapPlannerDuration(float64(g.original)+float64(g.delta/g.pixelsPerMinute), max)
	g.Preview = minutes
	g.Layout.minutes = minutes
	text := fmt.Sprintf("%dm", minutes)
	if _, err := backend.ResizePlannerBlock(g.k.Plan, g.Key, g.Index, minutes); err != nil {
		text = fmt.Sprintf("%dm · time unavailable", minutes)
	}
	g.Face.duration = text
	g.Face.Refresh()
	g.k.CalendarBody.Refresh()
}

func (g *PlannerResizeGrip) DragEnd() {
	if !g.Started {
		return
	}
	g.Started = false
	g.k.CalendarResizing = false
	p, err := backend.ResizePlannerBlock(g.k.Plan, g.Key, g.Index, g.Preview)
	if err != nil {
		g.Layout.minutes = g.original
		g.Face.duration = fmt.Sprintf("%dm", g.original)
		g.Face.Refresh()
		g.k.SetMessage(err.Error())
		g.k.CalendarBody.Refresh()
		return
	}
	if g.k.Save(p) {
		g.k.SetMessage("Duration saved locally; GitHub estimate unchanged.")
		DrawKeepingCalendarScroll(g.k)
	} else {
		g.Layout.minutes = g.original
		g.Face.duration = fmt.Sprintf("%dm", g.original)
		g.Face.Refresh()
		g.k.CalendarBody.Refresh()
	}
}

func SnapPlannerDuration(minutes float64, max int) int {
	limit := max / backend.PlannerDurationStep * backend.PlannerDurationStep
	if limit < backend.PlannerDurationStep {
		return 0
	}
	snapped := int(math.Round(minutes/backend.PlannerDurationStep)) * backend.PlannerDurationStep
	if snapped < backend.PlannerDurationStep {
		snapped = backend.PlannerDurationStep
	}
	if snapped > limit {
		snapped = limit
	}
	return snapped
}
