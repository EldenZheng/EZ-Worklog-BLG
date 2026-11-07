package tests

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestSprintSidebarMergesBoardsAndHidesZeroCounts(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	b := k.Plan.Boards[0]
	other := b
	other.ID = "other"
	other.URL = backend.SprintBoardURLs[1]
	other.Fields = append([]backend.SprintField{}, b.Fields...)
	iteration := c.Sprint
	iteration.ID = "other-sprint"
	other.Fields[0].Configuration.Iterations = []backend.SprintIteration{iteration, {ID: "empty", StartDate: backend.AddDays(iteration.StartDate, 28), Duration: 14}}
	other.Items = []backend.SprintItem{{ID: "j", Title: "Other issue", SprintID: "other-sprint"}}
	k.Plan.Boards = append(k.Plan.Boards, other)
	groups := k.CombinedSprints()
	if len(groups) != 1 || groups[0].Count != 2 {
		t.Fatalf("merged groups: %v", groups)
	}
	if b.SprintByKey(groups[0].Key).ID != "s" || other.SprintByKey(groups[0].Key).ID != "other-sprint" {
		t.Fatal("source IDs not resolved independently")
	}
	kanban.Draw(k)
	targets := 0
	for _, target := range k.Targets {
		if target.Kind == "sprint" {
			targets++
			if target.Board != "" {
				t.Fatal("sidebar still separates projects")
			}
		}
	}
	if targets != 1 {
		t.Fatalf("expected one nonempty sprint target, got %d", targets)
	}
	k.ProjectOff = map[string]bool{"other": true}
	if k.CombinedSprints()[0].Count != 1 {
		t.Fatal("legend did not filter merged counts")
	}
	if len(k.CalendarItems()) != 1 || kanban.Matches(k, other, other.Items[0]) {
		t.Fatal("project filter did not apply to both menus")
	}
}

func TestSprintCalendarDropdownSelectsFutureSprint(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	b := k.Plan.Boards[0]
	future := backend.SprintIteration{ID: "future", Title: "Future sprint", StartDate: backend.AddDays(c.Sprint.StartDate, 14), Duration: 14}
	b.Fields[0].Configuration.Iterations = append(b.Fields[0].Configuration.Iterations, future)
	b.Items = append(b.Items, backend.SprintItem{ID: "future-issue", Title: "Future", SprintID: future.ID, Effort: 60})
	k.Plan.Boards[0] = b
	kanban.Draw(k)
	var selector *widget.Select
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if s, ok := o.(*widget.Select); ok && len(s.Options) > 0 && s.Options[0] == sprintcalendar.CalendarSprintLabel(c.Sprint) {
			selector = s
		}
	})
	if selector == nil {
		t.Fatal("sprint dropdown missing")
	}
	selector.SetSelected("Future sprint | " + shell.SprintDateLabel(future))
	items := k.CalendarItems()
	if len(items) != 1 || items[0].Item.ID != "future-issue" || k.Week != backend.WeekStartOf(future.StartDate) {
		t.Fatal("future sprint selection did not switch issues and dates")
	}
	p, err := ui.Store.LoadSprintPlan()
	if err != nil || p.CalendarSprint != backend.SprintKey(future) {
		t.Fatal("sprint selection was not persisted")
	}
}

func TestSprintResizeGripChangesOnlyLocalDuration(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{c.Sprint.StartDate, 540, 90}}}
	kanban.Draw(k)
	card := sprintcalendar.PlannerCard(k, c, 0).(*shell.PlannerDragCard)
	var grip *sprintcalendar.PlannerResizeGrip
	walkCanvas(card.Content, func(o fyne.CanvasObject) {
		if g, ok := o.(*sprintcalendar.PlannerResizeGrip); ok {
			grip = g
		}
	})
	if grip == nil {
		t.Fatal("resize grip missing")
	}
	before := card.Content.MinSize().Height
	grip.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DY: 30 * sprintcalendar.PlannerPixelsPerMinute}})
	if grip.Preview != 120 || card.Content.MinSize().Height <= before {
		t.Fatal("vertical resize did not preview 30 more minutes")
	}
	grip.DragEnd()
	p, err := ui.Store.LoadSprintPlan()
	if err != nil || p.Tasks[c.Key].Blocks[0].Minutes != 120 {
		t.Fatal("resized duration not saved")
	}
	if k.Plan.Boards[0].Items[0].Effort != 90 || k.Busy {
		t.Fatal("resize changed GitHub estimate")
	}
	p.Tasks["busy"] = backend.SprintTask{Blocks: []backend.PlanBlock{{c.Sprint.StartDate, 720, 60}}}
	if _, err = backend.ResizePlannerBlock(p, c.Key, 0, 240); err == nil {
		t.Fatal("overlapping resize accepted")
	}
	p.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{c.Sprint.StartDate, 540, 90}, {c.Sprint.StartDate, 660, 30}}}
	if _, err = backend.ResizePlannerBlock(p, c.Key, 0, 180); err == nil {
		t.Fatal("resize overlapped same issue's other block")
	}
}

