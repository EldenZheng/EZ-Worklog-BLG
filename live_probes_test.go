//go:build live

// Probes, not tests. Each one calls real GitHub through gh and prints what came
// back; they are how a scrape or a project read gets checked against the live
// account rather than against a fixture.
//
// They are behind a build tag because they are neither offline nor portable:
// the org, the project number and the issue are whichever ones were being
// worked on, so on anyone else's account they fail for reasons that say nothing
// about the code. `go test ./...` must stay green on a fresh clone.
//
// Run them deliberately, and edit the refs below to something you can see:
//
//	go test -tags live -run Probe -v
//	go test -tags live -run Proj -v
//	go test -tags live -run FindSubIssueLive -v

package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Scrape the pending commits for an org and print what the Log work tab would
// be showing.
func TestProbe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PANIC: %v", r)
		}
	}()
	store := newStore(t.TempDir())
	cfg := Config{Repos: []string{"bigledger"}, LookbackDays: 7}
	res, err := store.FetchPending(cfg)
	if err != nil {
		t.Logf("FetchPending error: %v", err)
	}
	fmt.Printf("count=%d groups=%d errors=%v range=%v\n",
		res.Count, len(res.Groups), res.Errors, res.Range)
	for i, g := range res.Groups {
		if i >= 3 {
			break
		}
		fmt.Printf("  group %s issue=%q commits=%d\n", g.Date, g.Issue, len(g.Commits))
	}
}

// Read a month back off the project board and total it per day, which is what
// the Status calendar is drawn from.
func TestProj(t *testing.T) {
	cfg := Config{ProjectURL: "https://github.com/orgs/bigledger/projects/9"}
	items, err := FetchProjectWorklogs(cfg, "blg-elden", "2026-07-01", "2026-07-31")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	fmt.Printf("total items=%d\n", len(items))
	july := map[string]int{}
	for _, it := range items {
		if strings.HasPrefix(it.Date, "2026-07") {
			july[it.Date] += it.Minutes
		}
	}
	keys := make([]string, 0, len(july))
	for d := range july {
		keys = append(keys, d)
	}
	sort.Strings(keys)
	fmt.Println("July per-day (field date):")
	sum := 0
	for _, d := range keys {
		fmt.Printf("  %s : %d\n", d, july[d])
		sum += july[d]
	}
	fmt.Printf("July total=%d over %d days\n", sum, len(keys))
}

// Live check against the sub-issue a failed push left behind: a retry must find
// it instead of creating a second one.
func TestFindSubIssueLive(t *testing.T) {
	_, _, _, node, err := getIssueContext("bigledger/blg-int-general-task#6511")
	if err != nil {
		t.Skipf("gh unavailable: %v", err)
	}
	if len(node.SubIssues.Nodes) == 0 {
		t.Fatal("sub-issues did not come back; the GraphQL feature header is likely missing")
	}
	// Resume is keyed on the URL the row recorded, not on the title: every
	// worklog for a day is titled the same, so a title match cannot tell one
	// entry's sub-issue from another's.
	want := node.SubIssues.Nodes[0].URL
	got := findSubIssueByURL(node, want)
	if got == nil {
		var titles []string
		for _, s := range node.SubIssues.Nodes {
			titles = append(titles, s.Title+" "+s.URL)
		}
		t.Fatalf("retry would create a duplicate; sub-issues present: %s", strings.Join(titles, " | "))
	}
	t.Logf("resume would reuse #%d %q (%s)", got.Number, got.Title, got.State)
}
