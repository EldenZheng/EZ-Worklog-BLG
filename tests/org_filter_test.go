package tests

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/menus/logwork"
	"worklog/internal/menus/report"
	"worklog/internal/menus/status"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
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
		if got := shell.PeriodOf(at(c.day)); got != c.want {
			t.Fatalf("%s belongs to period %s, got %s", c.day, c.want, got)
		}
	}

	// The period really does contain the day it was chosen for.
	from, to, err := backend.ReportBounds(shell.PeriodOf(at("2026-08-21")))
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
	s := backend.NewStore(tempDir(t))
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
	ui := &shell.UI{Store: s, Win: w, HasCfg: true,
		CalMonth: "2026-08", RepMonth: "2026-08", Cfg: cfg}
	w.SetContent(report.BuildReportTab(ui))
	key := shell.ReportCacheKey("2026-08")
	ui.EnsureProjectCache()
	ui.ProjItems[key] = []backend.WorklogItem{{Date: "2026-08-11", Minutes: 480,
		URL: "https://github.com/exampleorg/sample-intranet/issues/1"}}
	ui.ProjLoaded[key] = true
	report.DrawReport(ui)

	if !contains(labels(ui.RepBox), "Set your base salary") {
		t.Fatalf("the report should say the salary is unset: %v", labels(ui.RepBox))
	}
	if contains(labels(ui.RepBox), "over 21 working days") {
		t.Fatalf("no rate line until there is a rate: %v", labels(ui.RepBox))
	}
}

// The Report tab fetches months before the period it shows, so a support set
// still unfinished at the cut-off can be seen. The three issues logged after
// the boundary finish the four logged before it, and the set is paid once, in
// the period that completed it — without anything being saved anywhere.
func TestTheReportCarriesAnUnfinishedSupportSetAcrossTheCutOff(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())
	w := a.NewWindow("t")
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w, HasCfg: true,
		CalMonth: "2026-08", RepMonth: "2026-09",
		Cfg: backend.Config{Repos: []string{"exampleorg"}, SupportSetPay: 125, BaseSalary: 2800, Currency: "RM"}}
	w.SetContent(report.BuildReportTab(ui))

	// Period "2026-09" runs 21 Aug to 20 Sep. Four support issues fall the far
	// side of that opening, three inside it.
	support := func(date string, from, to int) []backend.WorklogItem {
		var out []backend.WorklogItem
		for i := from; i < to; i++ {
			out = append(out, backend.WorklogItem{Date: date, Minutes: 60,
				Title: "Weekend support",
				URL:   fmt.Sprintf("https://github.com/exampleorg/support/issues/%d", i)})
		}
		return out
	}
	key := shell.ReportCacheKey("2026-09")
	ui.EnsureProjectCache()
	ui.ProjItems[key] = append(support("2026-08-16", 0, 4), support("2026-08-30", 4, 7)...)
	ui.ProjLoaded[key] = true
	report.DrawReport(ui)

	got := labels(ui.RepBox)
	if !contains(got, "4 carried in + 3 issues, 1 set(s)") {
		t.Fatalf("the four before the cut-off should finish the set: %v", got)
	}
	if !contains(got, "RM 125.00") {
		t.Fatalf("a completed set uses the configured rate: %v", got)
	}
	// The run-up is only there for the carry: it must not reach the period's own
	// figures. Three issues at an hour each is all this period logged.
	if !contains(got, "3.0h") {
		t.Fatalf("only the period's own hours count: %v", got)
	}
}

// A period that ends part-way through a set says how much is going forward, so
// the number is not a surprise when it turns up in the next report.
func TestTheReportNamesWhatCarriesToTheNextPeriod(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())
	w := a.NewWindow("t")
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w, HasCfg: true,
		CalMonth: "2026-08", RepMonth: "2026-09",
		Cfg: backend.Config{Repos: []string{"exampleorg"}, SupportSetPay: 125, BaseSalary: 2800, Currency: "RM"}}
	w.SetContent(report.BuildReportTab(ui))

	var items []backend.WorklogItem
	for i := 0; i < 4; i++ {
		items = append(items, backend.WorklogItem{Date: "2026-08-30", Minutes: 60,
			Title: "Weekend support",
			URL:   fmt.Sprintf("https://github.com/exampleorg/support/issues/%d", i)})
	}
	key := shell.ReportCacheKey("2026-09")
	ui.EnsureProjectCache()
	ui.ProjItems[key] = items
	ui.ProjLoaded[key] = true
	report.DrawReport(ui)

	got := labels(ui.RepBox)
	if !contains(got, "4 issues, 0 set(s), 4 carry to next") {
		t.Fatalf("an unfinished set should say what it leaves behind: %v", got)
	}
}

