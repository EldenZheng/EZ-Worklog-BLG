package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Adding a Worklog's reference to the commits it logs.
//
// The worklog SOP has the commit for a piece of work reference its Worklog
// sub-issue. A commit pushed before its Worklog existed (a log dated for a later
// day, say) gets the reference afterwards: its message is rewritten in the
// user's own clone and the branch is force-pushed. A commit not pushed yet
// gets it before it reaches GitHub, and the branch is then pushed plainly.
//
// The rewrite never checks anything out. Each commit is recreated with
// commit-tree from its own tree, so files, the index and uncommitted edits are
// untouched whichever branch is checked out; only messages, and so commit IDs,
// change. Only the part of the branch already on GitHub, up to its newest
// logged commit, is pushed, with a lease
// on the tip read just before, and the local branch moves only if it is still
// where it was. Every step is written to a journal first, so a retry picks up
// where an interrupted run stopped instead of starting over.

// Git runs git in dir and returns its stdout. A variable so tests can watch it.
var Git = RunGit

// GitTimeout bounds one git call; a fetch or push over a slow line is the
// longest of them.
const GitTimeout = 3 * time.Minute

// RunGit is the real runner behind Git. env is added to the environment and
// stdin, when not empty, is fed to the command.
func RunGit(dir string, env []string, stdin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), GitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	hideConsole(cmd)
	cmd.Env = append(os.Environ(), env...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("git %s did not finish within %s", args[0], GitTimeout)
	}
	if err != nil {
		var ee *exec.Error
		if errors.As(err, &ee) {
			return "", errors.New("git was not found on PATH")
		}
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("git %s: %s", args[0], msg)
	}
	return out.String(), nil
}

func git(dir string, args ...string) (string, error) {
	out, err := Git(dir, nil, "", args...)
	return strings.TrimSpace(out), err
}

// ---- finding the user's clones ----

var remoteRepoRe = regexp.MustCompile(`(?i)github\.com[:/]+([\w.-]+)/([\w.-]+?)(?:\.git)?/?$`)

// RepoFromRemote reads owner/repo off a GitHub remote URL, https or ssh.
func RepoFromRemote(url string) string {
	m := remoteRepoRe.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return ""
	}
	return m[1] + "/" + m[2]
}

// RepoClones maps each GitHub repository (lowercase owner/repo) to a clone of
// it in folders: each folder that is a clone, and each clone directly inside
// one. The remote's configured URL is read as written, so a URL rewritten by
// insteadOf still names the repository it stands for.
func RepoClones(folders []string) map[string]string {
	clones := map[string]string{}
	for _, c := range cloneList(folders) {
		if _, seen := clones[strings.ToLower(c.repo)]; !seen {
			clones[strings.ToLower(c.repo)] = c.dir
		}
	}
	return clones
}

type repoClone struct{ repo, dir string }

// cloneList is every clone in folders with the repository it is of, in the
// order found, so the first clone of a repository wins as before. The remotes
// share the small local-scan budget with git log reads, so a folder with many
// clones does not launch a core's worth of git processes at startup.
func cloneList(folders []string) []repoClone {
	var dirs []string
	for _, folder := range folders {
		folder = strings.TrimSpace(folder)
		if folder == "" {
			continue
		}
		dirs = append(dirs, folder)
		entries, err := os.ReadDir(folder)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, filepath.Join(folder, e.Name()))
			}
		}
	}
	repos := make([]string, len(dirs))
	var wg sync.WaitGroup
	for i, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			localScanSlots <- struct{}{}
			defer func() { <-localScanSlots }()
			if url, err := git(dir, "config", "--get", "remote.origin.url"); err == nil {
				repos[i] = RepoFromRemote(url)
			}
		}()
	}
	wg.Wait()
	var out []repoClone
	seen := map[string]bool{}
	for i, repo := range repos {
		if repo != "" && !seen[strings.ToLower(repo)] {
			seen[strings.ToLower(repo)] = true
			out = append(out, repoClone{repo, dirs[i]})
		}
	}
	return out
}

// ---- which entries need their refs ----

// Reference states of an entry whose commits must reference its Worklog.
const (
	RefIncomplete = "incomplete" // not added yet; ref_note says why
	RefLocalSync  = "local-sync" // on GitHub; the local branch still has the old commits
	RefComplete   = "complete"   // every commit references the Worklog
)

