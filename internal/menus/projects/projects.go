package projects

import (
	"fmt"
	"image/color"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// The Projects tab follows work that is bigger than a sprint. A project is a
// name and the parent issues (epics) pasted into it; the page reads their
// child issues from GitHub and charts how far along they are, who holds them
// and when they are likely to be finished. Its only writes to GitHub are from
// the issue list (see edit.go); the projects themselves are kept in
// data\projects.json.
//
// The page is one screen: a column on the left with the status ring, who
// holds the work and the parent issues, and beside it the headline tiles and
// the charts sharing the height between them.

// page is the tab's state for one window.
type page struct {
	ui      *shell.UI
	saved   Saved
	loadErr error

	tabRow *fyne.Container
	body   *fyne.Container
	root   *fyne.Container
	// note says when the open project was fetched, at the end of the tabs.
	note *fyne.Container
	// showProgress drives the loading strip under the tabs.
	showProgress func(bool, backend.FetchProgress) bool

	snaps    map[string]Snapshot
	loading  map[string]bool
	progress map[string]backend.FetchProgress
	// fetches counts a project's fetches, so one that lands after the project
	// was edited or fetched again is dropped.
	fetches map[string]int

	// menu is the open project's drop-down while it is showing.
	menu *widget.PopUp
	// weeks is the week each project's week panel shows, by project ID.
	weeks map[string]time.Time
	// months is the month the arrows on "Finished since start" moved to, by
	// project ID. Without one the chart shows the month of the week panel's
	// week.
	months map[string]time.Time
	// allWeeks marks the projects whose finished report shows every week
	// since the start rather than a page of months. It lasts until the tab
	// is left or another project picked; then the Settings range is back.
	allWeeks map[string]bool
	// search and listState narrow the issue list: words to find, and Open,
	// Done, Not planned or All.
	search, listState string
	// sortByDone orders the workload chart by issues finished rather than
	// issues assigned.
	sortByDone bool
	// listOpen is whether the issue list is unfolded; it starts folded, so
	// the charts fill the window with only the list's heading under them.
	listOpen  bool
	listGantt bool
	// sortBy is the list column the issues are sorted by ("" for none, the
	// newest first), and sortDesc whether the order is reversed.
	sortBy   string
	sortDesc bool
	// toggled is the parent issue whose tick was just clicked, while the page
	// is drawn again for it.
	toggled string
	// ticks are the parent issues' tick boxes, by project and parent, kept
	// from one drawing of the page to the next so a tick just clicked goes
	// on drawing itself while the figures round it change.
	ticks map[string]*widgets.TickCheck
	// scroll is the page as last drawn, for project scrolled, so a redraw
	// can keep its place.
	scroll   *container.Scroll
	scrolled string
}

var pages = map[*shell.UI]*page{}

// Now is the clock the page's figures are worked out against. A variable so
// the tests can hold it still.
var Now = time.Now

const (
	// arrivalSlot is the page's arrival; issuesSlot the issue popup's.
	arrivalSlot = "projects"
	issuesSlot  = "project-issues"
	// The week panel and the finished report arrive again on their own when
	// what they show changes: another week, another page of months, or all.
	weekSlot   = "project-week"
	reportSlot = "project-report"
	// popupSettle holds a popup's bubbles back until it has risen into place.
	popupSettle = 160 * time.Millisecond
)

// BuildProjectsTab builds the tab. The first project's issues are fetched
// when the tab is first opened, not here.
func BuildProjectsTab(ui *shell.UI) fyne.CanvasObject {
	p := &page{
		ui:    ui,
		snaps: map[string]Snapshot{}, loading: map[string]bool{},
		progress: map[string]backend.FetchProgress{}, fetches: map[string]int{},
		weeks: map[string]time.Time{}, months: map[string]time.Time{}, allWeeks: map[string]bool{}, listState: "Open",
		ticks: map[string]*widgets.TickCheck{}, sortBy: "Sprint",
	}
	pages[ui] = p
	p.saved, p.loadErr = Load(ui.Store)

	indicator := shell.NewLoadingIndicator()
	p.showProgress = indicator.ShowProgress
	p.tabRow = container.NewStack()
	p.note = container.NewStack()
	p.body = container.NewStack()
	p.root = container.NewBorder(container.NewVBox(p.tabRow, indicator.View), nil, nil, nil, p.body)
	p.buildTabs()
	p.draw()
	return p.root
}

// Open runs when the tab is selected: the open project arrives again, and is
// fetched if this is the first time it is shown.
func Open(ui *shell.UI) {
	if p := pages[ui]; p != nil {
		ui.ReplayArrival(arrivalSlot)
		ui.ReplayArrival(weekSlot)
		ui.ReplayArrival(reportSlot)
		clear(p.allWeeks)
		p.draw()
		p.load(false)
	}
}

// buildTabs lays the projects out as Log Work lays out its kinds, with a +
// at the end that adds one.
func (p *page) buildTabs() {
	options := make([]string, 0, len(p.saved.Projects)+1)
	for _, project := range p.saved.Projects {
		options = append(options, project.Name)
	}
	options = append(options, AddTabLabel)
	tabs := shell.NewUnderlineTabs(options, func(name string) {
		for _, project := range p.saved.Projects {
			if project.Name == name && project.ID != p.saved.Selected {
				p.saved.Selected = project.ID
				clear(p.allWeeks)
				p.save()
				p.draw()
				p.load(false)
				return
			}
		}
	})
	tabs.Action = AddTabLabel
	tabs.OnAction = func() { p.openEditor(nil) }
	// The open project's tab carries an arrow; clicking it drops the menu.
	tabs.OnMenu = func(_ string, anchor fyne.CanvasObject) { p.openMenu(anchor) }
	// Dragging a tab along the row reorders the projects, and the order is
	// kept in projects.json.
	tabs.OnReorder = func(order []string) {
		p.saved.Projects = Reordered(p.saved.Projects, order)
		p.save()
		p.buildTabs()
	}
	if cur := p.saved.Current(); cur != nil {
		p.saved.Selected = cur.ID
		tabs.Selected = cur.Name
	}
	// A long list of projects scrolls sideways rather than squeezing the names.
	p.tabRow.Objects = []fyne.CanvasObject{container.NewBorder(nil, nil, nil, p.note, container.NewHScroll(tabs))}
	p.setNote(nil)
	p.tabRow.Refresh()
}

func (p *page) save() {
	if err := Save(p.ui.Store, p.saved); err != nil {
		p.ui.Errf(err)
	}
}

// load fetches the open project's issues, unless they are already here or on
// their way. force fetches them again.
func (p *page) load(force bool) {
	cur := p.saved.Current()
	if cur == nil || len(cur.Issues) == 0 || p.loading[cur.ID] {
		return
	}
	if _, have := p.snaps[cur.ID]; have && !force {
		return
	}
	id, refs := cur.ID, slices.Clone(cur.Issues)
	p.fetches[id]++
	fetch := p.fetches[id]
	current := func() bool { return p.fetches[id] == fetch && p.saved.Find(id) != nil }
	p.loading[id] = true
	p.progress[id] = backend.FetchProgress{Stage: "Reading child issues"}
	p.draw()

	var snap Snapshot
	p.ui.Async(func() error {
		snap = Fetch(refs, func(state backend.FetchProgress) {
			fyne.Do(func() {
				if current() {
					p.progress[id] = state
					p.syncProgress()
				}
			})
		})
		return nil
	}, func() {
		if !current() {
			return
		}
		p.loading[id] = false
		p.snaps[id] = snap
		p.record(id, snap)
		p.draw()
	})
}

// syncProgress shows the loading strip while the open project is fetching.
func (p *page) syncProgress() {
	cur := p.saved.Current()
	active := cur != nil && p.loading[cur.ID]
	var state backend.FetchProgress
	if active {
		state = p.progress[cur.ID]
	}
	if p.showProgress(active, state) {
		p.root.Refresh()
	}
}

// draw rebuilds the page for the open project.
func (p *page) draw() {
	p.closeMenu()
	cur := p.saved.Current()
	p.syncProgress()
	p.setNote(nil)

	var content fyne.CanvasObject
	switch {
	case p.loadErr != nil:
		content = message("The saved projects could not be read.", p.loadErr.Error(), nil)
	case cur == nil:
		add := widget.NewButtonWithIcon("Add a project", theme.ContentAddIcon(), func() { p.openEditor(nil) })
		content = message("No projects yet",
			"A project follows the child issues of one or more parent issues, such as an epic. "+
				"Add one, paste its issues, and this page charts how far along they are.", add)
	default:
		snap, have := p.snaps[cur.ID]
		switch {
		case have:
			p.setNote(&snap)
			content = p.dashboard(*cur, snap)
		case p.loading[cur.ID]:
			content = message("Reading "+cur.Name+" from GitHub…", "", nil)
		default:
			content = message("Nothing fetched yet",
				"Click the arrow beside the project's name, up in its tab, and pick Refresh from GitHub.", nil)
		}
	}
	p.body.Objects = []fyne.CanvasObject{content}
	p.body.Refresh()
}

// setNote says, at the end of the row of tabs, when the open project was
// fetched.
func (p *page) setNote(snap *Snapshot) {
	p.note.Objects = nil
	if snap != nil {
		fetched := caption("Fetched "+snap.Fetched.Format("2 Jan 15:04"), currentPalette().soft)
		p.note.Objects = []fyne.CanvasObject{container.New(layout.NewCustomPaddedLayout(0, 0, theme.Padding()*2, theme.Padding()*2),
			container.NewCenter(fetched))}
	}
	// The row of tabs gives the note its width, so it lays out again.
	for _, row := range p.tabRow.Objects {
		row.Refresh()
	}
}

// record keeps how many issues were in review and in progress this week, so
// the week panel can show them for the weeks gone by. GitHub keeps no such
// history: what the app has seen at each refresh is all there is.
func (p *page) record(id string, snap Snapshot) {
	project := p.saved.Find(id)
	if project == nil {
		return
	}
	counts := StateCounts(project.Counted(snap).Items())
	key := WeekStart(Now()).Format("2006-01-02")
	if have, ok := project.History[key]; ok && have == counts {
		return
	}
	if project.History == nil {
		project.History = map[string]WeekCounts{}
	}
	project.History[key] = counts
	// Two years is plenty; the oldest weeks go first.
	if len(project.History) > maxWeeks {
		keys := slices.Sorted(maps.Keys(project.History))
		for _, old := range keys[:len(keys)-maxWeeks] {
			delete(project.History, old)
		}
	}
	p.save()
}

// message is a page with nothing to chart: a heading, a line under it, and
// perhaps a button.
func message(title, detail string, action fyne.CanvasObject) fyne.CanvasObject {
	rows := []fyne.CanvasObject{widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})}
	if detail != "" {
		text := widget.NewLabelWithStyle(detail, fyne.TextAlignCenter, fyne.TextStyle{})
		text.Wrapping = fyne.TextWrapWord
		rows = append(rows, text)
	}
	if action != nil {
		rows = append(rows, container.NewCenter(action))
	}
	column := container.NewVBox(rows...)
	return container.NewBorder(shell.VGap(48), nil, nil, nil,
		container.NewGridWithColumns(3, layout.NewSpacer(), container.NewVBox(column), layout.NewSpacer()))
}

