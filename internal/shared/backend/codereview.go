package backend

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CodeReviewItem is one PR in a daily code-review issue. The issue body uses
// this same spelling, which makes a pasted list both the input and the
// finished GitHub description:
//
//	https://github.com/owner/repo/pull/123 (10m)
//	Issue: [What the issue is called](https://github.com/owner/repo/issues/45)
//	- what was reviewed on it
//
// The Issue line names the issue(s) the PR is for and is optional; the "- "
// lines are the PR's notes. The issue description carries both; the board's
// Worklog Remarks keep only the compact PR list, which fits its cap.
type CodeReviewItem struct {
	URL     string
	Minutes int
	Issues  []ReviewIssue
	Notes   []string
}

// ReviewIssue is an issue a reviewed PR is for: owner/repo#N and its title.
type ReviewIssue struct {
	Ref, Title string
}

// reviewIssueLine is "Issue: [title](link)" for a PR's issues, or "" with none.
func reviewIssueLine(issues []ReviewIssue) string {
	var links []string
	for _, issue := range issues {
		owner, repo, num, err := SplitIssue(issue.Ref)
		if err != nil {
			continue
		}
		text := strings.TrimSpace(issue.Title)
		if text == "" {
			text = issue.Ref
		}
		text = strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(text)
		links = append(links, fmt.Sprintf("[%s](https://github.com/%s/%s/issues/%d)", text, owner, repo, num))
	}
	switch len(links) {
	case 0:
		return ""
	case 1:
		return "Issue: " + links[0]
	}
	return "Issues: " + strings.Join(links, ", ")
}

var (
	reviewIssueLineRe = regexp.MustCompile(`(?i)^issues?\s*:\s*(.*)$`)
	markdownLinkRe    = regexp.MustCompile(`\[((?:\\.|[^\]\\])*)\]\(([^)\s]+)\)`)
)