// refsLockedNote ends the note of an entry whose commits can never be changed:
// already on the default branch, or their pull request merged.
const refsLockedNote = "cannot be changed."

// RefsLocked reports whether r's commits can never get its Worklog's
// reference because they are already merged: found by the rewrite after the
// push, or before it when every commit is in merged_refs (merged_commits.go).
func RefsLocked(r Row) bool {
	if r["ref_state"] == RefIncomplete && strings.HasSuffix(r["ref_note"], refsLockedNote) {
		return true
	}
	return SOPApplies(r["date"]) && !RowIsLegacy(r) && r["ref_state"] != RefComplete && AllRefsMerged(r)
}

// NeedsWorklogRefs reports whether r's commits still have to be given its
// Worklog's reference: an SOP entry pushed with commits, not legacy.
func NeedsWorklogRefs(r Row) bool {
	return SOPApplies(r["date"]) && !RowIsLegacy(r) && strings.TrimSpace(r["refs"]) != "" &&
		r["pushed_at"] != "" && WorklogRef(r["issue_url"]) != "" &&
		(r["ref_state"] == "" || r["ref_state"] == RefIncomplete) && RowPushMode(r) == "subissue" &&
		len(UnmergedRefs(r)) > 0
}

// ---- the plan for one branch ----

// RewriteCommit is one commit of a batch, in order, oldest first.
type RewriteCommit struct {
	Old     string   `json:"old"`
	New     string   `json:"new,omitempty"`
	Subject string   `json:"subject"`
	Refs    []string `json:"refs,omitempty"` // Worklogs this commit gains, as owner/repo#N
}

// RewriteBatch is every commit to rewrite on one branch, and the journal entry
// for doing it. Problem, when set, is why it cannot be done; the entries are
// left incomplete with that note.
type RewriteBatch struct {
	ID        string          `json:"id"`
	Repo      string          `json:"repo"`
	Dir       string          `json:"dir"`
	Branch    string          `json:"branch"`
	OldRemote string          `json:"old_remote"`
	OldLocal  string          `json:"old_local,omitempty"`
	NewRemote string          `json:"new_remote,omitempty"`
	NewLocal  string          `json:"new_local,omitempty"`
	Sign      bool            `json:"sign,omitempty"`
	Commits   []RewriteCommit `json:"commits"`
	RowIDs    []string        `json:"row_ids"`
	Stage     string          `json:"stage"` // built, pushed, local-sync
	// PushOld is the commit whose rewritten twin is pushed as GitHub's tip.
	// Empty in journals written before it: GitHub's old tip, as then.
	PushOld string `json:"push_old,omitempty"`
	Problem string `json:"-"`
	// Done is a batch whose commits name their Worklogs already: nothing to
	// rewrite, and its entries are complete.
	Done bool `json:"-"`
}

// Short is a commit ID as the app stores it.
func Short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

type refTarget struct {
	repo, sha, worklog, rowID string
}

// PlanWorklogRefs works out, branch by branch, what adding the references of
// rows' Worklogs to their commits would rewrite. It fetches each clone first,
// and stops a branch (Problem) when the rewrite would not be safe.
func PlanWorklogRefs(cfg Config, rows []Row) []RewriteBatch {
	byRepo := map[string][]refTarget{}
	var repos []string
	for _, r := range rows {
		if !NeedsWorklogRefs(r) {
			continue
		}
		worklog := strings.TrimPrefix(WorklogRef(r["issue_url"]), "Refs ")
		// Merged commits are left as they are: the Worklog links them.
		for _, c := range UnmergedRefs(r) {
			repo := c.Repo
			key := strings.ToLower(repo)
			if byRepo[key] == nil {
				repos = append(repos, key)
			}
			byRepo[key] = append(byRepo[key], refTarget{repo: repo, sha: c.Sha, worklog: worklog, rowID: r["id"]})
		}
	}
	sort.Strings(repos)
	clones := RepoClones(cfg.RepoFolders)
	var batches []RewriteBatch
	for _, key := range repos {
		targets := byRepo[key]
		dir := clones[key]
		if dir == "" {
			batches = append(batches, RewriteBatch{Repo: targets[0].repo, RowIDs: rowIDsOf(targets),
				Problem: fmt.Sprintf("No clone of %s in Settings > Repo folders.", targets[0].repo)})
			continue
		}
		batches = append(batches, planRepo(dir, targets[0].repo, targets)...)
	}
	return batches
}