// ---- the project's menu ----

func (p *page) closeMenu() {
	if p.menu != nil {
		menu := p.menu
		p.menu = nil
		if r, ok := menu.Content.(*shell.Reveal); ok {
			shell.FoldPopUp(menu, r)
		}
	}
}

// openMenu drops the project's menu from under its tab: Edit, Refresh, Export
// and Delete, unfolding as the app's other drop-downs do.
func (p *page) openMenu(anchor fyne.CanvasObject) {
	if p.menu != nil {
		p.closeMenu()
		return
	}
	cur := p.saved.Current()
	if cur == nil || p.ui.Win == nil {
		return
	}
	shell.FinishDropdownClosings()
	fill := theme.Color(theme.ColorNameOverlayBackground)
	row := func(text string, icon fyne.Resource, ink color.Color, run func()) fyne.CanvasObject {
		label := canvas.NewText(text, ink)
		bg := canvas.NewRectangle(fill)
		bg.CornerRadius = 5
		line := container.New(layout.NewCustomPaddedHBoxLayout(theme.Padding()*2), widget.NewIcon(icon), label)
		return shell.NewCardTappable(container.NewStack(bg, container.NewPadded(line)), func() {
			p.closeMenu()
			run()
		})
	}
	ink := theme.Color(theme.ColorNameForeground)
	id := cur.ID
	rows := container.NewVBox(
		row("Edit project", theme.DocumentCreateIcon(), ink, func() {
			if project := p.saved.Find(id); project != nil {
				p.openEditor(project)
			}
		}),
		row("Refresh from GitHub", theme.ViewRefreshIcon(), ink, func() { p.load(true) }),
		row("Export issues to CSV", theme.DownloadIcon(), ink, func() {
			if snap, ok := p.snaps[id]; ok {
				p.export("all", snap.Items())
			} else {
				shell.ShowInformation("Nothing to export", "Refresh from GitHub first.", p.ui.Win)
			}
		}),
		widget.NewSeparator(),
		row("Delete project", theme.NewErrorThemedResource(theme.DeleteIcon()), theme.Color(theme.ColorNameError), p.confirmDelete),
	)
	body := container.NewPadded(rows)
	unfold := shell.NewReveal(body)
	unfold.Fold = true
	unfold.OnDismiss = p.closeMenu
	window := p.ui.Win.Canvas()
	p.menu = widget.NewPopUp(unfold, window)
	size := body.MinSize()
	size.Width = max(size.Width+theme.Padding()*4, 230)
	p.menu.Resize(size)
	origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(anchor)
	p.menu.ShowAtPosition(origin.Add(fyne.NewPos(0, anchor.Size().Height+2)))
	shell.UnfoldPopUp(p.menu, unfold, size)
}

// ---- the dashboard ----

const (
	tileMinWidth   = 118
	tileMaxWidth   = 460
	panelMinWidth  = float32(290)
	panelMinHeight = float32(210)
	// The left-hand column takes sideShare of the width, within these bounds.
	sideShare    = float32(0.28)
	sideMinWidth = float32(360)
	sideMaxWidth = float32(480)
	legendNameW  = float32(130)
	// keyNameW is how wide a name may be in a chart's legend.
	keyNameW = float32(80)
	// workloadFloor is the least height the workload keeps when the parent
	// issues below it grow: its heading, key and a couple of people.
	workloadFloor = float32(150)
)

// view is one drawing of a fetched project: its figures, colours and arrival.
type view struct {
	p   *page
	cur Project
	// all is the project as fetched, every parent issue in it; s is worked
	// out from the parent issues the project counts.
	all    Snapshot
	s      Summary
	pal    palette
	now    time.Time
	arrive *shell.Arrival
	// fetched stamps the figures, so fresh ones arrive anew.
	fetched int64
}

// arrival is slot's arrival for what the panel shows now: a new one, played
// from the start, whenever what changes.
func (v *view) arrival(slot, what string) *shell.Arrival {
	return v.p.ui.Arrival(slot, fmt.Sprintf("%s@%d|%s", v.cur.ID, v.fetched, what))
}

// dashboard is the charts for one fetched project, filling the page. On a
// window too short for it the whole page scrolls instead.
func (p *page) dashboard(cur Project, snap Snapshot) fyne.CanvasObject {
	v := &view{p: p, cur: cur, all: snap, s: Summarize(cur.Counted(snap), Now()), pal: currentPalette(), now: Now()}
	if v.s.Total+v.s.NotPlanned == 0 {
		title, detail := "No child issues", "None of this project's issues has child issues on GitHub yet."
		if len(cur.Excluded) > 0 && len(snap.Items()) > 0 {
			title, detail = "Every parent issue is left out", "Tick a parent issue below to count its child issues."
		}
		return container.NewBorder(message(title, detail, nil), nil, nil, nil, v.parentsPanel())
	}
	// A project arrives when it is first shown, when its tab is opened and when
	// fresh figures land; a redraw in between (a new sort order) does not.
	v.fetched = snap.Fetched.UnixNano()
	v.arrive = p.ui.Arrival(arrivalSlot, fmt.Sprintf("%s@%d", cur.ID, v.fetched))
	right := container.New(&gridLayout{}, v.tiles(),
		v.burnUp(), v.sinceStart(), v.week(), v.sprints(), v.priorities(), v.milestonesPanel())
	return p.withSections(cur.ID, container.New(dashLayout{}, v.side(), right), v.issueList())
}

// dashLayout stands the left-hand column beside the rest, at its share of the
// width.
type dashLayout struct{}

func pageGap() float32 { return theme.Padding() * 2 }

func (dashLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	side, right := objects[0].MinSize(), objects[1].MinSize()
	return fyne.NewSize(right.Width+pageGap()+sideMinWidth, max(right.Height, side.Height))
}

func (dashLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	width := min(max(size.Width*sideShare, sideMinWidth), sideMaxWidth)
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(width, size.Height))
	objects[1].Move(fyne.NewPos(width+pageGap(), 0))
	objects[1].Resize(fyne.NewSize(max(size.Width-width-pageGap(), 0), size.Height))
}

// gridLayout puts the tiles across the top at their own height, then the
// panels under them, given in this order: burn-up, finished report, week, by
// sprint, by priority and milestones. On a wide page burn-up and milestones
// share the first row with the finished report, which stands two rows tall at
// the right with the week under it; by sprint and by priority stack in two
// rows two columns wide. A narrower page drops to two columns, the finished
// report still two rows tall above the week, then to one. A row's height is
// its weight's share of what is left.
//
// cols is remembered from the last Layout, as a FlowGrid's rows are, because
// MinSize is asked for before anything knows the width.
type gridLayout struct{ cols int }

// cell is where one panel sits: its first column and row, and how many of
// each it covers.
type cell struct{ col, row, cols, rows int }

// place says where each panel sits at cols columns, and how much of the height
// each row takes.
func (l *gridLayout) place(cols int) ([]cell, []float32) {
	switch cols {
	case 3:
		return []cell{{0, 0, 1, 1}, {2, 0, 1, 2}, {2, 2, 1, 1}, {0, 1, 2, 1}, {0, 2, 2, 1}, {1, 0, 1, 1}},
			[]float32{1.1, 1, 1}
	case 2:
		return []cell{{0, 0, 1, 1}, {1, 0, 1, 2}, {1, 2, 1, 1}, {0, 3, 2, 1}, {0, 2, 1, 1}, {0, 1, 1, 1}},
			[]float32{1.1, 1.2, 1, 1}
	}
	return []cell{{0, 0, 1, 1}, {0, 1, 1, 1}, {0, 2, 1, 1}, {0, 3, 1, 1}, {0, 4, 1, 1}, {0, 5, 1, 1}},
		[]float32{1.1, 2, 1.1, 1, 1, 1.4}
}

