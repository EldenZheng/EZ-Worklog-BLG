package projects

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// GanttRange preserves the source of a bar's dates. A milestone due date alone
// is never treated as a task schedule, and blockers never invent task dates.
type GanttRange struct {
	Item       Item
	Start, End time.Time
	Scheduled  bool
}

func GanttRanges(items []Item, now time.Time) []GanttRange {
	var out []GanttRange
	for _, group := range byMilestone(items) {
		for _, it := range group.Items {
			r := GanttRange{Item: it}
			switch {
			case !it.SprintStart.IsZero():
				r.Start, r.End = it.SprintStart, it.SprintStart.AddDate(0, 0, max(it.SprintDays-1, 0))
				r.Scheduled = true
			case !it.Created.IsZero():
				r.Start, r.End = it.Created, now
				if it.Closed {
					r.End = it.Created
					if !it.ClosedAt.IsZero() {
						r.End = it.ClosedAt
					}
				}
			}
			if !r.Start.IsZero() && r.End.Before(r.Start) {
				r.End = r.Start
			}
			out = append(out, r)
		}
	}
	return out
}

const ganttLabelWidth = float32(280)
const ganttAxisHeight = float32(56)
const ganttRowHeight = float32(34)
const ganttGroupHeight = float32(62)

// GanttChart draws a milestone-grouped timeline from the same filtered issues
// as the list. Select a bar or its name to open the existing issue details.
type GanttChart struct {
	widget.BaseWidget
	Content  *fyne.Container
	Ranges   []GanttRange
	layout   *ganttLayout
	onSelect func(Item)
}

type ganttRow struct {
	rangeIndex  int // -1 for a milestone heading
	group       milestoneGroup
	top, height float32
	label       *canvas.Text
	bar         *canvas.Rectangle
	unscheduled *canvas.Text
	due         *canvas.Line
	badge       fyne.CanvasObject
	when        *canvas.Text
}

type ganttEdge struct {
	from, to int
	lines    []*canvas.Line
}
type ganttLayout struct {
	ranges           []GanttRange
	rows             []ganttRow
	edges            []ganttEdge
	first, last, now time.Time
	objects          []fyne.CanvasObject
	grids            []*canvas.Line
	ticks            []*canvas.Text
	today            *canvas.Line
	readout          *canvas.Text
	detail           string
	pill             *widgets.HoverPill
	hover            int
	height           float32
}

