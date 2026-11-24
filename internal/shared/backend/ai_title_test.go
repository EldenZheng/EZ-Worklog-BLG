package backend

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCleanSuggestedTitleKeepsTheLastLineBare(t *testing.T) {
	for in, want := range map[string]string{
		"Fixed the login redirect\n":                  "Fixed the login redirect",
		"thinking...\n\nTitle: \"Fixed the paging.\"": "Fixed the paging",
		"  `Reviewed i18n keys`  ":                    "Reviewed i18n keys",
		"":                                            "",
	} {
		if got := CleanSuggestedTitle(in); got != want {
			t.Errorf("CleanSuggestedTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSuggestWorklogTitleSendsThePromptOnStdinAndCleansTheAnswer(t *testing.T) {
	old := runTitleCLI
	t.Cleanup(func() { runTitleCLI = old })
	var asked TitleCLI
	var prompt string
	runTitleCLI = func(_ context.Context, cli TitleCLI, p string) (string, error) {
		asked, prompt = cli, p
		return "Fixed the paging.\n", nil
	}
	got, err := SuggestWorklogTitle("claude", KindCodeReview, "Parent", "- checked paging")
	if err != nil || got != "Fixed the paging" {
		t.Fatalf("got %q, %v", got, err)
	}
	if asked.Command != "claude" || !strings.Contains(prompt, "- checked paging") ||
		!strings.Contains(prompt, "Parent issue: Parent") || !strings.Contains(prompt, "what was reviewed") {
		t.Fatalf("asked %v with %q", asked, prompt)
	}
	if _, err := SuggestWorklogTitle("", KindOther, "", "x"); err == nil {
		t.Fatal("no CLI set should be an error")
	}
	if _, err := SuggestWorklogTitle("claude", KindOther, "", "  "); err == nil {
		t.Fatal("empty remarks should be an error")
	}
	runTitleCLI = func(context.Context, TitleCLI, string) (string, error) { return "", errors.New("not signed in") }
	if _, err := SuggestWorklogTitle("codex", KindOther, "", "x"); err == nil {
		t.Fatal("CLI failure should be an error")
	}
}

func TestAvailableTitleCLIsListsOnlyInstalledOnes(t *testing.T) {
	old := lookPath
	t.Cleanup(func() { lookPath = old })
	lookPath = func(name string) (string, error) {
		if name == "codex" {
			return `C:\bin\codex.cmd`, nil
		}
		return "", errors.New("not found")
	}
	got := AvailableTitleCLIs()
	if len(got) != 1 || got[0].Command != "codex" {
		t.Fatalf("got %v", got)
	}
}

func TestWorklogTitlesByKind(t *testing.T) {
	cfg := Config{WorklogOwner: "team-elden"}
	if got := SubTitleBaseFor(cfg, Row{"type": KindOther, "worklog_title": " Fixed paging "}); got != "Fixed paging" {
		t.Errorf("normal = %q", got)
	}
	if got := SubTitleBaseFor(cfg, Row{"type": KindOther}); got != "" {
		t.Errorf("untitled normal = %q, want the Worklog: <date> default", got)
	}
	if got := SubTitleBaseFor(cfg, Row{"type": KindCodeReview, "worklog_title": "PR #5 paging", "date": "2026-11-23"}); got != "Elden - Code Review: PR #5 paging" {
		t.Errorf("review = %q", got)
	}
	if got := SubTitleBaseFor(cfg, Row{"type": KindCodeReview, "date": "2026-11-23"}); got != "Elden - Code Review: 2026-11-23" {
		t.Errorf("untitled review = %q", got)
	}
	if got := BulkCodeReviewTitle(cfg, "2026-11-23", 6); got != "Elden - Bulk (6) Code Reviews: 2026-11-23" {
		t.Errorf("bulk = %q", got)
	}
	for title, want := range map[string]bool{
		"Elden - Bulk (6) Code Reviews: 2026-11-23":  true,
		"elden - bulk (12) code reviews: 2026-11-23": true,
		"Elden Code Review: 2026-11-23":              true,
		"Elden - Bulk (6) Code Reviews: 2026-11-22":  false,
		"Alex - Bulk (6) Code Reviews: 2026-11-23":   false,
	} {
		if got := isDailyReviewTitle(cfg, "2026-11-23", title); got != want {
			t.Errorf("isDailyReviewTitle(%q) = %v", title, got)
		}
	}
}

func TestCodeReviewNotesRideUnderTheirPR(t *testing.T) {
	items, err := ParseCodeReviewItems("https://github.com/o/r/pull/1 (10m)\n- checked keys\n* ran audit\nhttps://github.com/o/r/pull/2 (5m)")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || len(items[0].Notes) != 2 || items[0].Notes[1] != "ran audit" || len(items[1].Notes) != 0 {
		t.Fatalf("items = %+v", items)
	}
	if got := CodeReviewBody(items); got != "https://github.com/o/r/pull/1 (10m)\n- checked keys\n- ran audit\n\nhttps://github.com/o/r/pull/2 (5m)" {
		t.Fatalf("body = %q", got)
	}
	if got := CodeReviewList(items); got != "https://github.com/o/r/pull/1 (10m)\nhttps://github.com/o/r/pull/2 (5m)" {
		t.Fatalf("list = %q", got)
	}
	if _, err := ParseCodeReviewItems("- orphan note\nhttps://github.com/o/r/pull/1 (10m)"); err == nil {
		t.Fatal("a note before any PR should be an error")
	}
	// The bare list on the board does not wipe the notes from the description.
	merged, err := MergeCodeReviewItems(CodeReviewBody(items), CodeReviewList(items),
		"https://github.com/o/r/pull/2 (5m)\n- new note")
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 2 || len(merged[0].Notes) != 2 || len(merged[1].Notes) != 1 || merged[1].Notes[0] != "new note" {
		t.Fatalf("merged = %+v", merged)
	}
}

func TestRowNeedsWorklogTitleOnlyForUntitledWaitingEntries(t *testing.T) {
	day := "2026-11-25"
	for _, tc := range []struct {
		row  Row
		want bool
	}{
		{Row{"type": KindCommit, "date": day}, true},
		{Row{"type": KindCodeReview, "date": day}, true},
		{Row{"type": KindCommit, "date": day, "worklog_title": "Fixed paging"}, false},
		{Row{"type": KindCommit, "date": day, "pushed_at": "2026-11-25T10:00:00"}, false},
		{Row{"type": KindMeeting, "date": day}, false},
		{Row{"type": KindBulkReview, "date": day}, false},
	} {
		if got := RowNeedsWorklogTitle(tc.row); got != tc.want {
			t.Errorf("RowNeedsWorklogTitle(%v) = %v", tc.row, got)
		}
	}
}

func TestBulkReviewLabelCountsTheSavedPRs(t *testing.T) {
	cfg := Config{WorklogOwner: "team-elden"}
	r := Row{"date": "2026-11-28", "remarks": "https://github.com/o/r/pull/1 (10m)\nhttps://github.com/o/r/pull/2 (10m)",
		"issue_body": "https://github.com/o/r/pull/1 (10m)\n- Approved\n\nhttps://github.com/o/r/pull/2 (10m)\n- Approved"}
	if got := BulkReviewLabel(cfg, r); got != "Elden - Bulk (2) Code Reviews: 2026-11-28" {
		t.Fatalf("label = %q", got)
	}
	if got := BulkReviewLabel(cfg, Row{"date": "2026-11-28"}); got != "Elden - Bulk Code Reviews: 2026-11-28" {
		t.Fatalf("empty label = %q", got)
	}
}

func TestReviewBodyNamesEachPRsIssue(t *testing.T) {
	items := []CodeReviewItem{
		{URL: "https://github.com/o/r/pull/1", Minutes: 10, Issues: []ReviewIssue{{Ref: "o/t#5", Title: "Fix [x] - paging"}}, Notes: []string{"Approved"}},
		{URL: "https://github.com/o/r/pull/2", Minutes: 15, Issues: []ReviewIssue{{Ref: "o/t#6"}, {Ref: "o/t#7", Title: "Seven"}}},
		{URL: "https://github.com/o/r/pull/3", Minutes: 5},
	}
	body := CodeReviewBody(items)
	want := "https://github.com/o/r/pull/1 (10m)\nIssue: [Fix \\[x\\] - paging](https://github.com/o/t/issues/5)\n- Approved\n\n" +
		"https://github.com/o/r/pull/2 (15m)\nIssues: [o/t#6](https://github.com/o/t/issues/6), [Seven](https://github.com/o/t/issues/7)\n\n" +
		"https://github.com/o/r/pull/3 (5m)"
	if body != want {
		t.Fatalf("body = %q", body)
	}
	for name, parse := range map[string]func(string) []CodeReviewItem{
		"saved":  func(s string) []CodeReviewItem { got, _ := ParseCodeReviewItems(s); return got },
		"pasted": ParsePastedReviews,
		"one line": func(s string) []CodeReviewItem {
			return ParsePastedReviews(strings.ReplaceAll(s, "\n", " "))
		},
	} {
		got := parse(body)
		if len(got) != 3 || len(got[0].Issues) != 1 || got[0].Issues[0] != (ReviewIssue{Ref: "o/t#5", Title: "Fix [x] - paging"}) ||
			len(got[0].Notes) != 1 || got[0].Notes[0] != "Approved" || got[0].Minutes != 10 ||
			len(got[1].Issues) != 2 || got[1].Issues[0] != (ReviewIssue{Ref: "o/t#6"}) || got[1].Issues[1].Title != "Seven" || len(got[1].Notes) != 0 ||
			got[2].Minutes != 5 || len(got[2].Issues) != 0 {
			t.Fatalf("%s = %+v", name, got)
		}
	}
	// The earlier "PR [issue link] (10m)" spelling still reads.
	if got, err := ParseCodeReviewItems("https://github.com/o/r/pull/1 [https://github.com/o/t/issues/5] (10m)"); err != nil || got[0].Issues[0].Ref != "o/t#5" {
		t.Fatalf("old spelling = %+v, %v", got, err)
	}
	if CodeReviewList(items) != "https://github.com/o/r/pull/1 (10m)\nhttps://github.com/o/r/pull/2 (15m)\nhttps://github.com/o/r/pull/3 (5m)" {
		t.Fatal("the board list stays compact")
	}
}
