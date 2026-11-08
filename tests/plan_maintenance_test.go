package tests

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/logwork"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func maintenanceBoard() backend.SprintBoard {
	b := backend.SprintBoard{ID: "p", Title: "Project", Fields: []backend.SprintField{{Name: "Status", Options: []backend.FieldOption{{ID: "c", Name: "Created"}, {ID: "ip", Name: "In Progress"}}}}}
	b.Items = []backend.SprintItem{
		{ID: "a", URL: "https://github.com/o/r/issues/1", Title: "planned today", Status: "Created"},
		{ID: "b", URL: "https://github.com/o/r/issues/2", Title: "already started", Status: "In Progress"},
		{ID: "c", URL: "https://github.com/o/r/issues/3", Title: "waiting on customer", Status: "Pending Customer Feedback"},
		{ID: "d", URL: "https://github.com/o/r/issues/4", Title: "planned tomorrow", Status: "Created"},
	}
	return b
}

func TestPruneSprintPlanDropsPastBlocksAndEmptyEntries(t *testing.T) {
	const day = "2026-11-03"
	b := maintenanceBoard()
	p := backend.SprintPlan{Boards: []backend.SprintBoard{b}, Tasks: map[string]backend.SprintTask{
		// Past block goes, today and later stay.
		"https://github.com/o/r/issues/1": {Blocks: []backend.PlanBlock{{Date: "2026-11-02", Start: 540, Minutes: 60}, {Date: day, Start: 540, Minutes: 60}, {Date: "2026-11-04", Start: 540, Minutes: 60}}},
		// Only past blocks and nothing else: the whole entry goes.
		"https://github.com/o/r/issues/2": {Blocks: []backend.PlanBlock{{Date: "2026-10-30", Start: 540, Minutes: 60}}},
		// Past blocks but a local estimate on an issue still on the board: kept, emptied.
		"https://github.com/o/r/issues/3": {Estimate: 90, Blocks: []backend.PlanBlock{{Date: "2026-10-30", Start: 540, Minutes: 60}}},
		// Off every board with nothing ahead: gone, notes and all.
		"https://github.com/o/r/issues/9": {Notes: "old", Done: true},
		// Off the board but still planned ahead: kept.
		"https://github.com/o/r/issues/8": {Blocks: []backend.PlanBlock{{Date: "2026-11-05", Start: 540, Minutes: 60}}},
	}}
	got, changed := backend.PruneSprintPlan(p, day)
	if !changed {
		t.Fatal("pruning reported no change")
	}
	if blocks := got.Tasks["https://github.com/o/r/issues/1"].Blocks; len(blocks) != 2 || blocks[0].Date != day {
		t.Fatalf("past block not dropped: %+v", blocks)
	}
	if _, ok := got.Tasks["https://github.com/o/r/issues/2"]; ok {
		t.Fatal("entry with only past blocks survived")
	}
	if task, ok := got.Tasks["https://github.com/o/r/issues/3"]; !ok || task.Estimate != 90 || len(task.Blocks) != 0 {
		t.Fatalf("estimate on a live issue lost: %+v %v", task, ok)
	}
	if _, ok := got.Tasks["https://github.com/o/r/issues/9"]; ok {
		t.Fatal("orphan entry survived")
	}
	if _, ok := got.Tasks["https://github.com/o/r/issues/8"]; !ok {
		t.Fatal("future plan for an off-board issue was dropped")
	}
	if len(p.Tasks["https://github.com/o/r/issues/1"].Blocks) != 3 {
		t.Fatal("pruning mutated the original plan")
	}
	if _, again := backend.PruneSprintPlan(got, day); again {
		t.Fatal("pruning is not idempotent")
	}
}

func TestPruneSprintPlanKeepsMetadataWhenNoBoardsAreCached(t *testing.T) {
	p := backend.SprintPlan{Tasks: map[string]backend.SprintTask{"x": {Estimate: 30}}}
	got, changed := backend.PruneSprintPlan(p, "2026-11-03")
	if changed || got.Tasks["x"].Estimate != 30 {
		t.Fatal("a missing board cache must not wipe local estimates")
	}
}

