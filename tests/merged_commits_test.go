package tests

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"worklog/internal/shared/backend"
)

// GitHub's replies about commits: aaaaaaa is on the default branch with no
// pull request, bbbbbbb is in a merged (squashed) pull request, ccccccc is in
// an open one. bbbbbbb is settled by the first reply, so only the other two
// are compared with the default branch.
const mergedObjects = `{"data":{"repository":{
 "c0":{"associatedPullRequests":{"nodes":[]}},
 "c1":{"associatedPullRequests":{"nodes":[{"merged":true}]}},
 "c2":{"associatedPullRequests":{"nodes":[{"merged":false}]}}}}}`
const mergedCompares = `{"data":{"repository":{"defaultBranchRef":{
 "d0":{"status":"BEHIND"},"d1":{"status":"AHEAD"}}}}}`

func TestMergedCommitsReadsDefaultBranchAndMergedPullRequests(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	calls := 0
	backend.Gh = func(args []string, _ ...string) (string, error) {
		calls++
		q := strings.Join(args, " ")
		if !strings.Contains(q, "o=o") {
			t.Errorf("repo not passed as a variable: %v", args)
		}
		if strings.Contains(q, "compare(") {
			if strings.Contains(q, "bbbbbbb") {
				t.Error("compared a commit its merged pull request already settled")
			}
			return mergedCompares, nil
		}
		return mergedObjects, nil
	}
	got, err := backend.MergedCommits([]backend.CommitRef{
		{Repo: "o/r", Sha: "aaaaaaa"}, {Repo: "o/r", Sha: "bbbbbbb"}, {Repo: "o/r", Sha: "ccccccc"},
		{Repo: "o/r", Sha: "not-a-sha"},
	})
	if err != nil || calls != 2 {
		t.Fatalf("want two requests and no error, got %d calls, %v", calls, err)
	}
	if !got["o/r@aaaaaaa"] || !got["o/r@bbbbbbb"] || got["o/r@ccccccc"] || len(got) != 2 {
		t.Fatalf("wrong merged set: %v", got)
	}
}

