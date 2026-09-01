package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleWeek() []WorklogItem {
	return []WorklogItem{
		{Date: "2026-08-25", Minutes: 300, Owner: "blg-elden", Status: "Done",
			Title: "Worklog: 2026-08-25", URL: "https://github.com/BigLedger-Support/tuhu-finance/issues/72",
			ParentTitle: "Chinese translation: Internal Sales Return Applet",
			ParentURL:   "https://github.com/BigLedger-Support/tuhu-finance/issues/48",
			Assignees:   []string{"blg-elden"},
			Remarks:     "-Reverted labels\n-Moved getLabel\tinto refreshLabels()"},
		{Date: "2026-08-24", Minutes: 240, Owner: "blg-elden", Status: "Done",
			Title: "Worklog: 2026-08-24", URL: "https://github.com/BigLedger-Support/tuhu-finance/issues/70",
			ParentTitle: "Chinese translation: Internal Sales Return Applet",
			ParentURL:   "https://github.com/BigLedger-Support/tuhu-finance/issues/48",
			Assignees:   []string{"blg-elden"}},
		{Date: "2026-08-26", Minutes: 480, Owner: "blg-elden", Status: "Done",
			Title: "Worklog: 2026-08-26", URL: "https://github.com/bigledger/blg-sd-senwave-senheng/issues/551",
			ParentTitle: "Doc Item temp feature does not save",
			ParentURL:   "https://github.com/bigledger/blg-sd-senwave-senheng/issues/424"},
		// No parent: a worklog written straight onto an issue. It has to stand as
		// its own row rather than vanish out of the summary.
		{Date: "2026-08-27", Minutes: 60, Owner: "blg-elden", Status: "Done",
			Title: "Fix the null cap", URL: "https://github.com/bigledger/blg-intranet/issues/5760"},
	}
}

// The mail is written off this list, so it has to name the work rather than the
// worklogs: every worklog is called "Worklog: <date>", and a list of those says
// only that the week happened.
func TestWeekByIssueGroupsOnTheParentLongestFirst(t *testing.T) {
	got := weekByIssue(sampleWeek())
	if len(got) != 3 {
		t.Fatalf("three issues worked on, got %d: %+v", len(got), got)
	}

	// 300 + 240 on one issue beats the single 480.
	if got[0].Title != "Chinese translation: Internal Sales Return Applet" {
		t.Fatalf("the biggest issue should lead, got %q", got[0].Title)
	}
	if got[0].Minutes != 540 || got[0].Entries != 2 {
		t.Fatalf("two worklogs on one issue should add up: %d min over %d entries",
			got[0].Minutes, got[0].Entries)
	}
	if got[0].URL != "https://github.com/BigLedger-Support/tuhu-finance/issues/48" {
		t.Fatalf("the row should link the parent issue, got %q", got[0].URL)
	}
	if got[1].Minutes != 480 {
		t.Fatalf("expected the 480 second, got %d", got[1].Minutes)
	}
	// The parentless one is still there, under its own title.
	if got[2].Title != "Fix the null cap" || got[2].Minutes != 60 {
		t.Fatalf("a worklog with no parent must keep its place: %+v", got[2])
	}
}

// TSV has no quoting, so a tab or a newline in a cell does not escape — it
// becomes a new column or a new row. The remarks are full of both.
func TestWeekTSVKeepsEveryRowOnOneLine(t *testing.T) {
	out := weekTSV(sampleWeek())
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("a header and four entries, got %d lines:\n%s", len(lines), out)
	}
	if lines[0] != strings.Join(weekTSVHeader, "\t") {
		t.Fatalf("header should match the board's download, got %q", lines[0])
	}
	for i, ln := range lines {
		if n := strings.Count(ln, "\t"); n != len(weekTSVHeader)-1 {
			t.Fatalf("line %d has %d tabs, want %d: %q", i, n, len(weekTSVHeader)-1, ln)
		}
	}
	// Oldest first, the way the board downloads it.
	if !strings.HasPrefix(lines[1], "https://github.com/BigLedger-Support/tuhu-finance/issues/48\tWorklog: 2026-08-24") {
		t.Fatalf("rows should run oldest first, got %q", lines[1])
	}
	// Repository is read off the URL rather than fetched.
	if !strings.Contains(lines[1], "\tBigLedger-Support/tuhu-finance\t") {
		t.Fatalf("the repo column should come off the issue URL: %q", lines[1])
	}
}

// The saved views carry a view id and a column set that a project URL does not,
// so the link is kept whole and only the week and the owner are rewritten.
func TestWeekBoardLinksRewriteOnlyTheFilter(t *testing.T) {
	links := weekBoardLinks("blg-elden", "2026-08-24", "2026-08-30")
	if len(links) != len(boardViewURLs) {
		t.Fatalf("one link per saved view, got %d", len(links))
	}

	for i, link := range links {
		u, err := url.Parse(link)
		if err != nil {
			t.Fatalf("link %d does not parse: %v", i, err)
		}
		q := u.Query()
		want := `"worklog-owner-(blg-xxxx)":blg-elden "worklog-(date)":>=2026-08-24 "worklog-(date)":<=2026-08-30`
		if got := q.Get("filterQuery"); got != want {
			t.Fatalf("link %d filter = %q, want %q", i, got, want)
		}

		// Everything else survives, or the link opens the wrong view with the
		// wrong columns — which is the whole reason these are kept verbatim.
		orig, _ := url.Parse(boardViewURLs[i])
		for key, vals := range orig.Query() {
			if key == "filterQuery" {
				continue
			}
			if got := q[key]; strings.Join(got, ",") != strings.Join(vals, ",") {
				t.Fatalf("link %d dropped or changed %q: %v, want %v", i, key, got, vals)
			}
		}
		if u.Path != orig.Path {
			t.Fatalf("link %d changed view path: %q, want %q", i, u.Path, orig.Path)
		}
	}
}

