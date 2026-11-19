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
	for _, group := range ganttGroups(items) {
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

// ganttGroups orders each milestone's issues for the chart by when they start,
// earliest first, except that a blocker always stands above what it blocks.
// The bars then step forward down the chart instead of jumping back in time.
func ganttGroups(items []Item) []milestoneGroup {
	var out []milestoneGroup
	for _, g := range byMilestone(items) {
		out = append(out, orderByBlockers(g))
	}
	return out
}

// ganttStart is the day an issue's bar starts, as GanttRanges works it out;
// zero when it has no dates.
func ganttStart(it Item) time.Time {
	if !it.SprintStart.IsZero() {
		return it.SprintStart
	}
	return it.Created
}

func orderByBlockers(g milestoneGroup) milestoneGroup {
	n := len(g.Items)
	at := map[string]int{}
	for i, it := range g.Items {
		if it.URL != "" {
			at[strings.ToLower(it.URL)] = i
		}
	}
	// waiting[i] is how many of i's blockers in the group are not yet placed;
	// blocks[j] is the issues j blocks.
	waiting := make([]int, n)
	blocks := make([][]int, n)
	for i, it := range g.Items {
		seen := map[int]bool{}
		for _, b := range it.Blockers {
			j, ok := at[strings.ToLower(b.URL)]
			if !ok || j == i || seen[j] {
				continue
			}
			seen[j] = true
			waiting[i]++
			blocks[j] = append(blocks[j], i)
		}
	}
	// earlier says which of two issues comes first: the earlier start, the
	// undated last, then the list's order.
	earlier := func(a, b int) bool {
		sa, sb := ganttStart(g.Items[a]), ganttStart(g.Items[b])
		if sa.IsZero() != sb.IsZero() {
			return sb.IsZero()
		}
		if !sa.Equal(sb) {
			return sa.Before(sb)
		}
		return a < b
	}
	ordered := milestoneGroup{Name: g.Name, Due: g.Due}
	placed := make([]bool, n)
	for len(ordered.Items) < n {
		// The earliest issue whose blockers are all placed goes next. In a
		// loop of blockers nothing is free, so the earliest left goes anyway.
		pick, free := -1, false
		for i := range g.Items {
			if placed[i] {
				continue
			}
			ready := waiting[i] == 0
			if pick < 0 || (ready && !free) || (ready == free && earlier(i, pick)) {
				pick, free = i, ready
			}
		}
		placed[pick] = true
		ordered.Items = append(ordered.Items, g.Items[pick])
		for _, i := range blocks[pick] {
			waiting[i]--
		}
	}
	return ordered
}

const ganttLabelWidth = float32(280)
const ganttAxisHeight = float32(80)
const ganttRowHeight = float32(34)
const ganttGroupHeight = float32(62)

// ganttBarInset is how far each bar stops short of its first and last day, so
// back-to-back sprints stand apart and a link between them has room to turn.
const ganttBarInset = float32(8)

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
	heading     *fyne.Container
	bar         *canvas.Rectangle
	unscheduled *canvas.Text
	due         *canvas.Line
	dueLabel    *canvas.Text
	badge       fyne.CanvasObject
	when        *canvas.Text
	// band is the faint stripe behind every other issue row.
	band *canvas.Rectangle
}

// ganttEdge is a blocker link. One on the critical path always shows, in
// gold; any other only while the pointer is on one of its two issues.
type ganttEdge struct {
	from, to int
	critical bool
	lines    []*canvas.Line
}

// ganttEdgeSegments is the most lines a link takes: five for the path round
// the bars and two for the arrowhead.
const ganttEdgeSegments = 7

