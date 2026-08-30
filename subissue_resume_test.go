package main

import (
	"testing"
)

// A second entry on the same day and the same issue gets a numbered title, so
// the sub-issue list and the board say which is which. The first one of a day
// keeps its plain name — most days have exactly one.
func TestWorklogTitleNumbersTheSecondEntryOfADay(t *testing.T) {
	sub := func(titles ...string) issueNode {
		node := issueNode{}
		for i, ti := range titles {
			node.SubIssues.Nodes = append(node.SubIssues.Nodes,
				subIssue{Number: i + 1, Title: ti, State: "OPEN"})
		}
		return node
	}

	for _, c := range []struct {
		name   string
		titles []string
		want   string
	}{
		{"first of the day", nil, "Worklog: 2026-08-30"},
		{"another day's worklog is not this one",
			[]string{"Worklog: 2026-08-29"}, "Worklog: 2026-08-30"},
		{"second", []string{"Worklog: 2026-08-30"}, "Worklog: 2026-08-30 Part 2"},
		{"third", []string{"Worklog: 2026-08-30", "Worklog: 2026-08-30 Part 2"},
			"Worklog: 2026-08-30 Part 3"},
		// Taken from the highest part filed, not from a count: a deleted Part 2
		// must not hand its name to the next entry and collide in the list.
		{"a deleted part is not reused",
			[]string{"Worklog: 2026-08-30", "Worklog: 2026-08-30 Part 3"},
			"Worklog: 2026-08-30 Part 4"},
		{"padding and case are not identity",
			[]string{"  worklog: 2026-08-30  "}, "Worklog: 2026-08-30 Part 2"},
		// A title that only starts the same way is a different issue entirely.
		{"a longer date is a different day",
			[]string{"Worklog: 2026-08-300"}, "Worklog: 2026-08-30"},
	} {
		if got := worklogTitle(sub(c.titles...), "2026-08-30"); got != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// worklogPartOf is what reads those titles back, and it is also what decides
// whether the push reports a part at all.
func TestWorklogPartOfReadsTheNumberBack(t *testing.T) {
	const base = "Worklog: 2026-08-30"
	for title, want := range map[string]int{
		base:                    1,
		"  " + base + "  ":      1,
		base + " Part 2":        2,
		base + " part 11":       11,
		base + " Part 0":        0, // not a part, so not this day's worklog
		base + " Part":          0,
		base + " Part two":      0,
		"Worklog: 2026-08-29":   0,
		"Worklog: 2026-08-300":  0,
		"Refs bigledger/repo#1": 0,
		"":                      0,
	} {
		if got := worklogPartOf(title, base); got != want {
			t.Fatalf("worklogPartOf(%q) = %d, want %d", title, got, want)
		}
	}
}

// A retry has to land back on the sub-issue its own earlier attempt created,
// and on no other. Every worklog for one day shares the title
// "Worklog: <date>", so the URL the row recorded is the only thing that tells
// two entries' sub-issues apart.
func TestFindSubIssueMatchesTheURLTheRowRecorded(t *testing.T) {
	const (
		mine   = "https://github.com/BigLedger-Support/tuhu-finance/issues/72"
		theirs = "https://github.com/BigLedger-Support/tuhu-finance/issues/73"
	)
	node := issueNode{}
	node.SubIssues.Nodes = []subIssue{
		{Number: 71, Title: "Worklog: 2026-08-29", State: "CLOSED",
			URL: "https://github.com/BigLedger-Support/tuhu-finance/issues/71"},
		// Same day, same title, two different entries. Picking by title would
		// hand back whichever came first and write one entry over the other.
		{Number: 72, Title: "Worklog: 2026-08-30", State: "CLOSED", URL: mine},
		{Number: 73, Title: "Worklog: 2026-08-30", State: "OPEN", URL: theirs},
		{Number: 74, Title: "Worklog: 2026-08-31", State: "OPEN",
			URL: "  https://github.com/BigLedger-Support/tuhu-finance/issues/74  "},
	}

	cases := map[string]int{
		// Closed is still a match: the board closes a worklog as soon as it
		// moves to Done, so an entry's own sub-issue is usually closed by the
		// time anything is retried.
		mine:   72,
		theirs: 73,
		"https://github.com/BigLedger-Support/tuhu-finance/issues/74": 74, // padding trimmed
		"https://github.com/BigLedger-Support/tuhu-finance/issues/99": 0,  // not this issue's
		"": 0, // a first push has nothing to resume from
	}
	for url, want := range cases {
		got := findSubIssueByURL(node, url)
		switch {
		case want == 0 && got != nil:
			t.Fatalf("%q: expected no match, got #%d", url, got.Number)
		case want != 0 && got == nil:
			t.Fatalf("%q: expected #%d, got none", url, want)
		case want != 0 && got.Number != want:
			t.Fatalf("%q: expected #%d, got #%d", url, want, got.Number)
		}
	}
}
