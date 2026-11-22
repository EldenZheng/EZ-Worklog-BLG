package tests

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/logwork"
	"worklog/internal/menus/report"
	"worklog/internal/menus/status"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func countBesideRefresh(t *testing.T, root fyne.CanvasObject, pick *shell.GoldSelect) {
	t.Helper()
	var refresh *widget.Button
	walk(root, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Refresh from GitHub" {
			refresh = b
		}
	})
	if pick == nil || refresh == nil {
		t.Fatal("missing Worklog or Refresh from GitHub")
	}
	if !contains(labels(root), "Worklog") || contains(labels(root), "Count") {
		t.Fatal("the count control must be labelled Worklog")
	}
	d := fyne.CurrentApp().Driver()
	p, r := d.AbsolutePositionForObject(pick), d.AbsolutePositionForObject(refresh)
	if p.X+pick.Size().Width > r.X || p.Y != r.Y {
		t.Fatalf("Count must sit beside Refresh from GitHub: pick=%v refresh=%v", p, r)
	}
}

func chooseWorklogKeepingMenuOpen(t *testing.T, pick *shell.GoldSelect, index int, win fyne.Window) {
	t.Helper()
	if pick.Popup == nil {
		pick.Tapped(nil)
	}
	popup := pick.Popup
	row := pick.Rows.Objects[index]
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(row).Add(fyne.NewPos(20, row.Size().Height/2))
	test.TapCanvas(win.Canvas(), at)
	if pick.Popup != popup || !popup.Visible() {
		t.Fatal("Worklog selection closed its menu")
	}
}

func TestStatusAndReportCountSelectorsOwnTheirPageTotals(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()
	ui.HasCfg = true
	ui.CalMonth, ui.RepMonth = "2026-11", "2026-12"
	ui.Cfg.ProjectURL = "https://github.com/orgs/exampleorg/projects/9"
	statusKey, reportKey := shell.StatusCacheKey(ui.CalMonth), shell.ReportCacheKey(ui.RepMonth)
	rows := []backend.WorklogItem{
		{Date: "2026-11-23", Minutes: 480, ApprovedMinutes: 240, URL: "https://github.com/exampleorg/repo/issues/1"},
		{Date: "2026-11-24", Minutes: 120, URL: "https://github.com/exampleorg/repo/issues/2"},
	}
	ui.ProjItems[statusKey], ui.ProjItems[reportKey] = rows, rows
	ui.ProjLoaded[statusKey], ui.ProjLoaded[reportKey] = true, true
	ui.SelDay = "2026-11-23"
	ui.Win.Resize(fyne.NewSize(1600, 1000))
	status.DrawCalendar(ui)
	countBesideRefresh(t, ui.CalBody, ui.CalCount)
	if ui.CalCount.Selected != shell.MinutesReported || !strings.Contains(ui.CalSummary.Text, "600 min logged") {
		t.Fatal("Status must default to the reported total", ui.CalSummary.Text)
	}
	chooseWorklogKeepingMenuOpen(t, ui.CalCount, 1, ui.Win)
	if !strings.Contains(ui.CalSummary.Text, "240 min logged") || !contains(labels(ui.DaySwap), "240/480 min (50%)") {
		t.Fatal("Status count must update the month and selected day", ui.CalSummary.Text, labels(ui.DaySwap))
	}
	status.DrawCalendar(ui)
	if ui.CalCount.Selected != shell.MinutesApproved {
		t.Fatal("Status refresh reset its count choice")
	}

	ui.CalCount.CloseChoices()
	shell.FinishDropdownClosings()
	ui.Win.SetContent(report.BuildReportTab(ui))
	report.DrawReport(ui)
	countBesideRefresh(t, ui.RepHead, ui.RepCount)
	chartDay := func(date string) int {
		t.Helper()
		value := -1
		walk(ui.RepBox, func(o fyne.CanvasObject) {
			if chart, ok := o.(*report.DayChart); ok {
				for _, day := range chart.Days {
					if day.Date == date {
						value = day.Total
					}
				}
			}
		})
		return value
	}
	if ui.RepCount.Selected != shell.MinutesReported || chartDay("2026-11-23") != 480 || !contains(labels(ui.RepBox), "10.0h") {
		t.Fatal("Report must default to reported minutes independently of Status")
	}
	chooseWorklogKeepingMenuOpen(t, ui.RepCount, 1, ui.Win)
	if chartDay("2026-11-23") != 240 || chartDay("2026-11-24") != 0 || !contains(labels(ui.RepBox), "4.0h") {
		t.Fatal("Report chart and total must use approved minutes, with blank approvals as zero")
	}
	if rows[0].Minutes != 480 || ui.ProjItems[statusKey][0].Minutes != 480 {
		t.Fatal("count selection changed the shared reported cache")
	}
	chooseWorklogKeepingMenuOpen(t, ui.RepCount, 0, ui.Win)
	if chartDay("2026-11-23") != 480 || ui.CalCount.Selected != shell.MinutesApproved {
		t.Fatal("each page must keep its own count choice")
	}
	ui.RepCount.CloseChoices()
	shell.FinishDropdownClosings()
	ui.WeekStart = "2026-11-23"
	ui.Win.SetContent(logwork.BuildLogTab(ui))
	logwork.DrawWeekStrip(ui)
	walk(ui.WeekBox, func(o fyne.CanvasObject) {
		if pick, ok := o.(*shell.GoldSelect); ok && len(pick.Options) == 2 && pick.Options[0] == shell.MinutesReported {
			t.Fatal("Saved locally must not contain the Count selector")
		}
	})
	if !contains(labels(ui.WeekCols["2026-11-23"]), logwork.DayScoreLine(480)) {
		t.Fatal("Status count leaked into Saved locally")
	}
	picker := logwork.NewWorklogDatePicker(ui, "2026-11-23")
	if !contains(labels(picker.Grid), "480/480") {
		t.Fatal("Status count leaked into the Log Work date picker")
	}
	if got := shell.CountedWorklogMinutes(backend.WorklogItem{Date: "2026-11-20", Minutes: 90}, true); got != 90 {
		t.Fatal("legacy board-only minutes changed")
	}
}

