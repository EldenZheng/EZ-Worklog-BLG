package main

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type plannerDateTarget struct {
	date        string
	object      fyne.CanvasObject
	frame       *canvas.Rectangle
	base        color.Color
	strokeColor color.Color
	strokeWidth float32
}

var plannerAvailableBlue = color.NRGBA{R: 106, G: 186, B: 236, A: 255}

// The MEETING bubble is a local-only item that sits in the tray whatever sprint
// is selected, so time can be set aside for meetings on any date. It belongs to
// no board and no sprint, and nothing about it is sent to GitHub.
const plannerMeetingKey = "local/meeting"

var plannerMeetingBoard = sprintBoard{ID: "local", Title: "Meeting"}

func plannerMeetingCandidate() sprintCandidate {
	return sprintCandidate{
		Key:  plannerMeetingKey,
		Item: sprintItem{ID: "meeting", Title: "MEETING", Effort: 60},
		// Any date is inside the meeting's "sprint".
		Sprint: sprintIteration{ID: "meeting", Title: "Meeting", StartDate: "2000-01-01", Duration: 100 * 366},
	}
}

// scheduledItems is every issue the calendar draws bubbles for: the selected
// sprint's issues plus any issue on a visible board with a planned block, in
// whatever sprint it sits. Switching sprints changes the tray, not which plans
// can be seen on the dates. Each keeps its own sprint, so a drag still only
// lands inside that issue's sprint.
func (k *kanbanUI) scheduledItems() []sprintCandidate {
	out := append(k.uniqueCurrentItems(), plannerMeetingCandidate())
	seen := map[string]bool{}
	for _, c := range out {
		seen[c.Key] = true
	}
	for _, b := range k.plan.Boards {
		if !k.visibleBoard(b) {
			continue
		}
		for _, it := range b.Items {
			key := it.key(b.ID)
			if seen[key] || len(k.plan.Tasks[key].Blocks) == 0 {
				continue
			}
			seen[key] = true
			var sprint sprintIteration
			for _, s := range b.iterations() {
				if s.ID == it.SprintID {
					sprint = s
				}
			}
			out = append(out, sprintCandidate{key, it, sprint})
		}
	}
	return out
}

func (k *kanbanUI) uniqueCurrentItems() []sprintCandidate {
	seen := map[string]bool{}
	var out []sprintCandidate
	for _, c := range k.calendarItems() {
		if !seen[c.Key] {
			out = append(out, c)
			seen[c.Key] = true
		}
	}
	return out
}

