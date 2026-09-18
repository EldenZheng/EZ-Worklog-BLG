package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseBulkCodeReviewItems(t *testing.T) {
	items, err := parseCodeReviewItems(`
https://github.com/bigledger/one/pull/67 (10m)
https://github.com/BigLedger-Support/two/pull/5/files (25 minutes)
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := codeReviewMinutes(items); got != 35 {
		t.Fatalf("minutes = %d, want 35", got)
	}
	want := "https://github.com/bigledger/one/pull/67 (10m)\n" +
		"https://github.com/BigLedger-Support/two/pull/5 (25m)"
	if got := codeReviewBody(items); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestCanonicalPullRequestURL(t *testing.T) {
	got, ok := canonicalPullRequestURL("  https://github.com/bigledger/app/pull/123/files?diff=split  ")
	if !ok || got != "https://github.com/bigledger/app/pull/123" {
		t.Fatalf("canonical PR = %q, %v", got, ok)
	}
	if _, ok := canonicalPullRequestURL("https://github.com/bigledger/app/issues/123"); ok {
		t.Fatal("an issue URL must not be classified as a PR")
	}
}

func TestSingleReviewStillAcceptsDirectIssueLink(t *testing.T) {
	ref, lookup, err := resolveSingleCodeReviewTarget("https://github.com/bigledger/blg-intranet/issues/5804")
	if err != nil || ref != "bigledger/blg-intranet#5804" || len(lookup.IssueRefs) != 0 {
		t.Fatalf("direct issue resolved as ref=%q lookup=%+v err=%v", ref, lookup, err)
	}
}

func TestBulkCodeReviewRejectsMissingMinutesAndNonPRs(t *testing.T) {
	for _, input := range []string{
		"https://github.com/o/r/pull/1",
		"https://github.com/o/r/issues/1 (10m)",
		"https://github.com/o/r/pull/1 (0m)",
	} {
		if _, err := parseCodeReviewItems(input); err == nil {
			t.Fatalf("%q should be refused", input)
		}
	}
}

func TestIssueRefsInPRTextReadsBothConventions(t *testing.T) {
	text := strings.Join([]string{
		"Refs bigledger/blg-intranet/issues/5804:",
		"branch BigLedger-Support/tuhu-finance#47",
		"duplicate BIGLEDGER/blg-intranet/issues/5804",
	}, "\n")
	want := []string{"bigledger/blg-intranet#5804", "BigLedger-Support/tuhu-finance#47"}
	if got := issueRefsInText(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("refs = %#v, want %#v", got, want)
	}
}