type ganttLayout struct {
	ranges           []GanttRange
	rows             []ganttRow
	edges            []ganttEdge
	first, last, now time.Time
	objects          []fyne.CanvasObject
	grids            []*canvas.Line
	ticks            []*canvas.Text
	today            *canvas.Line
	todayLabel       *canvas.Text
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
	l.readout = caption("Hover an issue for its dates and blocker links; click to open its details.", p.soft)
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
	l.todayLabel = caption("Today "+now.Format("2 Jan 2006"), widgets.LogoGold)
	add(l.todayLabel)
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
	var bands []fyne.CanvasObject
	for _, group := range ganttGroups(items) {
		if group.Name != "" {
			label := boldText(group.Name, widgets.LogoGold)
			label.TextSize = theme.TextSize() + 4
			badge := shell.CountBadge(len(group.Items))
			heading := wrapHeading(label, group.Name)
			// The count keeps its place beside the title; only an overflowing
			// title grows the milestone row, leaving the due date below it.
			heading.Resize(fyne.NewSize(ganttLabelWidth-badge.MinSize().Width-24, heading.MinSize().Height))
			height := max(ganttGroupHeight, heading.MinSize().Height+34)
			r := ganttRow{rangeIndex: -1, group: group, top: y, height: height, label: label, heading: heading, badge: badge}
			add(r.badge)
			if !group.Due.IsZero() {
				r.when = caption("Due "+group.Due.Format("2 Jan 2006"), p.soft)
				add(r.when)
				r.due = canvas.NewLine(widgets.WithAlpha(widgets.LogoGold, .6))
				r.due.StrokeWidth = 2
				add(r.due)
				r.dueLabel = caption("Due "+group.Due.Format("2 Jan 2006"), widgets.LogoGold)
				add(r.dueLabel)
			}
			l.rows = append(l.rows, r)
			y += height
		}
		for n, it := range group.Items {
			label := caption(fmt.Sprintf("#%d  %s", it.Number, it.Title), p.ink)
			bar := canvas.NewRectangle(p.stateColor(it))
			bar.CornerRadius = 4
			missing := caption("Unscheduled · no dates", p.soft)
			row := ganttRow{rangeIndex: index, top: y, height: ganttRowHeight, label: label, bar: bar, unscheduled: missing}
			if n%2 == 1 {
				row.band = canvas.NewRectangle(widgets.WithAlpha(p.ink, .045))
				row.band.CornerRadius = 4
				bands = append(bands, row.band)
			}
			l.rows = append(l.rows, row)
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
			l.edges = append(l.edges, ganttEdge{from: from, to: to})
		}
	}
	// The critical path's issues are gold bars with a green outline and a
	// gold name; its links stay drawn, in green. Every other bar keeps its
	// state's colour.
	onPath, pathEdges := criticalPath(ranges, l.edges)
	for i := range l.edges {
		e := &l.edges[i]
		e.critical = pathEdges[i]
		ink := p.blocked
		switch {
		case e.critical:
			ink = p.done
		case blockerClosed(ranges[e.to].Item, ranges[e.from].Item):
			ink = p.soft
		}
		for j := 0; j < ganttEdgeSegments; j++ {
			line := canvas.NewLine(ink)
			line.StrokeWidth = 2
			line.Hide()
			e.lines = append(e.lines, line)
			add(line)
		}
	}
	for _, row := range l.rows {
		if row.rangeIndex >= 0 && onPath[row.rangeIndex] {
			row.bar.FillColor = widgets.LogoGold
			row.bar.StrokeColor = p.done
			row.bar.StrokeWidth = 2
			row.label.Color = widgets.LogoGold
			row.label.TextStyle = fyne.TextStyle{Bold: true}
		}
	}
	if len(onPath) > 0 {
		l.readout.Text += " Gold with green outline and links: the critical path."
		l.detail = l.readout.Text
	}
	for _, row := range l.rows {
		if row.bar != nil {
			add(row.bar)
			add(row.unscheduled)
		}
		if row.heading != nil {
			add(row.heading)
		} else {
			add(row.label)
		}
	}
	// The stripes lie under everything, the hover pill included.
	l.objects = append(bands, l.objects...)
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
		l.todayLabel.Show()
		l.today.Position1, l.today.Position2 = fyne.NewPos(x(l.now), ganttAxisHeight-10), fyne.NewPos(x(l.now), l.height)
		l.today.Refresh()
		l.todayLabel.Resize(l.todayLabel.MinSize())
		l.todayLabel.Move(fyne.NewPos(max(ganttLabelWidth, min(x(l.now)+6, size.Width-l.todayLabel.MinSize().Width-4)), 26))
	} else {
		l.today.Hide()
		l.todayLabel.Hide()
	}
	centres := map[int]float32{}
	// lanes[r] is where a link runs above and below range r's row: the row's
	// edges, clear of its bar.
	type lane struct{ above, below float32 }
	lanes := map[int]lane{}
	for _, row := range l.rows {
		if row.rangeIndex >= 0 {
			lanes[row.rangeIndex] = lane{above: row.top, below: row.top + row.height}
		}
	}
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
		if row.rangeIndex < 0 {
			row.heading.Move(fyne.NewPos(4, row.top+4))
			row.heading.Resize(fyne.NewSize(ganttLabelWidth-row.badge.MinSize().Width-24, row.heading.MinSize().Height))
			row.badge.Move(fyne.NewPos(ganttLabelWidth-row.badge.MinSize().Width-8, row.top+4))
			row.badge.Resize(row.badge.MinSize())
			if row.when != nil {
				row.when.Move(fyne.NewPos(4, row.top+row.height-28))
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
				row.dueLabel.Resize(row.dueLabel.MinSize())
				row.dueLabel.Move(fyne.NewPos(max(ganttLabelWidth, min(x(row.group.Due)+6, size.Width-row.dueLabel.MinSize().Width-4)), row.top+4))
			}
			continue
		}
		label.Text = shell.FitText(full, label.TextSize, label.TextStyle, ganttLabelWidth-16)
		label.Move(fyne.NewPos(4, row.top+(row.height-label.MinSize().Height)/2))
		label.Resize(label.MinSize())
		label.Refresh()
		if row.band != nil {
			row.band.Move(fyne.NewPos(0, row.top))
			row.band.Resize(fyne.NewSize(size.Width, row.height))
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
			left, right := barSpan(r, x)
			row.bar.Move(fyne.NewPos(left, row.top+9))
			row.bar.Resize(fyne.NewSize(max(right-left, 4), row.height-18))
		}
	}
	hovered := -1
	if l.hover >= 0 {
		hovered = l.rows[l.hover].rangeIndex
	}
	for _, e := range l.edges {
		if !e.critical && (hovered < 0 || (e.from != hovered && e.to != hovered)) {
			for _, line := range e.lines {
				line.Hide()
			}
			continue
		}
		from, to := l.ranges[e.from], l.ranges[e.to]
		_, end := barSpan(from, x)
		start, _ := barSpan(to, x)
		a, b := fyne.NewPos(end, centres[e.from]), fyne.NewPos(start, centres[e.to])
		run := lanes[e.to].above
		if b.Y < a.Y {
			run = lanes[e.to].below
		}
		path := ganttLinkPath(a, b, run, size.Width-4)
		segments := make([][2]fyne.Position, 0, ganttEdgeSegments)
		for i := 1; i < len(path); i++ {
			segments = append(segments, [2]fyne.Position{path[i-1], path[i]})
		}
		segments = append(segments, [2]fyne.Position{b, b.Add(fyne.NewPos(-5, -4))}, [2]fyne.Position{b, b.Add(fyne.NewPos(-5, 4))})
		for i, line := range e.lines {
			if i >= len(segments) {
				line.Hide()
				continue
			}
			line.Position1, line.Position2 = segments[i][0], segments[i][1]
			line.Show()
			line.Refresh()
		}
	}
	if l.hover >= 0 {
		row := l.rows[l.hover]
		l.pill.Fit(fyne.NewPos(0, row.top), fyne.NewSize(size.Width, row.height))
	}
}