func (k *kanbanUI) calendar() fyne.CanvasObject {
	k.loadCalendarWorklogs(false)
	work := k.calendarWorkData()
	groups := k.calendarSprintChoices()
	var options, keys []string
	for _, g := range groups {
		options = append(options, calendarSprintLabel(g.Sprint))
		keys = append(keys, g.Key)
	}
	sprintSelect := newGoldSelect(options, nil)
	if len(options) == 0 {
		sprintSelect.PlaceHolder = "No sprints"
		sprintSelect.Disable()
	} else {
		selected := k.calendarSprint
		if selected == "" || selected == "current" {
			selected = sprintKey(k.selectedCalendarSprint())
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
				p := k.plan
				p.CalendarSprint = keys[i]
				if !k.save(p) {
					return
				}
				k.calendarSprint = keys[i]
				for _, g := range groups {
					if g.Key == keys[i] {
						k.week = weekStartOf(g.Sprint.StartDate)
						k.month = g.Sprint.StartDate[:7]
					}
				}
				k.draw()
				return
			}
		}
	}

	mode := newGoldSelect([]string{"Week", "Sprint", "Month"}, func(v string) {
		p := k.plan
		p.CalendarMode = v
		if k.save(p) {
			k.calendarMode = v
			k.draw()
		}
	})
	mode.Selected = k.calendarMode
	if mode.Selected == "" {
		mode.Selected = "Week"
	}
	walk := func(step int) {
		if k.calendarMode == "Sprint" {
			selected := k.selectedCalendarSprint()
			for i, g := range groups {
				if g.Key == sprintKey(selected) {
					next := i + step
					if next >= 0 && next < len(groups) {
						sprintSelect.SetSelected(options[next])
					}
					return
				}
			}
			return
		}
		if k.calendarMode == "Month" {
			t, _ := time.Parse("2006-01", k.month)
			k.month = t.AddDate(0, step, 0).Format("2006-01")
		} else {
			k.week = addDays(k.week, step*7)
		}
		k.draw()
	}
	period := k.week + " – " + addDays(k.week, 6)
	if k.calendarMode == "Month" {
		t, _ := time.Parse("2006-01", k.month)
		period = t.Format("January 2006")
	}
	if k.calendarMode == "Sprint" {
		selected := k.selectedCalendarSprint()
		if selected.ID != "" {
			period = selected.Title + " | " + sprintDateLabel(selected)
		}
	}
	var sprintControl fyne.CanvasObject = container.NewThemeOverride(sprintSelect, dropdownTheme{Theme: theme.Current()})
	if selected := k.selectedCalendarSprint(); selected.ID != "" && selected.contains(today()) {
		sprintControl = container.NewThemeOverride(sprintSelect, dropdownTheme{Theme: theme.Current(), current: true})
	}
	header := container.NewHBox(container.NewThemeOverride(mode, dropdownTheme{Theme: theme.Current()}), container.NewGridWrap(fyne.NewSize(360, 40), sprintControl), widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() { walk(-1) }), widget.NewLabelWithStyle(period, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() { walk(1) }), widget.NewButton("Today", func() { k.week = weekStartOf(today()); k.month = thisMonth(); k.draw() }))
	auto := widget.NewButton("Allocate unscheduled", func() {
		p := k.plan
		var unplaced []string
		p.Tasks, unplaced = allocateSprint(p, k.calendarItems(), today())
		if k.save(p) {
			k.draw()
			k.setMessage("Local schedule saved · weekdays 09:00–17:00; existing appointments preserved.")
			if len(unplaced) > 0 {
				dialog.ShowInformation("Sprint capacity", fmt.Sprintf("%d issues did not fit before the sprint ends and remain unscheduled. Adjust the local estimate or add time blocks manually.\n\n%s", len(unplaced), strings.Join(unplaced, "\n")), k.ui.win)
			}
		}
	})
	items := k.uniqueCurrentItems()
	scheduled := k.scheduledItems()
	clear := widget.NewButtonWithIcon("Clear", theme.DeleteIcon(), func() {
		_, count := clearSprintSchedule(k.plan, items)
		if count == 0 {
			k.setMessage("No local blocks to clear in the selected sprint.")
			return
		}
		k.confirmClear("Clear local schedule", fmt.Sprintf("Remove %d planned blocks from the selected sprint's visible projects? Local estimates and notes are kept; GitHub is unchanged.", count), func() {
			p, _ := clearSprintSchedule(k.plan, k.uniqueCurrentItems())
			if k.save(p) {
				k.setMessage("Selected sprint's local schedule cleared.")
				k.draw()
			}
		})
	})
	toolbar := container.NewBorder(nil, nil, header, container.NewHBox(clear, auto), nil)
	var dates fyne.CanvasObject
	if k.calendarMode == "Month" || k.calendarMode == "Sprint" {
		weeks := container.New(plannerRowsLayout{})
		first := k.month + "-01"
		start := weekStartOf(first)
		monthStart, _ := time.Parse("2006-01-02", first)
		end := monthStart.AddDate(0, 1, -1).Format("2006-01-02")
		if k.calendarMode == "Sprint" {
			selected := k.selectedCalendarSprint()
			if selected.ID != "" {
				start = weekStartOf(selected.StartDate)
				end = addDays(selected.StartDate, selected.Duration-1)
			}
		}
		for date := start; date <= end; date = addDays(date, 7) {
			scale := &plannerScale{}
			row := container.NewGridWithColumns(7)
			for d := 0; d < 7; d++ {
				day := addDays(date, d)
				outside := k.calendarMode == "Month" && !strings.HasPrefix(day, k.month)
				if k.calendarMode == "Sprint" {
					selected := k.selectedCalendarSprint()
					outside = selected.ID != "" && !selected.contains(day)
				}
				if outside {
					row.Add(widget.NewLabel(""))
				} else {
					row.Add(k.calendarDay(day, scheduled, scale, work.days[day]))
				}
			}
			weeks.Add(newPlannerWeek(row, scale, false))
		}
		if k.calendarMode == "Month" {
			scroll := newPlannerFastScroll(plannerScrollInset(weeks))
			dates = scroll
			k.calendarClip = scroll
		} else {
			scroll := container.NewVScroll(plannerScrollInset(weeks))
			dates = scroll
			k.calendarClip = scroll
		}
	} else {
		scale := &plannerScale{}
		row := container.NewGridWithColumns(7)
		for d := 0; d < 7; d++ {
			day := addDays(k.week, d)
			row.Add(k.calendarDay(day, scheduled, scale, work.days[day]))
		}
		scroll := container.NewVScroll(plannerScrollInset(newPlannerWeek(row, scale, true)))
		dates = scroll
		k.calendarClip = scroll
	}
	weekdays := container.NewGridWithColumns(7)
	for _, name := range []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"} {
		weekdays.Add(widget.NewLabelWithStyle(name, fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}))
	}
	axisSpacer := canvas.NewRectangle(color.Transparent)
	axisSpacer.SetMinSize(fyne.NewSize(plannerRulerWidth, 0))
	dates = container.NewBorder(container.NewBorder(nil, nil, axisSpacer, nil, weekdays), nil, nil, nil, dates)
	unscheduled := []fyne.CanvasObject{k.plannerCard(plannerMeetingCandidate(), -1)}
	issues := 0
	for _, c := range items {
		t := k.plan.Tasks[c.Key]
		if !plannerIssueFinished(c.Item, t) {
			unscheduled = append(unscheduled, k.plannerCard(c, -1))
			issues++
		}
	}
	grid := container.NewVBox(unscheduled...)
	trayScroll := container.NewVScroll(grid)
	trayScroll.SetMinSize(fyne.NewSize(250, 0))
	k.calendarIssueClip = trayScroll
	var tray fyne.CanvasObject = trayScroll
	if issues == 0 {
		empty := widget.NewLabel("No active issues in the selected sprint.")
		empty.Wrapping = fyne.TextWrapWord
		grid.Add(empty)
	}
	trayTitle := widget.NewLabelWithStyle(fmt.Sprintf("Sprint issues (%d)", issues), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	trayNote := widget.NewLabel("Drag again to plan another day")
	trayNote.Wrapping = fyne.TextWrapWord
	sidebar := container.NewBorder(container.NewVBox(trayTitle, trayNote), nil, nil, nil, tray)
	var summaries []string
	seenSprints := map[string]bool{}
	for _, b := range k.plan.Boards {
		if !k.visibleBoard(b) {
			continue
		}
		s := b.current(today())
		if k.calendarSprint != "" && k.calendarSprint != "current" {
			s = b.sprintByKey(k.calendarSprint)
		}
		summary := sprintSummary(s)
		if s.ID != "" && !seenSprints[summary] {
			summaries = append(summaries, summary)
			seenSprints[summary] = true
		}
	}
	periodNote := widget.NewLabel(strings.Join(summaries, "\n"))
	switch work.state {
	case "loading":
		periodNote.SetText(periodNote.Text + "\nLoading GitHub worklogs…")
	case "off":
		periodNote.SetText(periodNote.Text + "\nSet Worklog project URLs in Settings to load worked time.")
	case "error":
		periodNote.SetText(periodNote.Text + "\nWorklogs: " + truncate(work.detail, 180))
	}
	periodNote.Wrapping = fyne.TextWrapWord
	support := widget.NewCheck(fmt.Sprintf("+%d min weekday support (display only)", supportPlaceholderMins), nil)
	// Saved per week, shared with Log Work: ticked when every week shown is on,
	// and ticking it sets every week in the shown period.
	supportWeeks := supportDates(k.calendarRange())
	allOn := len(supportWeeks) > 0
	for _, w := range supportWeeks {
		allOn = allOn && k.ui.supportOn(w)
	}
	support.SetChecked(allOn)
	support.OnChanged = func(on bool) {
		k.ui.setSupport(on, supportWeeks...)
		k.ui.drawWeekStrip()
		k.draw()
	}
	noteRow := container.NewBorder(nil, nil, nil, goldChoice(support), periodNote)
	legend := container.NewBorder(nil, nil, nil, weekCaption("Projects: GitHub · Gold: drafts/support · Purple: plans"), k.projectLegend(true))
	return container.NewBorder(container.NewVBox(toolbar, noteRow), legend, sidebar, nil, dates)
}

