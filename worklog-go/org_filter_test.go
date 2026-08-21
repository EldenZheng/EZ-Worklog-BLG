package main

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// The Report tab opens on the period today is being worked into. A period runs
// the 21st to the 20th, so from the 21st the calendar month is the period that
// closed yesterday — every day worked since would be on a page you had to know
// to page forward to.
func TestReportOpensOnThePeriodTodayIsIn(t *testing.T) {
	at := func(s string) time.Time {
		when, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatalf("bad date %q: %v", s, err)
		}
		return when
	}
	cases := []struct{ day, want string }{
		{"2026-08-20", "2026-08"}, // the last day of the period ending in August
		{"2026-08-21", "2026-09"}, // the first day of the next one
		{"2026-08-01", "2026-08"},
		{"2026-12-21", "2027-01"}, // December rolls the year, not to month 13
		{"2026-01-31", "2026-02"}, // and never skips February the way +1 month does
	}
	for _, c := range cases {
		if got := periodOf(at(c.day)); got != c.want {
			t.Fatalf("%s belongs to period %s, got %s", c.day, c.want, got)
		}
	}

	// The period really does contain the day it was chosen for.
	from, to, err := reportBounds(periodOf(at("2026-08-21")))
	if err != nil {
		t.Fatalf("bounds: %v", err)
	}
	if from > "2026-08-21" || to < "2026-08-21" {
		t.Fatalf("21 Aug should sit inside %s – %s", from, to)
	}
}

// A fresh install assumes nothing about what anyone is paid. A default salary
// is the one wrong number that looks right — every other figure on the Report
// is real, so a made-up rate reads as this month's actual money.
func TestAFreshInstallAssumesNoSalary(t *testing.T) {
	s := newStore(t.TempDir())
	cfg, exists := s.LoadConfig()
	if exists {
		t.Fatal("an empty directory has no config to load")
	}
	if cfg.BaseSalary != 0 {
		t.Fatalf("no salary should be assumed, got %v", cfg.BaseSalary)
	}

	a := test.NewApp()
	defer a.Quit()
	// The stat tiles ask for bold monospace, which the test theme does not
	// define; laying them out under it panics inside the painter.
	test.ApplyTheme(t, theme.DefaultTheme())
	w := a.NewWindow("t")
	ui := &UI{store: s, win: w, hasCfg: true,
		calMonth: "2026-08", repMonth: "2026-08", cfg: cfg}
	w.SetContent(ui.buildReportTab())
	key := reportCacheKey("2026-08")
	ui.ensureProjectCache()
	ui.projItems[key] = []WorklogItem{{Date: "2026-08-11", Minutes: 480,
		URL: "https://github.com/bigledger/blg-intranet/issues/1"}}
	ui.projLoaded[key] = true
	ui.drawReport()

	if !contains(labels(ui.repBox), "Set your base salary") {
		t.Fatalf("the report should say the salary is unset: %v", labels(ui.repBox))
	}
	if contains(labels(ui.repBox), "over 21 working days") {
		t.Fatalf("no rate line until there is a rate: %v", labels(ui.repBox))
	}
}

// tapKey finds the colour-key entry for an org and clicks it. The match is on
// "<org> (" so that tapping bigledger does not land on bigledger-support, which
// starts with the same letters.
func tapKey(t *testing.T, root fyne.CanvasObject, org string) {
	t.Helper()
	var hit *tappable
	walk(root, func(o fyne.CanvasObject) {
		tp, ok := o.(*tappable)
		if !ok {
			return
		}
		for _, l := range labels(tp.content) {
			if strings.HasPrefix(l, org+" (") || l == org {
				hit = tp
			}
		}
	})
	if hit == nil {
		t.Fatalf("no key entry for %q among %v", org, labels(root))
	}
	hit.Tapped(&fyne.PointEvent{})
}

