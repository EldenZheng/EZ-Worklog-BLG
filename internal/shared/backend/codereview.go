package backend

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CodeReviewItem is one line in a daily code-review issue. The issue body uses
// this same compact spelling, which makes a pasted list both the input and the
// finished GitHub description:
//
//	https://github.com/owner/repo/pull/123 (10m)
type CodeReviewItem struct {
	URL     string
	Minutes int
}

// Issue references occur in PR titles, bodies and branch names in both of the
// forms used across the ExampleOrg repositories.
var issueRefAnywhereRe = regexp.MustCompile(`(?i)([\w.-]+)/([\w.-]+)(?:/issues/|#)(\d+)`)

// CanonicalPullRequestURL recognises a pull request written as a path, with or
// without the host: https://github.com/owner/repo/pull/123/files,
// github.com/owner/repo/pull/123 or owner/repo/pull/123. The shorthand
// owner/repo#123 is not taken here, because the same spelling names an issue;
// see PullRequestURLFromAny and CodeReviewTargets for the places that can tell.
func CanonicalPullRequestURL(raw string) (string, bool) {
	m := githubPathRe.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil || strings.EqualFold(m[3], "issues") {
		return "", false
	}
	return fmt.Sprintf("https://github.com/%s/%s/pull/%s", m[1], m[2], m[4]), true
}

// PullRequestURLFromAny is CanonicalPullRequestURL for a field that only ever
// holds pull requests, such as the bulk review sheet. There owner/repo#123 can
// only mean a PR, so the shorthand is read as one too.
func PullRequestURLFromAny(raw string) (string, bool) {
	if url, ok := CanonicalPullRequestURL(raw); ok {
		return url, true
	}
	if m := issRe.FindStringSubmatch(strings.TrimSpace(raw)); m != nil {
		return fmt.Sprintf("https://github.com/%s/%s/pull/%s", m[1], m[2], m[3]), true
	}
	return "", false
}

// ParseCodeReviewItems reads one PR per line. Minutes are deliberately kept on
// each PR rather than entered as one unexplained total: the daily issue shows
// where its time went, and the Worklog minutes can be derived without drift.
func ParseCodeReviewItems(text string) ([]CodeReviewItem, error) {
	var items []CodeReviewItem
	for i, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		open := strings.LastIndex(line, "(")
		if open < 0 || !strings.HasSuffix(line, ")") {
			return nil, fmt.Errorf("line %d needs minutes after the PR link, for example (10m)", i+1)
		}
		url := strings.TrimSpace(line[:open])
		minuteText := strings.TrimSpace(line[open+1 : len(line)-1])
		minuteText = strings.TrimSpace(strings.TrimSuffix(strings.ToLower(minuteText), "minutes"))
		minuteText = strings.TrimSpace(strings.TrimSuffix(minuteText, "minute"))
		minuteText = strings.TrimSpace(strings.TrimSuffix(minuteText, "mins"))
		minuteText = strings.TrimSpace(strings.TrimSuffix(minuteText, "min"))
		minuteText = strings.TrimSpace(strings.TrimSuffix(minuteText, "m"))
		mins, err := strconv.Atoi(minuteText)
		if err != nil || mins <= 0 {
			return nil, fmt.Errorf("line %d has invalid minutes; use a value such as (10m)", i+1)
		}
		canonical, ok := PullRequestURLFromAny(url)
		if !ok {
			return nil, fmt.Errorf("line %d is not a GitHub pull-request link", i+1)
		}
		items = append(items, CodeReviewItem{
			URL:     canonical,
			Minutes: mins,
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("paste at least one PR link")
	}
	return items, nil
}

func CodeReviewBody(items []CodeReviewItem) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("%s (%dm)", item.URL, item.Minutes))
	}
	return strings.Join(lines, "\n")
}

func CodeReviewMinutes(items []CodeReviewItem) int {
	total := 0
	for _, item := range items {
		total += item.Minutes
	}
	return total
}

// IssueRefsInText extracts all cross-repository issue references while keeping
// their first-seen order. This is the fallback for PRs that say "Refs ..." in
// their title/body/branch but do not populate GitHub's closing-issues list.
func IssueRefsInText(text string) []string {
	seen := map[string]bool{}
	var refs []string
	for _, match := range issueRefAnywhereRe.FindAllStringSubmatch(text, -1) {
		ref := fmt.Sprintf("%s/%s#%s", match[1], match[2], match[3])
		key := strings.ToLower(ref)
		if !seen[key] {
			seen[key] = true
			refs = append(refs, ref)
		}
	}
	return refs
}