func (k *kanbanUI) calendarDay(date string, items []sprintCandidate, scale *plannerScale, work plannerDayWork) fyne.CanvasObject {
	day, _ := time.Parse("2006-01-02", date)
	list := container.New(plannerDayBlocksLayout{scale})
	hover := &plannerDayHover{view: k.ui.win.Canvas()}
	hover.ExtendBaseWidget(hover)
	hover.clear = newPlannerDayClear(k, date, hover)
	type appointment struct {
		c            sprintCandidate
		index, start int
	}
	var appointments []appointment
	for _, c := range items {
		for index, b := range k.plan.Tasks[c.Key].Blocks {
			if b.Date == date {
				appointments = append(appointments, appointment{c, index, b.Start})
			}
		}
	}
	sort.SliceStable(appointments, func(i, j int) bool { return appointments[i].start < appointments[j].start })
	for _, a := range appointments {
		card := k.plannerCardWithScale(a.c, a.index, scale).(*plannerDragCard)
		card.day = hover
		face := card.content.(*fyne.Container).Objects[0].(*plannerTimedFace)
		face.day = hover
		list.Add(card)
	}
	hover.canClear = len(appointments) > 0
	totals := k.dayTotals(date, items, work)
	projected := totals.logged + totals.draft + totals.planned + totals.support
	dateLabel := fmt.Sprint(day.Day())
	if date == today() {
		dateLabel += " - Today"
	}
	caption := widget.NewLabelWithStyle(dateLabel, fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	var dateCaption fyne.CanvasObject = caption
	if date == today() {
		dateCaption = container.NewThemeOverride(caption, statusHeaderTheme{Theme: theme.Current(), ink: logoGold})
	}
	progress := k.ui.progressBarParts("planner/"+fmt.Sprint(day.Day()), date, totals.parts(k.colorConfig()), 6)
	draftLabel := fmt.Sprintf("%dm draft", totals.draft)
	if totals.support > 0 {
		draftLabel = fmt.Sprintf("%dm draft/support", totals.draft+totals.support)
	}
	reading := container.New(splitCaption{}, weekCaption(fmt.Sprintf("%dm GitHub", totals.logged)), weekCaption(draftLabel))
	planCaption := weekCaption(fmt.Sprintf("%dm planned", totals.planned))
	planCaption.Color = plannerPlanColor
	heading := container.New(plannerDayHeaderLayout{}, dateCaption, weekCaption(plannerDayPercentage(projected)), progress, hover.clear, reading, planCaption)
	hover.header = heading
	base := dayFill(totals.logged, totals.draft+totals.support+totals.planned)
	bg := canvas.NewRectangle(base)
	bg.CornerRadius = cellCornerRadius
	bg.StrokeWidth = 1
	bg.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	if projected > 0 {
		bg.StrokeColor = logoGold
		if projected >= target {
			bg.StrokeColor = theme.Color(theme.ColorNameSuccess)
		}
	}
	if date == today() {
		bg.StrokeWidth = 2
		bg.StrokeColor = logoGold
	}
	inner := container.New(plannerDayContentsLayout{}, heading, list)
	hover.body = container.NewStack(bg, inner)
	var obj fyne.CanvasObject = hover
	k.dates = append(k.dates, &plannerDateTarget{date: date, object: obj, frame: bg, base: base, strokeColor: bg.StrokeColor, strokeWidth: bg.StrokeWidth})
	return obj
}

func (k *kanbanUI) boardItem(c sprintCandidate) (sprintBoard, sprintItem, bool) {
	if c.Key == plannerMeetingKey {
		return plannerMeetingBoard, plannerMeetingCandidate().Item, true
	}
	for _, b := range k.plan.Boards {
		for _, it := range b.Items {
			if it.key(b.ID) == c.Key {
				return b, it, true
			}
		}
	}
	return sprintBoard{}, sprintItem{}, false
}

func (k *kanbanUI) plannerCard(c sprintCandidate, index int) fyne.CanvasObject {
	return k.plannerCardWithScale(c, index, nil)
}
func (k *kanbanUI) plannerCardWithScale(c sprintCandidate, index int, scale *plannerScale) fyne.CanvasObject {
	b, it, ok := k.boardItem(c)
	if !ok {
		return widget.NewLabel(c.Item.Title)
	}
	t := k.plan.Tasks[c.Key]
	finished := plannerIssueFinished(it, t)
	duration := sprintEstimate(it, t)
	caption := fmt.Sprintf("%s · %dm\n%s", projectShortName(b), duration, orDefault(it.Complexity, "Local plan"))
	compact := index >= 0
	if compact && index < len(t.Blocks) {
		block := t.Blocks[index]
		caption = fmt.Sprintf("%s–%s · %dm\n%s", planClock(block.Start), planClock(block.Start+block.Minutes), block.Minutes, projectShortName(b))
	}
	if index < 0 && len(t.Blocks) > 0 {
		caption += fmt.Sprintf("\n%d blocks planned · drag to add", len(t.Blocks))
	}
	if finished {
		caption = "Completed · " + caption
	}
	content := k.bubble(b, it, caption, compact)
	card := &plannerDragCard{k: k, candidate: c, index: index, content: content}
	if compact && index < len(t.Blocks) {
		size := &plannerDurationLayout{minutes: t.Blocks[index].Minutes, scale: scale}
		face := newPlannerTimedFace(it.Title, projectShortName(b), it.URL, t.Blocks[index].Minutes, k.accent(b, it))
		face.finished = finished
		card.content = container.New(size, face)
		if !finished {
			grip := &plannerResizeGrip{k: k, key: c.Key, index: index, layout: size, card: card, face: face}
			grip.ExtendBaseWidget(grip)
			card.content.(*fyne.Container).Add(grip)
		}
	}
	card.ExtendBaseWidget(card)
	return card
}

type plannerDragCard struct {
	widget.BaseWidget
	k         *kanbanUI
	candidate sprintCandidate
	index     int // -1: unscheduled; otherwise move this block only
	content   fyne.CanvasObject
	lifted    bool
	hover     *plannerDateTarget
	day       *plannerDayHover
}

func (c *plannerDragCard) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.content)
}
func (c *plannerDragCard) Cursor() desktop.Cursor  { return grabCursor{} }
func (c *plannerDragCard) Tapped(*fyne.PointEvent) { c.k.openCandidate(c.candidate) }
func (c *plannerDragCard) Dragged(e *fyne.DragEvent) {
	k := c.k
	if plannerIssueFinished(c.candidate.Item, k.plan.Tasks[c.candidate.Key]) || k.calendarResizing {
		return
	}
	if !c.lifted {
		c.lifted = true
		k.calendarDrag = c
		b, it, _ := k.boardItem(c.candidate)
		bg := canvas.NewRectangle(blendColor(theme.Color(theme.ColorNameInputBackground), k.accent(b, it), 0.5))
		bg.CornerRadius = 10
		ghost := container.NewStack(bg, container.NewPadded(widget.NewLabel(truncate(c.candidate.Item.Title, 35))))
		ghost.Resize(ghost.MinSize())
		k.calendarGhost.Objects = []fyne.CanvasObject{ghost}
	}
	if len(k.calendarGhost.Objects) > 0 {
		k.calendarGhost.Objects[0].Move(e.AbsolutePosition.Subtract(fyne.CurrentApp().Driver().AbsolutePositionForObject(k.calendarGhost)).Add(fyne.NewPos(12, 12)))
		k.calendarGhost.Refresh()
	}
	if scroll := plannerScroll(k.calendarClip); scroll != nil && pointIn(k.calendarClip, e.AbsolutePosition) {
		origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(k.calendarClip)
		y := e.AbsolutePosition.Y - origin.Y
		offset := scroll.Offset
		if y < 42 {
			offset.Y -= 24
		} else if y > scroll.Size().Height-42 {
			offset.Y += 24
		}
		scroll.ScrollToOffset(offset)
	}
	c.highlightDates(&e.AbsolutePosition)
}