// The target line is what a bar is being measured against, so it cannot be
// drawn underneath one: the days that reach or pass it are exactly the days
// worth looking at, and those are the days that used to bury it.
func TestTheTargetLineIsDrawnOverTheBars(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	days := testChartDays("2026-08-01", "2026-08-02")
	days[0].byOrg = map[string]int{"bigledger": target + 120} // past the line
	days[0].total = target + 120
	c := newDayChart(Config{Repos: []string{"bigledger"}}, days, target)
	r := c.CreateRenderer().(*dayChartRenderer)
	c.Resize(fyne.NewSize(900, 300))
	r.Layout(fyne.NewSize(900, 300))

	paintedAt := func(want fyne.CanvasObject) int {
		for i, o := range r.Objects() {
			if o == want {
				return i
			}
		}
		return -1
	}
	line := paintedAt(r.guide)
	if line < 0 {
		t.Fatal("the target line is not in the scene")
	}
	for i, s := range r.slots {
		for j, seg := range s.segments {
			if paintedAt(seg) > line {
				t.Fatalf("bar %d segment %d paints over the target line", i, j)
			}
		}
	}

	// One unbroken line across the plot. Dashes over a full bar are half a line,
	// which is the same problem in a smaller size.
	if got := r.guide.Size().Width; got != 900 {
		t.Fatalf("the line should span the chart, got %g of 900", got)
	}
	if r.guide.Size().Height < 1 {
		t.Fatalf("the line needs a thickness to read, got %g", r.guide.Size().Height)
	}
}

// statusUI is a Status tab holding one day of each org's work.
func statusUI(t *testing.T) *UI {
	t.Helper()
	w := test.NewApp().NewWindow("t")
	// The test theme defines no bold monospace face, and the day panel asks for
	// one; laying it out under that theme panics inside the painter.
	test.ApplyTheme(t, theme.DefaultTheme())
	ui := &UI{store: newStore(t.TempDir()), win: w,
		calMonth: "2026-08", repMonth: "2026-08"}
	ui.cfg = Config{Repos: []string{"bigledger", "BigLedger-Support"}}
	w.SetContent(ui.buildStatusTab())

	ui.ensureProjectCache()
	key := statusCacheKey("2026-08")
	ui.projItems[key] = []WorklogItem{
		{Date: "2026-08-11", Minutes: 120, Title: "platform work",
			URL: "https://github.com/bigledger/blg-intranet/issues/1"},
		{Date: "2026-08-12", Minutes: 60, Title: "support work",
			URL: "https://github.com/BigLedger-Support/pcimage-operations/issues/2"},
	}
	ui.projLoaded[key] = true
	ui.drawCalendar()
	return ui
}

// Clicking an org in the key takes it out of the month, leaving the other one
// to be read on its own. Two employers' work in one calendar can only ever be
// read on top of each other otherwise.
func TestTappingTheKeyTakesAnOrgOutOfTheCalendar(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()

	if got := ui.calSummary.Text; !strings.HasPrefix(got, "180 min") {
		t.Fatalf("the month starts with both orgs in it, got %q", got)
	}

	tapKey(t, ui.calLegend, "bigledger")
	if got := ui.calSummary.Text; !strings.HasPrefix(got, "60 min") {
		t.Fatalf("hiding bigledger should leave only the support hours, got %q", got)
	}

	// The org that was switched off still holds its place in the key, at what it
	// is worth — it is the only way back, and a key that only shows what is
	// already on screen tells you nothing you cannot see.
	if !contains(labels(ui.calLegend), "bigledger (2h)") {
		t.Fatalf("the hidden org must stay in the key: %v", labels(ui.calLegend))
	}

	// The day panel is that cell spelled out, so it hides the same work.
	ui.selDay = "2026-08-11"
	ui.drawDayPanel()
	if contains(labels(ui.daySwap), "platform work") {
		t.Fatalf("a hidden org's work should not open from the calendar: %v", labels(ui.daySwap))
	}

	// Tapping again puts it back: this is a filter, not a delete.
	tapKey(t, ui.calLegend, "bigledger")
	if got := ui.calSummary.Text; !strings.HasPrefix(got, "180 min") {
		t.Fatalf("tapping again should restore the month, got %q", got)
	}
}

// The pending list turns over constantly — one org today, two tomorrow — so
// its key is drawn even for a single org. A key that comes and goes is a key
// you cannot look for, and the colour on the tiles is then never named.
func TestThePendingKeyIsDrawnForASingleOrg(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()

	ui.pendingLoaded = true
	ui.pending = PendingResult{Count: 1, Range: []string{"2026-08-20", "2026-08-21"},
		Groups: []Group{{Date: "2026-08-21", Issue: "bigledger/blg-sd-one-living#293",
			Commits: []Commit{{Sha: "aaa1111", Repo: "bigledger/blg-shared-utilities",
				Message: "gate the clone button", Full: "gate the clone button"}}}}}
	ui.renderPending()

	if !contains(labels(ui.pendingBox), "bigledger (1 commit(s))") {
		t.Fatalf("the only org on the list still has to be named: %v", labels(ui.pendingBox))
	}

	// Switching that one org off empties the list, and the list has to say that
	// is why — "no unlogged commits" over a key counting one is a contradiction.
	tapKey(t, ui.pendingBox, "bigledger")
	got := labels(ui.pendingBox)
	if !contains(got, "hidden by the filter") {
		t.Fatalf("an empty list should say the filter emptied it: %v", got)
	}
	if contains(got, "No unlogged commits") {
		t.Fatalf("hidden is not the same as none: %v", got)
	}
}

