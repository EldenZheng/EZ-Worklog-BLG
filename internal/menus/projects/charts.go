package projects

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// The charts are drawn as shapes placed from the size they are given, like the
// Report's bar graph, so nothing is scaled or squashed, and each one fills
// whatever room the page hands it.
//
// Their colours are one fixed set per theme, each checked against that theme's
// background: neighbouring series stay apart for colour-blind readers, and
// every series is named in a legend or beside its mark rather than by colour
// alone. Text is always the theme's own ink, never a series colour.
//
// Each chart takes the page's arrival and plays its part in it, as the
// Report's chart does: the ring sweeps round, bars grow from their baseline,
// the burn-up draws itself left to right. A nil arrival leaves a chart drawn
// in full.

// palette is the chart colours for the theme in use.
type palette struct {
	done, progress, review, todo, backlog, blocked color.Color
	// neutral is for open work with no state of its own; dropped, dimmer
	// still, for what was closed without being done.
	neutral, dropped color.Color
	// ink and soft are the text colours; grid the axis and guide lines; surface
	// the panel the chart stands on, used for the gaps between marks.
	ink, soft, grid, surface color.Color
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func currentPalette() palette {
	bg := theme.Color(theme.ColorNameBackground)
	fg := theme.Color(theme.ColorNameForeground)
	p := palette{
		ink:     fg,
		soft:    shell.BlendColor(fg, bg, 0.35),
		grid:    shell.BlendColor(bg, fg, 0.16),
		surface: shell.BlendColor(bg, fg, 0.08), // a panel's fill
		neutral: shell.BlendColor(bg, fg, 0.50),
		dropped: shell.BlendColor(bg, fg, 0.24),
	}
	variant := fyne.CurrentApp().Settings().ThemeVariant()
	if shell.DarkSurface(theme.Current(), variant) {
		p.done, p.progress, p.review = rgb(0x199e70), rgb(0xc98500), rgb(0x3987e5)
		p.todo, p.backlog, p.blocked = rgb(0xd55181), rgb(0x9085e9), rgb(0xd95926)
	} else {
		p.done, p.progress, p.review = rgb(0x1baf7a), rgb(0xeda100), rgb(0x2a78d6)
		p.todo, p.backlog, p.blocked = rgb(0xe87ba4), rgb(0x6250d6), rgb(0xeb6834)
	}
	return p
}

// sliceColors gives each status slice its colour: its class's own, or the
// next one free when two slices share a class.
func (p palette) sliceColors(slices []Slice) []color.Color {
	own := map[int]color.Color{
		ClassDone: p.done, ClassProgress: p.progress, ClassReview: p.review,
		ClassToDo: p.todo, ClassBacklog: p.backlog, ClassBlocked: p.blocked,
		ClassOpen: p.progress, // open and nothing more known: the workload chart's Open
	}
	spare := []color.Color{p.progress, p.review, p.todo, p.backlog, p.blocked}
	used := map[color.Color]bool{}
	out := make([]color.Color, len(slices))
	for i, s := range slices {
		if s.Class == ClassNotPlanned {
			out[i] = p.dropped
			continue
		}
		c, ok := own[s.Class]
		if !ok || used[c] {
			c = p.neutral
			for _, candidate := range spare {
				if !used[candidate] {
					c = candidate
					break
				}
			}
		}
		used[c] = true
		out[i] = c
	}
	return out
}

// stateColor is the colour an issue's state is drawn in.
func (p palette) stateColor(it Item) color.Color {
	switch {
	case it.NotPlanned:
		return p.neutral
	case it.Closed:
		return p.done
	case it.Status == "":
		return p.progress
	}
	switch statusClass(it.Status) {
	case ClassReview:
		return p.review
	case ClassToDo:
		return p.todo
	case ClassBacklog:
		return p.backlog
	case ClassBlocked:
		return p.blocked
	}
	return p.progress
}

func caption(text string, ink color.Color) *canvas.Text {
	t := canvas.NewText(text, ink)
	t.TextSize = theme.CaptionTextSize()
	return t
}

func boldText(text string, ink color.Color) *canvas.Text {
	t := canvas.NewText(text, ink)
	t.TextStyle = fyne.TextStyle{Bold: true}
	return t
}

// swatch is a legend's colour key.
func swatch(c color.Color) fyne.CanvasObject {
	r := canvas.NewRectangle(c)
	r.CornerRadius = 3
	r.SetMinSize(fyne.NewSquareSize(10))
	return container.NewCenter(r)
}

// legend sets colour keys side by side.
func legend(entries ...fyne.CanvasObject) *fyne.Container {
	return container.New(layout.NewCustomPaddedHBoxLayout(theme.Padding()*4), entries...)
}

// legendEntry is a colour key with its name and, when given, its figure.
func legendEntry(p palette, c color.Color, name, figure string) fyne.CanvasObject {
	row := container.New(layout.NewCustomPaddedHBoxLayout(6), swatch(c), canvas.NewText(name, p.soft))
	if figure != "" {
		row.Add(boldText(figure, p.ink))
	}
	return row
}

// whole plays one piece over the arrival's full length: a chart that comes in
// as a single move.
func whole(arrive *shell.Arrival, set func(share float32)) {
	arrive.Cascade([]time.Duration{0}, func(_ int, share float32) { set(share) })
}

// ---- status doughnut ----

const (
	donutSize   = float32(150)
	donutCutout = float32(0.64)
	donutSweep  = 1200 * time.Millisecond
	// donutGap is the space between two slices, in degrees.
	donutGap = float32(2.4)
)

// Donut draws the slices as a ring with a headline figure in the middle. It
// arrives sweeping round from the top while the figure counts up.
func Donut(slices []Slice, colors []color.Color, big, small string, p palette, arrive *shell.Arrival) fyne.CanvasObject {
	total := 0
	for _, s := range slices {
		total += s.Count
	}
	var arcs []*canvas.Arc
	var spans [][2]float32
	if total == 0 {
		arcs = append(arcs, canvas.NewArc(0, 360, donutCutout, p.grid))
		spans = append(spans, [2]float32{0, 360})
	}
	angle := float32(0)
	for i, s := range slices {
		if s.Count == 0 {
			continue
		}
		sweep := float32(s.Count) / float32(total) * 360
		from, to := angle, angle+sweep
		// The gap comes off both ends, unless the slice is too thin to spare it.
		if len(slices) > 1 && sweep > donutGap*2 {
			from, to = from+donutGap/2, to-donutGap/2
		}
		arcs = append(arcs, canvas.NewArc(from, to, donutCutout, colors[i]))
		spans = append(spans, [2]float32{from, to})
		angle += sweep
	}
	headline := canvas.NewText(big, p.ink)
	headline.TextSize = theme.TextSize() + 10
	headline.TextStyle = fyne.TextStyle{Bold: true}
	headline.Alignment = fyne.TextAlignCenter
	under := caption(small, p.soft)
	under.Alignment = fyne.TextAlignCenter

	objects := make([]fyne.CanvasObject, 0, len(arcs)+2)
	for _, arc := range arcs {
		objects = append(objects, arc)
	}
	objects = append(objects, headline, under)
	arrive.CascadeFor(donutSweep, []time.Duration{0}, func(_ int, share float32) {
		for i, arc := range arcs {
			arc.StartAngle, arc.EndAngle = spans[i][0]*share, spans[i][1]*share
			arc.Refresh()
		}
	})
	arrive.CountUp([]*canvas.Text{headline})
	return container.New(donutLayout{}, objects...)
}

// donutLayout centres the ring, and the last two objects (the figure and its
// caption) inside it. The texts take the full width and centre themselves, so
// a figure counting up stays in the middle.
type donutLayout struct{}

func (donutLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSquareSize(donutSize) }

func (donutLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	side := min(size.Width, size.Height)
	at := fyne.NewPos((size.Width-side)/2, (size.Height-side)/2)
	n := len(objects)
	for _, arc := range objects[:n-2] {
		arc.Move(at)
		arc.Resize(fyne.NewSquareSize(side))
	}
	big, small := objects[n-2], objects[n-1]
	bigHeight, smallHeight := big.MinSize().Height, small.MinSize().Height
	top := (size.Height - bigHeight - smallHeight) / 2
	big.Move(fyne.NewPos(0, top))
	big.Resize(fyne.NewSize(size.Width, bigHeight))
	small.Move(fyne.NewPos(0, top+bigHeight))
	small.Resize(fyne.NewSize(size.Width, smallHeight))
}

// ---- horizontal bars ----

const (
	barThickness = float32(12)
	barRadius    = float32(4)
	barGap       = float32(2) // between the segments of one bar
	barRowHeight = float32(28)
	barLabelMax  = float32(150)
)

// Segment is one stretch of a bar.
type Segment struct {
	Value int
	Color color.Color
}

// bar is one horizontal bar of stacked segments.
type bar struct {
	box    *fyne.Container
	layout *barLayout
}

// thin draws the bar at a slimmer thickness, for a compact list.
func (b bar) thin(thickness float32) bar {
	b.layout.thickness = thickness
	return b
}

// grow sets how far the bar has arrived, 0 to 1.
func (b bar) grow(share float32) {
	b.layout.share = share
	b.box.Refresh()
}

// newBar is a bar scaled so that full fills the width. track, when set, is
// drawn full width behind the segments: for a bar that shows progress toward
// full rather than a bare amount.
func newBar(segments []Segment, full int, track color.Color) bar {
	l := &barLayout{max: full, share: 1, thickness: barThickness}
	var objects []fyne.CanvasObject
	if track != nil {
		rail := canvas.NewRectangle(track)
		rail.CornerRadius = barRadius
		objects = append(objects, rail)
		l.track = true
	}
	for _, s := range segments {
		if s.Value <= 0 {
			continue
		}
		l.values = append(l.values, s.Value)
		objects = append(objects, canvas.NewRectangle(s.Color))
	}
	return bar{box: container.New(l, objects...), layout: l}
}

// growBars brings the bars in one after another, each growing from its
// baseline.
func growBars(arrive *shell.Arrival, bars []bar) {
	arrive.Cascade(shell.ArriveDelays(len(bars)), func(i int, share float32) { bars[i].grow(share) })
}

type barLayout struct {
	values    []int
	max       int
	track     bool
	share     float32
	thickness float32
}

func (l *barLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(60, l.thickness) }

func (l *barLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	barThickness := l.thickness
	y := (size.Height - barThickness) / 2
	if l.track {
		objects[0].Move(fyne.NewPos(0, y))
		objects[0].Resize(fyne.NewSize(size.Width, barThickness))
		objects = objects[1:]
	}
	if l.max <= 0 {
		return
	}
	x := float32(0)
	for i, value := range l.values {
		rect := objects[i].(*canvas.Rectangle)
		w := size.Width * float32(value) / float32(l.max) * l.share
		last := i == len(l.values)-1
		if !last {
			w -= barGap // the gap is taken out of the segment, not added to the bar
		}
		w = max(w, 0)
		if l.share >= 1 {
			w = max(w, 1) // a sliver, so a small count is still seen
		}
		// Square where it meets the baseline or the next segment, round at the
		// bar's far end. A tracked bar is round at both, to sit in its rail.
		rect.TopLeftCornerRadius, rect.BottomLeftCornerRadius = 0, 0
		rect.TopRightCornerRadius, rect.BottomRightCornerRadius = 0, 0
		if last {
			rect.TopRightCornerRadius, rect.BottomRightCornerRadius = barRadius, barRadius
		}
		if i == 0 && l.track {
			rect.TopLeftCornerRadius, rect.BottomLeftCornerRadius = barRadius, barRadius
		}
		rect.Move(fyne.NewPos(x, y))
		rect.Resize(fyne.NewSize(w, barThickness))
		x += w
		if !last {
			x += barGap
		}
	}
}

// BarRow is one line of a bar table: a name, its bar, and the figure at the
// bar's end. OnTap, when set, makes the whole line clickable.
type BarRow struct {
	Label    string
	Segments []Segment
	Figure   string
	OnTap    func()
}

// BarTable lines the rows up in three columns, the bars all on one scale.
func BarTable(rows []BarRow, p palette, arrive *shell.Arrival) fyne.CanvasObject {
	top := 0
	for _, row := range rows {
		sum := 0
		for _, s := range row.Segments {
			sum += s.Value
		}
		top = max(top, sum)
	}
	var objects []fyne.CanvasObject
	var bars []bar
	for _, row := range rows {
		label := canvas.NewText(shell.FitText(row.Label, theme.TextSize(), fyne.TextStyle{}, barLabelMax), p.soft)
		b := newBar(row.Segments, top, nil)
		bars = append(bars, b)
		// The last of the four lies over the whole line and takes its clicks.
		hit := shell.NewTappable(canvas.NewRectangle(color.Transparent), row.OnTap)
		if row.OnTap == nil {
			hit.Look = desktop.DefaultCursor
		}
		objects = append(objects, label, b.box, boldText(row.Figure, p.ink), hit)
	}
	growBars(arrive, bars)
	list := newHoverList(0)
	label, figure := (barTableLayout{}).columns(objects)
	for i := 0; i+barTableStride <= len(objects); i += barTableStride {
		index := i / barTableStride
		hit := objects[i+3].(*shell.Tappable)
		hit.OnHover = func(on bool) { list.hover(index, on) }
		list.Rows.Add(container.New(barTableLayout{label: label, figure: figure}, objects[i:i+barTableStride]...))
	}
	return list
}

// barTableLayout places objects four to a row: label, bar, figure, and the
// click area over all three. The label and figure columns are as wide as
// their widest entry.
type barTableLayout struct{ label, figure float32 }

const barTableStride = 4

func (l barTableLayout) columns(objects []fyne.CanvasObject) (label, figure float32) {
	label, figure = l.label, l.figure
	for i := 0; i+barTableStride <= len(objects); i += barTableStride {
		label = max(label, objects[i].MinSize().Width)
		figure = max(figure, objects[i+2].MinSize().Width)
	}
	return label, figure
}

func (l barTableLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	label, figure := l.columns(objects)
	return fyne.NewSize(label+figure+90, barRowHeight*float32(len(objects)/barTableStride))
}

func (l barTableLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	label, figure := l.columns(objects)
	gap := theme.Padding() * 2
	inset := theme.Padding() // keeps the text off the click area's outline
	width := max(size.Width-label-figure-gap*2-inset*2, 0)
	for i := 0; i+barTableStride <= len(objects); i += barTableStride {
		y := barRowHeight * float32(i/barTableStride)
		columns := []struct{ x, w float32 }{
			{inset, label}, {inset + label + gap, width}, {inset + label + gap + width + gap, figure}}
		for j, column := range columns {
			o := objects[i+j]
			h := o.MinSize().Height
			if j == 1 {
				h = barRowHeight
			}
			o.Move(fyne.NewPos(column.x, y+(barRowHeight-h)/2))
			o.Resize(fyne.NewSize(column.w, h))
		}
		objects[i+3].Move(fyne.NewPos(0, y+1))
		objects[i+3].Resize(fyne.NewSize(size.Width, barRowHeight-2))
	}
}

// ---- burn-up ----

const (
	plotMinHeight = float32(110)
	plotLineWidth = float32(2)
	plotDot       = float32(8)
	plotRing      = float32(2)
	// projectedAlpha fades the part of a line that has not happened yet.
	projectedAlpha = float32(0.45)
)

func faded(c color.Color, alpha float32) color.Color {
	r, g, b, _ := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(alpha * 255)}
}

