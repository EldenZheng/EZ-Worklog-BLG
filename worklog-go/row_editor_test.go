package main

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// walk visits every object in a widget tree.
func walk(o fyne.CanvasObject, fn func(fyne.CanvasObject)) {
	fn(o)
	switch v := o.(type) {
	case *tappable:
		walk(v.content, fn)
	case *dayColumn:
		walk(v.body, fn)
	case *dragTile:
		walk(v.content, fn)
	case *widget.Card:
		if v.Content != nil {
			walk(v.Content, fn)
		}
	case *container.Scroll:
		walk(v.Content, fn)
	case *dayChart:
		// A widget's children live in its renderer, which is where the bars are.
		for _, o := range v.CreateRenderer().Objects() {
			walk(o, fn)
		}
	case *fyne.Container:
		for _, c := range v.Objects {
			walk(c, fn)
		}
	}
}

func entriesIn(o fyne.CanvasObject) []*widget.Entry {
	var out []*widget.Entry
	walk(o, func(c fyne.CanvasObject) {
		if e, ok := c.(*widget.Entry); ok {
			out = append(out, e)
		}
	})
	return out
}

// entryByPlaceholder picks a field out of the form by the hint it shows, which
// survives reordering the layout in a way an index does not.
func entryByPlaceholder(o fyne.CanvasObject, hint string) *widget.Entry {
	for _, e := range entriesIn(o) {
		if e.PlaceHolder == hint {
			return e
		}
	}
	return nil
}

func multiLineEntry(o fyne.CanvasObject) *widget.Entry {
	for _, e := range entriesIn(o) {
		if e.MultiLine {
			return e
		}
	}
	return nil
}

func tapButton(t *testing.T, o fyne.CanvasObject, label string) {
	t.Helper()
	var found *widget.Button
	walk(o, func(c fyne.CanvasObject) {
		if b, ok := c.(*widget.Button); ok && b.Text == label {
			found = b
		}
	})
	if found == nil {
		t.Fatalf("button %q not found", label)
	}
	found.OnTapped()
}

func editorUI(t *testing.T) *UI {
	t.Helper()
	w := test.NewApp().NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: thisMonth(), repMonth: thisMonth()}
	ui.cfg = Config{Repos: []string{"bigledger"}}
	w.SetContent(ui.buildLogTab())
	return ui
}

// Editing a saved row rewrites it where it stands. Appending instead would
// double-count the minutes and leave the original still waiting to push.
func TestRowEditorSavesInPlace(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()

	made, err := ui.store.AppendRows([]Row{{
		"date": today(), "minutes": "90", "type": "commit",
		"description": "half written", "issue": "bigledger/repo#7",
		"refs": "3613aff", "remarks": "- half written", "mode": "subissue",
	}})
	if err != nil {
		t.Fatal(err)
	}
	row := made[0]

	refreshed, finished := 0, 0
	form := ui.rowEditor(row, func() { refreshed++ }, func() { finished++ }).all()

	mins := entryByPlaceholder(form, "480")
	if mins == nil {
		t.Fatal("minutes field is missing from the editor")
	}
	mins.SetText("480")
	rem := multiLineEntry(form)
	if rem == nil {
		t.Fatal("remarks box is missing from the editor")
	}
	if !strings.Contains(rem.Text, "half written") {
		t.Fatalf("editor opened without the row's remarks: %q", rem.Text)
	}
	rem.SetText("- finished the thing")

	tapButton(t, form, "Save")

	rows, err := ui.store.ReadRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("an edit must not append a row, store holds %d", len(rows))
	}
	got := rows[0]
	if got["id"] != row["id"] {
		t.Fatalf("row identity changed: %q -> %q", row["id"], got["id"])
	}
	if got["minutes"] != "480" || got["remarks"] != "- finished the thing" {
		t.Fatalf("edits were not written: %+v", got)
	}
	if got["pushed_at"] != "" {
		t.Fatalf("saving locally must not mark the row pushed: %q", got["pushed_at"])
	}
	if refreshed == 0 {
		t.Fatal("the list behind the popup was never redrawn")
	}
	if finished == 0 {
		t.Fatal("a plain save should close the popup")
	}
}