func rowIDsOf(targets []refTarget) []string {
	var ids []string
	seen := map[string]bool{}
	for _, t := range targets {
		if !seen[t.rowID] {
			seen[t.rowID] = true
			ids = append(ids, t.rowID)
		}
	}
	return ids
}

func planRepo(dir, repo string, targets []refTarget) []RewriteBatch {
	stop := func(ts []refTarget, branch, why string) RewriteBatch {
		return RewriteBatch{Repo: repo, Dir: dir, Branch: branch, RowIDs: rowIDsOf(ts), Problem: why}
	}
	if _, err := git(dir, "fetch", "--quiet", "origin"); err != nil {
		return []RewriteBatch{stop(targets, "", "Could not fetch "+repo+": "+err.Error())}
	}
	def := defaultBranch(dir)
	type found struct {
		full string
		t    refTarget
	}
	byBranch := map[string][]found{}
	pushedBranch := map[string]bool{}
	var branches []string
	var batches []RewriteBatch
	for _, t := range targets {
		full, err := git(dir, "rev-parse", "--verify", "--quiet", t.sha+"^{commit}")
		if err != nil || full == "" {
			batches = append(batches, stop([]refTarget{t}, "", fmt.Sprintf("Commit %s is not in the clone at %s.", t.sha, dir)))
			continue
		}
		if def != "" {
			if _, err := git(dir, "merge-base", "--is-ancestor", full, "refs/remotes/origin/"+def); err == nil {
				batches = append(batches, stop([]refTarget{t}, def, fmt.Sprintf("Commit %s is already on %s, so it %s", t.sha, def, refsLockedNote)))
				continue
			}
		}
		out, _ := git(dir, "for-each-ref", "--contains", full, "--format=%(refname)", "refs/remotes/origin/")
		var on []string
		for _, ref := range strings.Fields(out) {
			name := strings.TrimPrefix(ref, "refs/remotes/origin/")
			if name != "HEAD" && name != def {
				on = append(on, name)
			}
		}
		onGitHub := len(on) > 0
		if !onGitHub {
			// Not pushed yet: the local branch that holds it, if just one.
			out, _ := git(dir, "for-each-ref", "--contains", full, "--format=%(refname:short)", "refs/heads/")
			on = strings.Fields(out)
		}
		switch len(on) {
		case 0:
			batches = append(batches, stop([]refTarget{t}, "", fmt.Sprintf("Commit %s is on no branch on GitHub or in the clone.", t.sha)))
			continue
		case 1:
		default:
			batches = append(batches, stop([]refTarget{t}, "", fmt.Sprintf("Commit %s is on several branches (%s); rewriting one would leave the others behind.", t.sha, strings.Join(on, ", "))))
			continue
		}
		if byBranch[on[0]] == nil {
			branches = append(branches, on[0])
		}
		byBranch[on[0]] = append(byBranch[on[0]], found{full, t})
		// A branch with any of its commits on GitHub is rewritten as one,
		// and its unpushed commits after GitHub's tip go along.
		pushedBranch[on[0]] = pushedBranch[on[0]] || onGitHub
	}
	sort.Strings(branches)
	for _, branch := range branches {
		var ts []refTarget
		refs := map[string][]string{}
		for _, f := range byBranch[branch] {
			ts = append(ts, f.t)
			if !containsFold(refs[f.full], f.t.worklog) {
				refs[f.full] = append(refs[f.full], f.t.worklog)
			}
		}
		// A commit that already names its Worklog (typed in by hand, or
		// rewritten before) gains nothing.
		for full, wanted := range refs {
			msg, _ := Git(dir, nil, "", "log", "-1", "--format=%B", full)
			var missing []string
			for _, ref := range wanted {
				if !MessageNamesIssue(msg, ref) {
					missing = append(missing, ref)
				}
			}
			if len(missing) == 0 {
				delete(refs, full)
			} else {
				refs[full] = missing
			}
		}
		if len(refs) == 0 {
			batches = append(batches, RewriteBatch{Repo: repo, Dir: dir, Branch: branch, RowIDs: rowIDsOf(ts), Done: true})
			continue
		}
		plan := planBranch
		if !pushedBranch[branch] {
			plan = func(dir, repo, branch string, refs map[string][]string) (RewriteBatch, string) {
				return planLocalBranch(dir, repo, branch, def, refs)
			}
		}
		b, why := plan(dir, repo, branch, refs)
		if why != "" {
			batches = append(batches, stop(ts, branch, why))
			continue
		}
		b.RowIDs = rowIDsOf(ts)
		batches = append(batches, b)
	}
	return batches
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

func defaultBranch(dir string) string {
	if out, err := git(dir, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(out, "refs/remotes/origin/")
	}
	for _, name := range []string{"main", "master"} {
		if _, err := git(dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+name); err == nil {
			return name
		}
	}
	return ""
}

// planBranch checks one branch and lists the commits from the oldest target to
// the tip. refs maps each target's full ID to the Worklogs it gains.
func planBranch(dir, repo, branch string, refs map[string][]string) (RewriteBatch, string) {
	b := RewriteBatch{Repo: repo, Dir: dir, Branch: branch}
	if out, err := Gh([]string{"pr", "list", "--repo", repo, "--head", branch, "--state", "merged", "--json", "number", "--jq", "length"}); err != nil {
		return b, "Could not check whether " + branch + "'s pull request is merged: " + err.Error()
	} else if strings.TrimSpace(out) != "0" && strings.TrimSpace(out) != "" {
		return b, branch + "'s pull request is already merged, so its commits " + refsLockedNote
	}
	if out, err := Gh([]string{"api", "repos/" + repo + "/branches/" + branch, "--jq", ".protected"}); err != nil {
		return b, "Could not check whether " + branch + " is protected: " + err.Error()
	} else if strings.TrimSpace(out) == "true" {
		return b, branch + " is a protected branch."
	}
	return planRange(dir, b, refs, true)
}

// planLocalBranch plans a branch whose commits to rewrite are all still only
// in the clone. Nothing on GitHub changes: the branch is pushed afterwards
// with its new commits on top of what GitHub has, an ordinary push. Never the
// default branch: work goes there by pull request, not by the app pushing it.
func planLocalBranch(dir, repo, branch, def string, refs map[string][]string) (RewriteBatch, string) {
	b := RewriteBatch{Repo: repo, Dir: dir, Branch: branch}
	if def != "" && branch == def {
		return b, "These commits are on " + branch + ", the default branch, and not pushed yet. Push them yourself."
	}
	return planRange(dir, b, refs, false)
}

// planRange lists the commits from the oldest target to the tip of the local
// branch (or GitHub's, with no local one) and picks how far to push. onGitHub
// says the branch must be on GitHub already.
func planRange(dir string, b RewriteBatch, refs map[string][]string, onGitHub bool) (RewriteBatch, string) {
	branch := b.Branch
	if busy := operationInProgress(dir); busy != "" {
		return b, busy
	}
	remote, err := git(dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	if err != nil || remote == "" {
		if onGitHub {
			return b, branch + " is not on GitHub any more."
		}
		remote = ""
	}
	b.OldRemote = remote
	local, _ := git(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if local != "" {
		if remote != "" {
			if _, err := git(dir, "merge-base", "--is-ancestor", remote, local); err != nil {
				return b, "Your local " + branch + " and GitHub's have diverged. Pull it first."
			}
		}
		b.OldLocal = local
	}
	tip := OrDefault(local, remote)
	if tip == "" {
		return b, branch + " is not in the clone any more."
	}
	// The oldest target is the one with the most commits after it.
	oldest, most := "", -1
	for full := range refs {
		out, err := git(dir, "rev-list", "--count", full+".."+tip)
		if err != nil {
			return b, "Could not read " + branch + ": " + err.Error()
		}
		var n int
		fmt.Sscanf(out, "%d", &n)
		if n > most {
			oldest, most = full, n
		}
	}
	base, err := git(dir, "rev-parse", "--verify", "--quiet", oldest+"^")
	if err != nil || base == "" {
		return b, "Commit " + Short(oldest) + " is the first in its repository and cannot be rewritten here."
	}
	out, err := git(dir, "rev-list", "--reverse", "--topo-order", "--parents", base+".."+tip)
	if err != nil {
		return b, "Could not read " + branch + ": " + err.Error()
	}
	prev := base
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || f[1] != prev {
			return b, branch + " has a merge commit after " + Short(oldest) + "; this version only rewrites straight history."
		}
		subject, _ := git(dir, "log", "-1", "--format=%s", f[0])
		b.Commits = append(b.Commits, RewriteCommit{Old: f[0], Subject: subject, Refs: refs[f[0]]})
		prev = f[0]
	}
	for full := range refs {
		if !b.hasCommit(full) {
			return b, "Commit " + Short(full) + " is not in " + branch + "'s history."
		}
	}
	if out, _ := git(dir, "config", "--bool", "commit.gpgsign"); out == "true" {
		b.Sign = true
	}
	b.choosePush()
	return b, ""
}

// choosePush sets PushOld, the commit whose rewritten twin becomes GitHub's
// tip: GitHub's own tip, or the newest commit gaining a reference when that
// one is past it, so a logged commit not pushed yet goes up with its
// reference. Unlogged commits after it stay only in the clone.
func (b *RewriteBatch) choosePush() {
	at := -1
	for i, c := range b.Commits {
		if c.Old == b.OldRemote {
			at = i
		}
	}
	for i, c := range b.Commits {
		if len(c.Refs) > 0 && i > at {
			at = i
		}
	}
	if at >= 0 {
		b.PushOld = b.Commits[at].Old
	}
}

// Unpushed reports whether every commit gaining a reference is past GitHub's
// tip: nothing on GitHub is rewritten, and the push only adds commits.
func (b RewriteBatch) Unpushed() bool {
	for _, c := range b.Commits {
		if c.Old == b.OldRemote {
			return false
		}
	}
	return true
}

func (b RewriteBatch) hasCommit(full string) bool {
	for _, c := range b.Commits {
		if c.Old == full {
			return true
		}
	}
	return false
}

// operationInProgress names a rebase, merge, cherry-pick or revert under way
// in any worktree of the clone, or "".
func operationInProgress(dir string) string {
	out, err := git(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return "Could not read the clone's worktrees: " + err.Error()
	}
	var trees []string
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree "); ok {
			trees = append(trees, p)
		}
	}
	for _, tree := range trees {
		gitDir, err := git(tree, "rev-parse", "--absolute-git-dir")
		if err != nil {
			continue
		}
		for _, mark := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD"} {
			if _, err := os.Stat(filepath.Join(gitDir, mark)); err == nil {
				return "A rebase, merge or cherry-pick is in progress in " + tree + ". Finish it first."
			}
		}
	}
	return ""
}

