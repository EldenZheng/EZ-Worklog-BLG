package main

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func TestSprintOutlinesAndColumnFooterPosition(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.kanban
	b := k.plan.Boards[0]
	b.Fields = append(b.Fields, sprintField{Name: "Status", Options: []fieldOption{{Name: "In Progress"}, {Name: "Live"}}})
	b.Items[0].Status = "In Progress"
	k.plan.Boards[0] = b
	w.SetContent(k.root)
	k.draw()
	w.Content().Refresh()
	for _, target := range k.targets {
		if target.kind == "status" && target.value == "In Progress" {
			if target.frame.StrokeColor != kanbanColumnBorder("In Progress") || target.frame.StrokeWidth <= 0 || target.frame.FillColor != kanbanColumnFill("In Progress") {
				t.Fatal("status hue missing from soft column background and outline")
			}
		}
		if target.kind == "sprint" && target.value == sprintKey(c.Sprint) {
			if target.frame.StrokeColor != logoGold || target.frame.FillColor != logoGold {
				t.Fatal("selected sprint should have a gold fill")
			}
		}
	}
	var controls fyne.CanvasObject
	walkCanvas(k.body, func(o fyne.CanvasObject) {
		if group, ok := o.(*compactCheckGroup); ok {
			controls = group
		}
	})
	if controls == nil {
		t.Fatal("column controls missing")
	}
	if fyne.CurrentApp().Driver().AbsolutePositionForObject(controls).X < k.sideClip.Position().X+k.sideClip.Size().Width {
		t.Fatal("column controls overlap sprint sidebar")
	}
}

func TestSprintTodayAndCurrentSelectAreGold(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	var todayTarget *plannerDateTarget
	for _, target := range k.dates {
		if target.date == today() {
			todayTarget = target
		}
	}
	if todayTarget == nil || todayTarget.frame.StrokeColor != logoGold {
		t.Fatal("today does not have a gold outline")
	}
	found := false
	selectGold := false
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if label, ok := o.(*widget.Label); ok && strings.HasSuffix(label.Text, "- Today") {
			found = true
			if label.Theme().Color(theme.ColorNameForeground, theme.VariantDark) != logoGold {
				t.Fatal("today text not gold")
			}
		}
		if selectBox, ok := o.(*widget.Select); ok && strings.HasSuffix(selectBox.Selected, "- Current") {
			selectGold = selectBox.Theme().Color(theme.ColorNameForeground, theme.VariantDark) == logoGold
		}
	})
	if !found || !selectGold {
		t.Fatal("today suffix or gold current sprint selector missing")
	}
	card := k.plannerCard(c, -1).(*plannerDragCard)
	card.MouseIn(nil)
	card.MouseOut()
	if todayTarget.frame.StrokeColor != logoGold || todayTarget.frame.StrokeWidth != 2 {
		t.Fatal("hover lost today's outline")
	}
}

func TestSprintCompactPopupHasNoOuterScrollAndSaves(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.kanban
	k.editor(k.plan.Boards[0], c.Item)
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
	p, err := ui.store.loadSprintPlan()
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
