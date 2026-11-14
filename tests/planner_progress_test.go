package tests

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
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
)

type plannerRefreshCanvas struct {
	fyne.Canvas
	refreshed []fyne.CanvasObject
}

func (c *plannerRefreshCanvas) Refresh(o fyne.CanvasObject) {
	c.refreshed = append(c.refreshed, o)
	c.Canvas.Refresh(o)
}

func TestSprintDayHoverRepaintsHiddenDeleteWithoutClick(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: k.Week, Start: 540, Minutes: 60}}}
	kanban.Draw(k)
	var day *shell.PlannerDayHover
	for _, d := range k.Dates {
		if d.Date == k.Week {
			day = d.Object.(*shell.PlannerDayHover)
		}
	}
	if day == nil {
		t.Fatal("planned day missing")
	}
	view := &plannerRefreshCanvas{Canvas: w.Canvas()}
	day.View = view
	origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(day)
	// Hover over the header's empty space, with no click or full-page refresh.
	test.MoveMouse(w.Canvas(), origin.Add(fyne.NewPos(15, 60)))
	if !day.Clear.Visible() || len(view.refreshed) == 0 || view.refreshed[len(view.refreshed)-1] != day.Header {
		t.Fatal("showing Delete did not repaint its visible header")
	}
	// Delete floats over the date's line: the date keeps its width.
	if day.Header.Objects[0].Size().Width != day.Header.Size().Width {
		t.Fatal("Delete moved the date")
	}
	if dir := os.Getenv("WORKLOG_PLANNER_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(dir, "planner-hover-without-click.png"))
		if err != nil {
			t.Fatal(err)
		}
		// Capture directly: the usual helper refreshes the entire page first,
		// which concealed the missing native repaint in the earlier test.
		err = png.Encode(file, w.Canvas().Capture())
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	count := len(view.refreshed)
	test.MoveMouse(w.Canvas(), fyne.NewPos(10, 10))
	if day.Clear.Visible() || len(view.refreshed) <= count || view.refreshed[len(view.refreshed)-1] != day.Header {
		t.Fatal("leaving the day did not repaint the hidden control")
	}
}