func (c *plannerDragCard) highlightDates(at *fyne.Position) {
	k := c.k
	c.hover = nil
	if k.calendarResizing || k.calendarDrag != nil && k.calendarDrag != c || c.index >= 0 && !c.lifted {
		return
	}
	for _, target := range k.dates {
		_, valid := plannerSlot(k.plan, c.candidate, c.index, target.date)
		target.frame.FillColor = target.base
		target.frame.StrokeWidth = target.strokeWidth
		target.frame.StrokeColor = target.strokeColor
		if valid {
			target.frame.FillColor = withAlpha(plannerAvailableBlue, 0.13)
			target.frame.StrokeColor = plannerAvailableBlue
			target.frame.StrokeWidth = 2
			if at != nil && pointIn(target.object, *at) && pointIn(k.calendarClip, *at) {
				c.hover = target
				target.frame.FillColor = withAlpha(plannerAvailableBlue, 0.25)
				target.frame.StrokeColor = logoGold
				target.frame.StrokeWidth = 4
			}
		}
		target.frame.Refresh()
	}
}

func (c *plannerDragCard) MouseIn(*desktop.MouseEvent) {
	if c.day != nil {
		c.day.enter(c)
	}
	if !c.lifted && c.index < 0 && c.k.calendarDrag == nil && !c.k.calendarResizing {
		c.highlightDates(nil)
	}
}
func (c *plannerDragCard) MouseMoved(*desktop.MouseEvent) {}
func (c *plannerDragCard) MouseOut() {
	if c.day != nil {
		c.day.leave(c)
	}
	if !c.lifted && c.index < 0 && c.k.calendarDrag == nil && !c.k.calendarResizing {
		c.resetDates()
	}
}
func (c *plannerDragCard) resetDates() {
	for _, t := range c.k.dates {
		t.frame.FillColor = t.base
		t.frame.StrokeWidth = t.strokeWidth
		t.frame.StrokeColor = t.strokeColor
		t.frame.Refresh()
	}
}

