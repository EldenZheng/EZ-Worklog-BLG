package sprintcalendar

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// ScheduledItems is every issue the calendar draws bubbles for: the selected
// sprint's issues plus any issue on a visible board with a planned block, in
// whatever sprint it sits. Switching sprints changes the tray, not which plans
// can be seen on the dates. Each keeps its own sprint, so a drag still only
// lands inside that issue's sprint.
func ScheduledItems(k *shell.KanbanUI) []backend.SprintCandidate {
	out := append(UniqueCurrentItems(k), shell.PlannerMeetingCandidate())
	seen := map[string]bool{}
	for _, c := range out {
		seen[c.Key] = true
	}
	for _, b := range k.Plan.Boards {
		if !k.VisibleBoard(b) {
			continue
		}
		for _, it := range b.Items {
			key := it.Key(b.ID)
			if seen[key] || len(k.Plan.Tasks[key].Blocks) == 0 {
				continue
			}
			seen[key] = true
			var sprint backend.SprintIteration
			for _, s := range b.Iterations() {
				if s.ID == it.SprintID {
					sprint = s
				}
			}
			out = append(out, backend.SprintCandidate{key, it, sprint})
		}
	}
	return out
}

func UniqueCurrentItems(k *shell.KanbanUI) []backend.SprintCandidate {
	seen := map[string]bool{}
	var out []backend.SprintCandidate
	for _, c := range k.CalendarItems() {
		if !seen[c.Key] {
			out = append(out, c)
			seen[c.Key] = true
		}
	}
	return out
}

