//go:build demo

package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"worklog/internal/assemble"
	"worklog/internal/menus/kanban"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func TestReadmePlannerDemoFrames(t *testing.T) {
	out, frames := os.Getenv("DEMO_OUT"), os.Getenv("DEMO_FRAMES")
	if out == "" || frames == "" {
		t.Skip("DEMO_OUT and DEMO_FRAMES not set")
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, darkGold{shell.GoldTheme{Theme: theme.DefaultTheme()}})
	w := a.NewWindow(shell.WindowTitle)
	defer w.Close()
	w.Resize(fyne.NewSize(1560, 1000))
	ui := &shell.UI{Store: backend.NewStore(t.TempDir()), Win: w, CalMonth: shell.ThisMonth(), RepMonth: shell.CurrentPeriod(), HasCfg: true, ProfileLoaded: true}
	ui.Cfg = backend.Config{Repos: []string{"acme-corp", "acme-support"}, WorklogOwner: "demo-user", ProjectURL: "https://github.com/orgs/acme-corp/projects/1", ExtraProjectURLs: []string{"https://github.com/orgs/acme-support/projects/2"}, KanbanBoards: []backend.KanbanSource{}, OrgColors: map[string]string{"acme-corp": "#397FCE", "acme-support": "#D64A4A"}}
	plan := seedPlannerDemo(t, ui)
	assemble.BuildAllTabs(ui)
	w.SetContent(ui.Tabs)
	ui.Kanban.Loaded = true
	ui.Tabs.SelectIndex(7)
	kanban.Draw(ui.Kanban)
	recordKanbanDemo(t, ui, w, filepath.Join(frames, "kanban"), out)
	// Each recording starts from the same fictional board, independently.
	ui.Kanban.Save(plan)
	ui.Kanban.Filter = "current"
	ui.Kanban.ProjectOff = nil
	ui.Tabs.SelectIndex(8)
	kanban.Draw(ui.Kanban)
	recordPlannerDemo(t, ui, w, filepath.Join(frames, "planner"), out)
}