// The calendar is the other way round: a month of one org is one colour, and a
// key naming the only colour on screen is furniture above the grid.
func TestNoKeyWhenThereIsOnlyOneOrg(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()

	key := statusCacheKey("2026-08")
	ui.projItems[key] = []WorklogItem{{Date: "2026-08-11", Minutes: 120,
		URL: "https://github.com/bigledger/blg-intranet/issues/1"}}
	ui.drawCalendar()

	if len(ui.calLegend.Objects) != 0 {
		t.Fatalf("a single org needs no key: %v", labels(ui.calLegend))
	}
}

// The filter is one decision about what you are looking at, so it holds across
// the tabs rather than being made again on each. The report's own numbers move
// with it — the period really is worth less when one employer is left out.
func TestTheFilterCarriesToTheReport(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()

	ui.hasCfg = true
	ui.win.SetContent(ui.buildReportTab())
	key := reportCacheKey("2026-08")
	ui.projItems[key] = []WorklogItem{
		{Date: "2026-08-11", Minutes: 120, Title: "platform work",
			URL: "https://github.com/bigledger/blg-intranet/issues/1"},
		{Date: "2026-08-12", Minutes: 60, Title: "support work",
			URL: "https://github.com/BigLedger-Support/pcimage-operations/issues/2"},
	}
	ui.projLoaded[key] = true
	ui.drawReport()

	if !contains(labels(ui.repBox), "3.0h") {
		t.Fatalf("the whole period is three hours: %v", labels(ui.repBox))
	}

	ui.toggleOrg("bigledger")
	if !contains(labels(ui.repBox), "1.0h") {
		t.Fatalf("with bigledger hidden the period is one hour: %v", labels(ui.repBox))
	}
	// The split is the report's key, so it keeps naming the hidden org — and at
	// its real share of the period, not recomputed to 100% of what is left.
	got := labels(ui.repBox)
	if !contains(got, "bigledger") {
		t.Fatalf("the hidden org must stay in the split: %v", got)
	}
	if !contains(got, "1h · 33%") {
		t.Fatalf("the visible org keeps its share of the whole period: %v", got)
	}
}

// The pending list carries the same key and the same filter, so a day spent on
// one employer can be logged without the other one's commits in the way.
func TestTappingTheKeyFiltersThePendingList(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()

	ui.cfg = Config{Repos: []string{"bigledger", "BigLedger-Support"}}
	ui.pendingLoaded = true
	ui.pending = PendingResult{Count: 2, Range: []string{"2026-08-10", "2026-08-12"},
		Groups: []Group{
			{Date: "2026-08-11", Issue: "bigledger/blg-intranet#42",
				Commits: []Commit{{Sha: "aaa1111", Repo: "bigledger/blg-intranet",
					Message: "cap the dropdown", Full: "cap the dropdown"}}},
			{Date: "2026-08-11", Issue: "BigLedger-Support/pcimage-operations#51",
				Commits: []Commit{{Sha: "bbb2222", Repo: "bigledger/blg-shared-utilities",
					Message: "widen the picker", Full: "widen the picker"}}},
		}}
	ui.renderPending()

	got := labels(ui.pendingBox)
	if !contains(got, "cap the dropdown") || !contains(got, "widen the picker") {
		t.Fatalf("both commits start out on the list: %v", got)
	}
	if !contains(got, "bigledger (1 commit(s))") {
		t.Fatalf("the key counts commits, not hours: %v", got)
	}

	tapKey(t, ui.pendingBox, "bigledger")
	got = labels(ui.pendingBox)
	if contains(got, "cap the dropdown") {
		t.Fatalf("the hidden org's commit should be off the list: %v", got)
	}
	if !contains(got, "widen the picker") {
		t.Fatalf("the other org's commit should still be there: %v", got)
	}
	// Still counted in the key at its full size, so it can be brought back.
	if !contains(got, "bigledger (1 commit(s))") {
		t.Fatalf("the hidden org keeps its count in the key: %v", got)
	}
}
