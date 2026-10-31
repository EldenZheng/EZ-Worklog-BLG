package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestSprintAllocationPreservesBlocksSplitsAndStopsAtEnd(t *testing.T) {
	s := sprintIteration{ID: "s", StartDate: "2026-10-22", Duration: 14}
	p := sprintPlan{Tasks: map[string]sprintTask{"existing": {Blocks: []planBlock{{"2026-11-02", 540, 120}}}}}
	c := []sprintCandidate{{"large", sprintItem{Title: "large", Effort: 600}, s}, {"overflow", sprintItem{Title: "overflow", Effort: 2000}, s}, {"small", sprintItem{Title: "small", Effort: 30}, s}}
	tasks, unplaced := allocateSprint(p, c, "2026-10-31")
	if !reflect.DeepEqual(tasks["existing"], p.Tasks["existing"]) {
		t.Fatal("existing appointment changed")
	}
	want := []planBlock{{"2026-11-02", 660, 360}, {"2026-11-03", 540, 240}}
	if !reflect.DeepEqual(tasks["large"].Blocks, want) {
		t.Fatalf("large task: %v", tasks["large"].Blocks)
	}
	if len(unplaced) != 1 || unplaced[0] != "overflow" || len(tasks["overflow"].Blocks) != 0 {
		t.Fatalf("overflow: %v, %v", unplaced, tasks)
	}
	if tasks["small"].Blocks[0].Start != 780 {
		t.Fatal("failed allocation leaked reservations")
	}
	if len(p.Tasks) != 1 {
		t.Fatal("input mutated")
	}
}

func TestSprintEstimateAndBoundaries(t *testing.T) {
	it := sprintItem{Effort: 95, Complexity: "XL - 4 hours"}
	if sprintEstimate(it, sprintTask{}) != 95 || sprintEstimate(it, sprintTask{Estimate: 20}) != 20 || sprintEstimate(it, sprintTask{Complexity: "S - 30 mins"}) != 30 {
		t.Fatal("estimate precedence")
	}
	if sprintEstimate(sprintItem{Complexity: "XXL - 8 hours"}, sprintTask{}) != 480 {
		t.Fatal("complexity estimate")
	}
	s := sprintIteration{StartDate: "2026-10-22", Duration: 14}
	if !s.contains("2026-11-04") || s.contains("2026-11-05") {
		t.Fatal("exclusive sprint boundary")
	}
	tasks := map[string]sprintTask{"other": {Blocks: []planBlock{{"2026-11-02", 540, 120}}}}
	if validatePlanBlock("mine", planBlock{"2026-11-02", 600, 30}, tasks) == nil {
		t.Fatal("overlap allowed")
	}
	if validatePlanBlock("mine", planBlock{"2026-11-02", 660, 30}, tasks) != nil {
		t.Fatal("adjacent block rejected")
	}
}

func TestSprintProjectsAreIndependentOfViews(t *testing.T) {
	if strings.Contains(sprintMetadataQuery, "view(") || strings.Contains(sprintMetadataQuery, "$view") {
		t.Fatal("metadata still depends on a view")
	}
	for _, url := range sprintBoardURLs {
		if strings.Contains(url, "/views/") {
			t.Fatal("source still references shared view")
		}
	}
	if sprintProjectFilter != "assignee:@me is:open" {
		t.Fatal("personal filter changed")
	}
	if !sameSprintProject("https://github.com/orgs/BIGLEDGER/projects/9/views/11", sprintBoardURLs[0]) {
		t.Fatal("old cached board not recognized")
	}
}

func TestSprintCalendarDropRespectsCapacityAndPreservesDuration(t *testing.T) {
	c := sprintCandidate{Key: "mine", Item: sprintItem{Effort: 90}, Sprint: sprintIteration{StartDate: "2026-10-22", Duration: 14}}
	p := sprintPlan{Tasks: map[string]sprintTask{"other": {Blocks: []planBlock{{"2026-11-02", 540, 120}}}}}
	if _, ok := plannerSlot(p, c, -1, "2026-11-05"); ok {
		t.Fatal("date outside sprint allowed")
	}
	block, ok := plannerSlot(p, c, -1, "2026-11-02")
	if !ok || block.Start != 660 || block.Minutes != 90 {
		t.Fatalf("default project estimate not used: %v", block)
	}
	p.Tasks["mine"] = sprintTask{Estimate: 180}
	moved, err := movePlannerIssue(p, c, -1, "2026-11-02")
	if err != nil {
		t.Fatal(err)
	}
	if moved.Tasks["mine"].Blocks[0].Minutes != 180 || len(p.Tasks["mine"].Blocks) != 0 {
		t.Fatal("local estimate lost or input mutated")
	}
	moved, err = movePlannerIssue(moved, c, 0, "2026-11-03")
	if err != nil || moved.Tasks["mine"].Blocks[0].Minutes != 180 {
		t.Fatal("moving block changed duration")
	}
	if c.Item.Effort != 90 {
		t.Fatal("GitHub estimate changed")
	}
	moved.Tasks["full"] = sprintTask{Blocks: []planBlock{{"2026-11-04", 540, 480}}}
	if _, ok := plannerSlot(moved, c, 0, "2026-11-04"); ok {
		t.Fatal("full date allowed")
	}
	if len(moved.Tasks["other"].Blocks) != 1 {
		t.Fatal("other appointment changed")
	}
}

