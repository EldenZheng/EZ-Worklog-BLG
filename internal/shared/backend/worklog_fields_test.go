package backend

import "testing"

func TestWorklogFieldSelectionPrefersCurrentAndFindsDeprecatedProject(t *testing.T) {
	fields := []field{{ID: "old", Name: "ZZZ (DEPRECATED) Worklog (Date)"}, {ID: "new", Name: "Worklog (Date)"}}
	for _, order := range [][]field{fields, {fields[1], fields[0]}} {
		if got := findField(order, "worklog", "date"); got == nil || got.ID != "new" {
			t.Fatalf("current field not preferred: %+v", got)
		}
	}
	if got := findField(fields[:1], "worklog", "date"); got == nil || got.ID != "old" {
		t.Fatalf("deprecated fallback not found: %+v", got)
	}
	items := []projectItem{{ID: "unrelated"}, {ID: "worklog"}}
	items[1].Project.Fields.Nodes = fields[:1]
	if got := pickProject(items); got == nil || got.ID != "worklog" {
		t.Fatal("deprecated-only project was skipped")
	}
}
