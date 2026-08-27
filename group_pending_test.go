package main

import (
	"strings"
	"testing"
)

// Two commits on the same issue and the same day get their own bubble each, so
// every set of remarks carries exactly one sha and one commit's bullets.
func TestEachCommitGetsItsOwnBubble(t *testing.T) {
	commits := []Commit{
		{Sha: "3613aff", Repo: "bigledger/repo", Date: "2026-08-09", Issue: "bigledger/repo#12",
			Message: "relative date defaults", Full: "relative date defaults\n- resolver\n- packer"},
		{Sha: "9993d06", Repo: "bigledger/repo", Date: "2026-08-09", Issue: "bigledger/repo#12",
			Message: "partial-save fix", Full: "partial-save fix\n- error path dispatches"},
	}
	groups := groupPending(commits, map[string]bool{}, map[string]bool{})
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
	commits := []Commit{
		{Sha: "aaa1111", Date: "2026-08-07", Issue: "bigledger/repo#1", Message: "older"},
		{Sha: "bbb2222", Date: "2026-08-09", Issue: "bigledger/repo#2", Message: "newer"},
		{Sha: "bbb2222", Date: "2026-08-09", Issue: "bigledger/repo#2", Message: "dupe"},
		{Sha: "ccc3333", Date: "2026-08-09", Issue: "bigledger/repo#2", Message: "Merge branch 'main'"},
		{Sha: "ddd4444", Date: "2026-08-08", Issue: "bigledger/repo#3", Message: "already logged"},
	}
	groups := groupPending(commits, map[string]bool{"ddd4444": true}, map[string]bool{})

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
	groups := []Group{
		{Date: "2026-08-11", Issue: "bigledger/repo#42", Commits: []Commit{{Sha: "aaa1111"}}},
		{Date: "2026-08-11", Issue: "bigledger/repo#7", Commits: []Commit{{Sha: "bbb2222"}}},
		{Date: "2026-08-10", Issue: "bigledger/repo#42", Commits: []Commit{{Sha: "ccc3333"}}},
		{Date: "2026-08-09", Commits: []Commit{{Sha: "ddd4444"}}}, // no ref at all
		{Date: "2026-08-09", Issue: "bigledger/repo#42", Commits: []Commit{{Sha: "eee5555"}}},
	}
	got := pendingSections(groups)
	if len(got) != 2 {
		t.Fatalf("one heading plus the loose tiles is 2 sections, got %d", len(got))
	}
	if got[0].issue != "bigledger/repo#42" {
		t.Fatalf("the repeated issue leads, got %q", got[0].issue)
	}
	if len(got[0].groups) != 3 {
		t.Fatalf("all three tiles belong to the heading, got %d", len(got[0].groups))
	}
	// Newest first inside the section, the order they were handed over in.
	if got[0].groups[0].Commits[0].Sha != "aaa1111" || got[0].groups[2].Commits[0].Sha != "eee5555" {
		t.Fatalf("the section reordered its tiles: %v", got[0].groups)
	}
	if got[1].issue != "" {
		t.Fatalf("the last section carries no heading, got %q", got[1].issue)
	}
	if len(got[1].groups) != 2 {
		t.Fatalf("the lone tile and the refless one fall to the foot, got %d", len(got[1].groups))
	}

	// Every tile survives, whatever section it lands in.
	total := 0
	for _, s := range got {
		total += len(s.groups)
	}
	if total != len(groups) {
		t.Fatalf("%d tiles went in, %d came out", len(groups), total)
	}
}

// Nothing repeats: no headings, one plain block of tiles.
func TestPendingSectionsLeaveSinglesAlone(t *testing.T) {
	got := pendingSections([]Group{
		{Date: "2026-08-11", Issue: "bigledger/repo#1", Commits: []Commit{{Sha: "aaa1111"}}},
		{Date: "2026-08-10", Issue: "bigledger/repo#2", Commits: []Commit{{Sha: "bbb2222"}}},
	})
	if len(got) != 1 || got[0].issue != "" || len(got[0].groups) != 2 {
		t.Fatalf("expected one unheaded section of 2, got %+v", got)
	}
}

// commitSubject prefers the first real body line: a "Refs …" subject only
// repeats the issue the tile already names.
func TestCommitSubjectSkipsRefsHeader(t *testing.T) {
	c := Commit{
		Sha:     "05c9cae",
		Repo:    "bigledger/repo",
		Message: "Refs bigledger/repo/issues/483:",
		Full:    "Refs bigledger/repo/issues/483:\n- Cap fix: limit null to ROW_LIMIT = 9999.",
	}
	if got := commitSubject(c); !strings.Contains(got, "ROW_LIMIT = 9999") {
		t.Fatalf("expected the body line, got %q", got)
	}
	plain := Commit{Sha: "9f8a180", Message: "sync screen script", Full: "sync screen script"}
	if got := commitSubject(plain); got != "sync screen script" {
		t.Fatalf("plain subject should pass through, got %q", got)
	}
	if got := commitSubject(Commit{Sha: "deadbee"}); got != "Commit deadbee" {
		t.Fatalf("empty message needs a fallback, got %q", got)
	}
}
