package main

import (
	"testing"

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
	ui := &UI{store: newStore(t.TempDir()), win: w,
		cfg: Config{Repos: []string{"bigledger"}}}

	for _, c := range []struct {
		name string
		row  Row
	}{
		{"never pushed", Row{"id": "1", "issue": "bigledger/repo#1", "date": "2026-08-09"}},
		{"partial push", Row{"id": "2", "issue": "bigledger/repo#2", "date": "2026-08-09",
			"item_id": "PVTI_x", "issue_url": "https://github.com/bigledger/repo/issues/2"}},
	} {
		b := buttonNamed(ui.rowTile(c.row, func() {}), "Push")
		if b == nil {
			t.Fatalf("%s: no push button on the tile", c.name)
		}
		if b.Disabled() {
			t.Fatalf("%s: an unsent row must be pushable", c.name)
		}
	}

	// A finished row keeps a button, but it is plainly a second send: the board
	// fields are overwritten and the sub-issue reused, not created twice.
	done := Row{"id": "3", "issue": "bigledger/repo#3", "date": "2026-08-09",
		"pushed_at": "2026-08-10T03:01:18"}
	tile := ui.rowTile(done, func() {})
	if buttonNamed(tile, "Push") != nil {
		t.Fatal("a pushed row should not offer a first push")
	}
	if buttonNamed(tile, "Re-push") == nil {
		t.Fatal("a pushed row should still offer a re-push")
	}

	// Nothing to push to: the button is there but dead, and the editor is where
	// an issue ref gets added.
	bare := ui.rowTile(Row{"id": "4", "date": "2026-08-09", "type": "meeting"}, func() {})
	b := buttonNamed(bare, "Push")
	if b == nil || !b.Disabled() {
		t.Fatal("a row with no issue ref must not offer a live push")
	}
}

// A push is only "done" when nothing was left unwritten.
func TestPushResultComplete(t *testing.T) {
	if !(PushResult{Notes: []string{"Reused the existing sub-issue #9024."}}).Complete() {
		t.Fatal("informational notes must not mark a push incomplete")
	}
	if (PushResult{Problems: []string{"Could not set Status to Done: nope"}}).Complete() {
		t.Fatal("a problem must mark the push incomplete")
	}
}

// The tile shows raw minutes. Hours and minutes had to be converted back to a
// number before every push, since minutes is what the worklog field takes.
func TestRowTileShowsRawMinutes(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, cfg: Config{Repos: []string{"bigledger"}}}

	tile := ui.rowTile(Row{
		"id": "1", "date": "2026-08-11", "minutes": "90",
		"type": "commit", "issue": "bigledger/repo#7",
	}, func() {})
	got := labels(tile)
	if !contains(got, "90 min") {
		t.Fatalf("tile should show the minutes as logged: %v", got)
	}
	if contains(got, "1h 30m") {
		t.Fatalf("tile should not convert to hours: %v", got)
	}
}