func TestPlannedTodayStillCreatedOnlyPicksCreatedIssuesPlannedToday(t *testing.T) {
	const day = "2026-11-03"
	b := maintenanceBoard()
	p := backend.SprintPlan{Boards: []backend.SprintBoard{b}, Tasks: map[string]backend.SprintTask{
		"https://github.com/o/r/issues/1": {Blocks: []backend.PlanBlock{{Date: day, Start: 540, Minutes: 60}}},
		"https://github.com/o/r/issues/2": {Blocks: []backend.PlanBlock{{Date: day, Start: 600, Minutes: 60}}},
		"https://github.com/o/r/issues/3": {Blocks: []backend.PlanBlock{{Date: day, Start: 660, Minutes: 60}}},
		"https://github.com/o/r/issues/4": {Blocks: []backend.PlanBlock{{Date: "2026-11-04", Start: 540, Minutes: 60}}},
	}}
	got := sprintcalendar.PlannedTodayStillCreated(p, day)
	if len(got) != 1 || got[0].Item.ID != "a" {
		t.Fatalf("want only the Created issue planned today, got %+v", got)
	}
	p.Tasks["https://github.com/o/r/issues/1"] = backend.SprintTask{Done: true, Blocks: []backend.PlanBlock{{Date: day, Start: 540, Minutes: 60}}}
	if len(sprintcalendar.PlannedTodayStillCreated(p, day)) != 0 {
		t.Fatal("a locally done issue was started")
	}
	p.Tasks["https://github.com/o/r/issues/1"] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: day, Start: 540, Minutes: 60}}}
	p.Boards[0].Fields[0].Options = []backend.FieldOption{{ID: "c", Name: "Created"}}
	if len(sprintcalendar.PlannedTodayStillCreated(p, day)) != 0 {
		t.Fatal("a board without In Progress was targeted")
	}
}

func TestSupportWeekFinishedAfterFriday(t *testing.T) {
	week := "2026-10-26" // Monday
	for date, want := range map[string]bool{
		"2026-10-26": false,
		"2026-10-30": false, // Friday itself
		"2026-10-31": true,  // Saturday
		"2026-11-02": true,
	} {
		if got := sprintcalendar.SupportWeekFinished(week, date); got != want {
			t.Fatalf("%s: got %v want %v", date, got, want)
		}
	}
}

func TestSupportWeekDroppedOnceEveryWeekdayIsLogged(t *testing.T) {
	ui := weekUI(t)
	ui.Cfg.DisplayName = "Alex"
	week := ui.WeekStart
	sprintcalendar.SetSupport(ui, true, week)
	byDay := map[string][]backend.WorklogItem{}
	days := shell.WeekDates(week)
	// Empty rota slots on every weekday, as the rota creates them up front:
	// nothing is logged yet, so the tick stays.
	for _, d := range days[:5] {
		byDay[d] = []backend.WorklogItem{{Title: "Alex: Weekly Support (12PM - 3PM)"}}
	}
	sprintcalendar.DropLoggedSupportWeek(ui, week, byDay, "ok")
	if !sprintcalendar.SupportOn(ui, week) {
		t.Fatal("dropped on empty rota slots")
	}
	for _, d := range days[:4] {
		byDay[d] = []backend.WorklogItem{{ParentTitle: "Alex: Weekly Support (12PM - 3PM)", Minutes: 180}}
	}
	sprintcalendar.DropLoggedSupportWeek(ui, week, byDay, "ok")
	if !sprintcalendar.SupportOn(ui, week) {
		t.Fatal("dropped while Friday is still unlogged")
	}
	byDay[days[4]] = []backend.WorklogItem{{ParentTitle: "Alex: Weekly Support (12PM - 3PM)", Minutes: 180}}
	sprintcalendar.DropLoggedSupportWeek(ui, week, byDay, "loading")
	if !sprintcalendar.SupportOn(ui, week) {
		t.Fatal("decided on data still loading")
	}
	sprintcalendar.DropLoggedSupportWeek(ui, week, byDay, "ok")
	if sprintcalendar.SupportOn(ui, week) || ui.Store.LoadSupportWeeks()[week] {
		t.Fatal("fully logged week was not dropped")
	}
}

