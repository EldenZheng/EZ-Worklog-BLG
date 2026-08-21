package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestWorklogProjectFilter(t *testing.T) {
	from, to, err := monthBounds("2026-07")
	if err != nil {
		t.Fatal(err)
	}
	if from != "2026-07-01" || to != "2026-07-31" {
		t.Fatalf("unexpected month bounds: %s..%s", from, to)
	}
	got := worklogProjectFilter("blg-elden", "2026-07-27", "2026-08-02")
	want := `"worklog-owner-(blg-xxxx)":blg-elden "worklog-(date)":>=2026-07-27 "worklog-(date)":<=2026-08-02`
	if got != want {
		t.Fatalf("filter mismatch:\n got %q\nwant %q", got, want)
	}
}

func TestReportBounds(t *testing.T) {
	from, to, err := reportBounds("2026-07")
	if err != nil {
		t.Fatal(err)
	}
	if from != "2026-06-21" || to != "2026-07-20" {
		t.Fatalf("unexpected report bounds: %s..%s", from, to)
	}
}

// The working-day counter runs off the calendar, not off the pay divisor. The
// Jun 21 – Jul 20 2026 window happens to land on 21 weekdays; Aug's does not,
// which is the case the tile exists for. Today is still workable, so it belongs
// to the days left, not the days gone.
func TestWorkingDaysProgress(t *testing.T) {
	from, to, err := reportBounds("2026-07")
	if err != nil {
		t.Fatal(err)
	}
	gone, total := workingDaysProgress(from, to, "2026-07-01")
	if total != 21 {
		t.Fatalf("Jun 21 – Jul 20 2026 holds 21 weekdays, got %d", total)
	}
	// Jun 22–30 is 7 weekdays; Jul 1 itself is still open.
	if gone != 7 {
		t.Fatalf("expected 7 working days gone by Jul 1, got %d", gone)
	}

	// A closed period reads n/n; one not yet open reads 0/n.
	if gone, total := workingDaysProgress(from, to, "2026-08-15"); gone != total || total != 21 {
		t.Fatalf("closed period should read n/n, got %d/%d", gone, total)
	}
	if gone, _ := workingDaysProgress(from, to, "2026-01-01"); gone != 0 {
		t.Fatalf("nothing is gone before the period opens, got %d", gone)
	}

	// Jul 21 – Aug 20 2026 is a 23-weekday window: the counter must report the
	// calendar's number and let the tile explain the gap from the divisor.
	augFrom, augTo, err := reportBounds("2026-08")
	if err != nil {
		t.Fatal(err)
	}
	if _, total := workingDaysProgress(augFrom, augTo, "2026-08-10"); total != 23 {
		t.Fatalf("Jul 21 – Aug 20 2026 holds 23 weekdays, got %d", total)
	}
	if _, total := workingDaysProgress("bad", to, "2026-07-01"); total != 0 {
		t.Fatalf("unparseable bounds should count nothing, got %d", total)
	}
}

func TestMergeCommitsAreExcludedAndRemarksHaveFallback(t *testing.T) {
	merge := Commit{Sha: "aaaaaaa", Message: "Merge pull request #42 from feature"}
	if !isMergeCommit(merge) {
		t.Fatal("expected GitHub merge commit to be excluded")
	}
	// A message-less commit has nothing to describe it but its sha, so the
	// fallback keeps it — that is content, not a tag.
	remarks := buildRemarks([]Commit{{Sha: "bbbbbbb"}})
	if remarks != "- Commit bbbbbbb" {
		t.Fatalf("unexpected fallback remarks: %q", remarks)
	}
}

// Remarks are handed over complete even when they exceed remarkCap: the editor
// counts characters, blocks the push, and offers AI compaction. Silently
// dropping bullets here used to lose real work.
func TestOversizedRemarkIsNotTruncated(t *testing.T) {
	long := strings.Repeat("meaningful work ", 100)
	remarks := buildRemarks([]Commit{{Sha: "ccccccc", Message: long}})
	if strings.Contains(remarks, "more)") {
		t.Fatalf("remarks were truncated: %q", remarks)
	}
	if strings.Contains(remarks, "ccccccc") {
		t.Fatalf("remarks should carry no sha tag: %q", truncate(remarks, 80))
	}
	if !strings.Contains(remarks, strings.TrimSpace(long)) {
		t.Fatalf("commit text was altered: %q", remarks)
	}
}

