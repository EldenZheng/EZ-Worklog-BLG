package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func logListUI(t *testing.T) *UI {
	t.Helper()
	w := test.NewApp().NewWindow("t")
	test.ApplyTheme(t, theme.DefaultTheme())
	ui := &UI{store: newStore(t.TempDir()), win: w,
		calMonth: "2026-08", repMonth: "2026-08", weekStart: "2026-08-24"}
	ui.cfg = Config{
		Repos:        []string{"bigledger", "BigLedger-Support"},
		WorklogOwner: "blg-elden",
		ProjectURL:   "https://github.com/orgs/bigledger/projects/9",
	}
	ui.issueInfo = map[string]IssueInfo{}
	ui.ensureProjectCache()
	w.SetContent(ui.buildLogListTab())
	return ui
}

// The two kinds of waiting work are one question — what does this issue still
// owe the board — so they land in one list per issue, not two lists to compare.
func TestPendingByIssueMergesSavedMinutesAndUnloggedCommits(t *testing.T) {
	ui := logListUI(t)
	defer fyne.CurrentApp().Quit()

	if _, err := ui.store.AppendRows([]Row{
		{"date": "2026-08-25", "minutes": "240", "type": kindCommit, "issue": "bigledger/repo#1"},
		{"date": "2026-08-26", "minutes": "60", "type": kindCommit, "issue": "bigledger/repo#1"},
		// Already on the board: not waiting on anything.
		{"date": "2026-08-26", "minutes": "480", "type": kindCommit, "issue": "bigledger/repo#9",
			"pushed_at": "2026-08-26T10:00:00"},
		// No ref until it is pushed — named for what it will become rather than
		// lumped in with everything else that has no issue.
		{"date": "2026-08-27", "minutes": "90", "type": kindMeeting},
	}); err != nil {
		t.Fatal(err)
	}
	ui.pending = PendingResult{Groups: []Group{
		// Same issue as the saved minutes above: one row, both amounts.
		{Date: "2026-08-27", Issue: "bigledger/repo#1", Commits: []Commit{{Sha: "a"}, {Sha: "b"}}},
		{Date: "2026-08-27", Issue: "bigledger/repo#2", Commits: []Commit{{Sha: "c"}}},
		// Ignored commits are fetched so they can be handed back; they are not
		// work still to do.
		{Date: "2026-08-27", Issue: "bigledger/repo#3",
			Commits: []Commit{{Sha: "d"}}, Ignored: true},
	}}
	ui.pendingLoaded = true

	got := ui.pendingByIssue()
	byIssue := map[string]pendingIssue{}
	for _, p := range got {
		byIssue[p.Issue] = p
	}

	if p := byIssue["bigledger/repo#1"]; p.Minutes != 300 || p.Commits != 2 || p.Entries != 2 {
		t.Fatalf("one issue's saved minutes and unlogged commits should meet: %+v", p)
	}
	if p := byIssue["bigledger/repo#2"]; p.Minutes != 0 || p.Commits != 1 {
		t.Fatalf("a commit-only issue should still be listed: %+v", p)
	}
	if _, listed := byIssue["bigledger/repo#3"]; listed {
		t.Fatal("an ignored commit is not work still to do")
	}
	if _, listed := byIssue["bigledger/repo#9"]; listed {
		t.Fatal("a pushed entry is on the board, not pending")
	}
	if p := byIssue["Elden Meeting & ad hocs: 2026-08-27"]; p.Minutes != 90 {
		t.Fatalf("a meeting should be named for the issue it will become: %+v", byIssue)
	}

	// Nearest to pushable first: minutes lead, then commit count.
	if got[0].Issue != "bigledger/repo#1" {
		t.Fatalf("the biggest waiting issue should lead, got %q", got[0].Issue)
	}
}

