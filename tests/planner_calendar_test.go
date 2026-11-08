package tests

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func plannerFixture(t *testing.T) (*shell.UI, fyne.Window, backend.SprintCandidate) {
	t.Helper()
	a := test.NewApp()
	t.Cleanup(a.Quit)
	w := a.NewWindow("calendar")
	t.Cleanup(w.Close)
	b := backend.SprintBoard{ID: "p", URL: backend.SprintBoardURLs[0], Title: "Project", Fields: []backend.SprintField{{Name: "Sprint"}}, Items: []backend.SprintItem{{ID: "i", Title: "Plan me", SprintID: "s", Effort: 90}}}
	s := backend.SprintIteration{ID: "s", Title: "Sprint", StartDate: backend.WeekStartOf(shell.Today()), Duration: 14}
	b.Fields[0].Configuration.Iterations = []backend.SprintIteration{s}
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w}
	kanban.BuildKanbanTab(ui)
	ui.Kanban.Plan = backend.SprintPlan{Boards: []backend.SprintBoard{b}, Tasks: map[string]backend.SprintTask{}}
	w.SetContent(kanban.BuildPlannerTab(ui))
	w.Resize(fyne.NewSize(1400, 950))
	w.Content().Refresh()
	return ui, w, backend.SprintCandidate{Key: "p/i", Item: b.Items[0], Sprint: s}
}

func TestSprintCalendarHoverAndDropSaveOnlyLocally(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	// Next week: today's date has its own thick border, which on a Monday
	// would read as a hover highlight on the first day.
	k.Week = backend.AddDays(k.Week, 7)
	k.Plan.Tasks["busy"] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: k.Week, Start: 540, Minutes: 480}}}
	kanban.Draw(k)
	w.Content().Refresh()
	card := sprintcalendar.PlannerCard(k, c, -1).(*shell.PlannerDragCard)
	card.MouseIn(nil)
	var allowed *shell.PlannerDateTarget
	for _, target := range k.Dates {
		if target.Date == k.Week && target.Frame.StrokeWidth != 1 {
			t.Fatal("full date highlighted")
		}
		if target.Date == backend.AddDays(k.Week, 1) {
			allowed = target
			if target.Frame.StrokeWidth != 2 {
				t.Fatal("valid date not highlighted")
			}
		}
	}
	if allowed == nil {
		t.Fatal("valid date missing")
	}
	card.MouseOut()
	if allowed.Frame.StrokeWidth != 1 {
		t.Fatal("hover highlight not cleared")
	}
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(allowed.Object).Add(fyne.NewPos(10, 10))
	card.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: at}})
	if card.Hover != allowed || allowed.Frame.StrokeWidth != 4 {
		t.Fatal("hovered date not selected")
	}
	card.DragEnd()
	p, err := ui.Store.LoadSprintPlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Tasks[c.Key].Blocks) != 1 || p.Tasks[c.Key].Blocks[0].Minutes != 90 || p.Tasks[c.Key].Blocks[0].Date != allowed.Date {
		t.Fatal("local drop not persisted")
	}
	if k.Plan.Boards[0].Items[0].Effort != 90 || k.Busy {
		t.Fatal("drop changed GitHub data or started a GitHub write")
	}
}

func TestSprintCalendarMonthRetainsAllBubbles(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	k.CalendarMode = "Month"
	k.Month = c.Sprint.StartDate[:7]
	date := c.Sprint.StartDate
	b := k.Plan.Boards[0]
	for i := 0; i < 18; i++ {
		it := backend.SprintItem{ID: fmt.Sprint(i), Title: fmt.Sprintf("Issue %d", i), SprintID: "s", Effort: 10}
		b.Items = append(b.Items, it)
		k.Plan.Tasks[it.Key(b.ID)] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: date, Start: 540 + i*10, Minutes: 10}}}
	}
	k.Plan.Boards[0] = b
	kanban.Draw(k)
	w.Content().Refresh()
	for _, target := range k.Dates {
		if target.Date == date {
			count := countPlannerBubbles(target.Object)
			if count != 18 {
				t.Fatalf("month retained %d of 18 bubbles", count)
			}
			if target.Object.MinSize().Height < sprintcalendar.PlannerDayHeaderHeight+180*sprintcalendar.PlannerPixelsPerMinute {
				t.Fatal("month day clipped planned minutes")
			}
			return
		}
	}
	t.Fatal("month date missing")
}

func countPlannerBubbles(o fyne.CanvasObject) int {
	switch x := o.(type) {
	case *shell.PlannerDayHover:
		return countPlannerBubbles(x.Body)
	case *shell.PlannerDragCard:
		return 1
	case *fyne.Container:
		n := 0
		for _, child := range x.Objects {
			n += countPlannerBubbles(child)
		}
		return n
	case *container.Scroll:
		return countPlannerBubbles(x.Content)
	}
	return 0
}