// BurnUp draws the project's scope and finished work week by week. Where the
// two lines meet, the project is done. projection is how many weeks past the
// last one the lines run on, at the current pace, to reach that point. It
// arrives drawing itself from the first week to the last.
func BurnUp(weeks []WeekPoint, projection int, p palette, arrive *shell.Arrival) fyne.CanvasObject {
	l := &burnUpLayout{weeks: weeks, projection: projection, share: 1}
	last := weeks[len(weeks)-1]
	// The scale tops out at the most issues the project has had, so the lines
	// fill the height: 227 issues reach the top, not a rounded-up 400.
	l.top = max(last.Scope, 1)
	for _, w := range weeks {
		l.top = max(l.top, w.Scope)
	}
	add := func(o fyne.CanvasObject) fyne.CanvasObject { l.objects = append(l.objects, o); return o }
	line := func(c color.Color) *canvas.Line {
		ln := canvas.NewLine(c)
		ln.StrokeWidth = plotLineWidth
		add(ln)
		return ln
	}
	hair := func() *canvas.Rectangle { return add(canvas.NewRectangle(p.grid)).(*canvas.Rectangle) }

	for _, share := range []float32{0, 0.5, 1} {
		l.grid = append(l.grid, hair())
		l.gridLabels = append(l.gridLabels, add(caption(fmt.Sprint(int(float32(l.top)*share)), p.soft)).(*canvas.Text))
		l.gridShares = append(l.gridShares, share)
	}
	if projection > 0 {
		l.today = hair()
	}
	for range len(weeks) - 1 {
		l.scope = append(l.scope, line(p.review))
	}
	for range len(weeks) - 1 {
		l.done = append(l.done, line(p.done))
	}
	if projection > 0 {
		l.scopeAhead = line(faded(p.review, projectedAlpha))
		l.doneAhead = line(faded(p.done, projectedAlpha))
	}
	// The dots mark where each line stands today, ringed in the panel's colour
	// so they stay clear of the lines they sit on.
	dot := func(c color.Color) *canvas.Circle {
		d := canvas.NewCircle(c)
		d.StrokeColor, d.StrokeWidth = p.surface, plotRing
		add(d)
		return d
	}
	l.scopeDot, l.doneDot = dot(p.review), dot(p.done)

	l.firstLabel = add(caption(shortDate(weeks[0].Start), p.soft)).(*canvas.Text)
	if projection > 0 {
		l.todayLabel = add(caption("Today", p.soft)).(*canvas.Text)
		end := last.Start.AddDate(0, 0, 7*projection)
		l.lastLabel = add(caption(shortDate(end), p.soft)).(*canvas.Text)
	} else {
		l.lastLabel = add(caption("Today", p.soft)).(*canvas.Text)
	}

	// Under the pointer, a week is marked with a guide and its two dots, and a
	// box beside them says which week it is and what it brought.
	l.hover = -1
	l.guide = add(canvas.NewRectangle(widgets.WithAlpha(p.ink, 0.25))).(*canvas.Rectangle)
	l.hoverScope, l.hoverDone = dot(p.review), dot(p.done)
	l.tipBg = canvas.NewRectangle(theme.Color(theme.ColorNameMenuBackground))
	l.tipBg.CornerRadius = 6
	l.tipBg.StrokeColor, l.tipBg.StrokeWidth = widgets.LogoGold, 1
	add(l.tipBg)
	l.tipTitle = add(boldText("", widgets.LogoGold)).(*canvas.Text)
	l.tipTitle.TextSize = theme.CaptionTextSize() + 1
	for _, ink := range []color.Color{p.review, p.done, p.soft} {
		key := canvas.NewRectangle(ink)
		key.CornerRadius = 2
		l.tipKeys = append(l.tipKeys, add(key).(*canvas.Rectangle))
		l.tipLines = append(l.tipLines, add(caption("", p.ink)).(*canvas.Text))
	}
	l.tipKeys[2].Hide() // the totals line has no colour of its own
	l.showHover()

	c := &burnUpChart{layout: l}
	c.box = container.New(l, l.objects...)
	c.ExtendBaseWidget(c)
	whole(arrive, func(share float32) {
		l.share = share
		l.Layout(c.box.Objects, c.box.Size())
	})
	return c
}

