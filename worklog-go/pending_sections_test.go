package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// separators counts the rules drawn in a tree — one between each section.
func separators(o fyne.CanvasObject) int {
	n := 0
	walk(o, func(c fyne.CanvasObject) {
		if _, ok := c.(*widget.Separator); ok {
			n++
		}
	})
	return n
}

// Several commits on one issue are drawn as their own block: the issue title
// once above them, the tiles under it, and a rule before whatever follows. The
// tiles are not merged — three commits are still three tiles, each with its own
// remarks and its own minutes.
func TestPendingListGroupsTilesUnderTheIssue(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()

	const issue = "bigledger/blg-intranet#42"
	ui.issueInfo = map[string]IssueInfo{
		issue: {Org: "bigledger", Repo: "blg-intranet", Number: 42,
			Title: "DOC ITEM ENHANCEMENT"},
		"bigledger/blg-intranet#9": {Org: "bigledger", Repo: "blg-intranet", Number: 9,
			Title: "Lone piece of work"},
	}
	commit := func(sha, msg string) []Commit {
		return []Commit{{Sha: sha, Repo: "bigledger/blg-intranet", Message: msg, Full: msg}}
	}
	ui.pending = PendingResult{Count: 4, Groups: []Group{
		{Date: "2026-08-11", Issue: issue, Commits: commit("aaa1111", "cap the dropdown")},
		{Date: "2026-08-10", Issue: issue, Commits: commit("bbb2222", "widen the picker")},
		{Date: "2026-08-09", Issue: issue, Commits: commit("ccc3333", "fix the tooltip")},
		{Date: "2026-08-09", Issue: "bigledger/blg-intranet#9",
			Commits: commit("ddd4444", "tidy imports")},
	}}
	ui.renderPending()

	got := labels(ui.pendingBox)
	count := 0
	for _, l := range got {
		if l == "DOC ITEM ENHANCEMENT" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the issue title should be the heading and nothing else, found %d: %v", count, got)
	}
	// Under a heading the commit is what tells the tiles apart, so each one
	// leads with its own subject.
	for _, want := range []string{"cap the dropdown", "widen the picker", "fix the tooltip"} {
		if !contains(got, want) {
			t.Fatalf("tile %q is missing from the section: %v", want, got)
		}
	}
	if !contains(got, "3 commits") {
		t.Fatalf("the heading should tally its tiles: %v", got)
	}

	// The lone tile keeps the issue title it is filed against, since no heading
	// above it is saying so — but the block it sits in is named, so it does not
	// read as more of the section above.
	if !contains(got, "Lone piece of work") {
		t.Fatalf("an unheaded tile still names its issue: %v", got)
	}
	if !contains(got, "Other commits") {
		t.Fatalf("the loose tiles need a heading of their own: %v", got)
	}

	// Two blocks: the headed issue and the loose tiles, one rule between them.
	if n := len(flowGrids(ui.pendingBox)); n != 2 {
		t.Fatalf("expected a grid per section, got %d", n)
	}
	if n := separators(ui.pendingBox); n != 1 {
		t.Fatalf("expected one rule between the two sections, got %d", n)
	}
}

// Nothing shares an issue: no headings at all, not even "Other commits" — with
// one section there is no other for it to be other than.
func TestPendingListSkipsHeadingsWithNothingToGroup(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()

	ui.pending = PendingResult{Count: 2, Groups: []Group{
		{Date: "2026-08-11", Issue: "bigledger/repo#1",
			Commits: []Commit{{Sha: "aaa1111", Message: "one", Full: "one"}}},
		{Date: "2026-08-10", Issue: "bigledger/repo#2",
			Commits: []Commit{{Sha: "bbb2222", Message: "two", Full: "two"}}},
	}}
	ui.renderPending()

	if n := separators(ui.pendingBox); n != 0 {
		t.Fatalf("a list with nothing to group needs no rules, got %d", n)
	}
	if got := labels(ui.pendingBox); contains(got, "Other commits") {
		t.Fatalf("the only section should carry no heading: %v", got)
	}
	if n := len(flowGrids(ui.pendingBox)); n != 1 {
		t.Fatalf("expected a single block of tiles, got %d", n)
	}
	// With no heading above them the tiles are named after their issue refs.
	if got := labels(ui.pendingBox); !contains(got, "bigledger/repo#1") {
		t.Fatalf("an unheaded tile should still name its issue: %v", got)
	}
}
