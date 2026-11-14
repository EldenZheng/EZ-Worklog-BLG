package projects

import (
	"cmp"
	"fmt"
	"image/color"
	"slices"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// ---- the page under the charts ----

// pageStack holds the charts at the height of the window less the folding
// sections under them (the issue list), so with those folded
// the whole page fits and their headings show along the bottom edge. Unfolded,
// a section runs on under the charts, reached by scrolling down.
type pageStack struct {
	scroll *container.Scroll
	// peeks are how tall each section is folded: its heading alone.
	peeks []func() float32
}

// peek is the room the folded sections take under the charts.
func (l *pageStack) peek() float32 {
	total := float32(0)
	for _, p := range l.peeks {
		total += pageGap() + p()
	}
	return total
}

func (l *pageStack) viewport() float32 {
	if l.scroll == nil {
		return 0
	}
	return l.scroll.Size().Height
}

// dashHeight is the charts' height: the window less the folded list.
func (l *pageStack) dashHeight(dash fyne.CanvasObject) float32 {
	return max(dash.MinSize().Height, l.viewport()-l.peek())
}

func (l *pageStack) MinSize(objects []fyne.CanvasObject) fyne.Size {
	width, height := objects[0].MinSize().Width, l.dashHeight(objects[0])
	for _, o := range objects[1:] {
		m := o.MinSize()
		width, height = max(width, m.Width), height+pageGap()+m.Height
	}
	return fyne.NewSize(width, height)
}

func (l *pageStack) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	y := l.dashHeight(objects[0])
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(size.Width, y))
	for _, o := range objects[1:] {
		y += pageGap()
		o.Move(fyne.NewPos(0, y))
		h := o.MinSize().Height
		o.Resize(fyne.NewSize(size.Width, h))
		y += h
	}
}

// section is a folding panel under the charts and how tall it is folded.
type section struct {
	panel fyne.CanvasObject
	peek  func() float32
}

// withSections is the page: the charts filling the window, the folding
// sections under them. A redraw of the same project keeps where the page was
// scrolled to.
func (p *page) withSections(id string, dash fyne.CanvasObject, sections ...section) fyne.CanvasObject {
	stack := &pageStack{}
	objects := []fyne.CanvasObject{dash}
	for _, s := range sections {
		objects = append(objects, s.panel)
		stack.peeks = append(stack.peeks, s.peek)
	}
	scroll := container.NewVScroll(container.New(stack, objects...))
	stack.scroll = scroll
	if p.scroll != nil && p.scrolled == id {
		scroll.Offset = p.scroll.Offset
	}
	p.scroll, p.scrolled = scroll, id
	return scroll
}

// slideSection follows a section as it unfolds or folds, a frame at a time.
// Unfolding, the page rides up with it until its heading reaches the top of
// the window; folding, the page settles back as it shrinks.
func (p *page) slideSection(panel fyne.CanvasObject, open, final bool) {
	scroll := p.scroll
	if scroll == nil || scroll.Content == nil {
		return
	}
	shell.Reflow(false, scroll, scroll.Content)
	if open {
		limit := max(scroll.Content.Size().Height-scroll.Size().Height, 0)
		scroll.ScrollToOffset(fyne.NewPos(0, min(panel.Position().Y, limit)))
	}
	if final {
		scroll.Refresh()
	} else {
		canvas.Refresh(scroll)
	}
}

// sectionSlide is how long a section under the charts takes to fold open or
// shut, the page panning along with it.
const sectionSlide = 600 * time.Millisecond

// foldSection is a panel under the charts whose gold heading folds its body
// away and back, as Log Work's Saved locally does. open keeps whether it is
// unfolded across redraws (not across restarts); end stands at the heading's
// right.
func (p *page) foldSection(title string, open *bool, end, body fyne.CanvasObject) section {
	heading := canvas.NewText(title, widgets.LogoGold)
	heading.TextSize = theme.TextSize() + 2
	heading.TextStyle = fyne.TextStyle{Bold: true}
	var panel fyne.CanvasObject
	// The whole heading line folds the section, over sectionSlide, and the
	// page pans with it.
	line, fold := shell.CollapseLine(container.NewCenter(heading), end, body, *open, sectionSlide,
		func(now bool) { *open = now },
		func(final bool) { p.slideSection(panel, *open, final) })
	panel = shell.Panel(container.New(layout.NewCustomPaddedVBoxLayout(0), line, fold))
	return section{panel, func() float32 { return panel.MinSize().Height - fold.MinSize().Height }}
}