func TestStatusAndReportCountLateWorklogsOnTheirLoggedForDay(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	backend.Gh = func(_ []string, _ ...string) (string, error) {
		return `{"data":{"search":{"issueCount":1,"nodes":[{"url":"https://github.com/exampleorg/repo/issues/1","createdAt":"2026-11-23T16:30:00Z","body":"Logged for 2026-11-21\n\nlate work","issueFieldValues":{"nodes":[{"value":120,"field":{"name":"Worklog Reported(mins)"}},{"value":60,"field":{"name":"Worklog Approved(mins)"}}]}}]}}}`, nil
	}
	items, err := backend.FetchProjectWorklogs(backend.Config{}, "owner", "2026-11-21", "2026-11-30")
	if err != nil || len(items) != 1 {
		t.Fatalf("could not read the late Worklog: %+v, %v", items, err)
	}
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()
	ui.HasCfg = true
	ui.CalMonth, ui.RepMonth = "2026-11", "2026-12"
	for _, key := range []string{shell.StatusCacheKey(ui.CalMonth), shell.ReportCacheKey(ui.RepMonth)} {
		ui.ProjItems[key], ui.ProjLoaded[key] = items, true
	}
	for _, tc := range []struct {
		approved bool
		minutes  int
		caption  string
	}{
		{false, 120, "120/480 min (25%)"},
		{true, 60, "60/480 min (12%)"},
	} {
		ui.StatusApproved, ui.ReportApproved = tc.approved, tc.approved
		ui.SelDay = "2026-11-21"
		status.DrawCalendar(ui)
		if !contains(labels(ui.DaySwap), tc.caption) {
			t.Fatalf("Status lost the late Worklog (approved=%v): %v", tc.approved, labels(ui.DaySwap))
		}
		ui.SelDay = "2026-11-24" // actual creation day in Malaysia
		status.DrawDayPanel(ui)
		if !contains(labels(ui.DaySwap), "0/480 min (0%)") {
			t.Fatalf("Status also counted the Worklog on its creation day: %v", labels(ui.DaySwap))
		}
		ui.Win.SetContent(report.BuildReportTab(ui))
		report.DrawReport(ui)
		found := false
		walk(ui.RepBox, func(o fyne.CanvasObject) {
			if chart, ok := o.(*report.DayChart); ok {
				for _, day := range chart.Days {
					if day.Date == "2026-11-21" {
						found = true
						if day.Total != tc.minutes {
							t.Errorf("Report has %d minutes on the logged-for day; want %d", day.Total, tc.minutes)
						}
					}
					if day.Date == "2026-11-24" && day.Total != 0 {
						t.Error("Report also counted the Worklog on its creation day")
					}
				}
			}
		})
		if !found {
			t.Fatal("Report's chart did not include the logged-for day")
		}
	}
}

func TestFifteenHoursStayLoggedWithoutIncreasingDailyPay(t *testing.T) {
	// Several Worklogs on the same day share one pay cap. The original
	// reported/approved minutes must survive the report calculation intact.
	items := []backend.WorklogItem{
		{Date: "2026-11-23", Minutes: 600, ApprovedMinutes: 540},
		{Date: "2026-11-23", Minutes: 300, ApprovedMinutes: 180},
	}
	for _, tc := range []struct {
		approved bool
		logged   int
	}{
		{false, 900}, // 15 hours reported
		{true, 720},  // 12 hours approved
	} {
		counted := shell.CountedWorklogItems(items, tc.approved)
		totals := backend.TotalsFromItems(counted, "")
		rep := backend.ReportFromTotals(backend.Config{BaseSalary: 2100}, totals)
		if rep.TotalMin != tc.logged || totals["2026-11-23"] != tc.logged {
			t.Fatalf("logged time was clipped (approved=%v): %+v", tc.approved, rep)
		}
		if rep.PayableDays != 1 || rep.Receivable != 100 || rep.DaysComplete != 1 {
			t.Fatalf("more than eight hours increased daily pay (approved=%v): %+v", tc.approved, rep)
		}
	}
	if items[0].Minutes != 600 || items[1].Minutes != 300 {
		t.Fatal("pay calculation changed the source worklogs")
	}
}

