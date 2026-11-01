package main

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
)

func TestSprintClickTogglesOffWithoutAllIssuesButton(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	key := sprintKey(c.Sprint)
	k.filter = "current"
	k.toggleSprint(key)
	if k.filter != "all" {
		t.Fatal("current sprint did not deselect")
	}
	k.toggleSprint(key)
	if k.filter != "sprint" {
		t.Fatal("sprint did not select")
	}
	k.toggleSprint(key)
	if k.filter != "all" {
		t.Fatal("sprint did not toggle off")
	}
	k.draw()
	walkCanvas(k.body, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "All issues" {
			t.Fatal("redundant All issues button")
		}
	})
}

func TestSprintIssueCanBePlannedOnMultipleDays(t *testing.T) {
	ui, _, c := plannerFixture(t)
	p := ui.kanban.plan
	first, err := movePlannerIssue(p, c, -1, c.Sprint.StartDate)
	if err != nil {
		t.Fatal(err)
	}
	second, err := movePlannerIssue(first, c, -1, addDays(c.Sprint.StartDate, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Tasks[c.Key].Blocks) != 2 || len(first.Tasks[c.Key].Blocks) != 1 {
		t.Fatal("additional date replaced existing block")
	}
	ui.kanban.plan = second
	ui.kanban.draw()
	trayCards := 0
	walkCanvas(ui.kanban.calendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*plannerDragCard); ok && card.index == -1 && card.candidate.Key != plannerMeetingKey {
			trayCards++
		}
	})
	if trayCards != 1 {
		t.Fatal("scheduled issue disappeared from planning tray")
	}
}

func TestSprintViewModeUsesSelectedSprintDates(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	var mode *widget.Select
	walkCanvas(k.calendarBody, func(o fyne.CanvasObject) {
		if s, ok := o.(*widget.Select); ok && len(s.Options) == 3 && s.Options[1] == "Sprint" {
			mode = s
		}
	})
	if mode == nil {
		t.Fatal("Sprint missing from Week/Month view selector")
	}
	mode.SetSelected("Sprint")
	if len(k.dates) < c.Sprint.Duration {
		t.Fatal("sprint view omitted dates")
	}
	p, err := ui.store.loadSprintPlan()
	if err != nil || p.CalendarMode != "Sprint" {
		t.Fatal("Sprint view preference not saved")
	}
}

func TestLogSearchFiltersPendingAndCalendarRows(t *testing.T) {
	ui := &UI{issueInfo: map[string]IssueInfo{"o/r#7": {Title: "Invoice reconciliation"}}, logSearch: "invoice fix"}
	g := Group{Issue: "o/r#7", Commits: []Commit{{Sha: "abc123", Message: "Fix rounding"}}}
	if !ui.pendingMatchesSearch(g) {
		t.Fatal("pending search ignored issue title or commit message")
	}
	if ui.pendingMatchesSearch(Group{Issue: "o/r#8", Commits: []Commit{{Message: "Fix login"}}}) {
		t.Fatal("nonmatching pending group included")
	}
	ui.logSearch = "INVOICE"
	if !ui.rowMatchesSearch(Row{"issue": "o/r#7"}) || !ui.worklogMatchesSearch(WorklogItem{ParentTitle: "Invoice reconciliation"}) {
		t.Fatal("calendar search disagrees with pending search")
	}
	if ui.rowMatchesSearch(Row{"issue": "o/r#8", "description": "Login"}) {
		t.Fatal("nonmatching calendar row included")
	}
}

func TestSettingsScrollsOnlyRightAndColorPickerPersists(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("settings")
	defer w.Close()
	ui := &UI{store: newStore(t.TempDir()), win: w, cfg: Config{GithubAuthor: "example", Repos: []string{"exampleorg"}}}
	view := ui.buildSettingsTab()
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
	if err := ui.saveOrgColor("ExampleOrg", color.NRGBA{R: 20, G: 70, B: 180, A: 255}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := ui.store.LoadConfig()
	if cfg.OrgColors["exampleorg"] != "#1446b4" {
		t.Fatalf("custom color not saved: %v", cfg.OrgColors)
	}
	picker := ui.orgColorPicker("exampleorg", orgColor(ui.cfg, "exampleorg"), nil)
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
	a.Settings().SetTheme(goldTheme{Theme: theme.DefaultTheme()})
	w := a.NewWindow("settings")
	defer w.Close()
	ui := &UI{store: newStore(t.TempDir()), win: w, cfg: Config{GithubAuthor: "example", Repos: []string{"bigledger", "BigLedger-Support"}, ProjectURL: sprintBoardURLs[0], ExtraProjectURLs: []string{sprintBoardURLs[1]}}}
	w.SetContent(ui.buildSettingsTab())
	w.Resize(fyne.NewSize(1560, 1000))
	captureSprintPNG(t, w, filepath.Join(dir, "settings.png"))
	picker := ui.orgColorPicker("bigledger", orgColor(ui.cfg, "bigledger"), nil)
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
	ui.pendingLoaded = true
	ui.issueInfo = map[string]IssueInfo{"exampleorg/r#7": {Title: "Invoice"}, "exampleorg/r#8": {Title: "Login"}}
	ui.pending = PendingResult{Groups: []Group{{Issue: "exampleorg/r#7", Commits: []Commit{{Message: "Invoice fix", Sha: "aaa"}}}, {Issue: "exampleorg/r#8", Commits: []Commit{{Message: "Login fix", Sha: "bbb"}}}}}
	if err := ui.store.WriteRows([]Row{{"id": "invoice", "date": ui.weekStart, "minutes": "30", "issue": "exampleorg/r#7", "description": "Invoice"}, {"id": "login", "date": ui.weekStart, "minutes": "45", "issue": "exampleorg/r#8", "description": "Login"}}); err != nil {
		t.Fatal(err)
	}
	var search *widget.Entry
	walk(ui.logBody, func(o fyne.CanvasObject) {
		if entry, ok := o.(*widget.Entry); ok && entry.PlaceHolder == "Search issues, titles or commits" {
			search = entry
		}
	})
	if search == nil {
		t.Fatal("pending search entry missing")
	}
	search.SetText("Invoice")
	rows := []string{}
	walk(ui.weekBox, func(o fyne.CanvasObject) {
		if card, ok := o.(*dragTile); ok {
			rows = append(rows, card.row["id"])
		}
	})
	if len(rows) != 1 || rows[0] != "invoice" {
		t.Fatalf("calendar did not follow pending search: %v", rows)
	}
}