// ---- milestones ----

// milestonesPanel is each milestone with its due date and how much of it is
// done. A line opens its issues.
func (v *view) milestonesPanel() fyne.CanvasObject {
	pal := v.pal
	list := v.s.Milestones
	if len(list) == 0 {
		return chartPanel("Milestones", nil, noteLabel("No milestones on these issues."), "")
	}
	rows := newHoverList(theme.Padding())
	var bars []bar
	var figures []*canvas.Text
	for _, m := range list {
		due := "no due date"
		ink := pal.soft
		switch {
		case m.Name == noMilestone:
			due = ""
		case m.Due.IsZero():
		case m.Open > 0 && m.Due.Before(v.now):
			due = "overdue since " + m.Due.Format("2 Jan")
			ink = theme.Color(theme.ColorNameError)
		default:
			due = "due " + m.Due.Format("2 Jan 2006")
		}
		// The name gives way to the due date, cut short with an ellipsis at
		// whatever width is left, rather than running under it.
		name := boldText(m.Name, pal.ink)
		name.TextSize = theme.TextSize() + 3
		fitted := container.New(&fitHeading{text: name, full: m.Name}, name)
		figure := caption(fmt.Sprintf("%d of %d  ·  %s", m.Done, m.Total(), share(m.Done, m.Total())), pal.ink)
		figure.TextStyle = fyne.TextStyle{Bold: true}
		figures = append(figures, figure)
		b := newBar([]Segment{{m.Done, pal.done}}, m.Total(), pal.grid).thin(8)
		bars = append(bars, b)
		var when fyne.CanvasObject
		if due != "" {
			when = container.New(layout.NewCustomPaddedLayout(0, 0, theme.Padding()*2, 0), caption(due, ink))
		}
		line := container.New(layout.NewCustomPaddedVBoxLayout(2),
			container.NewBorder(nil, nil, nil, when, fitted),
			container.NewBorder(nil, nil, nil, container.NewCenter(figure), b.box))
		rows.add(container.New(layout.NewCustomPaddedLayout(1, 1, 2, 2), line), func() {
			v.p.showIssues("Milestone: "+m.Name, issuesIn(m.Items)...)
		})
	}
	// Each milestone uncovers in turn, its bar grows from the left and its
	// figure counts up.
	v.arrive.RevealIn(rows.Rows)
	growBars(v.arrive, bars)
	v.arrive.CountUp(figures)
	return chartPanel("Milestones", nil, listScroll(rows), "")
}

// ---- the issue list ----

const (
	// listShown is the most rows the list draws; a search narrows the rest.
	listShown  = 150
	unassigned = "Unassigned"
	noStatus   = "No status"
	// noValue is the drop-down choice that clears a Priority or Sprint.
	noValue = "None"
)

// listColumns are the list's columns and their shares of its width.
var listColumns = []struct {
	name  string
	share float32
}{
	{"Issue", 4}, {"Assignee", 1.6}, {"Status", 1.6}, {"Priority", 1.1}, {"Sprint", 1.3}, {"Milestone", 1.2}, {"Age", 0.9},
}

func listWeights() []float32 {
	var w []float32
	for _, c := range listColumns {
		w = append(w, c.share)
	}
	return w
}

