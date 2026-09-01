package main

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// The window stays open for days, so a list fetched once per session went stale
// and hid every commit made after it: work done at noon was still missing at
// six. Returning to the tab refetches once the held list has aged out.
func TestAHeldPendingListGoesStale(t *testing.T) {
	if pendingIsStale(time.Now()) {
		t.Fatal("a list that just landed should be shown, not fetched again")
	}
	if !pendingIsStale(time.Now().Add(-pendingFresh)) {
		t.Fatal("a list older than the freshness window should be refetched")
	}
	// Zero value: nothing has ever landed, so there is nothing to keep.
	if !pendingIsStale(time.Time{}) {
		t.Fatal("with no fetch behind it the list is stale by definition")
	}
}

// A fresh list is not refetched when the tab comes back into view — the tab is
// switched to constantly, and every switch costing a round trip to GitHub is
// what the freshness window is for.
func TestReturningToTheTabKeepsAFreshList(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()

	ui.pendingLoaded = true
	ui.pendingAt = time.Now()
	ui.loadPending(false)
	if ui.pendingLoading {
		t.Fatal("a list fetched moments ago should be reused, not fetched again")
	}
}

// The Log tab asks GitHub with the same button as the other tabs, in the same
// words and with the same icon — three tabs, one way to refresh.
func TestLogTabRefreshLooksLikeTheOthers(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()

	tab := ui.buildLogTab()
	b := buttonNamed(tab, "Refresh from GitHub")
	if b == nil {
		t.Fatalf("the Log tab should carry the same refresh as Status: %v", labels(tab))
	}
	if b.Icon == nil {
		t.Fatal("the refresh button should carry the refresh icon the other tabs use")
	}
}

// A week runs Monday to Sunday and the cache is a month at a time, so the week
// of the 31st is served by two of them. Refreshing only today's month left the
// other half of the strip showing whatever it had cached — work pushed onto the
// 31st stayed invisible on the Log tab until Status was walked back a month and
// refreshed by hand.
func TestLogTabRefreshCoversEveryMonthTheWeekTouches(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: thisMonth(), repMonth: thisMonth()}
	ui.cfg = Config{ProjectURL: "https://github.com/orgs/bigledger/projects/9"}

	// A week that straddles the turn of a month needs both of them.
	ui.weekStart = "2026-08-31" // Monday; the week runs into September
	months := ui.logTabMonths()
	for _, want := range []string{"2026-08", "2026-09"} {
		if !containsStr(months, want) {
			t.Fatalf("a week across the turn needs %s: %v", want, months)
		}
	}
	// Today's month is always in, since the score at the foot is scored against
	// it whatever week the strip has been walked to.
	if !containsStr(months, thisMonth()) {
		t.Fatalf("today's month should always be refreshed: %v", months)
	}

	// A week sitting inside one month reaches no further than that month: a
	// refresh should not cost a fetch it has no use for. Today's month is still
	// there, so two is the most this can be.
	ui.weekStart = "2026-08-10" // Monday, and the whole week is August
	got := ui.logTabMonths()
	if containsStr(got, "2026-07") {
		t.Fatalf("a week inside August should not reach into July: %v", got)
	}
	if len(got) > 2 {
		t.Fatalf("today's month plus one is all this week needs: %v", got)
	}

	// Named once, however many ways it is reached — a month refreshed twice is
	// a wasted round trip to GitHub.
	ui.weekStart = weekStartOf(today())
	if names := ui.logTabMonths(); len(names) != len(uniqueStrs(names)) {
		t.Fatalf("a month should be named once: %v", names)
	}

	// And the refresh really drops them, so the next read goes to GitHub.
	ui.weekStart = "2026-08-31"
	ui.ensureProjectCache()
	for _, m := range []string{"2026-08", "2026-09"} {
		key := statusCacheKey(m)
		ui.projItems[key] = []WorklogItem{{Date: m + "-01", Minutes: 60}}
		ui.projLoaded[key] = true
		// Marked in flight so the refetch stops at loadProject's own guard: the
		// point under test is the cache being dropped, not a call to GitHub.
		ui.projLoading[key] = true
	}
	ui.refreshTodayScore()
	for _, m := range []string{"2026-08", "2026-09"} {
		if ui.projLoaded[statusCacheKey(m)] {
			t.Fatalf("%s should have been dropped so the next read refetches it", m)
		}
	}
}

