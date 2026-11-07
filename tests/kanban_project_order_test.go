package tests

import (
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"

	"worklog/internal/menus/kanban"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func twoProjectFixture(t *testing.T) (*shell.UI, fyne.Window) {
	t.Helper()
	ui, w, _ := plannerFixture(t)
	k := ui.Kanban
	status := backend.SprintField{Name: "Status", Options: []backend.FieldOption{{Name: "Created"}, {Name: "In Progress"}}}
	first := &k.Plan.Boards[0]
	first.Fields = append(first.Fields, status)
	first.Items = []backend.SprintItem{
		{ID: "a1", Title: "A1", Status: "Created", SprintID: "s"},
		{ID: "a2", Title: "A2", Status: "Created", SprintID: "s"},
		{ID: "a3", Title: "A3", Status: "Created", SprintID: "s"},
		{ID: "a4", Title: "A4", Status: "In Progress", SprintID: "s"},
	}
	second := *first
	second.ID, second.Title, second.URL = "q", "Second", backend.SprintBoardURLs[1]
	second.Items = []backend.SprintItem{
		{ID: "b1", Title: "B1", Status: "Created", SprintID: "s"},
		{ID: "b2", Title: "B2", Status: "Created", SprintID: "s"},
	}
	k.Plan.Boards = append(k.Plan.Boards, second)
	k.Filter = "all"
	w.SetContent(k.Root)
	w.Resize(fyne.NewSize(1400, 950))
	kanban.Draw(k)
	w.Content().Refresh()
	return ui, w
}

func columnIDs(k *shell.KanbanUI, status string) string {
	var ids []string
	for _, c := range kanban.ColumnOrder(k, status) {
		ids = append(ids, c.Item.ID)
	}
	return strings.Join(ids, ",")
}

func openIDs(k *shell.KanbanUI) string {
	var ids []string
	for _, c := range k.OpenCalendarItems() {
		ids = append(ids, c.Item.ID)
	}
	return strings.Join(ids, ",")
}

func boardIDs(boards []backend.SprintBoard) []string {
	var ids []string
	for _, b := range boards {
		ids = append(ids, b.ID)
	}
	return ids
}

func legendChips(page fyne.CanvasObject) []*shell.ProjectChip {
	var chips []*shell.ProjectChip
	walkCanvas(page, func(o fyne.CanvasObject) {
		if c, ok := o.(*shell.ProjectChip); ok {
			chips = append(chips, c)
		}
	})
	return chips
}

// The titles of the Sprint Issues cards as drawn, meeting first.
func trayTitles(k *shell.KanbanUI) []string {
	var titles []string
	walkCanvas(k.CalendarIssueClip, func(o fyne.CanvasObject) {
		if c, ok := o.(*shell.PlannerDragCard); ok {
			titles = append(titles, c.Candidate.Item.Title)
		}
	})
	return titles
}

// Projects share positions rather than stacking, and a project that runs out
// leaves the rest to the others.
func TestColumnsShareProjectPositions(t *testing.T) {
	ui, _ := twoProjectFixture(t)
	k := ui.Kanban
	if got := columnIDs(k, "Created"); got != "a1,b1,a2,b2,a3" {
		t.Fatal(got)
	}
	if got := columnIDs(k, "In Progress"); got != "a4" {
		t.Fatal("a project's position counts only its cards in the column", got)
	}
	var drawn []string
	for _, c := range k.ColumnCards["Created"] {
		drawn = append(drawn, c.Item.ID)
	}
	if strings.Join(drawn, ",") != "a1,b1,a2,b2,a3" {
		t.Fatal("board not drawn in the shared order", drawn)
	}
	k.Plan.ProjectOrder = []string{"q", "p"}
	if got := columnIDs(k, "Created"); got != "b1,a1,b2,a2,a3" {
		t.Fatal("legend order ignored", got)
	}
	k.ProjectOff = map[string]bool{"p": true}
	if got := columnIDs(k, "Created"); got != "b1,b2" {
		t.Fatal("hidden project still shown", got)
	}
}

// Sprint Issues merges the same way, and the merge skips finished issues, so
// they do not push a project's open issues to the bottom.
func TestSprintIssuesShareProjectPositions(t *testing.T) {
	ui, _ := twoProjectFixture(t)
	k := ui.Kanban
	kanban.Draw(k)
	if got := trayTitles(k); !reflect.DeepEqual(got, []string{"MEETING", "A1", "B1", "A2", "B2", "A3", "A4"}) {
		t.Fatal("tray order", got)
	}
	k.Plan.ProjectOrder = []string{"q", "p"}
	kanban.Draw(k)
	if got := trayTitles(k); !reflect.DeepEqual(got, []string{"MEETING", "B1", "A1", "B2", "A2", "A3", "A4"}) {
		t.Fatal("tray ignores the legend order", got)
	}
	k.Plan.ProjectOrder = nil
	k.Plan.Boards[0].Items[0].Status = "Done"
	k.Plan.Tasks["p/a2"] = backend.SprintTask{Done: true}
	if got := openIDs(k); got != "a3,b1,a4,b2" {
		t.Fatal("finished issues hold positions", got)
	}
}

// "Allocate unscheduled" fills the calendar in the order Sprint Issues lists.
func TestAllocateFollowsSprintIssuesOrder(t *testing.T) {
	ui, _ := twoProjectFixture(t)
	k := ui.Kanban
	k.Plan.ProjectOrder = []string{"q", "p"}
	var order []string
	for _, c := range k.OpenCalendarItems() {
		order = append(order, c.Key)
	}
	if !reflect.DeepEqual(order, []string{"q/b1", "p/a1", "q/b2", "p/a2", "p/a3", "p/a4"}) {
		t.Fatal(order)
	}
	tasks, _ := backend.AllocateSprint(k.Plan, k.OpenCalendarItems(), k.OpenCalendarItems()[0].Sprint.StartDate)
	first, second := tasks["q/b1"].Blocks, tasks["p/a1"].Blocks
	if len(first) == 0 || len(second) == 0 || first[0].Date > second[0].Date || first[0].Date == second[0].Date && first[0].Start > second[0].Start {
		t.Fatal("first listed issue was not placed first", first, second)
	}
}

// A board the saved order does not know goes last; a saved board that has gone
// is skipped.
func TestOrderedBoardsHandlesUnknownBoards(t *testing.T) {
	ui, _ := twoProjectFixture(t)
	k := ui.Kanban
	k.Plan.ProjectOrder = []string{"gone", "q"}
	if got := boardIDs(k.OrderedBoards()); !reflect.DeepEqual(got, []string{"q", "p"}) {
		t.Fatal(got)
	}
	if got := boardIDs(k.Plan.Boards); !reflect.DeepEqual(got, []string{"p", "q"}) {
		t.Fatal("the shared board order changed", got)
	}
}

func TestLegendSlot(t *testing.T) {
	spans := []shell.ChipSpan{{"a", 0, 100}, {"b", 110, 210}, {"c", 220, 320}}
	for _, tc := range []struct {
		drag string
		x    float32
		want string
		line float32
	}{
		{"c", 10, "c,a,b", -5},
		{"c", 120, "a,c,b", 105},
		{"a", 300, "b,c,a", 325},
		{"a", 170, "b,a,c", 215},
		{"b", 120, "a,b,c", 105},
	} {
		order, line := shell.LegendSlot(spans, tc.drag, tc.x, 10)
		if strings.Join(order, ",") != tc.want || line != tc.line {
			t.Fatal(tc, order, line)
		}
	}
	if order, _ := shell.LegendSlot(spans[:1], "a", 50, 10); !reflect.DeepEqual(order, []string{"a"}) {
		t.Fatal("lone chip", order)
	}
}

func dragChipOnto(drag, onto *shell.ProjectChip) {
	over := fyne.CurrentApp().Driver().AbsolutePositionForObject(onto).Add(fyne.NewPos(5, onto.Size().Height/2))
	drag.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: over}})
}

