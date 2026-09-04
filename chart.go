package main

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// dayChart is the report's bar graph, drawn as shapes rather than as a picture.
//
// It used to be a raster of fixed pixel size stretched to the window, which is
// why it looked squashed: the drawing was made once at one size and then pulled
// to another. Here every bar is a real canvas object placed from the width and
// height the widget is actually given, so nothing is scaled and nothing is
// distorted — and the bars can carry rounded caps, org colours and a hover
// highlight without any of it being painted into pixels first.
type dayChart struct {
	widget.BaseWidget
	cfg      Config
	days     []chartDay
	goal     int // the daily target, drawn as a guide line
	hovered  int // index of the bar under the pointer, -1 for none
	onHover  func(date string)
	onSelect func(date string)
}

// chartDay is one slot on the axis: a date and how its minutes split by org.
// draft is what is saved on this machine for that day and not yet pushed — it
// is stacked on top of the org segments rather than counted into total, which
// stays what GitHub actually holds.
type chartDay struct {
	date  string
	byOrg map[string]int
	total int
	draft int
}

func newDayChart(cfg Config, days []chartDay, goal int) *dayChart {
	c := &dayChart{cfg: cfg, days: days, goal: goal, hovered: -1}
	c.ExtendBaseWidget(c)
	return c
}

// Chart geometry. The plot keeps a strip at the bottom for date labels and a
// margin at the top so a record day does not touch the ceiling.
const (
	chartMinHeight  = 240
	chartAxisHeight = 20
	chartTopMargin  = 10
	chartSideMargin = 6
	chartBarGap     = 0.28 // share of a slot left as space between bars
	chartGuideWidth = 2    // the target line, thick enough to read over a bar
	chartBarRadius  = 4
)

func (c *dayChart) MinSize() fyne.Size { return fyne.NewSize(0, chartMinHeight) }

// maxValue is the top of the scale: the busiest day, never less than the target
// so the guide line always has somewhere to sit.
func (c *dayChart) maxValue() int {
	maxVal := c.goal
	for _, d := range c.days {
		// The draft rides on top of the bar, so the scale has to hold both or a
		// day with work waiting would draw off the top of the plot.
		if t := d.total + d.draft; t > maxVal {
			maxVal = t
		}
	}
	if maxVal <= 0 {
		maxVal = 1
	}
	return maxVal
}

// slotAt maps a pointer position to a bar index, or -1 outside the plot.
func (c *dayChart) slotAt(x float32) int {
	w := c.Size().Width - chartSideMargin*2
	// The margins are outside the plot. Checked before the arithmetic below,
	// which truncates towards zero and would round the left margin into the
	// first bar.
	if w <= 0 || len(c.days) == 0 || x < chartSideMargin || x >= chartSideMargin+w {
		return -1
	}
	i := int((x - chartSideMargin) / w * float32(len(c.days)))
	if i < 0 || i >= len(c.days) {
		return -1
	}
	return i
}

func (c *dayChart) MouseIn(e *desktop.MouseEvent) { c.MouseMoved(e) }

func (c *dayChart) MouseMoved(e *desktop.MouseEvent) {
	i := c.slotAt(e.Position.X)
	if i == c.hovered {
		return
	}
	c.hovered = i
	if c.onHover != nil {
		if i < 0 {
			c.onHover("")
		} else {
			c.onHover(c.days[i].date)
		}
	}
	c.Refresh()
}

func (c *dayChart) MouseOut() {
	c.hovered = -1
	if c.onHover != nil {
		c.onHover("")
	}
	c.Refresh()
}

func (c *dayChart) Tapped(e *fyne.PointEvent) {
	if i := c.slotAt(e.Position.X); i >= 0 && c.onSelect != nil {
		c.onSelect(c.days[i].date)
	}
}

func (c *dayChart) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (c *dayChart) CreateRenderer() fyne.WidgetRenderer {
	r := &dayChartRenderer{chart: c}
	r.build()
	return r
}

type dayChartRenderer struct {
	chart *dayChart

	baseline *canvas.Rectangle
	guide    *canvas.Rectangle // the target line
	guideTag *canvas.Text
	slots    []*chartSlot
	objects  []fyne.CanvasObject
}

// chartSlot is one day's column: the highlight behind it, the stacked bar, and
// the tick under it.
type chartSlot struct {
	highlight *canvas.Rectangle
	orgs      []string // the segments' orgs, bottom-up
	segments  []*canvas.Rectangle
	draft     *canvas.Rectangle // unpushed minutes, capping the stack; nil if none
	label     *canvas.Text
}

