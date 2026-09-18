package main

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

var (
	pullURLRe = regexp.MustCompile(`(?i)^https://github\.com/([\w.-]+)/([\w.-]+)/pull/(\d+)(?:[/#?].*)?$`)
	// Issue references occur in PR titles, bodies and branch names in both of
	// the forms used across the BigLedger repositories.
	issueRefAnywhereRe = regexp.MustCompile(`(?i)([\w.-]+)/([\w.-]+)(?:/issues/|#)(\d+)`)
)

func canonicalPullRequestURL(raw string) (string, bool) {
	match := pullURLRe.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return "", false
	}
	return fmt.Sprintf("https://github.com/%s/%s/pull/%s", match[1], match[2], match[3]), true
}

// parseCodeReviewItems reads one PR per line. Minutes are deliberately kept on
// each PR rather than entered as one unexplained total: the daily issue shows
// where its time went, and the Worklog minutes can be derived without drift.
func parseCodeReviewItems(text string) ([]CodeReviewItem, error) {
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
		canonical, ok := canonicalPullRequestURL(url)
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

func codeReviewBody(items []CodeReviewItem) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("%s (%dm)", item.URL, item.Minutes))
	}
	return strings.Join(lines, "\n")
}

func codeReviewMinutes(items []CodeReviewItem) int {
	total := 0
	for _, item := range items {
		total += item.Minutes
	}
	return total
}

// issueRefsInText extracts all cross-repository issue references while keeping
// their first-seen order. This is the fallback for PRs that say "Refs ..." in
// their title/body/branch but do not populate GitHub's closing-issues list.
func issueRefsInText(text string) []string {
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

type codeReviewLookup struct {
	URL       string
	Title     string
	IssueRefs []string
}

// fetchCodeReviewLookup resolves the issue(s) named by a PR. GitHub's typed
// closingIssuesReferences is preferred; branch/title/body text covers the
// common non-closing "Refs owner/repo/issues/123" convention.
func fetchCodeReviewLookup(prURL string) (codeReviewLookup, error) {
	out, err := gh([]string{
		"pr", "view", prURL, "--json",
		"title,body,url,headRefName,closingIssuesReferences",
	})
	if err != nil {
		return codeReviewLookup{URL: prURL}, err
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
		return codeReviewLookup{URL: prURL}, ghErr("could not read PR data: %v", err)
	}
	lookup := codeReviewLookup{URL: orDefault(got.URL, prURL), Title: got.Title}
	seen := map[string]bool{}
	add := func(ref string) {
		key := strings.ToLower(strings.TrimSpace(ref))
		if key != "" && !seen[key] {
			seen[key] = true
			lookup.IssueRefs = append(lookup.IssueRefs, ref)
		}
	}
	for _, issue := range got.Closing {
		if ref, err := issueRefFromAny(issue.URL); err == nil {
			add(ref)
		}
	}
	for _, text := range []string{got.Head, got.Title, got.Body} {
		for _, ref := range issueRefsInText(text) {
			add(ref)
		}
	}
	return lookup, nil
}

// resolveSingleCodeReviewTarget accepts either the PR being reviewed or the
// target issue directly. A PR must name exactly one issue: silently picking one
// of several would file time against the wrong work item.
func resolveSingleCodeReviewTarget(raw string) (string, codeReviewLookup, error) {
	if prURL, ok := canonicalPullRequestURL(raw); ok {
		lookup, err := fetchCodeReviewLookup(prURL)
		if err != nil {
			return "", lookup, err
		}
		switch len(lookup.IssueRefs) {
		case 0:
			return "", lookup, ghErr("This PR does not reference an issue. Add a Refs line to the PR, or paste the issue link here instead.")
		case 1:
			return lookup.IssueRefs[0], lookup, nil
		default:
			return "", lookup, ghErr("This PR references more than one issue (%s). Paste the issue you want to worklog under.", strings.Join(lookup.IssueRefs, ", "))
		}
	}
	ref, err := issueRefFromAny(raw)
	return ref, codeReviewLookup{}, err
}