// Every bullet of a long multi-line body must survive.
func TestLongBodyKeepsEveryBullet(t *testing.T) {
	var body []string
	for i := 0; i < 12; i++ {
		body = append(body, fmt.Sprintf("- bullet %d %s", i, strings.Repeat("x", 100)))
	}
	full := "Refs bigledger/repo/issues/1:\n" + strings.Join(body, "\n")
	remarks := buildRemarks([]Commit{{Sha: "ddddddd", Repo: "bigledger/repo", Full: full}})
	if len(remarks) <= remarkCap {
		t.Fatalf("test needs input past the cap, got %d chars", len(remarks))
	}
	for i := 0; i < 12; i++ {
		if !strings.Contains(remarks, fmt.Sprintf("bullet %d", i)) {
			t.Fatalf("bullet %d was dropped: %q", i, remarks)
		}
	}
}

// The same instant arrives as "Z" from repos/*/commits and as an offset from
// search/commits. Both must land on one day or a commit is bucketed twice.
func TestLocalDateAgreesAcrossAPIFormats(t *testing.T) {
	fromREST := localDate("2026-08-06T14:58:12Z")
	fromSearch := localDate("2026-08-06T22:58:12.000+08:00")
	if fromREST != fromSearch {
		t.Fatalf("same instant bucketed differently: REST=%s search=%s", fromREST, fromSearch)
	}
	if got := localDate("not a timestamp"); got != "not a timestamp" {
		t.Fatalf("unparseable input should pass through, got %q", got)
	}
}

func TestRemarksUseBodyNotRefsHeader(t *testing.T) {
	c := Commit{
		Sha:     "05c9cae",
		Message: "Refs bigledger/blg-sd-senwave-senheng/issues/483:",
		Full: "Refs bigledger/blg-sd-senwave-senheng/issues/483:\n" +
			"- Cap fix: limit: null -> explicit ROW_LIMIT = 9999.\n" +
			"- Server-side search: opt-in serverSideSearch.",
	}
	got := buildRemarks([]Commit{c})
	if strings.Contains(got, "issues/483:") {
		t.Fatalf("boilerplate header leaked into remarks: %q", got)
	}
	for _, want := range []string{"ROW_LIMIT = 9999", "serverSideSearch"} {
		if !strings.Contains(got, want) {
			t.Fatalf("remarks lost %q: %q", want, got)
		}
	}
	// Remarks are the work, nothing else. The sha lives on the tile, on the
	// editor's commit link and in the row's refs column.
	if strings.Contains(got, "05c9cae") {
		t.Fatalf("sha leaked into remarks: %q", got)
	}
}

// A header-only message has no body to promote, so the subject must survive.
func TestRemarksFallBackToSubject(t *testing.T) {
	c := Commit{Sha: "9f8a180", Message: "sync screen script", Full: "sync screen script"}
	if got := buildRemarks([]Commit{c}); !strings.Contains(got, "sync screen script") {
		t.Fatalf("subject was dropped: %q", got)
	}
}

func TestMatchesConfig(t *testing.T) {
	entries := []string{"bigledger", "someone/explicit-repo"}
	cases := map[string]bool{
		"bigledger/blg-shared-utilities": true,
		"someone/explicit-repo":          true,
		"someone/other-repo":             false,
		"unrelated/thing":                false,
	}
	for repo, want := range cases {
		if got := matchesConfig(repo, entries); got != want {
			t.Fatalf("matchesConfig(%q) = %v, want %v", repo, got, want)
		}
	}
}