// burnUpChart is the burn-up with a week's figures under the pointer.
type burnUpChart struct {
	widget.BaseWidget
	box    *fyne.Container
	layout *burnUpLayout
}

func (c *burnUpChart) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(c.box) }

func (c *burnUpChart) MouseIn(e *desktop.MouseEvent) { c.MouseMoved(e) }

func (c *burnUpChart) MouseMoved(e *desktop.MouseEvent) {
	if i := c.layout.weekAt(e.Position.X); i != c.layout.hover {
		c.layout.hover = i
		c.layout.Layout(c.box.Objects, c.box.Size())
	}
}

func (c *burnUpChart) MouseOut() {
	c.layout.hover = -1
	c.layout.Layout(c.box.Objects, c.box.Size())
}

type burnUpLayout struct {
	weeks      []WeekPoint
	projection int
	top        int
	// hover is the week under the pointer, or -1. left, plotWidth and span are
	// where the last layout put the weeks, to find it from the pointer.
	hover           int
	left, plotWidth float32
	span            int
	guide           *canvas.Rectangle
	hoverScope      *canvas.Circle
	hoverDone       *canvas.Circle
	tipBg           *canvas.Rectangle
	tipTitle        *canvas.Text
	tipKeys         []*canvas.Rectangle
	tipLines        []*canvas.Text
	// share is how far across the plot the lines have been drawn, 0 to 1.
	share float32

	objects                           []fyne.CanvasObject
	grid                              []*canvas.Rectangle
	gridLabels                        []*canvas.Text
	gridShares                        []float32
	today                             *canvas.Rectangle
	scope, done                       []*canvas.Line
	scopeAhead, doneAhead             *canvas.Line
	scopeDot, doneDot                 *canvas.Circle
	firstLabel, todayLabel, lastLabel *canvas.Text
}

