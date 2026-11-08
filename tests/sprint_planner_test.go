package tests

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

	"worklog/internal/assemble"
	"worklog/internal/menus/kanban"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestSprintAllocationPreservesBlocksSplitsAndStopsAtEnd(t *testing.T) {
	s := backend.SprintIteration{ID: "s", StartDate: "2026-10-22", Duration: 14}
	p := backend.SprintPlan{Tasks: map[string]backend.SprintTask{"existing": {Blocks: []backend.PlanBlock{{Date: "2026-11-02", Start: 540, Minutes: 120}}}}}
	c := []backend.SprintCandidate{{"large", backend.SprintItem{Title: "large", Effort: 600}, s}, {"overflow", backend.SprintItem{Title: "overflow", Effort: 2000}, s}, {"small", backend.SprintItem{Title: "small", Effort: 30}, s}}
	tasks, unplaced := backend.AllocateSprint(p, c, "2026-10-31")
	if !reflect.DeepEqual(tasks["existing"], p.Tasks["existing"]) {
		t.Fatal("existing appointment changed")
	}
	want := []backend.PlanBlock{{Date: "2026-11-02", Start: 660, Minutes: 360}, {Date: "2026-11-03", Start: 540, Minutes: 240}}
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
	it := backend.SprintItem{Effort: 95, Complexity: "XL - 4 hours"}
	if backend.SprintEstimate(it, backend.SprintTask{}) != 95 || backend.SprintEstimate(it, backend.SprintTask{Estimate: 20}) != 20 || backend.SprintEstimate(it, backend.SprintTask{Complexity: "S - 30 mins"}) != 30 {
		t.Fatal("estimate precedence")
	}
	if backend.SprintEstimate(backend.SprintItem{Complexity: "XXL - 8 hours"}, backend.SprintTask{}) != 480 {
		t.Fatal("complexity estimate")
	}
	s := backend.SprintIteration{StartDate: "2026-10-22", Duration: 14}
	if !s.Contains("2026-11-04") || s.Contains("2026-11-05") {
		t.Fatal("exclusive sprint boundary")
	}
	tasks := map[string]backend.SprintTask{"other": {Blocks: []backend.PlanBlock{{Date: "2026-11-02", Start: 540, Minutes: 120}}}}
	if backend.ValidatePlanBlock("mine", backend.PlanBlock{Date: "2026-11-02", Start: 600, Minutes: 30}, tasks) == nil {
		t.Fatal("overlap allowed")
	}
	if backend.ValidatePlanBlock("mine", backend.PlanBlock{Date: "2026-11-02", Start: 660, Minutes: 30}, tasks) != nil {
		t.Fatal("adjacent block rejected")
	}
}

func TestSprintProjectsAreIndependentOfViews(t *testing.T) {
	if strings.Contains(backend.SprintMetadataQuery, "view(") || strings.Contains(backend.SprintMetadataQuery, "$view") {
		t.Fatal("metadata still depends on a view")
	}
	for _, url := range backend.SprintBoardURLs {
		if strings.Contains(url, "/views/") {
			t.Fatal("source still references shared view")
		}
	}
	if backend.SprintProjectFilter != "assignee:@me is:open" {
		t.Fatal("personal filter changed")
	}
	if !backend.SameSprintProject("https://github.com/orgs/BIGLEDGER/projects/9/views/11", backend.SprintBoardURLs[0]) {
		t.Fatal("old cached board not recognized")
	}
}

func TestSprintCalendarDropRespectsCapacityAndPreservesDuration(t *testing.T) {
	c := backend.SprintCandidate{Key: "mine", Item: backend.SprintItem{Effort: 90}, Sprint: backend.SprintIteration{StartDate: "2026-10-22", Duration: 14}}
	p := backend.SprintPlan{Tasks: map[string]backend.SprintTask{"other": {Blocks: []backend.PlanBlock{{Date: "2026-11-02", Start: 540, Minutes: 120}}}}}
	if _, ok := backend.PlannerSlot(p, c, -1, "2026-11-05"); ok {
		t.Fatal("date outside sprint allowed")
	}
	block, ok := backend.PlannerSlot(p, c, -1, "2026-11-02")
	if !ok || block.Start != 660 || block.Minutes != 90 {
		t.Fatalf("default project estimate not used: %v", block)
	}
	p.Tasks["mine"] = backend.SprintTask{Estimate: 180}
	moved, err := backend.MovePlannerIssue(p, c, -1, "2026-11-02")
	if err != nil {
		t.Fatal(err)
	}
	if moved.Tasks["mine"].Blocks[0].Minutes != 180 || len(p.Tasks["mine"].Blocks) != 0 {
		t.Fatal("local estimate lost or input mutated")
	}
	moved, err = backend.MovePlannerIssue(moved, c, 0, "2026-11-03")
	if err != nil || moved.Tasks["mine"].Blocks[0].Minutes != 180 {
		t.Fatal("moving block changed duration")
	}
	if c.Item.Effort != 90 {
		t.Fatal("GitHub estimate changed")
	}
	moved.Tasks["full"] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: "2026-11-04", Start: 540, Minutes: 480}}}
	if _, ok := backend.PlannerSlot(moved, c, 0, "2026-11-04"); ok {
		t.Fatal("full date allowed")
	}
	if len(moved.Tasks["other"].Blocks) != 1 {
		t.Fatal("other appointment changed")
	}
}

