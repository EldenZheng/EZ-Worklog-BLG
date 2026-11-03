package tests

import (
	"strings"
	"testing"

	"worklog/internal/shared/backend"
)

// A board item that is a sub-issue carries its parent's title and link, for the
// pill under the card's link; one with no parent carries none.
func TestSprintBoardReadsParentIssue(t *testing.T) {
	saved := backend.Gh
	defer func() { backend.Gh = saved }()
	backend.Gh = func(args []string, _ ...string) (string, error) {
		query := strings.Join(args, " ")
		if strings.Contains(query, "projectV2(number") {
			return `{"data":{"owner":{"projectV2":{"id":"P1","title":"Board","fields":{"nodes":[]}}}}}`, nil
		}
		return `{"data":{"node":{"items":{"nodes":[
			{"id":"I1","content":{"__typename":"Issue","title":"Child","url":"https://github.com/o/r/issues/2",
				"parent":{"title":"Epic","url":"https://github.com/o/r/issues/1"}},"fieldValues":{"nodes":[]}},
			{"id":"I2","content":{"__typename":"Issue","title":"Loose","url":"https://github.com/o/r/issues/3",
				"parent":null},"fieldValues":{"nodes":[]}}
		],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`, nil
	}
	b, err := backend.FetchSprintBoard("https://github.com/orgs/o/projects/1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(b.Items))
	}
	if got := b.Items[0]; got.ParentTitle != "Epic" || got.ParentURL != "https://github.com/o/r/issues/1" {
		t.Fatalf("child parent = %q %q", got.ParentTitle, got.ParentURL)
	}
	if got := b.Items[1]; got.ParentTitle != "" || got.ParentURL != "" {
		t.Fatalf("loose issue should have no parent, got %q %q", got.ParentTitle, got.ParentURL)
	}
}
