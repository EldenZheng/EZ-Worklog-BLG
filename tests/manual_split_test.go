package tests

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

const manualRemarksPlaceholder = "- one bullet per thing done; this becomes the worklog remarks"

func manualKindPicker(t *testing.T, tab fyne.CanvasObject) *shell.UnderlineTabs {
	t.Helper()
	var kinds *shell.UnderlineTabs
	walk(tab, func(o fyne.CanvasObject) {
		if g, ok := o.(*shell.UnderlineTabs); ok && len(g.Options) > 0 && g.Options[0] == "Commits" {
			kinds = g
		}
	})
	if kinds == nil {
		t.Fatal("missing Log Work kind selector")
	}
	return kinds
}

// Every manual kind with a remarks box offers the popup's split from the cap
// up, and a split entry saves its full text as the issue body.
func TestManualFormSplitsLongRemarks(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	ui.IssueInfo = map[string]backend.IssueInfo{"exampleorg/repo#7": {Title: "Known issue"}}
	tab := logwork.BuildLogTab(ui)
	kinds := manualKindPicker(t, tab)

	for _, tc := range []struct {
		pick, wantKind string
		fill           func()
	}{
		{"Worklog", backend.KindOther, func() {
			entryByPlaceholder(tab, logwork.IssuePlaceholder).SetText("exampleorg/repo#7")
		}},
		{"Meeting", backend.KindMeeting, func() {
			entryByPlaceholder(tab, "Meeting title (e.g. Sprint planning)").SetText("Standup")
		}},
		{"Code Review", backend.KindCodeReview, nil},
		{"Independent", backend.KindIndependent, nil},
	} {
		t.Run(tc.pick, func(t *testing.T) {
			kinds.SetSelected(tc.pick)
			entry := entryByPlaceholder(tab, manualRemarksPlaceholder)
			check := draftSplitCheck(tab)
			if entry == nil || check == nil {
				t.Fatal("form has no remarks box or split tick")
			}
			entry.SetText(strings.Repeat("x", backend.RemarkCap-1))
			if check.Visible() {
				t.Fatal("split offered below the cap")
			}
			entry.SetText(strings.Repeat("x", backend.RemarkCap))
			if !check.Visible() {
				t.Fatal("split missing at exactly 1024")
			}
			if tc.fill == nil {
				return // saving needs GitHub for these; the offer is what is checked
			}
			tc.fill()
			entryByPlaceholder(tab, "minutes").SetText("30")

			long := strings.Repeat("- completed some work\n", 70)
			entry.SetText(long)
			before, err := ui.Store.ReadRows()
			if err != nil {
				t.Fatal(err)
			}
			tapButton(t, tab, "Save draft")
			if rows, _ := ui.Store.ReadRows(); len(rows) != len(before) {
				t.Fatal("over-cap remarks saved without the split")
			}
			check.SetChecked(true)
			const summary = "- completed the work"
			splitRemarksEntry(t, tab).SetText(summary)
			tapButton(t, tab, "Save draft")
			rows, err := ui.Store.ReadRows()
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != len(before)+1 {
				t.Fatalf("rows = %d, want %d", len(rows), len(before)+1)
			}
			var saved backend.Row
			for _, row := range rows {
				if row["type"] == tc.wantKind {
					saved = row
				}
			}
			if saved["issue_body"] != strings.TrimSpace(long) || saved["remarks"] != summary {
				t.Fatalf("split texts not saved intact: body=%d, remarks=%d", len(saved["issue_body"]), len(saved["remarks"]))
			}
			if check.Checked || check.Visible() || entry.Text != "" {
				t.Fatal("form did not go back to one empty box after saving")
			}
		})
	}
}