func containsStr(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func uniqueStrs(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Refreshing the commits also drops the month behind the line at the foot of
// the tab: that score reads the Status cache, so refreshing one without the
// other left today's minutes reading whatever they were an hour ago.
func TestRefreshTodayScoreDropsTheStatusMonth(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: thisMonth(), repMonth: thisMonth()}
	ui.cfg = Config{ProjectURL: "https://github.com/orgs/bigledger/projects/9"}

	ui.ensureProjectCache()
	key := statusCacheKey(thisMonth())
	ui.projItems[key] = []WorklogItem{{Date: today(), Minutes: 120}}
	ui.projLoaded[key] = true
	// Marked in flight so the refetch stops at loadProject's own guard: the
	// point under test is the cache being dropped, not a call to GitHub.
	ui.projLoading[key] = true

	ui.refreshTodayScore()
	if ui.projLoaded[key] {
		t.Fatal("the month should be dropped so the next read refetches it")
	}

	// Nothing to score against: no project, no fetch, no error either.
	ui.cfg = Config{}
	ui.projLoaded[key] = true
	ui.refreshTodayScore()
	if !ui.projLoaded[key] {
		t.Fatal("with no project configured there is nothing to refresh")
	}
}

// A push writes a worklog item onto the project, so every cached range holding
// that date is now one item short — and only those. The month a date belongs to
// on the calendar is not the range it belongs to on the report: a report fetches
// its payroll period, the 21st through the 20th, plus the months of run-up it
// needs to see a weekend support set left unfinished at the cut-off. A push into
// that run-up really does change what the report says, so it counts as holding
// the date.
func TestAPushDropsOnlyTheRangesHoldingItsDate(t *testing.T) {
	loaded := map[string]bool{
		statusCacheKey("2026-08"): true, // 01 Aug – 31 Aug: holds it
		statusCacheKey("2026-07"): true, // 01 Jul – 31 Jul: does not
		reportCacheKey("2026-08"): true, // 14 Jul – 20 Aug: holds it, in period
		reportCacheKey("2026-09"): true, // 14 Aug – 20 Sep: holds it, in run-up
		reportCacheKey("2026-10"): true, // 14 Sep – 20 Oct: does not
		statusCacheKey("bad"):     true, // not a month, so not a range
	}
	// Asked for but never landed, so there is nothing cached to throw away.
	loading := map[string]bool{statusCacheKey("2026-08"): false}

	got := staleRanges(loaded, loading, "2026-08-14")
	want := map[string][2]string{
		statusCacheKey("2026-08"): {"2026-08-01", "2026-08-31"},
		reportCacheKey("2026-08"): {"2026-07-14", "2026-08-20"},
		reportCacheKey("2026-09"): {"2026-08-14", "2026-09-20"},
	}
	if len(got) != len(want) {
		t.Fatalf("a push should refetch the ranges holding its date, got %v", got)
	}
	for key, span := range want {
		if got[key] != span {
			t.Fatalf("%s should be refetched over %v, got %v", key, span, got[key])
		}
	}

	// A range never loaded costs nothing to leave alone: the next read of it
	// fetches it fresh anyway.
	if len(staleRanges(map[string]bool{statusCacheKey("2026-08"): false},
		nil, "2026-08-14")) != 0 {
		t.Fatal("a range that was never cached has nothing to drop")
	}
	// Nor is one already in the air refetched from here — it is marked instead.
	if len(staleRanges(map[string]bool{statusCacheKey("2026-08"): true},
		map[string]bool{statusCacheKey("2026-08"): true}, "2026-08-14")) != 0 {
		t.Fatal("a fetch already running should not have a second one started on it")
	}
}

// A push during a fetch is the case the cache cannot see: the answer already in
// the air was true when it was asked for and is stale by the time it lands.
// Marking it means loadProject throws it away and goes again.
func TestAPushDuringAFetchMarksItStale(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: thisMonth(), repMonth: thisMonth()}
	ui.cfg = Config{ProjectURL: "https://github.com/orgs/bigledger/projects/9"}

	ui.ensureProjectCache()
	key := statusCacheKey(thisMonth())
	ui.projLoading[key] = true

	ui.refreshAfterPush(today())
	if !ui.projStale[key] {
		t.Fatal("a fetch that was in the air when the push landed should be marked stale")
	}
	if !ui.projLoading[key] {
		t.Fatal("the running fetch should be left to finish, not restarted underneath itself")
	}

	// No project to read back from, so a push changes nothing worth fetching.
	ui.projStale = map[string]bool{}
	ui.cfg = Config{}
	ui.refreshAfterPush(today())
	if len(ui.projStale) != 0 {
		t.Fatal("with no project configured there is nothing to refetch")
	}
}

// A push takes seconds of round trips, so it puts a turning wheel in front of
// the window: the app is plainly working, and the push button is behind a modal
// rather than sitting there inviting a second click. The modal has to come down
// on every path out of the push, including the failed one — hence the stopper
// being safe to call twice.
func TestPushSpinnerCoversTheWindowUntilStopped(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	w.SetContent(widget.NewLabel("behind"))
	ui := &UI{store: newStore(t.TempDir()), win: w}

	stop := ui.pushSpinner("Pushing to GitHub…")
	if w.Canvas().Overlays().Top() == nil {
		t.Fatal("the spinner should be in front of the window while the push runs")
	}

	stop()
	if w.Canvas().Overlays().Top() != nil {
		t.Fatal("the spinner should come down when the push lands")
	}
	stop() // a second call is a no-op: the error path and the done path both stop it
}
