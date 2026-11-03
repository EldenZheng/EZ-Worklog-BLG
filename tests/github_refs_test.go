package tests

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
)

// An issue field reads every spelling GitHub uses: the shorthand, the path
// with or without the host, and the full link with anything after the number.
func TestIssueFieldsReadEverySpelling(t *testing.T) {
	want := "Example-Support/sample-ops#124"
	for _, in := range []string{
		"Example-Support/sample-ops#124",
		"Example-Support/sample-ops/issues/124",
		"github.com/Example-Support/sample-ops/issues/124",
		"www.github.com/Example-Support/sample-ops/issues/124",
		"https://github.com/Example-Support/sample-ops/issues/124",
		"  https://github.com/Example-Support/sample-ops/issues/124#issuecomment-99  ",
		"http://github.com/Example-Support/sample-ops/issues/124/",
	} {
		got, err := backend.IssueRefFromAny(in)
		if err != nil || got != want {
			t.Fatalf("%q = (%q, %v), want %q", in, got, err, want)
		}
	}
	// A PR is not an issue: time goes on the issue it belongs to.
	for _, pr := range []string{
		"Example-Support/sample-ops/pull/124",
		"https://github.com/Example-Support/sample-ops/pull/124/files",
	} {
		_, err := backend.IssueRefFromAny(pr)
		if err == nil || !strings.Contains(err.Error(), "pull request") {
			t.Fatalf("%q should be refused as a pull request, got %v", pr, err)
		}
	}
}

// A PR is read in the same spellings. The bare shorthand counts as a PR only
// where nothing else could be meant.
func TestPullRequestFieldsReadEverySpelling(t *testing.T) {
	want := "https://github.com/Example-Support/sample-ops/pull/7"
	for _, in := range []string{
		"Example-Support/sample-ops/pull/7",
		"github.com/Example-Support/sample-ops/pull/7",
		"https://github.com/Example-Support/sample-ops/pull/7/files?diff=split",
		"https://github.com/Example-Support/sample-ops/pulls/7",
	} {
		if got, ok := backend.CanonicalPullRequestURL(in); !ok || got != want {
			t.Fatalf("%q = (%q, %v), want %q", in, got, ok, want)
		}
		if got, ok := backend.PullRequestURLFromAny(in); !ok || got != want {
			t.Fatalf("bulk %q = (%q, %v), want %q", in, got, ok, want)
		}
	}
	if _, ok := backend.CanonicalPullRequestURL("Example-Support/sample-ops#7"); ok {
		t.Fatal("the shorthand may be an issue, so the single review field must ask GitHub")
	}
	if got, ok := backend.PullRequestURLFromAny("Example-Support/sample-ops#7"); !ok || got != want {
		t.Fatalf("the bulk sheet holds only PRs, so the shorthand is one: got (%q, %v)", got, ok)
	}
	items, err := backend.ParseCodeReviewItems("Example-Support/sample-ops#7 (10m)\nExample-Support/sample-ops/pull/8 (5m)")
	if err != nil {
		t.Fatal(err)
	}
	if items[0].URL != want || items[1].URL != "https://github.com/Example-Support/sample-ops/pull/8" {
		t.Fatalf("bulk items = %+v", items)
	}
}

// stubReview stands in for GitHub: which shorthand refs are PRs, the issues
// each PR names, and issue titles.
func stubReview(t *testing.T, prs map[string]bool, issues map[string][]string, titles map[string]string) {
	t.Helper()
	oldPR, oldLookup, oldInfo := backend.PullRequestForRef, backend.FetchCodeReviewLookup, backend.FetchIssueInfo
	backend.PullRequestForRef = func(ref string) (string, bool) {
		if !prs[ref] {
			return "", false
		}
		url, _ := backend.PullRequestURLFromAny(ref)
		return url, true
	}
	backend.FetchCodeReviewLookup = func(prURL string) (backend.CodeReviewLookup, error) {
		return backend.CodeReviewLookup{URL: prURL, IssueRefs: issues[prURL]}, nil
	}
	backend.FetchIssueInfo = func(ref string) (backend.IssueInfo, error) {
		return backend.IssueInfo{Title: titles[ref]}, nil
	}
	t.Cleanup(func() {
		backend.PullRequestForRef, backend.FetchCodeReviewLookup, backend.FetchIssueInfo = oldPR, oldLookup, oldInfo
	})
}