func TestPruneSupportWeeksDropsOnlyFinishedWeeks(t *testing.T) {
	ui := weekUI(t)
	current := backend.AddDays(backend.WeekStartOf(shell.Today()), 7) // next week: never finished, even on a weekend
	sprintcalendar.SetSupport(ui, true, "2026-08-10", current)
	if !sprintcalendar.PruneSupportWeeks(ui) {
		t.Fatal("a finished week should be pruned")
	}
	if sprintcalendar.SupportOn(ui, "2026-08-10") || !sprintcalendar.SupportOn(ui, current) {
		t.Fatal("pruned the wrong week")
	}
	if saved := ui.Store.LoadSupportWeeks(); saved["2026-08-10"] {
		t.Fatal("pruned week still on disk")
	}
	if sprintcalendar.PruneSupportWeeks(ui) {
		t.Fatal("nothing left to prune, but reported a change")
	}
}

// Opening Kanban never starts issues: only the Sprint Calendar and app start
// mark the next fetch for it.
func TestOnlyCalendarMarksFetchForAutoStart(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	k.Loading = true // keep load from reaching GitHub
	b := &k.Plan.Boards[0]
	b.Fields = append(b.Fields, backend.SprintField{Name: "Status", Options: []backend.FieldOption{{ID: "ip", Name: "In Progress"}}})
	b.Items[0].Status = "Created"
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: shell.Today(), Start: 540, Minutes: 60}}}

	k.PrunePlan() // what opening Kanban does
	if k.AutoStartPending {
		t.Fatal("Kanban marked a fetch for auto-start")
	}
	sprintcalendar.Maintain(k) // app start or opening Sprint Calendar
	if !k.AutoStartPending {
		t.Fatal("Sprint Calendar did not mark the fetch for auto-start")
	}
	k.AutoStartPending = false
	k.Busy = true
	sprintcalendar.RefreshCalendarSources(k)
	if k.AutoStartPending {
		t.Fatal("left pending while a GitHub write blocked the fetch")
	}
}

// Picking another sprint changes the tray, not which plans show on the dates.
func TestCalendarShowsPlannedBubblesFromOtherSprints(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	b := &k.Plan.Boards[0]
	next := backend.SprintIteration{ID: "s2", Title: "Next", StartDate: backend.AddDays(c.Sprint.StartDate, 14), Duration: 14}
	b.Fields[0].Configuration.Iterations = append(b.Fields[0].Configuration.Iterations, next)
	other := backend.SprintItem{ID: "o", Title: "Other sprint", SprintID: "s2", Effort: 60}
	b.Items = append(b.Items, other)
	day := backend.AddDays(k.Week, 1)
	k.Plan.Tasks[other.Key(b.ID)] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: day, Start: 540, Minutes: 60}}}
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: k.Week, Start: 540, Minutes: 90}}}
	k.CalendarSprint = backend.SprintKey(c.Sprint)
	kanban.Draw(k)

	bubbles := map[string]int{}
	for _, d := range k.Dates {
		bubbles[d.Date] = countPlannerBubbles(d.Object)
	}
	if bubbles[k.Week] != 1 {
		t.Fatalf("selected sprint's bubble missing: %v", bubbles)
	}
	if bubbles[day] != 1 {
		t.Fatalf("other sprint's planned bubble hidden: %v", bubbles)
	}
	for _, cand := range sprintcalendar.UniqueCurrentItems(k) {
		if cand.Item.ID == "o" {
			t.Fatal("other sprint's issue leaked into the selected sprint's tray")
		}
	}
	for _, cand := range sprintcalendar.ScheduledItems(k) {
		if cand.Item.ID == "o" && cand.Sprint.ID != "s2" {
			t.Fatal("other sprint's bubble should keep its own sprint for dragging")
		}
	}
	if _, n := backend.ClearPlannerDay(k.Plan, sprintcalendar.ScheduledItems(k), day); n != 1 {
		t.Fatal("the day's bin should clear what the day shows")
	}
}

// MEETING is always in the tray, lands on any date whatever the sprint, and
// counts as planned time.
func TestMeetingBubbleIsAlwaysAvailable(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	kanban.Draw(k)
	found := false
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*shell.PlannerDragCard); ok && card.Index == -1 && card.Candidate.Key == shell.PlannerMeetingKey {
			found = true
		}
	})
	if !found {
		t.Fatal("MEETING missing from the tray")
	}
	outside := backend.AddDays(c.Sprint.StartDate, 60) // well past the selected sprint
	p, err := backend.MovePlannerIssue(k.Plan, shell.PlannerMeetingCandidate(), -1, outside)
	if err != nil {
		t.Fatal("meeting should land on any date:", err)
	}
	if b := p.Tasks[shell.PlannerMeetingKey].Blocks; len(b) != 1 || b[0].Date != outside || b[0].Minutes != 60 {
		t.Fatalf("unexpected meeting block: %+v", b)
	}
	if _, err := backend.MovePlannerIssue(k.Plan, c, -1, outside); err == nil {
		t.Fatal("a sprint issue should still be kept inside its sprint")
	}
	k.Plan = p
	if got := sprintcalendar.DayTotals(k, outside, sprintcalendar.ScheduledItems(k), sprintcalendar.PlannerDayWork{}).Planned; got != 60 {
		t.Fatalf("meeting time not counted as planned: %d", got)
	}
	if got := sprintcalendar.PlannedMinutes(ui, outside, nil, nil); got != 60 {
		t.Fatalf("Log Work would not see the meeting: %d", got)
	}
}