func TestSprintMutationUsesSourceProjectIDs(t *testing.T) {
	b := backend.SprintBoard{ID: "support-project", Fields: []backend.SprintField{{ID: "support-status", Name: "Status", Options: []backend.FieldOption{{ID: "support-progress", Name: "In Progress"}}}, {ID: "support-sprint", Name: "Sprint"}}}
	b.Fields[1].Configuration.Iterations = []backend.SprintIteration{{ID: "support-35", Title: "Sprint 35"}}
	it := backend.SprintItem{ID: "support-item"}
	args, err := backend.SprintItemMutation(b, it, "status", "In Progress")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, v := range []string{"project=support-project", "item=support-item", "field=support-status", "value=support-progress", "singleSelectOptionId"} {
		if !strings.Contains(joined, v) {
			t.Fatalf("missing %s: %s", v, joined)
		}
	}
	args, err = backend.SprintItemMutation(b, it, "sprint", "support-35")
	if err != nil || !strings.Contains(strings.Join(args, " "), "iterationId:$value") {
		t.Fatalf("sprint mutation: %v, %v", args, err)
	}
	if _, err = backend.SprintItemMutation(b, it, "sprint", "other-project-35"); err == nil {
		t.Fatal("accepted sprint from another project")
	}
	args, err = backend.SprintItemMutation(b, it, "sprint", "")
	if err != nil || !strings.Contains(strings.Join(args, " "), "clearProjectV2ItemFieldValue") {
		t.Fatal("clear sprint mutation")
	}
}

func TestSprintPlanSaveReloadAndOfflineUI(t *testing.T) {
	s := backend.NewStore(tempDir(t))
	p := backend.SprintPlan{Tasks: map[string]backend.SprintTask{"issue": {Estimate: 90, Notes: "local only", Blocks: []backend.PlanBlock{{Date: "2026-11-02", Start: 540, Minutes: 90}}}}, Boards: []backend.SprintBoard{{ID: "project", Title: "Example", Fields: []backend.SprintField{{Name: "Status", Options: []backend.FieldOption{{Name: "In Progress"}}}}, Items: []backend.SprintItem{{ID: "i", Title: "Example task", Status: "In Progress"}}}}}
	if err := s.SaveSprintPlan(p); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadSprintPlan()
	if err != nil || !reflect.DeepEqual(got.Tasks, p.Tasks) {
		t.Fatalf("roundtrip: %v, %v", got, err)
	}
	if len(got.Boards) != 0 {
		t.Fatal("GitHub boards were saved locally; they are refetched each run")
	}
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("planner")
	defer w.Close()
	ui := &shell.UI{Store: s, Win: w}
	w.SetContent(kanban.BuildKanbanTab(ui))
	w.Resize(fyne.NewSize(1200, 800))
	k := ui.Kanban
	k.Plan.Boards = p.Boards // as after the first fetch
	k.Filter = "all"
	kanban.Draw(k)
	if len(k.Targets) < 2 {
		t.Fatal("drop targets missing")
	}
	w.SetContent(kanban.BuildPlannerTab(ui))
	kanban.Draw(k)
	kanban.Editor(k, p.Boards[0], p.Boards[0].Items[0])
}