// A commit GitHub does not have (not pushed yet) reads back as null and is
// never compared, which would fail the whole request.
func TestMergedCommitsSkipsACommitGitHubDoesNotHave(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	backend.Gh = func(args []string, _ ...string) (string, error) {
		q := strings.Join(args, " ")
		if strings.Contains(q, "compare(") {
			if strings.Contains(q, "deadbee") {
				return "", errors.New("Could not resolve head ref 'deadbee'.")
			}
			return `{"data":{"repository":{"defaultBranchRef":{"d0":{"status":"IDENTICAL"}}}}}`, nil
		}
		return `{"data":{"repository":{"c0":{"associatedPullRequests":{"nodes":[]}},"c1":null}}}`, nil
	}
	got, err := backend.MergedCommits([]backend.CommitRef{{Repo: "o/r", Sha: "aaaaaaa"}, {Repo: "o/r", Sha: "deadbee"}})
	if err != nil || !got["o/r@aaaaaaa"] || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestRefreshMergedRefsMarksDraftsAndKeepsMergedCommitsOutOfTheRewrite(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	store := backend.NewStore(t.TempDir())
	if _, err := store.AppendRows([]backend.Row{
		{"date": "2026-11-24", "type": backend.KindCommit, "issue": "o/r#5", "mode": "subissue",
			"refs": "o/r@aaaaaaa,o/r@ccccccc", "minutes": "30"},
		{"date": "2026-11-24", "type": backend.KindCommit, "issue": "o/r#5", "mode": "subissue",
			"refs": "o/r@bbbbbbb", "minutes": "30"},
		// A legacy entry whose commits are all merged stays legacy; one with an
		// unmerged commit loses the tag. Pre-SOP entries are never checked.
		{"date": "2026-11-24", "type": backend.KindCommit, "issue": "o/r#5", "refs": "o/r@aaaaaaa", "legacy": "1"},
		{"date": "2026-11-10", "type": backend.KindCommit, "issue": "o/r#5", "refs": "o/r@aaaaaaa"},
		{"date": "2026-11-24", "type": backend.KindCommit, "issue": "o/r#5", "refs": "o/r@ccccccc", "legacy": "1"},
		{"date": "2026-11-24", "type": backend.KindCommit, "issue": "o/r#5", "refs": "o/r@ccccccc", "legacy": "1",
			"pushed_at": "2026-11-20T10:00:00", "issue_url": "https://github.com/o/r/issues/7"},
	}); err != nil {
		t.Fatal(err)
	}
	// aaaaaaa is on the default branch, bbbbbbb in a merged pull request,
	// ccccccc in an open one. Aliases follow the order the shas are asked in.
	backend.Gh = func(args []string, _ ...string) (string, error) {
		q := strings.Join(args, " ")
		var defs, objs []string
		for _, sha := range []string{"aaaaaaa", "ccccccc", "bbbbbbb"} {
			if !strings.Contains(q, sha) {
				continue
			}
			n := strconv.Itoa(len(defs))
			status, merged := "AHEAD", "false"
			if sha == "aaaaaaa" {
				status = "BEHIND"
			}
			if sha == "bbbbbbb" {
				merged = "true"
			}
			defs = append(defs, `"d`+n+`":{"status":"`+status+`"}`)
			objs = append(objs, `"c`+n+`":{"associatedPullRequests":{"nodes":[{"merged":`+merged+`}]}}`)
		}
		return `{"data":{"repository":{"defaultBranchRef":{` + strings.Join(defs, ",") + `},` +
			strings.Join(objs, ",") + `}}}`, nil
	}
	changed, err := store.RefreshMergedRefs()
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	rows, _ := store.ReadRows()
	if rows[0]["merged_refs"] != "o/r@aaaaaaa" || rows[1]["merged_refs"] != "o/r@bbbbbbb" {
		t.Fatalf("merged_refs not recorded: %q %q", rows[0]["merged_refs"], rows[1]["merged_refs"])
	}
	if rows[2]["merged_refs"] != "o/r@aaaaaaa" || rows[2]["legacy"] != "1" || rows[3]["merged_refs"] != "" {
		t.Fatalf("all-merged legacy keeps its tag; pre-SOP is left alone: %v %v", rows[2], rows[3])
	}
	if rows[4]["legacy"] != "" || rows[5]["legacy"] != "1" {
		t.Fatalf("an unpushed legacy entry with an unmerged commit loses the tag; a pushed one keeps it: %q %q",
			rows[4]["legacy"], rows[5]["legacy"])
	}
	// Part merged: only the open commit still needs the reference.
	if un := backend.UnmergedRefs(rows[0]); len(un) != 1 || un[0].Sha != "ccccccc" || backend.RefsLocked(rows[0]) {
		t.Fatalf("part-merged entry: unmerged=%v locked=%v", un, backend.RefsLocked(rows[0]))
	}
	// All merged: tagged, and once pushed nothing is left to rewrite.
	pushed := rows[1]
	pushed["pushed_at"], pushed["issue_url"] = "2026-11-24T10:00:00", "https://github.com/o/r/issues/6001"
	if !backend.RefsLocked(pushed) || backend.NeedsWorklogRefs(pushed) {
		t.Fatal("an entry whose commits are all merged must be locked and skip the rewrite")
	}
	if len(backend.PlanWorklogRefs(backend.Config{}, []backend.Row{pushed})) != 0 {
		t.Fatal("the rewrite planned a merged commit")
	}
	// Known merged commits are not asked about again.
	backend.Gh = func(args []string, _ ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "aaaaaaa") {
			t.Error("asked again about a commit already known to be merged")
		}
		return `{"data":{"repository":{"defaultBranchRef":{"d0":{"status":"AHEAD"}},"c0":null}}}`, nil
	}
	if _, err := store.RefreshMergedRefs(); err != nil {
		t.Fatal(err)
	}
}