func Calendar(k *shell.KanbanUI) fyne.CanvasObject {
	k.BlockMarks = map[string][]shell.PlannerBlockMark{}
	loadCalendarWorklogs(k, false)
	work := CalendarWorkData(k)
	// A period shown for the first time, or the tab opened, arrives as Kanban's
	// cards do: each day uncovered from the top down, column after column, left
	// to right. Redraws of the same period (a drop, a resize) leave it be.
	from, to := calendarRange(k)
	var arrive *shell.Arrival
	if work.State != "loading" && !k.Loading {
		arrive = k.Ui.Arrival("planner", fmt.Sprint(k.CalendarMode, from, to))
	}
	var arriving []*shell.Reveal
	var delays []time.Duration
	place := func(day fyne.CanvasObject, row, col int) fyne.CanvasObject {
		if arrive == nil {
			return day
		}
		r := shell.NewReveal(day)
		arriving = append(arriving, r)
		delays = append(delays, shell.GridDelays(1, 7, row*7+col)[0])
		return r
	}
	groups := calendarSprintChoices(k)
	var options, keys []string
	for _, g := range groups {
		options = append(options, CalendarSprintLabel(g.Sprint))
		keys = append(keys, g.Key)
	}
	sprintSelect := shell.NewGoldSelect(options, nil)
	if len(options) == 0 {
		sprintSelect.PlaceHolder = "No sprints"
		sprintSelect.Disable()
	} else {
		selected := k.CalendarSprint
		if selected == "" || selected == "current" {
			selected = backend.SprintKey(selectedCalendarSprint(k))
		}
		sprintSelect.Selected = options[0]
		for i, key := range keys {
			if key == selected {
				sprintSelect.Selected = options[i]
			}
		}
	}
	sprintSelect.OnChanged = func(v string) {
		for i, label := range options {
			if label == v {
				p := k.Plan
				p.CalendarSprint = keys[i]
				if !k.Save(p) {
					return
				}
				k.CalendarSprint = keys[i]
				for _, g := range groups {
					if g.Key == keys[i] {
						k.Week = backend.WeekStartOf(g.Sprint.StartDate)
						k.Month = g.Sprint.StartDate[:7]
					}
				}
				shell.Hooks.KanbanUIDraw(k)
				return
			}
		}
	}

	mode := shell.NewGoldSelect([]string{"Week", "Sprint", "Month"}, func(v string) {
		p := k.Plan
		p.CalendarMode = v
		if k.Save(p) {
			k.CalendarMode = v
			shell.Hooks.KanbanUIDraw(k)
		}
	})
	mode.Selected = k.CalendarMode
	if mode.Selected == "" {
		mode.Selected = "Week"
	}
	walk := func(step int) {
		if k.CalendarMode == "Sprint" {
			selected := selectedCalendarSprint(k)
			for i, g := range groups {
				if g.Key == backend.SprintKey(selected) {
					next := i + step
					if next >= 0 && next < len(groups) {
						sprintSelect.SetSelected(options[next])
					}
					return
				}
			}
			return
		}
		if k.CalendarMode == "Month" {
			t, _ := time.Parse("2006-01", k.Month)
			k.Month = t.AddDate(0, step, 0).Format("2006-01")
		} else {
			k.Week = backend.AddDays(k.Week, step*7)
		}
		shell.Hooks.KanbanUIDraw(k)
	}
	period := k.Week + " – " + backend.AddDays(k.Week, 6)
	if k.CalendarMode == "Month" {
		t, _ := time.Parse("2006-01", k.Month)
		period = t.Format("January 2006")
	}
	if k.CalendarMode == "Sprint" {
		selected := selectedCalendarSprint(k)
		if selected.ID != "" {
			period = selected.Title + " | " + shell.SprintDateLabel(selected)
		}
	}
	var sprintControl fyne.CanvasObject = container.NewThemeOverride(sprintSelect, widgets.DropdownTheme{Theme: theme.Current()})
	if selected := selectedCalendarSprint(k); selected.ID != "" && selected.Contains(shell.Today()) {
		sprintControl = container.NewThemeOverride(sprintSelect, widgets.DropdownTheme{Theme: theme.Current(), Current: true})
	}
	today := widget.NewButton("Today", func() {
		k.Week = backend.WeekStartOf(shell.Today())
		k.Month = shell.ThisMonth()
		shell.Hooks.KanbanUIDraw(k)
	})
	// Nothing to jump to when today is already in view.
	onToday := k.Week == backend.WeekStartOf(shell.Today())
	switch k.CalendarMode {
	case "Month":
		onToday = k.Month == shell.ThisMonth()
	case "Sprint":
		onToday = selectedCalendarSprint(k).Contains(shell.Today())
	}
	if onToday {
		today.Disable()
	}
	// The shown sprints and their days left, filled in below once read, sit
	// just after Today.
	sprintInfo := widget.NewLabel("")
	header := container.New(&widgets.WrapRowLayout{}, container.NewHBox(container.NewThemeOverride(mode, widgets.DropdownTheme{Theme: theme.Current()}), container.NewGridWrap(fyne.NewSize(360, 40), sprintControl), widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() { walk(-1) }), widget.NewLabelWithStyle(period, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() { walk(1) }), today), sprintInfo)
	auto := widget.NewButton("Allocate unscheduled", func() {
		p := k.Plan
		var unplaced []string
		p.Tasks, unplaced = backend.AllocateSprint(p, k.OpenCalendarItems(), shell.Today())
		if k.Save(p) {
			shell.Hooks.KanbanUIDraw(k)
			k.SetMessage("Local schedule saved · weekdays 09:00–17:00; existing appointments preserved.")
			if len(unplaced) > 0 {
				shell.ShowInformation("Sprint capacity", fmt.Sprintf("%d issues did not fit before the sprint ends and remain unscheduled. Adjust the local estimate or add time blocks manually.\n\n%s", len(unplaced), strings.Join(unplaced, "\n")), k.Ui.Win)
			}
		}
	})
	items := UniqueCurrentItems(k)
	scheduled := ScheduledItems(k)
	clear := widget.NewButtonWithIcon("Clear", theme.DeleteIcon(), func() {
		_, count := backend.ClearSprintSchedule(k.Plan, items)
		if count == 0 {
			k.SetMessage("No local blocks to clear in the selected sprint.")
			return
		}
		ConfirmClear(k, "Clear local schedule", fmt.Sprintf("Remove %d planned blocks from the selected sprint's visible projects? Local estimates and notes are kept; GitHub is unchanged.", count), func() {
			p, _ := backend.ClearSprintSchedule(k.Plan, UniqueCurrentItems(k))
			if k.Save(p) {
				k.SetMessage("Selected sprint's local schedule cleared.")
				shell.Hooks.KanbanUIDraw(k)
			}
		})
	})
	tools := container.NewHBox(clear, auto)
	toolbar := container.New(widgets.NewSidesLayout(header, tools, k.CalendarBody.Size().Width), header, tools)
	var dates fyne.CanvasObject
	if k.CalendarMode == "Month" || k.CalendarMode == "Sprint" {
		weeks := container.New(PlannerRowsLayout{})
		first := k.Month + "-01"
		start := backend.WeekStartOf(first)
		monthStart, _ := time.Parse("2006-01-02", first)
		end := monthStart.AddDate(0, 1, -1).Format("2006-01-02")
		if k.CalendarMode == "Sprint" {
			selected := selectedCalendarSprint(k)
			if selected.ID != "" {
				start = backend.WeekStartOf(selected.StartDate)
				end = backend.AddDays(selected.StartDate, selected.Duration-1)
			}
		}
		for week, date := 0, start; date <= end; week, date = week+1, backend.AddDays(date, 7) {
			scale := &plannerScale{}
			row := container.NewGridWithColumns(7)
			for d := 0; d < 7; d++ {
				day := backend.AddDays(date, d)
				outside := k.CalendarMode == "Month" && !strings.HasPrefix(day, k.Month)
				if k.CalendarMode == "Sprint" {
					selected := selectedCalendarSprint(k)
					outside = selected.ID != "" && !selected.Contains(day)
				}
				if outside {
					row.Add(widget.NewLabel(""))
				} else {
					row.Add(place(calendarDay(k, day, scheduled, scale, work.Days[day]), week, d))
				}
			}
			weeks.Add(newPlannerWeek(row, scale, false))
		}
		if k.CalendarMode == "Month" {
			scroll := newPlannerFastScroll(plannerScrollInset(weeks))
			dates = scroll
			k.CalendarClip = scroll
		} else {
			scroll := container.NewVScroll(plannerScrollInset(weeks))
			dates = scroll
			k.CalendarClip = scroll
		}
	} else {
		scale := &plannerScale{}
		row := container.NewGridWithColumns(7)
		for d := 0; d < 7; d++ {
			day := backend.AddDays(k.Week, d)
			row.Add(place(calendarDay(k, day, scheduled, scale, work.Days[day]), 0, d))
		}
		scroll := container.NewVScroll(plannerScrollInset(newPlannerWeek(row, scale, true)))
		dates = scroll
		k.CalendarClip = scroll
	}
	arrive.Cascade(delays, func(i int, share float32) { arriving[i].SetShare(share) })
	weekdays := container.NewGridWithColumns(7)
	for _, name := range []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"} {
		weekdays.Add(widget.NewLabelWithStyle(name, fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}))
	}
	axisSpacer := canvas.NewRectangle(color.Transparent)
	axisSpacer.SetMinSize(fyne.NewSize(plannerRulerWidth, 0))
	dates = container.NewBorder(container.NewBorder(nil, nil, axisSpacer, nil, weekdays), nil, nil, nil, dates)
	// The Issues | Pull Request key filters the tray, as Kanban's filters its
	// cards; blocks already on the calendar stay.
	var open []backend.SprintCandidate
	for _, c := range k.OpenCalendarItems() {
		if k.VisibleCalendarType(c.Item) {
			open = append(open, c)
		}
	}
	trayItems := append([]backend.SprintCandidate{shell.PlannerMeetingCandidate()}, open...)
	var unscheduled []fyne.CanvasObject
	for _, c := range trayItems {
		unscheduled = append(unscheduled, PlannerCard(k, c, -1))
	}
	issues := len(open)
	grid := container.NewVBox(unscheduled...)
	noMatch := widget.NewLabel("No issues match the search.")
	noMatch.Wrapping = fyne.TextWrapWord
	grid.Add(noMatch)
	// The search hides the cards that do not match in place, so the field
	// keeps its focus while typing.
	filterTray := func(query string) {
		k.CalendarSearch = query
		found := false
		for i, c := range trayItems {
			show := TrayMatches(c, query)
			shell.ShowIf(unscheduled[i], show)
			found = found || show
		}
		shell.ShowIf(noMatch, !found && issues > 0)
		grid.Refresh()
	}
	search := shell.NewSearchEntry("Search issues", k.CalendarSearch, filterTray)
	filterTray(k.CalendarSearch)
	if issues == 0 {
		empty := widget.NewLabel("No active issues in the selected sprint.")
		empty.Wrapping = fyne.TextWrapWord
		grid.Add(empty)
	}
	// Gold, with the count at the right in the same badge as Kanban's, then
	// the search, with room under it before the capsule's edge.
	trayHeading := canvas.NewText("Sprint Issues", widgets.LogoGold)
	trayHeading.TextStyle = fyne.TextStyle{Bold: true}
	trayTitle := container.NewVBox(
		container.NewBorder(nil, nil, container.NewCenter(trayHeading), container.NewCenter(shell.CountBadge(issues))),
		container.New(layout.NewCustomPaddedLayout(0, theme.Padding()*2, 0, 0), search))
	// The heading floats in its own capsule over the top of the cards, which
	// scroll the tray's full height and pass behind it, as a Kanban column's
	// cards pass behind its title.
	capsule := canvas.NewRectangle(shell.BlendColor(theme.Color(theme.ColorNameBackground), theme.Color(theme.ColorNameForeground), 0.14))
	capsule.CornerRadius = 8
	pad := theme.Padding()
	heading := container.New(layout.NewCustomPaddedLayout(pad, 0, pad, pad), container.NewStack(capsule, container.NewPadded(trayTitle)))
	inset := &shell.ColumnInset{}
	// The tray keeps its own width whatever its cards hold: they wrap and
	// shorten to it rather than pushing the calendar to the right.
	trayScroll := container.NewVScroll(container.New(inset, container.New(trayWidthLayout{}, container.New(layout.NewCustomPaddedLayout(0, theme.InnerPadding(), theme.InnerPadding(), theme.InnerPadding()), grid))))
	trayScroll.SetMinSize(fyne.NewSize(trayWidth, 0))
	k.CalendarIssueClip = trayScroll
	tray := container.New(&shell.FloatingHeaderLayout{Inset: inset, Gap: theme.InnerPadding() + 2}, shell.NoScrollShadow(trayScroll), heading)
	// The issue tray is its own menu, so it sits in a rounded panel like
	// Settings; the issues scroll inside it while the panel stays put.
	sidebar := shell.PanelFrame(tray)
	var summaries []string
	seenSprints := map[string]bool{}
	for _, b := range k.Plan.Boards {
		if !k.VisibleBoard(b) {
			continue
		}
		s := b.Current(shell.Today())
		if k.CalendarSprint != "" && k.CalendarSprint != "current" {
			s = b.SprintByKey(k.CalendarSprint)
		}
		summary := shell.SprintSummary(s)
		if s.ID != "" && !seenSprints[summary] {
			summaries = append(summaries, summary)
			seenSprints[summary] = true
		}
	}
	sprintInfo.SetText(strings.Join(summaries, "\n"))
	periodNote := widget.NewLabel("")
	switch work.State {
	case "loading":
		periodNote.SetText("Loading GitHub worklogs…")
	case "off":
		periodNote.SetText("Set Worklog project URLs in Settings to load worked time.")
	case "error":
		periodNote.SetText("Worklogs: " + shell.Truncate(work.detail, 180))
	}
	periodNote.Wrapping = fyne.TextWrapWord
	// A bold gold "Support Week" in place of the checkbox when the rota puts
	// the user on support in the period shown.
	rota := false
	for _, day := range work.Days {
		rota = rota || shell.OnSupportRota(k.Ui.Cfg, day.Logged)
	}
	support := widget.NewCheck(shell.SupportCheckText(false), nil)
	// Saved per week, shared with Log Work: ticked when every week shown is on,
	// and ticking it sets every week in the shown period.
	supportWeeks := supportDates(calendarRange(k))
	allOn := len(supportWeeks) > 0
	for _, w := range supportWeeks {
		allOn = allOn && SupportOn(k.Ui, w)
	}
	support.SetChecked(allOn)
	support.OnChanged = func(on bool) {
		SetSupport(k.Ui, on, supportWeeks...)
		shell.Hooks.DrawWeekStrip(k.Ui)
		shell.Hooks.KanbanUIDraw(k)
	}
	// The project key sits on the support checkbox's line, as Kanban and Log
	// Work keep theirs by their controls.
	// The legend scrolls sideways, so it takes the row's free width; beside the
	// checkbox as a left part it was squeezed to nothing.
	types := shell.ItemTypeControls(k.VisibleCalendarType, func(selected []string) {
		chosen := append([]string{}, selected...)
		p := k.Plan
		p.CalendarTypes = &chosen
		if k.Save(p) {
			DrawKeepingCalendarScroll(k)
		}
	})
	noteRow := container.NewBorder(nil, nil, nil, container.NewHBox(types, shell.SupportChoice(support, rota).View), k.ProjectLegend(true))
	shell.ShowIf(periodNote, periodNote.Text != "")
	legend := container.NewBorder(nil, nil, nil, shell.WeekCaption("Projects: GitHub · Gold: drafts/support · Purple: plans"), nil)
	return container.NewBorder(container.NewVBox(toolbar, periodNote, noteRow), legend, sidebar, nil, dates)
}