func (l *gridLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	cols := l.cols
	if cols == 0 {
		cols = 3
	}
	cells, weights := l.place(cols)
	height := objects[0].MinSize().Height
	for _, floor := range rowFloors(cells, weights, objects[1:]) {
		height += pageGap() + floor
	}
	return fyne.NewSize(panelMinWidth, height)
}

// rowFloors is the least each row may be: its weight's share of the panels'
// least height, or more if a panel standing in that row alone needs more. A
// row squeezed under what its panel needs cut the panel's chart short, and a
// sideways scroll then drew its bar across the columns.
func rowFloors(cells []cell, weights []float32, panels []fyne.CanvasObject) []float32 {
	floors := make([]float32, len(weights))
	for r, w := range weights {
		floors[r] = panelMinHeight * w
	}
	for i, c := range cells {
		if i < len(panels) && c.rows == 1 {
			floors[c.row] = max(floors[c.row], panels[i].MinSize().Height)
		}
	}
	return floors
}

func (l *gridLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	gap := pageGap()
	tiles, panels := objects[0], objects[1:]
	// The tiles wrap by width, so their height is known only once they have it.
	tiles.Move(fyne.NewPos(0, 0))
	tiles.Resize(fyne.NewSize(size.Width, tiles.MinSize().Height))
	top := tiles.MinSize().Height
	tiles.Resize(fyne.NewSize(size.Width, top))

	cols := 3
	for cols > 1 && size.Width < panelMinWidth*float32(cols)+gap*float32(cols-1) {
		cols--
	}
	l.cols = cols
	cells, weights := l.place(cols)
	total := float32(0)
	for _, w := range weights {
		total += w
	}
	left := size.Height - top - gap*float32(len(weights))
	floors := rowFloors(cells, weights, panels)
	// Each row's top and height.
	ys, heights := make([]float32, len(weights)), make([]float32, len(weights))
	y := top + gap
	for r, w := range weights {
		ys[r], heights[r] = y, max(left*w/total, floors[r])
		y += heights[r] + gap
	}
	width := (size.Width - gap*float32(cols-1)) / float32(cols)
	for i, c := range cells {
		if i >= len(panels) {
			break
		}
		h := gap * float32(c.rows-1)
		for r := c.row; r < c.row+c.rows; r++ {
			h += heights[r]
		}
		panels[i].Move(fyne.NewPos(float32(c.col)*(width+gap), ys[c.row]))
		panels[i].Resize(fyne.NewSize(width*float32(c.cols)+gap*float32(c.cols-1), h))
	}
}

// tiles are the headline figures across the top. Each is a bubble in a colour
// of its own. Those with a list behind them open it.
func (v *view) tiles() fyne.CanvasObject {
	s, pal := v.s, v.pal
	var figures []*canvas.Text
	tile := func(name, figure, note string, accent color.Color, counts bool, open func(), beside ...fyne.CanvasObject) fyne.CanvasObject {
		big := canvas.NewText(figure, pal.ink)
		big.TextSize = theme.TextSize() + 8
		big.TextStyle = fyne.TextStyle{Bold: true}
		if counts {
			figures = append(figures, big)
		}
		var line fyne.CanvasObject = big
		if len(beside) > 0 {
			// Whatever stands beside the figure takes the rest of the line.
			line = container.NewBorder(nil, nil, big, nil,
				container.New(layout.NewCustomPaddedLayout(0, 0, theme.Padding()*2, 0), beside[0]))
		}
		bubble := shell.NewStripeBubble(accent, container.NewVBox(caption(name, pal.soft), line, tileNote(note)))
		if open == nil {
			return bubble
		}
		return shell.NewBubbleTappable(bubble, open)
	}
	forecast, forecastNote := "No forecast", "nothing finished lately"
	switch {
	case s.Open == 0:
		forecast, forecastNote = "Complete", "nothing left open"
	case !s.Finish.IsZero():
		// The year goes under the date, so the figure fits a narrow tile.
		forecast, forecastNote = s.Finish.Format("2 Jan"), s.Finish.Format("2006")+"  ·  current pace"
	}
	weekAgo := v.now.AddDate(0, 0, -7)
	paceFrom := v.now.AddDate(0, 0, -7*velocityWeeks)
	gold := color.Color(widgets.LogoGold)
	pace := s.Credit.Pace
	if len(pace) > sparkWeeks {
		pace = pace[len(pace)-sparkWeeks:]
	}
	tiles := []fyne.CanvasObject{
		tile("Progress", fmt.Sprintf("%d of %d", s.Done, s.Total), "issues done", pal.done, true, func() {
			v.p.showIssues("In scope", issueGroup{Name: "Done", Items: v.finishedSince(time.Time{})}, issueGroup{Name: "Open", Items: v.open()})
		}),
		tile("Open", fmt.Sprint(s.Open), fmt.Sprintf("%d with nobody assigned", s.UnassignedOpen), gold, true, func() {
			v.p.showIssues("Open", issueGroup{Name: "", Items: v.open()})
		}),
		tile("Last 7 Days", fmt.Sprintf("%d Done", s.ClosedLast7), fmt.Sprintf("%d opened", s.OpenedLast7), pal.review, true, func() {
			v.p.showIssues("Last 7 Days", issueGroup{Name: "Done", Items: v.finishedSince(weekAgo)},
				issueGroup{Name: "Opened", Items: s.Where(func(it Item) bool { return it.Created.After(weekAgo) })})
		}),
		tile("Pace", fmt.Sprintf("%.1f / wk", s.Velocity), paceTrend(s), pal.backlog, true, func() {
			v.p.showIssues(fmt.Sprintf("Finished in the last %d weeks", velocityWeeks), issueGroup{Name: "", Items: v.finishedSince(paceFrom)})
		}),
		// The pace line beside the forecast says whether that date is coming
		// nearer or slipping. The tile opens the forecast's workings.
		tile("Forecast", forecast, forecastNote, pal.todo, false, v.p.showForecast(s, v.now),
			Sparkline(pace, pal.ink, pal)),
	}
	tiles = v.arrive.Reveal(tiles, shell.ArriveDelays(len(tiles)), nil)
	v.arrive.CountUp(figures)
	return container.New(widgets.NewFlowGrid(tileMinWidth, tileMaxWidth, 0), tiles...)
}

// sparkWeeks is how many weeks of pace the Forecast tile's line covers.
const sparkWeeks = 12

// paceTrend says how the pace compares with the four weeks before it.
func paceTrend(s Summary) string {
	if !s.HasPrev {
		return fmt.Sprintf("finished a week, last %d", velocityWeeks)
	}
	change := s.Velocity - s.PrevVelocity
	switch {
	case change >= 0.05:
		return fmt.Sprintf("%.1f faster than the %d weeks before", change, velocityWeeks)
	case change <= -0.05:
		return fmt.Sprintf("%.1f slower than the %d weeks before", -change, velocityWeeks)
	}
	return fmt.Sprintf("same as the %d weeks before", velocityWeeks)
}

// tileNote is the small line under a tile's figure, cut short with an
// ellipsis when the tile is narrow rather than running out of it.
func tileNote(text string) fyne.CanvasObject {
	note := widget.NewLabel(text)
	note.SizeName = theme.SizeNameCaptionText
	note.Truncation = fyne.TextTruncateEllipsis
	tight := layout.NewCustomPaddedLayout(-theme.InnerPadding(), -theme.InnerPadding(), -theme.InnerPadding(), 0)
	return container.New(tight, container.NewThemeOverride(note,
		widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: currentPalette().soft}))
}

// open is the open issues, the longest waiting first.
func (v *view) open() []Item {
	items := v.s.Where(func(it Item) bool { return !it.Closed })
	sort.SliceStable(items, func(i, j int) bool { return items[i].Created.Before(items[j].Created) })
	return items
}

// finishedSince is the issues finished after t, the latest first.
func (v *view) finishedSince(t time.Time) []Item {
	items := v.s.Where(func(it Item) bool { return it.Closed && it.ClosedAt.After(t) })
	sort.SliceStable(items, func(i, j int) bool { return items[i].ClosedAt.After(items[j].ClosedAt) })
	return items
}

// panelHeading is a panel's gold name, with perhaps a control at its far end.
func panelHeading(title string, action fyne.CanvasObject) fyne.CanvasObject {
	heading := canvas.NewText(titleCase(title), widgets.LogoGold)
	heading.TextSize = theme.TextSize() + 2
	heading.TextStyle = fyne.TextStyle{Bold: true}
	var end fyne.CanvasObject
	if action != nil {
		end = container.NewCenter(action)
	}
	// On a narrow panel the name gives way to the control, cut short with an
	// ellipsis, rather than running under it.
	name := container.New(&fitHeading{text: heading, full: heading.Text}, heading)
	return container.NewBorder(nil, nil, nil, end, name)
}

// fitHeading shows a heading whole, or cut short to the width it is given.
type fitHeading struct {
	text *canvas.Text
	full string
}

func (l *fitHeading) MinSize([]fyne.CanvasObject) fyne.Size {
	ellipsis := fyne.MeasureText("…", l.text.TextSize, l.text.TextStyle)
	return fyne.NewSize(ellipsis.Width, fyne.MeasureText(l.full, l.text.TextSize, l.text.TextStyle).Height)
}

func (l *fitHeading) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	if shown := shell.FitText(l.full, l.text.TextSize, l.text.TextStyle, size.Width); shown != l.text.Text {
		l.text.Text = shown
		l.text.Refresh()
	}
	m := l.text.MinSize()
	l.text.Move(fyne.NewPos(0, (size.Height-m.Height)/2))
	l.text.Resize(m)
}

