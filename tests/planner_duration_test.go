package tests

import (
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestSprintDurationSnapsToAbsoluteQuarterHours(t *testing.T) {
	for _, tt := range []struct {
		value     float64
		max, want int
	}{{10, 480, 15}, {22, 480, 15}, {23, 480, 30}, {95, 480, 90}, {100, 480, 105}, {-20, 480, 15}, {500, 143, 135}, {30, 14, 0}} {
		if got := sprintcalendar.SnapPlannerDuration(tt.value, tt.max); got != tt.want {
			t.Errorf("snap(%v, %d) = %d, want %d", tt.value, tt.max, got, tt.want)
		}
	}
	legacy := backend.PlanBlock{Date: "2026-11-02", Start: 540, Minutes: 10}
	if err := backend.ValidatePlannerDuration(legacy, []backend.PlanBlock{legacy}); err != nil {
		t.Fatal("unchanged saved duration should be retained", err)
	}
	changed := legacy
	changed.Minutes = 25
	if backend.ValidatePlannerDuration(changed, []backend.PlanBlock{legacy}) == nil {
		t.Fatal("manual duration accepted an off-grid value")
	}
	p := backend.SprintPlan{Tasks: map[string]backend.SprintTask{"mine": {Blocks: []backend.PlanBlock{legacy}}}}
	if _, err := backend.ResizePlannerBlock(p, "mine", 0, 25); err == nil {
		t.Fatal("resize accepted an off-grid value")
	}
	resized, err := backend.ResizePlannerBlock(p, "mine", 0, 30)
	if err != nil || resized.Tasks["mine"].Blocks[0].Minutes != 30 || p.Tasks["mine"].Blocks[0].Minutes != 10 {
		t.Fatal("resize should save a valid duration without mutating the original", err)
	}
}

func TestSprintDurationAllocationRoundsLocallyAndPreservesAppointments(t *testing.T) {
	s := backend.SprintIteration{StartDate: "2026-11-02", Duration: 14}
	c := backend.SprintCandidate{Key: "mine", Item: backend.SprintItem{Effort: 95}, Sprint: s}
	legacy := []backend.PlanBlock{{Date: s.StartDate, Start: 550, Minutes: 10}, {Date: s.StartDate, Start: 600, Minutes: 10}}
	p := backend.SprintPlan{Tasks: map[string]backend.SprintTask{"busy": {Blocks: legacy}}}
	block, ok := backend.PlannerSlot(p, c, -1, s.StartDate)
	if !ok || block.Minutes != 105 || c.Item.Effort != 95 {
		t.Fatal("drop should round the local block up without changing source effort")
	}
	tasks, unplaced := backend.AllocateSprint(p, []backend.SprintCandidate{c}, s.StartDate)
	if len(unplaced) != 0 || !reflect.DeepEqual(tasks["busy"].Blocks, legacy) {
		t.Fatal("allocation changed existing appointments or failed to fit")
	}
	total := 0
	for _, b := range tasks[c.Key].Blocks {
		if !backend.ValidPlannerDuration(b.Minutes) {
			t.Fatalf("allocation created an off-grid block: %+v", b)
		}
		if err := backend.ValidatePlanBlock(c.Key, b, p.Tasks); err != nil {
			t.Fatal(err)
		}
		total += b.Minutes
	}
	if total != 105 || backend.SprintEstimate(c.Item, tasks[c.Key]) != 95 {
		t.Fatal("allocation should round only the planned time")
	}
	c.Item.Effort = 10
	block, ok = backend.PlannerSlot(p, c, -1, s.StartDate)
	if !ok || block.Minutes != 15 {
		t.Fatal("small project estimates should produce a minimum 15-minute block")
	}
}

func TestSprintDurationRulerFollowsResizeAndUsesMinuteLabels(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: c.Sprint.StartDate, Start: 540, Minutes: 10}}}
	card := sprintcalendar.PlannerCard(k, c, 0).(*shell.PlannerDragCard)
	var grip *sprintcalendar.PlannerResizeGrip
	walkCanvas(card.Content, func(o fyne.CanvasObject) {
		if g, ok := o.(*sprintcalendar.PlannerResizeGrip); ok {
			grip = g
		}
	})
	if grip == nil || grip.Face == nil {
		t.Fatal("bubble edge or face missing")
	}
	before := card.MinSize().Height
	grip.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DY: 15 * sprintcalendar.PlannerPixelsPerMinute}})
	if grip.Preview != 30 || card.MinSize().Height != 30*sprintcalendar.PlannerPixelsPerMinute || card.MinSize().Height <= before {
		t.Fatal("legacy duration did not snap to an absolute 15-minute step")
	}
	var ruler *sprintcalendar.DurationRuler
	count := 0
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if r, ok := o.(*sprintcalendar.DurationRuler); ok {
			ruler = r
			count++
		}
	})
	if count != 1 {
		t.Fatalf("expected one shared ruler per week, got %d", count)
	}
	r := test.WidgetRenderer(ruler).(*sprintcalendar.DurationRulerRenderer)
	r.Layout(fyne.NewSize(1000, r.MinSize().Height))
	if len(r.Marks) != 33 || r.Marks[1] != 15 || r.Marks[len(r.Marks)-1] != 480 {
		t.Fatalf("unexpected quarter-hour grid: %v", r.Marks)
	}
	for i, label := range r.Labels {
		if !strings.HasSuffix(label.Text, "m") || strings.Contains(label.Text, ":") {
			t.Fatal("ruler should show elapsed minutes only")
		}
		if i > 0 && label.Position().Y-r.Labels[i-1].Position().Y < label.MinSize().Height {
			t.Fatal("ruler labels overlap")
		}
	}
	if r.Lines[4].Position1.Y != sprintcalendar.PlannerDayHeaderHeight+60*ruler.Scale.Pixels() {
		t.Fatal("ruler and bubble scale do not match")
	}
	grip.DragEnd()
	saved, err := ui.Store.LoadSprintPlan()
	if err != nil || saved.Tasks[c.Key].Blocks[0].Minutes != 30 || k.Plan.Boards[0].Items[0].Effort != 90 {
		t.Fatal("resize must persist locally and preserve GitHub effort", err)
	}
}