// Two links to the same project differ only by the saved view, so the label has
// to carry it or the dialog names both the same.
func TestBoardLinkLabelNamesTheOrgProjectAndView(t *testing.T) {
	for _, c := range []struct{ url, want string }{
		{boardViewURLs[0], "bigledger · project 9 · view 19"},
		{boardViewURLs[1], "BigLedger-Support · project 1 · view 66"},
		{"https://github.com/orgs/bigledger/projects/9", "bigledger · project 9"},
		// Not a project URL at all: shown as it stands rather than mislabelled.
		{"https://example.com/nope", "https://example.com/nope"},
	} {
		if got := boardLinkLabel(c.url); got != c.want {
			t.Fatalf("boardLinkLabel(%.48s…) = %q, want %q", c.url, got, c.want)
		}
	}
}

// What lands on the clipboard: the boards to click, then the week in the order
// it should be talked about. The TSV stays out of it — pasted into a mail body
// it is a wall of tabs.
func TestWeekMailTextLeadsWithBoardsThenTheSummary(t *testing.T) {
	text := weekMailText("blg-elden", "2026-08-24", "2026-08-30", sampleWeek(), "C:\\out.tsv")

	for _, want := range []string{
		"Worklog 2026-08-24 to 2026-08-30 — blg-elden",
		"/orgs/bigledger/projects/9/views/19",
		"/orgs/BigLedger-Support/projects/1/views/66",
		"By issue (1080 min · 18h across 3 issues)",
		// Minutes per issue, the unit the board sums in.
		"540m",
		"480m",
		"60m",
		"Chinese translation: Internal Sales Return Applet",
		"C:\\out.tsv",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("mail text is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, strings.Join(weekTSVHeader, "\t")) {
		t.Fatal("the TSV belongs in the attachment, not in the mail body")
	}
	if boards, summary := strings.Index(text, "Boards:"), strings.Index(text, "By issue"); boards > summary {
		t.Fatal("the boards should come before the summary")
	}
}

// The export lands in Downloads, beside the board's own TSV downloads and where
// the mail client's attach dialog opens. A missing Downloads must not fail the
// export — the file goes to the app's own folder instead.
func TestDownloadsDirFallsBackWhenThereIsNoDownloads(t *testing.T) {
	fallback := t.TempDir()

	home := t.TempDir()
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	t.Setenv("HOME", home)
	if got := downloadsDir(fallback); got != fallback {
		t.Fatalf("no Downloads under home should fall back, got %q", got)
	}

	dl := filepath.Join(home, "Downloads")
	if err := os.Mkdir(dl, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := downloadsDir(fallback); got != dl {
		t.Fatalf("expected %q, got %q", dl, got)
	}

	// A file of that name where the folder should be is not a folder.
	other := t.TempDir()
	t.Setenv("USERPROFILE", other)
	t.Setenv("HOME", other)
	if err := os.WriteFile(filepath.Join(other, "Downloads"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := downloadsDir(fallback); got != fallback {
		t.Fatalf("a file named Downloads is not somewhere to write, got %q", got)
	}
}

// A commit written past midnight belongs to the evening it came out of, not to
// the calendar day the clock had just turned over into. On a Sunday night that
// is the difference between a week and the week after it.
func TestCommitsAreBucketedOnTheWorkingDayNotTheCalendarDay(t *testing.T) {
	// Built in the machine's own zone, since that is the day the app reasons in.
	at := func(day, hour int) string {
		return time.Date(2026, 8, day, hour, 30, 0, 0, time.Local).Format(time.RFC3339)
	}
	for _, c := range []struct{ ts, want, why string }{
		{at(31, 1), "2026-08-30", "1:30am is the night before's overtime"},
		{at(31, 7), "2026-08-30", "still before the day turns over at 08:00"},
		{at(31, 8), "2026-08-31", "08:30 starts the new day"},
		{at(30, 23), "2026-08-30", "late evening is its own day"},
	} {
		if got := commitWorkDate(c.ts); got != c.want {
			t.Fatalf("%s: commitWorkDate(%s) = %s, want %s", c.why, c.ts, got, c.want)
		}
	}

	// The two APIs spell the same instant differently; both must still land on
	// one day or a commit is offered twice.
	if a, b := commitWorkDate("2026-08-06T14:58:12Z"),
		commitWorkDate("2026-08-06T22:58:12.000+08:00"); a != b {
		t.Fatalf("same instant bucketed differently: %s and %s", a, b)
	}
	// Nothing parseable: still bucketed somewhere rather than lost.
	if got := commitWorkDate("not a timestamp"); got != "not a timestamp" {
		t.Fatalf("unparseable input should pass through, got %q", got)
	}
}

// An empty week must still produce a file and a readable mail: a week off is a
// thing to report, not an error.
func TestExportWeekWritesAFileEvenWithNothingOnTheBoard(t *testing.T) {
	dir := t.TempDir()
	path, n, text, err := exportWeek(dir, "blg-elden", "2026-08-24", "2026-08-30", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("no items means no entries, got %d", n)
	}
	if filepath.Base(path) != "worklog-2026-08-24-to-2026-08-30.tsv" {
		t.Fatalf("the file should name its week, got %q", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimRight(string(body), "\n") != strings.Join(weekTSVHeader, "\t") {
		t.Fatalf("an empty week is a header and no rows, got %q", body)
	}
	if !strings.Contains(text, "Nothing on the board for this week.") {
		t.Fatalf("the mail should say the week is empty:\n%s", text)
	}
}
