package main

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const kanbanTabName = "Kanban"
const plannerTabName = "Sprint Calendar"

type kanbanTarget struct {
	object             fyne.CanvasObject
	clip               fyne.CanvasObject
	frame              *canvas.Rectangle
	board, kind, value string
	strokeColor        color.Color
	strokeWidth        float32
}
type kanbanUI struct {
	ui                                  *UI
	plan                                sprintPlan
	root, body, ghostLayer              *fyne.Container
	sideClip, boardClip                 fyne.CanvasObject
	message                             *widget.Label
	targets                             []*kanbanTarget
	hover                               *kanbanTarget
	loading, busy                       bool
	loaded                              bool
	storageErr                          error
	filter, board, sprint, search, week string
	calendarBody, calendarGhost         *fyne.Container
	calendarMessage                     *widget.Label
	calendarMode, month                 string
	dates                               []*plannerDateTarget
	calendarClip                        fyne.CanvasObject
	calendarIssueClip                   fyne.CanvasObject
	calendarDrawnMode                   string
	calendarOffsets                     map[string]fyne.Position
	autoStarted                         map[string]bool // issues already auto-started this session
	autoStartPending                    bool            // next board fetch should start today's planned issues
	pending                             map[string]kanbanPending
	columnCards                         map[string][]*sprintDragCard // status value: cards as drawn
	hoverSlot                           kanbanSlot
	dropLine                            *canvas.Rectangle
	activity, calendarActivity          *loadingIndicator
	calendarPage                        *fyne.Container
	refreshButtons                      []*widget.Button
	calendarDrag                        *plannerDragCard
	calendarResizing                    bool
	calendarActive                      bool
	projectOff                          map[string]bool
	sourceVersion                       int
	calendarSprint                      string
	extraStatuses                       map[string]bool
	statusChoices                       *widget.CheckGroup
	statusChoiceView                    fyne.CanvasObject
	typeChoiceView                      fyne.CanvasObject
}

func (ui *UI) buildKanbanTab() fyne.CanvasObject {
	k := &kanbanUI{ui: ui, calendarMode: "Week", month: thisMonth(), filter: "current", week: weekStartOf(today())}
	ui.kanban = k
	var err error
	k.plan, err = ui.store.loadSprintPlan()
	k.storageErr = err
	k.calendarSprint = k.plan.CalendarSprint
	if k.plan.KanbanColumns != nil {
		k.extraStatuses = map[string]bool{}
		for _, name := range *k.plan.KanbanColumns {
			k.extraStatuses[name] = true
		}
	}
	if k.plan.CalendarMode == "Month" || k.plan.CalendarMode == "Sprint" {
		k.calendarMode = k.plan.CalendarMode
	}
	k.body = container.NewStack()
	k.ghostLayer = container.NewWithoutLayout()
	k.message = widget.NewLabel("")
	k.message.Wrapping = fyne.TextWrapWord
	k.message.Hide()
	if err != nil {
		k.setMessage("Cannot read local planner: " + err.Error())
	}
	search := widget.NewEntry()
	search.SetPlaceHolder("Search issues or project")
	search.OnChanged = func(s string) { k.search = strings.ToLower(s); k.draw() }
	refresh := widget.NewButtonWithIcon("Refresh GitHub", theme.ViewRefreshIcon(), func() { k.load(true) })
	k.refreshButtons = append(k.refreshButtons, refresh)
	k.activity = newLoadingIndicator()
	top := container.NewBorder(nil, nil, widget.NewLabelWithStyle("Kanban", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), refresh, search)
	k.root = container.NewBorder(container.NewVBox(top, k.activity.view, k.message), nil, nil, nil, container.NewStack(k.body, k.ghostLayer))
	k.draw()
	return k.root
}

func (k *kanbanUI) save(p sprintPlan) bool {
	if k.storageErr != nil {
		k.ui.errf(fmt.Errorf("local planner could not be read; preserving its file: %w", k.storageErr))
		return false
	}
	if err := k.ui.store.saveSprintPlan(p); err != nil {
		k.ui.errf(err)
		return false
	}
	k.plan = p
	// Log Work's day bars show the planned time; keep them in step.
	if k.ui.weekBox != nil {
		k.ui.drawWeekStrip()
	}
	return true
}

