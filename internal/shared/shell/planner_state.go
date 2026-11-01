package shell

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/widgets"
)

type PlannerDateTarget struct {
	Date        string
	Object      fyne.CanvasObject
	Frame       *canvas.Rectangle
	Base        color.Color
	StrokeColor color.Color
	StrokeWidth float32
}

var PlannerAvailableBlue = color.NRGBA{R: 106, G: 186, B: 236, A: 255}

// The MEETING bubble is a local-only item that sits in the tray whatever sprint
// is selected, so time can be set aside for meetings on any date. It belongs to
// no board and no sprint, and nothing about it is sent to GitHub.
const PlannerMeetingKey = "local/meeting"

var plannerMeetingBoard = backend.SprintBoard{ID: "local", Title: "Meeting"}

func PlannerMeetingCandidate() backend.SprintCandidate {
	return backend.SprintCandidate{
		Key:  PlannerMeetingKey,
		Item: backend.SprintItem{ID: "meeting", Title: "MEETING", Effort: 60},
		// Any date is inside the meeting's "sprint".
		Sprint: backend.SprintIteration{ID: "meeting", Title: "Meeting", StartDate: "2000-01-01", Duration: 100 * 366},
	}
}

type PlannerDragCard struct {
	widget.BaseWidget
	K         *KanbanUI
	Candidate backend.SprintCandidate
	Index     int // -1: unscheduled; otherwise move this block only
	Content   fyne.CanvasObject
	lifted    bool
	Hover     *PlannerDateTarget
	Day       *PlannerDayHover
}

func (c *PlannerDragCard) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.Content)
}

func (c *PlannerDragCard) Cursor() desktop.Cursor { return widgets.GrabCursor{} }

func (c *PlannerDragCard) Tapped(*fyne.PointEvent) { Hooks.KanbanUIOpenCandidate(c.K, c.Candidate) }

func (c *PlannerDragCard) Dragged(e *fyne.DragEvent) {
	k := c.K
	if backend.PlannerIssueFinished(c.Candidate.Item, k.Plan.Tasks[c.Candidate.Key]) || k.CalendarResizing {
		return
	}
	if !c.lifted {
		c.lifted = true
		k.CalendarDrag = c
		b, it, _ := k.BoardItem(c.Candidate)
		bg := canvas.NewRectangle(BlendColor(theme.Color(theme.ColorNameInputBackground), k.Accent(b, it), 0.5))
		bg.CornerRadius = 10
		ghost := container.NewStack(bg, container.NewPadded(widget.NewLabel(Truncate(c.Candidate.Item.Title, 35))))
		ghost.Resize(ghost.MinSize())
		k.CalendarGhost.Objects = []fyne.CanvasObject{ghost}
	}
	if len(k.CalendarGhost.Objects) > 0 {
		k.CalendarGhost.Objects[0].Move(e.AbsolutePosition.Subtract(fyne.CurrentApp().Driver().AbsolutePositionForObject(k.CalendarGhost)).Add(fyne.NewPos(12, 12)))
		k.CalendarGhost.Refresh()
	}
	if scroll := PlannerScroll(k.CalendarClip); scroll != nil && pointIn(k.CalendarClip, e.AbsolutePosition) {
		origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(k.CalendarClip)
		y := e.AbsolutePosition.Y - origin.Y
		offset := scroll.Offset
		if y < 42 {
			offset.Y -= 24
		} else if y > scroll.Size().Height-42 {
			offset.Y += 24
		}
		scroll.ScrollToOffset(offset)
	}
	c.HighlightDates(&e.AbsolutePosition)
}

func (c *PlannerDragCard) HighlightDates(at *fyne.Position) {
	k := c.K
	c.Hover = nil
	if k.CalendarResizing || k.CalendarDrag != nil && k.CalendarDrag != c || c.Index >= 0 && !c.lifted {
		return
	}
	for _, target := range k.Dates {
		_, valid := backend.PlannerSlot(k.Plan, c.Candidate, c.Index, target.Date)
		target.Frame.FillColor = target.Base
		target.Frame.StrokeWidth = target.StrokeWidth
		target.Frame.StrokeColor = target.StrokeColor
		if valid {
			target.Frame.FillColor = widgets.WithAlpha(PlannerAvailableBlue, 0.13)
			target.Frame.StrokeColor = PlannerAvailableBlue
			target.Frame.StrokeWidth = 2
			if at != nil && pointIn(target.Object, *at) && pointIn(k.CalendarClip, *at) {
				c.Hover = target
				target.Frame.FillColor = widgets.WithAlpha(PlannerAvailableBlue, 0.25)
				target.Frame.StrokeColor = widgets.LogoGold
				target.Frame.StrokeWidth = 4
			}
		}
		target.Frame.Refresh()
	}
}

func (c *PlannerDragCard) MouseIn(*desktop.MouseEvent) {
	if c.Day != nil {
		c.Day.Enter(c)
	}
	if !c.lifted && c.Index < 0 && c.K.CalendarDrag == nil && !c.K.CalendarResizing {
		c.HighlightDates(nil)
	}
}

func (c *PlannerDragCard) MouseMoved(*desktop.MouseEvent) {}

func (c *PlannerDragCard) MouseOut() {
	if c.Day != nil {
		c.Day.Leave(c)
	}
	if !c.lifted && c.Index < 0 && c.K.CalendarDrag == nil && !c.K.CalendarResizing {
		c.ResetDates()
	}
}

