package tests

import (
	"reflect"
	"strings"
	"testing"
	"worklog/internal/shared/backend"
)

func TestParseBulkCodeReviewItems(t *testing.T) {
	items, err := backend.ParseCodeReviewItems(`
https://github.com/exampleorg/one/pull/67 (10m)
https://github.com/Example-Support/two/pull/5/files (25 minutes)
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := backend.CodeReviewMinutes(items); got != 35 {
		t.Fatalf("minutes = %d, want 35", got)
	}
	want := "https://github.com/exampleorg/one/pull/67 (10m)\n" +
		"https://github.com/Example-Support/two/pull/5 (25m)"
	if got := backend.CodeReviewBody(items); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestCanonicalPullRequestURL(t *testing.T) {
	got, ok := backend.CanonicalPullRequestURL("  https://github.com/exampleorg/app/pull/123/files?diff=split  ")
	if !ok || got != "https://github.com/exampleorg/app/pull/123" {
		t.Fatalf("canonical PR = %q, %v", got, ok)
	}
	if _, ok := backend.CanonicalPullRequestURL("https://github.com/exampleorg/app/issues/123"); ok {
		t.Fatal("an issue URL must not be classified as a PR")
	}
}

func TestSingleReviewStillAcceptsDirectIssueLink(t *testing.T) {
	ref, lookup, err := backend.ResolveSingleCodeReviewTarget("https://github.com/exampleorg/sample-intranet/issues/5804")
	if err != nil || ref != "exampleorg/sample-intranet#5804" || len(lookup.IssueRefs) != 0 {
		t.Fatalf("direct issue resolved as ref=%q lookup=%+v err=%v", ref, lookup, err)
	}
}

func TestBulkCodeReviewRejectsMissingMinutesAndNonPRs(t *testing.T) {
	for _, input := range []string{
		"https://github.com/o/r/pull/1",
		"https://github.com/o/r/issues/1 (10m)",
		"https://github.com/o/r/pull/1 (0m)",
	} {
		if _, err := backend.ParseCodeReviewItems(input); err == nil {
			t.Fatalf("%q should be refused", input)
		}
	}
}

func TestIssueRefsInPRTextReadsBothConventions(t *testing.T) {
	text := strings.Join([]string{
		"Refs exampleorg/sample-intranet/issues/5804:",
		"branch Example-Support/sample-finance#47",
		"duplicate EXAMPLEORG/sample-intranet/issues/5804",
	}, "\n")
	want := []string{"exampleorg/sample-intranet#5804", "Example-Support/sample-finance#47"}
	if got := backend.IssueRefsInText(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("refs = %#v, want %#v", got, want)
	}
}

func TestCodeReviewTargetLabelIncludesFetchedTitle(t *testing.T) {
	ref := "exampleorg/sample-intranet#5804"
	if got := backend.CodeReviewTargetLabel(ref, "Fix invoice rounding"); got != "Worklog issue: Fix invoice rounding  ·  "+ref {
		t.Fatalf("target label = %q", got)
	}
	if got := backend.CodeReviewTargetLabel(ref, ""); got != "Worklog issue: "+ref {
		t.Fatalf("fallback target label = %q", got)
	}
}