func TestSprintProgressReadsGitHubDraftsAndRemainingPlans(t *testing.T) {
	ui, w, c := plannerFixture(t)
	fyne.CurrentApp().Settings().SetTheme(shell.GoldTheme{Theme: theme.DefaultTheme()})
	k := ui.Kanban
	b := &k.Plan.Boards[0]
	b.Items[0].URL = "https://github.com/bigledger/example/issues/1"
	c.Item = b.Items[0]
	c.Key = c.Item.Key(b.ID)
	remote := backend.SprintItem{ID: "remote-done", URL: "https://github.com/bigledger/example/issues/2", Title: "Finished in GitHub", SprintID: c.Sprint.ID, Status: "Done", Effort: 120}
	planned := backend.SprintItem{ID: "planned", URL: "https://github.com/bigledger/example/issues/3", Title: "Still planned", SprintID: c.Sprint.ID, Effort: 240}
	b.Items = append(b.Items, remote, planned)
	k.Plan.Tasks[c.Key] = backend.SprintTask{Done: true, Blocks: []backend.PlanBlock{{Date: k.Week, Start: 540, Minutes: 120}}}
	k.Plan.Tasks[remote.Key(b.ID)] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: k.Week, Start: 660, Minutes: 120}}}
	k.Plan.Tasks[planned.Key(b.ID)] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: k.Week, Start: 780, Minutes: 240}}}
	ui.Cfg.ProjectURL = b.URL
	ui.EnsureProjectCache()
	for _, month := range sprintcalendar.CalendarMonths(k) {
		ui.ProjLoaded[shell.StatusCacheKey(month)] = true
	}
	key := shell.StatusCacheKey(k.Week[:7])
	ui.ProjItems[key] = []backend.WorklogItem{
		{Date: k.Week, Minutes: 120, URL: "https://github.com/bigledger/example/issues/101", ParentURL: c.Item.URL},
		{Date: k.Week, Minutes: 120, URL: "https://github.com/bigledger/example/issues/102", ParentURL: remote.URL},
	}
	if err := ui.Store.WriteRows([]backend.Row{{"id": "draft", "date": k.Week, "minutes": "60", "issue": "bigledger/example#3"}}); err != nil {
		t.Fatal(err)
	}
	kanban.Draw(k)
	assertReading := func(done, draft, plan int) {
		t.Helper()
		bar := ui.Meters["planner/"+dayNumber(k.Week)]
		doneShare, draftShare, plannedShare := float32(0), float32(0), float32(0)
		for _, part := range bar.Target {
			if part.Key == "@draft" {
				draftShare += part.Weight
			} else if part.Key == "@plan" {
				plannedShare += part.Weight
				if part.Fill != shell.PlannerPlanColor {
					t.Fatal("plans should be purple")
				}
			} else {
				doneShare += part.Weight
			}
		}
		denominator := float32(max(backend.Target, done+draft+plan))
		if doneShare != float32(done)/denominator || draftShare != float32(draft)/denominator || plannedShare != float32(plan)/denominator {
			t.Fatal("GitHub/draft/planned progress split is wrong", bar.Target)
		}
		for _, d := range k.Dates {
			if d.Date == k.Week {
				if d.Base != shell.DayFill(done, draft+plan) || countPlannerBubbles(d.Object) != 3 {
					t.Fatal("completed blocks disappeared or completion background is wrong")
				}
				foundPercent, foundDone, foundPlanned := false, false, false
				walkCanvas(d.Object, func(o fyne.CanvasObject) {
					if text, ok := o.(*canvas.Text); ok {
						if text.Text == sprintcalendar.PlannerDayPercentage(done+draft+plan) {
							foundPercent = true
						}
						if text.Text == fmt.Sprintf("%dm GitHub", done) {
							foundDone = true
						}
						if text.Text == fmt.Sprintf("%dm planned", plan) {
							foundPlanned = true
						}
					}
				})
				if !foundPercent || !foundDone || !foundPlanned {
					t.Fatal("day does not explain completed and planned minutes")
				}
			}
		}
	}
	assertReading(240, 60, 480)
	if dir := os.Getenv("WORKLOG_PLANNER_SCREENSHOTS"); dir != "" {
		captureSprintPNG(t, w, filepath.Join(dir, "planner-progress-mixed.png"))
	}
	// Exercise the existing completion checkbox and local Save action.
	kanban.EditorFor(k, *b, planned, true)
	var complete *widget.Check
	var save *widget.Button
	walkPopup(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if check, ok := o.(*widget.Check); ok && check.Text == "Completed locally" {
			complete = check
		}
		if button, ok := o.(*widget.Button); ok && button.Text == "Save local plan" {
			save = button
		}
	})
	if complete == nil || save == nil {
		t.Fatal("completion controls missing")
	}
	complete.SetChecked(true)
	test.Tap(save)
	assertReading(240, 60, 480)
	p, err := ui.Store.LoadSprintPlan()
	if err != nil || !p.Tasks[planned.Key(b.ID)].Done || len(p.Tasks[planned.Key(b.ID)].Blocks) != 1 || k.Plan.Boards[0].Items[1].Status != "Done" || k.Busy {
		t.Fatal("completion lost schedule data or changed GitHub state", err)
	}
	// The shared GitHub cache landing and a draft being pushed refresh the
	// active planner through the same drawRecent path as Log Work.
	ui.ProjItems[key] = append(ui.ProjItems[key], backend.WorklogItem{Date: k.Week, Minutes: 240, URL: "https://github.com/bigledger/example/issues/103", ParentURL: planned.URL})
	if err := ui.Store.UpdateRow("draft", backend.Row{"pushed_at": "now"}); err != nil {
		t.Fatal(err)
	}
	k.CalendarActive = true
	logwork.DrawRecent(ui)
	assertReading(480, 0, 480)
	if dir := os.Getenv("WORKLOG_PLANNER_SCREENSHOTS"); dir != "" {
		captureSprintPNG(t, w, filepath.Join(dir, "planner-progress-done.png"))
	}
}