// countedHeading is a panel's heading with a count in a badge after its name.
func countedHeading(title string, count int, action fyne.CanvasObject) fyne.CanvasObject {
	heading := canvas.NewText(titleCase(title), widgets.LogoGold)
	heading.TextSize = theme.TextSize() + 2
	heading.TextStyle = fyne.TextStyle{Bold: true}
	var end fyne.CanvasObject
	if action != nil {
		end = container.NewCenter(action)
	}
	name := container.New(layout.NewCustomPaddedHBoxLayout(theme.Padding()*2),
		container.NewCenter(heading), container.NewCenter(shell.CountBadge(count)))
	return container.NewBorder(nil, nil, name, end)
}

// titleCase gives each word of a heading a capital first letter: "Workload
// By Person". Words that start with a figure are left as they are.
func titleCase(s string) string {
	words := strings.Split(s, " ")
	for i, w := range words {
		r := []rune(w)
		if len(r) > 0 && unicode.IsLower(r[0]) {
			r[0] = unicode.ToUpper(r[0])
			words[i] = string(r)
		}
	}
	return strings.Join(words, " ")
}

// noteLabel is a small line under a chart that wraps rather than running out
// of its panel.
func noteLabel(text string) fyne.CanvasObject {
	note := widget.NewLabel(text)
	note.SizeName = theme.SizeNameCaptionText
	note.Wrapping = fyne.TextWrapWord
	return container.NewThemeOverride(note, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: currentPalette().soft})
}

// chartPanel is a panel whose middle takes all the height it is given: its
// name, perhaps a legend under it, the chart or list, and perhaps a note.
func chartPanel(title string, key, middle fyne.CanvasObject, note string) fyne.CanvasObject {
	return actionPanel(title, nil, key, middle, note)
}

// actionPanel is chartPanel with a control at the right of the heading. The
// chart stands clear of the heading above it and of the panel's sides.
func actionPanel(title string, action, key, middle fyne.CanvasObject, note string) fyne.CanvasObject {
	pad := theme.Padding()
	top := container.NewVBox(panelHeading(title, action))
	if key != nil {
		top.Add(key)
	}
	top.Add(shell.VGap(pad * 2))
	var foot fyne.CanvasObject
	if note != "" {
		foot = container.NewVBox(shell.VGap(pad), noteLabel(note))
	}
	body := container.New(layout.NewCustomPaddedLayout(0, 0, pad, pad), middle)
	return shell.Panel(container.NewBorder(top, foot, nil, nil, body))
}

// listScroll scrolls a panel's rows inside it, clear of the scroll bar.
func listScroll(rows fyne.CanvasObject) fyne.CanvasObject {
	scroll := container.NewVScroll(container.New(layout.NewCustomPaddedLayout(0, 0, 0, 12), rows))
	scroll.SetMinSize(fyne.NewSize(0, 60))
	return scroll
}

func share(n, of int) string {
	if of == 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", int(float64(n)/float64(of)*100+0.5))
}

// issuesIn splits items into those still open and those done, for a popup.
func issuesIn(items []Item) []issueGroup {
	var open, done []Item
	for _, it := range items {
		if it.Closed {
			done = append(done, it)
		} else {
			open = append(open, it)
		}
	}
	return []issueGroup{{Name: "Open", Items: open}, {Name: "Done", Items: done}}
}

// byAssignee splits each group's issues by who holds them in the popup.
func byAssignee(groups []issueGroup) []issueGroup {
	for i := range groups {
		groups[i].ByPerson = true
	}
	return groups
}

// heldBy splits issues by their first assignee, A to Z, with those held by
// nobody last as Unassigned; each person's issues newest first.
func heldBy(items []Item) []issueGroup {
	sorted := slices.Clone(items)
	SortIssues(sorted, "Assignee", false)
	var out []issueGroup
	for _, it := range sorted {
		who := unassigned
		if len(it.Assignees) > 0 {
			who = it.Assignees[0]
		}
		if len(out) == 0 || out[len(out)-1].Name != who {
			out = append(out, issueGroup{Name: who})
		}
		out[len(out)-1].Items = append(out[len(out)-1].Items, it)
	}
	return out
}

// ---- the left-hand column ----

// side is the left-hand column: the status ring, who holds the work, and the
// parent issues, with a rule between each. The workload list takes what
// height is left, and scrolls.
func (v *view) side() fyne.CanvasObject {
	s, pal := v.s, v.pal

	// Status: the pie of what state the child issues are in. A state's line in
	// the key opens its issues.
	colors := pal.sliceColors(s.Statuses)
	all := s.Total + s.NotPlanned
	key := container.New(layout.NewCustomPaddedVBoxLayout(0))
	for i, slice := range s.Statuses {
		name := shell.FitText(slice.Label, theme.TextSize(), fyne.TextStyle{}, legendNameW)
		entry := legendEntry(pal, colors[i], name, fmt.Sprintf("%d  ·  %s", slice.Count, share(slice.Count, all)))
		key.Add(shell.NewCardTappable(container.New(layout.NewCustomPaddedLayout(2, 2, 6, 6), entry), func() {
			v.p.showIssues(slice.Label, issueGroup{Name: "", Items: slice.Items})
		}))
	}
	donut := Donut(s.Statuses, colors, fmt.Sprintf("%d%%", s.RingPercent()), "Done", pal, v.arrive)
	status := container.NewVBox(panelHeading("Status", nil), shell.VGap(theme.Padding()*2),
		container.NewBorder(nil, nil, donut, nil, container.NewCenter(key)))
	// The key's lines uncover one after another as the ring sweeps round.
	v.arrive.RevealIn(key)

	// Workload: a bar a person, finished and still open. A line opens what
	// that person holds.
	people := slices.Clone(s.People)
	sort.SliceStable(people, func(i, j int) bool {
		a, b := people[i], people[j]
		if v.p.sortByDone && a.Done != b.Done {
			return a.Done > b.Done
		}
		if a.Total() != b.Total() {
			return a.Total() > b.Total()
		}
		return a.Done > b.Done
	})
	var rows []BarRow
	add := func(person PersonLoad, holder string) {
		rows = append(rows, BarRow{
			Label:    person.Name,
			Segments: []Segment{{person.Done, pal.done}, {person.Open, pal.progress}},
			Figure:   fmt.Sprintf("%d of %d", person.Done, person.Total()),
			OnTap:    func() { v.p.showIssues(person.Name, issuesIn(s.Held(holder))...) },
		})
	}
	for _, person := range people {
		add(person, person.Name)
	}
	if s.Unassigned.Total() > 0 {
		add(s.Unassigned, "")
	}
	order := "Most assigned first"
	if v.p.sortByDone {
		order = "Most finished first"
	}
	tag := shell.NewGoldTag(order, func() {
		v.p.sortByDone = !v.p.sortByDone
		v.p.draw()
	})
	tag.Wide = true // its whole text, never cut short
	var list fyne.CanvasObject = caption("Nobody is assigned to these issues.", pal.soft)
	if len(rows) > 0 {
		list = listScroll(BarTable(rows, pal, v.arrive))
	}
	// How many people hold work, in a badge beside the heading.
	workload := container.NewBorder(container.NewVBox(
		countedHeading("Workload by person", len(s.People), tag),
		legend(legendEntry(pal, pal.done, "Done", ""), legendEntry(pal, pal.progress, "Open", "")),
		shell.VGap(theme.Padding()),
	), nil, nil, nil, list)

	parents, natural := v.parents()
	rule := func() fyne.CanvasObject {
		return container.New(layout.NewCustomPaddedLayout(theme.Padding()*2, theme.Padding()*2, 0, 0), widget.NewSeparator())
	}
	column := container.New(&sideLayout{parents: natural}, status, rule(), workload, rule(), parents)
	return shell.Panel(column)
}

// sideLayout stacks the left-hand column: the status at its own height, the
// parent issues at theirs as far as the workload above them can give way, and
// the workload in between taking the rest. A list of eight or so parents shows
// whole; only a longer one, or a short window, scrolls.
type sideLayout struct{ parents func() float32 }

func (l *sideLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	width, height := float32(0), float32(0)
	for _, o := range objects {
		m := o.MinSize()
		width = max(width, m.Width)
		height += m.Height
	}
	return fyne.NewSize(width, height+120) // room for a few people
}

func (l *sideLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	status, rule1, workload, rule2, parents := objects[0], objects[1], objects[2], objects[3], objects[4]
	fixed := status.MinSize().Height + rule1.MinSize().Height + rule2.MinSize().Height
	room := size.Height - fixed - max(workload.MinSize().Height, workloadFloor)
	parentsHeight := max(min(l.parents(), room), parents.MinSize().Height)
	y := float32(0)
	place := func(o fyne.CanvasObject, h float32) {
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, h))
		y += h
	}
	place(status, status.MinSize().Height)
	place(rule1, rule1.MinSize().Height)
	rest := size.Height - y - rule2.MinSize().Height - parentsHeight
	place(workload, max(rest, workload.MinSize().Height))
	place(rule2, rule2.MinSize().Height)
	place(parents, parentsHeight)
}

