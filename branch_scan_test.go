package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad test timestamp %q: %v", s, err)
	}
	return at
}

// The event feed is the only place unmerged branches are named, and it is
// lossy: a brand new branch reports as a CreateEvent, and the PushEvent that
// should follow it is the one GitHub is most likely to drop. Both types count.
func TestEventsNameBranchesFromPushAndCreate(t *testing.T) {
	feed := strings.Join([]string{
		`{"at":"2026-08-18T10:15:30Z","type":"PushEvent","repo":"bigledger/one","ref":"refs/heads/issues/548"}`,
		`{"at":"2026-08-18T11:32:40Z","type":"CreateEvent","repo":"bigledger/two","ref":"issues/51","refType":"branch"}`,
		`{"at":"2026-08-18T11:33:00Z","type":"CreateEvent","repo":"bigledger/two","ref":"v1.2","refType":"tag"}`,
		`{"at":"2026-08-18T12:00:00Z","type":"IssuesEvent","repo":"bigledger/three"}`,
	}, "\n")

	got := parseAuthorEvents(feed,
		mustTime(t, "2026-08-16T00:00:00Z"), mustTime(t, "2026-08-19T00:00:00Z"))

	want := []pushRef{
		{repo: "bigledger/one", ref: "issues/548"},
		{repo: "bigledger/two", ref: "issues/51"},
	}
	if len(got.refs) != len(want) {
		t.Fatalf("a pushed and a created branch are both work to scan, got %v", got.refs)
	}
	for i, w := range want {
		if got.refs[i] != w {
			t.Fatalf("ref %d = %v, want %v", i, got.refs[i], w)
		}
	}
	// A tag is not a branch, so it never becomes a ref to list commits on.
	for _, r := range got.refs {
		if r.ref == "v1.2" {
			t.Fatal("a created tag is not a branch")
		}
	}
}

// Every repo the author touched is worth a branch sweep, whatever the event
// was: the missing PushEvent is precisely the case this covers, so waiting for
// one would defeat the point. Repos are reported once each.
func TestEventsCollectEveryRepoTouchedOnce(t *testing.T) {
	feed := strings.Join([]string{
		`{"at":"2026-08-18T12:00:00Z","type":"IssueCommentEvent","repo":"bigledger/three"}`,
		`{"at":"2026-08-18T12:05:00Z","type":"IssuesEvent","repo":"bigledger/three"}`,
		`{"at":"2026-08-18T12:10:00Z","type":"PushEvent","repo":"bigledger/one","ref":"refs/heads/main"}`,
		`{"at":"2026-08-10T12:10:00Z","type":"PushEvent","repo":"bigledger/old","ref":"refs/heads/main"}`,
	}, "\n")

	got := parseAuthorEvents(feed,
		mustTime(t, "2026-08-16T00:00:00Z"), mustTime(t, "2026-08-19T00:00:00Z"))

	want := []string{"bigledger/three", "bigledger/one"}
	if len(got.repos) != len(want) {
		t.Fatalf("each repo touched in the window should be swept once, got %v", got.repos)
	}
	for i, w := range want {
		if got.repos[i] != w {
			t.Fatalf("repo %d = %q, want %q", i, got.repos[i], w)
		}
	}
	// Outside the window is someone else's week; it is not fetched.
	for _, r := range got.repos {
		if r == "bigledger/old" {
			t.Fatal("an event older than the window should not pull in its repo")
		}
	}
}

// The event feed is not just missing branches, it misses whole repositories:
// if the dropped PushEvent was a repo's only appearance in the window, nothing
// in the feed names it. The org listing does, and it is sorted newest push
// first, so the walk ends at the first repo too old to matter.
func TestRepoListingStopsAtTheFirstRepoOlderThanTheWindow(t *testing.T) {
	page := strings.Join([]string{
		`{"name":"bigledger/grn-stock-in","at":"2026-08-19T16:06:02Z"}`,
		`{"name":"bigledger/akaun-platform-java","at":"2026-08-19T15:54:44Z"}`,
		`{"name":"bigledger/quiet-since-june","at":"2026-06-02T09:00:00Z"}`,
		`{"name":"bigledger/quieter-still","at":"2026-01-02T09:00:00Z"}`,
	}, "\n")

	got, more := parseReposPushedSince(page, mustTime(t, "2026-08-16T00:00:00Z"))

	want := []string{"bigledger/grn-stock-in", "bigledger/akaun-platform-java"}
	if len(got) != len(want) {
		t.Fatalf("only repos pushed in the window can hold unlogged work, got %v", got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("repo %d = %q, want %q", i, got[i], w)
		}
	}
	// Everything past the cutoff is older still, so asking for page two would be
	// hundreds of repos of nothing.
	if more {
		t.Fatal("a page that ran past the cutoff ends the walk")
	}
}