// ---- doing it ----

// RunWorklogRefs carries out a planned batch: it recreates the commits with
// their Worklog references, pushes the part that was on GitHub, moves the local
// branch, and updates the commit IDs the app has saved. Each stage is written
// to the journal first; a failure leaves the batch there for ResumeWorklogRefs.
func (s *Store) RunWorklogRefs(b RewriteBatch) (RewriteBatch, error) {
	if b.Problem != "" {
		return b, errors.New(b.Problem)
	}
	if _, err := git(b.Dir, "fetch", "--quiet", "origin"); err != nil {
		return b, err
	}
	// Nothing may have moved since the plan was shown.
	if remote, _ := git(b.Dir, "rev-parse", "--verify", "refs/remotes/origin/"+b.Branch); remote != b.OldRemote {
		return b, errors.New(b.Branch + " changed on GitHub since this was checked. Try again.")
	}
	if local, _ := git(b.Dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+b.Branch); local != b.OldLocal {
		return b, errors.New("Your local " + b.Branch + " changed since this was checked. Try again.")
	}
	if busy := operationInProgress(b.Dir); busy != "" {
		return b, errors.New(busy)
	}
	parent, err := git(b.Dir, "rev-parse", b.Commits[0].Old+"^")
	if err != nil {
		return b, err
	}
	newOf := map[string]string{}
	for i, c := range b.Commits {
		made, err := recreateCommit(b.Dir, c.Old, parent, c.Refs, b.Sign)
		if err != nil {
			return b, err
		}
		b.Commits[i].New = made
		newOf[c.Old] = made
		parent = made
	}
	b.NewRemote = newOf[OrDefault(b.PushOld, b.OldRemote)]
	if b.OldLocal != "" {
		b.NewLocal = newOf[b.OldLocal]
	}
	if b.NewRemote == "" {
		return b, errors.New("GitHub's tip of " + b.Branch + " was not in the rewritten range.")
	}
	b.ID = newID()
	b.Stage = "built"
	if err := s.saveRewrite(b); err != nil {
		return b, err
	}
	return s.ResumeWorklogRefs(b)
}

