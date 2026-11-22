package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"worklog/internal/shared/backend"
)

// rewriteFixture is a clone of "rw-org/app" whose GitHub is a bare repository
// on disk: main with one commit, and a branch feat with commits A and B pushed
// and C committed but not pushed. The working tree has an uncommitted edit.
type rewriteFixture struct {
	t                *testing.T
	remote, clone    string
	a, b, c, mainTip string
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRewriteFixture(t *testing.T) *rewriteFixture {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := tempDir(t)
	f := &rewriteFixture{t: t, remote: filepath.Join(root, "remote.git"), clone: filepath.Join(root, "app")}
	gitT(t, root, "init", "--quiet", "--bare", "-b", "main", f.remote)
	gitT(t, root, "init", "--quiet", "-b", "main", f.clone)
	for _, kv := range [][2]string{{"user.name", "Sample Dev"}, {"user.email", "dev@example.com"},
		{"commit.gpgsign", "false"}, {"core.hooksPath", filepath.Join(root, "no-hooks")}, {"core.autocrlf", "false"}} {
		gitT(t, f.clone, "config", kv[0], kv[1])
	}
	gitT(t, f.clone, "remote", "add", "origin", "https://github.com/rw-org/app.git")
	gitT(t, f.clone, "config", "url."+filepath.ToSlash(f.remote)+".insteadOf", "https://github.com/rw-org/app.git")
	commit := func(name, msg string) string {
		if err := os.WriteFile(filepath.Join(f.clone, name), []byte(msg+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitT(t, f.clone, "add", name)
		gitT(t, f.clone, "commit", "--quiet", "-m", msg)
		return gitT(t, f.clone, "rev-parse", "HEAD")
	}
	f.mainTip = commit("base.txt", "Base")
	gitT(t, f.clone, "push", "--quiet", "origin", "main")
	gitT(t, f.clone, "checkout", "--quiet", "-b", "feat")
	f.a = commit("a.txt", "Commit A")
	f.b = commit("b.txt", "Commit B")
	gitT(t, f.clone, "push", "--quiet", "origin", "feat")
	f.c = commit("c.txt", "Commit C, not pushed")
	gitT(t, f.clone, "fetch", "--quiet", "origin")
	gitT(t, f.clone, "remote", "set-head", "origin", "main")
	if err := os.WriteFile(filepath.Join(f.clone, "a.txt"), []byte("edited, not committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	backend.Gh = func(args []string, _ ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "pr list"):
			return "0", nil
		case strings.Contains(joined, "/branches/"):
			return "false", nil
		}
		return "", backend.GhErr("offline")
	}
	return f
}

func (f *rewriteFixture) store(t *testing.T, worklogFor ...string) (*backend.Store, backend.Config, []backend.Row) {
	store := backend.NewStore(tempDir(t))
	var rows []backend.Row
	for i, sha := range worklogFor {
		rows = append(rows, backend.Row{"date": "2026-11-24", "type": "commit", "minutes": "60", "mode": "subissue",
			"issue": "rw-org/app#5185", "refs": "rw-org/app@" + backend.Short(sha), "pushed_at": "2026-11-24T10:00:00",
			"issue_url": "https://github.com/rw-org/app/issues/" + []string{"6001", "6002"}[i]})
	}
	made, err := store.AppendRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	return store, backend.Config{RepoFolders: []string{filepath.Dir(f.clone)}}, made
}

func TestWorklogRefsRewriteTheBranchKeepingFilesAndUnpushedWork(t *testing.T) {
	f := newRewriteFixture(t)
	store, cfg, made := f.store(t, f.a)
	if err := store.SetIgnored(backend.Short(f.b), true); err != nil {
		t.Fatal(err)
	}
	batches := backend.PlanWorklogRefs(cfg, made)
	if len(batches) != 1 || batches[0].Problem != "" || batches[0].Branch != "feat" || len(batches[0].Commits) != 3 {
		t.Fatalf("plan = %+v", batches)
	}
	if got := batches[0].Commits[0]; got.Old != f.a || len(got.Refs) != 1 || got.Refs[0] != "rw-org/app#6001" {
		t.Fatalf("A should gain the Worklog ref: %+v", got)
	}
	done, err := store.RunWorklogRefs(batches[0])
	if err != nil {
		t.Fatal(err)
	}
	remoteTip := gitT(t, f.remote, "rev-parse", "refs/heads/feat")
	if remoteTip != done.NewRemote || gitT(t, f.remote, "log", "-1", "--format=%s", remoteTip) != "Commit B" {
		t.Fatalf("GitHub should get the rewritten B and nothing past it, got %s", gitT(t, f.remote, "log", "--format=%s", "feat"))
	}
	if msg := gitT(t, f.remote, "log", "-1", "--format=%B", remoteTip+"^"); !strings.Contains(msg, "Refs rw-org/app#6001") {
		t.Fatalf("A on GitHub should reference its Worklog:\n%s", msg)
	}
	local := gitT(t, f.clone, "rev-parse", "refs/heads/feat")
	if local != done.NewLocal || gitT(t, f.clone, "rev-parse", local+"^") != remoteTip ||
		gitT(t, f.clone, "log", "-1", "--format=%s", local) != "Commit C, not pushed" {
		t.Fatal("the local branch should carry the unpushed commit on top of the rewritten ones")
	}
	if gitT(t, f.clone, "rev-parse", local+"^{tree}") != gitT(t, f.clone, "rev-parse", f.c+"^{tree}") {
		t.Fatal("the files must be exactly as they were")
	}
	if status := gitT(t, f.clone, "status", "--porcelain"); !strings.Contains(status, "a.txt") {
		t.Fatalf("the uncommitted edit must survive, status: %q", status)
	}
	if gitT(t, f.clone, "log", "-1", "--format=%an %ae %ad", done.Commits[0].New) != gitT(t, f.clone, "log", "-1", "--format=%an %ae %ad", f.a) {
		t.Fatal("author and date must be kept")
	}
	rows, _ := store.ReadRows()
	if rows[0]["refs"] != "rw-org/app@"+backend.Short(done.Commits[0].New) || rows[0]["ref_state"] != backend.RefComplete {
		t.Fatalf("the entry should point at the rewritten commit and be complete: %v", rows[0])
	}
	ignored, _ := store.IgnoredShas()
	if !ignored[backend.Short(done.Commits[1].New)] || ignored[backend.Short(f.b)] {
		t.Fatalf("ignored commits follow the rewrite: %v", ignored)
	}
	if journal, _ := store.RewriteJournal(); len(journal) != 0 {
		t.Fatalf("a finished rewrite leaves no journal: %+v", journal)
	}
	if again := backend.PlanWorklogRefs(cfg, rows); len(again) != 0 {
		t.Fatalf("a complete entry needs nothing more: %+v", again)
	}
}

func TestWorklogRefsNeverOverwriteACommitMadeDuringThePush(t *testing.T) {
	f := newRewriteFixture(t)
	store, cfg, made := f.store(t, f.b)
	batches := backend.PlanWorklogRefs(cfg, made)
	if len(batches) != 1 || batches[0].Problem != "" {
		t.Fatalf("plan = %+v", batches)
	}
	// A commit lands on the local branch right after the push, before the
	// branch is moved.
	realGit := backend.Git
	t.Cleanup(func() { backend.Git = realGit })
	var late string
	backend.Git = func(dir string, env []string, stdin string, args ...string) (string, error) {
		if late == "" && len(args) > 0 && args[0] == "update-ref" {
			if err := os.WriteFile(filepath.Join(f.clone, "d.txt"), []byte("d\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitT(t, f.clone, "add", "d.txt")
			gitT(t, f.clone, "commit", "--quiet", "-m", "Commit D, made meanwhile")
			late = gitT(t, f.clone, "rev-parse", "HEAD")
		}
		return realGit(dir, env, stdin, args...)
	}
	done, err := store.RunWorklogRefs(batches[0])
	if err != nil {
		t.Fatal(err)
	}
	if done.Stage != "local-sync" || gitT(t, f.clone, "rev-parse", "refs/heads/feat") != late {
		t.Fatal("the local branch must be left alone when it moved")
	}
	rows, _ := store.ReadRows()
	if rows[0]["ref_state"] != backend.RefLocalSync {
		t.Fatalf("the entry should say the local sync is pending: %v", rows[0])
	}
	journal, _ := store.RewriteJournal()
	if len(journal) != 1 {
		t.Fatalf("the unfinished sync stays in the journal: %+v", journal)
	}
	if _, err := store.ResumeWorklogRefs(journal[0]); err != nil {
		t.Fatal(err)
	}
	tip := gitT(t, f.clone, "rev-parse", "refs/heads/feat")
	if gitT(t, f.clone, "log", "-1", "--format=%s", tip) != "Commit D, made meanwhile" ||
		gitT(t, f.clone, "rev-parse", tip+"~2") != done.NewRemote {
		t.Fatalf("the retry carries C and D onto the rewritten branch:\n%s", gitT(t, f.clone, "log", "--format=%h %s", "feat"))
	}
	rows, _ = store.ReadRows()
	if rows[0]["ref_state"] != backend.RefComplete {
		t.Fatalf("then the entry is complete: %v", rows[0])
	}
}

func TestWorklogRefsStopWhereARewriteIsNotSafe(t *testing.T) {
	f := newRewriteFixture(t)
	store, cfg, made := f.store(t, f.mainTip)
	if b := backend.PlanWorklogRefs(cfg, made); len(b) != 1 || !strings.Contains(b[0].Problem, "already on main") {
		t.Fatalf("a commit on main cannot be changed: %+v", b)
	}
	_, _, made = f.store(t, f.a)
	batches := backend.PlanWorklogRefs(cfg, made)
	// GitHub's branch moves after the plan was shown.
	other := filepath.Join(filepath.Dir(f.clone), "other")
	gitT(t, filepath.Dir(f.clone), "clone", "--quiet", "-b", "feat", f.remote, other)
	gitT(t, other, "-c", "user.name=x", "-c", "user.email=x@example.com", "commit", "--quiet", "--allow-empty", "-m", "Someone else")
	gitT(t, other, "push", "--quiet", "origin", "feat")
	before := gitT(t, f.remote, "rev-parse", "refs/heads/feat")
	if _, err := store.RunWorklogRefs(batches[0]); err == nil || !strings.Contains(err.Error(), "changed on GitHub") {
		t.Fatalf("a moved branch must stop the rewrite, got %v", err)
	}
	if gitT(t, f.remote, "rev-parse", "refs/heads/feat") != before {
		t.Fatal("GitHub must be unchanged")
	}
	if backend.AddWorklogRefs("Fix\n\nRefs rw-org/app#6001\n", []string{"rw-org/app#6001"}) != "Fix\n\nRefs rw-org/app#6001\n" {
		t.Fatal("a message naming its Worklog already is left as it is")
	}
	_ = store
}