func (*burnUpLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(200, plotMinHeight)
}

func (l *burnUpLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	left := float32(0)
	for _, label := range l.gridLabels {
		left = max(left, label.MinSize().Width)
	}
	left += 8
	axis := l.firstLabel.MinSize().Height + 4
	plot := fyne.NewSize(max(size.Width-left-plotDot, 1), max(size.Height-axis-plotDot, 1))
	topY := plotDot / 2
	y := func(v int) float32 { return topY + plot.Height*(1-float32(v)/float32(l.top)) }
	span := max(len(l.weeks)-1+l.projection, 1)
	x := func(i int) float32 { return left + plot.Width*float32(i)/float32(span) }
	// Nothing is drawn right of reach while the chart arrives.
	reach := left + plot.Width*l.share

	for i, rule := range l.grid {
		at := topY + plot.Height*(1-l.gridShares[i])
		rule.Move(fyne.NewPos(left, at))
		rule.Resize(fyne.NewSize(plot.Width, 1))
		m := l.gridLabels[i].MinSize()
		l.gridLabels[i].Move(fyne.NewPos(left-8-m.Width, at-m.Height/2))
		l.gridLabels[i].Resize(m)
	}
	place := func(ln *canvas.Line, x1, y1, x2, y2 float32) {
		switch {
		case x1 >= reach:
			x2, y2 = x1, y1 // not reached yet: nothing to draw
		case x2 > reach:
			y2 = y1 + (y2-y1)*(reach-x1)/(x2-x1)
			x2 = reach
		}
		ln.Position1, ln.Position2 = fyne.NewPos(x1, y1), fyne.NewPos(x2, y2)
		if x1 == x2 && y1 == y2 {
			ln.Hide()
		} else {
			ln.Show()
		}
		ln.Refresh()
	}
	for i := range l.scope {
		a, b := l.weeks[i], l.weeks[i+1]
		place(l.scope[i], x(i), y(a.Scope), x(i+1), y(b.Scope))
		place(l.done[i], x(i), y(a.Done), x(i+1), y(b.Done))
	}
	now := len(l.weeks) - 1
	last := l.weeks[now]
	arrived := x(now) <= reach
	if l.projection > 0 {
		place(l.scopeAhead, x(now), y(last.Scope), x(span), y(last.Scope))
		place(l.doneAhead, x(now), y(last.Done), x(span), y(last.Scope))
		l.today.Move(fyne.NewPos(x(now), topY))
		l.today.Resize(fyne.NewSize(1, plot.Height))
	}
	for dot, v := range map[*canvas.Circle]int{l.scopeDot: last.Scope, l.doneDot: last.Done} {
		dot.Move(fyne.NewPos(x(now)-plotDot/2, y(v)-plotDot/2))
		dot.Resize(fyne.NewSquareSize(plotDot))
		if arrived {
			dot.Show()
		} else {
			dot.Hide()
		}
	}

	labelY := topY + plot.Height + 4
	put := func(label *canvas.Text, centre float32) {
		m := label.MinSize()
		at := min(max(centre-m.Width/2, left), max(size.Width-m.Width, left))
		label.Move(fyne.NewPos(at, labelY))
		label.Resize(m)
	}
	first := l.firstLabel.MinSize()
	l.firstLabel.Move(fyne.NewPos(left, labelY))
	l.firstLabel.Resize(first)
	put(l.lastLabel, x(span))
	if l.todayLabel != nil {
		put(l.todayLabel, x(now))
		// Dropped when it would run into the dates at either end.
		m := l.todayLabel.MinSize()
		at := l.todayLabel.Position().X
		if at > left+first.Width+6 && at+m.Width < l.lastLabel.Position().X-6 {
			l.todayLabel.Show()
		} else {
			l.todayLabel.Hide()
		}
	} else if l.lastLabel.Position().X < left+first.Width+6 {
		l.firstLabel.Hide()
	} else {
		l.firstLabel.Show()
	}

	l.left, l.plotWidth, l.span = left, plot.Width, span
	l.placeHover(size, x, y, topY, plot.Height)
}

