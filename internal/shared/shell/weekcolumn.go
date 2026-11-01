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
)

// ---- drop targets ----

// DayColumn is a day in the strip and the thing a dragged card lands on. The
// driver keeps sending hover events to everything except the card being dragged,
// so hovering is what says where a drop would go.
type DayColumn struct {
	widget.BaseWidget
	Ui    *UI
	Date  string
	Body  fyne.CanvasObject
	Frame *canvas.Rectangle

	Mins     int // already on GitHub
	Waiting  int // saved here, standing on this day
	Planned  int // Sprint Calendar time not yet logged or drafted
	BaseFill color.Color
	Pulse    *fyne.Animation // running only while a card hovers this day
}

func (c *DayColumn) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.Body)
}

func (c *DayColumn) MouseIn(*desktop.MouseEvent) {
	c.Ui.HoverDay = c.Date
	Hooks.MarkDropTargets(c.Ui)
}

func (c *DayColumn) MouseMoved(*desktop.MouseEvent) {}

func (c *DayColumn) MouseOut() {
	// Only clear what this column set: leaving one column for the next fires the
	// new column's MouseIn first, and blindly clearing would undo it.
	if c.Ui.HoverDay == c.Date {
		c.Ui.HoverDay = ""
		Hooks.MarkDropTargets(c.Ui)
	}
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
	to := BlendColor(bg, theme.Color(theme.ColorNamePrimary), backend.WeekDraftTint)
	if c.Mins+c.Waiting+c.Planned+dropping >= backend.Target {
		to = BlendColor(bg, theme.Color(theme.ColorNameSuccess), backend.WeekDoneTint)
	}
	c.Pulse = canvas.NewColorRGBAAnimation(c.BaseFill, to, pulsePeriod, func(col color.Color) {
		c.Frame.FillColor = col
		c.Frame.Refresh()
	})
	c.Pulse.AutoReverse = true
	c.Pulse.RepeatCount = fyne.AnimationRepeatForever
	c.Pulse.Start()
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
}