// Dragging a chip on Kanban reorders both menus and survives a restart; the
// board list keeps its order.
func TestDraggingKanbanChipReordersBothMenus(t *testing.T) {
	ui, _ := twoProjectFixture(t)
	k := ui.Kanban
	chips := legendChips(k.Body)
	if len(chips) != 2 || chips[0].Board.ID != "p" {
		t.Fatal("legend chips", len(chips))
	}
	dragChipOnto(chips[1], chips[0])
	if k.DropLine == nil || !k.DropLine.Visible() {
		t.Fatal("drop line not shown")
	}
	chips[1].DragEnd()
	if !reflect.DeepEqual(k.Plan.ProjectOrder, []string{"q", "p"}) {
		t.Fatal("order not set", k.Plan.ProjectOrder)
	}
	if len(k.GhostLayer.Objects) != 0 || k.Dragging {
		t.Fatal("drag state left behind")
	}
	if got := legendChips(k.Body); got[0].Board.ID != "q" {
		t.Fatal("legend not redrawn in the new order")
	}
	if columnIDs(k, "Created") != "b1,a1,b2,a2,a3" {
		t.Fatal("columns not reordered", columnIDs(k, "Created"))
	}
	saved, err := ui.Store.LoadSprintPlan()
	if err != nil || !reflect.DeepEqual(saved.ProjectOrder, []string{"q", "p"}) {
		t.Fatal("order not saved", saved.ProjectOrder, err)
	}
	if got := boardIDs(k.Plan.Boards); !reflect.DeepEqual(got, []string{"p", "q"}) {
		t.Fatal("the shared board order changed", got)
	}
	var calendar []string
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if text, ok := o.(*canvas.Text); ok && strings.Contains(text.Text, " estimated)") {
			calendar = append(calendar, strings.Fields(text.Text)[0])
		}
	})
	if !reflect.DeepEqual(calendar, []string{"Second", "Project"}) {
		t.Fatal("calendar legend not reordered", calendar)
	}
}