// The tab draws both columns and the boards above them, and says what it is
// waiting on rather than reading as empty.
func TestLogListDrawsBoardsAndBothColumns(t *testing.T) {
	ui := logListUI(t)
	defer fyne.CurrentApp().Quit()

	key := statusCacheKey("2026-08")
	ui.projItems[key] = []WorklogItem{
		{Date: "2026-08-25", Minutes: 300, Title: "Worklog: 2026-08-25",
			URL:         "https://github.com/bigledger/blg-intranet/issues/2",
			ParentTitle: "Chinese translation", ParentURL: "https://github.com/bigledger/blg-intranet/issues/1"},
	}
	ui.projLoaded[key] = true
	if _, err := ui.store.AppendRows([]Row{
		{"date": "2026-08-26", "minutes": "120", "type": kindCommit, "issue": "bigledger/repo#7"},
	}); err != nil {
		t.Fatal(err)
	}
	ui.pendingLoaded = true
	ui.drawLogList()

	got := labels(ui.listBox)
	for _, want := range []string{
		"Boards for this week",
		"bigledger · project 9 · view 19",
		"By issue",
		"Chinese translation", // the board column
		"Pending — not on the board yet",
		"120m", // the pending column
	} {
		if !contains(got, want) {
			t.Fatalf("Log List is missing %q: %v", want, got)
		}
	}
}

// Before the commits have landed the pending column has to say so. Reading
// "Nothing waiting" while the fetch is still in the air is a different claim.
func TestPendingColumnSaysItIsStillReading(t *testing.T) {
	ui := logListUI(t)
	defer fyne.CurrentApp().Quit()

	ui.drawLogList()
	if got := labels(ui.listBox); !contains(got, "Reading your commits from GitHub…") {
		t.Fatalf("an unloaded pending list should say so: %v", got)
	}

	ui.pendingLoaded = true
	ui.drawLogList()
	if got := labels(ui.listBox); !contains(got, "Nothing waiting.") {
		t.Fatalf("once loaded and empty it should say so: %v", got)
	}
}

// The week is scored against every day in it, weekends included: weekend
// support is a worked, paid rota here, so a Saturday's minutes count towards the
// week like any other day's.
func TestWeekGoalCountsEveryDay(t *testing.T) {
	full := weekDates("2026-08-24") // Monday to Sunday
	if got := weekGoal(full); got != 7*target {
		t.Fatalf("a full week is 8h × 7, got %d want %d", got, 7*target)
	}
	// A part-week scores against the days it is actually showing.
	if got := weekGoal([]string{"2026-08-29", "2026-08-30"}); got != 2*target {
		t.Fatalf("a weekend is still two days, got %d", got)
	}
	if got := weekGoal([]string{"not a date"}); got != 0 {
		t.Fatalf("an unreadable date should not add a day, got %d", got)
	}
}

// The bar reads the week the way the strip reads a day: what is banked, and
// what is only saved here.
func TestWeekProgressPanelScoresBankedAndWaitingApart(t *testing.T) {
	ui := logListUI(t)
	defer fyne.CurrentApp().Quit()

	key := statusCacheKey("2026-08")
	ui.projItems[key] = []WorklogItem{
		{Date: "2026-08-25", Minutes: 480, URL: "https://github.com/bigledger/blg-intranet/issues/1"},
		{Date: "2026-08-26", Minutes: 240, URL: "https://github.com/bigledger/blg-intranet/issues/2"},
	}
	ui.projLoaded[key] = true
	if _, err := ui.store.AppendRows([]Row{
		{"date": "2026-08-27", "minutes": "300", "type": kindCommit, "issue": "bigledger/repo#7"},
		// Outside the shown week: it belongs to another week's score.
		{"date": "2026-09-10", "minutes": "999", "type": kindCommit, "issue": "bigledger/repo#8"},
		// Already pushed: the board reports it, so counting it here doubles it.
		{"date": "2026-08-25", "minutes": "480", "type": kindCommit, "issue": "bigledger/repo#9",
			"pushed_at": "2026-08-25T10:00:00"},
	}); err != nil {
		t.Fatal(err)
	}
	ui.pendingLoaded = true
	ui.drawLogList()

	got := labels(ui.listBox)
	if !contains(got, "This week") {
		t.Fatalf("the week needs a score of its own: %v", got)
	}
	// 720 banked of 3360 (8h × 7) is 21%; the 300 saved locally takes it to
	// 1020, which is 30% — the number the drafts actually raise.
	if !contains(got, "720 min") || !contains(got, "of 56h") || !contains(got, "21%") {
		t.Fatalf("the week should be scored against all seven days: %v", got)
	}
	// 720 + 300 = 1020 of 3360, leaving 2340 to find.
	if !contains(got, "+300 min waiting · 1020/3360 min (2340m left) 30%") {
		t.Fatalf("the drafts should say what the week would come to: %v", got)
	}
	// Another week's entry must not reach this week's bar. It is still listed in
	// the pending column beside it, which is not week-scoped on purpose — that
	// column is there so nothing waiting goes unnoticed.
	if contains(got, "+1299 min waiting to push") {
		t.Fatalf("another week's entry was counted into this week's bar: %v", got)
	}
}