func (c *plannerDragCard) DragEnd() {
	if !c.lifted {
		return
	}
	c.lifted = false
	k := c.k
	if k.calendarDrag == c {
		k.calendarDrag = nil
	}
	k.calendarGhost.Objects = nil
	k.calendarGhost.Refresh()
	for _, t := range k.dates {
		t.frame.FillColor = t.base
		t.frame.StrokeWidth = t.strokeWidth
		t.frame.StrokeColor = t.strokeColor
		t.frame.Refresh()
	}
	target := c.hover
	c.hover = nil
	if target == nil {
		return
	}
	p, err := movePlannerIssue(k.plan, c.candidate, c.index, target.date)
	if err != nil {
		k.ui.errf(err)
		return
	}
	if k.save(p) {
		k.setMessage("Saved locally · GitHub estimates and dates were not changed.")
		k.drawKeepingCalendarScroll()
	}
}

func (k *kanbanUI) selectedCalendarSprint() sprintIteration {
	for _, b := range k.plan.Boards {
		if !k.visibleBoard(b) {
			continue
		}
		s := b.current(today())
		if k.calendarSprint != "" && k.calendarSprint != "current" {
			s = b.sprintByKey(k.calendarSprint)
		}
		if s.ID != "" {
			return s
		}
	}
	return sprintIteration{}
}