func TestSprintMutationUsesSourceProjectIDs(t *testing.T) {
	b := sprintBoard{ID: "support-project", Fields: []sprintField{{ID: "support-status", Name: "Status", Options: []fieldOption{{ID: "support-progress", Name: "In Progress"}}}, {ID: "support-sprint", Name: "Sprint"}}}
	b.Fields[1].Configuration.Iterations = []sprintIteration{{ID: "support-35", Title: "Sprint 35"}}
	it := sprintItem{ID: "support-item"}
	args, err := sprintItemMutation(b, it, "status", "In Progress")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, v := range []string{"project=support-project", "item=support-item", "field=support-status", "value=support-progress", "singleSelectOptionId"} {
		if !strings.Contains(joined, v) {
			t.Fatalf("missing %s: %s", v, joined)
		}
	}
	args, err = sprintItemMutation(b, it, "sprint", "support-35")
	if err != nil || !strings.Contains(strings.Join(args, " "), "iterationId:$value") {
		t.Fatalf("sprint mutation: %v, %v", args, err)
	}
	if _, err = sprintItemMutation(b, it, "sprint", "other-project-35"); err == nil {
		t.Fatal("accepted sprint from another project")
	}
	args, err = sprintItemMutation(b, it, "sprint", "")
	if err != nil || !strings.Contains(strings.Join(args, " "), "clearProjectV2ItemFieldValue") {
		t.Fatal("clear sprint mutation")
	}
}

func TestSprintPlanSaveReloadAndOfflineUI(t *testing.T) {
	s := newStore(t.TempDir())
	p := sprintPlan{Tasks: map[string]sprintTask{"issue": {Estimate: 90, Notes: "local only", Blocks: []planBlock{{"2026-11-02", 540, 90}}}}, Boards: []sprintBoard{{ID: "project", Title: "Example", Fields: []sprintField{{Name: "Status", Options: []fieldOption{{Name: "In Progress"}}}}, Items: []sprintItem{{ID: "i", Title: "Example task", Status: "In Progress"}}}}}
	if err := s.saveSprintPlan(p); err != nil {
		t.Fatal(err)
	}
	got, err := s.loadSprintPlan()
	if err != nil || !reflect.DeepEqual(got, p) {
		t.Fatalf("roundtrip: %v, %v", got, err)
	}
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("planner")
	defer w.Close()
	ui := &UI{store: s, win: w}
	w.SetContent(ui.buildKanbanTab())
	w.Resize(fyne.NewSize(1200, 800))
	k := ui.kanban
	k.filter = "all"
	k.draw()
	if len(k.targets) < 2 {
		t.Fatal("drop targets missing")
	}
	w.SetContent(ui.buildPlannerTab())
	k.draw()
	k.editor(p.Boards[0], p.Boards[0].Items[0])
}