// ResumeWorklogRefs takes a journalled batch on from the stage it reached.
func (s *Store) ResumeWorklogRefs(b RewriteBatch) (RewriteBatch, error) {
	if b.Stage == "built" {
		if err := pushRewrite(b); err != nil {
			// The branch moved on GitHub, so these rewritten commits can never
			// be pushed. Forget them; the next try plans afresh.
			if errors.Is(err, errRemoteMoved) {
				_ = s.dropRewrite(b.ID)
			}
			return b, err
		}
		b.Stage = "pushed"
		if err := s.saveRewrite(b); err != nil {
			return b, err
		}
	}
	// GitHub has the references from here on; the app's own records follow.
	if err := s.remapRewrite(b); err != nil {
		return b, err
	}
	state := RefComplete
	err := s.syncLocal(&b)
	// Commits carried across get new IDs too.
	if rerr := s.remapRewrite(b); rerr != nil {
		return b, rerr
	}
	if err != nil {
		b.Stage = "local-sync"
		state = RefLocalSync
		_ = s.saveRewrite(b)
		_ = s.setRefState(b.RowIDs, state, err.Error())
		return b, nil
	}
	if err := s.setRefState(b.RowIDs, state, ""); err != nil {
		return b, err
	}
	return b, s.dropRewrite(b.ID)
}