// Save & push on a row with no issue ref saves and says so. It must not reach
// for the network, and it must not close the popup with the message unread.
func TestRowEditorPushWithoutIssueStaysLocal(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()

	made, err := ui.store.AppendRows([]Row{{
		"date": today(), "minutes": "60", "type": "meeting",
		"description": "standup", "remarks": "- standup",
	}})
	if err != nil {
		t.Fatal(err)
	}

	finished := 0
	form := ui.rowEditor(made[0], func() {}, func() { finished++ }).all()
	tapButton(t, form, "Save & push")

	if !contains(labels(form), "no issue ref to push to") {
		t.Fatalf("expected the entry to say why nothing was pushed: %v", labels(form))
	}
	if finished != 0 {
		t.Fatal("the popup closed before the message could be read")
	}
	rows, _ := ui.store.ReadRows()
	if rows[0]["pushed_at"] != "" {
		t.Fatalf("nothing was pushed, so pushed_at must stay empty: %q", rows[0]["pushed_at"])
	}
}

// buttonNames lists every button label in a tree.
func buttonNames(o fyne.CanvasObject) []string {
	var out []string
	walk(o, func(c fyne.CanvasObject) {
		if b, ok := c.(*widget.Button); ok {
			out = append(out, b.Text)
		}
	})
	return out
}

// The popup's buttons ride in the same row as Back, above the scroll. Left at
// the foot of the form they sat under a full set of remarks, and Back had a
// line to itself.
func TestPopupKeepsTheActionsBesideBack(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()

	made, err := ui.store.AppendRows([]Row{{
		"date": today(), "minutes": "90", "issue": "bigledger/repo#7", "remarks": "- work",
	}})
	if err != nil {
		t.Fatal(err)
	}
	back := widget.NewButton("Back", func() {})
	shell := popupShell(back, ui.rowEditor(made[0], func() {}, func() {}))

	for _, want := range []string{"Back", "Save", "Save & push", "Compact with AI"} {
		if !contains(buttonNames(shell), want) {
			t.Fatalf("%q is missing from the popup: %v", want, buttonNames(shell))
		}
	}

	// Whatever scrolls must not carry them, or they scroll away again.
	var scroll *container.Scroll
	walk(shell, func(c fyne.CanvasObject) {
		if s, ok := c.(*container.Scroll); ok && scroll == nil {
			scroll = s
		}
	})
	if scroll == nil {
		t.Fatal("the popup body should scroll")
	}
	for _, name := range buttonNames(scroll.Content) {
		if name == "Save & push" || name == "Back" {
			t.Fatalf("%q scrolls with the form: %v", name, buttonNames(scroll.Content))
		}
	}
	// The mode dropdown rides up with them.
	if !contains(labels(shell), "Push as") {
		t.Fatalf("the push mode picker should be on the bar: %v", labels(shell))
	}
}

// The remarks own the foot of the popup, outside the scroll that holds the
// fields. Inside it, the entry's own scroller was nested in another one, and
// the wheel was swallowed by whichever the pointer was over — over the remarks
// that meant the form would not move at all.
func TestRemarksOwnTheFootOfThePopup(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()

	back := widget.NewButton("Back", func() {})
	shell := popupShell(back, ui.rowEditor(
		Row{"id": "1", "date": today(), "remarks": "- work"}, func() {}, func() {}))

	// One scroller in the popup, and it holds the fields. A second one around
	// the remarks would nest with the entry's own and swallow the wheel again.
	var scrolls []*container.Scroll
	walk(shell, func(c fyne.CanvasObject) {
		if s, ok := c.(*container.Scroll); ok {
			scrolls = append(scrolls, s)
		}
		if _, ok := c.(*container.Split); ok {
			t.Fatal("the popup should have no draggable divider")
		}
	})
	if len(scrolls) != 1 {
		t.Fatalf("the popup should hold exactly one scroller, found %d", len(scrolls))
	}
	if multiLineEntry(scrolls[0]) != nil {
		t.Fatal("the remarks are back inside the scrolling fields")
	}
	if multiLineEntry(shell) == nil {
		t.Fatal("the remarks are missing from the popup")
	}
}