// readReviewIssues reads an "Issue: [title](link), …" line. Links written
// bare, or as owner/repo#N, are read too.
func readReviewIssues(line string) ([]ReviewIssue, bool) {
	m := reviewIssueLineRe.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return nil, false
	}
	var issues []ReviewIssue
	rest := m[1]
	for _, link := range markdownLinkRe.FindAllStringSubmatch(rest, -1) {
		if ref, err := IssueRefFromAny(link[2]); err == nil {
			title := strings.NewReplacer(`\\`, `\`, `\[`, "[", `\]`, "]").Replace(link[1])
			if strings.EqualFold(title, ref) {
				title = ""
			}
			issues = append(issues, ReviewIssue{Ref: ref, Title: title})
		}
	}
	for _, part := range strings.Split(markdownLinkRe.ReplaceAllString(rest, ""), ",") {
		if ref, err := IssueRefFromAny(strings.TrimSpace(part)); err == nil {
			issues = append(issues, ReviewIssue{Ref: ref})
		}
	}
	return issues, true
}

// cutReviewIssues takes an older "[issue link, …]" off a PR line's link part
// and reads the issues in it.
func cutReviewIssues(link string) (string, []ReviewIssue) {
	link = strings.TrimSpace(link)
	open := strings.LastIndex(link, "[")
	if open < 0 || !strings.HasSuffix(link, "]") {
		return link, nil
	}
	var issues []ReviewIssue
	for _, part := range strings.Split(link[open+1:len(link)-1], ",") {
		if ref, err := IssueRefFromAny(strings.TrimSpace(part)); err == nil {
			issues = append(issues, ReviewIssue{Ref: ref})
		}
	}
	return strings.TrimSpace(link[:open]), issues
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
		if note, ok := reviewNoteLine(line); ok {
			if len(items) == 0 {
				return nil, fmt.Errorf("line %d is a note, but no PR link comes before it", i+1)
			}
			if note != "" {
				items[len(items)-1].Notes = append(items[len(items)-1].Notes, note)
			}
			continue
		}
		if issues, ok := readReviewIssues(line); ok {
			if len(items) == 0 {
				return nil, fmt.Errorf("line %d names an issue, but no PR link comes before it", i+1)
			}
			items[len(items)-1].Issues = append(items[len(items)-1].Issues, issues...)
			continue
		}
		open := strings.LastIndex(line, "(")
		if open < 0 || !strings.HasSuffix(line, ")") {
			return nil, fmt.Errorf("line %d needs minutes after the PR link, for example (10m)", i+1)
		}
		url, issues := cutReviewIssues(line[:open])
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
			Issues:  issues,
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("paste at least one PR link")
	}
	return items, nil
}

// reviewNoteLine reads a "- note" (or "* note", "• note") line under a PR.
func reviewNoteLine(line string) (string, bool) {
	for _, bullet := range []string{"-", "*", "•"} {
		if rest, ok := strings.CutPrefix(line, bullet); ok {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

// CodeReviewBody is the full PR list, each PR followed by its notes. It is the
// daily issue's description. With notes, a blank line parts one PR from the
// next so each reads as its own block.
func CodeReviewBody(items []CodeReviewItem) string {
	noted := false
	for _, item := range items {
		noted = noted || len(item.Notes) > 0
	}
	lines := make([]string, 0, len(items))
	for i, item := range items {
		if noted && i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, fmt.Sprintf("%s (%dm)", item.URL, item.Minutes))
		if issue := reviewIssueLine(item.Issues); issue != "" {
			lines = append(lines, issue)
		}
		for _, note := range item.Notes {
			lines = append(lines, "- "+note)
		}
	}
	return strings.Join(lines, "\n")
}

// CodeReviewList is the PR list without notes, for the board's Worklog
// Remarks, which are capped at RemarkCap.
func CodeReviewList(items []CodeReviewItem) string {
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
	// The linked issues and the branch name say what the PR is for. The title,
	// then the body, are read only when those say nothing: they also mention
	// related PRs and earlier issues, which are not what was reviewed.
	for _, text := range []string{got.Head, got.Title, got.Body} {
		if len(lookup.IssueRefs) > 0 {
			break
		}
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

// pastedPRRe finds a PR link in pasted text, with the "[issue link]" and the
// "(10m)" after it when there are.
var pastedPRRe = regexp.MustCompile(`(?i)(?:https?://)?(?:www\.)?github\.com/[\w.-]+/[\w.-]+/pulls?/\d+[^\s()\[]*(?:\s*\[([^\]]*)\])?(?:\s*\(\s*(\d+)\s*m[a-z]*\s*\))?`)

// pastedIssueRe finds the "Issue: [title](link), …" line under a pasted PR.
var pastedIssueRe = regexp.MustCompile(`(?i)issues?\s*:\s*\[(?:\\.|[^\]\\])*\]\([^)\s]+\)(?:\s*,\s*\[(?:\\.|[^\]\\])*\]\([^)\s]+\))*`)

// pastedBulletRe parts a PR's points, on their own lines or run together on
// one: a one-line field joins pasted lines with spaces.
var pastedBulletRe = regexp.MustCompile(`(?:^|\s)[-*•]\s+`)

// ParsePastedReviews reads the PRs out of text pasted into a bulk review: each
// link, its "(10m)" and the "- " points after it. A link without minutes takes
// ten. Nil when there is no PR link at all.
func ParsePastedReviews(text string) []CodeReviewItem {
	locs := pastedPRRe.FindAllStringSubmatchIndex(text, -1)
	var items []CodeReviewItem
	for i, m := range locs {
		raw := text[m[0]:m[1]]
		if open := strings.IndexAny(raw, "(["); open >= 0 {
			raw = raw[:open]
		}
		url, ok := PullRequestURLFromAny(strings.TrimSpace(raw))
		if !ok {
			continue
		}
		var issues []ReviewIssue
		if m[2] >= 0 {
			_, issues = cutReviewIssues("[" + text[m[2]:m[3]] + "]")
		}
		mins := 10
		if m[4] >= 0 {
			if n, err := strconv.Atoi(text[m[4]:m[5]]); err == nil && n > 0 {
				mins = n
			}
		}
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		// The Issue line comes out first, so a title with " - " in it is
		// not taken for a point.
		rest := text[m[1]:end]
		if at := pastedIssueRe.FindStringIndex(rest); at != nil {
			found, _ := readReviewIssues(rest[at[0]:at[1]])
			issues = append(issues, found...)
			rest = rest[:at[0]] + " " + rest[at[1]:]
		}
		var notes []string
		for j, part := range pastedBulletRe.Split(strings.ReplaceAll(rest, "\n", " "), -1) {
			if part = strings.TrimSpace(part); part != "" && (j > 0 || len(notes) > 0) {
				notes = append(notes, part)
			}
		}
		items = append(items, CodeReviewItem{URL: url, Minutes: mins, Issues: issues, Notes: notes})
	}
	return items
}