func calendarDay(k *shell.KanbanUI, date string, items []backend.SprintCandidate, scale *plannerScale, work PlannerDayWork) fyne.CanvasObject {
	day, _ := time.Parse("2006-01-02", date)
	list := container.New(plannerDayBlocksLayout{scale})
	hover := &shell.PlannerDayHover{View: k.Ui.Win.Canvas()}
	hover.ExtendBaseWidget(hover)
	hover.Clear = newPlannerDayClear(k, date, hover)
	hover.HoverHeight = plannerDayBarBottom
	type appointment struct {
		c            backend.SprintCandidate
		index, start int
	}
	var appointments []appointment
	for _, c := range items {
		for index, b := range k.Plan.Tasks[c.Key].Blocks {
			if b.Date == date {
				appointments = append(appointments, appointment{c, index, b.Start})
			}
		}
	}
	sort.SliceStable(appointments, func(i, j int) bool { return appointments[i].start < appointments[j].start })
	numbers := map[string]map[int][2]int{}
	for _, a := range appointments {
		if numbers[a.c.Key] == nil {
			numbers[a.c.Key] = blockNumbers(k.Plan.Tasks[a.c.Key].Blocks, date)
		}
		nc := numbers[a.c.Key][a.index]
		card := plannerCardTitled(k, a.c, a.index, scale, blockTitle(issueTitle(k, a.c.Item), nc[0], nc[1])).(*shell.PlannerDragCard)
		card.Day = hover
		face := card.Content.(*fyne.Container).Objects[0].(*PlannerTimedFace)
		face.day = hover
		k.BlockMarks[a.c.Key] = append(k.BlockMarks[a.c.Key], face)
		list.Add(card)
	}
	hover.CanClear = len(appointments) > 0
	totals := DayTotals(k, date, items, work)
	projected := totals.Logged + totals.Draft + totals.Planned + totals.support
	dateLabel := fmt.Sprint(day.Day())
	if date == shell.Today() {
		dateLabel += " - Today"
	}
	caption := widget.NewLabelWithStyle(dateLabel, fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	var dateCaption fyne.CanvasObject = caption
	if date == shell.Today() {
		dateCaption = container.NewThemeOverride(caption, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	}
	progress := k.Ui.ProgressBarParts("planner/"+fmt.Sprint(day.Day()), date, totals.parts(k.ColorConfig()), 6)
	draftLabel := fmt.Sprintf("%dm draft", totals.Draft)
	if totals.support > 0 {
		draftLabel = fmt.Sprintf("%dm draft/support", totals.Draft+totals.support)
	}
	reading := container.New(shell.SplitCaption{}, shell.WeekCaption(fmt.Sprintf("%dm GitHub", totals.Logged)), shell.WeekCaption(draftLabel))
	planCaption := shell.WeekCaption(fmt.Sprintf("%dm planned", totals.Planned))
	planCaption.Color = shell.PlannerPlanColor
	// What the day still has room for once GitHub, the drafts and the plans
	// are counted, on the planned line.
	left := container.New(shell.SplitCaption{}, planCaption, shell.WeekCaption(fmt.Sprintf("%dm left", max(0, backend.Target-projected))))
	heading := container.New(plannerDayHeaderLayout{}, dateCaption, shell.WeekCaption(PlannerDayPercentage(projected)), progress, hover.ClearView, reading, left)
	hover.Header = heading
	base := shell.DayFill(totals.Logged, totals.Draft+totals.support+totals.Planned)
	bg := canvas.NewRectangle(base)
	bg.CornerRadius = shell.CellCornerRadius
	// The plain edge Log Work's days have; the green fill says a day is done,
	// and only today is edged in gold.
	bg.StrokeWidth = 1
	bg.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	if date == shell.Today() {
		bg.StrokeWidth = 2
		bg.StrokeColor = widgets.LogoGold
	}
	inner := container.New(plannerDayContentsLayout{}, heading, list)
	hover.Body = container.NewStack(bg, inner)
	var obj fyne.CanvasObject = hover
	k.Dates = append(k.Dates, &shell.PlannerDateTarget{Date: date, Object: obj, Frame: bg, Base: base, StrokeColor: bg.StrokeColor, StrokeWidth: bg.StrokeWidth})
	return obj
}

func PlannerCard(k *shell.KanbanUI, c backend.SprintCandidate, index int) fyne.CanvasObject {
	return plannerCardWithScale(k, c, index, nil)
}

func plannerCardWithScale(k *shell.KanbanUI, c backend.SprintCandidate, index int, scale *plannerScale) fyne.CanvasObject {
	return plannerCardTitled(k, c, index, scale, "")
}

// plannerCardTitled is a card whose block is called title ("" for the issue's
// own title), as the numbered blocks of an issue planned twice in a day are.
func plannerCardTitled(k *shell.KanbanUI, c backend.SprintCandidate, index int, scale *plannerScale, title string) fyne.CanvasObject {
	b, it, ok := k.BoardItem(c)
	if !ok {
		return widget.NewLabel(c.Item.Title)
	}
	t := k.Plan.Tasks[c.Key]
	finished := backend.PlannerIssueFinished(it, t)
	duration := backend.SprintEstimate(it, t)
	compact := index >= 0
	// A pull request links to the issue it works on, and is named after it.
	linked := it
	linked.URL = issueLink(k, it)
	linked.Title = issueTitle(k, it)
	var content fyne.CanvasObject
	if c.Key == shell.PlannerMeetingKey {
		// The meeting in the sidebar is a template, not a task: its title is
		// enough.
		content = k.Bubble(b, it, "", compact)
	} else {
		// Kanban's card, with the issue's planned block count as a gold badge
		// at the right of its link, like the tray's own count.
		var badge fyne.CanvasObject
		if index < 0 && len(t.Blocks) > 0 {
			badge = shell.CountBadge(len(t.Blocks))
		}
		body := shell.CardBody(b, it, shell.CardOptions{Link: linked.URL, LinkLabel: shell.ShortIssueRef(linked.URL), Title: linked.Title, Estimate: duration, Badge: badge})
		if finished {
			body.Add(widget.NewLabel("Completed"))
		}
		content = shell.Hooks.KanbanUIBubbleSurface(k, b, it, body)
	}
	card := &shell.PlannerDragCard{K: k, Candidate: c, Index: index, Content: content}
	if compact && index < len(t.Blocks) {
		size := &PlannerDurationLayout{minutes: t.Blocks[index].Minutes, scale: scale}
		if title == "" {
			title = linked.Title
		}
		// A block's description rides where a project name would.
		var icon fyne.Resource
		if c.Key != shell.PlannerMeetingKey {
			icon = it.TypeIcon()
		}
		face := newPlannerTimedFace(title, t.Blocks[index].Note, linked.URL, t.Blocks[index].Minutes, k.Accent(b, it), icon)
		if c.Key == shell.PlannerMeetingKey {
			// A meeting has no issue to open: a click describes it instead.
			card.OnTap = func() { editMeetingNote(k, c, index) }
		}
		face.finished = finished
		face.onDelete = func() { confirmDeleteBlock(k, c, index) }
		card.Content = container.New(size, face)
		if !finished {
			grip := &PlannerResizeGrip{k: k, Key: c.Key, Index: index, Layout: size, card: card, Face: face}
			grip.ExtendBaseWidget(grip)
			card.Content.(*fyne.Container).Add(grip)
		}
	}
	card.ExtendBaseWidget(card)
	return card
}

func selectedCalendarSprint(k *shell.KanbanUI) backend.SprintIteration {
	for _, b := range k.Plan.Boards {
		if !k.VisibleBoard(b) {
			continue
		}
		s := b.Current(shell.Today())
		if k.CalendarSprint != "" && k.CalendarSprint != "current" {
			s = b.SprintByKey(k.CalendarSprint)
		}
		if s.ID != "" {
			return s
		}
	}
	return backend.SprintIteration{}
}

// TrayMatches reports whether a tray card answers the search: its title,
// owner/repo#number or link, ignoring case. An empty search matches all.
func TrayMatches(c backend.SprintCandidate, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	ref, _ := backend.IssueRefFromAny(c.Item.URL)
	for _, s := range []string{c.Item.Title, ref, c.Item.URL} {
		if strings.Contains(strings.ToLower(s), query) {
			return true
		}
	}
	return false
}

// trayWidth is the Sprint Issues tray's width, whatever its cards hold.
const trayWidth = float32(260)

// trayWidthLayout asks for no width of its own, so the tray's width is fixed
// and its one child is laid out to it.
type trayWidthLayout struct{}

func (trayWidthLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, objects[0].MinSize().Height)
}

func (trayWidthLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.Position{})
	objects[0].Resize(size)
}
