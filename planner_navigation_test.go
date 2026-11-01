package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestSprintWeekExpandsScaleButMonthKeepsNaturalHeight(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.kanban
	k.plan.Tasks[c.Key] = sprintTask{Blocks: []planBlock{{k.week, 540, 120}}}
	w.Resize(fyne.NewSize(1500, 1250))
	k.draw()
	w.Content().Refresh()
	var face *plannerDragCard
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*plannerDragCard); ok && card.index == 0 {
			face = card
		}
	})
	l := face.content.(*fyne.Container).Layout.(*plannerDurationLayout)
	if l.pixels() <= plannerPixelsPerMinute {
		t.Fatal("week did not expand into spare height")
	}
	if math.Abs(float64(face.Size().Height-120*l.pixels())) > 0.1 {
		t.Fatal("expanded bubble does not follow ruler")
	}
	// The week sits inside the top inset that keeps day frames off the clip.
	week := plannerScroll(k.calendarClip).Content.(*fyne.Container).Objects[0].(*fyne.Container)
	ruler := week.Objects[0].(*durationRuler)
	if ruler.scale.pixels() != l.pixels() {
		t.Fatal("ruler and bubble scales differ")
	}
	end := plannerDayHeaderHeight + 480*l.pixels() + 8
	if math.Abs(float64(end-week.Size().Height)) > 1 {
		t.Fatal("week's minute grid does not fill the available height", end, week.Size().Height)
	}
	w.Resize(fyne.NewSize(1500, 900))
	w.Content().Refresh()
	if l.pixels() < plannerPixelsPerMinute {
		t.Fatal("week shrank below readable scale")
	}
	k.calendarMode = "Month"
	k.draw()
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if r, ok := o.(*durationRuler); ok && r.scale.pixels() != plannerPixelsPerMinute {
			t.Fatal("month rows expanded their scale")
		}
	})
}

func TestSprintMonthScrollBoostAndOffsetsSurviveRedrawAndViewSwitch(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.kanban
	for i := 0; i < 20; i++ {
		k.plan.Boards[0].Items = append(k.plan.Boards[0].Items, sprintItem{ID: fmt.Sprint(i), Title: "More planning", SprintID: c.Sprint.ID})
	}
	k.calendarMode = "Month"
	k.draw()
	w.Content().Refresh()
	scroll := plannerScroll(k.calendarClip)
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(k.calendarClip).Add(fyne.NewPos(100, 200))
	test.Scroll(w.Canvas(), at, 0, -20)
	if scroll.Offset.Y != 120 {
		t.Fatal("month wheel movement was not boosted", scroll.Offset.Y)
	}
	scroll.ScrollToOffset(fyne.NewPos(0, 1000))
	plannerScroll(k.calendarIssueClip).ScrollToOffset(fyne.NewPos(0, 400))
	k.draw()
	if scrollOffset(k.calendarClip).Y != 1000 || scrollOffset(k.calendarIssueClip).Y != 400 {
		t.Fatal("redraw reset calendar or issues sidebar")
	}
	k.calendarMode = "Week"
	k.draw()
	k.calendarMode = "Month"
	k.draw()
	if scrollOffset(k.calendarClip).Y != 1000 {
		t.Fatal("switching views lost the month position")
	}
	if _, ok := k.calendarClip.(*plannerFastScroll); !ok {
		t.Fatal("month did not use fast scroll")
	}
}

func TestSprintKanbanSidebarClickPreservesItsScroll(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.kanban
	b := &k.plan.Boards[0]
	for i := 1; i < 18; i++ {
		s := sprintIteration{ID: fmt.Sprint(i), Title: fmt.Sprintf("Sprint %d", i), StartDate: addDays(c.Sprint.StartDate, i*14), Duration: 14}
		b.Fields[0].Configuration.Iterations = append(b.Fields[0].Configuration.Iterations, s)
		b.Items = append(b.Items, sprintItem{ID: fmt.Sprint(i), Title: "Issue", SprintID: s.ID})
	}
	w.SetContent(k.root)
	k.draw()
	w.Content().Refresh()
	plannerScroll(k.sideClip).ScrollToOffset(fyne.NewPos(0, 800))
	var row *tappable
	walkCanvas(plannerScroll(k.sideClip).Content, func(o fyne.CanvasObject) {
		if t, ok := o.(*tappable); ok {
			row = t
		}
	})
	if row == nil {
		t.Fatal("sprint rows missing")
	}
	test.Tap(row)
	if scrollOffset(k.sideClip).Y != 800 || k.filter != "sprint" {
		t.Fatal("selecting a lower sprint reset sidebar", scrollOffset(k.sideClip), k.filter)
	}
}

func TestSprintDayHoverClearAndFullBackgroundKeepOtherDays(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.kanban
	day := k.week
	k.plan.Tasks[c.Key] = sprintTask{Notes: "keep", Blocks: []planBlock{{day, 540, 480}, {addDays(day, 1), 540, 30}}}
	k.draw()
	w.Content().Refresh()
	var target *plannerDateTarget
	for _, d := range k.dates {
		if d.date == day {
			target = d
		}
	}
	if target == nil || target.base != dayFill(0, 480) {
		t.Fatal("full day lacks green fill")
	}
	hover := target.object.(*plannerDayHover)
	if hover.clear.Visible() {
		t.Fatal("day Clear should start hidden")
	}
	origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(hover)
	// A scheduled bubble also keeps its day's Clear action visible.
	test.MoveMouse(w.Canvas(), origin.Add(fyne.NewPos(25, plannerDayHeaderHeight+20)))
	if !hover.clear.Visible() {
		t.Fatal("hovering a day bubble did not reveal Clear")
	}
	buttonPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(hover.clear).Add(fyne.NewPos(13, 13))
	test.MoveMouse(w.Canvas(), buttonPos)
	if !hover.clear.Visible() {
		t.Fatal("Clear vanished while moving onto it")
	}
	if dir := os.Getenv("WORKLOG_PLANNER_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		captureSprintPNG(t, w, filepath.Join(dir, "planner-full-day-hover.png"))
	}
	test.Tap(hover.clear)
	if len(k.plan.Tasks[c.Key].Blocks) != 2 {
		t.Fatal("day Clear bypassed confirmation")
	}
	var confirm *widget.Button
	walkPopup(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Clear" {
			confirm = b
		}
	})
	if confirm == nil {
		t.Fatal("day clear confirmation missing")
	}
	test.Tap(confirm)
	p, err := ui.store.loadSprintPlan()
	if err != nil || len(p.Tasks[c.Key].Blocks) != 1 || p.Tasks[c.Key].Blocks[0].Date != addDays(day, 1) || p.Tasks[c.Key].Notes != "keep" {
		t.Fatal("day clear affected other dates or metadata", err)
	}
}