func TestPlannedBarPartsAddsPurpleAfterLoggedAndDrafts(t *testing.T) {
	parts := shell.PlannedBarParts(backend.Config{}, map[string]int{"org": 240}, map[string]int{"org": 60}, 120)
	last := parts[len(parts)-1]
	if last.Key != "@plan" || last.Fill != shell.PlannerPlanColor || last.Weight != float32(120)/480 {
		t.Fatalf("purple planned part missing or wrong: %+v", parts)
	}
	over := shell.PlannedBarParts(backend.Config{}, map[string]int{"org": 480}, nil, 120)
	total := float32(0)
	for _, p := range over {
		total += p.Weight
	}
	if total > 1.0001 {
		t.Fatalf("bar overflows when the day is over target: %v", total)
	}
	if none := shell.PlannedBarParts(backend.Config{}, map[string]int{"org": 240}, nil, 0); len(none) != 1 {
		t.Fatalf("no plan should leave the bar as it was: %+v", none)
	}
}

// Log Work turns a day green once logged + drafts + planned reach 480, and keeps
// its draft figure apart from the plans.
func TestLogWorkDayGreenWhenPlannedFillsTheDay(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.Kanban
	ds := k.Week
	drafts := []backend.Row{{"id": "a", "date": ds, "minutes": "300"}}
	before := logwork.WeekColumn(ui, ds, nil, drafts)
	if before.BaseFill != shell.DayFill(0, 300) {
		t.Fatal("300 min of drafts alone should not be green")
	}
	k.Plan.Tasks[shell.PlannerMeetingKey] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: ds, Start: 540, Minutes: 180}}}
	after := logwork.WeekColumn(ui, ds, nil, drafts)
	if after.BaseFill != shell.DayFill(0, 480) || after.Planned != 180 {
		t.Fatalf("drafts + planned at 480 should turn the day green: planned=%d", after.Planned)
	}
	if after.Waiting != 300 {
		t.Fatal("planned time leaked into the draft figure the captions read")
	}
}

// Log List colours its days by the same totals as Log Work: green once
// logged + drafts + planned reach 480.
func TestDayTotalFillCountsPlannedTime(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.Kanban
	ds := k.Week
	drafts := []backend.Row{{"id": "a", "date": ds, "minutes": "300"}}
	if got := ui.DayTotalFill(ds, nil, drafts); got != shell.DayFill(0, 300) {
		t.Fatal("300 min of drafts alone should not be green")
	}
	k.Plan.Tasks[shell.PlannerMeetingKey] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: ds, Start: 540, Minutes: 180}}}
	if got := ui.DayTotalFill(ds, nil, drafts); got != shell.DayFill(0, 480) {
		t.Fatal("drafts + planned at 480 should turn the day green")
	}
}

// Today's column on the Commit List (and the meeting update, which shares it)
// is outlined in gold, the rest stay plain and none is filled.
func TestCommitListOutlinesTodayInGold(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	days := shell.WeekDates(backend.WeekStartOf(shell.Today()))
	gold, plain := 0, 0
	walkCanvas(ui.CommitDaysPanel(days, nil), func(o fyne.CanvasObject) {
		r, ok := o.(*canvas.Rectangle)
		if !ok || r.CornerRadius != shell.CellCornerRadius {
			return
		}
		if r.FillColor != color.Transparent {
			t.Fatal("Commit List days are not coloured in")
		}
		if r.StrokeColor == widgets.LogoGold {
			gold++
		} else {
			plain++
		}
	})
	if gold != 1 || plain != len(days)-1 {
		t.Fatalf("want today alone in gold: gold=%d plain=%d", gold, plain)
	}
}
