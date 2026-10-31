package main

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
)

func plannerFixture(t *testing.T) (*UI, fyne.Window, sprintCandidate) {
	t.Helper()
	a := test.NewApp()
	t.Cleanup(a.Quit)
	w := a.NewWindow("calendar")
	t.Cleanup(w.Close)
	b := sprintBoard{ID: "p", URL: sprintBoardURLs[0], Title: "Project", Fields: []sprintField{{Name: "Sprint"}}, Items: []sprintItem{{ID: "i", Title: "Plan me", SprintID: "s", Effort: 90}}}
	s := sprintIteration{ID: "s", Title: "Sprint", StartDate: weekStartOf(today()), Duration: 14}
	b.Fields[0].Configuration.Iterations = []sprintIteration{s}
	ui := &UI{store: newStore(t.TempDir()), win: w}
	ui.buildKanbanTab()
	ui.kanban.plan = sprintPlan{Boards: []sprintBoard{b}, Tasks: map[string]sprintTask{}}
	w.SetContent(ui.buildPlannerTab())
	w.Resize(fyne.NewSize(1400, 950))
	w.Content().Refresh()
	return ui, w, sprintCandidate{Key: "p/i", Item: b.Items[0], Sprint: s}
}

func TestSprintCalendarHoverAndDropSaveOnlyLocally(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.kanban
	k.plan.Tasks["busy"] = sprintTask{Blocks: []planBlock{{k.week, 540, 480}}}
	k.draw()
	w.Content().Refresh()
	card := k.plannerCard(c, -1).(*plannerDragCard)
	card.MouseIn(nil)
	var allowed *plannerDateTarget
	for _, target := range k.dates {
		if target.date == k.week && target.frame.StrokeWidth != 1 {
			t.Fatal("full date highlighted")
		}
		if target.date == addDays(k.week, 1) {
			allowed = target
			if target.frame.StrokeWidth != 2 {
				t.Fatal("valid date not highlighted")
			}
		}
	}
	if allowed == nil {
		t.Fatal("valid date missing")
	}
	card.MouseOut()
	if allowed.frame.StrokeWidth != 1 {
		t.Fatal("hover highlight not cleared")
	}
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(allowed.object).Add(fyne.NewPos(10, 10))
	card.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: at}})
	if card.hover != allowed || allowed.frame.StrokeWidth != 4 {
		t.Fatal("hovered date not selected")
	}
	card.DragEnd()
	p, err := ui.store.loadSprintPlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Tasks[c.Key].Blocks) != 1 || p.Tasks[c.Key].Blocks[0].Minutes != 90 || p.Tasks[c.Key].Blocks[0].Date != allowed.date {
		t.Fatal("local drop not persisted")
	}
	if p.Boards[0].Items[0].Effort != 90 || k.busy {
		t.Fatal("drop changed GitHub data or started a GitHub write")
	}
}

func TestSprintCalendarMonthRetainsAllBubbles(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.kanban
	k.calendarMode = "Month"
	k.month = c.Sprint.StartDate[:7]
	date := c.Sprint.StartDate
	b := k.plan.Boards[0]
	for i := 0; i < 18; i++ {
		it := sprintItem{ID: fmt.Sprint(i), Title: fmt.Sprintf("Issue %d", i), SprintID: "s", Effort: 10}
		b.Items = append(b.Items, it)
		k.plan.Tasks[it.key(b.ID)] = sprintTask{Blocks: []planBlock{{date, 540 + i*10, 10}}}
	}
	k.plan.Boards[0] = b
	k.draw()
	w.Content().Refresh()
	for _, target := range k.dates {
		if target.date == date {
			count := countPlannerBubbles(target.object)
			if count != 18 {
				t.Fatalf("month retained %d of 18 bubbles", count)
			}
			if target.object.MinSize().Height < plannerDayHeaderHeight+180*plannerPixelsPerMinute {
				t.Fatal("month day clipped planned minutes")
			}
			return
		}
	}
	t.Fatal("month date missing")
}

func countPlannerBubbles(o fyne.CanvasObject) int {
	switch x := o.(type) {
	case *plannerDayHover:
		return countPlannerBubbles(x.body)
	case *plannerDragCard:
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