func NewGanttChart(items []Item, now time.Time, p palette, onSelect func(Item)) *GanttChart {
	ranges := GanttRanges(items, now)
	l := &ganttLayout{ranges: ranges, now: now, pill: widgets.NewHoverPill(), hover: -1}
	add := func(o fyne.CanvasObject) { l.objects = append(l.objects, o) }
	add(l.pill.Rect)
	l.readout = caption("Hover an issue for dates and blockers; click to open its details.", p.soft)
	l.detail = l.readout.Text
	add(l.readout)
	for i := 0; i < 6; i++ {
		grid := canvas.NewLine(p.grid)
		grid.StrokeWidth = 1
		l.grids = append(l.grids, grid)
		add(grid)
		tick := caption("", p.soft)
		l.ticks = append(l.ticks, tick)
		add(tick)
	}
	l.today = canvas.NewLine(widgets.WithAlpha(widgets.LogoGold, .6))
	l.today.StrokeWidth = 1.5
	add(l.today)
	include := func(t time.Time) {
		if t.IsZero() {
			return
		}
		if l.first.IsZero() || t.Before(l.first) {
			l.first = t
		}
		if l.last.IsZero() || t.After(l.last) {
			l.last = t
		}
	}
	for _, r := range ranges {
		include(r.Start)
		include(r.End)
		include(r.Item.MilestoneDue)
	}
	if l.first.IsZero() {
		l.first, l.last = now, now
	}
	l.first = dateOnly(l.first).AddDate(0, 0, -1)
	l.last = dateOnly(l.last).AddDate(0, 0, 1)
	y, index := ganttAxisHeight, 0
	for _, group := range byMilestone(items) {
		if group.Name != "" {
			label := boldText(group.Name, widgets.LogoGold)
			label.TextSize = theme.TextSize() + 4
			r := ganttRow{rangeIndex: -1, group: group, top: y, height: ganttGroupHeight, label: label}
			r.badge = shell.CountBadge(len(group.Items))
			add(r.badge)
			if !group.Due.IsZero() {
				r.when = caption("Due "+group.Due.Format("2 Jan 2006"), p.soft)
				add(r.when)
				r.due = canvas.NewLine(widgets.WithAlpha(widgets.LogoGold, .6))
				r.due.StrokeWidth = 2
				add(r.due)
			}
			l.rows = append(l.rows, r)
			y += ganttGroupHeight
		}
		for _, it := range group.Items {
			label := caption(fmt.Sprintf("#%d  %s", it.Number, it.Title), p.ink)
			bar := canvas.NewRectangle(p.stateColor(it))
			bar.CornerRadius = 4
			missing := caption("Unscheduled · no dates", p.soft)
			l.rows = append(l.rows, ganttRow{rangeIndex: index, top: y, height: ganttRowHeight, label: label, bar: bar, unscheduled: missing})
			index++
			y += ganttRowHeight
		}
	}
	l.height = y + 8
	// Connect only relationships GitHub returned and whose endpoints are in
	// this filtered view. Blockers outside it remain named in the readout.
	byURL := map[string]int{}
	for i, r := range ranges {
		byURL[strings.ToLower(r.Item.URL)] = i
	}
	for to, r := range ranges {
		for _, blocker := range r.Item.Blockers {
			from, ok := byURL[strings.ToLower(blocker.URL)]
			if !ok || from == to || ranges[from].Start.IsZero() || r.Start.IsZero() {
				continue
			}
			e := ganttEdge{from: from, to: to}
			ink := p.blocked
			if blocker.Closed {
				ink = p.grid
			}
			for i := 0; i < 5; i++ {
				line := canvas.NewLine(ink)
				line.StrokeWidth = 1.3
				e.lines = append(e.lines, line)
				add(line)
			}
			l.edges = append(l.edges, e)
		}
	}
	for _, row := range l.rows {
		if row.bar != nil {
			add(row.bar)
			add(row.unscheduled)
		}
		add(row.label)
	}
	c := &GanttChart{Content: container.New(l, l.objects...), Ranges: ranges, layout: l, onSelect: onSelect}
	c.ExtendBaseWidget(c)
	return c
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
func dayDistance(a, b time.Time) float32                     { return float32(dateOnly(b).Sub(dateOnly(a)).Hours() / 24) }
func (l *ganttLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(960, l.height) }
func (l *ganttLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	width := max(size.Width-ganttLabelWidth-12, 1)
	x := func(t time.Time) float32 {
		return ganttLabelWidth + width*dayDistance(l.first, t)/max(dayDistance(l.first, l.last), 1)
	}
	for i, grid := range l.grids {
		xx := ganttLabelWidth + width*float32(i)/5
		grid.Position1, grid.Position2 = fyne.NewPos(xx, ganttAxisHeight-10), fyne.NewPos(xx, l.height)
		grid.Refresh()
		text := l.ticks[i]
		date := l.first.AddDate(0, 0, int(dayDistance(l.first, l.last)*float32(i)/5))
		text.Text = date.Format("2 Jan 06")
		text.Refresh()
		m := text.MinSize()
		text.Move(fyne.NewPos(min(xx, size.Width-m.Width), ganttAxisHeight-m.Height-12))
		text.Resize(m)
	}
	l.readout.Text = shell.FitText(l.detail, l.readout.TextSize, l.readout.TextStyle, size.Width-8)
	l.readout.Move(fyne.NewPos(4, 0))
	l.readout.Resize(l.readout.MinSize())
	l.readout.Refresh()
	if !l.now.Before(l.first) && !l.now.After(l.last) {
		l.today.Show()
		l.today.Position1, l.today.Position2 = fyne.NewPos(x(l.now), ganttAxisHeight-10), fyne.NewPos(x(l.now), l.height)
		l.today.Refresh()
	} else {
		l.today.Hide()
	}
	centres := map[int]float32{}
	for i, row := range l.rows {
		label := row.label
		full := row.group.Name
		if row.rangeIndex >= 0 {
			it := l.ranges[row.rangeIndex].Item
			full = fmt.Sprintf("#%d  %s", it.Number, it.Title)
			if it.BlockerCount > 0 || len(it.Blockers) > 0 {
				full += fmt.Sprintf(" · %d blockers", max(it.BlockerCount, len(it.Blockers)))
			}
		}
		label.Text = shell.FitText(full, label.TextSize, label.TextStyle, ganttLabelWidth-16)
		label.Move(fyne.NewPos(4, row.top+(row.height-label.MinSize().Height)/2))
		label.Resize(label.MinSize())
		label.Refresh()
		if row.rangeIndex < 0 {
			label.Text = shell.FitText(full, label.TextSize, label.TextStyle, ganttLabelWidth-row.badge.MinSize().Width-24)
			label.Refresh()
			label.Move(fyne.NewPos(4, row.top+4))
			label.Resize(label.MinSize())
			row.badge.Move(fyne.NewPos(ganttLabelWidth-row.badge.MinSize().Width-8, row.top+4))
			row.badge.Resize(row.badge.MinSize())
			if row.when != nil {
				row.when.Move(fyne.NewPos(4, row.top+34))
				row.when.Resize(row.when.MinSize())
			}
			if row.due != nil {
				end := l.height
				for _, next := range l.rows[i+1:] {
					if next.rangeIndex < 0 {
						end = next.top
						break
					}
				}
				row.due.Position1, row.due.Position2 = fyne.NewPos(x(row.group.Due), row.top), fyne.NewPos(x(row.group.Due), end)
				row.due.Refresh()
			}
			continue
		}
		r := l.ranges[row.rangeIndex]
		centres[row.rangeIndex] = row.top + row.height/2
		if r.Start.IsZero() {
			row.bar.Hide()
			row.unscheduled.Show()
			row.unscheduled.Move(fyne.NewPos(ganttLabelWidth+8, row.top+8))
			row.unscheduled.Resize(row.unscheduled.MinSize())
		} else {
			row.unscheduled.Hide()
			row.bar.Show()
			row.bar.Move(fyne.NewPos(x(r.Start), row.top+9))
			row.bar.Resize(fyne.NewSize(max(x(r.End.AddDate(0, 0, 1))-x(r.Start), 4), row.height-18))
		}
	}
	for _, e := range l.edges {
		from, to := l.ranges[e.from], l.ranges[e.to]
		a, b := fyne.NewPos(x(from.End.AddDate(0, 0, 1)), centres[e.from]), fyne.NewPos(x(to.Start), centres[e.to])
		bend := min(max(a.X+8, (a.X+b.X)/2), size.Width-4)
		arrow := float32(5)
		if bend < b.X {
			arrow = -5
		}
		points := [][2]fyne.Position{{a, fyne.NewPos(bend, a.Y)}, {fyne.NewPos(bend, a.Y), fyne.NewPos(bend, b.Y)}, {fyne.NewPos(bend, b.Y), b}, {b, b.Add(fyne.NewPos(arrow, -4))}, {b, b.Add(fyne.NewPos(arrow, 4))}}
		for i, line := range e.lines {
			line.Position1, line.Position2 = points[i][0], points[i][1]
			line.Refresh()
		}
	}
	if l.hover >= 0 {
		row := l.rows[l.hover]
		l.pill.Fit(fyne.NewPos(0, row.top), fyne.NewSize(size.Width, row.height))
	}
}

func (c *GanttChart) rowAt(y float32) int {
	for i, row := range c.layout.rows {
		if row.rangeIndex >= 0 && y >= row.top && y < row.top+row.height {
			return i
		}
	}
	return -1
}
func (c *GanttChart) MouseIn(e *desktop.MouseEvent) { c.MouseMoved(e) }
func (c *GanttChart) MouseMoved(e *desktop.MouseEvent) {
	l := c.layout
	i := c.rowAt(e.Position.Y)
	if i == l.hover {
		return
	}
	l.hover = i
	if i < 0 {
		l.pill.MoveTo(fyne.Position{}, fyne.Size{}, false)
		return
	}
	row := l.rows[i]
	r := l.ranges[row.rangeIndex]
	dates := "Unscheduled · no dates"
	if !r.Start.IsZero() {
		source := "opened → closed/today"
		if r.Scheduled {
			source = "sprint schedule"
		}
		dates = r.Start.Format("2 Jan 2006") + " – " + r.End.Format("2 Jan 2006") + " (" + source + ")"
	}
	text := fmt.Sprintf("#%d · %s", r.Item.Number, dates)
	if !r.Item.MilestoneDue.IsZero() {
		text += " · milestone due " + r.Item.MilestoneDue.Format("2 Jan 2006")
	}
	for _, b := range r.Item.Blockers {
		state := "open"
		if b.Closed {
			state = "closed"
		}
		text += fmt.Sprintf(" · blocked by #%d (%s)", b.Number, state)
	}
	if r.Item.BlockerCount > len(r.Item.Blockers) {
		text += " · more blockers on GitHub"
	}
	l.detail = text
	l.pill.MoveTo(fyne.NewPos(0, row.top), fyne.NewSize(c.Size().Width, row.height), true)
	c.Content.Refresh()
}
func (c *GanttChart) MouseOut() {
	c.layout.hover = -1
	c.layout.pill.MoveTo(fyne.Position{}, fyne.Size{}, false)
}
func (c *GanttChart) Cursor() desktop.Cursor { return desktop.PointerCursor }
func (c *GanttChart) Tapped(e *fyne.PointEvent) {
	if i := c.rowAt(e.Position.Y); i >= 0 && c.onSelect != nil {
		c.onSelect(c.Ranges[c.layout.rows[i].rangeIndex].Item)
	}
}
func (c *GanttChart) CreateRenderer() fyne.WidgetRenderer {
	return &ganttRenderer{WidgetRenderer: widget.NewSimpleRenderer(c.Content), pill: c.layout.pill}
}

type ganttRenderer struct {
	fyne.WidgetRenderer
	pill *widgets.HoverPill
}

func (r *ganttRenderer) Destroy() { r.pill.Stop(); r.WidgetRenderer.Destroy() }