// SortIssues orders items by a list column: Issue, Assignee, Status,
// Priority, Sprint, Milestone or Age; any other, such as "", is the list's
// own order, the newest first. Ascending is A to Z, the board's own
// order of statuses, the most urgent priority first, the earliest sprint or
// milestone first, and the youngest issue first; desc reverses it. Issues with
// nothing in that column come last either way, and ties keep the newest first.
func SortIssues(items []Item, by string, desc bool) {
	// key is an issue's place in the order; missing is an empty column.
	type key struct {
		rank    int
		when    time.Time
		text    string
		missing bool
	}
	// A status the board does not list sorts after those it does.
	const unlisted = 1 << 20
	keyOf := func(it Item) key {
		switch by {
		case "Issue":
			return key{rank: it.Number, text: strings.ToLower(it.Repo)}
		case "Assignee":
			if len(it.Assignees) == 0 {
				return key{missing: true}
			}
			return key{text: strings.ToLower(it.Assignees[0])}
		case "Status":
			if it.Status == "" {
				return key{missing: true}
			}
			rank := unlisted
			if it.Board != nil {
				if i := slices.IndexFunc(it.Board.Options, func(o Option) bool { return o.Name == it.Status }); i >= 0 {
					rank = i
				}
			}
			return key{rank: rank, text: strings.ToLower(it.Status)}
		case "Priority":
			if it.Priority == "" {
				return key{missing: true}
			}
			return key{rank: priorityRank(it.Priority), text: strings.ToLower(it.Priority)}
		case "Sprint":
			if it.Sprint == "" {
				return key{missing: true}
			}
			return key{when: it.SprintStart, text: strings.ToLower(it.Sprint)}
		case "Milestone":
			if it.Milestone == "" {
				return key{missing: true}
			}
			// A milestone with no due date after those with one.
			rank := 0
			if it.MilestoneDue.IsZero() {
				rank = 1
			}
			return key{rank: rank, when: it.MilestoneDue, text: strings.ToLower(it.Milestone)}
		}
		// Age: the youngest is the latest created.
		return key{rank: -int(it.Created.Unix())}
	}
	slices.SortStableFunc(items, func(x, y Item) int {
		a, b := keyOf(x), keyOf(y)
		if a.missing != b.missing {
			if a.missing {
				return 1
			}
			return -1
		}
		c := cmp.Or(cmp.Compare(a.rank, b.rank), a.when.Compare(b.when), strings.Compare(a.text, b.text))
		if desc {
			c = -c
		}
		if c != 0 {
			return c
		}
		return y.Created.Compare(x.Created)
	})
}

