package tests

import (
	"bytes"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/logwork"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestSprintClearAndLogWorkDeleteUseRedActionsAndGoldCancel(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	fyne.CurrentApp().Settings().SetTheme(shell.GoldTheme{Theme: theme.DefaultTheme()})
	k := ui.Kanban
	applied := false
	sprintcalendar.ConfirmClear(k, "Clear local schedule", "Remove the selected sprint's local time blocks?", func() { applied = true })
	var confirm, cancel *widget.Button
	walkPopup(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok {
			if b.Text == "Clear" {
				confirm = b
			}
			if b.Text == "Cancel" {
				cancel = b
			}
		}
	})
	if confirm == nil || cancel == nil {
		t.Fatal("popup controls missing")
	}
	assertStyle := func(confirm, cancel *widget.Button) {
		t.Helper()
		if confirm.Theme().Color(theme.ColorNamePrimary, theme.VariantDark) != theme.Current().Color(theme.ColorNameError, theme.VariantDark) || cancel.Theme().Color(theme.ColorNamePrimary, theme.VariantDark) != widgets.LogoGold {
			t.Fatal("removal action should be red and Cancel should be gold")
		}
		if confirm.Theme().Size(theme.SizeNameButtonRadius) != 100 || cancel.Theme().Size(theme.SizeNameButtonRadius) != 8 || cancel.Theme().Color(theme.ColorNameForegroundOnPrimary, theme.VariantDark) == color.White {
			t.Fatal("removal should be pill-shaped; Cancel stays gold and rounded")
		}
	}
	assertStyle(confirm, cancel)
	if ui.RemovalPopupSize(widget.NewLabel("Confirm removal")).Width > 640 {
		t.Fatal("confirmation is too wide")
	}
	if dir := os.Getenv("WORKLOG_PLANNER_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		captureSprintPNG(t, w, filepath.Join(dir, "planner-clear-popup.png"))
	}
	test.Tap(cancel)
	if applied {
		t.Fatal("cancel applied the clear")
	}
	logwork.ConfirmDelete(ui, backend.Row{"id": "unused"}, func() { applied = true })
	confirm, cancel = nil, nil
	walkPopup(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok {
			if b.Text == "Delete" {
				confirm = b
			}
			if b.Text == "Cancel" {
				cancel = b
			}
		}
	})
	if confirm == nil || cancel == nil {
		t.Fatal("Log Work deletion controls missing")
	}
	assertStyle(confirm, cancel)
	if dir := os.Getenv("WORKLOG_PLANNER_SCREENSHOTS"); dir != "" {
		captureSprintPNG(t, w, filepath.Join(dir, "log-work-delete-popup.png"))
	}
	test.Tap(cancel)
	if applied {
		t.Fatal("cancel deleted a Log Work entry")
	}
}

func TestSprintWeekdaySupportIsDisplayOnlyAndSkipsRealSupport(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	ui.Cfg.DisplayName = "Alex"
	ui.Cfg.WorklogOwner = "team-alex"
	k.Plan.Tasks[c.Key] = backend.SprintTask{Estimate: 300, Blocks: []backend.PlanBlock{{k.Week, 540, 300}}}
	if err := ui.Store.SaveSprintPlan(k.Plan); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(ui.Store.Dir, "sprint-planner.json")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	kanban.Draw(k)
	var support *widget.Check
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if check, ok := o.(*widget.Check); ok && strings.Contains(check.Text, "+180 min weekday support") {
			support = check
		}
	})
	if support == nil || support.Checked {
		t.Fatal("display-only support checkbox missing or unexpectedly enabled")
	}
	if support.Theme().Color(theme.ColorNamePrimary, theme.VariantDark) != widgets.LogoGold {
		t.Fatal("support checkbox should use gold styling")
	}
	support.SetChecked(true)
	for _, date := range k.Dates {
		if date.Date == k.Week && date.Base != shell.DayFill(0, 480) {
			t.Fatal("300 planned + 180 support should show a full green day")
		}
		if date.Date == backend.AddDays(k.Week, 5) && countPlannerBubbles(date.Object) != 0 {
			t.Fatal("support created a weekend bubble")
		}
	}
	bar := ui.Meters["planner/"+dayNumber(k.Week)]
	found := false
	for _, part := range bar.Target {
		if part.Key == "@draft" && part.Weight == float32(180)/480 {
			found = true
		}
	}
	if !found {
		t.Fatal("support is missing from the projected progress bar")
	}
	weekend := ui.Meters["planner/"+dayNumber(backend.AddDays(k.Week, 5))]
	if len(weekend.Target) != 0 {
		t.Fatal("weekend received support minutes")
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(before, after) || k.Plan.Tasks[c.Key].Blocks[0].Minutes != 300 {
		t.Fatal("display-only support changed the saved schedule", err)
	}
	if dir := os.Getenv("WORKLOG_PLANNER_SCREENSHOTS"); dir != "" {
		captureSprintPNG(t, w, filepath.Join(dir, "planner-support.png"))
	}
	real := backend.SprintItem{ID: "support", Title: "Alex: Weekly Support (12PM - 3PM)", SprintID: c.Sprint.ID, Effort: 180}
	b := &k.Plan.Boards[0]
	b.Items = append(b.Items, real)
	k.Plan.Tasks[real.Key(b.ID)] = backend.SprintTask{Blocks: []backend.PlanBlock{{k.Week, 840, 180}}}
	kanban.Draw(k)
	foundProjected := false
	for _, date := range k.Dates {
		if date.Date == k.Week {
			walkCanvas(date.Object, func(o fyne.CanvasObject) {
				if text, ok := o.(*canvas.Text); ok && text.Text == "480m planned" {
					foundProjected = true
				}
			})
		}
	}
	if !foundProjected {
		t.Fatal("real weekly support was counted twice")
	}
	var todayButton *widget.Button
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Today" {
			todayButton = b
		}
	})
	if !sprintcalendar.SupportOn(ui, k.Week) || sprintcalendar.SupportOn(ui, backend.AddDays(k.Week, 7)) {
		t.Fatal("ticking the calendar should save support for the shown week only")
	}
	test.Tap(todayButton)
	if !sprintcalendar.SupportOn(ui, k.Week) {
		t.Fatal("support is saved per week and survives navigation")
	}
}

func dayNumber(date string) string {
	var year, month, day int
	fmt.Sscanf(date, "%d-%d-%d", &year, &month, &day)
	return fmt.Sprint(day)
}
