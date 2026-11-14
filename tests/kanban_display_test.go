package tests

import (
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestSprintColumnsDefaultOrderAndPersistedControls(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.Kanban
	options := []backend.FieldOption{{Name: "Live"}, {Name: "Done"}, {Name: "Analysis - Investigation"}, {Name: "Created"}, {Name: "In Progress"}, {Name: "Pending Customer Feedback"}, {Name: "Pending Test"}, {Name: "Server Update Required"}, {Name: "In Review"}, {Name: "Terminated"}}
	k.Plan.Boards[0].Fields = append(k.Plan.Boards[0].Fields, backend.SprintField{Name: "Status", Options: options})
	shown, extra := kanban.StatusColumns(k)
	if !reflect.DeepEqual(shown, kanban.PrimaryKanbanStatuses) {
		t.Fatalf("wrong default order: %v", shown)
	}
	if len(extra) != 3 {
		t.Fatalf("extra statuses missing: %v", extra)
	}
	kanban.Draw(k)
	var controls *widget.CheckGroup
	walkCanvas(k.Body, func(o fyne.CanvasObject) {
		if c, ok := o.(*widget.CheckGroup); ok {
			controls = c
		}
	})
	if controls == nil {
		t.Fatal("bottom column controls missing")
	}
	controls.SetSelected([]string{"Live"})
	shown, _ = kanban.StatusColumns(k)
	if shown[len(shown)-1] != "Live" {
		t.Fatal("temporary column not shown")
	}
	kanban.BuildKanbanTab(ui)
	if !ui.Kanban.ExtraStatuses["Live"] || len(ui.Kanban.ExtraStatuses) != 1 {
		t.Fatal("column choices were not restored")
	}
	if kanban.KanbanStatusColor("In Progress") == kanban.KanbanStatusColor("Analysis - Investigation") || kanban.KanbanStatusColor("In Review") == kanban.KanbanStatusColor("Pending Customer Feedback") {
		t.Fatal("status colors not distinct")
	}
}

func TestSprintDropdownUsesDatesWithoutCurrentSuffix(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	label := sprintcalendar.CalendarSprintLabel(c.Sprint)
	// The running sprint is marked in gold, not by a suffix that overflows.
	if !strings.Contains(label, " | ") || strings.Contains(label, "Current") {
		t.Fatalf("current sprint label: %s", label)
	}
	var selector *widget.Select
	var mode *widget.Select
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if s, ok := o.(*widget.Select); ok {
			if len(s.Options) > 0 && strings.Contains(s.Options[0], " | ") {
				selector = s
			}
			if reflect.DeepEqual(s.Options, []string{"Week", "Sprint", "Month"}) {
				mode = s
			}
		}
	})
	if selector == nil || selector.Selected != label || mode == nil {
		t.Fatal("sprint label or view order incorrect")
	}
}

func TestSprintBubblesHaveDirectIssueHyperlink(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.Kanban
	b := k.Plan.Boards[0]
	it := b.Items[0]
	it.URL = "https://github.com/example/repo/issues/12"
	card := kanban.Card(k, b, it).(*shell.SprintDragCard)
	found := false
	walkCanvas(card.Content, func(o fyne.CanvasObject) {
		if link, ok := o.(*shell.HoverLink); ok && link.URL.String() == it.URL {
			found = true
		}
	})
	if !found {
		t.Fatal("issue link missing from bubble")
	}
}

func TestSprintCalendarRowsResizeAndEveryDayHasPercentage(t *testing.T) {
	short := canvas.NewRectangle(nil)
	short.SetMinSize(fyne.NewSize(100, 100))
	busy := canvas.NewRectangle(nil)
	busy.SetMinSize(fyne.NewSize(100, 300))
	rows := container.New(sprintcalendar.PlannerRowsLayout{}, short, busy)
	rows.Resize(fyne.NewSize(700, 800))
	if busy.Size().Height <= 300 || short.Size().Height <= 100 {
		t.Fatal("rows did not use spare viewport height")
	}
	busy.SetMinSize(fyne.NewSize(100, 900))
	rows.Refresh()
	if rows.MinSize().Height <= 900 {
		t.Fatal("busy dates were clipped")
	}
	if sprintcalendar.PlannerDayPercentage(120) != "120 min · 25%" || !strings.Contains(sprintcalendar.PlannerDayPercentage(0), "0%") {
		t.Fatal("daily percentages incorrect")
	}
	ui, _, _ := plannerFixture(t)
	found := false
	walkCanvas(ui.Kanban.CalendarBody, func(o fyne.CanvasObject) {
		if text, ok := o.(*canvas.Text); ok && text.Text == sprintcalendar.PlannerDayPercentage(0) {
			found = true
		}
	})
	if !found {
		t.Fatal("empty dates lack a percentage")
	}
}
