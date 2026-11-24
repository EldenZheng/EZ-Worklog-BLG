package backend

import (
	"context"
	"strings"
	"testing"
)

func TestCleanBulletsKeepsTheAnswersPoints(t *testing.T) {
	got := CleanBullets("Here you go:\n```\n- Adds paging\n* Fixes the cache\n• \n```")
	if len(got) != 2 || got[0] != "Adds paging" || got[1] != "Fixes the cache" {
		t.Fatalf("got %q", got)
	}
	if got := CleanBullets("Adds paging\nFixes the cache"); len(got) != 2 {
		t.Fatalf("plain lines = %q", got)
	}
}

func TestFitRemarksKeepsWholePointsUnderTheCap(t *testing.T) {
	long := strings.Repeat("x", 600)
	got := FitRemarks([]string{long, long, "short"})
	if len(got) > RemarkCap || strings.Count(got, "\n") != 0 || !strings.HasPrefix(got, "- x") {
		t.Fatalf("got %d chars: %q", len(got), got[:20])
	}
	if got := FitRemarks([]string{strings.Repeat("y", 2000)}); len(got) != RemarkCap {
		t.Fatalf("a single long point should be cut to the cap, got %d", len(got))
	}
}

func TestSuggestPRNotesReadsThePRAndReturnsItsPoints(t *testing.T) {
	oldFetch, oldRun := FetchPullRequestBrief, runTitleCLI
	t.Cleanup(func() { FetchPullRequestBrief, runTitleCLI = oldFetch, oldRun })
	FetchPullRequestBrief = func(url string) (PullRequestBrief, error) {
		return PullRequestBrief{Title: "Lazy-load the dropdown", Body: "Pages of 50", Files: []string{"a.ts (+10 -2)"},
			Reviews: []string{"APPROVED: lgtm"}, Comments: []string{"a.ts: preselected rows past 50 are dropped"}}, nil
	}
	var prompt string
	runTitleCLI = func(_ context.Context, _ TitleCLI, p string) (string, error) {
		prompt = p
		return "- Loads 50 rows a page\n- Keeps preselected rows", nil
	}
	got, err := SuggestPRNotes("claude", "https://github.com/o/r/pull/1")
	if err != nil || len(got) != 2 || got[0] != "Loads 50 rows a page" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, want := range []string{"Lazy-load the dropdown", "Pages of 50", "a.ts (+10 -2)", "https://github.com/o/r/pull/1",
		"not a summary of the pull request", "Approved, no blocking issues", "APPROVED: lgtm", "preselected rows past 50 are dropped"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt misses %q", want)
		}
	}
}

func TestSummarizeRemarksFitsTheCap(t *testing.T) {
	old := runTitleCLI
	t.Cleanup(func() { runTitleCLI = old })
	runTitleCLI = func(context.Context, TitleCLI, string) (string, error) {
		return strings.Repeat("- "+strings.Repeat("z", 300)+"\n", 6), nil
	}
	got, err := SummarizeRemarks("claude", strings.Repeat("long notes ", 200))
	if err != nil || len(got) > RemarkCap || !strings.HasPrefix(got, "- z") {
		t.Fatalf("got %d chars, %v", len(got), err)
	}
	if _, err := SummarizeRemarks("claude", " "); err == nil {
		t.Fatal("nothing to summarise should be an error")
	}
}

func TestParsePastedReviewsReadsLinesCopiedFromABulkReview(t *testing.T) {
	multi := "https://github.com/o/r/pull/10 (15m)\n- Reviewed the fix\n- Approved, no blocking issues\n\nhttps://github.com/o/s/pull/2 (10m)\n- Checked the scss"
	// A one-line field joins pasted lines with spaces.
	for _, text := range []string{multi, strings.ReplaceAll(multi, "\n", " ")} {
		got := ParsePastedReviews(text)
		if len(got) != 2 || got[0].URL != "https://github.com/o/r/pull/10" || got[0].Minutes != 15 ||
			len(got[0].Notes) != 2 || got[0].Notes[1] != "Approved, no blocking issues" || got[1].Minutes != 10 || len(got[1].Notes) != 1 {
			t.Fatalf("parsed %q as %+v", text, got)
		}
	}
	if got := ParsePastedReviews("https://github.com/o/r/pull/7 (20m)"); len(got) != 1 || got[0].Minutes != 20 {
		t.Fatalf("single line = %+v", got)
	}
	if got := ParsePastedReviews("no links here"); got != nil {
		t.Fatalf("no link = %+v", got)
	}
}