// weekAt is the week nearest x, or -1 when x is off the weeks gone by.
func (l *burnUpLayout) weekAt(x float32) int {
	if l.plotWidth <= 0 || l.span <= 0 || l.share < 1 {
		return -1
	}
	step := l.plotWidth / float32(l.span)
	i := int((x - l.left + step/2) / step)
	if x < l.left-step/2 || i < 0 || i >= len(l.weeks) {
		return -1
	}
	return i
}

// showHover shows or hides the hover marks as a week is under the pointer.
func (l *burnUpLayout) showHover() {
	objects := []fyne.CanvasObject{l.guide, l.hoverScope, l.hoverDone, l.tipBg, l.tipTitle, l.tipKeys[0], l.tipKeys[1]}
	for _, t := range l.tipLines {
		objects = append(objects, t)
	}
	for _, o := range objects {
		if l.hover >= 0 {
			o.Show()
		} else {
			o.Hide()
		}
	}
}

// placeHover puts the guide, the dots and the box on the week under the
// pointer: the box on the guide's right, or its left near the chart's end.
func (l *burnUpLayout) placeHover(size fyne.Size, x func(int) float32, y func(int) float32, topY, plotHeight float32) {
	l.showHover()
	if l.hover < 0 || l.hover >= len(l.weeks) {
		return
	}
	w := l.weeks[l.hover]
	at := x(l.hover)
	l.guide.Move(fyne.NewPos(at, topY))
	l.guide.Resize(fyne.NewSize(1, plotHeight))
	for dot, v := range map[*canvas.Circle]int{l.hoverScope: w.Scope, l.hoverDone: w.Done} {
		dot.Move(fyne.NewPos(at-plotDot/2, y(v)-plotDot/2))
		dot.Resize(fyne.NewSquareSize(plotDot))
		dot.Refresh()
	}

	l.tipTitle.Text = weekLabel(w.Start, l.hover == len(l.weeks)-1)
	if l.hover != len(l.weeks)-1 {
		l.tipTitle.Text = "Week of " + l.tipTitle.Text
	}
	l.tipLines[0].Text = fmt.Sprintf("Created  %d", w.Opened)
	l.tipLines[1].Text = fmt.Sprintf("Done  %d", w.Closed)
	l.tipLines[2].Text = fmt.Sprintf("By then: %d of %d done", w.Done, w.Scope)
	const pad, key, gap = float32(8), float32(8), float32(6)
	width := l.tipTitle.MinSize().Width
	height := l.tipTitle.MinSize().Height
	for i, t := range l.tipLines {
		t.Refresh()
		indent := key + gap
		if i == 2 {
			indent = 0
		}
		width = max(width, indent+t.MinSize().Width)
		height += t.MinSize().Height
	}
	box := fyne.NewSize(width+pad*2, height+pad*2)
	left := at + 10
	if left+box.Width > size.Width {
		left = at - 10 - box.Width
	}
	left = max(left, 0)
	top := topY
	l.tipBg.Move(fyne.NewPos(left, top))
	l.tipBg.Resize(box)
	l.tipBg.Refresh()
	row := fyne.NewPos(left+pad, top+pad)
	l.tipTitle.Move(row)
	l.tipTitle.Resize(l.tipTitle.MinSize())
	l.tipTitle.Refresh()
	row.Y += l.tipTitle.MinSize().Height
	for i, t := range l.tipLines {
		m := t.MinSize()
		indent := float32(0)
		if i < 2 {
			indent = key + gap
			l.tipKeys[i].Move(fyne.NewPos(row.X, row.Y+(m.Height-key)/2))
			l.tipKeys[i].Resize(fyne.NewSquareSize(key))
		}
		t.Move(fyne.NewPos(row.X+indent, row.Y))
		t.Resize(m)
		row.Y += m.Height
	}
}