func TestSprintDragTargetsAndCancellation(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("drag")
	defer w.Close()
	ui := &UI{store: newStore(t.TempDir()), win: w}
	w.SetContent(ui.buildKanbanTab())
	w.Resize(fyne.NewSize(1200, 800))
	k := ui.kanban
	b := sprintBoard{ID: "p", Title: "Project", Fields: []sprintField{{Name: "Status", Options: []fieldOption{{Name: "Created"}, {Name: "In Progress"}}}, {Name: "Sprint"}}}
	b.Fields[1].Configuration.Iterations = []sprintIteration{{ID: "own-sprint", Title: "Sprint", StartDate: today(), Duration: 14}}
	it := sprintItem{ID: "i", Title: "Drag me", Status: "Created", SprintID: "own-sprint"}
	b.Items = []sprintItem{it}
	k.plan.Boards = []sprintBoard{b}
	k.filter = "all"
	k.draw()
	w.Content().Refresh()
	card := k.card(b, it).(*sprintDragCard)
	var target *kanbanTarget
	for _, candidate := range k.targets {
		if candidate.kind == "sprint" && candidate.value == sprintKey(b.iterations()[0]) {
			target = candidate
			break
		}
	}
	if target == nil {
		t.Fatal("source sprint target missing")
	}
	at := a.Driver().AbsolutePositionForObject(target.object).Add(fyne.NewPos(10, 10))
	card.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: at}})
	if k.hover != target {
		t.Fatal("drag did not select source sprint")
	}
	card.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: fyne.NewPos(-10, -10)}})
	card.DragEnd()
	if k.hover != nil || len(k.ghostLayer.Objects) != 0 || k.busy {
		t.Fatal("cancelled drag did not clean up")
	}
	target.board = "different-project"
	card.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: at}})
	if k.hover != nil {
		t.Fatal("accepted a sprint from another project")
	}
	card.DragEnd()
}

func TestLiveSprintBoardsReadOnly(t *testing.T) {
	if os.Getenv("WORKLOG_KANBAN_LIVE") != "1" {
		t.Skip("opt-in read-only GitHub check")
	}
	for _, url := range sprintBoardURLs {
		b, err := fetchSprintBoard(url)
		if err != nil {
			t.Fatal(err)
		}
		if b.ID == "" || b.field("Status").ID == "" || b.field("Sprint").ID == "" {
			t.Fatalf("missing metadata: %s", url)
		}
		t.Logf("%s: %d matching issues, current sprint %s", b.Title, len(b.Items), b.current(today()).Title)
	}
}