// issueList is every issue in a table: who holds it, its Status, Priority and
// Sprint, which can be changed from here, then its milestone and age. The
// heading folds the list away and back; a column's name sorts by it, a
// second click the other way round, and a third back to Sprint order. The search and the Open / Done / All choice narrow it
// without redrawing the page.
func (v *view) issueList() section {
	p, pal := v.p, v.pal
	rows := container.NewVBox()
	count := container.NewHBox()
	header := container.New(widgets.NewFluidRow(listWeights()...))
	viewBody := container.NewVBox(header, widget.NewSeparator(), rows)
	slide := shell.NewMorph(viewBody)
	slide.Duration = 600 * time.Millisecond
	var shown []Item
	mode := shell.NewUnderlineTabs([]string{"Issues", "Gantt"}, nil)
	mode.Selected = "Issues"
	if p.listGantt {
		mode.Selected = "Gantt"
	}
	var fill func()
	drawHeader := func() {
		header.Objects = nil
		for _, c := range listColumns {
			ink, sorted := pal.soft, c.name == p.sortBy
			if sorted {
				ink = widgets.LogoGold
			}
			label := caption(c.name, ink)
			label.TextStyle = fyne.TextStyle{Bold: sorted}
			cell := container.NewHBox(container.NewCenter(label))
			if sorted {
				// The arrow says which way: up for ascending, down for descending.
				arrow := widget.NewIcon(theme.MenuDropUpIcon())
				if p.sortDesc {
					arrow = widget.NewIcon(theme.MenuDropDownIcon())
				}
				cell.Add(container.NewThemeOverride(arrow, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold}))
			}
			column := c.name
			header.Add(shell.NewCardTappable(container.New(layout.NewCustomPaddedLayout(0, 0, theme.Padding(), 0), cell), func() {
				// A column sorts ascending, then descending, then back to Sprint order.
				switch {
				case p.sortBy != column:
					p.sortBy, p.sortDesc = column, false
				case !p.sortDesc:
					p.sortDesc = true
				default:
					p.sortBy, p.sortDesc = "Sprint", false
				}
				fill()
			}))
		}
		header.Refresh()
	}
	fill = func() {
		shown = shown[:0]
		for _, it := range v.s.Items {
			state := "Open"
			if it.Closed {
				state = "Done"
			}
			if it.NotPlanned {
				state = "Not planned"
			}
			if p.listState != "All" && state != p.listState {
				continue
			}
			if it.Matches(p.search) {
				shown = append(shown, it)
			}
		}
		SortIssues(shown, p.sortBy, p.sortDesc)
		drawHeader()
		rows.Objects = nil
		// Issues with a milestone stand under its name, the milestones in
		// the panel's order and those in none last; each keeps the sort.
		index := 0
		if p.listGantt && len(shown) > 0 {
			rows.Add(noteLabel("Sprint bars show the assigned sprint; other bars show opened to closed/today. Gold markers are milestone due dates. Arrows are GitHub blockers; unscheduled issues are labelled."))
			rows.Add(container.NewHScroll(NewGanttChart(shown[:min(len(shown), listShown)], v.now, pal, func(it Item) {
				p.showIssues(fmt.Sprintf("#%d %s", it.Number, it.Title), issueGroup{Name: "", Items: []Item{it}})
			})))
			header.Hide()
		} else {
			header.Show()
			for _, stage := range byMilestone(shown[:min(len(shown), listShown)]) {
				if stage.Name != "" {
					rows.Add(milestoneHeading(stage, pal))
				}
				for _, it := range stage.Items {
					rows.Add(v.listRow(it, index))
					index++
				}
			}
		}
		count.Objects = []fyne.CanvasObject{shell.CountBadge(len(shown))}
		switch {
		case len(shown) == 0:
			rows.Add(container.NewPadded(caption("No issues match.", pal.soft)))
		case len(shown) > listShown:
			count.Add(caption(fmt.Sprintf("Showing %d; search to narrow it", listShown), pal.soft))
		}
		count.Refresh()
		rows.Refresh()
		if p.scroll != nil {
			p.scroll.Refresh()
		}
	}

	search := shell.NewSearchEntry("Search issues, people, status, sprint…", p.search, func(text string) {
		p.search = text
		fill()
	})
	state := shell.NewGoldSelect([]string{"Open", "Done", "Not planned", "All"}, nil)
	state.Selected = p.listState
	state.OnChanged = func(choice string) {
		p.listState = choice
		fill()
	}
	mode.OnChanged = func(choice string) {
		gantt := choice == "Gantt"
		if gantt == p.listGantt {
			return
		}
		if p.listOpen {
			slide.Snapshot()
		}
		p.listGantt = gantt
		fill()
		direction := 1
		if !gantt {
			direction = -1
		}
		slide.Change(direction, func(final bool) {
			if p.scroll != nil {
				shell.Reflow(final, p.scroll, p.scroll.Content)
			}
		})
	}
	export := widget.NewButtonWithIcon("Export CSV", theme.DownloadIcon(), func() {
		p.export("list", shown)
	})
	fill()

	controls := container.NewBorder(nil, nil, nil,
		container.NewHBox(mode, shell.WideSelect(state), export), search)
	body := container.NewVBox(shell.VGap(theme.Padding()), controls,
		shell.VGap(theme.Padding()), slide)

	return p.foldSection("Issues", &p.listOpen, count, body)
}

// milestoneGroup is the issues of one milestone in the issue list.
type milestoneGroup struct {
	Name  string
	Due   time.Time
	Items []Item
}

