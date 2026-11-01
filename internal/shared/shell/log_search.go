package shell

import (
	"strings"

	"worklog/internal/shared/backend"
)

func matchesSearch(query string, values ...string) bool {
	text := strings.ToLower(strings.Join(values, " "))
	for _, word := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}

func (ui *UI) PendingMatchesSearch(g backend.Group) bool {
	values := []string{g.Issue, g.Remarks, ui.IssueInfo[g.Issue].Title, g.Date}
	for _, c := range g.Commits {
		values = append(values, c.Sha, c.Repo, c.Message, c.Full)
	}
	return matchesSearch(ui.LogSearch, values...)
}

func (ui *UI) RowMatchesSearch(r backend.Row) bool {
	var values []string
	for _, v := range r {
		values = append(values, v)
	}
	values = append(values, ui.IssueInfo[r["issue"]].Title)
	return matchesSearch(ui.LogSearch, values...)
}

func (ui *UI) WorklogMatchesSearch(it backend.WorklogItem) bool {
	return matchesSearch(ui.LogSearch, it.Title, it.ParentTitle, it.URL, it.ParentURL, it.Remarks, it.Date)
}
