package backend

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

const KanbanIssues = "Issues"

const KanbanPullRequests = "Pull requests"

var kanbanIssueIcon = theme.NewThemedResource(fyne.NewStaticResource("kanban-issue.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16"><path fill="#000" fill-rule="evenodd" d="M8 1 C11.866 1 15 4.134 15 8 C15 11.866 11.866 15 8 15 C4.134 15 1 11.866 1 8 C1 4.134 4.134 1 8 1 Z M8 2.5 C4.962 2.5 2.5 4.962 2.5 8 C2.5 11.038 4.962 13.5 8 13.5 C11.038 13.5 13.5 11.038 13.5 8 C13.5 4.962 11.038 2.5 8 2.5 Z M8 6.5 C8.828 6.5 9.5 7.172 9.5 8 C9.5 8.828 8.828 9.5 8 9.5 C7.172 9.5 6.5 8.828 6.5 8 C6.5 7.172 7.172 6.5 8 6.5 Z"/></svg>`)))

var kanbanPullRequestIcon = theme.NewThemedResource(fyne.NewStaticResource("kanban-pull-request.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16"><path fill="#000" fill-rule="evenodd" d="M4 .5 C7.333 .5 7.333 5.5 4 5.5 C.667 5.5 .667 .5 4 .5 Z M4 2 C2.667 2 2.667 4 4 4 C5.333 4 5.333 2 4 2 Z M4 10.5 C7.333 10.5 7.333 15.5 4 15.5 C.667 15.5 .667 10.5 4 10.5 Z M4 12 C2.667 12 2.667 14 4 14 C5.333 14 5.333 12 4 12 Z M12 10.5 C15.333 10.5 15.333 15.5 12 15.5 C8.667 15.5 8.667 10.5 12 10.5 Z M12 12 C10.667 12 10.667 14 12 14 C13.333 14 13.333 12 12 12 Z"/><path fill="#000" d="M3.25 5.5 H4.75 V10.5 H3.25 Z M9 0 L6 3 L9 6 V3.75 C10.243 3.75 11.25 4.757 11.25 6 V10.5 H12.75 V6 C12.75 3.929 11.071 2.25 9 2.25 Z"/></svg>`)))