func shortDate(t time.Time) string { return t.Format("2 Jan") }

// ---- columns ----

const (
	columnMax = float32(26)
	paceDot   = float32(6)
	// columnSlot is the narrowest a column's slot gets in a chart that
	// scrolls sideways, so its label still reads.
	columnSlot = float32(48)
	// scrollBarRoom keeps a sideways scroll's bar clear of the labels, the bar
	// at its widest under the pointer included.
	scrollBarRoom = float32(24)
)

// personColors are the colours of the people in the weekly chart, in the same
// checked order as the status colours, with Others in the neutral grey.
func (p palette) personColors(names []string) []color.Color {
	order := []color.Color{p.done, p.progress, p.review, p.todo, p.backlog, p.blocked}
	out := make([]color.Color, len(names))
	next := 0
	for i, name := range names {
		if name == othersName {
			out[i] = p.neutral
			continue
		}
		out[i] = order[next%len(order)]
		next++
	}
	return out
}

// ColumnSpec is what a column chart draws: a stacked column a slot, bottom
// segment first, on one scale.
type ColumnSpec struct {
	Stacks [][]Segment
	// Figures stand over the columns; "" leaves one bare.
	Figures []string
	// Labels name each column under the axis. Without them, Ends names the
	// first and last.
	Labels []string
	Ends   [2]string
	// Line runs across the columns on the same scale, such as a pace.
	Line    []float64
	LineInk color.Color
	// Marked is the column with a gold wash behind it, -1 for none.
	Marked int
	// OnTap, when set, makes each column clickable.
	OnTap func(i int)
	// Scrolls asks for room enough that no column is narrower than
	// columnSlot; put the chart in a sideways scroll.
	Scrolls bool
}

// ColumnChart is a chart of vertical columns. Under the pointer a column is
// lit; with OnTap set, clicking it runs OnTap.
type ColumnChart struct {
	widget.BaseWidget
	Content *fyne.Container
	layout  *columnLayout
	onTap   func(int)
}

// NewColumnChart builds the chart. Its columns arrive one after another,
// growing up from the axis, and the line follows them.
func NewColumnChart(spec ColumnSpec, p palette, arrive *shell.Arrival) *ColumnChart {
	n := len(spec.Stacks)
	l := &columnLayout{spec: spec, hover: -1, pill: widgets.NewHoverPill()}
	top := 1.0
	for _, stack := range spec.Stacks {
		sum := 0
		for _, s := range stack {
			sum += s.Value
		}
		top = max(top, float64(sum))
	}
	for _, v := range spec.Line {
		top = max(top, v)
	}
	l.top = top

	add := func(o fyne.CanvasObject) { l.objects = append(l.objects, o) }
	l.marked = canvas.NewRectangle(widgets.WithAlpha(widgets.LogoGold, 0.14))
	l.marked.CornerRadius = 4
	l.lit = l.pill.Rect
	l.baseline = canvas.NewRectangle(p.grid)
	add(l.marked)
	add(l.lit)
	add(l.baseline)
	for _, stack := range spec.Stacks {
		var column []*canvas.Rectangle
		for _, s := range stack {
			r := canvas.NewRectangle(s.Color)
			column = append(column, r)
			add(r)
		}
		l.segments = append(l.segments, column)
	}
	ink := spec.LineInk
	if ink == nil {
		ink = p.ink
	}
	if len(spec.Line) == n {
		for range max(n-1, 0) {
			ln := canvas.NewLine(ink)
			ln.StrokeWidth = plotLineWidth
			l.lines = append(l.lines, ln)
			add(ln)
		}
		for range n {
			dot := canvas.NewCircle(ink)
			dot.StrokeColor, dot.StrokeWidth = p.surface, plotRing
			l.dots = append(l.dots, dot)
			add(dot)
		}
	}
	for i := range n {
		text := ""
		if i < len(spec.Figures) {
			text = spec.Figures[i]
		}
		figure := boldText(text, p.ink)
		figure.TextSize = theme.CaptionTextSize() + 1
		l.figures = append(l.figures, figure)
		add(figure)
	}
	if spec.Labels != nil {
		for _, name := range spec.Labels {
			label := caption(name, p.soft)
			l.labels = append(l.labels, label)
			add(label)
		}
	} else {
		for _, name := range spec.Ends {
			label := caption(name, p.soft)
			l.labels = append(l.labels, label)
			add(label)
		}
	}
	c := &ColumnChart{Content: container.New(l, l.objects...), layout: l, onTap: spec.OnTap}
	c.ExtendBaseWidget(c)
	if arrive != nil && n > 0 {
		l.shares = make([]float32, n)
		arrive.Cascade(shell.ArriveDelays(n), func(i int, share float32) {
			// The last column can round to fully arrived a frame or two before
			// the cascade ends; once the shares are let go, later frames have
			// nothing left to do.
			if l.shares == nil {
				return
			}
			l.shares[i] = share
			if i == n-1 { // one relayout a frame, once every column has its share
				if share >= 1 {
					l.shares = nil
				}
				l.Layout(c.Content.Objects, c.Content.Size())
			}
		})
	}
	return c
}

