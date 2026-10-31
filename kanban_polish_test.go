package main

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func TestSprintLegendContainsCountAndEstimateWithoutDuplicateSummary(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.kanban
	found := false
	walkCanvas(k.body, func(o fyne.CanvasObject) {
		if text, ok := o.(*canvas.Text); ok && strings.Contains(text.Text, "1 issue | 1h 30m estimated") {
			found = true
		}
		if label, ok := o.(*widget.Label); ok && label.Text == "Shown" {
			t.Fatal("duplicate summary row remains")
		}
	})
	if !found {
		t.Fatal("legend does not show count and estimate")
	}
	k.setMessage("")
	if k.message.Visible() {
		t.Fatal("empty refresh message reserves space")
	}
}

func TestSprintAllColumnChecksAreGoldStableAndAnimated(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.kanban
	b := k.plan.Boards[0]
	b.Fields = append(b.Fields, sprintField{Name: "Status", Options: []fieldOption{{Name: "Created"}, {Name: "In Progress"}, {Name: "Live"}}})
	k.plan.Boards[0] = b
	k.draw()
	group := k.statusChoices
	if len(group.Options) != 4 {
		t.Fatalf("expected all columns including No status: %v", group.Options)
	}
	var compact *compactCheckGroup
	var feedback *choiceFeedback
	walkCanvas(k.statusChoiceView, func(o fyne.CanvasObject) {
		if g, ok := o.(*compactCheckGroup); ok {
			compact = g
		}
		if f, ok := o.(*choiceFeedback); ok {
			feedback = f
		}
	})
	if compact == nil || feedback == nil {
		t.Fatal("native checkbox feedback missing")
	}
	for _, obj := range test.WidgetRenderer(compact).Objects() {
		if check, ok := obj.(*widget.Check); ok && check.Theme().Color(theme.ColorNamePrimary, theme.VariantDark) != logoGold {
			t.Fatal("checkbox is not gold")
		}
	}
	group.SetSelected([]string{"In Progress", "Live"})
	shown, _ := k.statusColumns()
	if len(shown) != 2 || shown[0] != "In Progress" || group != k.statusChoices || feedback.animation == nil {
		t.Fatal("column toggle lost state, widget identity or animation")
	}
	group.SetSelected(nil)
	shown, _ = k.statusColumns()
	if len(shown) != 0 {
		t.Fatal("primary columns cannot be hidden")
	}
}

func TestSprintGoldDropdownSelectionAndKeyboard(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.kanban
	b := k.plan.Boards[0]
	future := sprintIteration{ID: "future", Title: "Sprint next", StartDate: addDays(c.Sprint.StartDate, 14), Duration: 14}
	b.Fields[0].Configuration.Iterations = append(b.Fields[0].Configuration.Iterations, future)
	b.Items = append(b.Items, sprintItem{ID: "future", Title: "Next", SprintID: future.ID})
	k.plan.Boards[0] = b
	k.draw()
	w.Content().Refresh()
	var selector, mode *goldSelect
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if s, ok := o.(*goldSelect); ok {
			if len(s.Options) > 0 && s.Options[0] == "Week" {
				mode = s
			} else {
				selector = s
			}
		}
	})
	if selector == nil || mode == nil {
		t.Fatal("calendar dropdowns missing")
	}
	selector.SetSelected(calendarSprintLabel(future))
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if s, ok := o.(*goldSelect); ok && len(s.Options) > 0 && strings.Contains(s.Options[0], " | ") {
			selector = s
		}
	})
	selector.Tapped(nil)
	if selector.popup == nil || !selector.popup.Visible() || selector.flash.animation == nil {
		t.Fatal("dropdown did not open with animation")
	}
	goldFill, goldCurrent := false, false
	walkCanvas(selector.rows, func(o fyne.CanvasObject) {
		if rect, ok := o.(*canvas.Rectangle); ok && rect.FillColor == logoGold {
			goldFill = true
		}
		if label, ok := o.(*widget.Label); ok && strings.HasSuffix(label.Text, "- Current") {
			goldCurrent = label.Theme().Color(theme.ColorNameForeground, theme.VariantDark) == logoGold
		}
	})
	if !goldFill || !goldCurrent {
		t.Fatal("selected row fill or current sprint text is not gold")
	}
	selector.TypedKey(&fyne.KeyEvent{Name: fyne.KeyUp})
	selector.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if k.calendarSprint != sprintKey(c.Sprint) {
		t.Fatal("keyboard selection did not select current sprint")
	}
}

func TestSprintNoSprintUsesGoldTextAndSelectedFill(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.kanban
	k.plan.Boards[0].Items = append(k.plan.Boards[0].Items, sprintItem{ID: "none", Title: "No sprint issue"})
	k.draw()
	found := false
	walkCanvas(k.body, func(o fyne.CanvasObject) {
		if label, ok := o.(*widget.Label); ok && label.Text == "No Sprint" {
			found = label.Theme().Color(theme.ColorNameForeground, theme.VariantDark) == logoGold
		}
	})
	if !found {
		t.Fatal("No Sprint text should be gold")
	}
	k.filter = "none"
	k.draw()
	for _, target := range k.targets {
		if target.kind == "sprint" && target.value == "" {
			if target.frame.FillColor != logoGold {
				t.Fatal("selected No Sprint lacks gold fill")
			}
			return
		}
	}
	t.Fatal("No Sprint row missing")
}