// parents is each pasted issue with how much of it is done, or why it could
// not be read, as a compact list. The tick at its left counts it in the
// figures or leaves it out. It returns the list and how tall it would be with
// every row showing.
func (v *view) parents() (fyne.CanvasObject, func() float32) {
	pal := v.pal
	pad := theme.Padding()
	list := newHoverList(pad)
	// Each row's tick and bar, which are all that move as the page arrives;
	// a parent that could not be read has no bar.
	var ticks []*widgets.TickCheck
	var bars []*bar
	var refs []string
	// A link's own padding is trimmed, so a row is two tight lines.
	tight := func(o fyne.CanvasObject) fyne.CanvasObject {
		in := -theme.InnerPadding() / 2
		return container.New(layout.NewCustomPaddedLayout(in, in, 0, 0), o)
	}
	counted := 0
	for _, parent := range v.all.Parents {
		epic, counts := Epic(parent), v.cur.Counts(parent.Ref)
		if counts {
			counted++
		}
		// The title alone; the ref is only for a parent whose title is unknown.
		name := parent.Title
		if name == "" {
			name = parent.Ref
		}
		// A link that keeps its row lit while the pointer is on it.
		link := shell.NewHoverLink(name, issueLink(parent))
		if h, ok := link.(*shell.HoverLink); ok {
			h.SizeName = theme.SizeNameCaptionText
			h.Truncation = fyne.TextTruncateEllipsis
		}
		var body fyne.CanvasObject
		var rowBar *bar
		if parent.Err != "" {
			problem := widget.NewLabel(parent.Err)
			problem.SizeName = theme.SizeNameCaptionText
			problem.Wrapping = fyne.TextWrapWord
			body = container.New(layout.NewCustomPaddedVBoxLayout(0), tight(link),
				tight(container.NewThemeOverride(problem, errorInk{theme.Current()})))
		} else {
			ink, fill := pal.ink, pal.done
			if !counts {
				ink, fill = pal.soft, pal.dropped
			}
			figure := caption(fmt.Sprintf("%d of %d  ·  %s", epic.Done, epic.Total, share(epic.Done, epic.Total)), ink)
			figure.TextStyle = fyne.TextStyle{Bold: true}
			b := newBar([]Segment{{epic.Done, fill}}, epic.Total, pal.grid).thin(6)
			rowBar = &b
			body = container.New(layout.NewCustomPaddedVBoxLayout(0), tight(link),
				container.NewBorder(nil, nil, nil, container.NewCenter(figure), b.box))
		}
		ref, id := parent.Ref, v.cur.ID
		key := id + "|" + strings.ToLower(ref)
		tick := v.p.ticks[key]
		if tick == nil {
			p := v.p
			tick = widgets.NewTickCheck(counts, widgets.LogoGold, func(on bool) { p.countParent(id, ref, on) })
			v.p.ticks[key] = tick
		}
		tick.SetChecked(counts)
		// The bar of the parent just ticked or unticked grows again in its
		// new colour, in time with the tick.
		if rowBar != nil && strings.EqualFold(ref, v.p.toggled) {
			growAlone(*rowBar)
		}
		gold := container.NewThemeOverride(tick, goldTick{theme.Current()})
		ticks, bars, refs = append(ticks, tick), append(bars, rowBar), append(refs, ref)
		list.add(container.NewBorder(nil, nil, container.NewCenter(gold), nil, body), nil)
	}
	// As the page arrives only the ticks and the bars move: one after
	// another, each tick drawing itself in as its bar grows, over
	// parentMotion. The box just clicked is left to its own motion.
	toggled := v.p.toggled
	v.arrive.CascadeFor(parentMotion, shell.ArriveDelays(len(ticks)), func(i int, share float32) {
		if strings.EqualFold(refs[i], toggled) {
			return
		}
		if ticks[i].Checked() {
			ticks[i].ShowTick(share)
		}
		if bars[i] != nil {
			bars[i].grow(share)
		}
	})
	rows := list.Rows
	var count fyne.CanvasObject
	if counted < len(v.all.Parents) {
		count = caption(fmt.Sprintf("%d of %d counted", counted, len(v.all.Parents)), pal.soft)
	}
	head := container.NewVBox(panelHeading("Parent issues", count), shell.VGap(pad))
	panel := container.NewBorder(head, nil, nil, nil, listScroll(list))
	return panel, func() float32 { return head.MinSize().Height + rows.MinSize().Height + pad }
}

// parentMotion is how long a parent issue's tick and bar take to arrive.
const parentMotion = 600 * time.Millisecond

// countParent counts a parent issue's child issues in the project's figures,
// or leaves them out, and draws the page again from what was fetched.
func (p *page) countParent(id, ref string, on bool) {
	project := p.saved.Find(id)
	if project == nil || project.Counts(ref) == on {
		return
	}
	project.SetCounts(ref, on)
	p.save()
	// The figures change in place; only the box just clicked, drawing its
	// tick, and its parent's bar move.
	p.toggled = ref
	p.draw()
	p.toggled = ""
}

// growAlone grows one bar from nothing on its own, in time with a tick.
func growAlone(b bar) {
	if !fyne.CurrentApp().Settings().ShowAnimations() {
		return
	}
	b.grow(0)
	anim := fyne.NewAnimation(widgets.TickTime, func(done float32) { b.grow(done) })
	anim.Curve = fyne.AnimationEaseInOut
	anim.Start()
}

// goldTick paints a check box in the app's gold: its outline, its tick and
// the wash under the pointer.
type goldTick struct{ fyne.Theme }

func (t goldTick) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary, theme.ColorNameInputBorder:
		return widgets.LogoGold
	case theme.ColorNameHover, theme.ColorNameFocus:
		return widgets.WithAlpha(widgets.LogoGold, 0.18)
	}
	return t.Theme.Color(name, v)
}

// parentsPanel is the parent issues on their own, for a project with no child
// issues to chart.
func (v *view) parentsPanel() fyne.CanvasObject {
	list, _ := v.parents()
	return shell.Panel(list)
}

// ---- the charts ----

// burnUp is scope against finished work over time, and where the two are
// heading.
func (v *view) burnUp() fyne.CanvasObject {
	s, pal := v.s, v.pal
	if len(s.Weeks) == 0 {
		return chartPanel("Burn-up", nil, caption("No dated issues to plot.", pal.soft), "")
	}
	projection := s.ProjectionWeeks(v.now)
	key := legend(
		legendEntry(pal, pal.review, "All issues", fmt.Sprint(s.Total)),
		legendEntry(pal, pal.done, "Done", fmt.Sprint(s.Done)))
	if projection > 0 {
		key.Add(legendEntry(pal, faded(pal.done, projectedAlpha), "Projected", ""))
	}
	// A burn-up chart counts up: the work done climbs toward the work there
	// is, and the project is finished where the two lines meet.
	note := "Done climbs toward all issues; they meet when finished."
	if s.Open == 0 {
		note = "Every issue in scope is done: the lines have met."
	}
	return chartPanel("Burn-up", key, BurnUp(s.Weeks, projection, pal, v.arrive), note)
}

// weekIndex is the week picked for the week panel: the one last picked, or
// this one.
func (v *view) weekIndex() int {
	weeks := v.s.Weeks
	if picked, ok := v.p.weeks[v.cur.ID]; ok {
		for i, w := range weeks {
			if w.Start.Equal(picked) {
				return i
			}
		}
	}
	return len(weeks) - 1
}

// pickWeek shows week i in the week panel.
func (v *view) pickWeek(i int) {
	if i >= 0 && i < len(v.s.Weeks) {
		v.p.weeks[v.cur.ID] = v.s.Weeks[i].Start
		// The columns follow the week to its month.
		delete(v.p.months, v.cur.ID)
		v.p.draw()
	}
}

// monthOf is the first day of t's month.
func monthOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

// creditMonths lists the months the weeks start in, oldest first, with the
// first and one past the last week of each.
func creditMonths(weeks []time.Time) (months []time.Time, bounds [][2]int) {
	for i, w := range weeks {
		m := monthOf(w)
		if n := len(months); n > 0 && months[n-1].Equal(m) {
			bounds[n-1][1] = i + 1
			continue
		}
		months = append(months, m)
		bounds = append(bounds, [2]int{i, i + 1})
	}
	return months, bounds
}

// reportPages splits n months into pages of per months each, counted back from
// the newest, so the latest page is always full and the oldest may be short.
// Each page is the first and one past the last month in it, oldest first.
func reportPages(n, per int) [][2]int {
	var pages [][2]int
	for to := n; to > 0; to -= per {
		pages = append([][2]int{{max(to-per, 0), to}}, pages...)
	}
	return pages
}

// monthsLabel names the months from first to last: "Nov 2026", or
// "Sep – Nov 2026", or "Dec 2025 – Feb 2026" across a new year.
func monthsLabel(first, last time.Time) string {
	switch {
	case first.Equal(last):
		return last.Format("Jan 2006")
	case first.Year() == last.Year():
		return first.Format("Jan") + " – " + last.Format("Jan 2006")
	}
	return first.Format("Jan 2006") + " – " + last.Format("Jan 2006")
}

