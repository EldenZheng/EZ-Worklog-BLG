package tests

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func calendarCards(k *shell.KanbanUI) int {
	n := 0
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if _, ok := o.(*shell.PlannerDragCard); ok {
			n++
		}
	})
	return n
}

// While GitHub is still fetching the boards, the Sprint Calendar draws only
// its controls; the days and the tray come once the fetch lands.
func TestSprintCalendarWaitsForGitHub(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.Kanban
	k.Loading = true
	kanban.Draw(k)
	if n := calendarCards(k); n != 0 {
		t.Fatalf("calendar drew %d cards while loading", n)
	}
	if !contains(labels(k.CalendarBody), "Loading project boards…") {
		t.Fatal("the calendar should say what it is waiting for")
	}
	k.Loading = false
	kanban.Draw(k)
	if calendarCards(k) == 0 {
		t.Fatal("the calendar's cards should show once loaded")
	}
}

// A block dropped above another on the same day takes its place: the day's
// positions are renumbered, and no block gets a time of day.
func TestSprintCalendarReordersBlocksInADay(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	b := k.Plan.Boards[0]
	b.Items = append(b.Items, backend.SprintItem{ID: "j", Title: "Second", SprintID: "s", Effort: 60})
	k.Plan.Boards[0] = b
	day := backend.AddDays(c.Sprint.StartDate, 1)
	k.Week = backend.WeekStartOf(day)
	k.Plan.Tasks = map[string]backend.SprintTask{
		"p/i": {Blocks: []backend.PlanBlock{{Date: day, Start: 540, Minutes: 90}}},
		"p/j": {Blocks: []backend.PlanBlock{{Date: day, Start: 630, Minutes: 60}}},
	}
	kanban.Draw(k)
	w.Content().Refresh()
	cards := k.DayCards[day]
	if len(cards) != 2 || cards[0].Candidate.Key != "p/i" {
		t.Fatalf("day cards: %d", len(cards))
	}
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(cards[0]).Add(fyne.NewPos(10, 2))
	cards[1].Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: at}})
	if k.TrayDropLine == nil || !k.TrayDropLine.Visible() {
		t.Fatal("no drop line among the day's blocks")
	}
	cards[1].DragEnd()
	i, j := k.Plan.Tasks["p/i"].Blocks[0], k.Plan.Tasks["p/j"].Blocks[0]
	if j.Pos != 0 || i.Pos != 1 || i.Start != 0 || j.Start != 0 || i.Date != day || j.Date != day {
		t.Fatalf("after the drop: i=%+v j=%+v", i, j)
	}
}

func TestArrangePlannerDayAddsAndMovesBlocks(t *testing.T) {
	p := backend.SprintPlan{Tasks: map[string]backend.SprintTask{
		"a": {Blocks: []backend.PlanBlock{{Date: "2026-11-10", Start: 600, Minutes: 30, Note: "keep"}}},
		"b": {Blocks: []backend.PlanBlock{{Date: "2026-11-09", Start: 540, Minutes: 60}}},
	}}
	got, err := backend.ArrangePlannerDay(p, "2026-11-10", []backend.PlanBlockRef{{Key: "b", Index: 0}, {Key: "c", Index: -1}, {Key: "a", Index: 0}}, 45)
	if err != nil {
		t.Fatal(err)
	}
	b, c, a := got.Tasks["b"].Blocks[0], got.Tasks["c"].Blocks[0], got.Tasks["a"].Blocks[0]
	if b.Date != "2026-11-10" || b.Pos != 0 || c.Pos != 1 || c.Minutes != 45 || a.Pos != 2 || a.Note != "keep" || a.Start != 0 {
		t.Fatalf("arranged: a=%+v b=%+v c=%+v", a, b, c)
	}
	if p.Tasks["b"].Blocks[0].Date != "2026-11-09" {
		t.Fatal("the input plan was changed")
	}
}

// Log Work's Saved locally list waits for the week's GitHub fetch, as the week
// strip does.
func TestSavedLocallyWaitsForGitHub(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	store := backend.NewStore(tempDir(t))
	old := backend.AddDays(shell.Today(), -40)
	if _, err := store.AppendRows([]backend.Row{{"date": old, "minutes": "30", "issue": "exampleorg/repo#1"}}); err != nil {
		t.Fatal(err)
	}
	ui := &shell.UI{Store: store, Win: a.NewWindow("test"), WeekStart: backend.WeekStartOf(shell.Today()),
		Cfg: backend.Config{Repos: []string{"exampleorg"}, ProjectURL: backend.SprintBoardURLs[0]}}
	ui.EnsureProjectCache()
	for _, month := range shell.WeekMonths(ui.WeekStart) {
		ui.ProjLoading[shell.StatusCacheKey(month)] = true
	}
	ui.Win.SetContent(logwork.BuildLogTab(ui))
	logwork.DrawRecent(ui)
	if !contains(labels(ui.RecentBox), "Loading from GitHub…") {
		t.Fatalf("Saved locally should wait for GitHub: %v", labels(ui.RecentBox))
	}
	for _, month := range shell.WeekMonths(ui.WeekStart) {
		key := shell.StatusCacheKey(month)
		ui.ProjLoading[key] = false
		ui.ProjLoaded[key] = true
	}
	logwork.DrawRecent(ui)
	if contains(labels(ui.RecentBox), "Loading from GitHub…") || !contains(labels(ui.RecentBox), "1 waiting to push") {
		t.Fatal("Saved locally should show its entries once GitHub has landed")
	}
}