func (k *kanbanUI) load(force bool) {
	if k.loading || k.busy || (k.loaded && !force) {
		return
	}
	k.loading = true
	k.syncActivity()
	k.setMessage("Loading configured project boards… You can use the saved local calendar while loading.")
	previousBoards := append([]sprintBoard{}, k.plan.Boards...)
	sources := append([]kanbanSource{}, kanbanSources(k.ui.cfg)...)
	version := k.sourceVersion
	go func() {
		boards := make([]sprintBoard, 0, 2)
		var errors []string
		for _, source := range sources {
			u := source.URL
			b, err := fetchSprintBoardWithFilter(u, source.Filter)
			if err != nil {
				errors = append(errors, u+": "+err.Error())
				for _, old := range previousBoards {
					if sameSprintProject(old.URL, u) {
						boards = append(boards, old)
					}
				}
			} else {
				boards = append(boards, b)
			}
		}
		fyne.Do(func() {
			k.loading = false
			if version != k.sourceVersion {
				k.load(true)
				return
			}
			k.loaded = len(errors) == 0
			p := k.plan
			p.Boards = boards
			p.Fetched = time.Now().Format(time.RFC3339)
			if k.save(p) {
				k.setMessage("")
				if len(errors) > 0 {
					k.setMessage("Refresh incomplete; saved data retained for unavailable boards. " + strings.Join(errors, "; "))
				}
				// Fresh statuses: drop plans for issues that have left the
				// boards. Today's planned issues are started only when the
				// fetch came from the Sprint Calendar or app start, never from
				// Kanban, where status changes are made by dragging.
				k.prunePlan()
				if k.autoStartPending {
					k.autoStartPending = false
					k.startPlannedToday()
				}
			}
			k.draw()
		})
	}()
}

func (k *kanbanUI) currentItems() []sprintCandidate {
	var out []sprintCandidate
	for _, b := range k.plan.Boards {
		if !k.visibleBoard(b) {
			continue
		}
		s := b.current(today())
		if s.ID == "" {
			continue
		}
		for _, it := range b.Items {
			if it.SprintID == s.ID {
				out = append(out, sprintCandidate{it.key(b.ID), it, s})
			}
		}
	}
	return out
}

func (k *kanbanUI) matches(b sprintBoard, it sprintItem) bool {
	return k.visibleBoard(b) && k.matchesIssueFilter(b, it)
}

func (k *kanbanUI) matchesIssueFilter(b sprintBoard, it sprintItem) bool {
	if !k.visibleItemType(it) {
		return false
	}
	if k.search != "" && !strings.Contains(strings.ToLower(it.Title+" "+it.URL+" "+b.Title), k.search) {
		return false
	}
	switch k.filter {
	case "current":
		s := b.current(today())
		return s.ID != "" && it.SprintID == s.ID
	case "none":
		return it.SprintID == ""
	case "sprint":
		s := b.sprintByKey(k.sprint)
		return s.ID != "" && it.SprintID == s.ID
	}
	return true
}

func (k *kanbanUI) setMessage(text string) {
	if k.message != nil {
		k.message.SetText(text)
		showIf(k.message, strings.TrimSpace(text) != "")
	}
	if k.calendarMessage != nil {
		k.calendarMessage.SetText(text)
		showIf(k.calendarMessage, strings.TrimSpace(text) != "")
	}
}

