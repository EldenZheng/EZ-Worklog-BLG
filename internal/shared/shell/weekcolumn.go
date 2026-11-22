package shell

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/widgets"
)

// ---- drop targets ----

// DayColumn is a day in the strip and the thing a dragged card lands on. The
// driver keeps sending hover events to everything except the card being dragged,
// so hovering is what says where a drop would go.
type DayColumn struct {
	widget.BaseWidget
	Ui             *UI
	Date           string
	Body           fyne.CanvasObject
	Frame          *canvas.Rectangle
	Heading        fyne.CanvasObject
	HeadingContent fyne.CanvasObject
	HeadingFill    *canvas.Rectangle
	DateHoverFrame *canvas.Rectangle
	DateHovered    bool
	// Edge redraws the day's outline over its cards while the heading is
	// pinned. Cards then pass behind the heading and show round it, inside
	// the day, as in a Kanban column, without running over its top edge.
	Edge *canvas.Rectangle
	// Cover hides cards in the strip above the pinned outline, the page's
	// own background between the view's top and the day's top edge.
	Cover *canvas.Rectangle
	// Clip is the day's own fill from its pinned top edge down to the middle
	// of its heading: as in a Kanban column, a card scrolling up slips under
	// the heading and is gone by its middle line.
	Clip *canvas.Rectangle

	Mins     int // already on GitHub
	Waiting  int // saved here, standing on this day
	Planned  int // Sprint Calendar time not yet logged on GitHub; separate from drafts
	BaseFill color.Color
	Pulse    *fyne.Animation // running only while a card hovers this day
}

func (c *DayColumn) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.Body)
}

func (c *DayColumn) MouseIn(*desktop.MouseEvent) {
	c.Ui.HoverDay = c.Date
	queueDropMarks(c.Ui)
}

func (c *DayColumn) MouseMoved(*desktop.MouseEvent) {}

func (c *DayColumn) MouseOut() {
	// Only clear what this column set: leaving one column for the next fires the
	// new column's MouseIn first, and blindly clearing would undo it.
	if c.Ui.HoverDay == c.Date {
		c.Ui.HoverDay = ""
		queueDropMarks(c.Ui)
	}
}

var dropMarksQueued bool

// queueDropMarks repaints the drop targets once the pointer move being handled
// is done. Moving onto a card in a day reaches the day as "left" and the card
// dragged over it then finds the day again; painting in between restarted
// the day's pulse on every card it crossed.
func queueDropMarks(ui *UI) {
	if dropMarksQueued {
		return
	}
	dropMarksQueued = true
	fyne.Do(func() {
		dropMarksQueued = false
		Hooks.MarkDropTargets(ui)
	})
}

// pulsePeriod is one breath of the day under a carried card. Slow enough to read
// as the day answering, fast enough to answer before the card is let go.
const pulsePeriod = 700 * time.Millisecond

// StartPulse breathes the day the card is being held over between its own colour
// and the colour it would take if the card were dropped there — green when those
// minutes would finish the day. The drop is a guess until it lands, so the
// preview is shown as a pulse rather than painted on as though it already had.
func (c *DayColumn) StartPulse(dropping int) {
	if c.Pulse != nil {
		return // already breathing for this same card
	}
	bg := theme.Color(theme.ColorNameBackground)
	// Green only where the drop would actually finish the day. Otherwise the day
	// still lifts, towards the accent rather than towards done — a card has to
	// look like it can land somewhere even when landing there settles nothing.
	to := BlendColor(bg, widgets.LogoGold, backend.WeekDraftTint)
	if c.MeetsTarget(dropping) {
		to = BlendColor(bg, theme.Color(theme.ColorNameSuccess), backend.WeekDoneTint)
	}
	c.Pulse = canvas.NewColorRGBAAnimation(c.BaseFill, to, pulsePeriod, func(col color.Color) {
		c.Frame.FillColor = col
		c.Frame.Refresh()
		c.PaintEdge()
	})
	c.Pulse.AutoReverse = true
	c.Pulse.RepeatCount = fyne.AnimationRepeatForever
	c.Pulse.Start()
}

// MeetsTarget includes the minutes a drop would add to this day.
func (c *DayColumn) MeetsTarget(adding int) bool {
	return c.Mins+c.Waiting+c.Planned+adding >= backend.Target
}

// StopPulse puts the day back to the colour it earned on its own.
func (c *DayColumn) StopPulse() {
	if c.Pulse == nil {
		return
	}
	c.Pulse.Stop()
	c.Pulse = nil
	c.Frame.FillColor = c.BaseFill
	c.Frame.Refresh()
	c.PaintEdge()
}

// FollowHeading keeps the day's outline around its heading: once the page
// scrolls and the heading is pinned, the outline's top comes down with it,
// a padding under the top of the view, as it sits around the heading before
// scrolling. Cards scroll behind the heading, as in a Kanban column, and are
// gone by its middle line.
func (c *DayColumn) FollowHeading() {
	if c.Heading == nil || c.Frame == nil || c.Body == nil {
		return
	}
	y := c.Heading.Position().Y
	size := c.Body.Size()
	for _, r := range []*canvas.Rectangle{c.Frame, c.DateHoverFrame} {
		if r == nil {
			continue
		}
		r.Move(fyne.NewPos(0, y))
		r.Resize(fyne.NewSize(size.Width, max(0, size.Height-y)))
	}
	e, cover, clip := c.Edge, c.Cover, c.Clip
	if e == nil || cover == nil || clip == nil {
		return
	}
	if y <= 0 {
		e.Hide()
		cover.Hide()
		clip.Hide()
		return
	}
	// Both sit among the day's contents, inset a padding in the column.
	pad := theme.Padding()
	e.Move(fyne.NewPos(-pad, y-pad))
	e.Resize(fyne.NewSize(size.Width, max(0, size.Height-y)))
	e.Show()
	cover.FillColor = theme.Color(theme.ColorNameBackground)
	cover.Move(fyne.NewPos(-pad, y-2*pad-1))
	cover.Resize(fyne.NewSize(size.Width, pad+1))
	cover.Show()
	cover.Refresh()
	clip.Move(fyne.NewPos(-pad, y-pad))
	clip.Resize(fyne.NewSize(size.Width, pad+c.Heading.Size().Height/2))
	clip.Show()
	c.PaintEdge()
}

// PaintEdge copies the day's outline onto its pinned edge, and its fill onto
// the clip under the heading.
func (c *DayColumn) PaintEdge() {
	if c.Edge == nil || c.Frame == nil {
		return
	}
	if c.Clip != nil {
		fill := c.Frame.FillColor
		if fill == nil {
			fill = color.Transparent
		}
		if _, _, _, a := fill.RGBA(); a == 0 {
			fill = theme.Color(theme.ColorNameBackground)
		}
		c.Clip.FillColor = fill
		c.Clip.CornerRadius = c.Frame.CornerRadius
		c.Clip.Refresh()
	}
	c.Edge.FillColor = color.Transparent
	c.Edge.StrokeColor, c.Edge.StrokeWidth = c.Frame.StrokeColor, c.Frame.StrokeWidth
	c.Edge.CornerRadius = c.Frame.CornerRadius
	c.Edge.Refresh()
}
