package main

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// flowGrids collects every wrapping grid in a tree, in order: the pending list
// draws the live commits in one and the ignored ones in another.
func flowGrids(o fyne.CanvasObject) []*fyne.Container {
	var out []*fyne.Container
	c, ok := o.(*fyne.Container)
	if !ok {
		return nil
	}
	if _, ok := c.Layout.(*flowGrid); ok {
		return []*fyne.Container{c}
	}
	for _, child := range c.Objects {
		out = append(out, flowGrids(child)...)
	}
	return out
}

func pendingUI(t *testing.T) *UI {
	t.Helper()
	w := test.NewApp().NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: thisMonth(), repMonth: thisMonth()}
	ui.cfg = Config{Repos: []string{"bigledger"}}
	w.SetContent(ui.buildLogTab())
	return ui
}

// A tile is named after the issue it is filed against, not after the commit
// that happened to reach it. The commit line stays in the footer, where it
// still tells two tiles on the same issue apart.
func TestPendingBubbleLeadsWithTheIssueTitle(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()

	g := Group{Date: "2026-08-10", Issue: "bigledger/blg-intranet#42",
		Commits: []Commit{{Sha: "abc1234", Repo: "bigledger/blg-intranet",
			Message: "cap the dropdown", Full: "cap the dropdown"}}}
	ui.issueInfo = map[string]IssueInfo{
		"bigledger/blg-intranet#42": {Org: "bigledger", Repo: "blg-intranet", Number: 42,
			Title: "Category dropdown capped at 100 rows"},
	}

	got := labels(ui.groupBubble(g, false))
	if !contains(got, "Category dropdown capped at 100 rows") {
		t.Fatalf("the heading should be the issue title: %v", got)
	}
	if !contains(got, "cap the dropdown") {
		t.Fatalf("the commit line should still be on the tile: %v", got)
	}

	// Title not loaded yet: the ref stands in, rather than the commit taking
	// the heading and then being replaced a moment later.
	ui.issueInfo = map[string]IssueInfo{}
	got = labels(ui.groupBubble(g, false))
	if !contains(got, "bigledger/blg-intranet#42") {
		t.Fatalf("with no title the ref should lead: %v", got)
	}

	// No issue at all: the commit is the only thing that can name the tile.
	bare := Group{Date: "2026-08-10", Commits: []Commit{
		{Sha: "def5678", Repo: "bigledger/other", Message: "tidy imports", Full: "tidy imports"}}}
	if got = labels(ui.groupBubble(bare, false)); !contains(got, "tidy imports") {
		t.Fatalf("a commit with no issue must still name itself: %v", got)
	}
}

