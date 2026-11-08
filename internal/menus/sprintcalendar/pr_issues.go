package sprintcalendar

import (
	"fmt"

	"fyne.io/fyne/v2"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// issueLink is the link a card or block shows for it: the item's own, except
// that a pull request links to the issue it works on, found the way Log Work's
// code review finds it (the PR's closing issues, then Refs in its branch,
// title and body). The answer is kept in the plan, so each PR is asked about
// once; until it lands, the PR's own link shows.
func issueLink(k *shell.KanbanUI, it backend.SprintItem) string {
	if it.URL == "" || it.ItemType() != backend.KanbanPullRequests {
		return it.URL
	}
	issue, known := k.Plan.PRIssues[it.URL]
	// Only while the calendar is open: a lookup is a GitHub call. An issue
	// saved without its title (by an earlier build) is asked about once more.
	if (!known || issue.URL != "" && issue.Title == "") && k.CalendarActive && !k.PRAsked[it.URL] {
		if k.PRAsked == nil {
			k.PRAsked = map[string]bool{}
		}
		k.PRAsked[it.URL] = true
		go lookUpPRIssue(k, it.URL)
	}
	return backend.OrDefault(issue.URL, it.URL)
}

func lookUpPRIssue(k *shell.KanbanUI, pr string) {
	lookup, err := backend.FetchCodeReviewLookup(pr)
	if err != nil {
		return // not remembered: asked again next session
	}
	var issue backend.PRIssue
	for _, ref := range lookup.IssueRefs {
		if owner, repo, n, err := backend.SplitIssue(ref); err == nil {
			issue.URL = fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, n)
			// The title is best-effort: the link is what matters.
			if info, err := backend.FetchIssueInfo(ref); err == nil {
				issue.Title = info.Title
			}
			break
		}
	}
	fyne.Do(func() {
		p := k.Plan
		p.PRIssues = make(map[string]backend.PRIssue, len(k.Plan.PRIssues)+1)
		for pr, issue := range k.Plan.PRIssues {
			p.PRIssues[pr] = issue
		}
		// A zero PRIssue remembers a PR that names no issue.
		p.PRIssues[pr] = issue
		if !k.Save(p) || issue.URL == "" || !k.CalendarActive {
			return
		}
		if k.CalendarDrag != nil || k.CalendarResizing {
			k.RedrawPending = true
			return
		}
		DrawKeepingCalendarScroll(k)
	})
}

// issueTitle is the title to show for it: for a pull request, the title of
// the issue it works on once known.
func issueTitle(k *shell.KanbanUI, it backend.SprintItem) string {
	if it.ItemType() == backend.KanbanPullRequests {
		if issue := k.Plan.PRIssues[it.URL]; issue.Title != "" {
			return issue.Title
		}
	}
	return it.Title
}