func seedPlannerDemo(t *testing.T, ui *shell.UI) backend.SprintPlan {
	start := backend.WeekStartOf(shell.Today())
	fields := []backend.SprintField{{Name: "Status", Options: []backend.FieldOption{{Name: "Created"}, {Name: "In Progress"}, {Name: "In Review"}, {Name: "Done"}}}, {Name: "Sprint"}}
	fields[1].Configuration.Iterations = []backend.SprintIteration{{ID: "current", Title: "Sprint 35", StartDate: start, Duration: 14}, {ID: "next", Title: "Sprint 36", StartDate: backend.AddDays(start, 14), Duration: 14}}
	main := backend.SprintBoard{ID: "core", Title: "Customer Platform", URL: ui.Cfg.ProjectURL, Fields: fields, Items: []backend.SprintItem{
		{ID: "invoice", Kind: "Issue", Title: "Invoice export to PDF", URL: demoURL(demoIssues[0]), Status: "Created", SprintID: "current", Effort: 90, Complexity: "M - 60 mins"},
		{ID: "search", Kind: "Issue", Title: "Improve customer search for large tenants", URL: demoURL(demoIssues[1]), Status: "In Progress", SprintID: "current", Effort: 120, Complexity: "L - 2 hours"},
		{ID: "review", Kind: "PullRequest", Title: "Review the shared invoice PDF template", URL: "https://github.com/acme-corp/web-app/pull/415", Status: "In Review", SprintID: "current", Effort: 45, Complexity: "S - 30 mins"},
		{ID: "webhook", Kind: "Issue", Title: "Rate-limit public webhook deliveries", URL: demoURL(demoIssues[2]), Status: "Created", SprintID: "next", Effort: 60, Complexity: "M - 60 mins"},
		{ID: "docs", Kind: "Issue", Title: "Document the webhook retry policy", URL: demoURL(demoIssues[5]), Status: "Done", SprintID: "current", Effort: 60, Complexity: "M - 60 mins"},
	}}
	support := backend.SprintBoard{ID: "support", Title: "Customer Support", URL: ui.Cfg.ExtraProjectURLs[0], Fields: fields, Items: []backend.SprintItem{
		{ID: "payment", Kind: "Issue", Title: "Resolve a stuck customer payment sync", URL: demoURL(demoIssues[4]), Status: "In Progress", SprintID: "current", Effort: 60, Complexity: "M - 60 mins"},
	}}
	plan := backend.SprintPlan{Boards: []backend.SprintBoard{main, support}, Tasks: map[string]backend.SprintTask{
		demoURL(demoIssues[0]): {Blocks: []backend.PlanBlock{{Date: start, Start: 540, Minutes: 90}}},
		demoURL(demoIssues[1]): {Blocks: []backend.PlanBlock{{Date: backend.AddDays(start, 1), Start: 540, Minutes: 120}}},
		demoURL(demoIssues[4]): {Blocks: []backend.PlanBlock{{Date: backend.AddDays(start, 3), Start: 540, Minutes: 60}}},
	}}
	if err := ui.Store.SaveSprintPlan(plan); err != nil {
		t.Fatal(err)
	}
	ui.EnsureProjectCache()
	logged := []backend.WorklogItem{
		{Date: start, Minutes: 300, URL: "https://github.com/acme-corp/web-app/issues/2401", ParentURL: demoURL(demoIssues[0])},
		{Date: start, Minutes: 180, URL: "https://github.com/acme-support/helpdesk/issues/2402", ParentURL: demoURL(demoIssues[4])},
		{Date: backend.AddDays(start, 1), Minutes: 120, URL: "https://github.com/acme-corp/web-app/issues/2403", ParentURL: demoURL(demoIssues[5])},
		{Date: backend.AddDays(start, 2), Minutes: 60, URL: "https://github.com/acme-support/helpdesk/issues/2404", ParentURL: demoURL(demoIssues[4])},
	}
	for shift := -1; shift <= 2; shift++ {
		key := shell.StatusCacheKey(shell.ShiftMonth(shell.ThisMonth(), shift))
		ui.ProjLoaded[key] = true
		from, to, _ := shell.CacheBounds(key)
		for _, item := range logged {
			if item.Date >= from && item.Date <= to {
				ui.ProjItems[key] = append(ui.ProjItems[key], item)
			}
		}
	}
	if err := ui.Store.WriteRows([]backend.Row{{"id": "demo-draft", "date": backend.AddDays(start, 1), "minutes": "45", "issue": demoRef(demoIssues[0]), "owner": "demo-user", "issue_title": demoIssues[0].Title}}); err != nil {
		t.Fatal(err)
	}
	return plan
}

func demoKanbanCard(t *testing.T, k *shell.KanbanUI, id string) *shell.SprintDragCard {
	t.Helper()
	var found *shell.SprintDragCard
	walkCanvas(k.Body, func(o fyne.CanvasObject) {
		if card, ok := o.(*shell.SprintDragCard); ok && card.Item.ID == id {
			found = card
		}
	})
	if found == nil {
		t.Fatal("demo card missing", id)
	}
	return found
}

