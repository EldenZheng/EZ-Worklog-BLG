package backend

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Offering a pull request once a branch's work is all logged.
//
// The title is the issue the branch is for (owner/repo#N). The description
// opens "Refs owner/repo#N:" and lists the commits' messages, a line each as
// points, without the Refs lines the commits carry for their Worklogs. A pull
// request goes into the repository's default branch, on the repository the
// clone is of: a fork (the senwave ones) merges into its own default branch,
// customization/senwave, never into the original's.

// PullRequestPlan is what Create PR would open, or the pull request already
// open for the branch.
type PullRequestPlan struct {
	Repo, Branch, Base string
	Title, Body        string
	// Existing is the URL of a pull request already open from Branch.
	Existing string
	// Unlogged counts the user's commits on the branch not logged yet.
	Unlogged int
}

// LoggedTest says whether a commit is logged: saved in an entry, or naming a
// Worklog an entry pushed.
func (s *Store) LoggedTest() (func(sha, message string) bool, error) {
	rows, err := s.ReadRows()
	if err != nil {
		return nil, err
	}
	shas := map[string]bool{}
	var worklogs []string
	for _, r := range rows {
		for _, c := range ParseCommitRefs(r["refs"]) {
			shas[strings.ToLower(Short(c.Sha))] = true
		}
		if ref := strings.TrimPrefix(WorklogRef(r["issue_url"]), "Refs "); ref != "" {
			worklogs = append(worklogs, ref)
		}
	}
	return func(sha, message string) bool {
		if shas[strings.ToLower(Short(sha))] {
			return true
		}
		for _, ref := range worklogs {
			if MessageNamesIssue(message, ref) {
				return true
			}
		}
		return false
	}, nil
}

// PlanPullRequest reads branch in the clone at dir: its commits past the
// default branch, as GitHub has them, and any pull request open from it.
func PlanPullRequest(dir, repo, branch, issue string, logged func(sha, message string) bool) (PullRequestPlan, error) {
	p := PullRequestPlan{Repo: repo, Branch: branch, Title: issue}
	p.Base = defaultBranch(dir)
	if p.Base == "" {
		out, err := Gh([]string{"repo", "view", repo, "--json", "defaultBranchRef", "--jq", ".defaultBranchRef.name"})
		if err != nil {
			return p, err
		}
		p.Base = strings.TrimSpace(out)
	}
	if p.Base == "" || p.Base == branch {
		return p, errors.New(branch + " is the default branch; there is nothing to open a pull request from.")
	}
	email, _ := git(dir, "config", "user.email")
	out, err := Git(dir, nil, "", "log", "--reverse", "--no-merges", "--format=%H%x1f%ae%x1f%B%x1e",
		"refs/remotes/origin/"+p.Base+"..refs/remotes/origin/"+branch)
	if err != nil {
		return p, err
	}
	var messages []string
	for _, rec := range strings.Split(out, "\x1e") {
		f := strings.SplitN(strings.TrimLeft(rec, "\n"), "\x1f", 3)
		if len(f) != 3 || f[0] == "" {
			continue
		}
		// Other people's commits on the branch are theirs to log.
		if email != "" && !strings.EqualFold(f[1], email) {
			continue
		}
		if !logged(f[0], f[2]) {
			p.Unlogged++
		}
		messages = append(messages, f[2])
	}
	if len(messages) == 0 {
		return p, errors.New(branch + " has no commits of yours past " + p.Base + ".")
	}
	p.Body = PullRequestBody(issue, messages)
	open, err := Gh([]string{"pr", "list", "--repo", repo, "--head", branch, "--state", "open",
		"--json", "url", "--jq", ".[0].url // empty"})
	if err != nil {
		return p, err
	}
	p.Existing = strings.TrimSpace(open)
	return p, nil
}

// A line that only names issues ("Refs owner/repo#6001"), or the prefix of
// one that goes on ("Refs owner/repo#5185: fix the totals").
var issueRefsPrefix = regexp.MustCompile(`(?i)^(?:(?:refs?|closes|fixes|resolves)\s+)?(?:[\w.-]+/[\w.-]+)?#\d+(?:\s*,\s*(?:[\w.-]+/[\w.-]+)?#\d+)*\s*:?\s*`)

// PullRequestBody is "Refs issue:" and then each line of the commits'
// messages as a point, without the references they carry. A line that is
// already a point keeps its text; repeats are listed once.
func PullRequestBody(issue string, messages []string) string {
	lines := []string{"Refs " + issue + ":"}
	seen := map[string]bool{}
	for _, m := range messages {
		for _, line := range strings.Split(m, "\n") {
			t := strings.TrimSpace(line)
			if loc := issueRefsPrefix.FindStringIndex(t); loc != nil && loc[0] == 0 {
				t = strings.TrimSpace(t[loc[1]:])
			}
			for _, bullet := range []string{"- ", "* ", "• "} {
				if strings.HasPrefix(t, bullet) {
					t = strings.TrimSpace(t[len(bullet):])
					break
				}
			}
			if t == "" || seen[strings.ToLower(t)] {
				continue
			}
			seen[strings.ToLower(t)] = true
			lines = append(lines, "- "+t)
		}
	}
	return strings.Join(lines, "\n")
}

// CompareURL is GitHub's page for opening the pull request by hand, with the
// title and description filled in.
func (p PullRequestPlan) CompareURL() string {
	q := url.Values{}
	q.Set("expand", "1")
	q.Set("title", p.Title)
	q.Set("body", p.Body)
	return "https://github.com/" + p.Repo + "/compare/" + pathRef(p.Base) + "..." + pathRef(p.Branch) + "?" + q.Encode()
}

// pathRef escapes a branch name for a URL path, keeping its slashes.
func pathRef(ref string) string {
	parts := strings.Split(ref, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

// CreatePullRequest opens the pull request and returns its URL. The repo is
// named outright: in a fork, gh would otherwise aim at the original.
func CreatePullRequest(p PullRequestPlan) (string, error) {
	out, err := Gh([]string{"pr", "create", "--repo", p.Repo, "--base", p.Base, "--head", p.Branch,
		"--title", p.Title, "--body", p.Body})
	if err != nil {
		return "", err
	}
	return lastLine(out), nil
}
