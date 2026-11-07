package tests

import (
	"errors"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func draftSplitCheck(form fyne.CanvasObject) *widget.Check {
	var check *widget.Check
	walk(form, func(o fyne.CanvasObject) {
		if feedback, ok := o.(*widgets.ChoiceFeedback); ok {
			if c, ok := feedback.Content.(*widget.Check); ok && c.Text == "Separate worklog remarks" {
				check = c
			}
		}
	})
	return check
}

func TestDraftEditorDescriptionUsesIssueTitle(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	ui.IssueInfo = map[string]backend.IssueInfo{"exampleorg/repo#7": {Title: "Current issue title"}}
	for _, tc := range []struct {
		name, issue, savedTitle, description, want string
	}{
		{"loaded", "exampleorg/repo#7", "Old title", "old commit subjects", "Current issue title"},
		{"persisted", "exampleorg/repo#8", "Saved issue title", "", "Saved issue title"},
		{"unavailable", "exampleorg/repo#9", "", "", "exampleorg/repo#9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := logwork.RowEditor(ui, backend.Row{
				"date": shell.Today(), "type": backend.KindOther, "issue": tc.issue,
				"issue_title": tc.savedTitle, "description": tc.description, "remarks": "- actual work",
			}, func() {}, nil).All()
			var description *widget.Entry
			walk(form, func(o fyne.CanvasObject) {
				if box, ok := o.(*fyne.Container); ok && len(box.Objects) == 2 {
					if label, ok := box.Objects[0].(*widget.Label); ok && label.Text == "Description (kept locally, not pushed)" {
						description, _ = box.Objects[1].(*widget.Entry)
					}
				}
			})
			if description == nil || description.Text != tc.want {
				t.Fatalf("description did not show %q", tc.want)
			}
		})
	}
}

func TestDraftOwnIssueCreationUsesFullSplitBody(t *testing.T) {
	previous := backend.Gh
	t.Cleanup(func() { backend.Gh = previous })
	cfg := backend.Config{WorklogOwner: "team-alex", MeetingRepo: "exampleorg/repo", BulkCodeReviewRepo: "exampleorg/repo"}
	for _, kind := range []string{backend.KindMeeting, backend.KindBulkReview, backend.KindIndependent} {
		for _, tc := range []struct {
			name, body, remarks, description, want string
		}{
			{"split", strings.Repeat("full writeup ", 120), "- brief", "Issue title", strings.Repeat("full writeup ", 120)},
			{"unsplit", "", "- full remarks", "Issue title", "- full remarks"},
			{"legacy", "", "", "legacy work", "legacy work"},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				var createdBody string
				stopped := errors.New("stopped before creation")
				backend.Gh = func(args []string, _ ...string) (string, error) {
					if len(args) >= 2 && args[0] == "issue" && args[1] == "list" {
						return "[]", nil
					}
					if len(args) >= 2 && args[0] == "issue" && args[1] == "create" {
						for i, arg := range args {
							if arg == "--body" && i+1 < len(args) {
								createdBody = args[i+1]
							}
						}
						return "", stopped
					}
					t.Fatalf("unexpected GitHub call: %v", args)
					return "", stopped
				}
				_, err := backend.EnsureIssueRef(cfg, backend.Row{
					"type": kind, "date": "2026-11-06", "parent_repo": "exampleorg/repo", "parent_title": "Task",
					"issue_body": tc.body, "remarks": tc.remarks, "description": tc.description,
				})
				if !errors.Is(err, stopped) || createdBody != strings.TrimSpace(tc.want) {
					t.Fatalf("issue creation body = %q, err = %v", createdBody, err)
				}
			})
		}
	}
}

func TestDraftEditorEveryKindCanSplitAndReopen(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	for _, kind := range []string{backend.KindCommit, backend.KindOther, backend.KindCodeReview,
		backend.KindMeeting, backend.KindBulkReview, backend.KindIndependent} {
		t.Run(kind, func(t *testing.T) {
			made, err := ui.Store.AppendRows([]backend.Row{{
				"date": shell.Today(), "type": kind, "issue": "exampleorg/repo#7",
				"remarks": "- original work", "description": "old local description",
			}})
			if err != nil {
				t.Fatal(err)
			}
			form := logwork.RowEditor(ui, made[0], func() {}, nil).All()
			check := draftSplitCheck(form)
			if check == nil {
				t.Fatal("draft has no split checkbox")
			}
			entry := multiLineEntry(form)
			entry.SetText(strings.Repeat("x", backend.RemarkCap-1))
			if check.Visible() {
				t.Fatal("split offered below the cap")
			}
			entry.SetText(strings.Repeat("x", backend.RemarkCap))
			if !check.Visible() {
				t.Fatal("split missing at exactly 1024")
			}
			long := strings.Repeat("- completed some work\n", 70)
			entry.SetText(long)
			check.SetChecked(true)
			tapButton(t, form, "Save")
			rows, err := ui.Store.ReadRows()
			if err != nil {
				t.Fatal(err)
			}
			var saved backend.Row
			for _, row := range rows {
				if row["id"] == made[0]["id"] {
					saved = row
				}
			}
			if saved["issue_body"] != strings.TrimSpace(long) || saved["remarks"] == "" || len(saved["remarks"]) > backend.RemarkCap {
				t.Fatalf("split texts not saved intact: body=%d, remarks=%d", len(saved["issue_body"]), len(saved["remarks"]))
			}
			reopened := logwork.RowEditor(ui, saved, func() {}, nil).All()
			if check := draftSplitCheck(reopened); check == nil || !check.Checked || !check.Visible() {
				t.Fatal("saved draft did not reopen split")
			}
		})
	}
}

func TestDraftEditorPreservesLegacyDescriptionAsRemarks(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	for _, kind := range []string{backend.KindCommit, backend.KindOther} {
		form := logwork.RowEditor(ui, backend.Row{
			"date": shell.Today(), "type": kind, "description": "legacy work details",
			"issue_title": "Issue title", "issue": "exampleorg/repo#7",
		}, func() {}, nil).All()
		if got := multiLineEntry(form).Text; got != "legacy work details" {
			t.Fatalf("%s lost legacy details: %q", kind, got)
		}
	}
}
