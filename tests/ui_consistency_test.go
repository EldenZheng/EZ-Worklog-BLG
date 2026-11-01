package tests

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/logwork"
	"worklog/internal/menus/settings"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestSprintClickTogglesOffWithoutAllIssuesButton(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	key := backend.SprintKey(c.Sprint)
	k.Filter = "current"
	kanban.ToggleSprint(k, key)
	if k.Filter != "all" {
		t.Fatal("current sprint did not deselect")
	}
	kanban.ToggleSprint(k, key)
	if k.Filter != "sprint" {
		t.Fatal("sprint did not select")
	}
	kanban.ToggleSprint(k, key)
	if k.Filter != "all" {
		t.Fatal("sprint did not toggle off")
	}
	kanban.Draw(k)
	walkCanvas(k.Body, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "All issues" {
			t.Fatal("redundant All issues button")
		}
	})
}

func TestSprintIssueCanBePlannedOnMultipleDays(t *testing.T) {
	ui, _, c := plannerFixture(t)
	p := ui.Kanban.Plan
	first, err := backend.MovePlannerIssue(p, c, -1, c.Sprint.StartDate)
	if err != nil {
		t.Fatal(err)
	}
	second, err := backend.MovePlannerIssue(first, c, -1, backend.AddDays(c.Sprint.StartDate, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Tasks[c.Key].Blocks) != 2 || len(first.Tasks[c.Key].Blocks) != 1 {
		t.Fatal("additional date replaced existing block")
	}
	ui.Kanban.Plan = second
	kanban.Draw(ui.Kanban)
	trayCards := 0
	walkCanvas(ui.Kanban.CalendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*shell.PlannerDragCard); ok && card.Index == -1 && card.Candidate.Key != shell.PlannerMeetingKey {
			trayCards++
		}
	})
	if trayCards != 1 {
		t.Fatal("scheduled issue disappeared from planning tray")
	}
}

func TestSprintViewModeUsesSelectedSprintDates(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	var mode *widget.Select
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if s, ok := o.(*widget.Select); ok && len(s.Options) == 3 && s.Options[1] == "Sprint" {
			mode = s
		}
	})
	if mode == nil {
		t.Fatal("Sprint missing from Week/Month view selector")
	}
	mode.SetSelected("Sprint")
	if len(k.Dates) < c.Sprint.Duration {
		t.Fatal("sprint view omitted dates")
	}
	p, err := ui.Store.LoadSprintPlan()
	if err != nil || p.CalendarMode != "Sprint" {
		t.Fatal("Sprint view preference not saved")
	}
}

func TestLogSearchFiltersPendingAndCalendarRows(t *testing.T) {
	ui := &shell.UI{IssueInfo: map[string]backend.IssueInfo{"o/r#7": {Title: "Invoice reconciliation"}}, LogSearch: "invoice fix"}
	g := backend.Group{Issue: "o/r#7", Commits: []backend.Commit{{Sha: "abc123", Message: "Fix rounding"}}}
	if !ui.PendingMatchesSearch(g) {
		t.Fatal("pending search ignored issue title or commit message")
	}
	if ui.PendingMatchesSearch(backend.Group{Issue: "o/r#8", Commits: []backend.Commit{{Message: "Fix login"}}}) {
		t.Fatal("nonmatching pending group included")
	}
	ui.LogSearch = "INVOICE"
	if !ui.RowMatchesSearch(backend.Row{"issue": "o/r#7"}) || !ui.WorklogMatchesSearch(backend.WorklogItem{ParentTitle: "Invoice reconciliation"}) {
		t.Fatal("calendar search disagrees with pending search")
	}
	if ui.RowMatchesSearch(backend.Row{"issue": "o/r#8", "description": "Login"}) {
		t.Fatal("nonmatching calendar row included")
	}
}

