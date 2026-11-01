package tests

import (
	"testing"
	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"

	"fyne.io/fyne/v2/test"
)

// Run: go test -run Render -v
func TestRender(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PANIC in render: %v", r)
		}
	}()
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")

	ui := &shell.UI{Store: backend.NewStore(t.TempDir()), Win: w, CalMonth: shell.ThisMonth(), RepMonth: shell.ThisMonth()}
	ui.Cfg = backend.Config{Repos: []string{"exampleorg"}, WorklogOwner: "team-alex", DefaultMode: "subissue"}
	content := logwork.BuildLogTab(ui)
	w.SetContent(content)

	res := backend.PendingResult{
		Count: 2,
		Range: []string{"2026-07-28", "2026-08-03"},
		Groups: []backend.Group{
			{Date: "2026-07-31", Issue: "exampleorg/sample-shared-utilities#147",
				Commits: []backend.Commit{{Sha: "abc1234", Repo: "exampleorg/sample-shared-utilities", Date: "2026-07-31", Message: "fix thing"}},
				Remarks: "- fix thing (abc1234)"},
			{Date: "2026-07-30", Issue: "",
				Commits: []backend.Commit{{Sha: "def5678", Repo: "exampleorg/other", Date: "2026-07-30", Message: "no issue here"}},
				Remarks: "- no issue here (def5678)"},
		},
	}
	ui.Pending = res
	logwork.RenderPending(ui)
	t.Log("renderPending OK")

	// Bubbles render before the issue titles arrive, then again after — both
	// states must survive, including the group with no issue ref at all.
	ui.IssueInfo = map[string]backend.IssueInfo{
		"exampleorg/sample-shared-utilities#147": {
			Org: "exampleorg", Repo: "sample-shared-utilities", Number: 147,
			Title: "Category dropdown capped at 100 rows",
			URL:   "https://github.com/exampleorg/sample-shared-utilities/issues/147",
		},
	}
	logwork.RenderPending(ui)
	t.Log("renderPending with issue titles OK")

	// The popup builds the full editor; a nil callback must not blow up.
	for _, g := range res.Groups {
		logwork.OpenGroupEditor(ui, g)
	}
	t.Log("openGroupEditor OK")
}

// Logging part of a group must leave the untouched commits pending.
func TestDropLoggedCommitsKeepsUnpicked(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &shell.UI{Store: backend.NewStore(t.TempDir()), Win: w, CalMonth: shell.ThisMonth(), RepMonth: shell.ThisMonth()}
	ui.Cfg = backend.Config{Repos: []string{"exampleorg"}}
	w.SetContent(logwork.BuildLogTab(ui))

	g := backend.Group{Date: "2026-08-06", Issue: "exampleorg/repo#1", Commits: []backend.Commit{
		{Sha: "aaa1111", Repo: "exampleorg/repo"},
		{Sha: "bbb2222", Repo: "exampleorg/repo"},
	}}
	ui.Pending = backend.PendingResult{Count: 2, Range: []string{"2026-08-01", "2026-08-06"}, Groups: []backend.Group{g}}

	logwork.DropLoggedCommits(ui, g, []backend.Commit{{Sha: "aaa1111"}})
	if len(ui.Pending.Groups) != 1 || ui.Pending.Count != 1 {
		t.Fatalf("expected one commit left, got groups=%d count=%d",
			len(ui.Pending.Groups), ui.Pending.Count)
	}
	if ui.Pending.Groups[0].Commits[0].Sha != "bbb2222" {
		t.Fatalf("wrong commit survived: %+v", ui.Pending.Groups[0].Commits)
	}

	// Logging the rest empties the group entirely.
	logwork.DropLoggedCommits(ui, g, []backend.Commit{{Sha: "bbb2222"}})
	if len(ui.Pending.Groups) != 0 || ui.Pending.Count != 0 {
		t.Fatalf("expected the group to disappear, got groups=%d count=%d",
			len(ui.Pending.Groups), ui.Pending.Count)
	}
}
