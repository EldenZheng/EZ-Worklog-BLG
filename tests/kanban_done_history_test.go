package tests

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"worklog/internal/menus/kanban"
	"worklog/internal/shared/backend"
)

func doneHistoryBoard() backend.SprintBoard {
	today := backend.WorkDate(time.Now())
	field := backend.SprintField{Name: "Sprint"}
	field.Configuration.CompletedIterations = []backend.SprintIteration{
		{ID: "older", Title: "Older sprint", StartDate: backend.AddDays(today, -42), Duration: 14},
		{ID: "previous", Title: "Previous sprint", StartDate: backend.AddDays(today, -14), Duration: 14},
	}
	field.Configuration.Iterations = []backend.SprintIteration{
		{ID: "current", Title: "Current sprint", StartDate: today, Duration: 14},
		{ID: "future", Title: "Future sprint", StartDate: backend.AddDays(today, 14), Duration: 14},
	}
	return backend.SprintBoard{ID: "P", URL: "https://github.com/orgs/exampleorg/projects/1",
		Filter: `assignee:@me is:open -status:Done label:"release work"`, Fields: []backend.SprintField{
			{Name: "Status", Options: []backend.FieldOption{{Name: "Created"}, {Name: "Done"}}}, field,
		}}
}

func doneHistoryPage(t *testing.T, items []backend.SprintItem, more bool) string {
	t.Helper()
	var nodes []any
	for _, item := range items {
		kind, state := "Issue", "OPEN"
		if item.Kind != "" {
			kind = item.Kind
		}
		if item.Closed {
			state = "CLOSED"
		}
		nodes = append(nodes, map[string]any{"id": item.ID,
			"content": map[string]string{"__typename": kind, "title": item.Title, "state": state},
			"fieldValues": map[string]any{"nodes": []any{
				map[string]any{"name": item.Status, "field": map[string]string{"name": "Status"}},
				map[string]any{"iterationId": item.SprintID, "field": map[string]string{"name": "Sprint"}},
			}},
		})
	}
	raw, err := json.Marshal(map[string]any{"data": map[string]any{"node": map[string]any{"items": map[string]any{
		"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": more, "endCursor": "next-page"},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestKanbanFetchIncludesClosedDoneFromCurrentAndPreviousSprints(t *testing.T) {
	b := doneHistoryBoard()
	oldGh := backend.Gh
	defer func() { backend.Gh = oldGh }()
	filters := []string{}
	backend.Gh = func(args []string, _ ...string) (string, error) {
		query := strings.Join(args, " ")
		if strings.Contains(query, "projectV2(number") {
			raw, err := json.Marshal(map[string]any{"data": map[string]any{"owner": map[string]any{"projectV2": map[string]any{
				"id": b.ID, "title": "History", "fields": map[string]any{"nodes": b.Fields},
			}}}})
			return string(raw), err
		}
		filter := ""
		for _, arg := range args {
			if strings.HasPrefix(arg, "filter=") {
				filter = strings.TrimPrefix(arg, "filter=")
			}
		}
		filters = append(filters, filter)
		if filter == b.Filter {
			return doneHistoryPage(t, []backend.SprintItem{{ID: "open", Status: "Created", SprintID: "previous"}}, false), nil
		}
		if filter != `assignee:@me label:"release work" status:Done` {
			t.Fatalf("Done history lost source ownership/label constraints or stayed scoped to current: %q", filter)
		}
		if strings.Contains(query, "after=next-page") {
			return doneHistoryPage(t, []backend.SprintItem{
				{ID: "previous-done", Status: "Done", SprintID: "previous", Closed: true}, // duplicate
				{ID: "older-done", Status: "Done", SprintID: "older", Closed: true, Kind: "PullRequest"},
				{ID: "future-done", Status: "Done", SprintID: "future"},
				{ID: "unscheduled-done", Status: "Done"},
			}, false), nil
		}
		return doneHistoryPage(t, []backend.SprintItem{
			{ID: "current-done", Status: "Done", SprintID: "current", Closed: true},
			{ID: "previous-done", Status: "Done", SprintID: "previous", Closed: true},
		}, true), nil
	}
	done, total := 0, backend.SprintBoardSteps
	got, err := backend.FetchSprintBoardTracked(b.URL, b.Filter, func(d, more int) { done += d; total += more })
	if err != nil || len(got.Items) != 4 || len(filters) != 3 || done != total {
		t.Fatalf("history fetch: err=%v items=%+v filters=%v progress=%d/%d", err, got.Items, filters, done, total)
	}
	ui, _, _ := plannerFixture(t)
	k := ui.Kanban
	k.Plan.Boards = []backend.SprintBoard{got}
	k.Filter, k.Sprint = "sprint", backend.SprintKey(got.Iterations()[1])
	kanban.Draw(k)
	if cards := kanban.ColumnOrder(k, "Done"); len(cards) != 1 || cards[0].Item.ID != "previous-done" || !cards[0].Item.Closed {
		t.Fatalf("previous sprint's closed Done card must reach the Done column: %+v", cards)
	}
	found := false
	for _, group := range k.CombinedSprints(true) {
		if group.Sprint.ID == "previous" {
			found = group.Count == 2
		}
	}
	if !found {
		t.Fatal("previous sprint sidebar count must include open and Done work")
	}
}

func TestRefreshPreviousSprintFetchesDoneAndPreservesOtherSprints(t *testing.T) {
	b := doneHistoryBoard()
	b.Items = []backend.SprintItem{{ID: "current-keep", SprintID: "current"}, {ID: "previous-stale", SprintID: "previous"}}
	oldGh := backend.Gh
	defer func() { backend.Gh = oldGh }()
	filters := []string{}
	var mu sync.Mutex // a sprint's open and Done searches run side by side
	backend.Gh = func(args []string, _ ...string) (string, error) {
		filter := ""
		for _, arg := range args {
			if strings.HasPrefix(arg, "filter=") {
				filter = strings.TrimPrefix(arg, "filter=")
			}
		}
		mu.Lock()
		filters = append(filters, filter)
		mu.Unlock()
		if strings.Contains(filter, "status:Done") && !strings.Contains(filter, "-status:Done") {
			if filter != `assignee:@me label:"release work" status:Done sprint:"Previous sprint"` {
				t.Fatalf("Done refresh must target the selected previous sprint: %q", filter)
			}
			return doneHistoryPage(t, []backend.SprintItem{{ID: "done", SprintID: "previous", Status: "Done", Closed: true}}, false), nil
		}
		return doneHistoryPage(t, []backend.SprintItem{{ID: "fresh", SprintID: "previous", Status: "Created"}}, false), nil
	}
	got, err := backend.RefreshSprint(b, backend.SprintKey(b.Iterations()[1]), nil)
	if err != nil || len(filters) != 2 || len(got.Items) != 3 || got.Items[1].ID != "done" || got.Items[2].ID != "current-keep" {
		t.Fatalf("previous sprint refresh: err=%v filters=%v items=%+v", err, filters, got.Items)
	}
	backend.Gh = func(args []string, _ ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), " status:Done ") {
			return "", errors.New("Done pass failed")
		}
		return doneHistoryPage(t, nil, false), nil
	}
	got, err = backend.RefreshSprint(b, backend.SprintKey(b.Iterations()[1]), nil)
	if err == nil || !reflect.DeepEqual(got, b) {
		t.Fatal("a failed Done history pass must preserve all cached sprint items")
	}
}
