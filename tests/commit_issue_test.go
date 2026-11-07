package tests

import (
	"strings"
	"testing"

	"worklog/internal/shared/backend"
)

func TestBranchIssueFallback(t *testing.T) {
	for branch, want := range map[string]string{
		"refs/heads/Example-Support/finance/issues/48": "Example-Support/finance#48",
		"Example-Support/finance#48":                   "Example-Support/finance#48",
		"issues/48":                                    "org/app#48",
		"feature/issue-48-rounding":                    "org/app#48",
		"48-fix-invoice-rounding":                      "org/app#48",
		"main":                                         "",
		"release/v1.4.8":                               "",
	} {
		if got := backend.ParseBranchIssue(branch, "org/app"); got != want {
			t.Errorf("branch %q = %q, want %q", branch, got, want)
		}
	}
}

func TestCommitBranchResolutionPreservesDeclarationsAndRejectsConflicts(t *testing.T) {
	commits := []backend.Commit{
		{Repo: "org/app", Sha: "a"}, // default-branch scan can finish first
		{Repo: "org/app", Sha: "a", Branch: "issues/48"},
		{Repo: "org/app", Sha: "b", Issue: "other/tracker#7", Branch: "issues/48"},
		{Repo: "org/app", Sha: "c", Branch: "issues/48"},
		{Repo: "org/app", Sha: "c", Branch: "issues/49"},
		{Repo: "org/other", Sha: "a", Branch: "main"},
	}
	backend.ResolveCommitBranchIssues(commits)
	want := []string{"org/app#48", "org/app#48", "other/tracker#7", "", "", ""}
	for i, c := range commits {
		if c.Issue != want[i] {
			t.Errorf("commit %d = %q, want %q", i, c.Issue, want[i])
		}
	}
}

func TestNamedBranchUsesLinkedPRIssueOnce(t *testing.T) {
	original := backend.Gh
	t.Cleanup(func() { backend.Gh = original })
	calls := 0
	backend.Gh = func(args []string, headers ...string) (string, error) {
		calls++
		if !strings.Contains(strings.Join(args, " "), "--head fix-invoice-rounding") {
			t.Fatalf("unexpected lookup: %v", args)
		}
		return `[{"title":"Fix invoice rounding","closingIssuesReferences":[{"url":"https://github.com/other/tracker/issues/7"}]}]`, nil
	}
	commits := []backend.Commit{
		{Repo: "org/app", Sha: "a", Branch: "fix-invoice-rounding"},
		{Repo: "org/app", Sha: "b", Branch: "fix-invoice-rounding"},
	}
	backend.ResolveCommitBranchIssues(commits)
	if calls != 1 || commits[0].Issue != "other/tracker#7" || commits[1].Issue != "other/tracker#7" {
		t.Fatalf("branch lookup calls=%d, commits=%+v", calls, commits)
	}
}
