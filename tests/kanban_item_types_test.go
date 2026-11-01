package tests

import (
	"reflect"
	"strings"
	"testing"
	"worklog/internal/menus/kanban"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestSprintItemTypeFilterPersistsAndKeepsCalendarIndependent(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	b := &k.Plan.Boards[0]
	b.Items[0].URL = "https://github.com/bigledger/app/issues/12"
	pr := backend.SprintItem{ID: "pr", Title: "Pull request", URL: "https://github.com/bigledger/app/pull/13", SprintID: c.Sprint.ID}
	b.Items = append(b.Items, pr)
	kanban.Draw(k)
	if !kanban.Matches(k, *b, b.Items[0]) || !kanban.Matches(k, *b, pr) || k.CombinedSprints(true)[0].Count != 2 {
		t.Fatal("both types must be visible by default")
	}
	columns := []string{"Created", "Done"}
	k.Plan.KanbanColumns = &columns
	k.Plan.CalendarMode = "Month"
	// Toggle the issue icon legend itself, rather than a hidden checkbox.
	test.Tap(k.TypeChoiceView.(*fyne.Container).Objects[0].(*shell.Tappable))
	if kanban.Matches(k, *b, b.Items[0]) || !kanban.Matches(k, *b, pr) || k.CombinedSprints(true)[0].Count != 1 {
		t.Fatal("type filter did not update board or sprint counts")
	}
	if len(k.CalendarItems()) != 2 {
		t.Fatal("Kanban type filter changed the separate calendar")
	}
	kanban.BuildKanbanTab(ui)
	k = ui.Kanban
	if k.Plan.KanbanTypes == nil || !reflect.DeepEqual(*k.Plan.KanbanTypes, []string{backend.KanbanPullRequests}) || !reflect.DeepEqual(*k.Plan.KanbanColumns, columns) || k.CalendarMode != "Month" {
		t.Fatal("type preference did not restore or disturbed existing preferences")
	}
	kanban.SetItemTypes(k, nil)
	kanban.BuildKanbanTab(ui)
	k = ui.Kanban
	if k.Plan.KanbanTypes == nil || len(*k.Plan.KanbanTypes) != 0 || k.VisibleItemType(pr) {
		t.Fatal("empty type selection was not persisted")
	}
	kanban.SetItemTypes(k, []string{backend.KanbanIssues, backend.KanbanPullRequests})
	if !k.VisibleItemType(pr) {
		t.Fatal("types could not be re-enabled")
	}
}

func TestSprintIssueAndPRCardsHaveDistinctIconsAndLinks(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.Kanban
	k.Filter = "all"
	b := k.Plan.Boards[0]
	for _, tc := range []struct {
		item                  backend.SprintItem
		kind, icon, link, ref string
	}{
		{backend.SprintItem{URL: "https://github.com/owner/repo/issues/12", Kind: "Issue"}, backend.KanbanIssues, "kanban-issue.svg", "Open issue", "owner/repo#12"},
		{backend.SprintItem{URL: "https://github.com/owner/repo/pull/13?tab=files"}, backend.KanbanPullRequests, "kanban-pull-request.svg", "Open PR", "owner/repo#13"},
		{backend.SprintItem{Kind: "PullRequest"}, backend.KanbanPullRequests, "kanban-pull-request.svg", "", backend.ProjectShortName(b)},
	} {
		if tc.item.ItemType() != tc.kind || tc.item.RepositoryNumber(b) != tc.ref {
			t.Fatal("incorrect type or numbered repository", tc.item)
		}
		iconFound, linkFound := false, tc.link == ""
		walkCanvas(kanban.CardContents(k, b, tc.item), func(o fyne.CanvasObject) {
			if icon, ok := o.(*widget.Icon); ok && strings.HasSuffix(icon.Resource.Name(), tc.icon) {
				iconFound = true
			}
			if link, ok := o.(*widget.Hyperlink); ok && link.Text == tc.link && link.URL.String() == tc.item.URL {
				linkFound = true
			}
		})
		if !iconFound || !linkFound {
			t.Fatal("type-specific icon or direct link missing", tc.kind, iconFound, linkFound)
		}
	}
	if k.VisibleItemType(backend.SprintItem{Kind: "DraftIssue"}) {
		t.Fatal("unnumbered project drafts should not appear as GitHub issues")
	}
}