// Under the cap a manual entry stays one text, and Bulk Review, which has no
// remarks box, never picks up what was left in it.
func TestManualFormWithoutSplitStoresNoBody(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	ui.IssueInfo = map[string]backend.IssueInfo{"exampleorg/repo#7": {Title: "Known issue"}}
	tab := logwork.BuildLogTab(ui)
	kinds := manualKindPicker(t, tab)

	kinds.SetSelected("Worklog")
	entryByPlaceholder(tab, logwork.IssuePlaceholder).SetText("exampleorg/repo#7")
	entryByPlaceholder(tab, "minutes").SetText("15")
	entryByPlaceholder(tab, manualRemarksPlaceholder).SetText("- short")
	tapButton(t, tab, "Save draft")
	rows, err := ui.Store.ReadRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["remarks"] != "- short" || rows[0]["issue_body"] != "" {
		t.Fatalf("rows = %v", rows)
	}

	entryByPlaceholder(tab, manualRemarksPlaceholder).SetText(strings.Repeat("- left over\n", 120))
	draftSplitCheck(tab).SetChecked(true)
	kinds.SetSelected("Bulk Review")
	entryByPlaceholder(tab, logwork.PullRequestPlaceholder).SetText("https://github.com/exampleorg/repo/pull/1")
	entryByPlaceholder(tab, "10").SetText("20")
	entryByPlaceholder(tab, "- what you checked on this PR, a point per line").SetText("- checked the paging")
	tapButton(t, tab, "Save draft")
	rows, err = ui.Store.ReadRows()
	if err != nil {
		t.Fatal(err)
	}
	var bulk backend.Row
	for _, row := range rows {
		if row["type"] == backend.KindBulkReview {
			bulk = row
		}
	}
	// The PR's own remarks go in the issue body; the board keeps the bare list.
	if bulk == nil || bulk["issue_body"] != "https://github.com/exampleorg/repo/pull/1 (20m)\n- checked the paging" ||
		bulk["remarks"] != "https://github.com/exampleorg/repo/pull/1 (20m)" {
		t.Fatalf("bulk row = %v", bulk)
	}
}

// A saved bulk review opens on the same sheet as the Bulk Review form, and
// saving it writes the PR list and the points back the same way.
func TestSavedBulkReviewEditsOnTheSheet(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	made, err := ui.Store.AppendRows([]backend.Row{{
		"date": shell.Today(), "minutes": "25", "type": backend.KindBulkReview, "mode": "issue",
		"remarks":    "https://github.com/o/r/pull/1 (10m)\nhttps://github.com/o/r/pull/2 (15m)",
		"issue_body": "https://github.com/o/r/pull/1 (10m)\n- Approved, no blocking issues\n\nhttps://github.com/o/r/pull/2 (15m)\n- Checked the paging",
	}})
	if err != nil {
		t.Fatal(err)
	}
	form := logwork.RowEditor(ui, made[0], func() {}, nil).All()
	var prs, notes []*widget.Entry
	walk(form, func(o fyne.CanvasObject) {
		if e, ok := o.(*widget.Entry); ok {
			switch e.PlaceHolder {
			case logwork.PullRequestPlaceholder:
				prs = append(prs, e)
			case "- what you checked on this PR, a point per line":
				notes = append(notes, e)
			}
		}
	})
	if len(prs) != 2 || prs[1].Text != "https://github.com/o/r/pull/2" || notes[1].Text != "- Checked the paging" {
		t.Fatalf("sheet rows = %d, notes %v", len(prs), notes)
	}
	if !contains(labels(form), "Total: 25 min") {
		t.Fatalf("total missing: %v", labels(form))
	}
	notes[1].SetText("- Checked the paging\n- Approved, no blocking issues")
	tapButton(t, form, "Save")
	rows, _ := ui.Store.ReadRows()
	if rows[0]["remarks"] != "https://github.com/o/r/pull/1 (10m)\nhttps://github.com/o/r/pull/2 (15m)" ||
		!strings.HasSuffix(rows[0]["issue_body"], "(15m)\n- Checked the paging\n- Approved, no blocking issues") || rows[0]["minutes"] != "25" {
		t.Fatalf("saved = %q / %q / %q", rows[0]["remarks"], rows[0]["issue_body"], rows[0]["minutes"])
	}
}
