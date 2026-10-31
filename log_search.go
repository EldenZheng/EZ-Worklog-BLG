package main

import "strings"

func matchesSearch(query string, values ...string) bool {
	text := strings.ToLower(strings.Join(values, " "))
	for _, word := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}
func (ui *UI) pendingMatchesSearch(g Group) bool {
	values := []string{g.Issue, g.Remarks, ui.issueInfo[g.Issue].Title, g.Date}
	for _, c := range g.Commits {
		values = append(values, c.Sha, c.Repo, c.Message, c.Full)
	}
	return matchesSearch(ui.logSearch, values...)
}
func (ui *UI) rowMatchesSearch(r Row) bool {
	var values []string
	for _, v := range r {
		values = append(values, v)
	}
	values = append(values, ui.issueInfo[r["issue"]].Title)
	return matchesSearch(ui.logSearch, values...)
}
func (ui *UI) worklogMatchesSearch(it WorklogItem) bool {
	return matchesSearch(ui.logSearch, it.Title, it.ParentTitle, it.URL, it.ParentURL, it.Remarks, it.Date)
}