// pushRewrite pushes the rewritten twin of PushOld (GitHub's old tip, or the
// newest logged commit past it), and nothing past that, with a lease on the
// old tip.
func pushRewrite(b RewriteBatch) error {
	ref := "refs/heads/" + b.Branch
	// With no old tip (a branch not on GitHub yet) the lease is that the
	// branch must not exist there; past an old tip it is a fast-forward.
	_, err := git(b.Dir, "push", "--quiet", "origin",
		"--force-with-lease="+ref+":"+b.OldRemote, b.NewRemote+":"+ref)
	if err == nil {
		if b.OldRemote == "" {
			// A first push: let a plain git push follow it from here.
			if up, _ := git(b.Dir, "rev-parse", "--abbrev-ref", b.Branch+"@{upstream}"); up == "" {
				_, _ = git(b.Dir, "branch", "--set-upstream-to=origin/"+b.Branch, b.Branch)
			}
		}
		return nil
	}
	// The outcome may be unknown (a dropped connection). If GitHub's branch
	// already holds the new commits, the push happened.
	if _, ferr := git(b.Dir, "fetch", "--quiet", "origin"); ferr == nil {
		if _, aerr := git(b.Dir, "merge-base", "--is-ancestor", b.NewRemote, "refs/remotes/origin/"+b.Branch); aerr == nil {
			return nil
		}
	}
	if cur, _ := git(b.Dir, "rev-parse", "--verify", "refs/remotes/origin/"+b.Branch); cur != b.OldRemote {
		return fmt.Errorf("%w: %s changed on GitHub; GitHub is unchanged by this. Try again", errRemoteMoved, b.Branch)
	}
	verb := "Force-pushing"
	if b.Unpushed() {
		verb = "Pushing"
	}
	return fmt.Errorf("%s %s failed, so GitHub is unchanged: %v", verb, b.Branch, err)
}

var errRemoteMoved = errors.New("branch moved on GitHub")

