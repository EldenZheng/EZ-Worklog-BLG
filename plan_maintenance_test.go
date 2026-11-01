package main

import (
	"testing"

	"fyne.io/fyne/v2"
)

func maintenanceBoard() sprintBoard {
	b := sprintBoard{ID: "p", Title: "Project", Fields: []sprintField{{Name: "Status", Options: []fieldOption{{ID: "c", Name: "Created"}, {ID: "ip", Name: "In Progress"}}}}}
	b.Items = []sprintItem{
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
	p := sprintPlan{Boards: []sprintBoard{b}, Tasks: map[string]sprintTask{
		// Past block goes, today and later stay.
		"https://github.com/o/r/issues/1": {Blocks: []planBlock{{"2026-11-02", 540, 60}, {day, 540, 60}, {"2026-11-04", 540, 60}}},
		// Only past blocks and nothing else: the whole entry goes.
		"https://github.com/o/r/issues/2": {Blocks: []planBlock{{"2026-10-30", 540, 60}}},
		// Past blocks but a local estimate on an issue still on the board: kept, emptied.
		"https://github.com/o/r/issues/3": {Estimate: 90, Blocks: []planBlock{{"2026-10-30", 540, 60}}},
		// Off every board with nothing ahead: gone, notes and all.
		"https://github.com/o/r/issues/9": {Notes: "old", Done: true},
		// Off the board but still planned ahead: kept.
		"https://github.com/o/r/issues/8": {Blocks: []planBlock{{"2026-11-05", 540, 60}}},
	}}
	got, changed := pruneSprintPlan(p, day)
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
	if _, again := pruneSprintPlan(got, day); again {
		t.Fatal("pruning is not idempotent")
	}
}

func TestPruneSprintPlanKeepsMetadataWhenNoBoardsAreCached(t *testing.T) {
	p := sprintPlan{Tasks: map[string]sprintTask{"x": {Estimate: 30}}}
	got, changed := pruneSprintPlan(p, "2026-11-03")
	if changed || got.Tasks["x"].Estimate != 30 {
		t.Fatal("a missing board cache must not wipe local estimates")
	}
}

func TestPlannedTodayStillCreatedOnlyPicksCreatedIssuesPlannedToday(t *testing.T) {
	const day = "2026-11-03"
	b := maintenanceBoard()
	p := sprintPlan{Boards: []sprintBoard{b}, Tasks: map[string]sprintTask{
		"https://github.com/o/r/issues/1": {Blocks: []planBlock{{day, 540, 60}}},
		"https://github.com/o/r/issues/2": {Blocks: []planBlock{{day, 600, 60}}},
		"https://github.com/o/r/issues/3": {Blocks: []planBlock{{day, 660, 60}}},
		"https://github.com/o/r/issues/4": {Blocks: []planBlock{{"2026-11-04", 540, 60}}},
	}}
	got := plannedTodayStillCreated(p, day)
	if len(got) != 1 || got[0].item.ID != "a" {
		t.Fatalf("want only the Created issue planned today, got %+v", got)
	}
	p.Tasks["https://github.com/o/r/issues/1"] = sprintTask{Done: true, Blocks: []planBlock{{day, 540, 60}}}
	if len(plannedTodayStillCreated(p, day)) != 0 {
		t.Fatal("a locally done issue was started")
	}
	p.Tasks["https://github.com/o/r/issues/1"] = sprintTask{Blocks: []planBlock{{day, 540, 60}}}
	p.Boards[0].Fields[0].Options = []fieldOption{{ID: "c", Name: "Created"}}
	if len(plannedTodayStillCreated(p, day)) != 0 {
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
		if got := supportWeekFinished(week, date); got != want {
			t.Fatalf("%s: got %v want %v", date, got, want)
		}
	}
}

func TestSupportWeekDroppedOnceEveryWeekdayIsLogged(t *testing.T) {
	ui := weekUI(t)
	ui.cfg.DisplayName = "Alex"
	week := ui.weekStart
	ui.setSupport(true, week)
	byDay := map[string][]WorklogItem{}
	days := weekDates(week)
	for _, d := range days[:4] {
		byDay[d] = []WorklogItem{{ParentTitle: "Alex: Weekly Support (12PM - 3PM)"}}
	}
	ui.dropLoggedSupportWeek(week, byDay, "ok")
	if !ui.supportOn(week) {
		t.Fatal("dropped while Friday is still unlogged")
	}
	byDay[days[4]] = []WorklogItem{{ParentTitle: "Alex: Weekly Support (12PM - 3PM)"}}
	ui.dropLoggedSupportWeek(week, byDay, "loading")
	if !ui.supportOn(week) {
		t.Fatal("decided on data still loading")
	}
	ui.dropLoggedSupportWeek(week, byDay, "ok")
	if ui.supportOn(week) || ui.store.loadSupportWeeks()[week] {
		t.Fatal("fully logged week was not dropped")
	}
}

func TestPruneSupportWeeksDropsOnlyFinishedWeeks(t *testing.T) {
	ui := weekUI(t)
	current := addDays(weekStartOf(today()), 7) // next week: never finished, even on a weekend
	ui.setSupport(true, "2026-08-10", current)
	if !ui.pruneSupportWeeks() {
		t.Fatal("a finished week should be pruned")
	}
	if ui.supportOn("2026-08-10") || !ui.supportOn(current) {
		t.Fatal("pruned the wrong week")
	}
	if saved := ui.store.loadSupportWeeks(); saved["2026-08-10"] {
		t.Fatal("pruned week still on disk")
	}
	if ui.pruneSupportWeeks() {
		t.Fatal("nothing left to prune, but reported a change")
	}
}

// Opening Kanban never starts issues: only the Sprint Calendar and app start
// mark the next fetch for it.
func TestOnlyCalendarMarksFetchForAutoStart(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	k.loading = true // keep load from reaching GitHub
	b := &k.plan.Boards[0]
	b.Fields = append(b.Fields, sprintField{Name: "Status", Options: []fieldOption{{ID: "ip", Name: "In Progress"}}})
	b.Items[0].Status = "Created"
	k.plan.Tasks[c.Key] = sprintTask{Blocks: []planBlock{{today(), 540, 60}}}

	k.prunePlan() // what opening Kanban does
	if k.autoStartPending {
		t.Fatal("Kanban marked a fetch for auto-start")
	}
	k.maintain() // app start or opening Sprint Calendar
	if !k.autoStartPending {
		t.Fatal("Sprint Calendar did not mark the fetch for auto-start")
	}
	k.autoStartPending = false
	k.busy = true
	k.refreshCalendarSources()
	if k.autoStartPending {
		t.Fatal("left pending while a GitHub write blocked the fetch")
	}
}

// Picking another sprint changes the tray, not which plans show on the dates.
func TestCalendarShowsPlannedBubblesFromOtherSprints(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	b := &k.plan.Boards[0]
	next := sprintIteration{ID: "s2", Title: "Next", StartDate: addDays(c.Sprint.StartDate, 14), Duration: 14}
	b.Fields[0].Configuration.Iterations = append(b.Fields[0].Configuration.Iterations, next)
	other := sprintItem{ID: "o", Title: "Other sprint", SprintID: "s2", Effort: 60}
	b.Items = append(b.Items, other)
	day := addDays(k.week, 1)
	k.plan.Tasks[other.key(b.ID)] = sprintTask{Blocks: []planBlock{{day, 540, 60}}}
	k.plan.Tasks[c.Key] = sprintTask{Blocks: []planBlock{{k.week, 540, 90}}}
	k.calendarSprint = sprintKey(c.Sprint)
	k.draw()

	bubbles := map[string]int{}
	for _, d := range k.dates {
		bubbles[d.date] = countPlannerBubbles(d.object)
	}
	if bubbles[k.week] != 1 {
		t.Fatalf("selected sprint's bubble missing: %v", bubbles)
	}
	if bubbles[day] != 1 {
		t.Fatalf("other sprint's planned bubble hidden: %v", bubbles)
	}
	for _, cand := range k.uniqueCurrentItems() {
		if cand.Item.ID == "o" {
			t.Fatal("other sprint's issue leaked into the selected sprint's tray")
		}
	}
	for _, cand := range k.scheduledItems() {
		if cand.Item.ID == "o" && cand.Sprint.ID != "s2" {
			t.Fatal("other sprint's bubble should keep its own sprint for dragging")
		}
	}
	if _, n := clearPlannerDay(k.plan, k.scheduledItems(), day); n != 1 {
		t.Fatal("the day's bin should clear what the day shows")
	}
}

// MEETING is always in the tray, lands on any date whatever the sprint, and
// counts as planned time.
func TestMeetingBubbleIsAlwaysAvailable(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	k.draw()
	found := false
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*plannerDragCard); ok && card.index == -1 && card.candidate.Key == plannerMeetingKey {
			found = true
		}
	})
	if !found {
		t.Fatal("MEETING missing from the tray")
	}
	outside := addDays(c.Sprint.StartDate, 60) // well past the selected sprint
	p, err := movePlannerIssue(k.plan, plannerMeetingCandidate(), -1, outside)
	if err != nil {
		t.Fatal("meeting should land on any date:", err)
	}
	if b := p.Tasks[plannerMeetingKey].Blocks; len(b) != 1 || b[0].Date != outside || b[0].Minutes != 60 {
		t.Fatalf("unexpected meeting block: %+v", b)
	}
	if _, err := movePlannerIssue(k.plan, c, -1, outside); err == nil {
		t.Fatal("a sprint issue should still be kept inside its sprint")
	}
	k.plan = p
	if got := k.dayTotals(outside, k.scheduledItems(), plannerDayWork{}).planned; got != 60 {
		t.Fatalf("meeting time not counted as planned: %d", got)
	}
	if got := ui.plannedMinutes(outside, nil, nil); got != 60 {
		t.Fatalf("Log Work would not see the meeting: %d", got)
	}
}