func (c *ColumnChart) CreateRenderer() fyne.WidgetRenderer {
	return &columnRenderer{WidgetRenderer: widget.NewSimpleRenderer(c.Content), pill: c.layout.pill}
}

type columnRenderer struct {
	fyne.WidgetRenderer
	pill *widgets.HoverPill
}

func (r *columnRenderer) Destroy() { r.pill.Stop(); r.WidgetRenderer.Destroy() }

func (c *ColumnChart) slotAt(x float32) int {
	n := len(c.layout.spec.Stacks)
	w := c.Size().Width
	if n == 0 || w <= 0 || x < 0 || x >= w {
		return -1
	}
	return min(int(x/w*float32(n)), n-1)
}

func (c *ColumnChart) Tapped(e *fyne.PointEvent) {
	if i := c.slotAt(e.Position.X); i >= 0 && c.onTap != nil {
		c.onTap(i)
	}
}

func (c *ColumnChart) MouseIn(e *desktop.MouseEvent) { c.MouseMoved(e) }

func (c *ColumnChart) MouseMoved(e *desktop.MouseEvent) {
	if i := c.slotAt(e.Position.X); i != c.layout.hover {
		c.layout.hover = i
		c.layout.moveHover(c.Content.Size())
	}
}

func (c *ColumnChart) MouseOut() {
	c.layout.hover = -1
	c.layout.moveHover(c.Content.Size())
}