// A full page of recent pushes means the cutoff is somewhere on the next one.
// Stopping at the page boundary would silently drop every repo behind it.
func TestRepoListingAsksForAnotherPageWhenTheFirstIsAllRecent(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf(`{"name":"bigledger/repo-%d","at":"2026-08-19T10:00:00Z"}`, i)
	}

	got, more := parseReposPushedSince(strings.Join(lines, "\n"),
		mustTime(t, "2026-08-16T00:00:00Z"))

	if len(got) != 100 {
		t.Fatalf("every repo on the page is in the window, got %d", len(got))
	}
	if !more {
		t.Fatal("a full page of recent pushes means the cutoff is on the next one")
	}
}

// A repo created and never pushed to has no push date. Reading that as older
// than the cutoff would end the walk on it and lose the active repos behind it.
func TestRepoListingIgnoresARepoWithNoPushDate(t *testing.T) {
	page := strings.Join([]string{
		`{"name":"bigledger/never-used","at":null}`,
		`{"name":"bigledger/grn-stock-in","at":"2026-08-19T16:06:02Z"}`,
		`{"name":"bigledger/quiet-since-june","at":"2026-06-02T09:00:00Z"}`,
	}, "\n")

	got, _ := parseReposPushedSince(page, mustTime(t, "2026-08-16T00:00:00Z"))

	want := []string{"bigledger/grn-stock-in"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("an undated repo should neither count nor stop the walk, got %v", got)
	}
}

// A branch someone else deleted still looks live in the activity log, because
// that log only reports the author's own actions: they pushed the branch, a
// colleague merged the pull request and deleted it, and the deletion is filed
// under the colleague. Listing the commits of a branch that is no longer there
// answers 404, and a merged branch is not something to put an error on screen
// about — the work reached the default branch, where the commit search has it.
func TestAGoneBranchIsNotAnError(t *testing.T) {
	gone := ghErr("gh: Not Found (HTTP 404)")
	if !isNotFound(gone) {
		t.Fatal("a 404 from gh should be recognised as one")
	}
	for _, other := range []error{
		nil,
		ghErr("gh: Bad credentials (HTTP 401)"),
		ghErr("gh: API rate limit exceeded (HTTP 403)"),
		ghErr("gh CLI not found — install it and run `gh auth login`."),
	} {
		if isNotFound(other) {
			t.Fatalf("%v is not a missing branch and must still be reported", other)
		}
	}
}

// A repo carries years of dead branches. Only the ones the author moved in the
// window can hold unlogged work, and scanning the rest is a round trip each.
// The same branch is created and then pushed to, so it is named once.
func TestActivityRefsKeepOnlyTheOnesTouchedInTheWindow(t *testing.T) {
	activity := strings.Join([]string{
		`{"ref":"refs/heads/BigLedger-Support/pcimage-operations/issues/51","at":"2026-08-18T13:31:05Z","kind":"push"}`,
		`{"ref":"refs/heads/BigLedger-Support/pcimage-operations/issues/51","at":"2026-08-18T11:32:40Z","kind":"branch_creation"}`,
		`{"ref":"refs/heads/main","at":"2026-08-17T07:07:14Z","kind":"push"}`,
		`{"ref":"refs/heads/bigledger/blg-sd-pc-image#2351","at":"2026-08-17T13:19:53Z","kind":"branch_deletion"}`,
		`{"ref":"refs/heads/bigledger/blg-sd-pc-image#2351","at":"2026-08-17T09:47:09Z","kind":"push"}`,
		`{"ref":"refs/heads/copilot/cleanup-deployment-scripts","at":"2025-11-14T02:37:11Z","kind":"push"}`,
		`{"ref":"","at":"2026-08-18T13:31:02Z","kind":"push"}`,
		`not json`,
	}, "\n")

	got := parseActivityRefs(activity, mustTime(t, "2026-08-16T00:00:00Z"))

	want := []string{"BigLedger-Support/pcimage-operations/issues/51", "main"}
	if len(got) != len(want) {
		t.Fatalf("only branches touched in the window are worth listing, got %v", got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("branch %d = %q, want %q", i, got[i], w)
		}
	}
	// A branch deleted after its last push is gone: listing commits on it is a
	// 404, and the work went to the default branch with the merge anyway.
	for _, n := range got {
		if n == "bigledger/blg-sd-pc-image#2351" {
			t.Fatal("a deleted branch should not be scanned")
		}
	}
}
