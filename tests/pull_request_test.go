package tests

import (
	"net/url"
	"strings"
	"testing"

	"fyne.io/fyne/v2"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
)

func TestPullRequestBodyListsCommitLinesAsPointsWithoutRefs(t *testing.T) {
	got := backend.PullRequestBody("o/r#5185", []string{
		"Refs o/r#5185: fix the totals\n\n- round per day\n* keep the cap\n\nRefs o/r#6001",
		"Tidy the report\nRefs o/r#6002\n- round per day",
	})
	want := "Refs o/r#5185:\n- fix the totals\n- round per day\n- keep the cap\n- Tidy the report"
	if got != want {
		t.Fatalf("body:\n%s\nwant:\n%s", got, want)
	}
}

// The pull request goes into the clone's own repository, even a fork, and its
// default branch: gh's own guess in a fork is the original repository.
func TestPullRequestTargetsTheRepositoryAndItsDefaultBranch(t *testing.T) {
	f := newRewriteFixture(t)
	gitT(t, f.clone, "push", "--quiet", "origin", "feat")
	logged := func(sha, _ string) bool { return true }
	var created []string
	backend.Gh = func(args []string, _ ...string) (string, error) {
		switch {
		case len(args) > 1 && args[0] == "pr" && args[1] == "list":
			return "", nil
		case len(args) > 1 && args[0] == "pr" && args[1] == "create":
			created = args
			return "https://github.com/rw-org/app/pull/7\n", nil
		}
		return "", backend.GhErr("offline")
	}
	p, err := backend.PlanPullRequest(f.clone, "rw-org/app", "feat", "rw-org/app#5185", logged)
	if err != nil || p.Base != "main" || p.Unlogged != 0 || p.Existing != "" || p.Title != "rw-org/app#5185" {
		t.Fatalf("plan %+v, %v", p, err)
	}
	if p.Body != "Refs rw-org/app#5185:\n- Commit A\n- Commit B\n- Commit C, not pushed" {
		t.Fatalf("body:\n%s", p.Body)
	}
	u, err := url.Parse(p.CompareURL())
	if err != nil || u.Path != "/rw-org/app/compare/main...feat" || u.Query().Get("title") != p.Title || u.Query().Get("body") != p.Body {
		t.Fatalf("compare URL %q", p.CompareURL())
	}
	made, err := backend.CreatePullRequest(p)
	joined := strings.Join(created, " ")
	if err != nil || made != "https://github.com/rw-org/app/pull/7" ||
		!strings.Contains(joined, "--repo rw-org/app") || !strings.Contains(joined, "--base main") || !strings.Contains(joined, "--head feat") {
		t.Fatalf("create %q, %v; args %q", made, err, joined)
	}
	// One of your commits still unlogged: nothing is offered.
	p, _ = backend.PlanPullRequest(f.clone, "rw-org/app", "feat", "rw-org/app#5185",
		func(sha, _ string) bool { return sha != f.b })
	if p.Unlogged != 1 {
		t.Fatalf("want one unlogged commit, got %d", p.Unlogged)
	}
}

// An entry or a pending commit only on this machine says so beside it.
func TestUnpushedWorkIsTaggedPendingPush(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	row := backend.Row{"date": "2026-11-24", "type": backend.KindCommit, "issue": "o/r#5", "mode": "subissue",
		"refs": "o/r@abc1234", "minutes": "30"}
	for _, card := range []fyne.CanvasObject{logwork.RowTile(ui, row, func() {}), logwork.DayRowCard(ui, row, func() {})} {
		if contains(labels(card), "Pending Push") {
			t.Fatal("tagged before anything was read from the clones")
		}
	}
	ui.Pending.Local = map[string]bool{backend.CommitRef{Repo: "o/r", Sha: "abc1234"}.Key(): true}
	for _, card := range []fyne.CanvasObject{logwork.RowTile(ui, row, func() {}), logwork.DayRowCard(ui, row, func() {})} {
		if !contains(labels(card), "Pending Push") {
			t.Fatalf("saved entry of an unpushed commit not tagged: %v", labels(card))
		}
	}
	g := backend.Group{Date: "2026-11-24", Issue: "o/r#5", Commits: []backend.Commit{{Sha: "abc1234", Repo: "o/r", Message: "Fix", Local: true}}}
	if !contains(labels(logwork.GroupBubble(ui, g, false)), "Pending Push") {
		t.Fatal("pending commit only on this machine not tagged")
	}
	g.Commits[0].Local = false
	if contains(labels(logwork.GroupBubble(ui, g, false)), "Pending Push") {
		t.Fatal("a pushed commit was tagged")
	}
}