func (c *PlannerDragCard) ResetDates() {
	for _, t := range c.K.Dates {
		t.Frame.FillColor = t.Base
		t.Frame.StrokeWidth = t.StrokeWidth
		t.Frame.StrokeColor = t.StrokeColor
		t.Frame.Refresh()
	}
}

func (c *PlannerDragCard) DragEnd() {
	if !c.lifted {
		return
	}
	c.lifted = false
	k := c.K
	if k.CalendarDrag == c {
		k.CalendarDrag = nil
	}
	defer func() {
		if k.RedrawPending {
			Hooks.KanbanUIDraw(k)
		}
	}()
	k.CalendarGhost.Objects = nil
	k.CalendarGhost.Refresh()
	for _, t := range k.Dates {
		t.Frame.FillColor = t.Base
		t.Frame.StrokeWidth = t.StrokeWidth
		t.Frame.StrokeColor = t.StrokeColor
		t.Frame.Refresh()
	}
	target := c.Hover
	c.Hover = nil
	if target == nil {
		return
	}
	p, err := backend.MovePlannerIssue(k.Plan, c.Candidate, c.Index, target.Date)
	if err != nil {
		k.Ui.Errf(err)
		return
	}
	if k.Save(p) {
		k.SetMessage("Saved locally · GitHub estimates and dates were not changed.")
		Hooks.KanbanUIDrawKeepingCalendarScroll(k)
	}
}

// Share hover ownership across the date, its bubbles, links and Clear button.
type PlannerDayHover struct {
	widget.BaseWidget
	Body     fyne.CanvasObject
	Clear    *PlannerDayClearButton
	owner    fyne.CanvasObject
	CanClear bool
	Header   *fyne.Container
	View     fyne.Canvas
}

func (h *PlannerDayHover) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(h.Body)
}

func (h *PlannerDayHover) Enter(owner fyne.CanvasObject) {
	h.owner = owner
	if h.CanClear {
		h.showClear(true)
	}
}

func (h *PlannerDayHover) Leave(owner fyne.CanvasObject) {
	if h.owner == owner {
		h.owner = nil
		h.showClear(false)
	}
}

func (h *PlannerDayHover) showClear(show bool) {
	if h.Clear.Visible() == show {
		return
	}
	if show {
		h.Clear.Show()
	} else {
		h.Clear.Hide()
	}
	if h.Header != nil {
		h.Header.Refresh()
		// Initially hidden widgets have not been associated with the native
		// canvas yet. Refresh their visible parent directly, without a click.
		if h.View != nil {
			h.View.Refresh(h.Header)
		}
	}
}

func (h *PlannerDayHover) MouseIn(*desktop.MouseEvent) { h.Enter(h) }

func (h *PlannerDayHover) MouseMoved(*desktop.MouseEvent) { h.Enter(h) }

func (h *PlannerDayHover) MouseOut() { h.Leave(h) }

type PlannerDayClearButton struct {
	widget.Button
	Day *PlannerDayHover
}

func (b *PlannerDayClearButton) MouseIn(e *desktop.MouseEvent) { b.Day.Enter(b); b.Button.MouseIn(e) }

func (b *PlannerDayClearButton) MouseMoved(e *desktop.MouseEvent) {
	b.Day.Enter(b)
	b.Button.MouseMoved(e)
}

func (b *PlannerDayClearButton) MouseOut() { b.Button.MouseOut(); b.Day.Leave(b) }

func PlannerScroll(o fyne.CanvasObject) *container.Scroll {
	switch scroll := o.(type) {
	case *container.Scroll:
		return scroll
	case *PlannerFastScroll:
		return &scroll.Scroll
	}
	return nil
}

func ScrollOffset(o fyne.CanvasObject) fyne.Position {
	if scroll := PlannerScroll(o); scroll != nil {
		return scroll.Offset
	}
	return fyne.Position{}
}

func RestoreScroll(o fyne.CanvasObject, offset fyne.Position) {
	if scroll := PlannerScroll(o); scroll != nil {
		scroll.ScrollToOffset(offset)
	}
}

// Only Month mode boosts wheel/trackpad movement. Scrollbar dragging remains
// native, and the rest of the app keeps its normal speed.
type PlannerFastScroll struct{ container.Scroll }

func (s *PlannerFastScroll) Scrolled(e *fyne.ScrollEvent) {
	boosted := *e
	boosted.Scrolled.DY *= 6
	s.Scroll.Scrolled(&boosted)
}

var PlannerPlanColor = color.NRGBA{R: 151, G: 112, B: 207, A: 255}

// PlannedBarParts is a day bar with the planned time in purple after the
// logged and drafted parts: the bar only, the captions are left alone.
func PlannedBarParts(cfg backend.Config, byOrg, draft map[string]int, planned int) []MeterPart {
	if planned <= 0 {
		return MeterParts(cfg, byOrg, draft, backend.Target)
	}
	denominator := backend.Target
	total := planned
	for _, m := range byOrg {
		total += m
	}
	for _, m := range draft {
		total += m
	}
	if total > denominator {
		denominator = total
	}
	return append(MeterParts(cfg, byOrg, draft, denominator),
		MeterPart{Key: "@plan", Fill: PlannerPlanColor, Weight: float32(planned) / float32(denominator)})
}
