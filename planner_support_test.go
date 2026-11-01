package main

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
)

func TestSprintClearAndLogWorkDeleteUseRedActionsAndGoldCancel(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	fyne.CurrentApp().Settings().SetTheme(goldTheme{Theme: theme.DefaultTheme()})
	k := ui.kanban
	applied := false
	k.confirmClear("Clear local schedule", "Remove the selected sprint's local time blocks?", func() { applied = true })
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
		if confirm.Theme().Color(theme.ColorNamePrimary, theme.VariantDark) != theme.Current().Color(theme.ColorNameError, theme.VariantDark) || cancel.Theme().Color(theme.ColorNamePrimary, theme.VariantDark) != logoGold {
			t.Fatal("removal action should be red and Cancel should be gold")
		}
		if confirm.Theme().Size(theme.SizeNameButtonRadius) != 100 || cancel.Theme().Size(theme.SizeNameButtonRadius) != 8 || cancel.Theme().Color(theme.ColorNameForegroundOnPrimary, theme.VariantDark) == color.White {
			t.Fatal("removal should be pill-shaped; Cancel stays gold and rounded")
		}
	}
	assertStyle(confirm, cancel)
	if ui.removalPopupSize(widget.NewLabel("Confirm removal")).Width > 640 {
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
	ui.confirmDelete(Row{"id": "unused"}, func() { applied = true })
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
	k := ui.kanban
	ui.cfg.DisplayName = "Alex"
	ui.cfg.WorklogOwner = "team-alex"
	k.plan.Tasks[c.Key] = sprintTask{Estimate: 300, Blocks: []planBlock{{k.week, 540, 300}}}
	if err := ui.store.saveSprintPlan(k.plan); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(ui.store.dir, "sprint-planner.json")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	k.draw()
	var support *widget.Check
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if check, ok := o.(*widget.Check); ok && strings.Contains(check.Text, "+180 min weekday support") {
			support = check
		}
	})
	if support == nil || support.Checked {
		t.Fatal("display-only support checkbox missing or unexpectedly enabled")
	}
	if support.Theme().Color(theme.ColorNamePrimary, theme.VariantDark) != logoGold {
		t.Fatal("support checkbox should use gold styling")
	}
	support.SetChecked(true)
	for _, date := range k.dates {
		if date.date == k.week && date.base != dayFill(0, 480) {
			t.Fatal("300 planned + 180 support should show a full green day")
		}
		if date.date == addDays(k.week, 5) && countPlannerBubbles(date.object) != 0 {
			t.Fatal("support created a weekend bubble")
		}
	}
	bar := ui.meters["planner/"+dayNumber(k.week)]
	found := false
	for _, part := range bar.target {
		if part.key == "@draft" && part.weight == float32(180)/480 {
			found = true
		}
	}
	if !found {
		t.Fatal("support is missing from the projected progress bar")
	}
	weekend := ui.meters["planner/"+dayNumber(addDays(k.week, 5))]
	if len(weekend.target) != 0 {
		t.Fatal("weekend received support minutes")
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(before, after) || k.plan.Tasks[c.Key].Blocks[0].Minutes != 300 {
		t.Fatal("display-only support changed the saved schedule", err)
	}
	if dir := os.Getenv("WORKLOG_PLANNER_SCREENSHOTS"); dir != "" {
		captureSprintPNG(t, w, filepath.Join(dir, "planner-support.png"))
	}
	real := sprintItem{ID: "support", Title: "Alex: Weekly Support (12PM - 3PM)", SprintID: c.Sprint.ID, Effort: 180}
	b := &k.plan.Boards[0]
	b.Items = append(b.Items, real)
	k.plan.Tasks[real.key(b.ID)] = sprintTask{Blocks: []planBlock{{k.week, 840, 180}}}
	k.draw()
	foundProjected := false
	for _, date := range k.dates {
		if date.date == k.week {
			walkCanvas(date.object, func(o fyne.CanvasObject) {
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
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Today" {
			todayButton = b
		}
	})
	if !ui.supportOn(k.week) || ui.supportOn(addDays(k.week, 7)) {
		t.Fatal("ticking the calendar should save support for the shown week only")
	}
	test.Tap(todayButton)
	if !ui.supportOn(k.week) {
		t.Fatal("support is saved per week and survives navigation")
	}
}

func dayNumber(date string) string {
	var year, month, day int
	fmt.Sscanf(date, "%d-%d-%d", &year, &month, &day)
	return fmt.Sprint(day)
}