func TestSprintScreenshots(t *testing.T) {
	dir := os.Getenv("WORKLOG_PLANNER_SCREENSHOTS")
	if dir == "" {
		t.Skip("optional visual check")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(goldTheme{Theme: a.Settings().Theme()})
	w := a.NewWindow("planner")
	defer w.Close()
	s := newStore(t.TempDir())
	b := sprintBoard{ID: "p", URL: sprintBoardURLs[0], Title: "BigLedger", Fields: []sprintField{{Name: "Status", Options: []fieldOption{{Name: "Created"}, {Name: "In Progress"}, {Name: "In Review"}}}, {Name: "Sprint"}}, Items: []sprintItem{{ID: "1", URL: "https://github.com/bigledger/example/issues/1", Title: "Improve invoice search and date filters", Status: "In Progress", SprintID: "s", Effort: 120}, {ID: "2", URL: "https://github.com/bigledger/example/issues/2", Title: "Review customer report", Status: "Created", SprintID: "s", Complexity: "S - 30 mins"}}}
	b.Fields[1].Configuration.Iterations = []sprintIteration{{ID: "s", Title: "Current sprint", StartDate: weekStartOf(today()), Duration: 14}}
	b.Items = append(b.Items, sprintItem{ID: "pr", Kind: "PullRequest", URL: "https://github.com/bigledger/example/pull/4", Title: "Review invoice search improvements", Status: "In Review", SprintID: "s", Effort: 45, Complexity: "S - 30 mins"})
	support := sprintBoard{ID: "support", URL: sprintBoardURLs[1], Title: "Support", Fields: b.Fields, Items: []sprintItem{{ID: "3", URL: "https://github.com/BigLedger-Support/example/issues/3", Title: "Investigate customer reconciliation", Status: "In Progress", SprintID: "s", Effort: 60}}}
	p := sprintPlan{Boards: []sprintBoard{b, support}, Tasks: map[string]sprintTask{"https://github.com/bigledger/example/issues/1": {Estimate: 120, Blocks: []planBlock{{addDays(weekStartOf(today()), 1), 540, 120}}}}}
	if err := s.saveSprintPlan(p); err != nil {
		t.Fatal(err)
	}
	ui := &UI{store: s, win: w, calMonth: thisMonth(), repMonth: currentPeriod()}
	ui.buildAllTabs()
	w.SetContent(ui.tabs)
	ui.tabs.SelectIndex(7)
	w.Resize(fyne.NewSize(1560, 1000))
	captureSprintPNG(t, w, filepath.Join(dir, "kanban.png"))
	ui.kanban.filter = "all"
	ui.kanban.draw()
	captureSprintPNG(t, w, filepath.Join(dir, "kanban-all-sprints.png"))
	ui.kanban.filter = "current"
	ui.kanban.draw()
	// A single wide preview makes all of the existing status hues reviewable.
	paletteMain, paletteSupport := b, support
	paletteMain.Fields = append([]sprintField{}, b.Fields...)
	paletteMain.Fields[0].Options = nil
	for _, status := range primaryKanbanStatuses {
		paletteMain.Fields[0].Options = append(paletteMain.Fields[0].Options, fieldOption{Name: status})
	}
	paletteSupport.Fields = paletteMain.Fields
	paletteMain.Items = nil
	paletteSupport.Items = nil
	titles := []string{"Review a new customer request", "Investigate report discrepancy", "Build invoice date filters", "Await customer clarification", "Validate reconciliation totals", "Deploy server configuration", "Review integration changes", "Complete weekly maintenance"}
	for i, status := range primaryKanbanStatuses {
		it := sprintItem{ID: fmt.Sprint(i + 10), Title: titles[i], Status: status, SprintID: "s", Effort: 60}
		if i%2 == 0 {
			it.URL = fmt.Sprintf("https://github.com/bigledger/example/issues/%d", i+10)
			paletteMain.Items = append(paletteMain.Items, it)
		} else {
			it.URL = fmt.Sprintf("https://github.com/BigLedger-Support/example/issues/%d", i+10)
			paletteSupport.Items = append(paletteSupport.Items, it)
		}
	}
	previous := ui.kanban.plan
	ui.kanban.plan.Boards = []sprintBoard{paletteMain, paletteSupport}
	w.Resize(fyne.NewSize(2520, 1000))
	ui.kanban.draw()
	captureSprintPNG(t, w, filepath.Join(dir, "kanban-palette.png"))
	ui.kanban.plan = previous
	w.Resize(fyne.NewSize(1560, 1000))
	ui.kanban.draw()
	ui.tabs.SelectIndex(8)
	ui.kanban.draw()
	captureSprintPNG(t, w, filepath.Join(dir, "planner.png"))
	var edge *plannerResizeGrip
	var trayCard *plannerDragCard
	walkCanvas(ui.kanban.calendarBody, func(o fyne.CanvasObject) {
		if g, ok := o.(*plannerResizeGrip); ok {
			edge = g
		}
		if c, ok := o.(*plannerDragCard); ok && c.index < 0 {
			trayCard = c
		}
	})
	if edge != nil {
		edge.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DY: 45 * plannerPixelsPerMinute}})
		captureSprintPNG(t, w, filepath.Join(dir, "planner-resizing.png"))
		edge.started = false
		ui.kanban.calendarResizing = false
		ui.kanban.draw() // Preview only: keep the fixture's saved appointments.
	}
	if trayCard != nil {
		for _, target := range ui.kanban.dates {
			if target.date == weekStartOf(today()) {
				at := a.Driver().AbsolutePositionForObject(target.object).Add(fyne.NewPos(10, 80))
				trayCard.highlightDates(&at)
				captureSprintPNG(t, w, filepath.Join(dir, "planner-drop-target.png"))
				trayCard.resetDates()
				break
			}
		}
	}
	var viewPick *goldSelect
	walkCanvas(ui.kanban.calendarBody, func(o fyne.CanvasObject) {
		if selectBox, ok := o.(*goldSelect); ok && len(selectBox.Options) > 0 && selectBox.Options[0] == "Week" {
			viewPick = selectBox
		}
	})
	if viewPick != nil {
		viewPick.Tapped(nil)
		captureSprintPNG(t, w, filepath.Join(dir, "planner-view-dropdown.png"))
		viewPick.closeChoices()
	}
	walkCanvas(ui.tabs.Items[8].Content, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Refresh GitHub" {
			if a.Driver().AbsolutePositionForObject(b).X+b.Size().Width > w.Canvas().Size().Width {
				t.Fatal("Refresh GitHub extends outside the calendar")
			}
		}
	})
	ui.kanban.calendarMode = "Month"
	ui.kanban.draw()
	captureSprintPNG(t, w, filepath.Join(dir, "planner-month.png"))
	ui.kanban.editorFor(b, b.Items[0], true)
	captureSprintPNG(t, w, filepath.Join(dir, "planner-editor.png"))
	walkPopup(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if button, ok := o.(*widget.Button); ok && button.Text == "Close" {
			button.OnTapped()
		}
	})
	ui.kanban.editor(b, b.Items[0])
	captureSprintPNG(t, w, filepath.Join(dir, "kanban-editor.png"))
}

func captureSprintPNG(t *testing.T, w fyne.Window, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	w.Content().Refresh()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, w.Canvas().Capture()); err != nil {
		t.Fatal(err)
	}
}