// tapKey finds the colour-key entry for an org and clicks it. The match is on
// "<org> (" so that tapping exampleorg does not land on example-support, which
// starts with the same letters.
func tapKey(t *testing.T, root fyne.CanvasObject, org string) {
	t.Helper()
	var hit *shell.Tappable
	walk(root, func(o fyne.CanvasObject) {
		tp, ok := o.(*shell.Tappable)
		if !ok {
			return
		}
		for _, l := range labels(tp.Content) {
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
	days[0].ByOrg = map[string]int{"exampleorg": backend.Target + 120} // past the line
	days[0].Total = backend.Target + 120
	c := report.NewDayChart(backend.Config{Repos: []string{"exampleorg"}}, days, backend.Target)
	r := c.CreateRenderer().(*report.DayChartRenderer)
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
	line := paintedAt(r.Guide)
	if line < 0 {
		t.Fatal("the target line is not in the scene")
	}
	for i, s := range r.Slots {
		for j, seg := range s.Segments {
			if paintedAt(seg) > line {
				t.Fatalf("bar %d segment %d paints over the target line", i, j)
			}
		}
	}

	// One unbroken line across the plot. Dashes over a full bar are half a line,
	// which is the same problem in a smaller size.
	if got := r.Guide.Size().Width; got != 900 {
		t.Fatalf("the line should span the chart, got %g of 900", got)
	}
	if r.Guide.Size().Height < 1 {
		t.Fatalf("the line needs a thickness to read, got %g", r.Guide.Size().Height)
	}
}

// statusUI is a Status tab holding one day of each org's work.
func statusUI(t *testing.T) *shell.UI {
	t.Helper()
	w := test.NewApp().NewWindow("t")
	// The test theme defines no bold monospace face, and the day panel asks for
	// one; laying it out under that theme panics inside the painter.
	test.ApplyTheme(t, theme.DefaultTheme())
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w,
		CalMonth: "2026-08", RepMonth: "2026-08"}
	ui.Cfg = backend.Config{Repos: []string{"exampleorg", "Example-Support"}}
	w.SetContent(status.BuildStatusTab(ui))

	ui.EnsureProjectCache()
	key := shell.StatusCacheKey("2026-08")
	ui.ProjItems[key] = []backend.WorklogItem{
		{Date: "2026-08-11", Minutes: 120, Title: "platform work",
			URL: "https://github.com/exampleorg/sample-intranet/issues/1"},
		{Date: "2026-08-12", Minutes: 60, Title: "support work",
			URL: "https://github.com/Example-Support/sample-operations/issues/2"},
	}
	ui.ProjLoaded[key] = true
	status.DrawCalendar(ui)
	return ui
}

// Clicking an org in the key takes it out of the month, leaving the other one
// to be read on its own. Two employers' work in one calendar can only ever be
// read on top of each other otherwise.
func TestTappingTheKeyTakesAnOrgOutOfTheCalendar(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()

	if got := ui.CalSummary.Text; !strings.HasPrefix(got, "180 min") {
		t.Fatalf("the month starts with both orgs in it, got %q", got)
	}

	tapKey(t, ui.CalLegend, "exampleorg")
	if got := ui.CalSummary.Text; !strings.HasPrefix(got, "60 min") {
		t.Fatalf("hiding exampleorg should leave only the support hours, got %q", got)
	}

	// The org that was switched off still holds its place in the key, at what it
	// is worth — it is the only way back, and a key that only shows what is
	// already on screen tells you nothing you cannot see.
	if !contains(labels(ui.CalLegend), "exampleorg (2h)") {
		t.Fatalf("the hidden org must stay in the key: %v", labels(ui.CalLegend))
	}

	// The day panel is that cell spelled out, so it hides the same work.
	ui.SelDay = "2026-08-11"
	status.DrawDayPanel(ui)
	if contains(labels(ui.DaySwap), "platform work") {
		t.Fatalf("a hidden org's work should not open from the calendar: %v", labels(ui.DaySwap))
	}

	// Tapping again puts it back: this is a filter, not a delete.
	tapKey(t, ui.CalLegend, "exampleorg")
	if got := ui.CalSummary.Text; !strings.HasPrefix(got, "180 min") {
		t.Fatalf("tapping again should restore the month, got %q", got)
	}
}

// The pending list turns over constantly — one org today, two tomorrow — so
// its key is drawn even for a single org. A key that comes and goes is a key
// you cannot look for, and the colour on the tiles is then never named.
func TestThePendingKeyIsDrawnForASingleOrg(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()

	ui.PendingLoaded = true
	ui.Pending = backend.PendingResult{Count: 1, Range: []string{"2026-08-20", "2026-08-21"},
		Groups: []backend.Group{{Date: "2026-08-21", Issue: "exampleorg/sample-client-one#293",
			Commits: []backend.Commit{{Sha: "aaa1111", Repo: "exampleorg/sample-shared-utilities",
				Message: "gate the clone button", Full: "gate the clone button"}}}}}
	logwork.RenderPending(ui)

	if !contains(labels(ui.LogLegend), "exampleorg (1)") {
		t.Fatalf("the only org on the list still has to be named: %v", labels(ui.LogLegend))
	}

	// Switching that one org off empties the list, and the list has to say that
	// is why — "no unlogged commits" over a key counting one is a contradiction.
	tapKey(t, ui.LogLegend, "exampleorg")
	got := labels(ui.PendingBox)
	if !contains(got, "hidden by the filter") {
		t.Fatalf("an empty list should say the filter emptied it: %v", got)
	}
	if contains(got, "No unlogged commits") {
		t.Fatalf("hidden is not the same as none: %v", got)
	}
}

// The key governs the whole tab, not just the commits: the saved-but-unpushed
// list and the week strip under it are the same work at a later stage, and
// filtering the top of the tab while the bottom still shows the other employer
// is not a filter.
func TestTheKeyFiltersTheSavedListAndTheWeekToo(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()

	ui.Cfg = backend.Config{Repos: []string{"exampleorg", "Example-Support"}}
	if _, err := ui.Store.AppendRows([]backend.Row{
		{"date": shell.Today(), "minutes": "120", "type": "commit",
			"issue": "exampleorg/sample-intranet#42", "description": "platform work"},
		{"date": shell.Today(), "minutes": "60", "type": "commit",
			"issue": "Example-Support/sample-operations#51", "description": "support work"},
	}); err != nil {
		t.Fatalf("seed rows: %v", err)
	}
	logwork.DrawRecent(ui)

	if !contains(labels(ui.RecentBox), "2 waiting to push") {
		t.Fatalf("both entries start out waiting: %v", labels(ui.RecentBox))
	}
	// The key counts the saved entries as well, so it is there before a single
	// commit has been fetched.
	if !contains(labels(ui.LogLegend), "exampleorg (1)") {
		t.Fatalf("saved entries alone should raise the key: %v", labels(ui.LogLegend))
	}

	tapKey(t, ui.LogLegend, "exampleorg")
	if !contains(labels(ui.RecentBox), "1 waiting to push") {
		t.Fatalf("the hidden org's entry should be off the list: %v", labels(ui.RecentBox))
	}
	if contains(labels(ui.WeekBox), "platform work") {
		t.Fatalf("nor should it stand on the week strip: %v", labels(ui.WeekBox))
	}
	if !contains(labels(ui.LogLegend), "exampleorg (1)") {
		t.Fatalf("the hidden org keeps its count in the key: %v", labels(ui.LogLegend))
	}
}

// The calendar is the other way round: a month of one org is one colour, and a
// key naming the only colour on screen is furniture above the grid.
func TestNoKeyWhenThereIsOnlyOneOrg(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()

	key := shell.StatusCacheKey("2026-08")
	ui.ProjItems[key] = []backend.WorklogItem{{Date: "2026-08-11", Minutes: 120,
		URL: "https://github.com/exampleorg/sample-intranet/issues/1"}}
	status.DrawCalendar(ui)

	if len(ui.CalLegend.Objects) != 0 {
		t.Fatalf("a single org needs no key: %v", labels(ui.CalLegend))
	}
}

// The filter is one decision about what you are looking at, so it holds across
// the tabs rather than being made again on each. The report's own numbers move
// with it — the period really is worth less when one employer is left out.
func TestTheFilterCarriesToTheReport(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()

	ui.HasCfg = true
	ui.Win.SetContent(report.BuildReportTab(ui))
	key := shell.ReportCacheKey("2026-08")
	ui.ProjItems[key] = []backend.WorklogItem{
		{Date: "2026-08-11", Minutes: 120, Title: "platform work",
			URL: "https://github.com/exampleorg/sample-intranet/issues/1"},
		{Date: "2026-08-12", Minutes: 60, Title: "support work",
			URL: "https://github.com/Example-Support/sample-operations/issues/2"},
	}
	ui.ProjLoaded[key] = true
	report.DrawReport(ui)

	if !contains(labels(ui.RepBox), "3.0h") {
		t.Fatalf("the whole period is three hours: %v", labels(ui.RepBox))
	}

	ui.ToggleOrg("exampleorg")
	if !contains(labels(ui.RepBox), "1.0h") {
		t.Fatalf("with exampleorg hidden the period is one hour: %v", labels(ui.RepBox))
	}
	// The split is the report's key, so it keeps naming the hidden org — and at
	// its real share of the period, not recomputed to 100% of what is left.
	got := labels(ui.RepBox)
	if !contains(got, "exampleorg") {
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

	ui.Cfg = backend.Config{Repos: []string{"exampleorg", "Example-Support"}}
	ui.PendingLoaded = true
	ui.Pending = backend.PendingResult{Count: 2, Range: []string{"2026-08-10", "2026-08-12"},
		Groups: []backend.Group{
			{Date: "2026-08-11", Issue: "exampleorg/sample-intranet#42",
				Commits: []backend.Commit{{Sha: "aaa1111", Repo: "exampleorg/sample-intranet",
					Message: "cap the dropdown", Full: "cap the dropdown"}}},
			{Date: "2026-08-11", Issue: "Example-Support/sample-operations#51",
				Commits: []backend.Commit{{Sha: "bbb2222", Repo: "exampleorg/sample-shared-utilities",
					Message: "widen the picker", Full: "widen the picker"}}},
		}}
	logwork.RenderPending(ui)

	got := labels(ui.PendingBox)
	if !contains(got, "cap the dropdown") || !contains(got, "widen the picker") {
		t.Fatalf("both commits start out on the list: %v", got)
	}
	if !contains(labels(ui.LogLegend), "exampleorg (1)") {
		t.Fatalf("the key names both orgs: %v", labels(ui.LogLegend))
	}

	tapKey(t, ui.LogLegend, "exampleorg")
	got = labels(ui.PendingBox)
	if contains(got, "cap the dropdown") {
		t.Fatalf("the hidden org's commit should be off the list: %v", got)
	}
	if !contains(got, "widen the picker") {
		t.Fatalf("the other org's commit should still be there: %v", got)
	}
	// Still counted in the key at its full size, so it can be brought back.
	if !contains(labels(ui.LogLegend), "exampleorg (1)") {
		t.Fatalf("the hidden org keeps its count in the key: %v", labels(ui.LogLegend))
	}
}

// The day panel keeps one width whatever the day holds: a long parent title
// or org name used to push it wider, so the calendar shifted between days.
func TestStatusDayPanelKeepsItsWidth(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()
	empty := ui.DaySide.MinSize().Width

	key := shell.StatusCacheKey("2026-08")
	ui.ProjItems[key] = append(ui.ProjItems[key], backend.WorklogItem{
		Date: "2026-08-11", Minutes: 45,
		Title:       strings.Repeat("a very long worklog title ", 8),
		ParentTitle: strings.Repeat("an even longer parent issue title ", 8),
		ParentURL:   "https://github.com/exampleorg/sample-intranet/issues/9",
		URL:         "https://github.com/exampleorg/sample-intranet/issues/10",
	})
	ui.SelDay = "2026-08-11"
	status.DrawCalendar(ui)
	status.DrawDayPanel(ui)
	if got := ui.DaySide.MinSize().Width; got != empty {
		t.Fatalf("day panel width changed from %g to %g", empty, got)
	}
}