func recordKanbanDemo(t *testing.T, ui *shell.UI, w fyne.Window, dir, out string) {
	k := ui.Kanban
	rec := newFrameRecorder(t, w, dir, [2]float32{1050, 660})
	base := rec.shot()
	if err := copyFile(filepath.Join(dir, base), filepath.Join(out, "10-kanban.png")); err != nil {
		t.Fatal(err)
	}
	rec.stay(base, 1400, false)
	// The icon legend filters types and saves the preference locally.
	for i := 0; i < 2; i++ {
		filter := k.TypeChoiceView.(*fyne.Container).Objects[1].(*shell.Tappable)
		rec.glide(rec.shot(), centreOf(filter), 200)
		test.Tap(filter)
		rec.stay(rec.shot(), 900, true)
	}
	// Show the normal drag feedback, then stage its fictional server response.
	for _, move := range []struct{ kind, value string }{{"status", "In Progress"}, {"sprint", backend.SprintKey(k.Plan.Boards[0].Iterations()[1])}} {
		card := demoKanbanCard(t, k, "invoice")
		var target *shell.KanbanTarget
		for _, candidate := range k.Targets {
			if candidate.Kind == move.kind && candidate.Value == move.value {
				target = candidate
			}
		}
		if target == nil {
			t.Fatal("demo drop target missing", move)
		}
		grab, aim := centreOf(card), centreOf(target.Object)
		if move.kind == "status" {
			aim[1] = grab[1]
		}
		rec.glide(rec.shot(), grab, 300)
		rec.stay(rec.shot(), 200, true)
		for step := 1; step <= 9; step++ {
			f := float32(step) / 9
			at := [2]float32{grab[0] + (aim[0]-grab[0])*f, grab[1] + (aim[1]-grab[1])*f}
			card.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: fyne.NewPos(at[0], at[1])}})
			rec.at = at
			rec.stay(rec.shot(), 65, false)
		}
		if k.Hover != target {
			t.Fatal("demo drag did not reach its target")
		}
		rec.stay(rec.shot(), 750, false)
		k.Hover = nil // Never call the GitHub mutation from a documentation demo.
		card.DragEnd()
		for bi := range k.Plan.Boards {
			for ii := range k.Plan.Boards[bi].Items {
				item := &k.Plan.Boards[bi].Items[ii]
				if item.ID == "invoice" {
					if move.kind == "status" {
						item.Status = move.value
					} else {
						item.SprintID = "next"
					}
				}
			}
		}
		kanban.Draw(k)
		rec.stay(rec.shot(), 1500, true)
	}
	// Select the next sprint and see its cards, then deselect to show all.
	var sprintRow *shell.Tappable
	walkCanvas(k.SideClip, func(o fyne.CanvasObject) {
		if row, ok := o.(*shell.Tappable); ok {
			walkCanvas(row.Content, func(child fyne.CanvasObject) {
				if label, ok := child.(*widget.Label); ok && label.Text == "Sprint 36" {
					sprintRow = row
				}
			})
		}
	})
	if sprintRow == nil {
		t.Fatal("future sprint row missing")
	}
	rec.glide(rec.shot(), centreOf(sprintRow), 200)
	test.Tap(sprintRow)
	rec.stay(rec.shot(), 1600, true)
	k.Filter = "all"
	kanban.Draw(k)
	rec.stay(rec.shot(), 2000, true)
	rec.finish([4]float32{0, 0, w.Canvas().Size().Width, w.Canvas().Size().Height})
}

func demoPlannerCard(t *testing.T, k *shell.KanbanUI, id string, index int) *shell.PlannerDragCard {
	t.Helper()
	var found *shell.PlannerDragCard
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*shell.PlannerDragCard); ok && card.Candidate.Item.ID == id && card.Index == index {
			found = card
		}
	})
	if found == nil {
		t.Fatal("planner demo card missing", id, index)
	}
	return found
}