func TestMarkedLateWorklogsBackfillBeforeTheDailyPayCap(t *testing.T) {
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	backend.Gh = func(_ []string, _ ...string) (string, error) {
		// Malaysia 24 Nov has 900 reported minutes created on it. Of those,
		// 180 explicitly belong to 23 Nov, which already has 300 minutes.
		return `{"data":{"search":{"issueCount":4,"nodes":[
{"url":"https://github.com/exampleorg/repo/issues/1","createdAt":"2026-11-23T03:00:00Z","body":"previous day's work","issueFieldValues":{"nodes":[{"value":300,"field":{"name":"Worklog Reported(mins)"}},{"value":240,"field":{"name":"Worklog Approved(mins)"}}]}},
{"url":"https://github.com/exampleorg/repo/issues/2","createdAt":"2026-11-23T16:30:00Z","body":"today's work","issueFieldValues":{"nodes":[{"value":480,"field":{"name":"Worklog Reported(mins)"}},{"value":480,"field":{"name":"Worklog Approved(mins)"}}]}},
{"url":"https://github.com/exampleorg/repo/issues/3","createdAt":"2026-11-24T03:00:00Z","body":"Logged for 2026-11-23\n\nafter-midnight work","issueFieldValues":{"nodes":[{"value":180,"field":{"name":"Worklog Reported(mins)"}},{"value":120,"field":{"name":"Worklog Approved(mins)"}}]}},
{"url":"https://github.com/exampleorg/repo/issues/4","createdAt":"2026-11-24T04:00:00Z","body":"extra work without a logged-for date","issueFieldValues":{"nodes":[{"value":240,"field":{"name":"Worklog Reported(mins)"}},{"value":180,"field":{"name":"Worklog Approved(mins)"}}]}}
]}}}`, nil
	}
	items, err := backend.FetchProjectWorklogs(backend.Config{}, "owner", "2026-11-21", "2026-11-30")
	if err != nil || len(items) != 4 {
		t.Fatalf("could not read the mixed-day Worklogs: %+v, %v", items, err)
	}
	for _, tc := range []struct {
		approved                  bool
		previous, current, logged int
		payableDays, receivable   float64
	}{
		{false, 480, 720, 1200, 2, 200},
		// Approval leaves the earlier day 120 minutes short. The 180
		// unmarked excess minutes on the current day must stay there.
		{true, 360, 660, 1020, 1.75, 175},
	} {
		totals := backend.TotalsFromItems(shell.CountedWorklogItems(items, tc.approved), "")
		if totals["2026-11-23"] != tc.previous || totals["2026-11-24"] != tc.current {
			t.Fatalf("backfill must follow each log's date (approved=%v): %v", tc.approved, totals)
		}
		rep := backend.ReportFromTotals(backend.Config{BaseSalary: 2100}, totals)
		if rep.PayableDays != tc.payableDays || rep.Receivable != tc.receivable || rep.TotalMin != tc.logged {
			t.Fatalf("backfill was capped too soon, duplicated, or excess received extra pay (approved=%v): %+v", tc.approved, rep)
		}
	}
	if items[1].Date != "2026-11-23" || items[1].Minutes != 180 || items[1].ApprovedMinutes != 120 {
		t.Fatal("counting changed the marked log's date or source minutes", items)
	}
}

func TestStatusAndReportCountChangesKeepArrivalAnimations(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()
	ui.HasCfg = true
	ui.CalMonth, ui.RepMonth = "2026-11", "2026-12"
	items := []backend.WorklogItem{{Date: "2026-11-23", Minutes: 480, ApprovedMinutes: 240}}
	for _, key := range []string{shell.StatusCacheKey(ui.CalMonth), shell.ReportCacheKey(ui.RepMonth)} {
		ui.ProjItems[key], ui.ProjLoaded[key] = items, true
	}
	status.DrawCalendar(ui)
	driver := collectMotion(t)
	ui.CalCount.Choose(1)
	if len(driver.anims) == 0 {
		t.Fatal("Status count change skipped its calendar animation")
	}
	ui.Win.SetContent(report.BuildReportTab(ui))
	report.DrawReport(ui)
	before := len(driver.anims)
	ui.RepCount.Choose(1)
	if len(driver.anims) <= before {
		t.Fatal("Report count change skipped its chart and figures' animations")
	}
}