func TestSettingsScrollsOnlyRightAndColorPickerPersists(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("settings")
	defer w.Close()
	ui := &shell.UI{Store: backend.NewStore(t.TempDir()), Win: w, Cfg: backend.Config{GithubAuthor: "example", Repos: []string{"exampleorg"}}}
	view := settings.BuildSettingsTab(ui)
	w.SetContent(view)
	w.Resize(fyne.NewSize(1400, 900))
	var scroll *container.Scroll
	var more *widget.Button
	walk(view, func(o fyne.CanvasObject) {
		if x, ok := o.(*container.Scroll); ok && x.Direction == container.ScrollVerticalOnly {
			scroll = x
		}
		if b, ok := o.(*widget.Button); ok && b.Text == "..." {
			more = b
		}
	})
	if scroll == nil || more == nil {
		t.Fatal("right scroll or color menu missing")
	}
	position := a.Driver().AbsolutePositionForObject(scroll)
	if position.X < 200 {
		t.Fatal("scrollbar still wraps left identity panel")
	}
	if err := settings.SaveOrgColor(ui, "ExampleOrg", color.NRGBA{R: 20, G: 70, B: 180, A: 255}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := ui.Store.LoadConfig()
	if cfg.OrgColors["exampleorg"] != "#1446b4" {
		t.Fatalf("custom color not saved: %v", cfg.OrgColors)
	}
	picker := settings.OrgColorPicker(ui, "exampleorg", shell.OrgColor(ui.Cfg, "exampleorg"), nil)
	if !picker.Advanced {
		t.Fatal("custom color controls not enabled")
	}
	picker.Hide()
}

func TestSettingsScreenshots(t *testing.T) {
	dir := os.Getenv("WORKLOG_PLANNER_SCREENSHOTS")
	if dir == "" {
		t.Skip("optional visual check")
	}
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(shell.GoldTheme{Theme: theme.DefaultTheme()})
	w := a.NewWindow("settings")
	defer w.Close()
	ui := &shell.UI{Store: backend.NewStore(t.TempDir()), Win: w, Cfg: backend.Config{GithubAuthor: "example", Repos: []string{"bigledger", "BigLedger-Support"}, ProjectURL: backend.SprintBoardURLs[0], ExtraProjectURLs: []string{backend.SprintBoardURLs[1]}}}
	w.SetContent(settings.BuildSettingsTab(ui))
	w.Resize(fyne.NewSize(1560, 1000))
	captureSprintPNG(t, w, filepath.Join(dir, "settings.png"))
	picker := settings.OrgColorPicker(ui, "bigledger", shell.OrgColor(ui.Cfg, "bigledger"), nil)
	captureSprintPNG(t, w, filepath.Join(dir, "settings-color-picker.png"))
	picker.Hide()
	var save *widget.Button
	walk(w.Content(), func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Save settings" {
			save = b
		}
	})
	if save == nil {
		t.Fatal("save button missing")
	}
	save.OnTapped()
	captureSprintPNG(t, w, filepath.Join(dir, "settings-saved.png"))
}

func TestLogSearchUpdatesPendingAndWeekTogether(t *testing.T) {
	ui := weekUI(t)
	ui.PendingLoaded = true
	ui.IssueInfo = map[string]backend.IssueInfo{"exampleorg/r#7": {Title: "Invoice"}, "exampleorg/r#8": {Title: "Login"}}
	ui.Pending = backend.PendingResult{Groups: []backend.Group{{Issue: "exampleorg/r#7", Commits: []backend.Commit{{Message: "Invoice fix", Sha: "aaa"}}}, {Issue: "exampleorg/r#8", Commits: []backend.Commit{{Message: "Login fix", Sha: "bbb"}}}}}
	if err := ui.Store.WriteRows([]backend.Row{{"id": "invoice", "date": ui.WeekStart, "minutes": "30", "issue": "exampleorg/r#7", "description": "Invoice"}, {"id": "login", "date": ui.WeekStart, "minutes": "45", "issue": "exampleorg/r#8", "description": "Login"}}); err != nil {
		t.Fatal(err)
	}
	var search *widget.Entry
	walk(ui.LogBody, func(o fyne.CanvasObject) {
		if entry, ok := o.(*widget.Entry); ok && entry.PlaceHolder == "Search issues, titles or commits" {
			search = entry
		}
	})
	if search == nil {
		t.Fatal("pending search entry missing")
	}
	search.SetText("Invoice")
	rows := []string{}
	walk(ui.WeekBox, func(o fyne.CanvasObject) {
		if card, ok := o.(*logwork.DragTile); ok {
			rows = append(rows, card.Row["id"])
		}
	})
	if len(rows) != 1 || rows[0] != "invoice" {
		t.Fatalf("calendar did not follow pending search: %v", rows)
	}
}