func recordPlannerDemo(t *testing.T, ui *shell.UI, w fyne.Window, dir, out string) {
	k := ui.Kanban
	rec := newFrameRecorder(t, w, dir, [2]float32{900, 650})
	base := rec.shot()
	if err := copyFile(filepath.Join(dir, base), filepath.Join(out, "11-sprint-calendar.png")); err != nil {
		t.Fatal(err)
	}
	rec.stay(base, 1400, false)
	// Reuse the same sidebar issue twice: each drop adds a local block.
	for _, day := range []int{2, 4} {
		card := demoPlannerCard(t, k, "invoice", -1)
		grab := centreOf(card)
		rec.glide(rec.shot(), grab, 200)
		card.MouseIn(nil)
		rec.stay(rec.shot(), 650, false)
		card.MouseOut()
		var target *shell.PlannerDateTarget
		for _, candidate := range k.Dates {
			if candidate.Date == backend.AddDays(k.Week, day) {
				target = candidate
			}
		}
		if target == nil {
			t.Fatal("planner demo date missing")
		}
		aim := centreOf(target.Object)
		aim[1] = fyne.CurrentApp().Driver().AbsolutePositionForObject(target.Object).Y + sprintcalendar.PlannerDayHeaderHeight + 50
		rec.stay(rec.shot(), 150, true)
		for step := 1; step <= 10; step++ {
			f := float32(step) / 10
			at := [2]float32{grab[0] + (aim[0]-grab[0])*f, grab[1] + (aim[1]-grab[1])*f}
			card.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: fyne.NewPos(at[0], at[1])}})
			rec.at = at
			rec.stay(rec.shot(), 60, false)
		}
		if card.Hover != target {
			t.Fatal("planner demo drop did not reach date")
		}
		rec.stay(rec.shot(), 650, false)
		card.DragEnd()
		rec.stay(rec.shot(), 1300, true)
		if day == 2 {
			var grip *sprintcalendar.PlannerResizeGrip
			walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
				if g, ok := o.(*sprintcalendar.PlannerResizeGrip); ok && g.Key == demoURL(demoIssues[0]) && g.Index == 1 {
					grip = g
				}
			})
			if grip == nil {
				t.Fatal("planner demo resize edge missing")
			}
			rec.glide(rec.shot(), centreOf(grip), 350)
			rec.stay(rec.shot(), 150, true)
			for step := 0; step < 4; step++ {
				delta := 15 * grip.Layout.Pixels()
				grip.Dragged(&fyne.DragEvent{Dragged: fyne.Delta{DY: delta}})
				rec.at[1] += delta
				rec.stay(rec.shot(), 180, false)
			}
			grip.DragEnd()
			rec.stay(rec.shot(), 1400, true)
			if k.Plan.Tasks[demoURL(demoIssues[0])].Blocks[1].Minutes != 150 {
				t.Fatal("demo resize did not save 15-minute increments")
			}
		}
	}
	// The top bar shows actual, draft and planned time; support is display-only.
	var support *widget.Check
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if check, ok := o.(*widget.Check); ok && strings.Contains(check.Text, "+180") {
			support = check
		}
	})
	if support == nil {
		t.Fatal("support checkbox missing")
	}
	rec.glide(rec.shot(), centreOf(support), 250)
	support.SetChecked(true)
	rec.stay(rec.shot(), 1600, true)
	// Show that the same calendar can switch to Month and back to Week.
	for _, index := range []int{2, 0} {
		var view *shell.GoldSelect
		walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
			if picker, ok := o.(*shell.GoldSelect); ok && len(picker.Options) == 3 && picker.Options[0] == "Week" {
				view = picker
			}
		})
		if view == nil {
			t.Fatal("planner view selector missing")
		}
		rec.glide(rec.shot(), centreOf(view), 250)
		view.Tapped(nil)
		rec.stay(rec.shot(), 650, true)
		rec.glide(rec.shot(), centreOf(view.Rows.Objects[index]), 250)
		view.Choose(index)
		rec.stay(rec.shot(), 1800, true)
	}
	// Hover reveals the per-day trash control; Cancel keeps every local block.
	card := demoPlannerCard(t, k, "invoice", 1)
	card.MouseIn(nil)
	if card.Day == nil || !card.Day.Clear.Visible() {
		t.Fatal("per-day clear was not revealed on hover")
	}
	rec.glide(rec.shot(), centreOf(card.Day.Clear), 450)
	card.Day.Clear.OnTapped()
	rec.stay(rec.shot(), 1500, true)
	var cancel *widget.Button
	walkPopup(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if button, ok := o.(*widget.Button); ok && button.Text == "Cancel" {
			cancel = button
		}
	})
	if cancel == nil {
		t.Fatal("clear confirmation Cancel missing")
	}
	rec.glide(rec.shot(), centreOf(cancel), 200)
	test.Tap(cancel)
	rec.stay(rec.shot(), 1500, true)
	rec.finish([4]float32{0, 0, w.Canvas().Size().Width, w.Canvas().Size().Height})
}
