package kanban

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"time"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func BuildKanbanTab(ui *shell.UI) fyne.CanvasObject {
	k := &shell.KanbanUI{Ui: ui, CalendarMode: "Week", Month: shell.ThisMonth(), Filter: "current", Week: backend.WeekStartOf(shell.Today())}
	ui.Kanban = k
	var err error
	k.Plan, err = ui.Store.LoadSprintPlan()
	k.StorageErr = err
	k.CalendarSprint = k.Plan.CalendarSprint
	if k.Plan.KanbanColumns != nil {
		k.ExtraStatuses = map[string]bool{}
		for _, name := range *k.Plan.KanbanColumns {
			k.ExtraStatuses[name] = true
		}
	}
	if k.Plan.CalendarMode == "Month" || k.Plan.CalendarMode == "Sprint" {
		k.CalendarMode = k.Plan.CalendarMode
	}
	k.Body = container.NewStack()
	k.GhostLayer = container.NewWithoutLayout()
	k.Message = widget.NewLabel("")
	k.Message.Wrapping = fyne.TextWrapWord
	k.Message.Hide()
	if err != nil {
		k.SetMessage("Cannot read local planner: " + err.Error())
	}
	search := widget.NewEntry()
	search.SetPlaceHolder("Search issues or project")
	search.OnChanged = func(s string) { k.Search = strings.ToLower(s); Draw(k) }
	refresh := widget.NewButtonWithIcon("Refresh GitHub", theme.ViewRefreshIcon(), func() { k.Load(true) })
	k.RefreshButtons = append(k.RefreshButtons, refresh)
	k.Activity = shell.NewLoadingIndicator()
	top := container.NewBorder(nil, nil, widget.NewLabelWithStyle("Kanban", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), refresh, search)
	k.Root = container.NewBorder(container.NewVBox(top, k.Activity.View, k.Message), nil, nil, nil, container.NewStack(k.Body, k.GhostLayer))
	Draw(k)
	return k.Root
}

func currentItems(k *shell.KanbanUI) []backend.SprintCandidate {
	var out []backend.SprintCandidate
	for _, b := range k.Plan.Boards {
		if !k.VisibleBoard(b) {
			continue
		}
		s := b.Current(shell.Today())
		if s.ID == "" {
			continue
		}
		for _, it := range b.Items {
			if it.SprintID == s.ID {
				out = append(out, backend.SprintCandidate{it.Key(b.ID), it, s})
			}
		}
	}
	return out
}

func Matches(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem) bool {
	return k.VisibleBoard(b) && MatchesIssueFilter(k, b, it)
}

func MatchesIssueFilter(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem) bool {
	if !k.VisibleItemType(it) {
		return false
	}
	if k.Search != "" && !strings.Contains(strings.ToLower(it.Title+" "+it.URL+" "+b.Title), k.Search) {
		return false
	}
	switch k.Filter {
	case "current":
		s := b.Current(shell.Today())
		return s.ID != "" && it.SprintID == s.ID
	case "none":
		return it.SprintID == ""
	case "sprint":
		s := b.SprintByKey(k.Sprint)
		return s.ID != "" && it.SprintID == s.ID
	}
	return true
}

