package tests

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestSprintOutlinesAndColumnFooterPosition(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	b := k.Plan.Boards[0]
	b.Fields = append(b.Fields, backend.SprintField{Name: "Status", Options: []backend.FieldOption{{Name: "In Progress"}, {Name: "Live"}}})
	b.Items[0].Status = "In Progress"
	k.Plan.Boards[0] = b
	w.SetContent(k.Root)
	kanban.Draw(k)
	w.Content().Refresh()
	for _, target := range k.Targets {
		if target.Kind == "status" && target.Value == "In Progress" {
			if target.Frame.StrokeColor != kanban.KanbanColumnBorder("In Progress") || target.Frame.StrokeWidth <= 0 || target.Frame.FillColor != kanban.KanbanColumnFill("In Progress") {
				t.Fatal("status hue missing from soft column background and outline")
			}
		}
		if target.Kind == "sprint" && target.Value == backend.SprintKey(c.Sprint) {
			if target.Frame.StrokeColor != widgets.LogoGold || target.Frame.FillColor != widgets.LogoGold {
				t.Fatal("selected sprint should have a gold fill")
			}
		}
	}
	var controls fyne.CanvasObject
	walkCanvas(k.Body, func(o fyne.CanvasObject) {
		if group, ok := o.(*widgets.CompactCheckGroup); ok {
			controls = group
		}
	})
	if controls == nil {
		t.Fatal("column controls missing")
	}
	if fyne.CurrentApp().Driver().AbsolutePositionForObject(controls).X < k.SideClip.Position().X+k.SideClip.Size().Width {
		t.Fatal("column controls overlap sprint sidebar")
	}
}

func TestSprintTodayAndCurrentSelectAreGold(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	var todayTarget *shell.PlannerDateTarget
	for _, target := range k.Dates {
		if target.Date == shell.Today() {
			todayTarget = target
		}
	}
	if todayTarget == nil || todayTarget.Frame.StrokeColor != widgets.LogoGold {
		t.Fatal("today does not have a gold outline")
	}
	found := false
	selectGold := false
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if label, ok := o.(*widget.Label); ok && strings.HasSuffix(label.Text, "- Today") {
			found = true
			if label.Theme().Color(theme.ColorNameForeground, theme.VariantDark) != widgets.LogoGold {
				t.Fatal("today text not gold")
			}
		}
		if selectBox, ok := o.(*widget.Select); ok && strings.HasSuffix(selectBox.Selected, "- Current") {
			selectGold = selectBox.Theme().Color(theme.ColorNameForeground, theme.VariantDark) == widgets.LogoGold
		}
	})
	if !found || !selectGold {
		t.Fatal("today suffix or gold current sprint selector missing")
	}
	card := sprintcalendar.PlannerCard(k, c, -1).(*shell.PlannerDragCard)
	card.MouseIn(nil)
	card.MouseOut()
	if todayTarget.Frame.StrokeColor != widgets.LogoGold || todayTarget.Frame.StrokeWidth != 2 {
		t.Fatal("hover lost today's outline")
	}
}

func TestSprintCompactPopupHasNoOuterScrollAndSaves(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	kanban.Editor(k, k.Plan.Boards[0], c.Item)
	overlay := w.Canvas().Overlays().Top()
	if overlay == nil {
		t.Fatal("popup missing")
	}
	var save *widget.Button
	var effort *widget.Entry
	walkPopup(overlay, func(o fyne.CanvasObject) {
		if _, ok := o.(*container.Scroll); ok {
			t.Fatal("issue popup still scrolls as a page")
		}
		if b, ok := o.(*widget.Button); ok && b.Text == "Save local plan" {
			save = b
		}
		if entry, ok := o.(*widget.Entry); ok && entry.Text == "90" && effort == nil {
			effort = entry
		}
	})
	if save == nil || effort == nil {
		t.Fatalf("compact popup controls missing: overlay=%T save=%v effort=%v", overlay, save != nil, effort != nil)
	}
	effort.SetText("120")
	save.OnTapped()
	p, err := ui.Store.LoadSprintPlan()
	if err != nil || p.Tasks[c.Key].Estimate != 120 {
		t.Fatal("compact popup did not save local estimate")
	}
	if p.Boards[0].Items[0].Effort != 90 {
		t.Fatal("popup changed GitHub estimate")
	}
}

func walkPopup(overlay fyne.CanvasObject, visit func(fyne.CanvasObject)) {
	if w, ok := overlay.(fyne.Widget); ok {
		for _, child := range test.WidgetRenderer(w).Objects() {
			walkCanvas(child, visit)
		}
	} else {
		walkCanvas(overlay, visit)
	}
}
