package main

import (
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func TestSprintColumnsDefaultOrderAndPersistedControls(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.kanban
	options := []fieldOption{{Name: "Live"}, {Name: "Done"}, {Name: "Analysis - Investigation"}, {Name: "Created"}, {Name: "In Progress"}, {Name: "Pending Customer Feedback"}, {Name: "Pending Test"}, {Name: "Server Update Required"}, {Name: "In Review"}, {Name: "Terminated"}}
	k.plan.Boards[0].Fields = append(k.plan.Boards[0].Fields, sprintField{Name: "Status", Options: options})
	shown, extra := k.statusColumns()
	if !reflect.DeepEqual(shown, primaryKanbanStatuses) {
		t.Fatalf("wrong default order: %v", shown)
	}
	if len(extra) != 3 {
		t.Fatalf("extra statuses missing: %v", extra)
	}
	k.draw()
	var controls *widget.CheckGroup
	walkCanvas(k.body, func(o fyne.CanvasObject) {
		if c, ok := o.(*widget.CheckGroup); ok {
			controls = c
		}
	})
	if controls == nil {
		t.Fatal("bottom column controls missing")
	}
	controls.SetSelected([]string{"Live"})
	shown, _ = k.statusColumns()
	if shown[len(shown)-1] != "Live" {
		t.Fatal("temporary column not shown")
	}
	ui.buildKanbanTab()
	if !ui.kanban.extraStatuses["Live"] || len(ui.kanban.extraStatuses) != 1 {
		t.Fatal("column choices were not restored")
	}
	if kanbanStatusColor("In Progress") == kanbanStatusColor("Analysis - Investigation") || kanbanStatusColor("In Review") == kanbanStatusColor("Pending Customer Feedback") {
		t.Fatal("status colors not distinct")
	}
}

func TestSprintDropdownUsesDatesAndCurrentSuffix(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	label := calendarSprintLabel(c.Sprint)
	if !strings.Contains(label, " | ") || !strings.HasSuffix(label, " - Current") {
		t.Fatalf("current sprint label: %s", label)
	}
	var selector *widget.Select
	var mode *widget.Select
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
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
	k := ui.kanban
	b := k.plan.Boards[0]
	it := b.Items[0]
	it.URL = "https://github.com/example/repo/issues/12"
	card := k.card(b, it).(*sprintDragCard)
	found := false
	walkCanvas(card.content, func(o fyne.CanvasObject) {
		if link, ok := o.(*widget.Hyperlink); ok && link.URL.String() == it.URL {
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
	rows := container.New(plannerRowsLayout{}, short, busy)
	rows.Resize(fyne.NewSize(700, 800))
	if busy.Size().Height <= 300 || short.Size().Height <= 100 {
		t.Fatal("rows did not use spare viewport height")
	}
	busy.SetMinSize(fyne.NewSize(100, 900))
	rows.Refresh()
	if rows.MinSize().Height <= 900 {
		t.Fatal("busy dates were clipped")
	}
	if plannerDayPercentage(120) != "120 min · 25%" || !strings.Contains(plannerDayPercentage(0), "0%") {
		t.Fatal("daily percentages incorrect")
	}
	ui, _, _ := plannerFixture(t)
	found := false
	walkCanvas(ui.kanban.calendarBody, func(o fyne.CanvasObject) {
		if text, ok := o.(*canvas.Text); ok && text.Text == plannerDayPercentage(0) {
			found = true
		}
	})
	if !found {
		t.Fatal("empty dates lack a percentage")
	}
}