// The Sprint Calendar's legend drags on its own page and reorders both menus.
func TestDraggingCalendarChipReordersBothMenus(t *testing.T) {
	ui, w := twoProjectFixture(t)
	k := ui.Kanban
	w.SetContent(k.CalendarPage)
	w.Resize(fyne.NewSize(1400, 950))
	kanban.Draw(k)
	w.Content().Refresh()
	chips := legendChips(k.CalendarBody)
	if len(chips) != 2 {
		t.Fatal("legend chips", len(chips))
	}
	dragChipOnto(chips[1], chips[0])
	if len(k.CalendarGhost.Objects) == 0 || len(k.GhostLayer.Objects) != 0 || !k.DropLine.Visible() {
		t.Fatal("drag not shown on the calendar page")
	}
	chips[1].DragEnd()
	if !reflect.DeepEqual(k.Plan.ProjectOrder, []string{"q", "p"}) || len(k.CalendarGhost.Objects) != 0 {
		t.Fatal("order not set", k.Plan.ProjectOrder)
	}
	if got := trayTitles(k); len(got) < 2 || got[1] != "B1" {
		t.Fatal("Sprint Issues not reordered", got)
	}
	if got := legendChips(k.Body); got[0].Board.ID != "q" {
		t.Fatal("Kanban legend not reordered")
	}
}

// Dropped off the legend, or back into its own place: nothing changes.
func TestLegendDragCancels(t *testing.T) {
	ui, _ := twoProjectFixture(t)
	k := ui.Kanban
	chips := legendChips(k.Body)
	driver := fyne.CurrentApp().Driver()
	first := driver.AbsolutePositionForObject(chips[0])
	off := first.Add(fyne.NewPos(5, 300))
	chips[1].Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: off}})
	if k.DropLine.Visible() {
		t.Fatal("line shown off the legend")
	}
	chips[1].DragEnd()
	second := driver.AbsolutePositionForObject(chips[1]).Add(fyne.NewPos(5, 5))
	chips[1].Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: second}})
	if k.DropLine.Visible() {
		t.Fatal("line shown for a drop that changes nothing")
	}
	chips[1].DragEnd()
	if k.Plan.ProjectOrder != nil {
		t.Fatal("order changed", k.Plan.ProjectOrder)
	}
}

// Tapping a chip hides the project until the app closes; every project starts
// shown again, while the order stays.
func TestLegendChipFiltersButStartsShown(t *testing.T) {
	ui, _ := twoProjectFixture(t)
	k := ui.Kanban
	k.Save(func() backend.SprintPlan { p := k.Plan; p.ProjectOrder = []string{"q", "p"}; return p }())
	kanban.Draw(k)
	legendChips(k.Body)[0].Chip.Tapped(nil)
	if !k.ProjectOff["q"] {
		t.Fatal("tap did not hide the project")
	}
	kanban.BuildKanbanTab(ui)
	if len(ui.Kanban.ProjectOff) != 0 {
		t.Fatal("a hidden project came back hidden", ui.Kanban.ProjectOff)
	}
	if !reflect.DeepEqual(ui.Kanban.Plan.ProjectOrder, []string{"q", "p"}) {
		t.Fatal("the order did not come back", ui.Kanban.Plan.ProjectOrder)
	}
}