// ganttLinkPath runs a blocker link from the end of the blocker's bar (a) to
// the start of the blocked one (b), always arriving from the left. When b
// starts before a ends, the link drops to the gap beside b's row (at run) and
// runs back along it, rather than doubling back across the bars.
func ganttLinkPath(a, b fyne.Position, run, right float32) []fyne.Position {
	if b.X-a.X >= 10 {
		bend := (a.X + b.X) / 2
		return []fyne.Position{a, fyne.NewPos(bend, a.Y), fyne.NewPos(bend, b.Y), b}
	}
	out, in := min(a.X+8, right), b.X-8
	return []fyne.Position{a, fyne.NewPos(out, a.Y), fyne.NewPos(out, run), fyne.NewPos(in, run), fyne.NewPos(in, b.Y), b}
}

// barSpan is where a range's bar starts and ends across the chart: its days,
// less ganttBarInset at each end (less on a bar too short to spare it).
func barSpan(r GanttRange, x func(time.Time) float32) (left, right float32) {
	left, right = x(r.Start), x(r.End.AddDate(0, 0, 1))
	inset := min(ganttBarInset, max((right-left-4)/2, 0))
	return left + inset, right - inset
}

// blockerClosed reports whether blocked lists blocker as a closed blocker.
func blockerClosed(blocked, blocker Item) bool {
	for _, b := range blocked.Blockers {
		if strings.EqualFold(b.URL, blocker.URL) {
			return b.Closed
		}
	}
	return false
}

// criticalPath is the chain of blockers that sets the finish: it ends at the
// blocked issue that finishes last and runs back, link by link, through the
// blocker that finishes latest, since that is the one each step waits on. It
// needs at least one link. A loop of blockers is cut where it closes.
func criticalPath(ranges []GanttRange, edges []ganttEdge) (onPath, pathEdges map[int]bool) {
	onPath, pathEdges = map[int]bool{}, map[int]bool{}
	into := map[int][]int{} // range: the edges arriving at it
	for i, e := range edges {
		into[e.to] = append(into[e.to], i)
	}
	end := -1
	for i := range ranges {
		if len(into[i]) > 0 && (end < 0 || ranges[i].End.After(ranges[end].End)) {
			end = i
		}
	}
	for r := end; r >= 0; {
		onPath[r] = true
		by := -1
		for _, ei := range into[r] {
			from := edges[ei].from
			if onPath[from] {
				continue
			}
			if by < 0 || ranges[from].End.After(ranges[edges[by].from].End) {
				by = ei
			}
		}
		if by < 0 {
			break
		}
		pathEdges[by] = true
		r = edges[by].from
	}
	return onPath, pathEdges
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
		c.Content.Refresh()
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
	c.Content.Refresh()
}
func (c *GanttChart) Cursor() desktop.Cursor { return desktop.PointerCursor }
func (c *GanttChart) Tapped(e *fyne.PointEvent) {
	if i := c.rowAt(e.Position.Y); i >= 0 && c.onSelect != nil {
		// The row clicked ripples across its gold hover.
		row := c.layout.rows[i]
		shell.RippleIn(c, fyne.NewPos(0, row.top), fyne.NewSize(c.Size().Width, row.height), e.AbsolutePosition)
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