// build makes one object per bar segment once, so a resize only moves things
// instead of rebuilding the scene.
func (r *dayChartRenderer) build() {
	c := r.chart
	r.objects = nil

	r.baseline = canvas.NewRectangle(theme.Color(theme.ColorNameInputBorder))
	r.objects = append(r.objects, r.baseline)

	muted := theme.Color(theme.ColorNamePlaceHolder)
	for _, day := range c.days {
		s := &chartSlot{orgs: orgsByShare(c.cfg, day.byOrg)}
		s.highlight = canvas.NewRectangle(color.Transparent)
		s.highlight.CornerRadius = 6
		r.objects = append(r.objects, s.highlight)

		for _, org := range s.orgs {
			seg := canvas.NewRectangle(orgColor(c.cfg, org))
			seg.CornerRadius = chartBarRadius
			s.segments = append(s.segments, seg)
			r.objects = append(r.objects, seg)
		}
		// Built after the org segments so it paints over their overlap rather
		// than under it, and only when there is something to draw: an empty
		// rectangle per day is a scene object per day for nothing.
		if day.draft > 0 {
			s.draft = canvas.NewRectangle(draftYellow(draftSolidTint))
			s.draft.CornerRadius = chartBarRadius
			r.objects = append(r.objects, s.draft)
		}
		s.label = canvas.NewText(chartDayLabel(day.date), muted)
		s.label.TextSize = theme.CaptionTextSize()
		r.objects = append(r.objects, s.label)
		r.slots = append(r.slots, s)
	}

	// The target line is built last so it paints last. It used to go in before
	// the bars, which meant a day at or over the target buried the very line it
	// was being measured against — exactly the days worth checking. It is solid
	// rather than dashed for the same reason: a broken line over a full bar is
	// half a line.
	warn := theme.Color(theme.ColorNameWarning)
	r.guide = canvas.NewRectangle(warn)
	r.objects = append(r.objects, r.guide)
	r.guideTag = canvas.NewText(hoursMins(c.goal)+" target", warn)
	r.guideTag.TextSize = theme.CaptionTextSize()
	r.objects = append(r.objects, r.guideTag)
}

// chartDayLabel is the tick under a bar. Only Mondays are named: thirty-one
// numbers in a row is a smear, and the hover readout carries the exact date.
func chartDayLabel(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil || t.Weekday() != time.Monday {
		return ""
	}
	return t.Format("2 Jan")
}

func (r *dayChartRenderer) Layout(size fyne.Size) {
	c := r.chart
	plotBottom := size.Height - chartAxisHeight
	plotTop := float32(chartTopMargin)
	plotH := plotBottom - plotTop
	if plotH < 1 || len(c.days) == 0 {
		return
	}
	maxVal := float32(c.maxValue())

	r.baseline.Move(fyne.NewPos(0, plotBottom))
	r.baseline.Resize(fyne.NewSize(size.Width, 1))

	guideY := plotBottom - float32(c.goal)/maxVal*plotH
	r.guide.Move(fyne.NewPos(0, guideY))
	r.guide.Resize(fyne.NewSize(size.Width, chartGuideWidth))
	// The tag rides on the right: the left of the plot is where the earliest
	// bars are, and a label sitting over them read as part of the first day.
	tag := r.guideTag.MinSize()
	r.guideTag.Move(fyne.NewPos(size.Width-tag.Width-chartSideMargin, guideY-tag.Height))
	r.guideTag.Resize(tag)

	slotW := (size.Width - chartSideMargin*2) / float32(len(c.days))
	barW := slotW * (1 - chartBarGap)
	if barW < 1 {
		barW = 1
	}
	for i, day := range c.days {
		s := r.slots[i]
		slotX := chartSideMargin + float32(i)*slotW
		barX := slotX + (slotW-barW)/2

		// A day worked and left short of the target keeps its column washed in
		// the target line's own colour, so the days that fell short can be
		// picked out of the month without measuring any bar against the line.
		// A day with nothing logged is not short, it is untouched, and stays
		// unpainted — otherwise a month not yet worked reads as a failed one.
		//
		// The hovered column lights up over whatever it was already showing, so
		// a day with nothing logged still shows what the pointer is on.
		// A day the drafts carry over the line is checked before a short one:
		// both are true of it, and the green is the more useful of the two —
		// nothing is owed on that day but a push.
		short := isShortDay(day.total)
		done := isDraftComplete(day.total, day.draft)
		switch {
		case i == c.hovered && done:
			s.highlight.FillColor = blendColor(draftDoneFill(),
				theme.Color(theme.ColorNameForeground), 0.10)
		case i == c.hovered && short:
			s.highlight.FillColor = blendColor(shortFill(),
				theme.Color(theme.ColorNameForeground), 0.10)
		case i == c.hovered:
			s.highlight.FillColor = blendColor(theme.Color(theme.ColorNameInputBackground),
				theme.Color(theme.ColorNameForeground), 0.10)
		case done:
			s.highlight.FillColor = draftDoneFill()
		case short:
			s.highlight.FillColor = shortFill()
		default:
			s.highlight.FillColor = color.Transparent
		}
		s.highlight.Move(fyne.NewPos(slotX, plotTop))
		s.highlight.Resize(fyne.NewSize(slotW, plotBottom-plotTop))
		s.highlight.Refresh()

		// Segments stack up from the baseline. Each one above the first reaches
		// down behind the one below it, so the rounded corners never notch the
		// joins; only the topmost cap is meant to show.
		y := plotBottom
		for si, seg := range s.segments {
			h := float32(day.byOrg[s.orgs[si]]) / maxVal * plotH
			if h <= 0 {
				seg.Resize(fyne.NewSize(0, 0))
				continue
			}
			top := y - h
			grown := h
			if si > 0 {
				grown += chartBarRadius
			}
			seg.Move(fyne.NewPos(barX, top))
			seg.Resize(fyne.NewSize(barW, grown))
			y = top
		}

		// The draft caps the stack, reaching down behind whatever it landed on
		// for the same reason the org segments do.
		if s.draft != nil {
			h := float32(day.draft) / maxVal * plotH
			if h <= 0 {
				s.draft.Resize(fyne.NewSize(0, 0))
			} else {
				top := y - h
				grown := h
				if len(s.segments) > 0 {
					grown += chartBarRadius
				}
				s.draft.Move(fyne.NewPos(barX, top))
				s.draft.Resize(fyne.NewSize(barW, grown))
			}
		}

		lblSize := s.label.MinSize()
		s.label.Move(fyne.NewPos(slotX+slotW/2-lblSize.Width/2, plotBottom+3))
		s.label.Resize(lblSize)
	}
}