func (ui *UI) buildPlannerTab() fyne.CanvasObject {
	k := ui.kanban
	if k == nil {
		ui.buildKanbanTab()
		k = ui.kanban
	}
	k.calendarBody = container.NewStack()
	k.calendarGhost = container.NewWithoutLayout()
	k.calendarMessage = widget.NewLabel(k.message.Text)
	k.calendarMessage.Wrapping = fyne.TextWrapWord
	showIf(k.calendarMessage, strings.TrimSpace(k.message.Text) != "")
	heading := widget.NewLabelWithStyle("Sprint Calendar · local planning", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	note := widget.NewLabel("Select a sprint, then drag bubbles onto highlighted dates. Drag a scheduled bubble's bottom edge to resize its local duration.")
	note.Wrapping = fyne.TextWrapWord
	refresh := widget.NewButtonWithIcon("Refresh GitHub", theme.ViewRefreshIcon(), k.refreshCalendarSources)
	k.refreshButtons = append(k.refreshButtons, refresh)
	k.calendarActivity = newLoadingIndicator()
	top := container.NewBorder(nil, nil, heading, refresh, nil)
	page := container.NewBorder(container.NewVBox(top, note, k.calendarActivity.view, k.calendarMessage), nil, nil, nil, container.NewStack(k.calendarBody, k.calendarGhost))
	k.calendarPage = page
	k.draw()
	return page
}

func (k *kanbanUI) draw() {
	sideOffset := scrollOffset(k.sideClip)
	boardOffset := scrollOffset(k.boardClip)
	issuesOffset := scrollOffset(k.calendarIssueClip)
	if k.calendarOffsets == nil {
		k.calendarOffsets = map[string]fyne.Position{}
	}
	if k.calendarClip != nil {
		k.calendarOffsets[k.calendarDrawnMode] = scrollOffset(k.calendarClip)
	}
	k.syncActivity()
	k.targets = nil
	k.hover = nil
	k.columnCards = map[string][]*sprintDragCard{}
	k.body.Objects = []fyne.CanvasObject{k.boardView()}
	k.body.Refresh()
	restoreScroll(k.sideClip, sideOffset)
	restoreScroll(k.boardClip, boardOffset)
	if k.calendarBody != nil {
		k.dates = nil
		k.calendarBody.Objects = []fyne.CanvasObject{k.calendar()}
		k.calendarBody.Refresh()
		restoreScroll(k.calendarClip, k.calendarOffsets[k.calendarMode])
		restoreScroll(k.calendarIssueClip, issuesOffset)
		k.calendarDrawnMode = k.calendarMode
	}
}

func (k *kanbanUI) dropArea(content fyne.CanvasObject, b, kind, value string) fyne.CanvasObject {
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	if kind == "sprint" {
		bg.FillColor = color.Transparent
	}
	bg.CornerRadius = 8
	obj := container.NewStack(bg, content)
	k.targets = append(k.targets, &kanbanTarget{object: obj, frame: bg, board: b, kind: kind, value: value})
	return obj
}

func (k *kanbanUI) boardView() fyne.CanvasObject {
	side := k.sprintSidebar()
	sideScroll := container.NewVScroll(container.NewPadded(side))
	sideScroll.SetMinSize(fyne.NewSize(255, 0))
	k.sideClip = sideScroll
	statuses, extra := k.statusColumns()
	columns := container.New(kanbanColumnsLayout{})
	for _, status := range statuses {
		status := status
		value := status
		if value == "No status" {
			value = ""
		}
		cards := container.NewVBox()
		count := 0
		for _, b := range k.plan.Boards {
			for _, it := range b.Items {
				if k.pendingStatus(b, it) != value || !k.matches(b, it) {
					continue
				}
				count++
				card := k.card(b, it)
				if d, ok := card.(*sprintDragCard); ok {
					k.columnCards[value] = append(k.columnCards[value], d)
				}
				cards.Add(card)
			}
		}
		header := widget.NewLabelWithStyle(status, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		header.Wrapping = fyne.TextWrapWord
		heading := container.NewPadded(container.NewBorder(nil, nil, nil, container.NewCenter(sprintCountBadge(count)), container.NewThemeOverride(header, statusHeaderTheme{Theme: theme.Current(), ink: statusHeaderInk(status)})))
		col := k.dropArea(container.NewBorder(heading, nil, nil, nil, container.NewVScroll(container.NewPadded(cards))), "", "status", value)
		frame := k.targets[len(k.targets)-1].frame
		frame.FillColor = kanbanColumnFill(status)
		frame.StrokeColor = kanbanColumnBorder(status)
		frame.StrokeWidth = 1
		columns.Add(container.New(kanbanColumnLayout{}, col))
	}
	boardScroll := container.NewHScroll(columns)
	k.boardClip = boardScroll
	for _, t := range k.targets {
		t.strokeColor = t.frame.StrokeColor
		t.strokeWidth = t.frame.StrokeWidth
		if t.kind == "status" {
			t.clip = boardScroll
		} else {
			t.clip = sideScroll
		}
	}
	if len(k.plan.Boards) == 0 {
		return container.NewCenter(widget.NewLabel("Add project board URLs and filters in Settings, then refresh GitHub."))
	}
	board := container.NewBorder(nil, k.columnControls(extra), nil, nil, boardScroll)
	legend := container.NewBorder(nil, nil, nil, k.itemTypeControls(), k.projectLegend())
	return container.NewBorder(legend, nil, sideScroll, nil, board)
}

func (k *kanbanUI) accent(b sprintBoard, it sprintItem) color.NRGBA {
	org := orgOf(it.URL)
	if org == "" {
		ref, err := parseProjectRef(b.URL)
		if err == nil {
			org = ref.login
		}
	}
	cfg := k.colorConfig()
	return orgColor(cfg, org)
}

func (k *kanbanUI) bubble(b sprintBoard, it sprintItem, caption string, compact bool) fyne.CanvasObject {
	title := widget.NewLabelWithStyle(it.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Wrapping = fyne.TextWrapWord
	if caption == "" {
		body := container.NewVBox(title)
		if it.URL != "" {
			body.Add(githubHyperlink("Open issue", it.URL))
		}
		return k.bubbleSurface(b, it, body)
	}
	foot := widget.NewLabel(caption)
	foot.Wrapping = fyne.TextWrapWord
	return k.bubbleSurface(b, it, k.bubbleContents(title, foot, it.URL))
}

func (k *kanbanUI) bubbleSurface(b sprintBoard, it sprintItem, body fyne.CanvasObject) fyne.CanvasObject {
	accent := k.accent(b, it)
	bg := canvas.NewRectangle(blendColor(theme.Color(theme.ColorNameInputBackground), accent, 0.24))
	bg.CornerRadius = 10
	bg.StrokeWidth = 1
	bg.StrokeColor = blendColor(theme.Color(theme.ColorNameInputBorder), accent, 0.65)
	stripe := canvas.NewRectangle(accent)
	stripe.CornerRadius = 2
	stripe.SetMinSize(fyne.NewSize(4, 0))
	return container.NewStack(bg, container.NewPadded(container.NewBorder(nil, nil, stripe, nil, container.NewPadded(body))))
}

func (k *kanbanUI) card(b sprintBoard, it sprintItem) fyne.CanvasObject {
	content := k.bubbleSurface(b, it, k.cardContents(b, it))
	if p, ok := k.pending[it.key(b.ID)]; ok {
		content = savingOverlay(content, p)
	}
	d := &sprintDragCard{k: k, board: b, item: it, content: content}
	d.ExtendBaseWidget(d)
	return d
}

func projectShortName(b sprintBoard) string {
	if ref, err := parseProjectRef(b.URL); err == nil {
		return ref.login
	}
	return b.Title
}

func sprintSummary(s sprintIteration) string {
	end := addDays(s.StartDate, s.Duration-1)
	days := 0
	for date := today(); s.contains(date); date = addDays(date, 1) {
		days++
	}
	label := fmt.Sprintf("%s · %s – %s · %d days", s.Title, s.StartDate, end, s.Duration)
	if s.contains(today()) {
		label += fmt.Sprintf(" · %d days left", days)
	}
	return label
}

func (k *kanbanUI) move(b sprintBoard, it sprintItem, kind, value string) {
	k.moveThen(b, it, kind, value, nil, nil)
}

// moveThen saves a status or sprint change to GitHub. While it runs the card
// shows the change in progress and the activity bar runs; done, if given, is
// called on the UI thread with the outcome, and start, if given, just before
// the request goes out. It reports whether a save started.
func (k *kanbanUI) moveThen(b sprintBoard, it sprintItem, kind, value string, start func(), done func(error)) bool {
	if k.busy || k.loading {
		k.setMessage("Wait for the GitHub request to finish before moving an issue.")
		return false
	}
	for _, latestBoard := range k.plan.Boards {
		if latestBoard.ID == b.ID {
			for _, latestItem := range latestBoard.Items {
				if latestItem.ID == it.ID {
					it = latestItem
				}
			}
		}
	}
	if kind == "status" && it.Status == value || kind == "sprint" && it.SprintID == value {
		return false
	}
	k.busy = true
	k.setPending(b, it, kind, value)
	p := k.pending[it.key(b.ID)]
	what := "status"
	if kind == "sprint" {
		what = "sprint"
	}
	k.setMessage(fmt.Sprintf("Saving %q to GitHub: %s %s…", it.Title, what, p.label))
	k.draw()
	if start != nil {
		start()
	}
	go func() {
		err := pushSprintItem(b, it, kind, value)
		fyne.Do(func() {
			k.busy = false
			k.clearPending(b, it)
			if done != nil {
				defer done(err)
			}
			if err != nil {
				k.setMessage(moveResult(it, p, err))
				k.draw()
				k.ui.errf(err)
				return
			}
			for bi := range k.plan.Boards {
				if k.plan.Boards[bi].ID == b.ID {
					for ii := range k.plan.Boards[bi].Items {
						i := &k.plan.Boards[bi].Items[ii]
						if i.ID == it.ID {
							if kind == "status" {
								i.Status = value
							} else {
								i.SprintID = value
							}
						}
					}
				}
			}
			k.save(k.plan)
			k.setMessage(moveResult(it, p, nil))
			k.draw()
		})
	}()
	return true
}

func pointIn(obj fyne.CanvasObject, p fyne.Position) bool {
	if obj == nil || !obj.Visible() {
		return false
	}
	o := fyne.CurrentApp().Driver().AbsolutePositionForObject(obj)
	s := obj.Size()
	return p.X >= o.X && p.Y >= o.Y && p.X < o.X+s.Width && p.Y < o.Y+s.Height
}

type sprintDragCard struct {
	widget.BaseWidget
	k       *kanbanUI
	board   sprintBoard
	item    sprintItem
	content fyne.CanvasObject
	lifted  bool
}

func (c *sprintDragCard) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.content)
}
func (c *sprintDragCard) Cursor() desktop.Cursor  { return grabCursor{} }
func (c *sprintDragCard) Tapped(*fyne.PointEvent) { c.k.editor(c.board, c.item) }
func (c *sprintDragCard) Dragged(e *fyne.DragEvent) {
	k := c.k
	if k.busy || k.loading {
		return
	}
	if !c.lifted {
		c.lifted = true
		bg := canvas.NewRectangle(theme.Color(theme.ColorNameOverlayBackground))
		bg.CornerRadius = 8
		ghost := container.NewStack(bg, container.NewPadded(widget.NewLabel(truncate(c.item.Title, 38))))
		ghost.Resize(ghost.MinSize())
		k.dropLine = newDropLine()
		k.ghostLayer.Objects = []fyne.CanvasObject{ghost, k.dropLine}
	}
	if len(k.ghostLayer.Objects) > 0 {
		k.ghostLayer.Objects[0].Move(e.AbsolutePosition.Subtract(fyne.CurrentApp().Driver().AbsolutePositionForObject(k.ghostLayer)).Add(fyne.NewPos(12, 12)))
		k.ghostLayer.Refresh()
	}
	k.hover = nil
	for _, t := range k.targets {
		valid := t.board == "" || t.board == c.board.ID
		if t.kind == "status" && t.value != "" {
			valid = false
			for _, o := range c.board.field("Status").Options {
				if o.Name == t.value {
					valid = true
				}
			}
		}
		if t.kind == "sprint" && t.value != "" {
			valid = valid && c.board.sprintByKey(t.value).ID != ""
		}
		hover := valid && pointIn(t.object, e.AbsolutePosition) && pointIn(t.clip, e.AbsolutePosition)
		t.frame.StrokeWidth = t.strokeWidth
		t.frame.StrokeColor = t.strokeColor
		if hover {
			k.hover = t
			t.frame.StrokeWidth = 3
			t.frame.StrokeColor = logoGold
		}
		t.frame.Refresh()
	}
	k.showDropSlot(c, k.hover, e.AbsolutePosition)
}
func (c *sprintDragCard) DragEnd() {
	if !c.lifted {
		return
	}
	c.lifted = false
	k := c.k
	k.ghostLayer.Objects = nil
	k.ghostLayer.Refresh()
	t := k.hover
	k.hover = nil
	slot := k.hoverSlot
	k.hoverSlot = kanbanSlot{}
	k.dropLine = nil
	for _, target := range k.targets {
		target.frame.StrokeWidth = target.strokeWidth
		target.frame.StrokeColor = target.strokeColor
		target.frame.Refresh()
	}
	if t != nil {
		value := t.value
		if t.kind == "sprint" && value != "" {
			value = c.board.sprintByKey(value).ID
		}
		if t.kind == "status" {
			k.place(c.board, c.item, value, slot)
			return
		}
		k.move(c.board, c.item, t.kind, value)
	}
}

func (k *kanbanUI) openCandidate(c sprintCandidate) {
	for _, b := range k.plan.Boards {
		for _, it := range b.Items {
			if it.key(b.ID) == c.Key {
				k.editorFor(b, it, true)
				return
			}
		}
	}
}

func (k *kanbanUI) editor(b sprintBoard, it sprintItem) { k.editorFor(b, it, false) }

func (k *kanbanUI) editorFor(b sprintBoard, it sprintItem, localOnly bool) {
	key := it.key(b.ID)
	task := k.plan.Tasks[key]
	estimate := widget.NewEntry()
	estimate.SetText(strconv.Itoa(sprintEstimate(it, task)))
	complexity := widget.NewSelect(append([]string{"Use GitHub value"}, sprintComplexities...), nil)
	complexity.SetSelected(orDefault(task.Complexity, "Use GitHub value"))
	complexity.OnChanged = func(v string) {
		t := sprintTask{}
		if v != "Use GitHub value" {
			t.Complexity = v
		}
		estimate.SetText(strconv.Itoa(sprintEstimate(it, t)))
	}
	notes := widget.NewMultiLineEntry()
	notes.SetText(task.Notes)
	notes.SetMinRowsVisible(2)
	done := widget.NewCheck("Completed locally", nil)
	done.SetChecked(task.Done)
	blocks := widget.NewMultiLineEntry()
	blocks.SetMinRowsVisible(4)
	var lines []string
	for _, block := range task.Blocks {
		lines = append(lines, fmt.Sprintf("%s %s %d", block.Date, planClock(block.Start), block.Minutes))
	}
	blocks.SetText(strings.Join(lines, "\n"))
	blocks.SetPlaceHolder("2026-11-02 09:00 120")
	local := container.NewVBox(container.NewGridWithColumns(2, labeled("Local effort (minutes)", estimate), labeled("Complexity", complexity)), labeled("Local notes", notes), goldChoice(done))
	schedule := container.NewVBox()
	current := sprintIteration{}
	for _, s := range b.iterations() {
		if s.ID == it.SprintID {
			current = s
		}
	}
	canPlan := current.ID != ""
	if canPlan {
		date := widget.NewEntry()
		if current.contains(today()) {
			date.SetText(today())
		} else {
			date.SetText(current.StartDate)
		}
		clock := widget.NewEntry()
		clock.SetText("09:00")
		duration := widget.NewEntry()
		duration.SetText(strconv.Itoa(plannerDurationUp(sprintEstimate(it, task))))
		schedule.Add(container.NewGridWithColumns(3, labeled("Date (YYYY-MM-DD)", date), labeled("Start (HH:MM)", clock), labeled("Minutes (15m steps)", duration)))
		schedule.Add(widget.NewButton("Add time block", func() {
			minutes, err := strconv.Atoi(strings.TrimSpace(duration.Text))
			if err != nil || !validPlannerDuration(minutes) {
				k.ui.errf(fmt.Errorf("duration must use 15-minute increments (15, 30, 45, …)"))
				return
			}
			line := strings.TrimSpace(date.Text) + " " + strings.TrimSpace(clock.Text) + " " + strings.TrimSpace(duration.Text)
			if strings.TrimSpace(blocks.Text) == "" {
				blocks.SetText(line)
			} else {
				blocks.SetText(strings.TrimSpace(blocks.Text) + "\n" + line)
			}
		}))
		schedule.Add(widget.NewLabel("Time blocks: YYYY-MM-DD HH:MM MINUTES"))
		schedule.Add(blocks)
	} else {
		schedule.Add(widget.NewLabel("Assign a sprint to schedule this issue."))
	}
	statusOptions := []string{"No status"}
	for _, o := range b.field("Status").Options {
		statusOptions = append(statusOptions, o.Name)
	}
	status := widget.NewSelect(statusOptions, nil)
	status.SetSelected(orDefault(it.Status, "No status"))
	sprintOptions := []string{"No sprint"}
	ids := []string{""}
	for _, s := range b.iterations() {
		sprintOptions = append(sprintOptions, s.Title+" · "+s.StartDate)
		ids = append(ids, s.ID)
	}
	sprint := widget.NewSelect(sprintOptions, nil)
	sprint.SetSelectedIndex(0)
	for i, id := range ids {
		if id == it.SprintID {
			sprint.SetSelectedIndex(i)
		}
	}
	// The popup covers the page message, so a save started here reports in
	// the popup too: bar running and buttons off while it is in flight.
	applyProgress := newLoadingIndicator()
	applyState := widget.NewLabel("")
	applyState.Wrapping = fyne.TextWrapWord
	applyState.Hide()
	var applyStatus, applySprint *widget.Button
	apply := func(kind, value string) {
		started := k.moveThen(b, it, kind, value, func() {
			applyState.Hide()
			applyProgress.showProgress(true, fetchProgress{Stage: "Saving " + kind + " to GitHub: " + fieldValueLabel(b, kind, value)})
			applyStatus.Disable()
			applySprint.Disable()
		}, func(err error) {
			applyProgress.setActive(false)
			applyState.Show()
			applyStatus.Enable()
			applySprint.Enable()
			if err == nil {
				if kind == "status" {
					it.Status = value
				} else {
					it.SprintID = value
				}
			}
			applyState.SetText(moveResult(it, kanbanPending{kind: kind, label: fieldValueLabel(b, kind, value)}, err))
		})
		switch {
		case started:
		case k.busy || k.loading:
			applyState.SetText("Wait for the GitHub request to finish.")
			applyState.Show()
		default:
			applyState.SetText("Already " + fieldValueLabel(b, kind, value) + " on GitHub.")
			applyState.Show()
		}
	}
	applyStatus = widget.NewButton("Apply status", func() {
		v := status.Selected
		if v == "No status" {
			v = ""
		}
		apply("status", v)
	})
	applySprint = widget.NewButton("Apply sprint", func() {
		if i := sprint.SelectedIndex(); i >= 0 {
			apply("sprint", ids[i])
		}
	})
	remote := container.NewVBox(widget.NewLabel("GitHub fields · each Apply saves to the source project"),
		container.NewBorder(nil, nil, nil, applyStatus, status),
		container.NewBorder(nil, nil, nil, applySprint, sprint),
		applyProgress.view, applyState)
	title := widget.NewLabelWithStyle(it.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Wrapping = fyne.TextWrapWord
	subtitle := container.NewBorder(nil, nil, nil, githubHyperlink("Open issue", it.URL), widget.NewLabel(fmt.Sprintf("Project estimate: %dm | Local changes stay on this computer", sprintEstimate(it, sprintTask{}))))
	left := container.NewVBox()
	if !localOnly {
		left.Add(remote)
		left.Add(widget.NewSeparator())
	}
	left.Add(local)
	columns := container.NewGridWithColumns(2, left, schedule)
	popupBody := container.NewStack()
	popupTitle := "Kanban issue"
	if localOnly {
		popupTitle = "Local plan"
	}
	popup := dialog.NewCustomWithoutButtons(popupTitle, popupBody, k.ui.win)
	save := widget.NewButton("Save local plan", func() {
		n, err := strconv.Atoi(strings.TrimSpace(estimate.Text))
		if err != nil || n <= 0 || n > 1000000 {
			k.ui.errf(fmt.Errorf("effort must be between 1 and 1000000 minutes"))
			return
		}
		t := task
		t.Estimate = n
		t.Complexity = ""
		if complexity.Selected != "Use GitHub value" {
			t.Complexity = complexity.Selected
		}
		t.Notes = notes.Text
		t.Done = done.Checked
		if canPlan {
			t.Blocks = nil
			for _, line := range strings.Split(blocks.Text, "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				parts := strings.Fields(line)
				if len(parts) != 3 {
					k.ui.errf(fmt.Errorf("schedule format: YYYY-MM-DD HH:MM MINUTES"))
					return
				}
				clock, err := time.Parse("15:04", parts[1])
				if err != nil {
					k.ui.errf(fmt.Errorf("enter time as HH:MM"))
					return
				}
				mins, err := strconv.Atoi(parts[2])
				if err != nil {
					k.ui.errf(fmt.Errorf("duration must be a number of minutes"))
					return
				}
				block := planBlock{parts[0], clock.Hour()*60 + clock.Minute(), mins}
				if err := validatePlannerDuration(block, task.Blocks); err != nil {
					k.ui.errf(err)
					return
				}
				if !current.contains(block.Date) {
					k.ui.errf(fmt.Errorf("schedule dates must fall inside %s", current.Title))
					return
				}
				if err := validatePlanBlock(key, block, k.plan.Tasks); err != nil {
					k.ui.errf(err)
					return
				}
				for _, old := range t.Blocks {
					if old.Date == block.Date && old.Start < block.Start+block.Minutes && block.Start < old.Start+old.Minutes {
						k.ui.errf(fmt.Errorf("schedule blocks overlap"))
						return
					}
				}
				t.Blocks = append(t.Blocks, block)
			}
		}
		p := k.plan
		tasks := map[string]sprintTask{}
		for key, value := range p.Tasks {
			tasks[key] = value
		}
		tasks[key] = t
		p.Tasks = tasks
		if k.save(p) {
			popup.Hide()
			k.setMessage("Local plan saved on this computer.")
			k.draw()
		}
	})
	actions := container.NewHBox(layout.NewSpacer(), widget.NewButton("Close", popup.Hide), save)
	popupBody.Add(container.NewBorder(container.NewVBox(title, subtitle), actions, nil, nil, columns))
	size := fyne.NewSize(1080, 560)
	if available := k.ui.win.Canvas().Size(); available.Width > 0 && available.Height > 0 {
		size.Width = fyne.Min(size.Width, available.Width*0.94)
		size.Height = fyne.Min(size.Height, available.Height*0.9)
	}
	popup.Resize(size)
	popup.Show()
}

type kanbanColumnLayout struct{}

// Fill the board viewport when there are few columns; retain horizontal
// scrolling once the readable minimum width no longer fits.
type kanbanColumnsLayout struct{}

func (kanbanColumnsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.Size{}
	}
	return fyne.NewSize(float32(len(objects))*260+float32(len(objects)-1)*theme.Padding(), 240)
}
func (kanbanColumnsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	gap := theme.Padding()
	width := fyne.Max(260, (size.Width-float32(len(objects)-1)*gap)/float32(len(objects)))
	for i, o := range objects {
		o.Move(fyne.NewPos(float32(i)*(width+gap), 0))
		o.Resize(fyne.NewSize(width, size.Height))
	}
}

func (kanbanColumnLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(260, 240) }
func (kanbanColumnLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		o.Move(fyne.NewPos(1, 1))
		o.Resize(fyne.NewSize(fyne.Max(1, size.Width-2), fyne.Max(1, size.Height-2)))
	}
}

func (k *kanbanUI) colorConfig() Config {
	cfg := k.ui.cfg
	cfg.ExtraProjectURLs = append([]string{}, cfg.ExtraProjectURLs...)
	for _, b := range k.plan.Boards {
		cfg.ExtraProjectURLs = append(cfg.ExtraProjectURLs, b.URL)
	}
	return cfg
}

func (k *kanbanUI) bubbleContents(title, foot fyne.CanvasObject, issueURL string) fyne.CanvasObject {
	body := container.NewVBox(title, foot)
	if issueURL != "" {
		body.Add(githubHyperlink("Open issue", issueURL))
	}
	return body
}
