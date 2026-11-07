package backend

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var branchIssueNumber = regexp.MustCompile(`(?i)(?:^|/)(?:issues?[/_-])([1-9][0-9]*)(?:$|[/_-])`)
var branchIssueSlug = regexp.MustCompile(`(?:^|/)([1-9][0-9]*)-[a-zA-Z]`)

// ParseBranchIssue recognises explicit references and GitHub's numbered issue
// branch names. Ordinary branch names and release version numbers are not IDs.
func ParseBranchIssue(branch, repo string) string {
	branch = strings.TrimPrefix(strings.TrimSpace(branch), "refs/heads/")
	if ref := ParseIssue(branch, repo); ref != "" {
		return ref
	}
	if repo != "" {
		for _, pattern := range []*regexp.Regexp{branchIssueNumber, branchIssueSlug} {
			if m := pattern.FindStringSubmatch(branch); m != nil {
				return fmt.Sprintf("%s#%s", repo, m[1])
			}
		}
	}
	return ""
}

// ResolveCommitBranchIssues fills only missing issue declarations. Scans can
// encounter the same commit on several branches, so resolve before deduping and
// leave conflicting branch candidates unfiled rather than depending on which
// concurrent scan finished first.
func ResolveCommitBranchIssues(commits []Commit) {
	type identity struct{ repo, sha string }
	candidates := map[identity]map[string]string{}
	explicit := map[identity]string{}
	branches := map[PushRef]string{}
	for _, c := range commits {
		key := identity{c.Repo, c.Sha}
		if c.Issue != "" {
			explicit[key] = c.Issue
		}
	}
	for _, c := range commits {
		key := identity{c.Repo, c.Sha}
		if explicit[key] != "" || c.Branch == "" || IsMergeCommit(c) {
			continue
		}
		branch := PushRef{Repo: c.Repo, Ref: c.Branch}
		ref, known := branches[branch]
		if !known {
			ref = ParseBranchIssue(c.Branch, c.Repo)
			if ref == "" && !commonIntegrationBranch(c.Branch) {
				ref = issueFromBranchPR(c.Repo, c.Branch)
			}
			branches[branch] = ref
		}
		if ref != "" {
			if candidates[key] == nil {
				candidates[key] = map[string]string{}
			}
			candidates[key][strings.ToLower(ref)] = ref
		}
	}
	for i, c := range commits {
		key := identity{c.Repo, c.Sha}
		if ref := explicit[key]; ref != "" {
			commits[i].Issue = ref
		} else if len(candidates[key]) == 1 {
			for _, ref := range candidates[key] {
				commits[i].Issue = ref
			}
		}
	}
}

func commonIntegrationBranch(branch string) bool {
	switch strings.ToLower(strings.TrimPrefix(branch, "refs/heads/")) {
	case "main", "master", "develop", "development", "dev", "staging", "production":
		return true
	}
	return false
}

// A named feature branch may carry only an issue's title. Its PR's linked
// issues provide the identity without guessing from similar issue names.
func issueFromBranchPR(repo, branch string) string {
	out, err := Gh([]string{"pr", "list", "--repo", repo, "--head", branch,
		"--state", "all", "--limit", "100", "--json", "title,body,closingIssuesReferences"})
	if err != nil {
		return ""
	}
	var prs []struct {
		Title, Body             string
		ClosingIssuesReferences []struct{ URL string }
	}
	if json.Unmarshal([]byte(out), &prs) != nil {
		return ""
	}
	refs := map[string]string{}
	add := func(ref string) {
		if ref != "" {
			refs[strings.ToLower(ref)] = ref
		}
	}
	for _, pr := range prs {
		for _, issue := range pr.ClosingIssuesReferences {
			ref, _ := IssueRefFromAny(issue.URL)
			add(ref)
		}
		for _, text := range []string{pr.Title, pr.Body} {
			for _, ref := range IssueRefsInText(text) {
				add(ref)
			}
			// A bare #123 is scoped to the PR repository.
			if len(IssueRefsInText(text)) == 0 {
				for _, match := range hashRe.FindAllStringSubmatch(text, -1) {
					add(repo + "#" + match[1])
				}
			}
		}
	}
	if len(refs) == 1 {
		for _, ref := range refs {
			return ref
		}
	}
	return ""
}