// syncLocal moves the local branch onto the rewritten commits. It never
// discards anything: it moves the branch only from where it is known to be,
// and commits made on it since are carried across rather than overwritten.
// Carried commits and the tip they end on are journalled before the branch
// moves, so a run cut short can always tell where it got to.
func (s *Store) syncLocal(b *RewriteBatch) error {
	if b.OldLocal == "" {
		return nil
	}
	ref := "refs/heads/" + b.Branch
	cur, _ := git(b.Dir, "rev-parse", "--verify", "--quiet", ref)
	if cur == "" || cur == b.NewLocal {
		return nil // deleted locally, or already moved
	}
	// Moved by an earlier run that stopped before recording it, with commits
	// made on top since.
	if _, err := git(b.Dir, "merge-base", "--is-ancestor", b.NewLocal, cur); err == nil {
		return nil
	}
	// Checked before every move: a branch must not change under a merge or
	// rebase the user is in the middle of.
	if busy := operationInProgress(b.Dir); busy != "" {
		return errors.New(busy)
	}
	if cur == b.OldLocal {
		_, err := git(b.Dir, "update-ref", ref, b.NewLocal, b.OldLocal)
		return err
	}
	if _, err := git(b.Dir, "merge-base", "--is-ancestor", b.OldLocal, cur); err != nil {
		return fmt.Errorf("Your local %s no longer contains the old commits. Pull it with --rebase.", b.Branch)
	}
	out, err := git(b.Dir, "rev-list", "--reverse", "--parents", b.OldLocal+".."+cur)
	if err != nil {
		return err
	}
	parent, prev := b.NewLocal, b.OldLocal
	var carried []RewriteCommit
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || f[1] != prev {
			return fmt.Errorf("Your local %s gained a merge commit; move it across with git pull --rebase.", b.Branch)
		}
		made, err := recreateCommit(b.Dir, f[0], parent, nil, b.Sign)
		if err != nil {
			return err
		}
		subject, _ := git(b.Dir, "log", "-1", "--format=%s", f[0])
		carried = append(carried, RewriteCommit{Old: f[0], New: made, Subject: subject})
		parent, prev = made, f[0]
	}
	b.Commits = append(b.Commits, carried...)
	b.OldLocal, b.NewLocal = cur, parent
	if err := s.saveRewrite(*b); err != nil {
		return err
	}
	_, err = git(b.Dir, "update-ref", ref, b.NewLocal, b.OldLocal)
	return err
}

// recreateCommit makes old's twin on parent: the same tree, author and
// committer, with refs added to its message.
func recreateCommit(dir, old, parent string, refs []string, sign bool) (string, error) {
	raw, err := Git(dir, nil, "", "cat-file", "commit", old)
	if err != nil {
		return "", err
	}
	header, message, _ := strings.Cut(raw, "\n\n")
	var tree, author, committer string
	for _, line := range strings.Split(header, "\n") {
		switch {
		case strings.HasPrefix(line, "tree "):
			tree = strings.TrimPrefix(line, "tree ")
		case strings.HasPrefix(line, "author "):
			author = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "committer "):
			committer = strings.TrimPrefix(line, "committer ")
		case strings.HasPrefix(line, "encoding "):
			return "", fmt.Errorf("Commit %s uses a non-UTF-8 message encoding this version does not rewrite.", Short(old))
		}
	}
	env := append(identityEnv("AUTHOR", author), identityEnv("COMMITTER", committer)...)
	args := []string{"commit-tree", tree, "-p", parent, "-F", "-"}
	if sign {
		args = append(args, "-S")
	}
	out, err := Git(dir, env, AddWorklogRefs(message, refs), args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

var identityRe = regexp.MustCompile(`^(.*) <([^>]*)> (\d+ [+-]\d{4})$`)

func identityEnv(role, line string) []string {
	m := identityRe.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	return []string{"GIT_" + role + "_NAME=" + m[1], "GIT_" + role + "_EMAIL=" + m[2], "GIT_" + role + "_DATE=" + m[3]}
}

// MessageNamesIssue reports whether message names the issue ref (owner/repo#N)
// as a whole: owner/repo#60010 does not name owner/repo#6001.
func MessageNamesIssue(message, ref string) bool {
	re, err := regexp.Compile(`(?i)(?:^|[^\w.-])` + regexp.QuoteMeta(ref) + `(?:\D|$)`)
	return err == nil && re.MatchString(message)
}

// AddWorklogRefs appends a "Refs owner/repo#N" line for each Worklog the
// message does not name already, after a blank line, as git trailers sit.
func AddWorklogRefs(message string, refs []string) string {
	var add []string
	for _, ref := range refs {
		if !MessageNamesIssue(message, ref) {
			add = append(add, "Refs "+ref)
		}
	}
	if len(add) == 0 {
		return message
	}
	body := strings.TrimRight(message, "\n")
	// Joined to a Refs block already closing the message, not a new paragraph.
	sep := "\n\n"
	if lines := strings.Split(body, "\n"); strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "Refs ") && len(lines) > 1 {
		sep = "\n"
	}
	return body + sep + strings.Join(add, "\n") + "\n"
}