func (c *ColumnChart) Cursor() desktop.Cursor {
	if c.onTap != nil {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

// columnLayout places a column chart's pieces, which it holds itself.
type columnLayout struct {
	spec  ColumnSpec
	top   float64
	hover int
	pill  *widgets.HoverPill
	// shares is how far each column has arrived; nil when all have.
	shares []float32

	objects     []fyne.CanvasObject
	marked, lit *canvas.Rectangle
	baseline    *canvas.Rectangle
	segments    [][]*canvas.Rectangle
	lines       []*canvas.Line
	dots        []*canvas.Circle
	figures     []*canvas.Text
	labels      []*canvas.Text
	// plot is the area the columns stand in, from the last Layout.
	plotTop, base float32
}

func (l *columnLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	width := float32(150)
	if l.spec.Scrolls {
		width = max(width, columnSlot*float32(len(l.spec.Stacks)))
		// The bar's room is on top of the chart's own: the dashboard keeps a
		// panel at least this tall, so the chart never runs on below the
		// scroll's foot with the bar drawn across its columns.
		return fyne.NewSize(width, plotMinHeight+scrollBarRoom)
	}
	return fyne.NewSize(width, plotMinHeight)
}

func (l *columnLayout) share(i int) float32 {
	if l.shares == nil {
		return 1
	}
	return l.shares[i]
}

// placeWashes puts the gold wash behind the marked column and the light one
// behind the column under the pointer.
func (l *columnLayout) placeWashes(size fyne.Size) {
	n := len(l.spec.Stacks)
	if n == 0 || size.Width <= 0 {
		return
	}
	slot := size.Width / float32(n)
	place := func(r *canvas.Rectangle, i int) {
		if i < 0 || i >= n {
			r.Hide()
			return
		}
		r.Move(fyne.NewPos(slot*float32(i)+1, 0))
		r.Resize(fyne.NewSize(slot-2, l.base+2))
		r.Show()
	}
	place(l.marked, l.spec.Marked)
	if l.hover >= 0 && l.hover < n {
		pos, frame := l.hoverFrame(size)
		l.pill.Fit(pos, frame)
	}
}

func (l *columnLayout) hoverFrame(size fyne.Size) (fyne.Position, fyne.Size) {
	slot := size.Width / float32(max(len(l.spec.Stacks), 1))
	return fyne.NewPos(slot*float32(l.hover)+1, 0), fyne.NewSize(max(slot-2, 0), l.base+2)
}

func (l *columnLayout) moveHover(size fyne.Size) {
	pos, frame := l.hoverFrame(size)
	l.pill.MoveTo(pos, frame, l.hover >= 0)
}

func (l *columnLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	if l.spec.Scrolls {
		// The scroll bar runs along the bottom; the labels stand above it.
		size.Height -= scrollBarRoom
	}
	n := len(l.spec.Stacks)
	if n == 0 || size.Width <= 0 || size.Height <= 0 {
		return
	}
	axis := caption("0", nil).MinSize().Height + 6
	headroom := l.figures[0].MinSize().Height + 4 // for the figure over the tallest
	plot := max(size.Height-axis-headroom, 1)
	l.plotTop, l.base = headroom, headroom+plot
	base := l.base
	height := func(v float64) float32 { return plot * float32(v/l.top) }
	l.baseline.Move(fyne.NewPos(0, base))
	l.baseline.Resize(fyne.NewSize(size.Width, 1))
	slot := size.Width / float32(n)
	width := min(columnMax, slot*0.62)
	l.placeWashes(size)

	for i, stack := range l.spec.Stacks {
		x := slot*float32(i) + (slot-width)/2
		y := base
		last := -1
		for k, s := range stack {
			if s.Value > 0 {
				last = k
			}
		}
		total := 0
		for k, s := range stack {
			total += s.Value
			r := l.segments[i][k]
			h := height(float64(s.Value)) * l.share(i)
			if s.Value > 0 && k != last {
				h = max(h-barGap, 0) // the gap is taken out of the segment below
			}
			r.TopLeftCornerRadius, r.TopRightCornerRadius = 0, 0
			if k == last {
				r.TopLeftCornerRadius, r.TopRightCornerRadius = barRadius, barRadius
			}
			r.Move(fyne.NewPos(x, y-h))
			r.Resize(fyne.NewSize(width, h))
			if s.Value > 0 {
				y -= h
				if k != last {
					y -= barGap * l.share(i)
				}
			}
		}
		figure := l.figures[i]
		m := figure.MinSize()
		figure.Move(fyne.NewPos(slot*float32(i)+(slot-m.Width)/2, base-height(float64(total))*l.share(i)-m.Height-2))
		figure.Resize(m)
	}

	// The line joins the middle of each column, each point showing once the
	// column under it has arrived.
	at := func(i int) fyne.Position {
		return fyne.NewPos(slot*float32(i)+slot/2, base-height(l.spec.Line[i]))
	}
	for i, dot := range l.dots {
		pos := at(i)
		dot.Move(fyne.NewPos(pos.X-paceDot/2, pos.Y-paceDot/2))
		dot.Resize(fyne.NewSquareSize(paceDot))
		if l.share(i) >= 1 {
			dot.Show()
		} else {
			dot.Hide()
		}
	}
	for i, ln := range l.lines {
		ln.Position1, ln.Position2 = at(i), at(i+1)
		if l.share(i+1) >= 1 {
			ln.Show()
		} else {
			ln.Hide()
		}
		ln.Refresh()
	}

	labelY := base + 4
	if l.spec.Labels != nil {
		for i, label := range l.labels {
			label.Text = shell.FitText(l.spec.Labels[i], label.TextSize, label.TextStyle, max(slot-4, 8))
			m := label.MinSize()
			label.Move(fyne.NewPos(slot*float32(i)+(slot-m.Width)/2, labelY))
			label.Resize(m)
			label.Refresh()
		}
		return
	}
	first, last := l.labels[0], l.labels[1]
	first.Move(fyne.NewPos(0, labelY))
	first.Resize(first.MinSize())
	m := last.MinSize()
	last.Move(fyne.NewPos(size.Width-m.Width, labelY))
	last.Resize(m)
	if n == 1 || first.MinSize().Width+m.Width+8 > size.Width {
		first.Hide()
	} else {
		first.Show()
	}
}

// ---- pace sparkline ----

// Sparkline is a small line of values, for a tile: no axis or labels, the
// last point marked.
func Sparkline(values []float64, ink color.Color, p palette) fyne.CanvasObject {
	l := &sparkLayout{values: values}
	var objects []fyne.CanvasObject
	for range max(len(values)-1, 0) {
		ln := canvas.NewLine(ink)
		ln.StrokeWidth = plotLineWidth
		l.lines = append(l.lines, ln)
		objects = append(objects, ln)
	}
	l.dot = canvas.NewCircle(ink)
	l.dot.StrokeColor, l.dot.StrokeWidth = p.surface, 1
	objects = append(objects, l.dot)
	return container.New(l, objects...)
}

type sparkLayout struct {
	values []float64
	lines  []*canvas.Line
	dot    *canvas.Circle
}

const sparkHeight = float32(22)

func (*sparkLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(40, sparkHeight) }

func (l *sparkLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	n := len(l.values)
	if n == 0 || size.Width <= 0 {
		l.dot.Hide()
		return
	}
	low, high := l.values[0], l.values[0]
	for _, v := range l.values {
		low, high = min(low, v), max(high, v)
	}
	low = min(low, 0) // the line stands on zero, so a flat pace reads as such
	span := max(high-low, 1e-9)
	inset := paceDot / 2
	at := func(i int) fyne.Position {
		x := inset + (size.Width-2*inset)*float32(i)/float32(max(n-1, 1))
		y := inset + (size.Height-2*inset)*float32(1-(l.values[i]-low)/span)
		return fyne.NewPos(x, y)
	}
	for i, ln := range l.lines {
		ln.Position1, ln.Position2 = at(i), at(i+1)
		ln.Refresh()
	}
	end := at(n - 1)
	l.dot.Move(fyne.NewPos(end.X-paceDot/2, end.Y-paceDot/2))
	l.dot.Resize(fyne.NewSquareSize(paceDot))
	l.dot.Show()
}
