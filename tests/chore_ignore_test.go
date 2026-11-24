package tests

import (
	"testing"

	"worklog/internal/shared/backend"
)

// A chore commit starts out ignored; one brought back stays back.
func TestChoreCommitsStartIgnored(t *testing.T) {
	for msg, want := range map[string]bool{
		"chore(ng21): 21:core-cli":                          true,
		"chore: bump deps":                                  true,
		"Chore!: drop node 18":                              true,
		"Refs o/r/issues/5:\nchore(ng19): 19:phone-input":   true,
		"Refs o/r/issues/5: chore(ng19): 19:phone-input":    true,
		"Refs o/r/issues/5:\n- Fixed the chore list paging": false,
		"choreography screen":                               false,
		"fix: paging":                                       false,
	} {
		if got := backend.IsChoreCommit(backend.Commit{Full: msg}); got != want {
			t.Errorf("IsChoreCommit(%q) = %v", msg, got)
		}
	}

	chore := backend.Commit{Sha: "aaa1111", Date: "2026-11-24", Issue: "o/r#5", Full: "chore(ng21): 21:core-cli"}
	work := backend.Commit{Sha: "bbb2222", Date: "2026-11-24", Issue: "o/r#5", Full: "fix: paging"}
	ignoredOf := func(set map[string]bool) map[string]bool {
		got := map[string]bool{}
		for _, g := range backend.GroupPending([]backend.Commit{chore, work}, nil, set) {
			got[g.Commits[0].Sha] = g.Ignored
		}
		return got
	}
	if got := ignoredOf(nil); !got["aaa1111"] || got["bbb2222"] {
		t.Fatalf("chore should start ignored, work not: %v", got)
	}
	if got := ignoredOf(map[string]bool{backend.KeptKey("aaa1111"): true}); got["aaa1111"] {
		t.Fatal("a chore brought back must stay back")
	}

	store := backend.NewStore(t.TempDir())
	if err := store.SetKept("aaa1111", true); err != nil {
		t.Fatal(err)
	}
	set, err := store.IgnoredShas()
	if err != nil || !set[backend.KeptKey("aaa1111")] || set["aaa1111"] {
		t.Fatalf("kept set = %v, %v", set, err)
	}
	if err := store.SetKept("aaa1111", false); err != nil {
		t.Fatal(err)
	}
	if set, _ := store.IgnoredShas(); len(set) != 0 {
		t.Fatalf("forgetting should empty it: %v", set)
	}
}
