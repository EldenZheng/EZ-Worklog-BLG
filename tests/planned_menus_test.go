package tests

import (
	"image/color"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/menus/loglist"
	"worklog/internal/menus/report"
	"worklog/internal/menus/status"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func stubPlanned(t *testing.T, byDay map[string]int) {
	t.Helper()
	old := shell.Hooks.PlannedMinutes
	shell.Hooks.PlannedMinutes = func(_ *shell.UI, date string, _ []backend.WorklogItem, _ []backend.Row) int {
		return byDay[date]
	}
	t.Cleanup(func() { shell.Hooks.PlannedMinutes = old })
}

func inkOf(t *testing.T, root fyne.CanvasObject, needle string) color.Color {
	t.Helper()
	var ink color.Color
	walk(root, func(o fyne.CanvasObject) {
		if text, ok := o.(*canvas.Text); ok && strings.Contains(text.Text, needle) {
			ink = text.Color
		}
	})
	if ink == nil {
		t.Fatalf("no text says %q: %v", needle, labels(root))
	}
	return ink
}

func TestWeekSummaryGivesDraftsAndPlansTheirOwnColouredSections(t *testing.T) {
	ui := logListUI(t)
	defer fyne.CurrentApp().Quit()
	stubPlanned(t, map[string]int{"2026-08-26": 120})
	key := shell.StatusCacheKey("2026-08")
	ui.ProjItems[key] = []backend.WorklogItem{{Date: "2026-08-25", Minutes: 720, URL: "https://github.com/exampleorg/sample-intranet/issues/1"}}
	ui.ProjLoaded[key] = true
	if _, err := ui.Store.AppendRows([]backend.Row{{"date": "2026-08-27", "minutes": "300", "type": backend.KindCommit, "issue": "exampleorg/repo#7"}}); err != nil {
		t.Fatal(err)
	}
	ui.PendingLoaded = true
	loglist.DrawLogList(ui)

	yellow := theme.Color(theme.ColorNameWarning)
	for needle, want := range map[string]color.Color{
		"+300 min waiting · 1020/3360 min (2340m left) 30%": yellow,
		"+120 min planned · 1140/3360 min (2220m left) 33%": shell.PlannerPlanColor,
		"+300 min waiting · 1020/2400 min (1380m left) 42%": yellow,
		"+120 min planned · 1140/2400 min (1260m left) 47%": shell.PlannerPlanColor,
	} {
		if got := inkOf(t, ui.ListBox, needle); !sameColor(got, want) {
			t.Fatalf("%q is drawn in %v, want %v", needle, got, want)
		}
	}
}

func TestStatusDayCarriesItsPlanInPurple(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()
	stubPlanned(t, map[string]int{"2026-08-13": 240})
	status.DrawCalendar(ui)

	if got := inkOf(t, ui.CalBox, "+240m · 50%"); !sameColor(got, shell.PlannerPlanColor) {
		t.Fatalf("the plan's figure is drawn in %v", got)
	}
	if !strings.Contains(ui.CalSummary.Text, "240 min planned") {
		t.Fatalf("the month should say what is planned: %q", ui.CalSummary.Text)
	}
}

func TestChartStacksThePlanOnTopOfTheDraft(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	days := []report.ChartDay{{Date: "2026-08-03", ByOrg: map[string]int{"exampleorg": 240}, Total: 240, Draft: 60, Planned: 300}}
	c := report.NewDayChart(backend.Config{Repos: []string{"exampleorg"}}, days, backend.Target)
	if got := c.MaxValue(); got != 600 {
		t.Fatalf("the scale has to hold the whole stack, got %d", got)
	}
	r := c.CreateRenderer().(*report.DayChartRenderer)
	c.Resize(fyne.NewSize(600, 260))
	r.Layout(fyne.NewSize(600, 260))
	slot := r.Slots[0]
	if slot.Plan == nil || !sameColor(slot.Plan.FillColor, shell.PlannerPlanColor) {
		t.Fatalf("no purple bar: %+v", slot.Plan)
	}
	if slot.Plan.Position().Y >= slot.Draft.Position().Y {
		t.Fatalf("the plan belongs above the draft: plan %v draft %v", slot.Plan.Position(), slot.Draft.Position())
	}
}

func TestPayableAfterPendingsCountsPlans(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())
	w := a.NewWindow("t")
	stubPlanned(t, map[string]int{"2026-08-11": 240})
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w, HasCfg: true, RepMonth: "2026-08",
		Cfg: backend.Config{Repos: []string{"exampleorg"}, WorklogOwner: "sample-owner", BaseSalary: 2793, Currency: "RM"}}
	w.SetContent(report.BuildReportTab(ui))
	key := shell.ReportCacheKey("2026-08")
	ui.EnsureProjectCache()
	ui.ProjItems[key] = []backend.WorklogItem{{Date: "2026-08-11", Minutes: 240, Owner: "sample-owner", URL: "https://github.com/exampleorg/work/issues/1"}}
	ui.ProjLoaded[key] = true
	w.Resize(fyne.NewSize(1500, 1000))
	report.DrawReport(ui)

	got := labels(ui.RepBox)
	if !contains(got, "Payable after pendings") || !contains(got, "+RM 66.50 once") || !contains(got, "of pending work lands") {
		t.Fatalf("half a day planned should lift the figure by half a day's pay: %v", got)
	}
}
