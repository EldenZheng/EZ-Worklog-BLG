package tests

import (
	"strings"
	"testing"
	"worklog/internal/menus/kanban"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func TestSprintLegendContainsCountAndEstimateWithoutDuplicateSummary(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.Kanban
	found := false
	walkCanvas(k.Body, func(o fyne.CanvasObject) {
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
	k.SetMessage("")
	if k.Message.Visible() {
		t.Fatal("empty refresh message reserves space")
	}
}

func TestSprintAllColumnChecksAreGoldStableAndAnimated(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.Kanban
	b := k.Plan.Boards[0]
	b.Fields = append(b.Fields, backend.SprintField{Name: "Status", Options: []backend.FieldOption{{Name: "Created"}, {Name: "In Progress"}, {Name: "Live"}}})
	k.Plan.Boards[0] = b
	kanban.Draw(k)
	group := k.StatusChoices
	if len(group.Options) != 4 {
		t.Fatalf("expected all columns including No status: %v", group.Options)
	}
	var compact *widgets.CompactCheckGroup
	var feedback *widgets.ChoiceFeedback
	walkCanvas(k.StatusChoiceView, func(o fyne.CanvasObject) {
		if g, ok := o.(*widgets.CompactCheckGroup); ok {
			compact = g
		}
		if f, ok := o.(*widgets.ChoiceFeedback); ok {
			feedback = f
		}
	})
	if compact == nil || feedback == nil {
		t.Fatal("native checkbox feedback missing")
	}
	for _, obj := range test.WidgetRenderer(compact).Objects() {
		if check, ok := obj.(*widget.Check); ok && check.Theme().Color(theme.ColorNamePrimary, theme.VariantDark) != widgets.LogoGold {
			t.Fatal("checkbox is not gold")
		}
	}
	group.SetSelected([]string{"In Progress", "Live"})
	shown, _ := kanban.StatusColumns(k)
	if len(shown) != 2 || shown[0] != "In Progress" || group != k.StatusChoices || feedback.Animation == nil {
		t.Fatal("column toggle lost state, widget identity or animation")
	}
	group.SetSelected(nil)
	shown, _ = kanban.StatusColumns(k)
	if len(shown) != 0 {
		t.Fatal("primary columns cannot be hidden")
	}
}

func TestSprintGoldDropdownSelectionAndKeyboard(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	b := k.Plan.Boards[0]
	future := backend.SprintIteration{ID: "future", Title: "Sprint next", StartDate: backend.AddDays(c.Sprint.StartDate, 14), Duration: 14}
	b.Fields[0].Configuration.Iterations = append(b.Fields[0].Configuration.Iterations, future)
	b.Items = append(b.Items, backend.SprintItem{ID: "future", Title: "Next", SprintID: future.ID})
	k.Plan.Boards[0] = b
	kanban.Draw(k)
	w.Content().Refresh()
	var selector, mode *shell.GoldSelect
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if s, ok := o.(*shell.GoldSelect); ok {
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
	selector.SetSelected(sprintcalendar.CalendarSprintLabel(future))
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if s, ok := o.(*shell.GoldSelect); ok && len(s.Options) > 0 && strings.Contains(s.Options[0], " | ") {
			selector = s
		}
	})
	selector.Tapped(nil)
	if selector.Popup == nil || !selector.Popup.Visible() || selector.Flash.Animation == nil {
		t.Fatal("dropdown did not open with animation")
	}
	goldFill, goldCurrent := false, false
	walkCanvas(selector.Rows, func(o fyne.CanvasObject) {
		if rect, ok := o.(*canvas.Rectangle); ok && rect.FillColor == widgets.LogoGold {
			goldFill = true
		}
		if label, ok := o.(*widget.Label); ok && strings.HasSuffix(label.Text, "- Current") {
			goldCurrent = label.Theme().Color(theme.ColorNameForeground, theme.VariantDark) == widgets.LogoGold
		}
	})
	if !goldFill || !goldCurrent {
		t.Fatal("selected row fill or current sprint text is not gold")
	}
	selector.TypedKey(&fyne.KeyEvent{Name: fyne.KeyUp})
	selector.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if k.CalendarSprint != backend.SprintKey(c.Sprint) {
		t.Fatal("keyboard selection did not select current sprint")
	}
}

func TestSprintNoSprintUsesGoldTextAndSelectedFill(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.Kanban
	k.Plan.Boards[0].Items = append(k.Plan.Boards[0].Items, backend.SprintItem{ID: "none", Title: "No sprint issue"})
	kanban.Draw(k)
	found := false
	walkCanvas(k.Body, func(o fyne.CanvasObject) {
		if label, ok := o.(*widget.Label); ok && label.Text == "No Sprint" {
			found = label.Theme().Color(theme.ColorNameForeground, theme.VariantDark) == widgets.LogoGold
		}
	})
	if !found {
		t.Fatal("No Sprint text should be gold")
	}
	k.Filter = "none"
	kanban.Draw(k)
	for _, target := range k.Targets {
		if target.Kind == "sprint" && target.Value == "" {
			if target.Frame.FillColor != widgets.LogoGold {
				t.Fatal("selected No Sprint lacks gold fill")
			}
			return
		}
	}
	t.Fatal("No Sprint row missing")
}
