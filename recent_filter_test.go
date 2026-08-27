package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// labels flattens every label/text string in a widget tree.
func labels(o fyne.CanvasObject) []string {
	var out []string
	switch v := o.(type) {
	case *widget.Label:
		out = append(out, v.Text)
	case *widget.Hyperlink:
		out = append(out, v.Text)
	case *widget.Button:
		out = append(out, v.Text)
	case *widget.Check:
		out = append(out, v.Text)
	case *canvas.Text:
		out = append(out, v.Text)
	case *tappable:
		out = append(out, labels(v.content)...)
	case *dayColumn:
		out = append(out, labels(v.body)...)
	case *dragTile:
		out = append(out, labels(v.content)...)
	case *widget.Card:
		if v.Content != nil {
			out = append(out, labels(v.Content)...)
		}
	case *fyne.Container:
		for _, c := range v.Objects {
			out = append(out, labels(c)...)
		}
	case *container.Scroll:
		out = append(out, labels(v.Content)...)
	}
	return out
}

// indexOf is contains with the answer to "and which one came first".
func indexOf(hay []string, needle string) int {
	for i, h := range hay {
		if strings.Contains(h, needle) {
			return i
		}
	}
	return -1
}

// inOrder reports whether the needles appear in the order given, each of them
// present. Reading order is top to bottom, so an earlier index is higher up.
func inOrder(hay []string, needles ...string) bool {
	last := -1
	for _, n := range needles {
		at := indexOf(hay, n)
		if at < 0 || at <= last {
			return false
		}
		last = at
	}
	return true
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if strings.Contains(h, needle) {
			return true
		}
	}
	return false
}

// The bottom list is a to-do: a pushed entry drops out of it, an unpushed one
// stays regardless of how old it is, and the checkbox brings the pushed ones
// back for this month.
func TestRecentShowsOnlyUnpushed(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: thisMonth(), repMonth: thisMonth()}
	ui.cfg = Config{Repos: []string{"bigledger"}}
	w.SetContent(ui.buildLogTab())
	// The week strip holds any waiting entry dated inside the week it shows, so
	// this list is tested with the strip parked well away from the rows below.
	ui.weekStart = "2020-01-06"

	month := thisMonth()
	lastMonth := shiftMonth(month, -1)
	if _, err := ui.store.AppendRows([]Row{
		{"date": month + "-02", "minutes": "60", "type": "commit",
			"description": "pushed already", "issue": "bigledger/repo#1",
			"pushed_at": time.Now().Format("2006-01-02T15:04:05")},
		{"date": month + "-03", "minutes": "90", "type": "commit",
			"description": "still waiting", "issue": "bigledger/repo#2"},
		{"date": lastMonth + "-20", "minutes": "30", "type": "commit",
			"description": "stale unpushed", "issue": "bigledger/repo#3"},
	}); err != nil {
		t.Fatal(err)
	}

	ui.drawRecent()
	got := labels(ui.recentBox)
	if contains(got, "pushed already") {
		t.Fatalf("a pushed entry must not be listed: %v", got)
	}
	for _, want := range []string{"still waiting", "stale unpushed"} {
		if !contains(got, want) {
			t.Fatalf("%q missing from the waiting list: %v", want, got)
		}
	}
	if !contains(got, "2 waiting to push") {
		t.Fatalf("header should count both unpushed rows: %v", got)
	}

	// Ticking the box reveals this month's pushed row; the stale one from last
	// month falls outside the month window by design.
	ui.showPushed.SetChecked(true)
	got = labels(ui.recentBox)
	if !contains(got, "pushed already") {
		t.Fatalf("checked box should reveal pushed entries: %v", got)
	}
	if !contains(got, "still waiting") {
		t.Fatalf("checked box should still list unpushed entries: %v", got)
	}
}

// With nothing left to push the list says so rather than rendering an empty
// table that reads like a failure.
func TestRecentEmptyWhenAllPushed(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: thisMonth(), repMonth: thisMonth()}
	ui.cfg = Config{Repos: []string{"bigledger"}}
	w.SetContent(ui.buildLogTab())

	if _, err := ui.store.AppendRows([]Row{{
		"date": thisMonth() + "-01", "minutes": "480", "type": "commit",
		"description": "done", "issue": "bigledger/repo#1",
		"pushed_at": "2026-08-01T10:00:00",
	}}); err != nil {
		t.Fatal(err)
	}
	ui.drawRecent()
	got := labels(ui.recentBox)
	if !contains(got, "Everything logged has been pushed") {
		t.Fatalf("expected the all-clear message, got: %v", got)
	}
	if !contains(got, "0 waiting to push") {
		t.Fatalf("expected a zero count, got: %v", got)
	}
}

