package report

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

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// DayChart is the report's bar graph, drawn as shapes rather than as a picture.
//
// It used to be a raster of fixed pixel size stretched to the window, which is
// why it looked squashed: the drawing was made once at one size and then pulled
// to another. Here every bar is a real canvas object placed from the width and
// height the widget is actually given, so nothing is scaled and nothing is
// distorted — and the bars can carry rounded caps, org colours and a hover
// highlight without any of it being painted into pixels first.
type DayChart struct {
	widget.BaseWidget
	cfg      backend.Config
	Days     []ChartDay
	goal     int // the daily target, drawn as a guide line
	Hovered  int // index of the bar under the pointer, -1 for none
	OnHover  func(date string)
	OnSelect func(date string)
}

// ChartDay is one slot on the axis: a date and how its minutes split by org.
// draft is what is saved on this machine for that day and not yet pushed — it
// is stacked on top of the org segments rather than counted into total, which
// stays what GitHub actually holds.
type ChartDay struct {
	Date  string
	ByOrg map[string]int
	Total int
	Draft int
}

func NewDayChart(cfg backend.Config, days []ChartDay, goal int) *DayChart {
	c := &DayChart{cfg: cfg, Days: days, goal: goal, Hovered: -1}
	c.ExtendBaseWidget(c)
	return c
}

// Chart geometry. The plot keeps a strip at the bottom for date labels and a
// margin at the top so a record day does not touch the ceiling.
const (
	chartMinHeight  = 240
	ChartAxisHeight = 20
	ChartTopMargin  = 10
	ChartSideMargin = 6
	chartBarGap     = 0.28 // share of a slot left as space between bars
	chartGuideWidth = 2    // the target line, thick enough to read over a bar
	ChartBarRadius  = 4
)

func (c *DayChart) MinSize() fyne.Size { return fyne.NewSize(0, chartMinHeight) }

// MaxValue is the top of the scale: the busiest day, never less than the target
// so the guide line always has somewhere to sit.
func (c *DayChart) MaxValue() int {
	maxVal := c.goal
	for _, d := range c.Days {
		// The draft rides on top of the bar, so the scale has to hold both or a
		// day with work waiting would draw off the top of the plot.
		if t := d.Total + d.Draft; t > maxVal {
			maxVal = t
		}
	}
	if maxVal <= 0 {
		maxVal = 1
	}
	return maxVal
}

// SlotAt maps a pointer position to a bar index, or -1 outside the plot.
func (c *DayChart) SlotAt(x float32) int {
	w := c.Size().Width - ChartSideMargin*2
	// The margins are outside the plot. Checked before the arithmetic below,
	// which truncates towards zero and would round the left margin into the
	// first bar.
	if w <= 0 || len(c.Days) == 0 || x < ChartSideMargin || x >= ChartSideMargin+w {
		return -1
	}
	i := int((x - ChartSideMargin) / w * float32(len(c.Days)))
	if i < 0 || i >= len(c.Days) {
		return -1
	}
	return i
}

func (c *DayChart) MouseIn(e *desktop.MouseEvent) { c.MouseMoved(e) }

func (c *DayChart) MouseMoved(e *desktop.MouseEvent) {
	i := c.SlotAt(e.Position.X)
	if i == c.Hovered {
		return
	}
	c.Hovered = i
	if c.OnHover != nil {
		if i < 0 {
			c.OnHover("")
		} else {
			c.OnHover(c.Days[i].Date)
		}
	}
	c.Refresh()
}

func (c *DayChart) MouseOut() {
	c.Hovered = -1
	if c.OnHover != nil {
		c.OnHover("")
	}
	c.Refresh()
}

func (c *DayChart) Tapped(e *fyne.PointEvent) {
	if i := c.SlotAt(e.Position.X); i >= 0 && c.OnSelect != nil {
		c.OnSelect(c.Days[i].Date)
	}
}

func (c *DayChart) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (c *DayChart) CreateRenderer() fyne.WidgetRenderer {
	r := &DayChartRenderer{chart: c}
	r.build()
	return r
}

type DayChartRenderer struct {
	chart *DayChart

	baseline *canvas.Rectangle
	Guide    *canvas.Rectangle // the target line
	guideTag *canvas.Text
	Slots    []*chartSlot
	objects  []fyne.CanvasObject
}

// chartSlot is one day's column: the highlight behind it, the stacked bar, and
// the tick under it.
type chartSlot struct {
	Highlight *canvas.Rectangle
	orgs      []string // the segments' orgs, bottom-up
	Segments  []*canvas.Rectangle
	Draft     *canvas.Rectangle // unpushed minutes, capping the stack; nil if none
	label     *canvas.Text
}

