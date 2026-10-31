package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestSprintSidebarMergesBoardsAndHidesZeroCounts(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	b := k.plan.Boards[0]
	other := b
	other.ID = "other"
	other.URL = sprintBoardURLs[1]
	other.Fields = append([]sprintField{}, b.Fields...)
	iteration := c.Sprint
	iteration.ID = "other-sprint"
	other.Fields[0].Configuration.Iterations = []sprintIteration{iteration, {ID: "empty", StartDate: addDays(iteration.StartDate, 28), Duration: 14}}
	other.Items = []sprintItem{{ID: "j", Title: "Other issue", SprintID: "other-sprint"}}
	k.plan.Boards = append(k.plan.Boards, other)
	groups := k.combinedSprints()
	if len(groups) != 1 || groups[0].Count != 2 {
		t.Fatalf("merged groups: %v", groups)
	}
	if b.sprintByKey(groups[0].Key).ID != "s" || other.sprintByKey(groups[0].Key).ID != "other-sprint" {
		t.Fatal("source IDs not resolved independently")
	}
	k.draw()
	targets := 0
	for _, target := range k.targets {
		if target.kind == "sprint" {
			targets++
			if target.board != "" {
				t.Fatal("sidebar still separates projects")
			}
		}
	}
	if targets != 1 {
		t.Fatalf("expected one nonempty sprint target, got %d", targets)
	}
	k.projectOff = map[string]bool{"other": true}
	if k.combinedSprints()[0].Count != 1 {
		t.Fatal("legend did not filter merged counts")
	}
	if len(k.calendarItems()) != 1 || k.matches(other, other.Items[0]) {
		t.Fatal("project filter did not apply to both menus")
	}
}

func TestSprintCalendarDropdownSelectsFutureSprint(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	b := k.plan.Boards[0]
	future := sprintIteration{ID: "future", Title: "Future sprint", StartDate: addDays(c.Sprint.StartDate, 14), Duration: 14}
	b.Fields[0].Configuration.Iterations = append(b.Fields[0].Configuration.Iterations, future)
	b.Items = append(b.Items, sprintItem{ID: "future-issue", Title: "Future", SprintID: future.ID, Effort: 60})
	k.plan.Boards[0] = b
	k.draw()
	var selector *widget.Select
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if s, ok := o.(*widget.Select); ok && len(s.Options) > 0 && s.Options[0] == calendarSprintLabel(c.Sprint) {
			selector = s
		}
	})
	if selector == nil {
		t.Fatal("sprint dropdown missing")
	}
	selector.SetSelected("Future sprint | " + sprintDateLabel(future))
	items := k.calendarItems()
	if len(items) != 1 || items[0].Item.ID != "future-issue" || k.week != weekStartOf(future.StartDate) {
		t.Fatal("future sprint selection did not switch issues and dates")
	}
	p, err := ui.store.loadSprintPlan()
	if err != nil || p.CalendarSprint != sprintKey(future) {
		t.Fatal("sprint selection was not persisted")
	}
}

func TestSprintResizeGripChangesOnlyLocalDuration(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	k.plan.Tasks[c.Key] = sprintTask{Blocks: []planBlock{{c.Sprint.StartDate, 540, 90}}}
	k.draw()
	card := k.plannerCard(c, 0).(*plannerDragCard)
	var grip *plannerResizeGrip
	walkCanvas(card.content, func(o fyne.CanvasObject) {
		if g, ok := o.(*plannerResizeGrip); ok {
			grip = g
		}
	})
	if grip == nil {
		t.Fatal("resize grip missing")
	}
	before := card.content.MinSize().Height
	grip.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DY: 30 * plannerPixelsPerMinute}})
	if grip.preview != 120 || card.content.MinSize().Height <= before {
		t.Fatal("vertical resize did not preview 30 more minutes")
	}
	grip.DragEnd()
	p, err := ui.store.loadSprintPlan()
	if err != nil || p.Tasks[c.Key].Blocks[0].Minutes != 120 {
		t.Fatal("resized duration not saved")
	}
	if p.Boards[0].Items[0].Effort != 90 || k.busy {
		t.Fatal("resize changed GitHub estimate")
	}
	p.Tasks["busy"] = sprintTask{Blocks: []planBlock{{c.Sprint.StartDate, 720, 60}}}
	if _, err = resizePlannerBlock(p, c.Key, 0, 240); err == nil {
		t.Fatal("overlapping resize accepted")
	}
	p.Tasks[c.Key] = sprintTask{Blocks: []planBlock{{c.Sprint.StartDate, 540, 90}, {c.Sprint.StartDate, 660, 30}}}
	if _, err = resizePlannerBlock(p, c.Key, 0, 180); err == nil {
		t.Fatal("resize overlapped same issue's other block")
	}
}

