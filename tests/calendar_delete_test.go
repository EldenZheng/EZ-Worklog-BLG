package tests

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/logwork"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func menuReveals(root fyne.CanvasObject) int {
	n := 0
	walkCanvas(root, func(o fyne.CanvasObject) {
		if _, ok := o.(*shell.Reveal); ok {
			n++
		}
	})
	return n
}

func TestDeletingSavedEntryReplaysTheMenuAfterConfirmation(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	rows, err := ui.Store.AppendRows([]backend.Row{
		{"date": "2026-08-11", "minutes": "60", "issue": "exampleorg/repo#1", "description": "Delete this"},
		{"date": "2026-08-12", "minutes": "30", "issue": "exampleorg/repo#2", "description": "Keep on week"},
		{"date": "2026-08-03", "minutes": "30", "issue": "exampleorg/repo#3", "description": "Keep in list"},
	})
	if err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)
	logwork.DrawRecent(ui)
	if menuReveals(ui.WeekBox) != 0 || menuReveals(ui.RecentBox) != 0 {
		t.Fatal("an ordinary redraw should leave the menu's entrance alone")
	}
	refreshes := 0
	refresh := func() { refreshes++; logwork.DrawRecent(ui) }
	choose := func(label string) {
		t.Helper()
		var button *widget.Button
		walkPopup(ui.Win.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
			if b, ok := o.(*widget.Button); ok && b.Text == label {
				button = b
			}
		})
		if button == nil {
			t.Fatalf("missing %s confirmation button", label)
		}
		test.Tap(button)
	}
	logwork.ConfirmDelete(ui, rows[0], refresh)
	choose("Cancel")
	logwork.DrawRecent(ui)
	if refreshes != 0 || menuReveals(ui.WeekBox) != 0 || menuReveals(ui.RecentBox) != 0 {
		t.Fatal("cancelling must not replay the menu or refresh deleted work")
	}
	logwork.ConfirmDelete(ui, rows[0], refresh)
	choose("Delete")
	remaining, err := ui.Store.ReadRows()
	if err != nil || len(remaining) != 2 || refreshes != 1 {
		t.Fatalf("the confirmed entry alone should be deleted: rows=%d refreshes=%d err=%v", len(remaining), refreshes, err)
	}
	if menuReveals(ui.WeekBox) == 0 || menuReveals(ui.RecentBox) == 0 {
		t.Fatal("deletion should replay both the week and the remaining saved bubbles")
	}
	logwork.DrawRecent(ui)
	if menuReveals(ui.WeekBox) != 0 || menuReveals(ui.RecentBox) != 0 {
		t.Fatal("a later redraw must not keep replaying the entrance")
	}
}

// A block on the calendar offers its own Delete while the pointer is on it, as
// the day does, and asks before removing only that block.
func TestSprintBlockDeleteShowsOnHoverAndRemovesOnlyThatBlock(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	day := k.Week
	k.Plan.Tasks[c.Key] = backend.SprintTask{Notes: "keep", Blocks: []backend.PlanBlock{
		{Date: day, Start: 540, Minutes: 60}, {Date: backend.AddDays(day, 1), Start: 600, Minutes: 30}}}
	kanban.Draw(k)
	kanban.Draw(k) // finish the original entrance before testing deletion
	w.Content().Refresh()

	var card *shell.PlannerDragCard
	walkCanvas(w.Content(), func(o fyne.CanvasObject) {
		if c, ok := o.(*shell.PlannerDragCard); ok && c.Index == 0 && card == nil {
			card = c
		}
	})
	if card == nil {
		t.Fatal("the scheduled block is missing")
	}
	var del *widget.Button
	var at fyne.CanvasObject // the Delete as the canvas holds it
	walkCanvas(card, func(o fyne.CanvasObject) {
		if d, ok := o.(*sprintcalendar.PlannerBlockDelete); ok {
			del, at = &d.Button, d
		}
	})
	if del == nil || del.Text != "Delete" || del.Visible() {
		t.Fatal("a block's Delete should be there and start hidden")
	}
	if del.Icon != theme.DeleteIcon() {
		t.Fatal("a block's Delete should carry the delete icon")
	}
	test.MoveMouse(w.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(card).Add(fyne.NewPos(10, 30)))
	if !del.Visible() {
		t.Fatal("hovering the block did not show its Delete")
	}
	// Moving onto the button keeps it, as the GH link keeps the block lit.
	test.MoveMouse(w.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(at).Add(fyne.NewPos(6, 6)))
	if !del.Visible() {
		t.Fatal("Delete vanished while moving onto it")
	}

	test.Tap(del)
	if len(k.Plan.Tasks[c.Key].Blocks) != 2 {
		t.Fatal("block Delete bypassed confirmation")
	}
	var confirm *widget.Button
	walkPopup(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Delete" {
			confirm = b
		}
	})
	if confirm == nil {
		t.Fatal("block Delete confirmation missing")
	}
	test.Tap(confirm)
	p, err := ui.Store.LoadSprintPlan()
	if err != nil || len(p.Tasks[c.Key].Blocks) != 1 || p.Tasks[c.Key].Blocks[0].Date != backend.AddDays(day, 1) || p.Tasks[c.Key].Notes != "keep" {
		t.Fatal("block Delete affected other blocks or the issue's notes", err, p.Tasks[c.Key])
	}
	if menuReveals(k.CalendarBody) != 7 {
		t.Fatal("deleting a block should replay the calendar's seven days")
	}
}