type CodeReviewLookup struct {
	URL       string
	Title     string
	IssueRefs []string
}

func CodeReviewTargetLabel(ref, title string) string {
	if title = strings.TrimSpace(title); title != "" {
		return fmt.Sprintf("Worklog issue: %s  ·  %s", title, ref)
	}
	return "Worklog issue: " + ref
}

// FetchCodeReviewLookup resolves the issue(s) named by a PR. GitHub's typed
// closingIssuesReferences is preferred; branch/title/body text covers the
// common non-closing "Refs owner/repo/issues/123" convention. A variable so
// tests can stand in for gh.
var FetchCodeReviewLookup = fetchCodeReviewLookup

func fetchCodeReviewLookup(prURL string) (CodeReviewLookup, error) {
	out, err := Gh([]string{
		"pr", "view", prURL, "--json",
		"title,body,url,headRefName,closingIssuesReferences",
	})
	if err != nil {
		return CodeReviewLookup{URL: prURL}, err
	}
	var got struct {
		Title   string `json:"title"`
		Body    string `json:"body"`
		URL     string `json:"url"`
		Head    string `json:"headRefName"`
		Closing []struct {
			URL string `json:"url"`
		} `json:"closingIssuesReferences"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		return CodeReviewLookup{URL: prURL}, GhErr("could not read PR data: %v", err)
	}
	lookup := CodeReviewLookup{URL: OrDefault(got.URL, prURL), Title: got.Title}
	seen := map[string]bool{}
	add := func(ref string) {
		key := strings.ToLower(strings.TrimSpace(ref))
		if key != "" && !seen[key] {
			seen[key] = true
			lookup.IssueRefs = append(lookup.IssueRefs, ref)
		}
	}
	for _, issue := range got.Closing {
		if ref, err := IssueRefFromAny(issue.URL); err == nil {
			add(ref)
		}
	}
	for _, text := range []string{got.Head, got.Title, got.Body} {
		for _, ref := range IssueRefsInText(text) {
			add(ref)
		}
	}
	return lookup, nil
}

// PullRequestForRef asks GitHub whether owner/repo#123 is a pull request, and
// returns its canonical link when it is. Issues and PRs share one numbering,
// so the shorthand alone cannot say. Any failure reads as "not a PR": the ref
// is then treated as the issue it looks like, which is what it was before.
// A variable so tests can stand in for gh.
var PullRequestForRef = func(ref string) (string, bool) {
	owner, repo, num, err := SplitIssue(ref)
	if err != nil {
		return "", false
	}
	out, err := Gh([]string{
		"api", fmt.Sprintf("repos/%s/%s/issues/%d", owner, repo, num),
		"--jq", `if .pull_request then "pr" else "issue" end`,
	})
	if err != nil || strings.TrimSpace(out) != "pr" {
		return "", false
	}
	return fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, num), true
}

// CodeReviewTargets lists the issues a single review could be filed under.
// The input is the PR being reviewed or the issue itself, in any spelling
// IssueRefFromAny or CanonicalPullRequestURL reads. owner/repo#123 is checked
// with GitHub, since it may be either.
//
// A PR that names several issues returns all of them, in the order the PR
// gives them, and the person picks one: choosing for them would file the time
// against the wrong work item as often as not. A PR that names none is an
// error, as there is nothing to file under.
func CodeReviewTargets(raw string) (CodeReviewLookup, error) {
	raw = strings.TrimSpace(raw)
	prURL, isPR := CanonicalPullRequestURL(raw)
	if !isPR && issRe.MatchString(raw) {
		prURL, isPR = PullRequestForRef(raw)
	}
	if isPR {
		lookup, err := FetchCodeReviewLookup(prURL)
		if err != nil {
			return lookup, err
		}
		if len(lookup.IssueRefs) == 0 {
			return lookup, GhErr("This PR does not reference an issue. Add a Refs line to the PR, or paste the issue link here instead.")
		}
		return lookup, nil
	}
	ref, err := IssueRefFromAny(raw)
	if err != nil {
		return CodeReviewLookup{}, err
	}
	return CodeReviewLookup{IssueRefs: []string{ref}}, nil
}