// ---- the app's records ----

func (s *Store) rewriteJournalPath() string { return filepath.Join(s.Dir, "rewrite-journal.json") }

// RewriteJournal is the batches started and not yet finished.
func (s *Store) RewriteJournal() ([]RewriteBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readRewritesLocked()
}

func (s *Store) readRewritesLocked() ([]RewriteBatch, error) {
	data, err := os.ReadFile(s.rewriteJournalPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var batches []RewriteBatch
	if err := json.Unmarshal(data, &batches); err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", s.rewriteJournalPath(), err)
	}
	return batches, nil
}

func (s *Store) writeRewritesLocked(batches []RewriteBatch) error {
	if len(batches) == 0 {
		err := os.Remove(s.rewriteJournalPath())
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	data, err := json.MarshalIndent(batches, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.rewriteJournalPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.rewriteJournalPath())
}

func (s *Store) saveRewrite(b RewriteBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	batches, err := s.readRewritesLocked()
	if err != nil {
		return err
	}
	replaced := false
	for i := range batches {
		if batches[i].ID == b.ID {
			batches[i], replaced = b, true
		}
	}
	if !replaced {
		batches = append(batches, b)
	}
	return s.writeRewritesLocked(batches)
}

func (s *Store) dropRewrite(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	batches, err := s.readRewritesLocked()
	if err != nil {
		return err
	}
	kept := batches[:0]
	for _, b := range batches {
		if b.ID != id {
			kept = append(kept, b)
		}
	}
	return s.writeRewritesLocked(kept)
}

// remapRewrite points every saved entry and ignored commit at the rewritten
// commits. Saved IDs are short, so each is matched by prefix. Run twice, it
// finds nothing left to change.
func (s *Store) remapRewrite(b RewriteBatch) error {
	newOf := func(sha string) string {
		if sha == "" {
			return ""
		}
		for _, c := range b.Commits {
			if strings.HasPrefix(c.Old, strings.ToLower(sha)) {
				return c.New[:min(len(sha), len(c.New))]
			}
		}
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.readRowsLocked()
	if err != nil {
		return err
	}
	changed := false
	for _, r := range rows {
		refs := ParseCommitRefs(r["refs"])
		if len(refs) == 0 {
			continue
		}
		touched := false
		parts := make([]string, 0, len(refs))
		for _, c := range refs {
			if (c.Repo == "" || strings.EqualFold(c.Repo, b.Repo)) && newOf(c.Sha) != "" {
				c.Sha, touched = newOf(c.Sha), true
			}
			parts = append(parts, FormatCommitRef(c.Repo, c.Sha))
		}
		if touched {
			r["refs"] = strings.Join(parts, ",")
			changed = true
		}
	}
	if changed {
		if err := s.writeRowsLocked(rows); err != nil {
			return err
		}
	}
	ignored, err := s.readIgnoredLocked()
	if err != nil {
		return err
	}
	dates, err := s.readIgnoredDatesLocked()
	if err != nil {
		return err
	}
	moved := false
	for sha, on := range ignored {
		if n := newOf(sha); n != "" {
			delete(ignored, sha)
			ignored[n] = on
			if d, ok := dates[sha]; ok {
				delete(dates, sha)
				dates[n] = d
			}
			moved = true
		}
	}
	if !moved {
		return nil
	}
	if err := s.writeIgnoredLocked(ignored); err != nil {
		return err
	}
	return s.writeIgnoredDatesLocked(dates)
}

func (s *Store) setRefState(ids []string, state, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.readRowsLocked()
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	for _, r := range rows {
		if want[r["id"]] {
			r["ref_state"], r["ref_note"] = state, note
		}
	}
	return s.writeRowsLocked(rows)
}

// MarkRefsComplete records that a batch's entries' commits name their Worklogs.
func (s *Store) MarkRefsComplete(b RewriteBatch) error {
	return s.setRefState(b.RowIDs, RefComplete, "")
}

// MarkRefsIncomplete records why a batch's entries could not get their refs.
func (s *Store) MarkRefsIncomplete(b RewriteBatch, why string) error {
	return s.setRefState(b.RowIDs, RefIncomplete, why)
}