// Ignoring a commit takes it out of the list without logging minutes against
// it, and it can be handed back.
func TestIgnoreAndRestoreAPendingCommit(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()

	ui.pending = PendingResult{
		Count: 2, Range: []string{"2026-08-04", "2026-08-10"},
		Groups: []Group{
			{Date: "2026-08-10", Issue: "bigledger/repo#1",
				Commits: []Commit{{Sha: "aaa1111", Repo: "bigledger/repo", Message: "keep me"}}},
			{Date: "2026-08-09", Issue: "bigledger/repo#2",
				Commits: []Commit{{Sha: "bbb2222", Repo: "bigledger/repo", Message: "skip me"}}},
		},
	}
	ui.renderPending()

	grids := flowGrids(ui.pendingBox)
	if len(grids) != 1 || len(grids[0].Objects) != 2 {
		t.Fatalf("expected one grid of two tiles, got %d grid(s)", len(grids))
	}

	ui.setIgnored(ui.pending.Groups[1], true)

	if !contains(labels(ui.pendingBox), "1 unlogged commit(s) from 2026-08-04 to 2026-08-10") {
		t.Fatalf("the count should drop with the tile: %v", labels(ui.pendingBox))
	}
	if ui.pending.Count != 1 {
		t.Fatalf("cached count should be 1, got %d", ui.pending.Count)
	}
	grids = flowGrids(ui.pendingBox)
	if len(grids) != 1 || len(grids[0].Objects) != 1 {
		t.Fatalf("the ignored tile should be out of the list, got %d tile(s)", len(grids[0].Objects))
	}
	if !contains(labels(ui.pendingBox), "Show 1 ignored") {
		t.Fatalf("an ignored commit must stay reachable: %v", labels(ui.pendingBox))
	}

	// It survives a refetch: the sha is on disk, not only in this session.
	skip, err := ui.store.IgnoredShas()
	if err != nil {
		t.Fatal(err)
	}
	if !skip["bbb2222"] || skip["aaa1111"] {
		t.Fatalf("wrong shas ignored: %v", skip)
	}

	// Opening the section shows it with a Restore button, and restoring puts it
	// back in the list and back on disk.
	tapButton(t, ui.pendingBox, "Show 1 ignored")
	if grids = flowGrids(ui.pendingBox); len(grids) != 2 {
		t.Fatalf("the ignored tiles need their own grid, got %d", len(grids))
	}
	tapButton(t, grids[1], "Restore")

	if grids = flowGrids(ui.pendingBox); len(grids) != 1 || len(grids[0].Objects) != 2 {
		t.Fatalf("restoring should return the tile to the list: %v", labels(ui.pendingBox))
	}
	if skip, _ = ui.store.IgnoredShas(); skip["bbb2222"] {
		t.Fatal("restoring should clear the sha from the ignore file")
	}
	if strings.Contains(strings.Join(labels(ui.pendingBox), " "), "ignored") {
		t.Fatalf("with nothing ignored the toggle should go: %v", labels(ui.pendingBox))
	}
}

// Every commit ignored is still fetched and grouped — marked, not dropped — so
// the list can offer it back without another round trip to GitHub.
func TestGroupPendingMarksIgnoredWithoutDropping(t *testing.T) {
	commits := []Commit{
		{Sha: "aaa1111", Date: "2026-08-09", Issue: "bigledger/repo#1", Message: "keep"},
		{Sha: "bbb2222", Date: "2026-08-09", Issue: "bigledger/repo#2", Message: "skip"},
	}
	groups := groupPending(commits, map[string]bool{}, map[string]bool{"bbb2222": true})
	if len(groups) != 2 {
		t.Fatalf("an ignored commit must still be carried, got %d group(s)", len(groups))
	}
	for _, g := range groups {
		want := g.Commits[0].Sha == "bbb2222"
		if g.Ignored != want {
			t.Fatalf("%s ignored=%v, want %v", g.Commits[0].Sha, g.Ignored, want)
		}
	}
}

// The ignore set is its own file: there is no worklog row to hang a skipped
// commit off, and no minutes to invent for one.
func TestIgnoredShasRoundTrip(t *testing.T) {
	s := newStore(t.TempDir())

	set, err := s.IgnoredShas()
	if err != nil || len(set) != 0 {
		t.Fatalf("a fresh store ignores nothing: %v, %v", set, err)
	}
	for _, sha := range []string{"bbb2222", "aaa1111", "aaa1111"} {
		if err := s.SetIgnored(sha, true); err != nil {
			t.Fatal(err)
		}
	}
	if set, err = s.IgnoredShas(); err != nil || len(set) != 2 {
		t.Fatalf("expected two shas, got %v (%v)", set, err)
	}
	if err := s.SetIgnored("aaa1111", false); err != nil {
		t.Fatal(err)
	}
	if set, _ = s.IgnoredShas(); set["aaa1111"] || !set["bbb2222"] {
		t.Fatalf("un-ignoring took the wrong sha: %v", set)
	}
	// Un-ignoring something that was never ignored is a no-op, not an error.
	if err := s.SetIgnored("ccc3333", false); err != nil {
		t.Fatal(err)
	}
}
