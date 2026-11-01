package tests

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestSprintWeekExpandsScaleButMonthKeepsNaturalHeight(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{k.Week, 540, 120}}}
	w.Resize(fyne.NewSize(1500, 1250))
	kanban.Draw(k)
	w.Content().Refresh()
	var face *shell.PlannerDragCard
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*shell.PlannerDragCard); ok && card.Index == 0 {
			face = card
		}
	})
	l := face.Content.(*fyne.Container).Layout.(*sprintcalendar.PlannerDurationLayout)
	if l.Pixels() <= sprintcalendar.PlannerPixelsPerMinute {
		t.Fatal("week did not expand into spare height")
	}
	if math.Abs(float64(face.Size().Height-120*l.Pixels())) > 0.1 {
		t.Fatal("expanded bubble does not follow ruler")
	}
	// The week sits inside the top inset that keeps day frames off the clip.
	week := shell.PlannerScroll(k.CalendarClip).Content.(*fyne.Container).Objects[0].(*fyne.Container)
	ruler := week.Objects[0].(*sprintcalendar.DurationRuler)
	if ruler.Scale.Pixels() != l.Pixels() {
		t.Fatal("ruler and bubble scales differ")
	}
	end := sprintcalendar.PlannerDayHeaderHeight + 480*l.Pixels() + 8
	if math.Abs(float64(end-week.Size().Height)) > 1 {
		t.Fatal("week's minute grid does not fill the available height", end, week.Size().Height)
	}
	w.Resize(fyne.NewSize(1500, 900))
	w.Content().Refresh()
	if l.Pixels() < sprintcalendar.PlannerPixelsPerMinute {
		t.Fatal("week shrank below readable scale")
	}
	k.CalendarMode = "Month"
	kanban.Draw(k)
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if r, ok := o.(*sprintcalendar.DurationRuler); ok && r.Scale.Pixels() != sprintcalendar.PlannerPixelsPerMinute {
			t.Fatal("month rows expanded their scale")
		}
	})
}

func TestSprintMonthScrollBoostAndOffsetsSurviveRedrawAndViewSwitch(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	for i := 0; i < 20; i++ {
		k.Plan.Boards[0].Items = append(k.Plan.Boards[0].Items, backend.SprintItem{ID: fmt.Sprint(i), Title: "More planning", SprintID: c.Sprint.ID})
	}
	k.CalendarMode = "Month"
	kanban.Draw(k)
	w.Content().Refresh()
	scroll := shell.PlannerScroll(k.CalendarClip)
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(k.CalendarClip).Add(fyne.NewPos(100, 200))
	test.Scroll(w.Canvas(), at, 0, -20)
	if scroll.Offset.Y != 120 {
		t.Fatal("month wheel movement was not boosted", scroll.Offset.Y)
	}
	scroll.ScrollToOffset(fyne.NewPos(0, 1000))
	shell.PlannerScroll(k.CalendarIssueClip).ScrollToOffset(fyne.NewPos(0, 400))
	kanban.Draw(k)
	if shell.ScrollOffset(k.CalendarClip).Y != 1000 || shell.ScrollOffset(k.CalendarIssueClip).Y != 400 {
		t.Fatal("redraw reset calendar or issues sidebar")
	}
	k.CalendarMode = "Week"
	kanban.Draw(k)
	k.CalendarMode = "Month"
	kanban.Draw(k)
	if shell.ScrollOffset(k.CalendarClip).Y != 1000 {
		t.Fatal("switching views lost the month position")
	}
	if _, ok := k.CalendarClip.(*shell.PlannerFastScroll); !ok {
		t.Fatal("month did not use fast scroll")
	}
}

func TestSprintKanbanSidebarClickPreservesItsScroll(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	b := &k.Plan.Boards[0]
	for i := 1; i < 18; i++ {
		s := backend.SprintIteration{ID: fmt.Sprint(i), Title: fmt.Sprintf("Sprint %d", i), StartDate: backend.AddDays(c.Sprint.StartDate, i*14), Duration: 14}
		b.Fields[0].Configuration.Iterations = append(b.Fields[0].Configuration.Iterations, s)
		b.Items = append(b.Items, backend.SprintItem{ID: fmt.Sprint(i), Title: "Issue", SprintID: s.ID})
	}
	w.SetContent(k.Root)
	kanban.Draw(k)
	w.Content().Refresh()
	shell.PlannerScroll(k.SideClip).ScrollToOffset(fyne.NewPos(0, 800))
	var row *shell.Tappable
	walkCanvas(shell.PlannerScroll(k.SideClip).Content, func(o fyne.CanvasObject) {
		if t, ok := o.(*shell.Tappable); ok {
			row = t
		}
	})
	if row == nil {
		t.Fatal("sprint rows missing")
	}
	test.Tap(row)
	if shell.ScrollOffset(k.SideClip).Y != 800 || k.Filter != "sprint" {
		t.Fatal("selecting a lower sprint reset sidebar", shell.ScrollOffset(k.SideClip), k.Filter)
	}
}

func TestSprintDayHoverClearAndFullBackgroundKeepOtherDays(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	day := k.Week
	k.Plan.Tasks[c.Key] = backend.SprintTask{Notes: "keep", Blocks: []backend.PlanBlock{{day, 540, 480}, {backend.AddDays(day, 1), 540, 30}}}
	kanban.Draw(k)
	w.Content().Refresh()
	var target *shell.PlannerDateTarget
	for _, d := range k.Dates {
		if d.Date == day {
			target = d
		}
	}
	if target == nil || target.Base != shell.DayFill(0, 480) {
		t.Fatal("full day lacks green fill")
	}
	hover := target.Object.(*shell.PlannerDayHover)
	if hover.Clear.Visible() {
		t.Fatal("day Clear should start hidden")
	}
	origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(hover)
	// A scheduled bubble also keeps its day's Clear action visible.
	test.MoveMouse(w.Canvas(), origin.Add(fyne.NewPos(25, sprintcalendar.PlannerDayHeaderHeight+20)))
	if !hover.Clear.Visible() {
		t.Fatal("hovering a day bubble did not reveal Clear")
	}
	buttonPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(hover.Clear).Add(fyne.NewPos(13, 13))
	test.MoveMouse(w.Canvas(), buttonPos)
	if !hover.Clear.Visible() {
		t.Fatal("Clear vanished while moving onto it")
	}
	if dir := os.Getenv("WORKLOG_PLANNER_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		captureSprintPNG(t, w, filepath.Join(dir, "planner-full-day-hover.png"))
	}
	test.Tap(hover.Clear)
	if len(k.Plan.Tasks[c.Key].Blocks) != 2 {
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
	p, err := ui.Store.LoadSprintPlan()
	if err != nil || len(p.Tasks[c.Key].Blocks) != 1 || p.Tasks[c.Key].Blocks[0].Date != backend.AddDays(day, 1) || p.Tasks[c.Key].Notes != "keep" {
		t.Fatal("day clear affected other dates or metadata", err)
	}
}