func TestSprintDragTargetsAndCancellation(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("drag")
	defer w.Close()
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w}
	w.SetContent(kanban.BuildKanbanTab(ui))
	w.Resize(fyne.NewSize(1200, 800))
	k := ui.Kanban
	b := backend.SprintBoard{ID: "p", Title: "Project", Fields: []backend.SprintField{{Name: "Status", Options: []backend.FieldOption{{Name: "Created"}, {Name: "In Progress"}}}, {Name: "Sprint"}}}
	b.Fields[1].Configuration.Iterations = []backend.SprintIteration{{ID: "own-sprint", Title: "Sprint", StartDate: shell.Today(), Duration: 14}}
	it := backend.SprintItem{ID: "i", Title: "Drag me", Status: "Created", SprintID: "own-sprint"}
	b.Items = []backend.SprintItem{it}
	k.Plan.Boards = []backend.SprintBoard{b}
	k.Filter = "all"
	kanban.Draw(k)
	w.Content().Refresh()
	card := kanban.Card(k, b, it).(*shell.SprintDragCard)
	var target *shell.KanbanTarget
	for _, candidate := range k.Targets {
		if candidate.Kind == "sprint" && candidate.Value == backend.SprintKey(b.Iterations()[0]) {
			target = candidate
			break
		}
	}
	if target == nil {
		t.Fatal("source sprint target missing")
	}
	at := a.Driver().AbsolutePositionForObject(target.Object).Add(fyne.NewPos(10, 10))
	card.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: at}})
	if k.Hover != target {
		t.Fatal("drag did not select source sprint")
	}
	card.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: fyne.NewPos(-10, -10)}})
	card.DragEnd()
	if k.Hover != nil || len(k.GhostLayer.Objects) != 0 || k.Busy {
		t.Fatal("cancelled drag did not clean up")
	}
	target.Board = "different-project"
	card.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: at}})
	if k.Hover != nil {
		t.Fatal("accepted a sprint from another project")
	}
	card.DragEnd()
}