// The day band reads the same as the strip on Log Work — the same score, the
// same split, the same colours — so a day means one thing on either tab. What it
// does not carry is the cards: nothing is dragged here.
func TestListDaysPanelMirrorsTheStripWithoutTheCards(t *testing.T) {
	ui := logListUI(t)
	defer fyne.CurrentApp().Quit()

	key := statusCacheKey("2026-08")
	ui.projItems[key] = []WorklogItem{
		{Date: "2026-08-25", Minutes: 480, URL: "https://github.com/bigledger/blg-intranet/issues/1"},
		{Date: "2026-08-26", Minutes: 240, URL: "https://github.com/bigledger/blg-intranet/issues/2"},
	}
	ui.projLoaded[key] = true
	if _, err := ui.store.AppendRows([]Row{
		{"date": "2026-08-26", "minutes": "300", "type": kindCommit,
			"issue": "bigledger/repo#7", "description": "standup notes"},
	}); err != nil {
		t.Fatal(err)
	}
	ui.pendingLoaded = true
	ui.drawLogList()

	got := labels(ui.listBox)
	for _, day := range []string{"Mon 24", "Tue 25", "Wed 26", "Sun 30"} {
		if !contains(got, day) {
			t.Fatalf("the week should run a column per day, missing %s: %v", day, got)
		}
	}
	// A finished day says so; a short one scores itself; an empty one is not
	// broken, it is empty.
	if !contains(got, "480 min · 100% ✓") {
		t.Fatalf("a day that made target should be marked: %v", got)
	}
	if !contains(got, "nothing logged") {
		t.Fatalf("an empty day should say so: %v", got)
	}
	// Banked and waiting are scored apart, then together — the same two captions
	// the strip carries.
	if !contains(got, "240 min · 50% · +300 min") {
		t.Fatalf("waiting minutes should be named beside the banked ones: %v", got)
	}
	if !contains(got, "540 min · 112% ✓") {
		t.Fatalf("the day should also score what it would come to: %v", got)
	}
	// The card itself belongs on Log Work, where it can be dragged.
	if contains(got, "standup notes") {
		t.Fatalf("Log List shows the score, not the cards: %v", got)
	}
}

// The amount reads in whatever unit the issue actually has.
func TestPendingAmountReadsInTheUnitItHas(t *testing.T) {
	for _, c := range []struct {
		in   pendingIssue
		want string
	}{
		{pendingIssue{Minutes: 300, Commits: 2}, "300m +2c"},
		{pendingIssue{Minutes: 300}, "300m"},
		{pendingIssue{Commits: 1}, "1 commit"},
		{pendingIssue{Commits: 3}, "3 commits"},
	} {
		if got := pendingAmount(c.in); got != c.want {
			t.Fatalf("pendingAmount(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}
