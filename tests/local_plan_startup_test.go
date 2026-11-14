package tests

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/logwork"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestSavedPlansFillLogWorkBeforeIssueDetailsLoad(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	date := shell.Today()
	const issue = "https://github.com/exampleorg/repo/issues/42"
	store := backend.NewStore(tempDir(t))
	plan := backend.SprintPlan{Tasks: map[string]backend.SprintTask{
		issue: {Blocks: []backend.PlanBlock{{Date: date, Start: 540, Minutes: 60}}},
		"https://github.com/exampleorg/repo/issues/99": {Done: true, Blocks: []backend.PlanBlock{{Date: date, Start: 600, Minutes: 30}}},
	}}
	if err := store.SaveSprintPlan(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendRows([]backend.Row{{"date": date, "minutes": "60", "issue": "exampleorg/repo#42"}}); err != nil {
		t.Fatal(err)
	}
	ui := &shell.UI{Store: store, Win: a.NewWindow("test"), WeekStart: backend.WeekStartOf(date),
		Cfg: backend.Config{Repos: []string{"exampleorg"}, ProjectURL: backend.SprintBoardURLs[0]}}
	ui.EnsureProjectCache()
	for _, month := range shell.WeekMonths(ui.WeekStart) {
		ui.ProjLoading[shell.StatusCacheKey(month)] = true
	}
	ui.Win.SetContent(logwork.BuildLogTab(ui))
	kanban.BuildKanbanTab(ui) // reads the saved tasks; no fetched boards yet
	ui.Kanban.Loading = true
	previousGh := backend.Gh
	calls := 0
	backend.Gh = func([]string, ...string) (string, error) { calls++; return "", fmt.Errorf("offline") }
	t.Cleanup(func() { backend.Gh = previousGh })

	logwork.DrawRecent(ui)
	col := ui.WeekCols[date]
	if col == nil || col.Planned != 90 || col.Waiting != 60 {
		t.Fatalf("saved plans and drafts should appear before boards load: %+v", col)
	}
	// The days are worked out, but held back until GitHub has landed, so the
	// section arrives once instead of again when the fetch lands.
	if len(ui.WeekBox.Objects) != 2 {
		t.Fatal("the week should be held back while GitHub worklogs are still loading")
	}
	foundPlan := false
	for _, meter := range ui.Meters {
		for _, part := range meter.Target {
			if part.Key == "@plan" && part.Weight == float32(90)/backend.Target && part.Fill == shell.PlannerPlanColor {
				foundPlan = true
			}
		}
	}
	if !foundPlan {
		t.Fatal("the saved planned minutes should fill the purple bar immediately")
	}
	var tip *shell.TipCaption
	walk(col, func(o fyne.CanvasObject) {
		if c, ok := o.(*shell.TipCaption); ok {
			tip = c
		}
	})
	if tip == nil || tip.Body == nil || !contains(labels(tip.Body), "Fetching Issue...") {
		t.Fatal("the local plan tooltip should explain that issue details are pending")
	}
	if calls != 0 {
		t.Fatal("showing local planned time must not start an extra GitHub request")
	}

	// The normal board fetch replaces the placeholder without counting both.
	ui.Kanban.Plan.Boards = []backend.SprintBoard{{ID: "p", Items: []backend.SprintItem{{ID: "i", URL: issue, Title: "Resolved issue title"}}}}
	ui.Kanban.Loading, ui.Kanban.Loaded = false, true
	logwork.DrawWeekStrip(ui)
	lines := sprintcalendar.PlannedLines(ui, date, nil, nil)
	if ui.WeekCols[date].Planned != 60 || len(lines) != 1 || lines[0].Title != "Resolved issue title" {
		t.Fatalf("fetched metadata should replace the placeholder once: %+v", lines)
	}
	if calls != 0 {
		t.Fatal("redrawing resolved details must use the existing fetched boards")
	}
}

func TestLocalPlanFallbackKeepsKnownProjectFilters(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: k.Week, Start: 540, Minutes: 60}}}
	k.ProjectOff = map[string]bool{"p": true}
	if got := sprintcalendar.PlannedMinutes(ui, k.Week, nil, nil); got != 0 {
		t.Fatalf("the fallback must not bring back a known hidden project: %d", got)
	}
}