func TestSprintKanbanConfigurationPersistsAndValidates(t *testing.T) {
	sources := []backend.KanbanSource{{URL: "https://github.com/orgs/example/projects/3/views/4", Filter: `assignee:@me is:open status:"In Progress"`}}
	if err := backend.ValidateKanbanSources(sources); err != nil {
		t.Fatal(err)
	}
	store := backend.NewStore(tempDir(t))
	cfg := backend.Config{KanbanBoards: sources}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, _ := store.LoadConfig()
	if !reflect.DeepEqual(backend.KanbanSources(loaded), sources) {
		t.Fatal("board URL/filter not preserved")
	}
	if len(backend.KanbanSources(backend.Config{KanbanBoards: []backend.KanbanSource{}, ProjectURL: backend.SprintBoardURLs[0]})) != 0 {
		t.Fatal("empty configuration did not disable sources")
	}
	if backend.ValidateKanbanSources(append(sources, sources[0])) == nil || backend.ValidateKanbanSources([]backend.KanbanSource{{URL: "https://example.com/orgs/example/projects/3"}}) == nil {
		t.Fatal("invalid board sources accepted")
	}
	if strings.Contains(backend.SprintMetadataQuery, "view(") {
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
	case *shell.PlannerFastScroll:
		walkCanvas(x.Content, visit)
	case *shell.PlannerDayHover:
		walkCanvas(x.Body, visit)
	case *shell.PlannerDayClearButton:
		visit(&x.Button)
	case *container.ThemeOverride:
		walkCanvas(x.Content, visit)
	case *widget.PopUp:
		walkCanvas(x.Content, visit)
	case *widgets.ChoiceFeedback:
		walkCanvas(x.Content, visit)
	case *shell.GoldSelect:
		visit(&x.Select)
	case *widgets.CompactCheckGroup:
		visit(&x.CheckGroup)
	case *shell.Tappable:
		walkCanvas(x.Content, visit)
	case *shell.Fold:
		walkCanvas(x.Content, visit)
	case *shell.StripeBubble:
		walkCanvas(x.Body, visit)
	case *shell.HoverArea:
		walkCanvas(x.Content, visit)
	case *shell.HoverRegion:
		walkCanvas(x.Content, visit)
	case *shell.Wipe:
		walkCanvas(x.Content, visit)
	case *shell.Morph:
		walkCanvas(x.Content, visit)
	case *shell.SprintDragCard:
		walkCanvas(x.Content, visit)
	case *shell.ProjectChip:
		walkCanvas(x.Chip, visit)
	case *shell.PlannerDragCard:
		walkCanvas(x.Content, visit)
	case *sprintcalendar.DurationRuler:
		for _, child := range test.WidgetRenderer(x).Objects() {
			walkCanvas(child, visit)
		}
	case *sprintcalendar.PlannerTimedFace:
		for _, child := range test.WidgetRenderer(x).Objects() {
			walkCanvas(child, visit)
		}
	case *sprintcalendar.PlannerIssueLink:
		visit(&x.Hyperlink)
	}
}

func TestLiveSprintConfiguredFilterReadOnly(t *testing.T) {
	if os.Getenv("WORKLOG_KANBAN_LIVE") != "1" {
		t.Skip("opt-in GitHub read")
	}
	filter := `assignee:@me is:open -status:Done`
	b, err := backend.FetchSprintBoardWithFilter(backend.SprintBoardURLs[1], filter)
	if err != nil {
		t.Fatal(err)
	}
	if b.Filter != filter {
		t.Fatal("custom filter replaced")
	}
	current := b.Current(shell.Today()).ID
	for _, it := range b.Items {
		if it.Status == "Done" && it.SprintID != current {
			t.Fatalf("Done item outside the current sprint: %s", it.Title)
		}
	}
	t.Logf("Custom project filter returned %d issues", len(b.Items))
}

func TestHiddenDoneKeepsOnlyCurrentSprintDone(t *testing.T) {
	f := backend.SprintField{Name: "Sprint"}
	f.Configuration.CompletedIterations = []backend.SprintIteration{{ID: "old", StartDate: "2026-10-19", Duration: 14}}
	f.Configuration.Iterations = []backend.SprintIteration{{ID: "now", StartDate: "2026-11-02", Duration: 14}, {ID: "next", StartDate: "2026-11-16", Duration: 14}}
	b := backend.SprintBoard{Fields: []backend.SprintField{f}}
	cases := []struct {
		it   backend.SprintItem
		want bool
	}{
		{backend.SprintItem{Status: "Done", SprintID: "now"}, false},
		{backend.SprintItem{Status: " done ", SprintID: "now"}, false},
		{backend.SprintItem{Status: "Done", SprintID: "old"}, true},
		{backend.SprintItem{Status: "Done", SprintID: "next"}, true},
		{backend.SprintItem{Status: "Done"}, true},
		{backend.SprintItem{Status: "In Progress", SprintID: "old"}, false},
		{backend.SprintItem{Status: "Created"}, false},
	}
	for _, c := range cases {
		if got := b.HiddenDone(c.it, "2026-11-05"); got != c.want {
			t.Errorf("HiddenDone(%+v) = %v, want %v", c.it, got, c.want)
		}
	}
	if !b.HiddenDone(backend.SprintItem{Status: "Done", SprintID: "now"}, "2027-01-01") {
		t.Error("Done shown with no sprint running")
	}
}

func TestCurrentDoneFilterOverridesStatusTerms(t *testing.T) {
	cases := map[string]string{
		"assignee:@me is:open -status:Done": "assignee:@me status:Done sprint:@current",
		"":                                  "status:Done sprint:@current",
		`repo:o/r status:"In Progress",Created label:"a b"`: `repo:o/r label:"a b" status:Done sprint:@current`,
		"assignee:@me sprint:@next -no:sprint has:status":   "assignee:@me status:Done sprint:@current",
	}
	for in, want := range cases {
		if got := backend.CurrentDoneFilter(in); got != want {
			t.Errorf("CurrentDoneFilter(%q) = %q, want %q", in, got, want)
		}
	}
}