// The remarks box wraps. Clipping meant a long bullet ran off to the right and
// scrolling down dragged the view sideways with it.
func TestRemarksBoxWrapsItsText(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()

	form := ui.rowEditor(Row{"id": "1", "date": today(), "remarks": "- a long line"},
		func() {}, func() {}).all()
	rem := multiLineEntry(form)
	if rem == nil {
		t.Fatal("remarks box is missing from the editor")
	}
	if rem.Wrapping != fyne.TextWrapWord {
		t.Fatalf("remarks should wrap, got %v", rem.Wrapping)
	}
}

// Every row in the waiting list carries an edit button, and it opens a popup.
func TestEntryTableRowOpensTheEditor(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()

	if _, err := ui.store.AppendRows([]Row{{
		"date": today(), "minutes": "90", "type": "commit",
		"description": "waiting", "issue": "bigledger/repo#7",
	}}); err != nil {
		t.Fatal(err)
	}
	ui.drawRecent()

	// A waiting entry dated today stands on its day in the week strip; one dated
	// outside the week on show is in the list below. Either way it can be edited.
	var edit *widget.Button
	walk(container.NewVBox(ui.weekBox, ui.recentBox), func(c fyne.CanvasObject) {
		if b, ok := c.(*widget.Button); ok && b.Icon == theme.DocumentCreateIcon() {
			edit = b
		}
	})
	if edit == nil {
		t.Fatal("the waiting list offers no way to edit a row")
	}
	edit.OnTapped()
	if ui.win.Canvas().Overlays().Top() == nil {
		t.Fatal("the edit button opened nothing")
	}
}

// A saved commit row is labelled by its issue, not by the commit subjects
// joined end to end — the remarks already carry the work in full.
func TestRowLabelPrefersTheIssueTitle(t *testing.T) {
	ui := &UI{issueInfo: map[string]IssueInfo{
		"bigledger/repo#12": {Org: "bigledger", Repo: "repo", Number: 12,
			Title: "Datepicker custom field: relative defaults"},
	}}
	commits := []Commit{
		{Sha: "3613aff", Message: "relative date defaults", Full: "relative date defaults\n- resolver"},
		{Sha: "9993d06", Message: "partial-save fix", Full: "partial-save fix\n- error path"},
	}
	if got := ui.rowLabel("bigledger/repo#12", commits); got != "Datepicker custom field: relative defaults" {
		t.Fatalf("expected the issue title, got %q", got)
	}
	// Title not loaded yet: fall back to the commit, and say how many there are
	// so the row is not mistaken for a single one.
	got := ui.rowLabel("bigledger/other#3", commits)
	if !strings.Contains(got, "relative date defaults") || !strings.Contains(got, "2 commits") {
		t.Fatalf("fallback should name the commit and its count, got %q", got)
	}
	if got := ui.rowLabel("bigledger/other#3", commits[:1]); got != "relative date defaults" {
		t.Fatalf("a lone commit should speak for itself, got %q", got)
	}
	if got := ui.rowLabel("bigledger/other#3", nil); got != "bigledger/other#3" {
		t.Fatalf("with nothing else to say, the ref is the label, got %q", got)
	}
}

// The tile reads from the issue title too, so a row saved before the title
// arrived still shows the issue once it has.
func TestRowTileShowsTheIssueTitle(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	ui.issueInfo = map[string]IssueInfo{
		"bigledger/repo#12": {Repo: "repo", Number: 12, Title: "Relative date defaults"},
	}
	tile := ui.rowTile(Row{
		"id": "1", "date": today(), "minutes": "90", "type": "commit",
		"issue": "bigledger/repo#12", "description": "old joined commit subjects",
	}, func() {})
	got := labels(tile)
	if !contains(got, "Relative date defaults") {
		t.Fatalf("tile should carry the issue title: %v", got)
	}
	if contains(got, "old joined commit subjects") {
		t.Fatalf("the stored description should not win over the title: %v", got)
	}
}