// sinceStart is the finished report: how many issues were finished each week
// since the first, by whom, with the four-week pace drawn across the columns.
// A column opens its week in the week panel.
//
// It shows a page of months at a time, as many as Settings asks (one unless
// told otherwise), with arrows at the top right to step through them: a
// project's whole run as columns squeezed them past reading on a small screen.
// It opens on the page of the week panel's week, this month's unless another
// week was picked. Clicking the months between the arrows shows every week
// since the start squeezed into the panel, as the report first was, the first
// week and this one named at the ends; clicking again comes back to this month.
func (v *view) sinceStart() fyne.CanvasObject {
	c, pal := v.s.Credit, v.pal
	if len(c.Weeks) == 0 {
		return chartPanel(finishedReport, nil, caption("No dated issues to plot.", pal.soft), "")
	}
	colors := pal.personColors(c.Names)
	key := container.New(&widgets.WrapRowLayout{})
	for i, name := range c.Names {
		key.Add(legendEntry(pal, colors[i], shell.FitText(name, theme.TextSize(), fyne.TextStyle{}, keyNameW), ""))
	}
	key.Add(paceKey(pal))

	marked := v.weekIndex()
	last := len(c.Weeks) - 1
	months, bounds := creditMonths(c.Weeks)
	pages := reportPages(len(months), backend.FinishedReportMonths(v.p.ui.Cfg))
	want := monthOf(c.Weeks[min(marked, last)])
	if m, ok := v.p.months[v.cur.ID]; ok {
		want = m
	}
	pi := len(pages) - 1
	for i, pg := range pages {
		for m := pg[0]; m < pg[1]; m++ {
			if months[m].Equal(want) {
				pi = i
			}
		}
	}
	all := v.p.allWeeks[v.cur.ID]
	from, to := bounds[pages[pi][0]][0], bounds[pages[pi][1]-1][1]
	if all {
		from, to = 0, len(c.Weeks)
	}

	spec := ColumnSpec{Marked: -1, OnTap: func(i int) { v.pickWeek(from + i) }, Line: c.Pace[from:to]}
	if marked >= from && marked < to {
		spec.Marked = marked - from
	}
	ends := func(w int) string {
		if w == last {
			return "This week"
		}
		return shortDate(c.Weeks[w])
	}
	// A month or two has few enough weeks to name each one; more, and only
	// the ends are named.
	if !all && to-from <= 9 {
		for w := from; w < to; w++ {
			spec.Labels = append(spec.Labels, ends(w))
		}
	} else {
		spec.Ends = [2]string{ends(from), ends(to - 1)}
	}
	peak, totals := 0, make([]int, to-from)
	for w, counts := range c.Counts[from:to] {
		var stack []Segment
		for k, n := range counts {
			stack = append(stack, Segment{n, colors[k]})
			totals[w] += n
		}
		spec.Stacks = append(spec.Stacks, stack)
		if totals[w] > totals[peak] {
			peak = w
		}
	}
	// Only the busiest week shown and this one carry their figure.
	spec.Figures = make([]string, len(totals))
	figured := []int{peak}
	if to-1 == last {
		figured = append(figured, len(totals)-1)
	}
	for _, w := range figured {
		if totals[w] > 0 {
			spec.Figures[w] = fmt.Sprint(totals[w])
		}
	}

	pick := func(i int) {
		v.p.months[v.cur.ID] = months[pages[i][1]-1]
		v.p.draw()
	}
	label := monthsLabel(months[pages[pi][0]], months[pages[pi][1]-1])
	if all {
		label = "All"
	}
	// Wide: a gold tag that hugs its text otherwise asks for no more room
	// than an ellipsis, and between the arrows it got no more.
	toggle := shell.NewGoldTag(label, func() {
		if v.p.allWeeks[v.cur.ID] {
			delete(v.p.allWeeks, v.cur.ID)
			v.p.months[v.cur.ID] = months[len(months)-1]
		} else {
			v.p.allWeeks[v.cur.ID] = true
		}
		v.p.draw()
	})
	toggle.Wide = true
	walk := stepperWith(toggle, !all && pi > 0, !all && pi < len(pages)-1,
		func() { pick(pi - 1) }, func() { pick(pi + 1) })

	// Stepping to another page, or to all of them, grows the columns again.
	chart := NewColumnChart(spec, pal, v.arrival(reportSlot, fmt.Sprint(from, "-", to)))
	note := "By first assignee. Click a week to open it."
	return actionPanel(finishedReport, walk, key, chart, note)
}

// finishedReport is the name of the panel of issues finished week by week.
const finishedReport = "Finished report"

// stepper walks through weeks or months: its label in gold between gold
// arrows, each arrow there only while there is somewhere to go.
func stepper(label string, canBack, canNext bool, onBack, onNext func()) fyne.CanvasObject {
	return stepperWith(boldText(label, widgets.LogoGold), canBack, canNext, onBack, onNext)
}

// stepperWith is a stepper with something of its own between the arrows.
func stepperWith(middle fyne.CanvasObject, canBack, canNext bool, onBack, onNext func()) fyne.CanvasObject {
	back := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), onBack)
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), onNext)
	if !canBack {
		back.Disable()
	}
	if !canNext {
		next.Disable()
	}
	row := container.NewHBox(shell.QuietButton(back), container.NewCenter(middle), shell.QuietButton(next))
	return container.NewThemeOverride(row, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
}

// paceKey is the legend entry for the pace line: a short line and a dot.
func paceKey(pal palette) fyne.CanvasObject {
	mark := canvas.NewRectangle(pal.ink)
	mark.SetMinSize(fyne.NewSize(14, plotLineWidth))
	return container.New(layout.NewCustomPaddedHBoxLayout(6), container.NewCenter(mark),
		canvas.NewText(fmt.Sprintf("%d-week pace", velocityWeeks), pal.soft))
}

// week is one week up close: how many issues were done, in review and in
// progress, how many were opened, and who finished them. The arrows walk
// from week to week.
func (v *view) week() fyne.CanvasObject {
	s, pal := v.s, v.pal
	if len(s.Weeks) == 0 {
		return chartPanel("Week", nil, caption("No dated issues to plot.", pal.soft), "")
	}
	i := v.weekIndex()
	start := s.Weeks[i].Start
	end := start.AddDate(0, 0, 7)
	thisWeek := i == len(s.Weeks)-1
	within := func(t time.Time) bool { return !t.Before(start) && t.Before(end) }

	done := s.Where(func(it Item) bool { return it.Closed && within(it.ClosedAt) })
	opened := s.Where(func(it Item) bool { return within(it.Created) })
	inClass := func(class int) []Item {
		return s.Where(func(it Item) bool { return !it.Closed && statusClass(it.Status) == class })
	}

	// In review and in progress are known for now, and for a past week only as
	// a refresh that week saw them.
	review, progress := "–", "–"
	var reviewItems, progressItems []Item
	note := ""
	switch counts, recorded := v.cur.History[start.Format("2006-01-02")]; {
	case thisWeek:
		reviewItems, progressItems = inClass(ClassReview), inClass(ClassProgress)
		review, progress = fmt.Sprint(len(reviewItems)), fmt.Sprint(len(progressItems))
	case recorded:
		review, progress = fmt.Sprint(counts.Review), fmt.Sprint(counts.Progress)
		note = "In review and in progress as the last refresh that week saw them."
	default:
		note = "In review and in progress are kept from each refresh, and none was made this week."
	}

	var figures []*canvas.Text
	stat := func(figure, name string, ink color.Color, items []Item, title string) fyne.CanvasObject {
		big := canvas.NewText(figure, pal.ink)
		big.TextSize = theme.TextSize() + 7
		big.TextStyle = fyne.TextStyle{Bold: true}
		figures = append(figures, big)
		cell := container.NewVBox(big, container.New(layout.NewCustomPaddedHBoxLayout(5), swatch(ink), caption(name, pal.soft)))
		padded := container.New(layout.NewCustomPaddedLayout(2, 2, 6, 6), cell)
		if items == nil {
			return padded
		}
		return shell.NewCardTappable(padded, func() { v.p.showIssues(title, issueGroup{Name: "", Items: items}) })
	}
	label := weekLabel(start, thisWeek)
	// Another week, picked here or on the finished report, counts its figures
	// up again and grows the bars of who finished them. The bubbles stay put.
	arrive := v.arrival(weekSlot, start.Format("2006-01-02"))
	stats := container.NewGridWithColumns(4,
		stat(fmt.Sprint(len(done)), "Done", pal.done, done, "Done, "+label),
		stat(review, "Review", pal.review, reviewItems, "In review"),
		stat(progress, "Progress", pal.progress, progressItems, "In progress"),
		stat(fmt.Sprint(len(opened)), "Opened", pal.neutral, opened, "Opened, "+label),
	)
	arrive.CountUp(figures)

	// Who finished them, by first assignee.
	byWho := map[string][]Item{}
	for _, it := range done {
		who := unassignedAs
		if len(it.Assignees) > 0 {
			who = it.Assignees[0]
		}
		byWho[who] = append(byWho[who], it)
	}
	names := slices.Collect(maps.Keys(byWho))
	sort.Slice(names, func(a, b int) bool {
		if len(byWho[names[a]]) != len(byWho[names[b]]) {
			return len(byWho[names[a]]) > len(byWho[names[b]])
		}
		return strings.ToLower(names[a]) < strings.ToLower(names[b])
	})
	var rows []BarRow
	for _, name := range names {
		items := byWho[name]
		rows = append(rows, BarRow{Label: name, Segments: []Segment{{len(items), pal.done}},
			Figure: fmt.Sprint(len(items)),
			OnTap:  func() { v.p.showIssues(name+", "+label, issueGroup{Name: "", Items: items}) }})
	}
	var who fyne.CanvasObject = caption("Nothing was finished this week.", pal.soft)
	if len(rows) > 0 {
		who = listScroll(BarTable(rows, pal, arrive))
	}

	walk := stepper(label, i > 0, !thisWeek, func() { v.pickWeek(i - 1) }, func() { v.pickWeek(i + 1) })
	middle := container.NewBorder(container.NewVBox(stats, shell.VGap(theme.Padding()*2),
		caption("Finished by", pal.soft)), nil, nil, nil, who)
	return actionPanel("Week", walk, nil, middle, note)
}

// weekLabel names a week: "This week", or its first and last days.
func weekLabel(start time.Time, thisWeek bool) string {
	if thisWeek {
		return "This week"
	}
	last := start.AddDate(0, 0, 6)
	if start.Month() == last.Month() {
		return fmt.Sprintf("%d – %s", start.Day(), last.Format("2 Jan"))
	}
	return start.Format("2 Jan") + " – " + last.Format("2 Jan")
}