// build makes one object per bar segment once, so a resize only moves things
// instead of rebuilding the scene.
func (r *DayChartRenderer) build() {
	c := r.chart
	r.objects = nil

	r.baseline = canvas.NewRectangle(theme.Color(theme.ColorNameInputBorder))
	r.objects = append(r.objects, r.baseline)

	muted := theme.Color(theme.ColorNamePlaceHolder)
	for _, day := range c.Days {
		s := &chartSlot{orgs: shell.OrgsByShare(c.cfg, day.ByOrg)}
		s.Highlight = canvas.NewRectangle(color.Transparent)
		s.Highlight.CornerRadius = 6
		r.objects = append(r.objects, s.Highlight)

		for _, org := range s.orgs {
			seg := canvas.NewRectangle(shell.OrgColor(c.cfg, org))
			seg.CornerRadius = ChartBarRadius
			s.Segments = append(s.Segments, seg)
			r.objects = append(r.objects, seg)
		}
		// Built after the org segments so it paints over their overlap rather
		// than under it, and only when there is something to draw: an empty
		// rectangle per day is a scene object per day for nothing.
		if day.Draft > 0 {
			s.Draft = canvas.NewRectangle(shell.DraftYellow(shell.DraftSolidTint))
			s.Draft.CornerRadius = ChartBarRadius
			r.objects = append(r.objects, s.Draft)
		}
		s.label = canvas.NewText(chartDayLabel(day.Date), muted)
		s.label.TextSize = theme.CaptionTextSize()
		r.objects = append(r.objects, s.label)
		r.Slots = append(r.Slots, s)
	}

	// The target line is built last so it paints last. It used to go in before
	// the bars, which meant a day at or over the target buried the very line it
	// was being measured against — exactly the days worth checking. It is solid
	// rather than dashed for the same reason: a broken line over a full bar is
	// half a line.
	warn := theme.Color(theme.ColorNameWarning)
	r.Guide = canvas.NewRectangle(warn)
	r.objects = append(r.objects, r.Guide)
	r.guideTag = canvas.NewText(backend.HoursMins(c.goal)+" target", warn)
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

func (r *DayChartRenderer) Layout(size fyne.Size) {
	c := r.chart
	plotBottom := size.Height - ChartAxisHeight
	plotTop := float32(ChartTopMargin)
	plotH := plotBottom - plotTop
	if plotH < 1 || len(c.Days) == 0 {
		return
	}
	maxVal := float32(c.MaxValue())

	r.baseline.Move(fyne.NewPos(0, plotBottom))
	r.baseline.Resize(fyne.NewSize(size.Width, 1))

	guideY := plotBottom - float32(c.goal)/maxVal*plotH
	r.Guide.Move(fyne.NewPos(0, guideY))
	r.Guide.Resize(fyne.NewSize(size.Width, chartGuideWidth))
	// The tag rides on the right: the left of the plot is where the earliest
	// bars are, and a label sitting over them read as part of the first day.
	tag := r.guideTag.MinSize()
	r.guideTag.Move(fyne.NewPos(size.Width-tag.Width-ChartSideMargin, guideY-tag.Height))
	r.guideTag.Resize(tag)

	slotW := (size.Width - ChartSideMargin*2) / float32(len(c.Days))
	barW := slotW * (1 - chartBarGap)
	if barW < 1 {
		barW = 1
	}
	for i, day := range c.Days {
		s := r.Slots[i]
		slotX := ChartSideMargin + float32(i)*slotW
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
		short := shell.IsShortDay(day.Total)
		done := shell.IsDraftComplete(day.Total, day.Draft)
		switch {
		case i == c.Hovered && done:
			s.Highlight.FillColor = shell.BlendColor(shell.DraftDoneFill(),
				theme.Color(theme.ColorNameForeground), 0.10)
		case i == c.Hovered && short:
			s.Highlight.FillColor = shell.BlendColor(shell.ShortFill(),
				theme.Color(theme.ColorNameForeground), 0.10)
		case i == c.Hovered:
			s.Highlight.FillColor = shell.BlendColor(theme.Color(theme.ColorNameInputBackground),
				theme.Color(theme.ColorNameForeground), 0.10)
		case done:
			s.Highlight.FillColor = shell.DraftDoneFill()
		case short:
			s.Highlight.FillColor = shell.ShortFill()
		default:
			s.Highlight.FillColor = color.Transparent
		}
		s.Highlight.Move(fyne.NewPos(slotX, plotTop))
		s.Highlight.Resize(fyne.NewSize(slotW, plotBottom-plotTop))
		s.Highlight.Refresh()

		// Segments stack up from the baseline. Each one above the first reaches
		// down behind the one below it, so the rounded corners never notch the
		// joins; only the topmost cap is meant to show.
		y := plotBottom
		for si, seg := range s.Segments {
			h := float32(day.ByOrg[s.orgs[si]]) / maxVal * plotH
			if h <= 0 {
				seg.Resize(fyne.NewSize(0, 0))
				continue
			}
			top := y - h
			grown := h
			if si > 0 {
				grown += ChartBarRadius
			}
			seg.Move(fyne.NewPos(barX, top))
			seg.Resize(fyne.NewSize(barW, grown))
			y = top
		}

		// The draft caps the stack, reaching down behind whatever it landed on
		// for the same reason the org segments do.
		if s.Draft != nil {
			h := float32(day.Draft) / maxVal * plotH
			if h <= 0 {
				s.Draft.Resize(fyne.NewSize(0, 0))
			} else {
				top := y - h
				grown := h
				if len(s.Segments) > 0 {
					grown += ChartBarRadius
				}
				s.Draft.Move(fyne.NewPos(barX, top))
				s.Draft.Resize(fyne.NewSize(barW, grown))
			}
		}

		lblSize := s.label.MinSize()
		s.label.Move(fyne.NewPos(slotX+slotW/2-lblSize.Width/2, plotBottom+3))
		s.label.Resize(lblSize)
	}
}

func (r *DayChartRenderer) MinSize() fyne.Size { return r.chart.MinSize() }

func (r *DayChartRenderer) Refresh() {
	r.Layout(r.chart.Size())
	canvas.Refresh(r.chart)
}

func (r *DayChartRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *DayChartRenderer) Destroy() {}

// ChartWithReadout pairs the chart with the line that names whatever is under
// the pointer, since a bar cannot label itself without crowding its neighbours.
func ChartWithReadout(ui *shell.UI, days []ChartDay, fromDate, toDate string) fyne.CanvasObject {
	chart := NewDayChart(ui.Cfg, days, backend.Target)

	byDate := map[string]ChartDay{}
	for _, d := range days {
		byDate[d.Date] = d
	}
	idle := fmt.Sprintf("Minutes per day, %s to %s — the line is the %s target. "+
		"Hover a bar for the day, click to open it.", fromDate, toDate, backend.HoursMins(backend.Target))
	for _, d := range days {
		// Only said when there is yellow on screen to explain. A permanent note
		// about drafts on a chart with none is furniture.
		if d.Draft > 0 {
			idle += " Yellow is saved here and not pushed yet."
			break
		}
	}
	readout := widget.NewLabelWithStyle(idle, fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	readout.Wrapping = fyne.TextWrapWord
	chart.OnHover = func(date string) {
		if date == "" {
			readout.SetText(idle)
			return
		}
		d := byDate[date]
		when, _ := time.Parse("2006-01-02", date)
		if d.Total == 0 && d.Draft == 0 {
			readout.SetText(when.Format("Mon 2 Jan") + " — nothing logged")
			return
		}
		if d.Total == 0 {
			readout.SetText(fmt.Sprintf("%s — nothing pushed   ·   %s saved here, waiting",
				when.Format("Mon 2 Jan"), backend.HoursMins(d.Draft)))
			return
		}
		line := fmt.Sprintf("%s — %s of %s", when.Format("Mon 2 Jan"),
			backend.HoursMins(d.Total), backend.HoursMins(backend.Target))
		for _, org := range shell.OrgsByShare(ui.Cfg, d.ByOrg) {
			line += fmt.Sprintf("   ·   %s %s", org, backend.HoursMins(d.ByOrg[org]))
		}
		if d.Draft > 0 {
			line += fmt.Sprintf("   ·   +%s waiting", backend.HoursMins(d.Draft))
		}
		readout.SetText(line)
	}
	chart.OnSelect = func(date string) { OpenDayOnCalendar(ui, date) }
	return container.NewVBox(chart, readout)
}

// OpenDayOnCalendar jumps from a bar in the report to that day on the Status
// tab, loading its month if the report was looking at a different one.
func OpenDayOnCalendar(ui *shell.UI, date string) {
	if len(date) < 7 {
		return
	}
	if month := date[:7]; ui.CalMonth != month {
		ui.CalMonth = month
		ui.LoadStatus(false)
	}
	ui.SelDay = date
	shell.Hooks.DrawCalendar(ui)
	shell.Hooks.DrawDayPanel(ui)
	if ui.Tabs != nil {
		ui.Tabs.SelectIndex(shell.StatusTabIndex)
	}
}