func TestWeekendSupportBonus(t *testing.T) {
	var items []WorklogItem
	for i := 1; i <= 7; i++ {
		items = append(items, WorklogItem{
			Title:       "Worklog: 2026-07-0" + string(rune('0'+i)),
			URL:         "https://github.com/bigledger/worklog/issues/" + string(rune('0'+i)),
			ParentTitle: "Elden: Weekend Support Time",
			ParentURL:   "https://github.com/bigledger/support/issues/" + string(rune('0'+i)),
		})
	}
	// A duplicate worklog for the same support issue must not be paid twice.
	items = append(items, items[0])
	qualifying, sets, bonus := weekendSupportBonus(items)
	if qualifying != 7 || sets != 1 || bonus != 300 {
		t.Fatalf("unexpected support bonus: issues=%d sets=%d bonus=%.2f", qualifying, sets, bonus)
	}
}

func TestCommaAmount(t *testing.T) {
	if got := commaAmount(1234567.8); got != "1,234,567.80" {
		t.Fatalf("unexpected formatted amount: %q", got)
	}
}

func TestParseProjectRefWithViewQuery(t *testing.T) {
	ref, err := parseProjectRef("https://github.com/orgs/bigledger/projects/9/views/19?filterQuery=x")
	if err != nil {
		t.Fatal(err)
	}
	if ref.ownerKind != "orgs" || ref.login != "bigledger" || ref.number != 9 {
		t.Fatalf("unexpected project ref: %+v", ref)
	}
}

func TestWorklogFromNode(t *testing.T) {
	minutes := 315.0
	item, ok := worklogFromNode(
		&projectItemContent{Title: "Worklog: 2026-07-27", URL: "https://github.com/bigledger/example/issues/1"},
		[]pvFieldValue{
			{Text: "blg-elden", Field: struct {
				Name string `json:"name"`
			}{Name: "Worklog Owner (blg-xxxx)"}},
			{Date: "2026-07-27", Field: struct {
				Name string `json:"name"`
			}{Name: "Worklog (Date)"}},
			{Number: &minutes, Field: struct {
				Name string `json:"name"`
			}{Name: "Worklog (Minutes)"}},
			{Text: "Implemented the report", Field: struct {
				Name string `json:"name"`
			}{Name: "Worklog (Remarks)"}},
		},
	)
	if !ok {
		t.Fatal("expected a worklog item")
	}
	if item.Owner != "blg-elden" || item.Date != "2026-07-27" || item.Minutes != 315 {
		t.Fatalf("unexpected worklog item: %+v", item)
	}
	if !strings.Contains(item.Remarks, "report") {
		t.Fatalf("remarks were not read: %q", item.Remarks)
	}
}

// Two orgs mean two boards. Every configured URL is read, the main one first,
// and a repeat is dropped so a duplicated line cannot double a day's minutes.
func TestProjectURLsMergesBoards(t *testing.T) {
	cfg := Config{
		ProjectURL: "https://github.com/orgs/bigledger/projects/9",
		ExtraProjectURLs: []string{
			" https://github.com/orgs/BigLedger-Support/projects/1 ",
			"https://github.com/orgs/bigledger/projects/9", // duplicate
			"",
		},
	}
	got := projectURLs(cfg)
	want := []string{
		"https://github.com/orgs/bigledger/projects/9",
		"https://github.com/orgs/BigLedger-Support/projects/1",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
	// Both boards must parse: they are read with the same query and the same
	// server-side filter, which only works because their field names match.
	for _, u := range got {
		if _, err := parseProjectRef(u); err != nil {
			t.Fatalf("%s does not parse: %v", u, err)
		}
	}
	if len(projectURLs(Config{})) != 0 {
		t.Fatal("no configured board should read as none, not as an empty URL")
	}
}

// An org without a Worklog issue type is a fact about the org, not a failed
// push. Confusing the two left the row waiting to be retried forever.
func TestMissingIssueTypeIsNotAPushFailure(t *testing.T) {
	err := fmt.Errorf("%w: %s has no %q issue type", errNoIssueType, "BigLedger-Support", "Worklog")
	if !errors.Is(err, errNoIssueType) {
		t.Fatal("a missing type must stay recognisable through wrapping")
	}
	if errors.Is(ghErr("network is down"), errNoIssueType) {
		t.Fatal("a transport failure must not read as a missing type")
	}
}