// A single review takes the PR or the issue in any spelling, and a PR naming
// several issues hands all of them back to be chosen from.
func TestCodeReviewTargetsTakeAnySpelling(t *testing.T) {
	pr := "https://github.com/exampleorg/app/pull/9"
	stubReview(t, map[string]bool{"exampleorg/app#9": true},
		map[string][]string{pr: {"exampleorg/tasks#1", "exampleorg/tasks#2"}}, nil)

	for _, in := range []string{pr, "exampleorg/app/pull/9", "exampleorg/app#9"} {
		got, err := backend.CodeReviewTargets(in)
		if err != nil || !reflect.DeepEqual(got.IssueRefs, []string{"exampleorg/tasks#1", "exampleorg/tasks#2"}) {
			t.Fatalf("%q = (%v, %v), want both issues", in, got.IssueRefs, err)
		}
	}
	for _, in := range []string{"exampleorg/tasks#5", "exampleorg/tasks/issues/5", "https://github.com/exampleorg/tasks/issues/5"} {
		got, err := backend.CodeReviewTargets(in)
		if err != nil || !reflect.DeepEqual(got.IssueRefs, []string{"exampleorg/tasks#5"}) {
			t.Fatalf("%q = (%v, %v), want the issue itself", in, got.IssueRefs, err)
		}
	}
	if _, err := backend.CodeReviewTargets("https://github.com/exampleorg/app/pull/10"); err == nil {
		t.Fatal("a PR that names no issue has nothing to file under")
	}
}

// When the PR names several issues, a dropdown beside the link lists them by
// title, and the review is not saved until one is picked.
func TestCodeReviewAsksWhichIssueWhenThePRNamesSeveral(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	pr := "https://github.com/exampleorg/app/pull/9"
	stubReview(t, map[string]bool{"exampleorg/app#9": true},
		map[string][]string{pr: {"exampleorg/tasks#1", "exampleorg/tasks#2"}},
		map[string]string{"exampleorg/tasks#1": "Fix invoice rounding", "exampleorg/tasks#2": "Export ledger"})

	tab := logwork.BuildLogTab(ui)
	var kinds *widget.RadioGroup
	walk(tab, func(o fyne.CanvasObject) {
		if g, ok := o.(*widget.RadioGroup); ok && len(g.Options) > 0 && g.Options[0] == "Commits" {
			kinds = g
		}
	})
	if kinds == nil {
		t.Fatal("missing Log Work kind selector")
	}
	kinds.SetSelected("Code Review")
	link := entryByPlaceholder(tab, logwork.ReviewPlaceholder)
	mins := entryByPlaceholder(tab, "minutes")
	if link == nil || mins == nil {
		t.Fatal("Code Review form is missing its link or minutes field")
	}
	var pick *widget.Select
	walk(tab, func(o fyne.CanvasObject) {
		if s, ok := o.(*widget.Select); ok && s.PlaceHolder == "Pick the issue" {
			pick = s
		}
	})
	if pick == nil {
		t.Fatal("no issue dropdown beside the link")
	}

	mins.SetText("15")
	link.SetText("exampleorg/app#9")
	deadline := time.Now().Add(5 * time.Second)
	for len(pick.Options) < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	want := []string{"tasks#1  ·  Fix invoice rounding", "tasks#2  ·  Export ledger"}
	if !reflect.DeepEqual(pick.Options, want) || !pick.Visible() {
		t.Fatalf("dropdown options = %v (visible %v), want %v", pick.Options, pick.Visible(), want)
	}

	test.Tap(buttonNamed(tab, "Save draft"))
	if rows, _ := ui.Store.ReadRows(); len(rows) != 0 {
		t.Fatalf("nothing should be saved before an issue is picked, got %v", rows)
	}
	if !contains(labels(tab), "This PR references 2 issues. Pick the one to log under from the list beside the link.") {
		t.Fatalf("the form should ask for a pick, labels %v", labels(tab))
	}

	pick.SetSelectedIndex(1)
	test.Tap(buttonNamed(tab, "Save draft"))
	rows, err := ui.Store.ReadRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["issue"] != "exampleorg/tasks#2" || rows[0]["issue_title"] != "Export ledger" {
		t.Fatalf("saved rows = %v, want one filed under the picked issue", rows)
	}
}

// The row editors store a pasted link the way every other row is stored.
func TestEditorIssueFieldStoresTheShorthand(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	made, err := ui.Store.AppendRows([]backend.Row{{
		"date": "2026-08-10", "minutes": "30", "type": backend.KindOther, "issue": "exampleorg/app#1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	form := logwork.RowEditor(ui, made[0], func() {}, nil).All()
	field := entryByPlaceholder(form, logwork.IssuePlaceholder)
	if field == nil {
		t.Fatal("row editor has no issue field")
	}
	field.SetText("https://github.com/Example-Support/sample-ops/issues/124")
	test.Tap(buttonNamed(form, "Save"))
	rows, _ := ui.Store.ReadRows()
	if len(rows) != 1 || rows[0]["issue"] != "Example-Support/sample-ops#124" {
		t.Fatalf("saved issue = %v, want Example-Support/sample-ops#124", rows)
	}

	field.SetText("Example-Support/sample-ops/pull/7")
	test.Tap(buttonNamed(form, "Save"))
	rows, _ = ui.Store.ReadRows()
	if rows[0]["issue"] != "Example-Support/sample-ops#124" {
		t.Fatalf("a PR link must not be saved as the issue, got %q", rows[0]["issue"])
	}
}
