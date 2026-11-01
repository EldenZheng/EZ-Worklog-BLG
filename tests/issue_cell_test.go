package tests

import (
	"testing"
	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// buttonNamed finds a button by its label anywhere in a widget tree.
func buttonNamed(o fyne.CanvasObject, label string) *widget.Button {
	var found *widget.Button
	walk(o, func(c fyne.CanvasObject) {
		if b, ok := c.(*widget.Button); ok && b.Text == label {
			found = b
		}
	})
	return found
}

// Every saved row offers its own push, and the label says which kind it is: a
// first push while pushed_at is empty, a deliberate re-push once it is set. A
// partially failed push leaves pushed_at empty so the row still reads as unsent.
func TestRowTilePushButtonTracksPushedState(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &shell.UI{Store: backend.NewStore(t.TempDir()), Win: w,
		Cfg: backend.Config{Repos: []string{"exampleorg"}}}

	for _, c := range []struct {
		name string
		row  backend.Row
	}{
		{"never pushed", backend.Row{"id": "1", "issue": "exampleorg/repo#1", "date": "2026-08-09"}},
		{"partial push", backend.Row{"id": "2", "issue": "exampleorg/repo#2", "date": "2026-08-09",
			"item_id": "PVTI_x", "issue_url": "https://github.com/exampleorg/repo/issues/2"}},
	} {
		b := buttonNamed(logwork.RowTile(ui, c.row, func() {}), "Push")
		if b == nil {
			t.Fatalf("%s: no push button on the tile", c.name)
		}
		if b.Disabled() {
			t.Fatalf("%s: an unsent row must be pushable", c.name)
		}
	}

	// A finished row keeps a button, but it is plainly a second send: the board
	// fields are overwritten and the sub-issue reused, not created twice.
	done := backend.Row{"id": "3", "issue": "exampleorg/repo#3", "date": "2026-08-09",
		"pushed_at": "2026-08-10T03:01:18"}
	tile := logwork.RowTile(ui, done, func() {})
	if buttonNamed(tile, "Push") != nil {
		t.Fatal("a pushed row should not offer a first push")
	}
	if buttonNamed(tile, "Re-push") == nil {
		t.Fatal("a pushed row should still offer a re-push")
	}

	// Nothing to push to: the button is there but dead, and the editor is where
	// an issue ref gets added.
	bare := logwork.RowTile(ui, backend.Row{"id": "4", "date": "2026-08-09", "type": "other"}, func() {})
	b := buttonNamed(bare, "Push")
	if b == nil || !b.Disabled() {
		t.Fatal("a row with no issue ref must not offer a live push")
	}

	// Three kinds have no ref on disk and are pushable anyway, because theirs is
	// decided when they are pushed: meeting and bulk review use daily issues,
	// and an independent entry creates the parent it names.
	for _, c := range []struct {
		name string
		row  backend.Row
	}{
		{"meeting", backend.Row{"id": "5", "date": "2026-08-09", "type": backend.KindMeeting}},
		{"bulk review", backend.Row{"id": "8", "date": "2026-08-09", "type": backend.KindBulkReview,
			"remarks": "https://github.com/o/r/pull/1 (10m)"}},
		{"independent", backend.Row{"id": "6", "date": "2026-08-09", "type": backend.KindIndependent,
			"parent_repo": "exampleorg/sample-intranet", "parent_title": "Tidy the docs"}},
	} {
		b := buttonNamed(logwork.RowTile(ui, c.row, func() {}), "Push")
		if b == nil || b.Disabled() {
			t.Fatalf("%s: its issue is made at push time, so push must be live", c.name)
		}
	}

	// An independent entry that never got a repo and a title has nothing to
	// create, so it is back to being unpushable.
	half := backend.Row{"id": "7", "date": "2026-08-09", "type": backend.KindIndependent}
	if b := buttonNamed(logwork.RowTile(ui, half, func() {}), "Push"); b == nil || !b.Disabled() {
		t.Fatal("an independent entry with no parent named cannot be pushed")
	}
}

func TestCodeReviewInputsResolveWithoutFetchButtonsAndBulkGrows(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	ui.Cfg.BulkCodeReviewRepo = "exampleorg/tasks"
	ui.Cfg.ProjectURL = "https://github.com/orgs/exampleorg/projects/9"
	ui.EnsureProjectCache()
	day := shell.Today()
	key := shell.StatusCacheKey(day[:7])
	ui.ProjItems[key] = []backend.WorklogItem{{Date: day, Minutes: 120}}
	ui.ProjLoaded[key] = true
	if _, err := ui.Store.AppendRows([]backend.Row{{
		"date": day, "minutes": "30", "type": backend.KindOther, "issue": "exampleorg/repo#7",
	}}); err != nil {
		t.Fatal(err)
	}
	tab := logwork.BuildLogTab(ui)
	if buttonNamed(tab, "Fetch issue") != nil {
		t.Fatal("single Code Review should resolve automatically, not show a Fetch issue button")
	}
	if buttonNamed(tab, "Fetch linked issues") != nil {
		t.Fatal("Bulk Review should resolve each row automatically, not show a fetch button")
	}
	var kinds *widget.RadioGroup
	walk(tab, func(o fyne.CanvasObject) {
		if group, ok := o.(*widget.RadioGroup); ok {
			for _, option := range group.Options {
				if option == "Bulk Review" {
					kinds = group
				}
			}
		}
	})
	if kinds == nil {
		t.Fatal("missing Log Work kind selector")
	}
	wantOrder := []string{"Commits", "Worklog", "Code Review", "Bulk Review", "Meeting", "Independent"}
	if len(kinds.Options) != len(wantOrder) {
		t.Fatalf("Log Work selector options = %v, want %v", kinds.Options, wantOrder)
	}
	for i, want := range wantOrder {
		if kinds.Options[i] != want {
			t.Fatalf("Log Work selector options = %v, want %v", kinds.Options, wantOrder)
		}
	}
	kinds.SetSelected("Bulk Review")
	if !contains(labels(tab), "Creates/reuses its own issue in "+ui.Cfg.BulkCodeReviewRepo) {
		t.Fatal("Bulk Review should say that it owns a general-task issue")
	}
	var prs, mins []*widget.Entry
	walk(tab, func(o fyne.CanvasObject) {
		if e, ok := o.(*widget.Entry); ok {
			switch e.PlaceHolder {
			case "https://github.com/exampleorg/repo/pull/123":
				prs = append(prs, e)
			case "10":
				mins = append(mins, e)
			}
		}
	})
	if len(prs) != 1 || len(mins) != 1 {
		t.Fatalf("bulk sheet should start with one row, got %d PR and %d minute cells", len(prs), len(mins))
	}
	prs[0].SetText("https://github.com/exampleorg/app/pull/12")
	mins[0].SetText("15")
	prs, mins = nil, nil
	walk(tab, func(o fyne.CanvasObject) {
		if e, ok := o.(*widget.Entry); ok {
			switch e.PlaceHolder {
			case "https://github.com/exampleorg/repo/pull/123":
				prs = append(prs, e)
			case "10":
				mins = append(mins, e)
			}
		}
	})
	if len(prs) != 2 || len(mins) != 2 {
		t.Fatalf("completing row one should add row two, got %d PR and %d minute cells", len(prs), len(mins))
	}
	got := labels(tab)
	for _, want := range []string{
		"Total: 15 min",
		day + " already on GitHub: " + logwork.DayScoreLine(120),
		"30 min on pending log",
		"+15 min this entry",
	} {
		if !contains(got, want) {
			t.Fatalf("bulk total should use the commit popup's day calculation; missing %q in %v", want, got)
		}
	}
	// Cancel the debounced lookup; this is an offline UI-shape test.
	prs[0].SetText("")
}

// A push is only "done" when nothing was left unwritten.
func TestPushResultComplete(t *testing.T) {
	if !(backend.PushResult{Notes: []string{"Reused the existing sub-issue #9024."}}).Complete() {
		t.Fatal("informational notes must not mark a push incomplete")
	}
	if (backend.PushResult{Problems: []string{"Could not set Status to Done: nope"}}).Complete() {
		t.Fatal("a problem must mark the push incomplete")
	}
}

// The tile shows raw minutes. Hours and minutes had to be converted back to a
// number before every push, since minutes is what the worklog field takes.
func TestRowTileShowsRawMinutes(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &shell.UI{Store: backend.NewStore(t.TempDir()), Win: w, Cfg: backend.Config{Repos: []string{"exampleorg"}}}

	tile := logwork.RowTile(ui, backend.Row{
		"id": "1", "date": "2026-08-11", "minutes": "90",
		"type": "commit", "issue": "exampleorg/repo#7",
	}, func() {})
	got := labels(tile)
	if !contains(got, "90 min") {
		t.Fatalf("tile should show the minutes as logged: %v", got)
	}
	if contains(got, "1h 30m") {
		t.Fatalf("tile should not convert to hours: %v", got)
	}
}

func TestDeleteConfirmationUsesCompactPopup(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	w.Resize(fyne.NewSize(1200, 800))
	ui := &shell.UI{Store: backend.NewStore(t.TempDir()), Win: w}

	sz := ui.RemovalPopupSize(widget.NewLabel("Delete this entry?"))
	if sz.Width > 640 || sz.Width < 400 || sz.Height >= 400 {
		t.Fatalf("delete confirmation should use a compact size, got %v", sz)
	}
	logwork.ConfirmDelete(ui, backend.Row{"id": "1"}, func() {})
	if w.Canvas().Overlays().Top() == nil {
		t.Fatal("delete confirmation did not open")
	}
}
