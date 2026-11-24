package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"worklog/internal/shared/backend"
)

func allTime() (string, string) {
	return "2000-01-01T00:00:00Z", time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
}

func commitIn(t *testing.T, dir, name, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(msg+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "add", name)
	gitT(t, dir, "commit", "--quiet", "-m", msg)
	return gitT(t, dir, "rev-parse", "HEAD")
}

// Only C is unpushed: it is read from the clone, on its branch, and A and B,
// which GitHub has, are not.
func TestUnpushedCommitsAreReadFromTheClone(t *testing.T) {
	f := newRewriteFixture(t)
	since, until := allTime()
	got := backend.LocalCommits(backend.Config{RepoFolders: []string{filepath.Dir(f.clone)}, Repos: []string{"rw-org"}}, since, until)
	if len(got) != 1 || got[0].Sha != backend.Short(f.c) || !got[0].Local || got[0].Branch != "feat" || got[0].Repo != "rw-org/app" {
		t.Fatalf("want only C, local on feat: %+v", got)
	}
	// Another org's clones are not this worklog's business.
	if other := backend.LocalCommits(backend.Config{RepoFolders: []string{filepath.Dir(f.clone)}, Repos: []string{"elsewhere"}}, since, until); len(other) != 0 {
		t.Fatalf("read a repo outside the config: %+v", other)
	}
}

// Logging C, which is only on this machine, adds its reference and pushes it
// as an ordinary push: GitHub's A and B stay exactly as they were.
func TestUnpushedLoggedCommitGetsItsRefAndAPlainPush(t *testing.T) {
	f := newRewriteFixture(t)
	oldRemote := gitT(t, f.remote, "rev-parse", "refs/heads/feat")
	store, cfg, made := f.store(t, f.c)
	batches := backend.PlanWorklogRefs(cfg, made)
	if len(batches) != 1 || batches[0].Problem != "" || !batches[0].Unpushed() || len(batches[0].Commits) != 1 {
		t.Fatalf("plan = %+v", batches)
	}
	done, err := store.RunWorklogRefs(batches[0])
	if err != nil {
		t.Fatal(err)
	}
	tip := gitT(t, f.remote, "rev-parse", "refs/heads/feat")
	if tip != done.Commits[0].New || gitT(t, f.remote, "rev-parse", tip+"^") != oldRemote {
		t.Fatal("GitHub should gain C on top of its own B, with nothing rewritten")
	}
	if msg := gitT(t, f.remote, "log", "-1", "--format=%B", tip); !strings.HasPrefix(msg, "Refs rw-org/app/issues/6001:\n") {
		t.Fatalf("C should reference its Worklog:\n%s", msg)
	}
	if gitT(t, f.clone, "rev-parse", "refs/heads/feat") != tip {
		t.Fatal("the local branch should be the pushed commit")
	}
	if status := gitT(t, f.clone, "status", "--porcelain"); !strings.Contains(status, "a.txt") {
		t.Fatalf("the uncommitted edit must survive, status: %q", status)
	}
	rows, _ := store.ReadRows()
	if rows[0]["ref_state"] != backend.RefComplete || rows[0]["refs"] != "rw-org/app@"+backend.Short(tip) {
		t.Fatalf("entry not complete on the new commit: %v", rows[0])
	}
}

// A branch never pushed is pushed new, and tracks GitHub's copy afterwards.
func TestUnpushedBranchIsPushedNewAndTracked(t *testing.T) {
	f := newRewriteFixture(t)
	gitT(t, f.clone, "stash", "--quiet")
	gitT(t, f.clone, "checkout", "--quiet", "-b", "5186-fix", "main")
	d := commitIn(t, f.clone, "d.txt", "Commit D")
	store, cfg, made := f.store(t, d)
	batches := backend.PlanWorklogRefs(cfg, made)
	if len(batches) != 1 || batches[0].Problem != "" || !batches[0].Unpushed() || batches[0].OldRemote != "" {
		t.Fatalf("plan = %+v", batches)
	}
	done, err := store.RunWorklogRefs(batches[0])
	if err != nil {
		t.Fatal(err)
	}
	if gitT(t, f.remote, "rev-parse", "refs/heads/5186-fix") != done.Commits[0].New {
		t.Fatal("the new branch should be on GitHub with D's twin")
	}
	if up := gitT(t, f.clone, "rev-parse", "--abbrev-ref", "5186-fix@{upstream}"); up != "origin/5186-fix" {
		t.Fatalf("the branch should track GitHub's copy, upstream %q", up)
	}
}

// The app never pushes the default branch: unpushed work there is yours to push.
func TestUnpushedCommitOnTheDefaultBranchIsLeftToYou(t *testing.T) {
	f := newRewriteFixture(t)
	gitT(t, f.clone, "stash", "--quiet")
	gitT(t, f.clone, "checkout", "--quiet", "main")
	m := commitIn(t, f.clone, "m.txt", "Commit on main")
	_, cfg, made := f.store(t, m)
	batches := backend.PlanWorklogRefs(cfg, made)
	if len(batches) != 1 || !strings.Contains(batches[0].Problem, "default branch") {
		t.Fatalf("plan = %+v", batches)
	}
	if gitT(t, f.remote, "rev-parse", "refs/heads/main") != f.mainTip {
		t.Fatal("main must not move")
	}
}

// A on GitHub and C only here, on one branch: one rewrite, pushed up to C.
func TestPushedAndUnpushedCommitsOnOneBranchGoUpTogether(t *testing.T) {
	f := newRewriteFixture(t)
	store, cfg, made := f.store(t, f.a, f.c)
	batches := backend.PlanWorklogRefs(cfg, made)
	if len(batches) != 1 || batches[0].Problem != "" || batches[0].Unpushed() || batches[0].PushOld != f.c {
		t.Fatalf("plan = %+v", batches)
	}
	done, err := store.RunWorklogRefs(batches[0])
	if err != nil {
		t.Fatal(err)
	}
	tip := gitT(t, f.remote, "rev-parse", "refs/heads/feat")
	// The Worklog's reference leads the message; the original follows it.
	if tip != done.NewRemote || !strings.HasSuffix(gitT(t, f.remote, "log", "-1", "--format=%B", tip), "\nCommit C, not pushed") {
		t.Fatal("GitHub should end on C's twin")
	}
}