func TestClearingDayOrSprintReplaysCalendarEntrance(t *testing.T) {
	for _, action := range []string{"day", "sprint"} {
		t.Run(action, func(t *testing.T) {
			ui, w, c := plannerFixture(t)
			k := ui.Kanban
			k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: k.Week, Start: 540, Minutes: 60}}}
			kanban.Draw(k)
			kanban.Draw(k)
			if menuReveals(k.CalendarBody) != 0 {
				t.Fatal("the original calendar entrance should have finished")
			}
			confirmLabel := "Clear"
			if action == "day" {
				confirmLabel = "Delete"
				for _, d := range k.Dates {
					if d.Date == k.Week {
						d.Object.(*shell.PlannerDayHover).Clear.OnTapped()
						break
					}
				}
			} else {
				button := buttonNamed(k.CalendarBody, "Clear")
				if button == nil {
					t.Fatal("sprint Clear button missing")
				}
				test.Tap(button)
			}
			var confirm *widget.Button
			walkPopup(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
				if b, ok := o.(*widget.Button); ok && b.Text == confirmLabel {
					confirm = b
				}
			})
			if confirm == nil {
				t.Fatal("removal confirmation missing")
			}
			test.Tap(confirm)
			if len(k.Plan.Tasks[c.Key].Blocks) != 0 || menuReveals(k.CalendarBody) != 7 {
				t.Fatal("a confirmed clear should remove the blocks and replay the calendar entrance")
			}
		})
	}
}

func TestSprintDayDeleteSaysDelete(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: k.Week, Start: 540, Minutes: 60}}}
	kanban.Draw(k)
	for _, d := range k.Dates {
		if d.Date == k.Week {
			hover := d.Object.(*shell.PlannerDayHover)
			if hover.Clear.Text != "Delete" || hover.Clear.Icon != theme.DeleteIcon() || hover.ClearView == nil {
				t.Fatalf("the day's Delete should read Delete beside its icon: %q", hover.Clear.Text)
			}
			return
		}
	}
	t.Fatal("planned day missing")
}

// A block that moved while its confirmation was open is not removed in its place.
func TestRemovePlannerBlockRefusesAChangedBlock(t *testing.T) {
	shown := backend.PlanBlock{Date: "2026-11-09", Start: 540, Minutes: 60}
	p := backend.SprintPlan{Tasks: map[string]backend.SprintTask{"a": {Blocks: []backend.PlanBlock{
		{Date: "2026-11-09", Start: 600, Minutes: 60}, shown}}}}
	if _, err := backend.RemovePlannerBlock(p, "a", 0, shown); err == nil {
		t.Fatal("a different block at the index should be refused")
	}
	got, err := backend.RemovePlannerBlock(p, "a", 1, shown)
	if err != nil || len(got.Tasks["a"].Blocks) != 1 || got.Tasks["a"].Blocks[0].Start != 600 {
		t.Fatal("the shown block should go and the other stay", err, got.Tasks["a"])
	}
	if len(p.Tasks["a"].Blocks) != 2 {
		t.Fatal("the plan passed in must not change before it is saved")
	}
}

// Log Work names the day's planned time in purple beside the day, and counts it
// in the waiting minutes and the projected score like the Sprint Calendar.
func TestLogWorkDayShowsPlannedTime(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.Kanban
	ds := k.Week
	drafts := []backend.Row{{"id": "a", "date": ds, "minutes": "300"}}
	if got := labels(logwork.WeekColumn(ui, ds, nil, drafts)); contains(got, "planned") || !contains(got, "+300 min") {
		t.Fatalf("a day with nothing planned should not mention plans: %v", got)
	}
	k.Plan.Tasks[shell.PlannerMeetingKey] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: ds, Start: 540, Minutes: 180}}}
	col := logwork.WeekColumn(ui, ds, nil, drafts)
	got := labels(col)
	if !contains(got, "180m planned") || !contains(got, "+480 min") || !contains(got, "480 min · 100% ✓") {
		t.Fatalf("planned time should show and count toward the day: %v", got)
	}
	if col.Waiting != 300 || col.Planned != 180 {
		t.Fatalf("waiting must stay drafts only: waiting=%d planned=%d", col.Waiting, col.Planned)
	}
}