func (r *dayChartRenderer) MinSize() fyne.Size { return r.chart.MinSize() }

func (r *dayChartRenderer) Refresh() {
	r.Layout(r.chart.Size())
	canvas.Refresh(r.chart)
}

func (r *dayChartRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *dayChartRenderer) Destroy() {}

// chartWithReadout pairs the chart with the line that names whatever is under
// the pointer, since a bar cannot label itself without crowding its neighbours.
func (ui *UI) chartWithReadout(days []chartDay, fromDate, toDate string) fyne.CanvasObject {
	chart := newDayChart(ui.cfg, days, target)

	byDate := map[string]chartDay{}
	for _, d := range days {
		byDate[d.date] = d
	}
	idle := fmt.Sprintf("Minutes per day, %s to %s — the line is the %s target. "+
		"Hover a bar for the day, click to open it.", fromDate, toDate, hoursMins(target))
	for _, d := range days {
		// Only said when there is yellow on screen to explain. A permanent note
		// about drafts on a chart with none is furniture.
		if d.draft > 0 {
			idle += " Yellow is saved here and not pushed yet."
			break
		}
	}
	readout := widget.NewLabelWithStyle(idle, fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	chart.onHover = func(date string) {
		if date == "" {
			readout.SetText(idle)
			return
		}
		d := byDate[date]
		when, _ := time.Parse("2006-01-02", date)
		if d.total == 0 && d.draft == 0 {
			readout.SetText(when.Format("Mon 2 Jan") + " — nothing logged")
			return
		}
		if d.total == 0 {
			readout.SetText(fmt.Sprintf("%s — nothing pushed   ·   %s saved here, waiting",
				when.Format("Mon 2 Jan"), hoursMins(d.draft)))
			return
		}
		line := fmt.Sprintf("%s — %s of %s", when.Format("Mon 2 Jan"),
			hoursMins(d.total), hoursMins(target))
		for _, org := range orgsByShare(ui.cfg, d.byOrg) {
			line += fmt.Sprintf("   ·   %s %s", org, hoursMins(d.byOrg[org]))
		}
		if d.draft > 0 {
			line += fmt.Sprintf("   ·   +%s waiting", hoursMins(d.draft))
		}
		readout.SetText(line)
	}
	chart.onSelect = func(date string) { ui.openDayOnCalendar(date) }
	return container.NewVBox(chart, readout)
}

// openDayOnCalendar jumps from a bar in the report to that day on the Status
// tab, loading its month if the report was looking at a different one.
func (ui *UI) openDayOnCalendar(date string) {
	if len(date) < 7 {
		return
	}
	if month := date[:7]; ui.calMonth != month {
		ui.calMonth = month
		ui.loadStatus(false)
	}
	ui.selDay = date
	ui.drawCalendar()
	ui.drawDayPanel()
	if ui.tabs != nil {
		ui.tabs.SelectIndex(statusTabIndex)
	}
}

// statusTabIndex is where the Status tab sits in the tab bar; the report links
// into it by position, which is all AppTabs offers. Log List sits between Log
// work and Status, so this is third.
const statusTabIndex = 2
