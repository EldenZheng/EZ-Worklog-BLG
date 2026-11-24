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

// A bare #N the message only mentions ("Updated blg-grid #41") is a guess: the
// issue the branch is for wins. A keyword-declared #N or a full ref still beats
// the branch, and a guess stays when the branch names no issue.
func TestBranchIssueBeatsAPassingHashMention(t *testing.T) {
	const repo = "acme/intranet"
	body := "| Work | Time |\n- Updated blg-grid #41 to share the same feature flag pattern\n- Analysed #5432"
	issue, guessed := backend.ParseCommitIssue(body, repo)
	if issue != repo+"#41" || !guessed {
		t.Fatalf("passing mention = %q guessed=%v", issue, guessed)
	}
	if issue, guessed := backend.ParseCommitIssue("Fixes #12: rounding", repo); issue != repo+"#12" || guessed {
		t.Fatalf("keyword ref = %q guessed=%v", issue, guessed)
	}
	commits := []backend.Commit{
		{Repo: repo, Sha: "a", Issue: repo + "#41", IssueGuessed: true, Branch: "acme/intranet/issues/6537"},
		{Repo: repo, Sha: "b", Issue: repo + "#41", IssueGuessed: true, Branch: "main"},
		{Repo: repo, Sha: "c", Issue: repo + "#12", Branch: "acme/intranet/issues/6537"},
	}
	backend.ResolveCommitBranchIssues(commits)
	want := []string{repo + "#6537", repo + "#41", repo + "#12"}
	for i, c := range commits {
		if c.Issue != want[i] {
			t.Errorf("commit %d = %q, want %q", i, c.Issue, want[i])
		}
	}
	if commits[0].IssueGuessed || !commits[1].IssueGuessed {
		t.Errorf("guess flags = %v, %v", commits[0].IssueGuessed, commits[1].IssueGuessed)
	}
}
