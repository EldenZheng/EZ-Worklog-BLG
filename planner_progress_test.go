package main

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
	k := ui.kanban
	k.plan.Tasks[c.Key] = sprintTask{Blocks: []planBlock{{k.week, 540, 60}}}
	k.draw()
	var day *plannerDayHover
	for _, d := range k.dates {
		if d.date == k.week {
			day = d.object.(*plannerDayHover)
		}
	}
	if day == nil {
		t.Fatal("planned day missing")
	}
	view := &plannerRefreshCanvas{Canvas: w.Canvas()}
	day.view = view
	origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(day)
	// Hover over unused calendar space, with no click or full-page refresh.
	test.MoveMouse(w.Canvas(), origin.Add(fyne.NewPos(15, plannerDayHeaderHeight+200)))
	if !day.clear.Visible() || len(view.refreshed) == 0 || view.refreshed[len(view.refreshed)-1] != day.header {
		t.Fatal("showing Delete did not repaint its visible header")
	}
	if day.header.Objects[0].Size().Width != day.header.Size().Width-28 {
		t.Fatal("hover did not lay out space for Delete")
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
	if day.clear.Visible() || len(view.refreshed) <= count || view.refreshed[len(view.refreshed)-1] != day.header {
		t.Fatal("leaving the day did not repaint the hidden control")
	}
}

func TestSprintProgressReadsGitHubDraftsAndRemainingPlans(t *testing.T) {
	ui, w, c := plannerFixture(t)
	fyne.CurrentApp().Settings().SetTheme(goldTheme{Theme: theme.DefaultTheme()})
	k := ui.kanban
	b := &k.plan.Boards[0]
	b.Items[0].URL = "https://github.com/bigledger/example/issues/1"
	c.Item = b.Items[0]
	c.Key = c.Item.key(b.ID)
	remote := sprintItem{ID: "remote-done", URL: "https://github.com/bigledger/example/issues/2", Title: "Finished in GitHub", SprintID: c.Sprint.ID, Status: "Done", Effort: 120}
	planned := sprintItem{ID: "planned", URL: "https://github.com/bigledger/example/issues/3", Title: "Still planned", SprintID: c.Sprint.ID, Effort: 240}
	b.Items = append(b.Items, remote, planned)
	k.plan.Tasks[c.Key] = sprintTask{Done: true, Blocks: []planBlock{{k.week, 540, 120}}}
	k.plan.Tasks[remote.key(b.ID)] = sprintTask{Blocks: []planBlock{{k.week, 660, 120}}}
	k.plan.Tasks[planned.key(b.ID)] = sprintTask{Blocks: []planBlock{{k.week, 780, 240}}}
	ui.cfg.ProjectURL = b.URL
	ui.ensureProjectCache()
	for _, month := range k.calendarMonths() {
		ui.projLoaded[statusCacheKey(month)] = true
	}
	key := statusCacheKey(k.week[:7])
	ui.projItems[key] = []WorklogItem{
		{Date: k.week, Minutes: 120, URL: "https://github.com/bigledger/example/issues/101", ParentURL: c.Item.URL},
		{Date: k.week, Minutes: 120, URL: "https://github.com/bigledger/example/issues/102", ParentURL: remote.URL},
	}
	if err := ui.store.WriteRows([]Row{{"id": "draft", "date": k.week, "minutes": "60", "issue": "bigledger/example#3"}}); err != nil {
		t.Fatal(err)
	}
	k.draw()
	assertReading := func(done, draft, plan int) {
		t.Helper()
		bar := ui.meters["planner/"+dayNumber(k.week)]
		doneShare, draftShare, plannedShare := float32(0), float32(0), float32(0)
		for _, part := range bar.target {
			if part.key == "@draft" {
				draftShare += part.weight
			} else if part.key == "@plan" {
				plannedShare += part.weight
				if part.fill != plannerPlanColor {
					t.Fatal("plans should be purple")
				}
			} else {
				doneShare += part.weight
			}
		}
		if doneShare != float32(done)/480 || draftShare != float32(draft)/480 || plannedShare != float32(plan)/480 {
			t.Fatal("GitHub/draft/planned progress split is wrong", bar.target)
		}
		for _, d := range k.dates {
			if d.date == k.week {
				if d.base != dayFill(done, draft+plan) || countPlannerBubbles(d.object) != 3 {
					t.Fatal("completed blocks disappeared or completion background is wrong")
				}
				foundPercent, foundDone, foundPlanned := false, false, false
				walkCanvas(d.object, func(o fyne.CanvasObject) {
					if text, ok := o.(*canvas.Text); ok {
						if text.Text == plannerDayPercentage(done+draft+plan) {
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
	assertReading(240, 60, 180)
	if dir := os.Getenv("WORKLOG_PLANNER_SCREENSHOTS"); dir != "" {
		captureSprintPNG(t, w, filepath.Join(dir, "planner-progress-mixed.png"))
	}
	// Exercise the existing completion checkbox and local Save action.
	k.editorFor(*b, planned, true)
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
	assertReading(240, 60, 0)
	p, err := ui.store.loadSprintPlan()
	if err != nil || !p.Tasks[planned.key(b.ID)].Done || len(p.Tasks[planned.key(b.ID)].Blocks) != 1 || p.Boards[0].Items[1].Status != "Done" || k.busy {
		t.Fatal("completion lost schedule data or changed GitHub state", err)
	}
	// The shared GitHub cache landing and a draft being pushed refresh the
	// active planner through the same drawRecent path as Log Work.
	ui.projItems[key] = append(ui.projItems[key], WorklogItem{Date: k.week, Minutes: 240, URL: "https://github.com/bigledger/example/issues/103", ParentURL: planned.URL})
	if err := ui.store.UpdateRow("draft", Row{"pushed_at": "now"}); err != nil {
		t.Fatal(err)
	}
	k.calendarActive = true
	ui.drawRecent()
	assertReading(480, 0, 0)
	if dir := os.Getenv("WORKLOG_PLANNER_SCREENSHOTS"); dir != "" {
		captureSprintPNG(t, w, filepath.Join(dir, "planner-progress-done.png"))
	}
}