func BuildPlannerTab(ui *shell.UI) fyne.CanvasObject {
	k := ui.Kanban
	if k == nil {
		BuildKanbanTab(ui)
		k = ui.Kanban
	}
	k.CalendarBody = container.NewStack()
	k.CalendarGhost = container.NewWithoutLayout()
	k.CalendarMessage = widget.NewLabel(k.Message.Text)
	k.CalendarMessage.Wrapping = fyne.TextWrapWord
	shell.ShowIf(k.CalendarMessage, strings.TrimSpace(k.Message.Text) != "")
	heading := widget.NewLabelWithStyle("Sprint Calendar · local planning", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	note := widget.NewLabel("Select a sprint, then drag bubbles onto highlighted dates. Drag a scheduled bubble's bottom edge to resize its local duration.")
	note.Wrapping = fyne.TextWrapWord
	refresh := widget.NewButtonWithIcon("Refresh GitHub", theme.ViewRefreshIcon(), func() { shell.Hooks.KanbanUIRefreshCalendarSources(k) })
	k.RefreshButtons = append(k.RefreshButtons, refresh)
	k.CalendarActivity = shell.NewLoadingIndicator()
	top := container.NewBorder(nil, nil, heading, refresh, nil)
	page := container.NewBorder(container.NewVBox(top, note, k.CalendarActivity.View, k.CalendarMessage), nil, nil, nil, container.NewStack(k.CalendarBody, k.CalendarGhost))
	k.CalendarPage = page
	Draw(k)
	return page
}

func Draw(k *shell.KanbanUI) {
	sideOffset := shell.ScrollOffset(k.SideClip)
	boardOffset := shell.ScrollOffset(k.BoardClip)
	issuesOffset := shell.ScrollOffset(k.CalendarIssueClip)
	if k.CalendarOffsets == nil {
		k.CalendarOffsets = map[string]fyne.Position{}
	}
	if k.CalendarClip != nil {
		k.CalendarOffsets[k.CalendarDrawnMode] = shell.ScrollOffset(k.CalendarClip)
	}
	k.SyncActivity()
	k.Targets = nil
	k.Hover = nil
	k.ColumnCards = map[string][]*shell.SprintDragCard{}
	k.Body.Objects = []fyne.CanvasObject{boardView(k)}
	k.Body.Refresh()
	shell.RestoreScroll(k.SideClip, sideOffset)
	shell.RestoreScroll(k.BoardClip, boardOffset)
	if k.CalendarBody != nil {
		k.Dates = nil
		k.CalendarBody.Objects = []fyne.CanvasObject{shell.Hooks.KanbanUICalendar(k)}
		k.CalendarBody.Refresh()
		shell.RestoreScroll(k.CalendarClip, k.CalendarOffsets[k.CalendarMode])
		shell.RestoreScroll(k.CalendarIssueClip, issuesOffset)
		k.CalendarDrawnMode = k.CalendarMode
	}
}

func dropArea(k *shell.KanbanUI, content fyne.CanvasObject, b, kind, value string) fyne.CanvasObject {
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	if kind == "sprint" {
		bg.FillColor = color.Transparent
	}
	bg.CornerRadius = 8
	obj := container.NewStack(bg, content)
	k.Targets = append(k.Targets, &shell.KanbanTarget{Object: obj, Frame: bg, Board: b, Kind: kind, Value: value})
	return obj
}

func boardView(k *shell.KanbanUI) fyne.CanvasObject {
	side := sprintSidebar(k)
	sideScroll := container.NewVScroll(container.NewPadded(side))
	sideScroll.SetMinSize(fyne.NewSize(255, 0))
	k.SideClip = sideScroll
	statuses, extra := StatusColumns(k)
	columns := container.New(KanbanColumnsLayout{})
	for _, status := range statuses {
		status := status
		value := status
		if value == "No status" {
			value = ""
		}
		cards := container.NewVBox()
		count := 0
		for _, b := range k.Plan.Boards {
			for _, it := range b.Items {
				if PendingStatus(k, b, it) != value || !Matches(k, b, it) {
					continue
				}
				count++
				card := Card(k, b, it)
				if d, ok := card.(*shell.SprintDragCard); ok {
					k.ColumnCards[value] = append(k.ColumnCards[value], d)
				}
				cards.Add(card)
			}
		}
		header := widget.NewLabelWithStyle(status, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		header.Wrapping = fyne.TextWrapWord
		heading := container.NewPadded(container.NewBorder(nil, nil, nil, container.NewCenter(sprintCountBadge(count)), container.NewThemeOverride(header, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: statusHeaderInk(status)})))
		col := dropArea(k, container.NewBorder(heading, nil, nil, nil, container.NewVScroll(container.NewPadded(cards))), "", "status", value)
		frame := k.Targets[len(k.Targets)-1].Frame
		frame.FillColor = KanbanColumnFill(status)
		frame.StrokeColor = KanbanColumnBorder(status)
		frame.StrokeWidth = 1
		columns.Add(container.New(kanbanColumnLayout{}, col))
	}
	boardScroll := container.NewHScroll(columns)
	k.BoardClip = boardScroll
	for _, t := range k.Targets {
		t.StrokeColor = t.Frame.StrokeColor
		t.StrokeWidth = t.Frame.StrokeWidth
		if t.Kind == "status" {
			t.Clip = boardScroll
		} else {
			t.Clip = sideScroll
		}
	}
	if len(k.Plan.Boards) == 0 {
		return container.NewCenter(widget.NewLabel("Add project board URLs and filters in Settings, then refresh GitHub."))
	}
	board := container.NewBorder(nil, columnControls(k, extra), nil, nil, boardScroll)
	legend := container.NewBorder(nil, nil, nil, itemTypeControls(k), k.ProjectLegend())
	return container.NewBorder(legend, nil, sideScroll, nil, board)
}

func BubbleSurface(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem, body fyne.CanvasObject) fyne.CanvasObject {
	accent := k.Accent(b, it)
	bg := canvas.NewRectangle(shell.BlendColor(theme.Color(theme.ColorNameInputBackground), accent, 0.24))
	bg.CornerRadius = 10
	bg.StrokeWidth = 1
	bg.StrokeColor = shell.BlendColor(theme.Color(theme.ColorNameInputBorder), accent, 0.65)
	stripe := canvas.NewRectangle(accent)
	stripe.CornerRadius = 2
	stripe.SetMinSize(fyne.NewSize(4, 0))
	return container.NewStack(bg, container.NewPadded(container.NewBorder(nil, nil, stripe, nil, container.NewPadded(body))))
}

func Card(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem) fyne.CanvasObject {
	content := BubbleSurface(k, b, it, CardContents(k, b, it))
	if p, ok := k.Pending[it.Key(b.ID)]; ok {
		content = SavingOverlay(content, p)
	}
	d := &shell.SprintDragCard{K: k, Board: b, Item: it, Content: content}
	d.ExtendBaseWidget(d)
	return d
}

func Move(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem, kind, value string) {
	MoveThen(k, b, it, kind, value, nil, nil)
}

// MoveThen saves a status or sprint change to GitHub. While it runs the card
// shows the change in progress and the activity bar runs; done, if given, is
// called on the UI thread with the outcome, and start, if given, just before
// the request goes out. It reports whether a save started.
func MoveThen(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem, kind, value string, start func(), done func(error)) bool {
	if k.Busy || k.Loading {
		k.SetMessage("Wait for the GitHub request to finish before moving an issue.")
		return false
	}
	for _, latestBoard := range k.Plan.Boards {
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
	k.Busy = true
	k.SetPending(b, it, kind, value)
	p := k.Pending[it.Key(b.ID)]
	what := "status"
	if kind == "sprint" {
		what = "sprint"
	}
	k.SetMessage(fmt.Sprintf("Saving %q to GitHub: %s %s…", it.Title, what, p.Label))
	Draw(k)
	if start != nil {
		start()
	}
	go func() {
		err := backend.PushSprintItem(b, it, kind, value)
		fyne.Do(func() {
			k.Busy = false
			k.ClearPending(b, it)
			if done != nil {
				defer done(err)
			}
			if err != nil {
				k.SetMessage(moveResult(it, p, err))
				Draw(k)
				k.Ui.Errf(err)
				return
			}
			for bi := range k.Plan.Boards {
				if k.Plan.Boards[bi].ID == b.ID {
					for ii := range k.Plan.Boards[bi].Items {
						i := &k.Plan.Boards[bi].Items[ii]
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
			k.Save(k.Plan)
			k.SetMessage(moveResult(it, p, nil))
			Draw(k)
		})
	}()
	return true
}

func OpenCandidate(k *shell.KanbanUI, c backend.SprintCandidate) {
	for _, b := range k.Plan.Boards {
		for _, it := range b.Items {
			if it.Key(b.ID) == c.Key {
				EditorFor(k, b, it, true)
				return
			}
		}
	}
}

func Editor(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem) {
	EditorFor(k, b, it, false)
}

func EditorFor(k *shell.KanbanUI, b backend.SprintBoard, it backend.SprintItem, localOnly bool) {
	key := it.Key(b.ID)
	task := k.Plan.Tasks[key]
	estimate := widget.NewEntry()
	estimate.SetText(strconv.Itoa(backend.SprintEstimate(it, task)))
	complexity := widget.NewSelect(append([]string{"Use GitHub value"}, backend.SprintComplexities...), nil)
	complexity.SetSelected(backend.OrDefault(task.Complexity, "Use GitHub value"))
	complexity.OnChanged = func(v string) {
		t := backend.SprintTask{}
		if v != "Use GitHub value" {
			t.Complexity = v
		}
		estimate.SetText(strconv.Itoa(backend.SprintEstimate(it, t)))
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
		lines = append(lines, fmt.Sprintf("%s %s %d", block.Date, backend.PlanClock(block.Start), block.Minutes))
	}
	blocks.SetText(strings.Join(lines, "\n"))
	blocks.SetPlaceHolder("2026-11-02 09:00 120")
	local := container.NewVBox(container.NewGridWithColumns(2, shell.Labeled("Local effort (minutes)", estimate), shell.Labeled("Complexity", complexity)), shell.Labeled("Local notes", notes), shell.GoldChoice(done))
	schedule := container.NewVBox()
	current := backend.SprintIteration{}
	for _, s := range b.Iterations() {
		if s.ID == it.SprintID {
			current = s
		}
	}
	canPlan := current.ID != ""
	if canPlan {
		date := widget.NewEntry()
		if current.Contains(shell.Today()) {
			date.SetText(shell.Today())
		} else {
			date.SetText(current.StartDate)
		}
		clock := widget.NewEntry()
		clock.SetText("09:00")
		duration := widget.NewEntry()
		duration.SetText(strconv.Itoa(backend.PlannerDurationUp(backend.SprintEstimate(it, task))))
		schedule.Add(container.NewGridWithColumns(3, shell.Labeled("Date (YYYY-MM-DD)", date), shell.Labeled("Start (HH:MM)", clock), shell.Labeled("Minutes (15m steps)", duration)))
		schedule.Add(widget.NewButton("Add time block", func() {
			minutes, err := strconv.Atoi(strings.TrimSpace(duration.Text))
			if err != nil || !backend.ValidPlannerDuration(minutes) {
				k.Ui.Errf(fmt.Errorf("duration must use 15-minute increments (15, 30, 45, …)"))
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
	for _, o := range b.Field("Status").Options {
		statusOptions = append(statusOptions, o.Name)
	}
	status := widget.NewSelect(statusOptions, nil)
	status.SetSelected(backend.OrDefault(it.Status, "No status"))
	sprintOptions := []string{"No sprint"}
	ids := []string{""}
	for _, s := range b.Iterations() {
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
	applyProgress := shell.NewLoadingIndicator()
	applyState := widget.NewLabel("")
	applyState.Wrapping = fyne.TextWrapWord
	applyState.Hide()
	var applyStatus, applySprint *widget.Button
	apply := func(kind, value string) {
		started := MoveThen(k, b, it, kind, value, func() {
			applyState.Hide()
			applyProgress.ShowProgress(true, backend.FetchProgress{Stage: "Saving " + kind + " to GitHub: " + backend.FieldValueLabel(b, kind, value)})
			applyStatus.Disable()
			applySprint.Disable()
		}, func(err error) {
			applyProgress.SetActive(false)
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
			applyState.SetText(moveResult(it, shell.KanbanPending{Kind: kind, Label: backend.FieldValueLabel(b, kind, value)}, err))
		})
		switch {
		case started:
		case k.Busy || k.Loading:
			applyState.SetText("Wait for the GitHub request to finish.")
			applyState.Show()
		default:
			applyState.SetText("Already " + backend.FieldValueLabel(b, kind, value) + " on GitHub.")
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
		applyProgress.View, applyState)
	title := widget.NewLabelWithStyle(it.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Wrapping = fyne.TextWrapWord
	subtitle := container.NewBorder(nil, nil, nil, shell.GithubHyperlink("Open issue", it.URL), widget.NewLabel(fmt.Sprintf("Project estimate: %dm | Local changes stay on this computer", backend.SprintEstimate(it, backend.SprintTask{}))))
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
	popup := dialog.NewCustomWithoutButtons(popupTitle, popupBody, k.Ui.Win)
	save := widget.NewButton("Save local plan", func() {
		n, err := strconv.Atoi(strings.TrimSpace(estimate.Text))
		if err != nil || n <= 0 || n > 1000000 {
			k.Ui.Errf(fmt.Errorf("effort must be between 1 and 1000000 minutes"))
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
					k.Ui.Errf(fmt.Errorf("schedule format: YYYY-MM-DD HH:MM MINUTES"))
					return
				}
				clock, err := time.Parse("15:04", parts[1])
				if err != nil {
					k.Ui.Errf(fmt.Errorf("enter time as HH:MM"))
					return
				}
				mins, err := strconv.Atoi(parts[2])
				if err != nil {
					k.Ui.Errf(fmt.Errorf("duration must be a number of minutes"))
					return
				}
				block := backend.PlanBlock{parts[0], clock.Hour()*60 + clock.Minute(), mins}
				if err := backend.ValidatePlannerDuration(block, task.Blocks); err != nil {
					k.Ui.Errf(err)
					return
				}
				if !current.Contains(block.Date) {
					k.Ui.Errf(fmt.Errorf("schedule dates must fall inside %s", current.Title))
					return
				}
				if err := backend.ValidatePlanBlock(key, block, k.Plan.Tasks); err != nil {
					k.Ui.Errf(err)
					return
				}
				for _, old := range t.Blocks {
					if old.Date == block.Date && old.Start < block.Start+block.Minutes && block.Start < old.Start+old.Minutes {
						k.Ui.Errf(fmt.Errorf("schedule blocks overlap"))
						return
					}
				}
				t.Blocks = append(t.Blocks, block)
			}
		}
		p := k.Plan
		tasks := map[string]backend.SprintTask{}
		for key, value := range p.Tasks {
			tasks[key] = value
		}
		tasks[key] = t
		p.Tasks = tasks
		if k.Save(p) {
			popup.Hide()
			k.SetMessage("Local plan saved on this computer.")
			Draw(k)
		}
	})
	actions := container.NewHBox(layout.NewSpacer(), widget.NewButton("Close", popup.Hide), save)
	popupBody.Add(container.NewBorder(container.NewVBox(title, subtitle), actions, nil, nil, columns))
	size := fyne.NewSize(1080, 560)
	if available := k.Ui.Win.Canvas().Size(); available.Width > 0 && available.Height > 0 {
		size.Width = fyne.Min(size.Width, available.Width*0.94)
		size.Height = fyne.Min(size.Height, available.Height*0.9)
	}
	popup.Resize(size)
	popup.Show()
}

type kanbanColumnLayout struct{}

// Fill the board viewport when there are few columns; retain horizontal
// scrolling once the readable minimum width no longer fits.
type KanbanColumnsLayout struct{}

func (KanbanColumnsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.Size{}
	}
	return fyne.NewSize(float32(len(objects))*260+float32(len(objects)-1)*theme.Padding(), 240)
}

func (KanbanColumnsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
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

func BubbleContents(k *shell.KanbanUI, title, foot fyne.CanvasObject, issueURL string) fyne.CanvasObject {
	body := container.NewVBox(title, foot)
	if issueURL != "" {
		body.Add(shell.GithubHyperlink("Open issue", issueURL))
	}
	return body
}
