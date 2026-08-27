package main

import (
	"testing"
)

func TestFindSubIssueMatching(t *testing.T) {
	node := issueNode{}
	node.SubIssues.Nodes = []subIssue{
		{Number: 1, Title: "Worklog: 2026-08-08", State: "OPEN"},
		{Number: 2, Title: "Worklog: 2026-08-09", State: "CLOSED"},
		{Number: 3, Title: "Worklog: 2026-08-10", State: "OPEN"},
		{Number: 4, Title: "  worklog: 2026-08-11  ", State: "OPEN"},
		{Number: 5, Title: "Worklog: 2026-08-12", State: "CLOSED"},
		{Number: 6, Title: "Worklog: 2026-08-12", State: "OPEN"},
	}
	cases := map[string]int{
		"Worklog: 2026-08-08": 1, // exact open match
		"Worklog: 2026-08-10": 3, // exact open match
		"Worklog: 2026-08-11": 4, // padded + different case still matches
		"Worklog: 2026-08-09": 2, // closed still counts; Done can auto-close
		"Worklog: 2026-08-12": 6, // open wins over a closed duplicate
		"Worklog: 2026-08-13": 0, // absent
	}
	for title, want := range cases {
		got := findSubIssue(node, title)
		switch {
		case want == 0 && got != nil:
			t.Fatalf("%q: expected no match, got #%d", title, got.Number)
		case want != 0 && got == nil:
			t.Fatalf("%q: expected #%d, got none", title, want)
		case want != 0 && got.Number != want:
			t.Fatalf("%q: expected #%d, got #%d", title, want, got.Number)
		}
	}
}