// byMilestone splits issues by milestone, in the order the milestones panel
// lists them (by due date, those with none after) and those in no milestone
// last, each keeping its order. When none has a milestone they stay as one
// group with no name.
func byMilestone(items []Item) []milestoneGroup {
	index := map[string]int{}
	var out []milestoneGroup
	var none []Item
	for _, it := range items {
		if it.Milestone == "" {
			none = append(none, it)
			continue
		}
		i, ok := index[it.Milestone]
		if !ok {
			i = len(out)
			index[it.Milestone] = i
			out = append(out, milestoneGroup{Name: it.Milestone, Due: it.MilestoneDue})
		}
		out[i].Items = append(out[i].Items, it)
	}
	if len(out) == 0 {
		return []milestoneGroup{{Items: items}}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Due.IsZero() != b.Due.IsZero() {
			return b.Due.IsZero()
		}
		if !a.Due.Equal(b.Due) {
			return a.Due.Before(b.Due)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	if len(none) > 0 {
		out = append(out, milestoneGroup{Name: noMilestone, Items: none})
	}
	return out
}

// MilestoneGroups is the milestone headings the issue list groups items
// under, in order; a single "" when it lists them ungrouped.
func MilestoneGroups(items []Item) []string {
	var names []string
	for _, g := range byMilestone(items) {
		names = append(names, g.Name)
	}
	return names
}

// milestoneHeading names a milestone over its issues in the list: gold, with
// how many and when it is due.
func milestoneHeading(g milestoneGroup, pal palette) fyne.CanvasObject {
	name := caption(g.Name, widgets.LogoGold)
	name.TextSize = theme.TextSize() + 4
	name.TextStyle = fyne.TextStyle{Bold: true}
	title := container.NewBorder(nil, nil, nil, container.NewCenter(shell.CountBadge(len(g.Items))),
		container.New(&fitHeading{text: name, full: g.Name}, name))
	lines := container.NewVBox(title)
	if !g.Due.IsZero() {
		lines.Add(caption("Due "+g.Due.Format("2 Jan 2006"), pal.soft))
	}
	return container.New(layout.NewCustomPaddedLayout(theme.Padding()*2, theme.Padding()*3, theme.Padding(), theme.Padding()), lines)
}

// listRow is one issue in the list. Its assignee, Status, Priority and Sprint
// are drop-downs that change it on GitHub.
func (v *view) listRow(it Item, index int) fyne.CanvasObject {
	pal := v.pal
	// How long it has been open, or how long ago it was closed.
	age := age(v.now.Sub(it.Created))
	if it.Closed && !it.ClosedAt.IsZero() {
		age = "closed " + ago(v.now.Sub(it.ClosedAt))
	}
	link := fittedLink(fmt.Sprintf("#%d  %s", it.Number, it.Title), it.URL)
	row := container.New(widgets.NewFluidRow(listWeights()...),
		link, v.assigneeChoice(it), v.statusChoice(it), v.fieldChoice(it, "priority"), v.fieldChoice(it, "sprint"),
		cellText(cmpOr(it.Milestone, "–")), cellText(age))
	if index%2 == 1 {
		// Every other row is shaded a touch, so a wide row is easy to follow.
		band := canvas.NewRectangle(widgets.WithAlpha(pal.ink, 0.04))
		band.CornerRadius = 4
		return container.NewStack(band, row)
	}
	return row
}

// cellText is a list cell's plain text, cut short to its column.
func cellText(s string) fyne.CanvasObject {
	l := widget.NewLabel(s)
	l.Truncation = fyne.TextTruncateEllipsis
	return l
}

func cmpOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// people is everyone the issue can be given to: whoever its repo lets issues
// be assigned to on GitHub, and those holding any issue in the project. When
// the repo's list could not be read, the project's people alone.
func (v *view) people(it Item) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if key := strings.ToLower(name); name != "" && !seen[key] {
			seen[key] = true
			names = append(names, name)
		}
	}
	if cur := v.p.saved.Current(); cur != nil {
		for _, name := range v.p.snaps[cur.ID].Assignable[it.Repo] {
			add(name)
		}
	}
	for _, other := range v.s.Items {
		for _, a := range other.Assignees {
			add(a)
		}
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	return append(names, unassigned)
}

// listSelect is a list cell's drop-down: searchable, so a long list of
// people or sprints is found by typing.
func listSelect(options []string) *shell.GoldSelect {
	choice := shell.NewGoldSelect(options, nil)
	choice.Searchable = true
	return choice
}

// assigneeChoice is the issue's assignee as a drop-down. Choosing someone
// makes them its only assignee on GitHub.
func (v *view) assigneeChoice(it Item) fyne.CanvasObject {
	current := unassigned
	if len(it.Assignees) > 0 {
		current = it.Assignees[0]
	}
	options := v.people(it)
	if !slices.Contains(options, current) {
		options = append([]string{current}, options...)
	}
	choice := listSelect(options)
	choice.Selected = current
	if len(it.Assignees) > 1 {
		choice.PlaceHolder = strings.Join(it.Assignees, ", ")
	}
	choice.OnChanged = func(who string) {
		if who == current {
			return
		}
		target := who
		if who == unassigned {
			target = ""
		}
		v.p.change(choice, current, func() error { return SetAssignee(it, target) }, it.URL, func(item *Item) {
			item.Assignees = nil
			if target != "" {
				item.Assignees = []string{target}
			}
		})
	}
	return choice
}

// statusChoice is the issue's Status on its board as a drop-down, or a note
// for an issue on no board.
func (v *view) statusChoice(it Item) fyne.CanvasObject {
	if it.Board == nil || len(it.Board.Options) == 0 {
		return cellText("not on a board")
	}
	var options []string
	for _, o := range it.Board.Options {
		options = append(options, o.Name)
	}
	current := it.Status
	choice := listSelect(options)
	choice.Selected = current
	// Each Status in its Kanban column's colour, in the list and the field.
	choice.Hue = v.pal.statusHue
	if it.Status == "" {
		choice.Selected = ""
		choice.PlaceHolder = noStatus
	}
	choice.OnChanged = func(status string) {
		if status == it.Status {
			return
		}
		v.p.change(choice, current, func() error { return SetStatus(it, status) }, it.URL, func(item *Item) {
			item.Status = status
		})
	}
	if current == "" {
		return choice
	}
	return container.NewThemeOverride(choice, shell.TintTheme{Theme: theme.Current(), Hue: v.pal.statusHue(current)})
}

// statusHue is a Status's colour: the Kanban column's, or for a status the
// Kanban does not know, the colour of its kind on the status ring.
func (p palette) statusHue(name string) color.Color {
	if hue, ok := shell.StatusHue(name); ok {
		return hue
	}
	return p.stateColor(Item{Status: name})
}

// fieldChoice is the issue's Priority or Sprint ("priority" or "sprint") on
// its board as a drop-down, with None to clear it. When its board has no such
// field the value shows as text: a priority read from a label is changed on
// GitHub, not here.
func (v *view) fieldChoice(it Item, kind string) fyne.CanvasObject {
	current, field, choices := it.Priority, "", []Option(nil)
	if kind == "sprint" {
		current = it.Sprint
	}
	if it.Board != nil {
		field, choices = it.Board.PriorityField, it.Board.Priorities
		if kind == "sprint" {
			field, choices = it.Board.SprintField, it.Board.Sprints
		}
	}
	if field == "" {
		return cellText(cmpOr(current, "–"))
	}
	var options []string
	for _, o := range choices {
		options = append(options, o.Name)
	}
	// A sprint already over is no longer offered, but the issue is still in it.
	if current != "" && !slices.Contains(options, current) {
		options = append([]string{current}, options...)
	}
	options = append(options, noValue)
	choice := listSelect(options)
	choice.Selected = current
	if current == "" {
		choice.PlaceHolder = "–"
	}
	if kind == "sprint" {
		// The sprint running now is written in gold, as on the Kanban.
		for _, o := range choices {
			if !o.Start.IsZero() && !v.now.Before(o.Start) && v.now.Before(o.Start.AddDate(0, 0, max(o.Days, 1))) {
				choice.Current = o.Name
			}
		}
	}
	choice.OnChanged = func(name string) {
		target := name
		if name == noValue {
			target = ""
		}
		if target == current {
			return
		}
		v.p.change(choice, current, func() error { return SetField(it, kind, target) }, it.URL, func(item *Item) {
			if kind == "priority" {
				item.Priority = target
				return
			}
			item.Sprint, item.SprintStart, item.SprintDays = target, time.Time{}, 0
			for _, o := range choices {
				if o.Name == target {
					item.SprintStart, item.SprintDays = o.Start, o.Days
				}
			}
		})
	}
	return choice
}

// change saves a change made from the list to GitHub. The drop-down stays put
// while it saves; on success the page takes the change in, and on failure the
// drop-down goes back and says why.
func (p *page) change(choice *shell.GoldSelect, previous string, save func() error, url string, apply func(*Item)) {
	cur := p.saved.Current()
	if cur == nil {
		return
	}
	id := cur.ID
	choice.Disable()
	var err error
	p.ui.Async(func() error {
		err = save()
		return nil
	}, func() {
		choice.Enable()
		if err != nil {
			// Back to what GitHub still holds, without running the change again.
			handler := choice.OnChanged
			choice.OnChanged = nil
			choice.SetSelected(previous)
			choice.OnChanged = handler
			p.ui.Errf(err)
			return
		}
		if snap, ok := p.snaps[id]; ok {
			snap.Patch(url, apply)
			if current := p.saved.Current(); current != nil && current.ID == id {
				p.draw()
			}
		}
	})
}
