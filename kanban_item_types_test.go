package main

import (
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestSprintItemTypeFilterPersistsAndKeepsCalendarIndependent(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	b := &k.plan.Boards[0]
	b.Items[0].URL = "https://github.com/bigledger/app/issues/12"
	pr := sprintItem{ID: "pr", Title: "Pull request", URL: "https://github.com/bigledger/app/pull/13", SprintID: c.Sprint.ID}
	b.Items = append(b.Items, pr)
	k.draw()
	if !k.matches(*b, b.Items[0]) || !k.matches(*b, pr) || k.combinedSprints(true)[0].Count != 2 {
		t.Fatal("both types must be visible by default")
	}
	columns := []string{"Created", "Done"}
	k.plan.KanbanColumns = &columns
	k.plan.CalendarMode = "Month"
	// Toggle the issue icon legend itself, rather than a hidden checkbox.
	test.Tap(k.typeChoiceView.(*fyne.Container).Objects[0].(*tappable))
	if k.matches(*b, b.Items[0]) || !k.matches(*b, pr) || k.combinedSprints(true)[0].Count != 1 {
		t.Fatal("type filter did not update board or sprint counts")
	}
	if len(k.calendarItems()) != 2 {
		t.Fatal("Kanban type filter changed the separate calendar")
	}
	ui.buildKanbanTab()
	k = ui.kanban
	if k.plan.KanbanTypes == nil || !reflect.DeepEqual(*k.plan.KanbanTypes, []string{kanbanPullRequests}) || !reflect.DeepEqual(*k.plan.KanbanColumns, columns) || k.calendarMode != "Month" {
		t.Fatal("type preference did not restore or disturbed existing preferences")
	}
	k.setItemTypes(nil)
	ui.buildKanbanTab()
	k = ui.kanban
	if k.plan.KanbanTypes == nil || len(*k.plan.KanbanTypes) != 0 || k.visibleItemType(pr) {
		t.Fatal("empty type selection was not persisted")
	}
	k.setItemTypes([]string{kanbanIssues, kanbanPullRequests})
	if !k.visibleItemType(pr) {
		t.Fatal("types could not be re-enabled")
	}
}

func TestSprintIssueAndPRCardsHaveDistinctIconsAndLinks(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.kanban
	k.filter = "all"
	b := k.plan.Boards[0]
	for _, tc := range []struct {
		item                  sprintItem
		kind, icon, link, ref string
	}{
		{sprintItem{URL: "https://github.com/owner/repo/issues/12", Kind: "Issue"}, kanbanIssues, "kanban-issue.svg", "Open issue", "owner/repo#12"},
		{sprintItem{URL: "https://github.com/owner/repo/pull/13?tab=files"}, kanbanPullRequests, "kanban-pull-request.svg", "Open PR", "owner/repo#13"},
		{sprintItem{Kind: "PullRequest"}, kanbanPullRequests, "kanban-pull-request.svg", "", projectShortName(b)},
	} {
		if tc.item.itemType() != tc.kind || tc.item.repositoryNumber(b) != tc.ref {
			t.Fatal("incorrect type or numbered repository", tc.item)
		}
		iconFound, linkFound := false, tc.link == ""
		walkCanvas(k.cardContents(b, tc.item), func(o fyne.CanvasObject) {
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
	if k.visibleItemType(sprintItem{Kind: "DraftIssue"}) {
		t.Fatal("unnumbered project drafts should not appear as GitHub issues")
	}
}
