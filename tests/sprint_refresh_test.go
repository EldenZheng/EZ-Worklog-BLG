package tests

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"worklog/internal/shared/backend"
)

func TestRefreshSprintScopesReadsAndPreservesOtherItems(t *testing.T) {
	saved := backend.Gh
	defer func() { backend.Gh = saved }()
	sprint := backend.SprintIteration{ID: "S1", Title: "Sprint One", StartDate: "2099-01-01", Duration: 14}
	field := backend.SprintField{Name: "Sprint"}
	field.Configuration.Iterations = []backend.SprintIteration{sprint}
	b := backend.SprintBoard{ID: "P1", Filter: "assignee:@me -status:Done sprint:@current", Fields: []backend.SprintField{field}, Items: []backend.SprintItem{
		{ID: "removed", SprintID: "S1"}, {ID: "moved-in", SprintID: "S2"}, {ID: "untouched", SprintID: "S2"},
	}}
	calls := 0
	backend.Gh = func(args []string, _ ...string) (string, error) {
		calls++
		query := strings.Join(args, " ")
		if !strings.Contains(query, `filter=assignee:@me -status:Done sprint:"Sprint One"`) || strings.Contains(query, "projectV2(number") {
			t.Fatalf("unscoped request: %s", query)
		}
		return `{"data":{"node":{"items":{"nodes":[{"id":"moved-in","content":{"__typename":"Issue","title":"Fresh"},"fieldValues":{"nodes":[{"iterationId":"S1","field":{"name":"Sprint"}}]}}],"pageInfo":{"hasNextPage":false}}}}}`, nil
	}
	got, err := backend.RefreshSprint(b, backend.SprintKey(sprint), nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(got.Items) != 2 || got.Items[0].Title != "Fresh" || got.Items[1].ID != "untouched" {
		t.Fatalf("calls=%d items=%+v", calls, got.Items)
	}
	if len(b.Items) != 3 || b.Items[0].ID != "removed" {
		t.Fatal("mutated original board")
	}
	backend.Gh = func([]string, ...string) (string, error) { return "", errors.New("offline") }
	got, err = backend.RefreshSprint(b, backend.SprintKey(sprint), nil)
	if err == nil || !reflect.DeepEqual(got, b) {
		t.Fatal("failed refresh must preserve board")
	}
}
