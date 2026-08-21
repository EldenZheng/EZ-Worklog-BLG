package main

import (
	"testing"

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

	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: thisMonth(), repMonth: thisMonth()}
	ui.cfg = Config{Repos: []string{"bigledger"}, WorklogOwner: "blg-elden", DefaultMode: "subissue"}
	content := ui.buildLogTab()
	w.SetContent(content)

	res := PendingResult{
		Count: 2,
		Range: []string{"2026-07-28", "2026-08-03"},
		Groups: []Group{
			{Date: "2026-07-31", Issue: "bigledger/blg-shared-utilities#147",
				Commits: []Commit{{Sha: "abc1234", Repo: "bigledger/blg-shared-utilities", Date: "2026-07-31", Message: "fix thing"}},
				Remarks: "- fix thing (abc1234)"},
			{Date: "2026-07-30", Issue: "",
				Commits: []Commit{{Sha: "def5678", Repo: "bigledger/other", Date: "2026-07-30", Message: "no issue here"}},
				Remarks: "- no issue here (def5678)"},
		},
	}
	ui.pending = res
	ui.renderPending()
	t.Log("renderPending OK")

	// Bubbles render before the issue titles arrive, then again after — both
	// states must survive, including the group with no issue ref at all.
	ui.issueInfo = map[string]IssueInfo{
		"bigledger/blg-shared-utilities#147": {
			Org: "bigledger", Repo: "blg-shared-utilities", Number: 147,
			Title: "Category dropdown capped at 100 rows",
			URL:   "https://github.com/bigledger/blg-shared-utilities/issues/147",
		},
	}
	ui.renderPending()
	t.Log("renderPending with issue titles OK")

	// The popup builds the full editor; a nil callback must not blow up.
	for _, g := range res.Groups {
		ui.openGroupEditor(g)
	}
	t.Log("openGroupEditor OK")
}

// Logging part of a group must leave the untouched commits pending.
func TestDropLoggedCommitsKeepsUnpicked(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: thisMonth(), repMonth: thisMonth()}
	ui.cfg = Config{Repos: []string{"bigledger"}}
	w.SetContent(ui.buildLogTab())

	g := Group{Date: "2026-08-06", Issue: "bigledger/repo#1", Commits: []Commit{
		{Sha: "aaa1111", Repo: "bigledger/repo"},
		{Sha: "bbb2222", Repo: "bigledger/repo"},
	}}
	ui.pending = PendingResult{Count: 2, Range: []string{"2026-08-01", "2026-08-06"}, Groups: []Group{g}}

	ui.dropLoggedCommits(g, []Commit{{Sha: "aaa1111"}})
	if len(ui.pending.Groups) != 1 || ui.pending.Count != 1 {
		t.Fatalf("expected one commit left, got groups=%d count=%d",
			len(ui.pending.Groups), ui.pending.Count)
	}
	if ui.pending.Groups[0].Commits[0].Sha != "bbb2222" {
		t.Fatalf("wrong commit survived: %+v", ui.pending.Groups[0].Commits)
	}

	// Logging the rest empties the group entirely.
	ui.dropLoggedCommits(g, []Commit{{Sha: "bbb2222"}})
	if len(ui.pending.Groups) != 0 || ui.pending.Count != 0 {
		t.Fatalf("expected the group to disappear, got groups=%d count=%d",
			len(ui.pending.Groups), ui.pending.Count)
	}
}