func TestLiveSprintBoardsReadOnly(t *testing.T) {
	if os.Getenv("WORKLOG_KANBAN_LIVE") != "1" {
		t.Skip("opt-in read-only GitHub check")
	}
	for _, url := range backend.SprintBoardURLs {
		b, err := backend.FetchSprintBoard(url)
		if err != nil {
			t.Fatal(err)
		}
		if b.ID == "" || b.Field("Status").ID == "" || b.Field("Sprint").ID == "" {
			t.Fatalf("missing metadata: %s", url)
		}
		t.Logf("%s: %d matching issues, current sprint %s", b.Title, len(b.Items), b.Current(shell.Today()).Title)
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
	a.Settings().SetTheme(shell.GoldTheme{Theme: a.Settings().Theme()})
	w := a.NewWindow("planner")
	defer w.Close()
	s := backend.NewStore(tempDir(t))
	b := backend.SprintBoard{ID: "p", URL: backend.SprintBoardURLs[0], Title: "BigLedger", Fields: []backend.SprintField{{Name: "Status", Options: []backend.FieldOption{{Name: "Created"}, {Name: "In Progress"}, {Name: "In Review"}}}, {Name: "Sprint"}}, Items: []backend.SprintItem{{ID: "1", URL: "https://github.com/bigledger/example/issues/1", Title: "Improve invoice search and date filters", Status: "In Progress", SprintID: "s", Effort: 120}, {ID: "2", URL: "https://github.com/bigledger/example/issues/2", Title: "Review customer report", Status: "Created", SprintID: "s", Complexity: "S - 30 mins"}}}
	b.Fields[1].Configuration.Iterations = []backend.SprintIteration{{ID: "s", Title: "Current sprint", StartDate: backend.WeekStartOf(shell.Today()), Duration: 14}}
	b.Items = append(b.Items, backend.SprintItem{ID: "pr", Kind: "PullRequest", URL: "https://github.com/bigledger/example/pull/4", Title: "Review invoice search improvements", Status: "In Review", SprintID: "s", Effort: 45, Complexity: "S - 30 mins"})
	support := backend.SprintBoard{ID: "support", URL: backend.SprintBoardURLs[1], Title: "Support", Fields: b.Fields, Items: []backend.SprintItem{{ID: "3", URL: "https://github.com/BigLedger-Support/example/issues/3", Title: "Investigate customer reconciliation", Status: "In Progress", SprintID: "s", Effort: 60}}}
	p := backend.SprintPlan{Boards: []backend.SprintBoard{b, support}, Tasks: map[string]backend.SprintTask{"https://github.com/bigledger/example/issues/1": {Estimate: 120, Blocks: []backend.PlanBlock{{Date: backend.AddDays(backend.WeekStartOf(shell.Today()), 1), Start: 540, Minutes: 120}}}}}
	if err := s.SaveSprintPlan(p); err != nil {
		t.Fatal(err)
	}
	ui := &shell.UI{Store: s, Win: w, CalMonth: shell.ThisMonth(), RepMonth: shell.CurrentPeriod()}
	assemble.BuildAllTabs(ui)
	w.SetContent(ui.Tabs)
	ui.Tabs.SelectIndex(7)
	w.Resize(fyne.NewSize(1560, 1000))
	captureSprintPNG(t, w, filepath.Join(dir, "kanban.png"))
	ui.Kanban.Filter = "all"
	kanban.Draw(ui.Kanban)
	captureSprintPNG(t, w, filepath.Join(dir, "kanban-all-sprints.png"))
	ui.Kanban.Filter = "current"
	kanban.Draw(ui.Kanban)
	// A single wide preview makes all of the existing status hues reviewable.
	paletteMain, paletteSupport := b, support
	paletteMain.Fields = append([]backend.SprintField{}, b.Fields...)
	paletteMain.Fields[0].Options = nil
	for _, status := range kanban.PrimaryKanbanStatuses {
		paletteMain.Fields[0].Options = append(paletteMain.Fields[0].Options, backend.FieldOption{Name: status})
	}
	paletteSupport.Fields = paletteMain.Fields
	paletteMain.Items = nil
	paletteSupport.Items = nil
	titles := []string{"Review a new customer request", "Investigate report discrepancy", "Build invoice date filters", "Await customer clarification", "Validate reconciliation totals", "Deploy server configuration", "Review integration changes", "Complete weekly maintenance"}
	for i, status := range kanban.PrimaryKanbanStatuses {
		it := backend.SprintItem{ID: fmt.Sprint(i + 10), Title: titles[i], Status: status, SprintID: "s", Effort: 60}
		if i%2 == 0 {
			it.URL = fmt.Sprintf("https://github.com/bigledger/example/issues/%d", i+10)
			paletteMain.Items = append(paletteMain.Items, it)
		} else {
			it.URL = fmt.Sprintf("https://github.com/BigLedger-Support/example/issues/%d", i+10)
			paletteSupport.Items = append(paletteSupport.Items, it)
		}
	}
	previous := ui.Kanban.Plan
	ui.Kanban.Plan.Boards = []backend.SprintBoard{paletteMain, paletteSupport}
	w.Resize(fyne.NewSize(2520, 1000))
	kanban.Draw(ui.Kanban)
	captureSprintPNG(t, w, filepath.Join(dir, "kanban-palette.png"))
	ui.Kanban.Plan = previous
	w.Resize(fyne.NewSize(1560, 1000))
	kanban.Draw(ui.Kanban)
	ui.Tabs.SelectIndex(8)
	kanban.Draw(ui.Kanban)
	captureSprintPNG(t, w, filepath.Join(dir, "planner.png"))
	var edge *sprintcalendar.PlannerResizeGrip
	var trayCard *shell.PlannerDragCard
	walkCanvas(ui.Kanban.CalendarBody, func(o fyne.CanvasObject) {
		if g, ok := o.(*sprintcalendar.PlannerResizeGrip); ok {
			edge = g
		}
		if c, ok := o.(*shell.PlannerDragCard); ok && c.Index < 0 {
			trayCard = c
		}
	})
	if edge != nil {
		edge.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DY: 45 * sprintcalendar.PlannerPixelsPerMinute}})
		captureSprintPNG(t, w, filepath.Join(dir, "planner-resizing.png"))
		edge.Started = false
		ui.Kanban.CalendarResizing = false
		kanban.Draw(ui.Kanban) // Preview only: keep the fixture's saved appointments.
	}
	if trayCard != nil {
		for _, target := range ui.Kanban.Dates {
			if target.Date == backend.WeekStartOf(shell.Today()) {
				at := a.Driver().AbsolutePositionForObject(target.Object).Add(fyne.NewPos(10, 80))
				trayCard.HighlightDates(&at)
				captureSprintPNG(t, w, filepath.Join(dir, "planner-drop-target.png"))
				trayCard.ResetDates()
				break
			}
		}
	}
	var viewPick *shell.GoldSelect
	walkCanvas(ui.Kanban.CalendarBody, func(o fyne.CanvasObject) {
		if selectBox, ok := o.(*shell.GoldSelect); ok && len(selectBox.Options) > 0 && selectBox.Options[0] == "Week" {
			viewPick = selectBox
		}
	})
	if viewPick != nil {
		viewPick.Tapped(nil)
		captureSprintPNG(t, w, filepath.Join(dir, "planner-view-dropdown.png"))
		viewPick.CloseChoices()
	}
	walkCanvas(ui.Tabs.Items[8].Content, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Refresh GitHub" {
			if a.Driver().AbsolutePositionForObject(b).X+b.Size().Width > w.Canvas().Size().Width {
				t.Fatal("Refresh GitHub extends outside the calendar")
			}
		}
	})
	ui.Kanban.CalendarMode = "Month"
	kanban.Draw(ui.Kanban)
	captureSprintPNG(t, w, filepath.Join(dir, "planner-month.png"))
	kanban.EditorFor(ui.Kanban, b, b.Items[0], true)
	captureSprintPNG(t, w, filepath.Join(dir, "planner-editor.png"))
	walkPopup(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if button, ok := o.(*widget.Button); ok && button.Text == "Close" {
			button.OnTapped()
		}
	})
	kanban.Editor(ui.Kanban, b, b.Items[0])
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