// buckets is a column chart of the scope split one way, a column a sprint or
// a priority: finished at the bottom, still open above. A column opens its
// issues.
func (v *view) buckets(title, none string, list []Bucket, marked int, note string) fyne.CanvasObject {
	pal := v.pal
	if len(list) == 0 {
		return chartPanel(title, nil, noteLabel(none), "")
	}
	spec := ColumnSpec{Marked: marked}
	// "Sprint 35", "Sprint 36"… under narrow columns all cut to "Sprint …":
	// a first word every named column shares is said by the heading already.
	shared, same := "", true
	for _, b := range list {
		if b.Name == noSprint || b.Name == noPriority {
			continue
		}
		first, _, ok := strings.Cut(b.Name, " ")
		if !ok || (shared != "" && shared != first) {
			same = false
		}
		shared = first
	}
	for _, b := range list {
		spec.Stacks = append(spec.Stacks, []Segment{{b.Done, pal.done}, {b.Open, pal.progress}})
		spec.Figures = append(spec.Figures, fmt.Sprintf("%d/%d", b.Done, b.Total()))
		label := b.Name
		if same && shared != "" && b.Name != noSprint && b.Name != noPriority {
			label = strings.TrimPrefix(b.Name, shared+" ")
		}
		spec.Labels = append(spec.Labels, label)
	}
	spec.OnTap = func(i int) { v.p.showIssues(title+": "+list[i].Name, byAssignee(issuesIn(list[i].Items))...) }
	key := legend(legendEntry(pal, pal.done, "Done", ""), legendEntry(pal, pal.progress, "Open", ""))
	// Every sprint or priority with an issue in it is shown; when there are
	// more than fit, the chart scrolls sideways.
	spec.Scrolls = true
	return chartPanel(title, key, container.NewHScroll(NewColumnChart(spec, pal, v.arrive)), note)
}

// sprints splits the scope by the board's Sprint: the latest sprints, the one
// running now washed gold, then the issues in no sprint.
func (v *view) sprints() fyne.CanvasObject {
	list := v.s.Sprints
	var named []Bucket
	var none []Bucket
	for _, b := range list {
		if b.Name == noSprint {
			none = append(none, b)
		} else {
			named = append(named, b)
		}
	}
	note := ""
	list = append(named, none...)
	marked := slices.IndexFunc(list, func(b Bucket) bool { return b.Current })
	if marked >= 0 {
		note = strings.TrimSpace(note + " Gold: the sprint running now.")
	}
	return v.buckets("By sprint", "No board gives these issues a Sprint.", list, marked, note)
}

// priorities splits the scope by the board's Priority, or priority labels.
func (v *view) priorities() fyne.CanvasObject {
	return v.buckets("By priority", "No board Priority or priority label (P1, priority: high) on these issues.",
		v.s.Priorities, -1, "")
}

// fittedLink is a link cut short with an ellipsis at whatever width it is
// given, so a long title never runs under what stands beside it.
func fittedLink(label, url string) fyne.CanvasObject {
	link := shell.GithubHyperlink(label, url)
	if h, ok := link.(*widget.Hyperlink); ok {
		h.Truncation = fyne.TextTruncateEllipsis
	}
	return link
}

// errorInk paints a label in the theme's error colour.
type errorInk struct{ fyne.Theme }

func (t errorInk) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground {
		return t.Theme.Color(theme.ColorNameError, v)
	}
	return t.Theme.Color(name, v)
}

func issueLink(parent Parent) string {
	if parent.URL != "" {
		return parent.URL
	}
	owner, repo, num, err := backend.SplitIssue(parent.Ref)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, num)
}

// age is how long ago, in the largest unit that fits.
func age(d time.Duration) string {
	days := int(d.Hours() / 24)
	switch {
	case days < 1:
		return "today"
	case days < 14:
		return fmt.Sprintf("%d d", days)
	case days < 70:
		return fmt.Sprintf("%d wk", days/7)
	}
	return fmt.Sprintf("%d mo", days/30)
}

func ago(d time.Duration) string {
	if text := age(d); text != "today" {
		return text + " ago"
	}
	return "today"
}

// ---- the issues behind a figure ----

// issueGroup is one block of an issue popup. A group with no name is the
// whole list and gets no heading.
type issueGroup struct {
	Name  string
	Items []Item
	// ByPerson splits the group under a small heading per assignee.
	ByPerson bool
}

const (
	issueMinWidth = 300
	issueMaxWidth = 520
)

// showIssues opens a popup of the issues behind a figure, a bubble each,
// arriving one after another once the popup is in place.
func (p *page) showIssues(title string, groups ...issueGroup) {
	win := p.ui.Win
	if win == nil {
		return
	}
	pal, now := currentPalette(), Now()
	total := 0
	for _, g := range groups {
		total += len(g.Items)
	}
	p.ui.ReplayArrival(issuesSlot)
	arrive := p.ui.Arrival(issuesSlot, title).After(popupSettle)
	delays := shell.ArriveDelays(total)

	rows := container.NewVBox()
	at := 0
	for _, g := range groups {
		if len(g.Items) == 0 {
			continue
		}
		if g.Name != "" {
			rows.Add(panelHeading(fmt.Sprintf("%s  ·  %d", g.Name, len(g.Items)), nil))
		}
		blocks := []issueGroup{{Items: g.Items}}
		if g.ByPerson {
			blocks = heldBy(g.Items)
		}
		for _, b := range blocks {
			if b.Name != "" {
				who := caption(fmt.Sprintf("%s  ·  %d", b.Name, len(b.Items)), pal.ink)
				who.TextStyle = fyne.TextStyle{Bold: true}
				rows.Add(container.New(layout.NewCustomPaddedLayout(theme.Padding(), 0, theme.Padding(), 0), who))
			}
			bubbles := make([]fyne.CanvasObject, len(b.Items))
			for i, it := range b.Items {
				bubbles[i] = p.issueBubble(it, now, pal)
			}
			bubbles = arrive.Reveal(bubbles, delays[at:at+len(b.Items)], nil)
			at += len(b.Items)
			rows.Add(container.New(widgets.NewFlowGrid(issueMinWidth, issueMaxWidth, 0), bubbles...))
		}
		rows.Add(shell.VGap(theme.Padding()))
	}
	if total == 0 {
		rows.Add(widget.NewLabel("No issues here."))
	}

	var d dialog.Dialog
	done := widget.NewButton("Close", func() { shell.CloseDialog(d, win) })
	export := widget.NewButtonWithIcon("Export CSV", theme.DownloadIcon(), func() {
		var all []Item
		for _, g := range groups {
			all = append(all, g.Items...)
		}
		p.export(title, all)
	})
	if total == 0 {
		export.Disable()
	}
	count := fmt.Sprintf("%d issues", total)
	if total == 1 {
		count = "1 issue"
	}
	content := container.NewBorder(nil, shell.DialogButtons(done, export), nil, nil,
		container.NewVScroll(container.New(layout.NewCustomPaddedLayout(0, 0, 0, 12), rows)))
	d = dialog.NewCustomWithoutButtons(p.ui.PopupTitle(title+"  ·  "+count), content, win)
	d.Resize(p.ui.PopupSize())
	shell.ShowDialog(d, win)
}

// export saves issues as a CSV in the exports folder and says where.
func (p *page) export(list string, items []Item) {
	name := "project"
	if cur := p.saved.Current(); cur != nil {
		name = cur.Name
	}
	path, err := ExportIssues(shell.ExportsDir(), name, list, items, Now())
	if err != nil {
		p.ui.Errf(err)
		return
	}
	shell.ShowInformation("Exported", fmt.Sprintf("Saved %d issues to:\n%s", len(items), path), p.ui.Win)
}

// ---- the forecast and its calculator ----