func TestSprintKanbanConfigurationPersistsAndValidates(t *testing.T) {
	sources := []kanbanSource{{URL: "https://github.com/orgs/example/projects/3/views/4", Filter: `assignee:@me is:open status:"In Progress"`}}
	if err := validateKanbanSources(sources); err != nil {
		t.Fatal(err)
	}
	store := newStore(t.TempDir())
	cfg := Config{KanbanBoards: sources}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, _ := store.LoadConfig()
	if !reflect.DeepEqual(kanbanSources(loaded), sources) {
		t.Fatal("board URL/filter not preserved")
	}
	if len(kanbanSources(Config{KanbanBoards: []kanbanSource{}, ProjectURL: sprintBoardURLs[0]})) != 0 {
		t.Fatal("empty configuration did not disable sources")
	}
	if validateKanbanSources(append(sources, sources[0])) == nil || validateKanbanSources([]kanbanSource{{URL: "https://example.com/orgs/example/projects/3"}}) == nil {
		t.Fatal("invalid board sources accepted")
	}
	if strings.Contains(sprintMetadataQuery, "view(") {
		t.Fatal("view dependency returned")
	}
}

func walkCanvas(o fyne.CanvasObject, visit func(fyne.CanvasObject)) {
	visit(o)
	switch x := o.(type) {
	case *fyne.Container:
		for _, child := range x.Objects {
			walkCanvas(child, visit)
		}
	case *container.Scroll:
		walkCanvas(x.Content, visit)
	case *plannerFastScroll:
		walkCanvas(x.Content, visit)
	case *plannerDayHover:
		walkCanvas(x.body, visit)
	case *plannerDayClearButton:
		visit(&x.Button)
	case *container.ThemeOverride:
		walkCanvas(x.Content, visit)
	case *widget.PopUp:
		walkCanvas(x.Content, visit)
	case *choiceFeedback:
		walkCanvas(x.content, visit)
	case *goldSelect:
		visit(&x.Select)
	case *compactCheckGroup:
		visit(&x.CheckGroup)
	case *tappable:
		walkCanvas(x.content, visit)
	case *sprintDragCard:
		walkCanvas(x.content, visit)
	case *plannerDragCard:
		walkCanvas(x.content, visit)
	case *durationRuler:
		for _, child := range test.WidgetRenderer(x).Objects() {
			walkCanvas(child, visit)
		}
	case *plannerTimedFace:
		for _, child := range test.WidgetRenderer(x).Objects() {
			walkCanvas(child, visit)
		}
	case *plannerIssueLink:
		visit(&x.Hyperlink)
	}
}

func TestLiveSprintConfiguredFilterReadOnly(t *testing.T) {
	if os.Getenv("WORKLOG_KANBAN_LIVE") != "1" {
		t.Skip("opt-in GitHub read")
	}
	filter := `assignee:@me is:open -status:Done`
	b, err := fetchSprintBoardWithFilter(sprintBoardURLs[1], filter)
	if err != nil {
		t.Fatal(err)
	}
	if b.Filter != filter {
		t.Fatal("custom filter replaced")
	}
	for _, it := range b.Items {
		if it.Status == "Done" {
			t.Fatalf("custom filter not applied: %s", it.Status)
		}
	}
	t.Logf("Custom project filter returned %d issues", len(b.Items))
}