func TestPlannedBarPartsAddsPurpleAfterLoggedAndDrafts(t *testing.T) {
	parts := plannedBarParts(Config{}, map[string]int{"org": 240}, map[string]int{"org": 60}, 120)
	last := parts[len(parts)-1]
	if last.key != "@plan" || last.fill != plannerPlanColor || last.weight != float32(120)/480 {
		t.Fatalf("purple planned part missing or wrong: %+v", parts)
	}
	over := plannedBarParts(Config{}, map[string]int{"org": 480}, nil, 120)
	total := float32(0)
	for _, p := range over {
		total += p.weight
	}
	if total > 1.0001 {
		t.Fatalf("bar overflows when the day is over target: %v", total)
	}
	if none := plannedBarParts(Config{}, map[string]int{"org": 240}, nil, 0); len(none) != 1 {
		t.Fatalf("no plan should leave the bar as it was: %+v", none)
	}
}

// Log Work turns a day green once logged + drafts + planned reach 480, without
// changing what its captions say.
func TestLogWorkDayGreenWhenPlannedFillsTheDay(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.kanban
	ds := k.week
	drafts := []Row{{"id": "a", "date": ds, "minutes": "300"}}
	before := ui.weekColumn(ds, nil, drafts)
	if before.baseFill != dayFill(0, 300) {
		t.Fatal("300 min of drafts alone should not be green")
	}
	k.plan.Tasks[plannerMeetingKey] = sprintTask{Blocks: []planBlock{{ds, 540, 180}}}
	after := ui.weekColumn(ds, nil, drafts)
	if after.baseFill != dayFill(0, 480) || after.planned != 180 {
		t.Fatalf("drafts + planned at 480 should turn the day green: planned=%d", after.planned)
	}
	if after.waiting != 300 {
		t.Fatal("planned time leaked into the draft figure the captions read")
	}
}