// showForecast returns what the Forecast tile opens: how the date was worked
// out, how it moves at other paces, and a calculator for a date or a pace of
// your own.
func (p *page) showForecast(s Summary, now time.Time) func() {
	return func() {
		win := p.ui.Win
		if win == nil {
			return
		}
		finish := func(t time.Time) string {
			if t.IsZero() {
				return "never"
			}
			return t.Format("Mon 2 Jan 2006")
		}
		perWeek := func(pace float64) string { return fmt.Sprintf("%.1f a week", pace) }

		intro := fmt.Sprintf("%d issues are open. At %s (issues finished over the last %.0f weeks, divided by %.0f), "+
			"they run out on %s.", s.Open, perWeek(s.Velocity), s.PaceSpan, s.PaceSpan, finish(s.Finish))
		if s.Open == 0 {
			intro = "Every issue in scope is done."
		}

		// What if: the same open issues at other paces the project has had.
		table := container.New(layout.NewFormLayout())
		for _, sc := range s.Scenarios(now) {
			table.Add(widget.NewLabel(sc.Name))
			table.Add(widget.NewLabelWithStyle(perWeek(sc.Pace)+"   ·   "+finish(sc.Finish), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
		}

		// New issues keep arriving: counting them, the gap closes only at the
		// pace finished beyond the pace opened.
		growth := fmt.Sprintf("New issues joined at %s over the same weeks. ", perWeek(s.OpenedPace))
		net := s.Velocity - s.OpenedPace
		switch {
		case s.Open == 0:
			growth = ""
		case net <= 0:
			growth += "Counting them, the open list is not shrinking: issues arrive as fast as they are finished."
		default:
			growth += fmt.Sprintf("Counting them, the open list shrinks by %s, and runs out on %s.",
				perWeek(net), finish(FinishAt(s.Open, net, now)))
		}

		// The calculator: pick a date for the pace it needs, or type a pace for
		// the date it reaches.
		needed := widget.NewLabel("")
		needed.Wrapping = fyne.TextWrapWord
		by := widget.NewDateEntry()
		by.OnChanged = func(t *time.Time) {
			if t == nil {
				needed.SetText("")
				return
			}
			pace := s.NeededPace(*t, now)
			switch {
			case s.Open == 0:
				needed.SetText("Nothing is left to finish.")
			case pace == 0:
				needed.SetText("Pick a date after today.")
			case pace <= s.Velocity:
				needed.SetText(fmt.Sprintf("Needs %s. The current pace (%s) gets there.", perWeek(pace), perWeek(s.Velocity)))
			default:
				needed.SetText(fmt.Sprintf("Needs %s: %.1f a week more than the current pace.", perWeek(pace), pace-s.Velocity))
			}
		}
		reaches := widget.NewLabel("")
		reaches.Wrapping = fyne.TextWrapWord
		pace := widget.NewEntry()
		pace.SetPlaceHolder("issues a week, e.g. 3")
		pace.OnChanged = func(text string) {
			var value float64
			if _, err := fmt.Sscan(strings.TrimSpace(text), &value); err != nil || value <= 0 {
				reaches.SetText("")
				return
			}
			reaches.SetText("Finishes on " + finish(FinishAt(s.Open, value, now)) + ".")
		}

		wrap := func(text string) fyne.CanvasObject {
			l := widget.NewLabel(text)
			l.Wrapping = fyne.TextWrapWord
			return l
		}
		body := container.NewVBox(
			wrap(intro),
			panelHeading("At other paces", nil), table,
		)
		if growth != "" {
			body.Add(wrap(growth))
		}
		body.Add(panelHeading("Calculator", nil))
		body.Add(container.New(layout.NewFormLayout(), widget.NewLabel("Finish by"), by, layout.NewSpacer(), needed,
			widget.NewLabel("At a pace of"), pace, layout.NewSpacer(), reaches))

		var d dialog.Dialog
		done := widget.NewButton("Close", func() { shell.CloseDialog(d, win) })
		d = dialog.NewCustomWithoutButtons("Forecast", container.NewBorder(nil, shell.DialogButtons(done), nil, nil,
			container.NewVScroll(body)), win)
		// As tall as its contents, with room for the answers to come.
		size := p.ui.PopupSize()
		d.Resize(fyne.NewSize(min(size.Width, 720), min(size.Height, body.MinSize().Height+170)))
		shell.ShowDialog(d, win)
	}
}

// issueBubble is one issue as a card in its organisation's colour: its title,
// linked, then its state and repo, who holds it, and how long it has run.
func (p *page) issueBubble(it Item, now time.Time, pal palette) fyne.CanvasObject {
	org, repo, _ := strings.Cut(it.Repo, "/")
	state := "Open"
	switch {
	case it.NotPlanned:
		state = "Not planned"
	case it.Closed:
		state = "Done"
	case it.Status != "":
		state = it.Status
	}
	who := "unassigned"
	if len(it.Assignees) > 0 {
		who = shell.Truncate(strings.Join(it.Assignees, ", "), 40)
	}
	when := "opened " + ago(now.Sub(it.Created))
	if it.Closed && !it.ClosedAt.IsZero() {
		when += "  ·  closed " + ago(now.Sub(it.ClosedAt))
	}
	tags := container.NewHBox(shell.OutlineTag(shell.Truncate(state, 28), pal.stateColor(it)))
	if it.Priority != "" {
		tags.Add(shell.OutlineTag(shell.Truncate(it.Priority, 16), pal.soft))
	}
	place := repo
	if it.Sprint != "" {
		place += "  ·  " + it.Sprint
	}
	tags.Add(container.NewCenter(caption(place, pal.soft)))
	body := container.New(layout.NewCustomPaddedVBoxLayout(2),
		fittedLink(fmt.Sprintf("#%d  %s", it.Number, it.Title), it.URL),
		tags,
		caption(who, pal.soft),
		caption(when, pal.soft),
	)
	return shell.HoverBubble(shell.OrgColor(p.ui.Cfg, org), body)
}

// ---- adding, editing and deleting ----

// openEditor is the popup that adds a project (existing nil) or changes one.
func (p *page) openEditor(existing *Project) {
	win := p.ui.Win
	title, id := "New project", ""
	name := widget.NewEntry()
	name.SetPlaceHolder("Checkout redesign")
	issues := widget.NewMultiLineEntry()
	issues.SetPlaceHolder("https://github.com/owner/repo/issues/123\nowner/repo#456")
	issues.Wrapping = fyne.TextWrapWord
	issues.SetMinRowsVisible(editorRows)
	if existing != nil {
		title, id = "Edit project", existing.ID
		name.SetText(existing.Name)
		issues.SetText(strings.Join(existing.Issues, "\n"))
	}
	problem := container.NewStack()
	fail := func(text string) {
		problem.Objects = []fyne.CanvasObject{shell.ColorLabel(text, theme.ColorNameError)}
		problem.Refresh()
	}

	var d dialog.Dialog
	cancel := widget.NewButton("Cancel", func() { shell.CloseDialog(d, win) })
	save := widget.NewButton("Save", func() {
		if text := p.saved.CheckName(name.Text, id); text != "" {
			fail(text)
			return
		}
		refs, unread := ParseIssues(issues.Text)
		switch {
		case len(unread) > 0:
			fail("Not an issue link: " + shell.Truncate(strings.Join(unread, ", "), 70))
			return
		case len(refs) == 0:
			fail("Paste at least one issue.")
			return
		}
		project := p.saved.Find(id)
		if project == nil {
			p.saved.Projects = append(p.saved.Projects, Project{ID: newID()})
			project = &p.saved.Projects[len(p.saved.Projects)-1]
		}
		if !slices.Equal(project.Issues, refs) {
			// What was fetched no longer matches; a fetch on its way is dropped.
			delete(p.snaps, project.ID)
			p.loading[project.ID] = false
			p.fetches[project.ID]++
		}
		project.Name, project.Issues = strings.TrimSpace(name.Text), refs
		// A parent issue no longer pasted is no longer left out either.
		project.Excluded = slices.DeleteFunc(project.Excluded, func(out string) bool {
			return !slices.ContainsFunc(refs, func(ref string) bool { return strings.EqualFold(ref, out) })
		})
		if len(project.Excluded) == 0 {
			project.Excluded = nil
		}
		p.saved.Selected = project.ID
		p.save()
		shell.CloseDialog(d, win)
		p.buildTabs()
		p.draw()
		p.load(false)
	})
	form := container.NewVBox(
		shell.LabeledHint("Name", "Shown on the project's tab.", name),
		shell.LabeledHint("Parent issues",
			"Paste the links of the issues whose child issues make up the project, one per line. "+
				"GitHub holds 100 child issues per issue, so a bigger project is spread over several: paste them all.",
			issues),
		problem,
		shell.DialogButtons(cancel, save),
	)
	d = dialog.NewCustomWithoutButtons(title, form, win)
	width, maxRows := float32(620), 0
	if win != nil && win.Canvas() != nil && win.Canvas().Size().Width > 0 {
		size := win.Canvas().Size()
		width = min(width, size.Width*0.92)
		// The box grows a row at a time with what is pasted, as Log Work's
		// remarks do, until the popup would outgrow the window; past that it
		// scrolls inside.
		row := fyne.MeasureText("M", theme.TextSize(), fyne.TextStyle{}).Height + theme.LineSpacing()
		maxRows = max(editorRows, editorRows+int((size.Height*0.85-d.MinSize().Height)/row))
	}
	grow := shell.AutoGrow(issues, editorRows, maxRows, func() {
		d.Resize(fyne.NewSize(width, d.MinSize().Height))
	})
	issues.OnChanged = func(string) { grow() }
	d.Resize(fyne.NewSize(width, d.MinSize().Height))
	shell.ShowDialog(d, win)
	// Rows are counted against the box's width, known once it is laid out.
	grow()
}

// editorRows is the least the project editor's issue box shows.
const editorRows = 6

// confirmDelete asks before the open project is deleted.
func (p *page) confirmDelete() {
	cur := p.saved.Current()
	if cur == nil {
		return
	}
	id := cur.ID
	body := widget.NewLabel("The project leaves this app only. Its issues on GitHub are not touched.")
	body.Wrapping = fyne.TextWrapWord
	var popup *widget.PopUp
	cancel := widget.NewButton("Cancel", func() { shell.ClosePopUp(popup) })
	confirm := widget.NewButton("Delete", func() {
		shell.ClosePopUp(popup)
		p.saved.Projects = slices.DeleteFunc(p.saved.Projects, func(project Project) bool { return project.ID == id })
		p.saved.Selected = ""
		delete(p.snaps, id)
		p.loading[id] = false
		p.fetches[id]++ // a fetch on its way is dropped
		p.save()
		p.buildTabs()
		p.draw()
		p.load(false)
	})
	heading := widget.NewLabelWithStyle(shell.Truncate("Delete "+cur.Name+"?", 60), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	content := shell.RemovalPopupContent(container.NewBorder(container.NewVBox(heading, body),
		shell.RemovalButtons(cancel, confirm), nil, nil, nil))
	window := p.ui.Win.Canvas()
	popup = widget.NewModalPopUp(content, window)
	size := p.ui.RemovalPopupSize(content)
	popup.Resize(size)
	popup.Show()
	popup.Move(fyne.NewPos((window.Size().Width-size.Width)/2, (window.Size().Height-size.Height)/2))
	shell.AnimateOpening(window)
}