// Bubbles go into the wrapping grid so several fit per line, and a long issue
// title is elided instead of blowing out its tile.
func TestPendingBubblesWrapIntoAGrid(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: thisMonth(), repMonth: thisMonth()}
	ui.cfg = Config{Repos: []string{"bigledger"}}
	w.SetContent(ui.buildLogTab())

	long := strings.Repeat("Category dropdown capped at one hundred rows ", 4)
	var groups []Group
	for i := 0; i < 5; i++ {
		groups = append(groups, Group{
			Date: "2026-08-0" + string(rune('1'+i)), Issue: "bigledger/repo#" + string(rune('1'+i)),
			Commits: []Commit{{Sha: "aaa111" + string(rune('1'+i)), Repo: "bigledger/repo"}},
		})
	}
	ui.pending = PendingResult{Count: 5, Range: []string{"2026-08-01", "2026-08-05"}, Groups: groups}
	ui.issueInfo = map[string]IssueInfo{
		"bigledger/repo#1": {Org: "bigledger", Repo: "repo", Number: 1, Title: long},
	}
	ui.renderPending()

	grid := findFlowGrid(ui.pendingBox)
	if grid == nil {
		t.Fatal("bubbles are not in a flowGrid, so they cannot flow several per line")
	}
	if len(grid.Objects) != len(groups) {
		t.Fatalf("grid holds %d tiles, want %d", len(grid.Objects), len(groups))
	}

	got := labels(grid)
	for _, s := range got {
		if len([]rune(s)) > bubbleTitleChars {
			t.Fatalf("tile text runs %d chars, past the %d that fit: %q",
				len([]rune(s)), bubbleTitleChars, s)
		}
	}
	// The footer carries the org-less form. The full "bigledger / repo #1" was
	// wide enough to collide with the date on the same line.
	if !contains(got, "repo #1") {
		t.Fatalf("tile footer lost the issue ref: %v", got)
	}
	if contains(got, "bigledger / repo") {
		t.Fatalf("tile footer still carries the org, which is what overflowed: %v", got)
	}
}

func TestShortIssueLabel(t *testing.T) {
	cases := []struct {
		info IssueInfo
		ref  string
		want string
	}{
		{IssueInfo{Org: "bigledger", Repo: "blg-int-general-task", Number: 6511},
			"bigledger/blg-int-general-task#6511", "blg-int-general-task #6511"},
		// Title lookup still in flight: the ref alone carries the same fields.
		{IssueInfo{}, "bigledger/blg-intranet#42", "blg-intranet #42"},
		// Unparseable refs are passed through rather than mangled.
		{IssueInfo{}, "not-a-ref", "not-a-ref"},
	}
	for _, c := range cases {
		if got := shortIssueLabel(c.info, c.ref); got != c.want {
			t.Fatalf("shortIssueLabel(%q) = %q, want %q", c.ref, got, c.want)
		}
	}
}

// The daily target is scored against the project, not the local CSV. Without a
// project configured the pane must say so rather than pass local minutes off as
// the real total.
func TestTodayProgressSourcesFromGitHub(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: thisMonth(), repMonth: thisMonth()}

	if got := ui.todayProgress(300, 300); !strings.Contains(got, "saved locally") ||
		!strings.Contains(got, "Settings") {
		t.Fatalf("no project configured should be stated plainly, got %q", got)
	}

	// With the month cached, the target is scored on GitHub's minutes (120),
	// and the 300 sitting unpushed locally is called out, not added in.
	ui.cfg = Config{ProjectURL: "https://github.com/orgs/bigledger/projects/9"}
	ui.ensureProjectCache()
	key := statusCacheKey(thisMonth())
	ui.projItems[key] = []WorklogItem{
		{Date: today(), Minutes: 120},
		{Date: "1999-01-01", Minutes: 999},
	}
	ui.projLoaded[key] = true

	got := ui.todayProgress(420, 300)
	if !strings.Contains(got, fmt.Sprintf("120/%d min", target)) {
		t.Fatalf("target should be scored on GitHub minutes, got %q", got)
	}
	if !strings.Contains(got, fmt.Sprintf("%d left", target-120)) {
		t.Fatalf("remaining should follow the GitHub total, got %q", got)
	}
	if !strings.Contains(got, "300 min saved locally, not pushed yet") {
		t.Fatalf("unpushed local minutes should be reported separately, got %q", got)
	}
	if strings.Contains(got, "420") {
		t.Fatalf("local total must not stand in for the GitHub total, got %q", got)
	}
}

// findFlowGrid returns the first container laid out by flowGrid.
func findFlowGrid(o fyne.CanvasObject) *fyne.Container {
	c, ok := o.(*fyne.Container)
	if !ok {
		return nil
	}
	if _, ok := c.Layout.(*flowGrid); ok {
		return c
	}
	for _, child := range c.Objects {
		if g := findFlowGrid(child); g != nil {
			return g
		}
	}
	return nil
}
