package main

import (
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestSprintDurationSnapsToAbsoluteQuarterHours(t *testing.T) {
	for _, tt := range []struct {
		value     float64
		max, want int
	}{{10, 480, 15}, {22, 480, 15}, {23, 480, 30}, {95, 480, 90}, {100, 480, 105}, {-20, 480, 15}, {500, 143, 135}, {30, 14, 0}} {
		if got := snapPlannerDuration(tt.value, tt.max); got != tt.want {
			t.Errorf("snap(%v, %d) = %d, want %d", tt.value, tt.max, got, tt.want)
		}
	}
	legacy := planBlock{"2026-11-02", 540, 10}
	if err := validatePlannerDuration(legacy, []planBlock{legacy}); err != nil {
		t.Fatal("unchanged saved duration should be retained", err)
	}
	changed := legacy
	changed.Minutes = 25
	if validatePlannerDuration(changed, []planBlock{legacy}) == nil {
		t.Fatal("manual duration accepted an off-grid value")
	}
	p := sprintPlan{Tasks: map[string]sprintTask{"mine": {Blocks: []planBlock{legacy}}}}
	if _, err := resizePlannerBlock(p, "mine", 0, 25); err == nil {
		t.Fatal("resize accepted an off-grid value")
	}
	resized, err := resizePlannerBlock(p, "mine", 0, 30)
	if err != nil || resized.Tasks["mine"].Blocks[0].Minutes != 30 || p.Tasks["mine"].Blocks[0].Minutes != 10 {
		t.Fatal("resize should save a valid duration without mutating the original", err)
	}
}

func TestSprintDurationAllocationRoundsLocallyAndPreservesAppointments(t *testing.T) {
	s := sprintIteration{StartDate: "2026-11-02", Duration: 14}
	c := sprintCandidate{Key: "mine", Item: sprintItem{Effort: 95}, Sprint: s}
	legacy := []planBlock{{s.StartDate, 550, 10}, {s.StartDate, 600, 10}}
	p := sprintPlan{Tasks: map[string]sprintTask{"busy": {Blocks: legacy}}}
	block, ok := plannerSlot(p, c, -1, s.StartDate)
	if !ok || block.Minutes != 105 || c.Item.Effort != 95 {
		t.Fatal("drop should round the local block up without changing source effort")
	}
	tasks, unplaced := allocateSprint(p, []sprintCandidate{c}, s.StartDate)
	if len(unplaced) != 0 || !reflect.DeepEqual(tasks["busy"].Blocks, legacy) {
		t.Fatal("allocation changed existing appointments or failed to fit")
	}
	total := 0
	for _, b := range tasks[c.Key].Blocks {
		if !validPlannerDuration(b.Minutes) {
			t.Fatalf("allocation created an off-grid block: %+v", b)
		}
		if err := validatePlanBlock(c.Key, b, p.Tasks); err != nil {
			t.Fatal(err)
		}
		total += b.Minutes
	}
	if total != 105 || sprintEstimate(c.Item, tasks[c.Key]) != 95 {
		t.Fatal("allocation should round only the planned time")
	}
	c.Item.Effort = 10
	block, ok = plannerSlot(p, c, -1, s.StartDate)
	if !ok || block.Minutes != 15 {
		t.Fatal("small project estimates should produce a minimum 15-minute block")
	}
}

func TestSprintDurationRulerFollowsResizeAndUsesMinuteLabels(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	k.plan.Tasks[c.Key] = sprintTask{Blocks: []planBlock{{c.Sprint.StartDate, 540, 10}}}
	card := k.plannerCard(c, 0).(*plannerDragCard)
	var grip *plannerResizeGrip
	walkCanvas(card.content, func(o fyne.CanvasObject) {
		if g, ok := o.(*plannerResizeGrip); ok {
			grip = g
		}
	})
	if grip == nil || grip.face == nil {
		t.Fatal("bubble edge or face missing")
	}
	before := card.MinSize().Height
	grip.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DY: 15 * plannerPixelsPerMinute}})
	if grip.preview != 30 || card.MinSize().Height != 30*plannerPixelsPerMinute || card.MinSize().Height <= before {
		t.Fatal("legacy duration did not snap to an absolute 15-minute step")
	}
	var ruler *durationRuler
	count := 0
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if r, ok := o.(*durationRuler); ok {
			ruler = r
			count++
		}
	})
	if count != 1 {
		t.Fatalf("expected one shared ruler per week, got %d", count)
	}
	r := test.WidgetRenderer(ruler).(*durationRulerRenderer)
	r.Layout(fyne.NewSize(1000, r.MinSize().Height))
	if len(r.marks) != 33 || r.marks[1] != 15 || r.marks[len(r.marks)-1] != 480 {
		t.Fatalf("unexpected quarter-hour grid: %v", r.marks)
	}
	for i, label := range r.labels {
		if !strings.HasSuffix(label.Text, "m") || strings.Contains(label.Text, ":") {
			t.Fatal("ruler should show elapsed minutes only")
		}
		if i > 0 && label.Position().Y-r.labels[i-1].Position().Y < label.MinSize().Height {
			t.Fatal("ruler labels overlap")
		}
	}
	if r.lines[4].Position1.Y != plannerDayHeaderHeight+60*ruler.scale.pixels() {
		t.Fatal("ruler and bubble scale do not match")
	}
	grip.DragEnd()
	saved, err := ui.store.loadSprintPlan()
	if err != nil || saved.Tasks[c.Key].Blocks[0].Minutes != 30 || saved.Boards[0].Items[0].Effort != 90 {
		t.Fatal("resize must persist locally and preserve GitHub effort", err)
	}
}
