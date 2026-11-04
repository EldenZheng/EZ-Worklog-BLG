package tests

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/logwork"
	"worklog/internal/menus/report"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// walk visits every object in a widget tree.
func walk(o fyne.CanvasObject, fn func(fyne.CanvasObject)) {
	fn(o)
	switch v := o.(type) {
	case *shell.Tappable:
		walk(v.Content, fn)
	case *shell.Fold:
		walk(v.Content, fn)
	case *shell.StripeBubble:
		walk(v.Body, fn)
	case *shell.HoverArea:
		walk(v.Content, fn)
	case *shell.HoverRegion:
		walk(v.Content, fn)
	case *shell.Wipe:
		walk(v.Content, fn)
	case *shell.Morph:
		walk(v.Content, fn)
	case *shell.DayColumn:
		walk(v.Body, fn)
	case *logwork.DragTile:
		walk(v.Content, fn)
	case *shell.HoverButton:
		fn(&v.Button)
	case *widget.Card:
		if v.Content != nil {
			walk(v.Content, fn)
		}
	case *container.Scroll:
		walk(v.Content, fn)
	case *container.ThemeOverride:
		walk(v.Content, fn)
	case *report.DayChart:
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

func editorUI(t *testing.T) *shell.UI {
	t.Helper()
	w := test.NewApp().NewWindow("t")
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w, CalMonth: shell.ThisMonth(), RepMonth: shell.ThisMonth()}
	ui.Cfg = backend.Config{Repos: []string{"exampleorg"}}
	w.SetContent(logwork.BuildLogTab(ui))
	return ui
}

// Editing a saved row rewrites it where it stands. Appending instead would
// double-count the minutes and leave the original still waiting to push.
func TestRowEditorSavesInPlace(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()

	made, err := ui.Store.AppendRows([]backend.Row{{
		"date": shell.Today(), "minutes": "90", "type": "commit",
		"description": "half written", "issue": "exampleorg/repo#7",
		"refs": "3613aff", "remarks": "- half written", "mode": "subissue",
	}})
	if err != nil {
		t.Fatal(err)
	}
	row := made[0]

	refreshed, finished := 0, 0
	form := logwork.RowEditor(ui, row, func() { refreshed++ }, func() { finished++ }).All()

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

	rows, err := ui.Store.ReadRows()
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

	made, err := ui.Store.AppendRows([]backend.Row{{
		"date": shell.Today(), "minutes": "60", "type": backend.KindOther,
		"description": "standup", "remarks": "- standup",
	}})
	if err != nil {
		t.Fatal(err)
	}

	finished := 0
	form := logwork.RowEditor(ui, made[0], func() {}, func() { finished++ }).All()
	tapButton(t, form, "Save & push")

	if !contains(labels(form), "no issue ref to push to") {
		t.Fatalf("expected the entry to say why nothing was pushed: %v", labels(form))
	}
	if finished != 0 {
		t.Fatal("the popup closed before the message could be read")
	}
	rows, _ := ui.Store.ReadRows()
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

	made, err := ui.Store.AppendRows([]backend.Row{{
		"date": shell.Today(), "minutes": "90", "issue": "exampleorg/repo#7", "remarks": "- work",
	}})
	if err != nil {
		t.Fatal(err)
	}
	back := widget.NewButton("Back", func() {})
	shell := logwork.PopupShell(back, logwork.RowEditor(ui, made[0], func() {}, func() {}))

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
	shell := logwork.PopupShell(back, logwork.RowEditor(ui,
		backend.Row{"id": "1", "date": shell.Today(), "remarks": "- work"}, func() {}, func() {}))

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

	form := logwork.RowEditor(ui, backend.Row{"id": "1", "date": shell.Today(), "remarks": "- a long line"},
		func() {}, func() {}).All()
	rem := multiLineEntry(form)
	if rem == nil {
		t.Fatal("remarks box is missing from the editor")
	}
	if rem.Wrapping != fyne.TextWrapWord {
		t.Fatalf("remarks should wrap, got %v", rem.Wrapping)
	}
}

func TestSavingDraftShowsLocalOnlyConfirmation(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()

	form := logwork.RowEditor(ui, backend.Row{
		"id": "1", "date": shell.Today(), "minutes": "20", "type": backend.KindOther,
		"issue": "exampleorg/repo#7", "remarks": "- draft",
	}, func() {}, func() {}).All()
	if ui.Win.Canvas().Overlays().Top() != nil {
		t.Fatal("test started with an unexpected dialog")
	}
	tapButton(t, form, "Save")
	if ui.Win.Canvas().Overlays().Top() == nil {
		t.Fatal("saving a draft should open the local-only confirmation")
	}
}

func TestIndependentEditorShowsStandaloneFieldsOnly(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()

	form := logwork.RowEditor(ui, backend.Row{
		"id": "1", "date": shell.Today(), "type": backend.KindIndependent,
		"parent_repo":  "exampleorg/sample-general-tasks",
		"parent_title": "Prepare release notes",
		"refs":         "exampleorg/repo@abc1234",
	}, func() {}, func() {}).All()

	got := labels(form)
	for _, unwanted := range []string{"Push as", "abc1234", "(no issue ref)"} {
		if contains(got, unwanted) {
			t.Fatalf("independent editor should not show %q: %v", unwanted, got)
		}
	}
	for _, want := range []string{"Standalone task", "Repository", "Standalone issue title"} {
		if !contains(got, want) {
			t.Fatalf("independent editor is missing %q: %v", want, got)
		}
	}
	if entryByPlaceholder(form, logwork.IssuePlaceholder) != nil {
		t.Fatal("independent editor should not expose an issue-ref field")
	}
	if entryByPlaceholder(form, "exampleorg/repository") == nil ||
		entryByPlaceholder(form, "Title for the standalone task") == nil {
		t.Fatal("independent editor should edit its destination repository and title")
	}
}

func TestBulkReviewEditorUsesOwnIssueControls(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()

	form := logwork.RowEditor(ui, backend.Row{
		"id": "1", "date": shell.Today(), "type": backend.KindBulkReview,
		"remarks": "https://github.com/exampleorg/repo/pull/7 (20m)",
		"refs":    "exampleorg/repo@abc1234",
	}, func() {}, func() {}).All()
	got := labels(form)
	for _, unwanted := range []string{"Push as", "abc1234", "(no issue ref)"} {
		if contains(got, unwanted) {
			t.Fatalf("bulk review editor should not show %q: %v", unwanted, got)
		}
	}
	if !contains(got, "Bulk review issue") {
		t.Fatalf("bulk review editor should identify its own issue: %v", got)
	}
	if entryByPlaceholder(form, logwork.IssuePlaceholder) != nil {
		t.Fatal("bulk review editor should not expose an issue-ref field")
	}
}

func TestOtherKeepsIssueBackedEditorControls(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()

	form := logwork.RowEditor(ui, backend.Row{
		"id": "1", "date": shell.Today(), "type": backend.KindOther,
		"issue": "exampleorg/repo#8",
	}, func() {}, func() {}).All()
	if !contains(labels(form), "Push as") {
		t.Fatal("Other is visually independent but still needs its Push-as control")
	}
	if entryByPlaceholder(form, logwork.IssuePlaceholder) == nil {
		t.Fatal("Other still points at an existing issue and needs its issue-ref field")
	}
}

// Every row in the waiting list carries an edit button, and it opens a popup.
func TestEntryTableRowOpensTheEditor(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()

	if _, err := ui.Store.AppendRows([]backend.Row{{
		"date": shell.Today(), "minutes": "90", "type": "commit",
		"description": "waiting", "issue": "exampleorg/repo#7",
	}}); err != nil {
		t.Fatal(err)
	}
	logwork.DrawRecent(ui)

	// A waiting entry dated today stands on its day in the week strip; one dated
	// outside the week on show is in the list below. Either way it can be edited.
	var edit *widget.Button
	walk(container.NewVBox(ui.WeekBox, ui.RecentBox), func(c fyne.CanvasObject) {
		if b, ok := c.(*widget.Button); ok && b.Icon == theme.DocumentCreateIcon() {
			edit = b
		}
	})
	if edit == nil {
		t.Fatal("the waiting list offers no way to edit a row")
	}
	edit.OnTapped()
	if ui.Win.Canvas().Overlays().Top() == nil {
		t.Fatal("the edit button opened nothing")
	}
}

// A saved commit row is labelled by its issue, not by the commit subjects
// joined end to end — the remarks already carry the work in full.
func TestRowLabelPrefersTheIssueTitle(t *testing.T) {
	ui := &shell.UI{IssueInfo: map[string]backend.IssueInfo{
		"exampleorg/repo#12": {Org: "exampleorg", Repo: "repo", Number: 12,
			Title: "Datepicker custom field: relative defaults"},
	}}
	commits := []backend.Commit{
		{Sha: "3613aff", Message: "relative date defaults", Full: "relative date defaults\n- resolver"},
		{Sha: "9993d06", Message: "partial-save fix", Full: "partial-save fix\n- error path"},
	}
	if got := logwork.RowLabel(ui, "exampleorg/repo#12", commits); got != "Datepicker custom field: relative defaults" {
		t.Fatalf("expected the issue title, got %q", got)
	}
	// Title not loaded yet: fall back to the commit, and say how many there are
	// so the row is not mistaken for a single one.
	got := logwork.RowLabel(ui, "exampleorg/other#3", commits)
	if !strings.Contains(got, "relative date defaults") || !strings.Contains(got, "2 commits") {
		t.Fatalf("fallback should name the commit and its count, got %q", got)
	}
	if got := logwork.RowLabel(ui, "exampleorg/other#3", commits[:1]); got != "relative date defaults" {
		t.Fatalf("a lone commit should speak for itself, got %q", got)
	}
	if got := logwork.RowLabel(ui, "exampleorg/other#3", nil); got != "exampleorg/other#3" {
		t.Fatalf("with nothing else to say, the ref is the label, got %q", got)
	}
}

// Manual bubbles derive their names from current row state. In particular, a
// drag changes the date only; the title must not keep the date captured in the
// old description.
func TestManualRowTitlesFollowCurrentDateAndIssue(t *testing.T) {
	ui := &shell.UI{
		Cfg: backend.Config{WorklogOwner: "team-alex"},
		IssueInfo: map[string]backend.IssueInfo{
			"exampleorg/repo#7": {Title: "Fix invoice rounding"},
		},
	}
	for _, c := range []struct {
		name string
		row  backend.Row
		want string
	}{
		{"meeting", backend.Row{"type": backend.KindMeeting, "date": "2026-09-18", "description": "old notes"},
			"Alex Meeting & ad hocs: 2026-09-18"},
		{"bulk review", backend.Row{"type": backend.KindBulkReview, "date": "2026-09-18", "description": "Alex Code Review: 2026-09-15"},
			"Alex Code Review: 2026-09-18"},
		{"single review issue title", backend.Row{"type": backend.KindCodeReview, "date": "2026-09-18", "issue": "exampleorg/repo#7", "description": "Code Review: 2026-09-15"},
			"Fix invoice rounding"},
		{"saved issue title", backend.Row{"type": backend.KindCodeReview, "date": "2026-09-18", "issue": "exampleorg/repo#8", "issue_title": "Persisted invoice title"},
			"Persisted invoice title"},
		{"other issue fallback", backend.Row{"type": backend.KindOther, "date": "2026-09-18", "issue": "exampleorg/other#9", "description": "Worklog: 2026-09-15"},
			"exampleorg/other#9"},
	} {
		if got := logwork.RowTitle(ui, c.row); got != c.want {
			t.Fatalf("%s: title = %q, want %q", c.name, got, c.want)
		}
	}
}

// The tile reads from the issue title too, so a row saved before the title
// arrived still shows the issue once it has.
func TestRowTileShowsTheIssueTitle(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	ui.IssueInfo = map[string]backend.IssueInfo{
		"exampleorg/repo#12": {Repo: "repo", Number: 12, Title: "Relative date defaults"},
	}
	tile := logwork.RowTile(ui, backend.Row{
		"id": "1", "date": shell.Today(), "minutes": "90", "type": "commit",
		"issue": "exampleorg/repo#12", "description": "old joined commit subjects",
	}, func() {})
	got := labels(tile)
	if !contains(got, "Relative date defaults") {
		t.Fatalf("tile should carry the issue title: %v", got)
	}
	if contains(got, "old joined commit subjects") {
		t.Fatalf("the stored description should not win over the title: %v", got)
	}
}
