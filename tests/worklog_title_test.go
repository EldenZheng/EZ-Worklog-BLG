package tests

import (
	"errors"
	"testing"

	"fyne.io/fyne/v2"

	"worklog/internal/menus/loglist"
	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func fillWorklogForm(t *testing.T, tab fyne.CanvasObject) {
	t.Helper()
	manualKindPicker(t, tab).SetSelected("Worklog")
	entryByPlaceholder(tab, logwork.IssuePlaceholder).SetText("exampleorg/repo#9")
	entryByPlaceholder(tab, "minutes").SetText("15")
	entryByPlaceholder(tab, manualRemarksPlaceholder).SetText("- work")
}

// A Worklog draft saves its issue's title, looked up when it is not known yet,
// so the bubble says what the issue is about.
func TestWorklogDraftSavesIssueTitle(t *testing.T) {
	previous := backend.Gh
	t.Cleanup(func() { backend.Gh = previous })
	calls := 0
	backend.Gh = func(args []string, _ ...string) (string, error) {
		calls++
		return `{"title":"Fix the export","state":"OPEN","number":9}`, nil
	}
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	tab := logwork.BuildLogTab(ui)
	fillWorklogForm(t, tab)
	tapButton(t, tab, "Save draft")
	rows, err := ui.Store.ReadRows()
	if err != nil {
		t.Fatal(err)
	}
	if calls == 0 || len(rows) != 1 || rows[0]["issue_title"] != "Fix the export" {
		t.Fatalf("calls = %d, rows = %v", calls, rows)
	}
	if got := logwork.RowTitle(&shell.UI{}, rows[0]); got != "Fix the export" {
		t.Fatalf("bubble title = %q", got)
	}
}

// With no way to reach GitHub the draft still saves, under its ref.
func TestWorklogDraftSavesWithoutTitleWhenLookupFails(t *testing.T) {
	previous := backend.Gh
	t.Cleanup(func() { backend.Gh = previous })
	backend.Gh = func(args []string, _ ...string) (string, error) { return "", errors.New("offline") }
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	tab := logwork.BuildLogTab(ui)
	fillWorklogForm(t, tab)
	tapButton(t, tab, "Save draft")
	rows, err := ui.Store.ReadRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["issue"] != "exampleorg/repo#9" || rows[0]["issue_title"] != "" {
		t.Fatalf("rows = %v", rows)
	}
}

// A title that lands after a bubble was drawn reaches the bubble too, so it
// cannot go on showing the ref while the popup, built later, shows the title.
func TestBubbleFollowsATitleLearnedLater(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	if _, err := ui.Store.AppendRows([]backend.Row{{
		"date": shell.Today(), "minutes": "15", "type": backend.KindOther, "issue": "exampleorg/repo#9",
	}}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)
	if contains(labels(ui.WeekBox), "Fix the export") {
		t.Fatal("the title is not known yet")
	}
	ui.LearnIssueInfos(map[string]backend.IssueInfo{"exampleorg/repo#9": {Title: "Fix the export"}})
	if !contains(labels(ui.WeekBox), "Fix the export") {
		t.Fatalf("the bubble did not pick up the title: %v", labels(ui.WeekBox))
	}
}

// Log List's pending bubble names a row by the title saved on it, as the row's
// own bubble and popup do, rather than falling back to the ref.
func TestPendingIssueUsesTheSavedTitle(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	if _, err := ui.Store.AppendRows([]backend.Row{{
		"date": shell.Today(), "minutes": "15", "type": backend.KindOther,
		"issue": "exampleorg/repo#9", "issue_title": "Fix the export",
	}}); err != nil {
		t.Fatal(err)
	}
	pending := loglist.PendingByIssue(ui)
	if len(pending) != 1 || pending[0].Title != "Fix the export" {
		t.Fatalf("pending = %+v", pending)
	}
}

// Moving a saved row to another issue drops the old issue's title with it.
func TestRowEditorDropsTitleWhenTheIssueChanges(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	made, err := ui.Store.AppendRows([]backend.Row{{
		"date": shell.Today(), "minutes": "15", "type": backend.KindOther,
		"issue": "exampleorg/repo#9", "issue_title": "Old issue", "remarks": "- work",
	}})
	if err != nil {
		t.Fatal(err)
	}
	form := logwork.RowEditor(ui, made[0], func() {}, nil).All()
	entryByPlaceholder(form, logwork.IssuePlaceholder).SetText("exampleorg/repo#10")
	tapButton(t, form, "Save")
	rows, err := ui.Store.ReadRows()
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["issue"] != "exampleorg/repo#10" || rows[0]["issue_title"] != "" {
		t.Fatalf("row = %v", rows[0])
	}
}
