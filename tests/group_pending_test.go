package tests

import (
	"strings"
	"testing"
	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
)

// Two commits on the same issue and the same day get their own bubble each, so
// every set of remarks carries exactly one sha and one commit's bullets.
func TestEachCommitGetsItsOwnBubble(t *testing.T) {
	commits := []backend.Commit{
		{Sha: "3613aff", Repo: "exampleorg/repo", Date: "2026-08-09", Issue: "exampleorg/repo#12",
			Message: "relative date defaults", Full: "relative date defaults\n- resolver\n- packer"},
		{Sha: "9993d06", Repo: "exampleorg/repo", Date: "2026-08-09", Issue: "exampleorg/repo#12",
			Message: "partial-save fix", Full: "partial-save fix\n- error path dispatches"},
	}
	groups := backend.GroupPending(commits, map[string]bool{}, map[string]bool{})
	if len(groups) != 2 {
		t.Fatalf("expected one bubble per commit, got %d", len(groups))
	}
	for _, g := range groups {
		if len(g.Commits) != 1 {
			t.Fatalf("bubble holds %d commits, want 1", len(g.Commits))
		}
		sha := g.Commits[0].Sha
		if strings.Contains(g.Remarks, sha) {
			t.Fatalf("remarks should carry no sha, got %q", g.Remarks)
		}
		// The other commit's work must not leak into these remarks.
		for _, other := range commits {
			if other.Sha == sha {
				continue
			}
			if strings.Contains(g.Remarks, strings.SplitN(other.Full, "\n- ", 2)[1]) {
				t.Fatalf("%s remarks carry %s's body: %q", sha, other.Sha, g.Remarks)
			}
		}
	}
	if !strings.Contains(groups[0].Remarks+groups[1].Remarks, "error path dispatches") {
		t.Fatal("body bullets were lost when the commits were split")
	}
}

// Already-logged shas, duplicates and merge commits never reach a bubble.
func TestGroupPendingFiltersAndOrders(t *testing.T) {
	commits := []backend.Commit{
		{Sha: "aaa1111", Date: "2026-08-07", Issue: "exampleorg/repo#1", Message: "older"},
		{Sha: "bbb2222", Date: "2026-08-09", Issue: "exampleorg/repo#2", Message: "newer"},
		{Sha: "bbb2222", Date: "2026-08-09", Issue: "exampleorg/repo#2", Message: "dupe"},
		{Sha: "ccc3333", Date: "2026-08-09", Issue: "exampleorg/repo#2", Message: "Merge branch 'main'"},
		{Sha: "ddd4444", Date: "2026-08-08", Issue: "exampleorg/repo#3", Message: "already logged"},
	}
	groups := backend.GroupPending(commits, map[string]bool{"ddd4444": true}, map[string]bool{})

	var shas []string
	for _, g := range groups {
		shas = append(shas, g.Commits[0].Sha)
	}
	want := []string{"bbb2222", "aaa1111"} // newest date first
	if strings.Join(shas, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", shas, want)
	}
}

// Tiles on the same issue stand together under one heading; a tile that is the
// only one on its issue has nothing to be grouped with and drops to the foot.
// Nothing is merged — every commit still has its own tile.
func TestPendingSectionsGroupRepeatIssues(t *testing.T) {
	groups := []backend.Group{
		{Date: "2026-08-11", Issue: "exampleorg/repo#42", Commits: []backend.Commit{{Sha: "aaa1111"}}},
		{Date: "2026-08-11", Issue: "exampleorg/repo#7", Commits: []backend.Commit{{Sha: "bbb2222"}}},
		{Date: "2026-08-10", Issue: "exampleorg/repo#42", Commits: []backend.Commit{{Sha: "ccc3333"}}},
		{Date: "2026-08-09", Commits: []backend.Commit{{Sha: "ddd4444"}}}, // no ref at all
		{Date: "2026-08-09", Issue: "exampleorg/repo#42", Commits: []backend.Commit{{Sha: "eee5555"}}},
	}
	got := logwork.PendingSections(groups)
	if len(got) != 2 {
		t.Fatalf("one heading plus the loose tiles is 2 sections, got %d", len(got))
	}
	if got[0].Issue != "exampleorg/repo#42" {
		t.Fatalf("the repeated issue leads, got %q", got[0].Issue)
	}
	if len(got[0].Groups) != 3 {
		t.Fatalf("all three tiles belong to the heading, got %d", len(got[0].Groups))
	}
	// Newest first inside the section, the order they were handed over in.
	if got[0].Groups[0].Commits[0].Sha != "aaa1111" || got[0].Groups[2].Commits[0].Sha != "eee5555" {
		t.Fatalf("the section reordered its tiles: %v", got[0].Groups)
	}
	if got[1].Issue != "" {
		t.Fatalf("the last section carries no heading, got %q", got[1].Issue)
	}
	if len(got[1].Groups) != 2 {
		t.Fatalf("the lone tile and the refless one fall to the foot, got %d", len(got[1].Groups))
	}

	// Every tile survives, whatever section it lands in.
	total := 0
	for _, s := range got {
		total += len(s.Groups)
	}
	if total != len(groups) {
		t.Fatalf("%d tiles went in, %d came out", len(groups), total)
	}
}

// Nothing repeats: no headings, one plain block of tiles.
func TestPendingSectionsLeaveSinglesAlone(t *testing.T) {
	got := logwork.PendingSections([]backend.Group{
		{Date: "2026-08-11", Issue: "exampleorg/repo#1", Commits: []backend.Commit{{Sha: "aaa1111"}}},
		{Date: "2026-08-10", Issue: "exampleorg/repo#2", Commits: []backend.Commit{{Sha: "bbb2222"}}},
	})
	if len(got) != 1 || got[0].Issue != "" || len(got[0].Groups) != 2 {
		t.Fatalf("expected one unheaded section of 2, got %+v", got)
	}
}

// commitSubject prefers the first real body line: a "Refs …" subject only
// repeats the issue the tile already names.
func TestCommitSubjectSkipsRefsHeader(t *testing.T) {
	c := backend.Commit{
		Sha:     "05c9cae",
		Repo:    "exampleorg/repo",
		Message: "Refs exampleorg/repo/issues/483:",
		Full:    "Refs exampleorg/repo/issues/483:\n- Cap fix: limit null to ROW_LIMIT = 9999.",
	}
	if got := backend.CommitSubject(c); !strings.Contains(got, "ROW_LIMIT = 9999") {
		t.Fatalf("expected the body line, got %q", got)
	}
	plain := backend.Commit{Sha: "9f8a180", Message: "sync screen script", Full: "sync screen script"}
	if got := backend.CommitSubject(plain); got != "sync screen script" {
		t.Fatalf("plain subject should pass through, got %q", got)
	}
	if got := backend.CommitSubject(backend.Commit{Sha: "deadbee"}); got != "Commit deadbee" {
		t.Fatalf("empty message needs a fallback, got %q", got)
	}
}
