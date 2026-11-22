package backend

import (
	"runtime"
	"strings"
	"sync"
)

// Commits that are only on this machine. A small, unhurried fix can stay
// unpushed until its Worklog exists: the app then adds the Worklog's
// reference before the commit ever reaches GitHub, so nothing there is
// rewritten, and pushes the branch as an ordinary push. Commits are read from
// the clones under Settings > Repo folders, on local branches and not on any
// remote-tracking branch. Pushed in the meantime, a commit comes back from
// GitHub as well, and GitHub's copy wins.

// localScanLimit is how many clones are read at once: one per core keeps a
// burst of git processes from stalling the machine, and is as quick.
func localScanLimit() int { return max(1, runtime.NumCPU()) }

// Key is how a commit is matched across the refs column, GitHub and the
// clones: repo and short sha, without case.
func (c CommitRef) Key() string { return commitKey(c) }

// LocalCommits reads the author's unpushed commits, authored since..until
// (RFC 3339), in every clone of a configured repo. A clone that cannot be read
// is skipped: this list only adds to what GitHub reports.
func LocalCommits(cfg Config, since, until string) []Commit {
	var todo []repoClone
	for _, c := range cloneList(cfg.RepoFolders) {
		if MatchesConfig(c.repo, cfg.Repos) {
			todo = append(todo, c)
		}
	}
	results := make([][]Commit, len(todo))
	sem := make(chan struct{}, localScanLimit())
	var wg sync.WaitGroup
	for i, c := range todo {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = unpushedCommits(c.dir, c.repo, since, until)
		}()
	}
	wg.Wait()
	var out []Commit
	for _, r := range results {
		out = append(out, r...)
	}
	return out
}

// unpushedCommits is one clone's commits by its configured user.email that
// are on a local branch and on no remote-tracking one.
func unpushedCommits(dir, repo, since, until string) []Commit {
	email, err := git(dir, "config", "user.email")
	if err != nil || strings.TrimSpace(email) == "" {
		return nil
	}
	out, err := Git(dir, nil, "", "log", "--branches", "--not", "--remotes", "--source", "--no-merges",
		"--author=<"+email+">", "--since="+since, "--until="+until,
		"--format=%H%x1f%S%x1f%aI%x1f%B%x1e")
	if err != nil {
		return nil
	}
	var got []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		f := strings.SplitN(strings.TrimLeft(rec, "\n"), "\x1f", 4)
		if len(f) != 4 || f[0] == "" {
			continue
		}
		full := strings.TrimRight(f[3], "\n")
		first := full
		if i := strings.IndexByte(full, '\n'); i >= 0 {
			first = full[:i]
		}
		got = append(got, Commit{
			Sha: Short(f[0]), Repo: repo, Date: CommitWorkDate(f[2]),
			Message: first, Full: full, Issue: ParseIssue(full, repo),
			Branch: strings.TrimPrefix(f[1], "refs/heads/"), Local: true,
		})
	}
	return got
}

// mergeLocal adds the local commits GitHub did not report. GitHub's copy of a
// commit wins: once it is there, it is pushed.
func mergeLocal(commits, local []Commit) []Commit {
	seen := map[string]bool{}
	for _, c := range commits {
		seen[CommitRef{Repo: c.Repo, Sha: Short(c.Sha)}.Key()] = true
	}
	for _, c := range local {
		k := CommitRef{Repo: c.Repo, Sha: c.Sha}.Key()
		if !seen[k] {
			seen[k] = true
			commits = append(commits, c)
		}
	}
	return commits
}

// localKeys is the set of commits only on this machine, for tagging the saved
// entries they belong to.
func localKeys(commits []Commit) map[string]bool {
	keys := map[string]bool{}
	for _, c := range commits {
		if c.Local {
			keys[CommitRef{Repo: c.Repo, Sha: c.Sha}.Key()] = true
		}
	}
	return keys
}
