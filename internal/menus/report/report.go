package report

import (
	_ "embed"
	"encoding/csv"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// ============================ Report ============================

func BuildReportTab(ui *shell.UI) fyne.CanvasObject {
	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		ui.RepMonth = shell.ShiftMonth(ui.RepMonth, -1)
		DrawReport(ui)
		ui.LoadReport(false)
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		ui.RepMonth = shell.ShiftMonth(ui.RepMonth, 1)
		DrawReport(ui)
		ui.LoadReport(false)
	})
	ui.RepTitle = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	exportBtn := widget.NewButton("Export CSV", func() { exportCSV(ui) })
	refresh := widget.NewButtonWithIcon("Refresh from GitHub", theme.ViewRefreshIcon(), func() {
		ui.EnsureProjectCache()
		delete(ui.ProjLoaded, shell.ReportCacheKey(ui.RepMonth))
		ui.RefreshRate(true) // an explicit refresh re-reads the rate too
		DrawReport(ui)
		ui.LoadReport(true)
	})
	navigation := container.NewHBox(prev, ui.RepTitle, next)
	actions := container.NewHBox(layout.NewSpacer(), refresh, exportBtn)
	head := container.New(widgets.NewFlowGrid(360, 0, 40), navigation, actions)

	ui.RepBox = container.NewVBox()
	ui.ReportProgress = shell.NewLoadingIndicator()
	ui.RepBody = container.NewVBox(head, ui.ReportProgress.View, ui.RepBox)
	// Two-way scroll: content that will not shrink further (a metric card at
	// its minimum, a table's date column) scrolls sideways rather than being
	// clipped by the window's right edge. Vertical scroll stays as before.
	return container.NewScroll(widget.NewCard("", "", ui.RepBody))
}

func DrawReport(ui *shell.UI) {
	defer ui.SyncLoading()
	if ui.RepBox == nil {
		return
	}
	ui.RepTitle.SetText(shell.ReportPeriodName(ui.RepMonth))
	if !ui.HasCfg {
		ui.RepBox.Objects = []fyne.CanvasObject{widget.NewLabel("Open Settings first.")}
		ui.RepBox.Refresh()
		shell.Relayout(ui.RepBody)
		return
	}
	// Report reflects the GitHub project, filtered to the worklog owner.
	key := shell.ReportCacheKey(ui.RepMonth)
	fetched, cached := ui.ItemsFor(key)
	// The fetch reaches months before the period so the weekend support carry
	// can be seen; everything the report reports on is the period alone.
	fromDate, toDate, _ := backend.ReportBounds(ui.RepMonth)
	var all, runUp []backend.WorklogItem
	for _, it := range fetched {
		switch {
		case it.Date < fromDate:
			runUp = append(runUp, it)
		case it.Date <= toDate:
			all = append(all, it)
		}
	}
	// Everything below the key reads the filtered set — the money included. The
	// period is worth a different amount depending on whose work is in it, and
	// showing one employer's chart over both employers' pay would be a lie in
	// the only place on the tab that has to be right.
	items := ui.ShownItems(all)
	if !cached {
		ui.EnsureProjectCache()
		if ui.ProjErr[key] != nil && !ui.ProjLoading[key] {
			ui.RepBox.Objects = []fyne.CanvasObject{shell.ColorLabel(ui.ProjErr[key].Error(), theme.ColorNameError)}
			ui.RepBox.Refresh()
			shell.Relayout(ui.RepBody)
			return
		}
		ui.RepBox.Objects = []fyne.CanvasObject{widget.NewLabel("Loading worklogs from GitHub…")}
		ui.RepBox.Refresh()
		shell.Relayout(ui.RepBody)
		return
	}
	totals := backend.TotalsFromItems(items, "")
	rep := backend.ReportFromTotals(ui.Cfg, totals)
	// Drafts are shown, never counted in the settled figures. Every figure
	// below this line — payable days, receivable, complete/logged — is what
	// the board holds, because that is what will actually be paid. Work still
	// sitting on this machine gets a projection of its own further down.
	draftByDay := ui.DraftMinutesByDay(fromDate, toDate)
	draftTotal := 0
	for _, m := range draftByDay {
		draftTotal += m
	}
	// What the run-up could not make a set out of comes forward. A remainder,
	// so a period never gets paid for work an earlier report already paid for.
	carried := backend.WeekendSupportIssues(ui.ShownItems(runUp)) % backend.SupportSetSize
	supportIssues := backend.WeekendSupportIssues(items)
	supportSets, supportBonus := backend.WeekendSupportBonus(carried, supportIssues, ui.Cfg.SupportSetPay)
	totalReceivable := rep.Receivable + supportBonus

	// Projected receivable is what the period would pay once every draft on
	// this machine is pushed. The pay math is the same, run on totals merged
	// with the drafts by day so a draft that finishes an under-target day
	// lifts payable days accordingly. Weekend support is kept as-is — a
	// draft's contribution to a support set is only known once it is pushed
	// with its issue attached, and inflating the projection would read as a
	// promise this figure cannot keep.
	projectedTotals := map[string]int{}
	for d, m := range totals {
		projectedTotals[d] = m
	}
	for d, m := range draftByDay {
		projectedTotals[d] += m
	}
	projected := backend.ReportFromTotals(ui.Cfg, projectedTotals)
	projectedTotalReceivable := projected.Receivable + supportBonus

	money := func(v float64) string { return rep.Currency + " " + shell.CommaAmount(v) }
	converted := func(v float64) string {
		if rep.FxRate == 0 || rep.DisplayCurrency == "" {
			return "Not configured"
		}
		return rep.DisplayCurrency + " " + shell.CommaAmount(v*rep.FxRate)
	}
	conv := converted(totalReceivable)
	projectedConv := converted(projectedTotalReceivable)
	supportConv := converted(supportBonus)

	// Working days are read off the calendar, so the tile is right even for a
	// period nobody has logged a minute into yet.
	wdGone, wdTotal := backend.WorkingDaysProgress(fromDate, toDate, shell.Today())
	daysGone, daysTotal := backend.CalendarDaysProgress(fromDate, toDate, shell.Today())
	wdLeft := wdTotal - wdGone
	wdNote := fmt.Sprintf("Working days gone — %d left", wdLeft)
	if wdTotal != backend.Divisor {
		// Pay still divides by 21 whatever the calendar says; say so rather
		// than let a 22-day period read like a miscount.
		wdNote += fmt.Sprintf(" (pay divides by %d)", backend.Divisor)
	}

	supportDetail := fmt.Sprintf("%d issues, %d set(s)", supportIssues, supportSets)
	if carried > 0 {
		supportDetail = fmt.Sprintf("%d carried in + %s", carried, supportDetail)
	}
	if left := (carried + supportIssues) % backend.SupportSetSize; left > 0 {
		supportDetail += fmt.Sprintf(", %d carry to next", left)
	}
	supportNote := "Weekend support bonus (" + supportDetail + ")"
	if ui.Cfg.SupportSetPay == 0 {
		supportNote += " — set the support payment in Settings"
	}

	// The yellow "projected" tile sits beside the settled one so the two
	// figures read as a pair — this-is-yours-now next to this-is-yours-once-
	// pushed. Its note carries the delta so a glance says how much depends on
	// the drafts.
	draftSurface, draftHeader, draftBorder := draftPanelTones()
	payableNote := fmt.Sprintf("%.2f days × %s", rep.PayableDays, money(rep.DailyRate))
	projectedNote := fmt.Sprintf("%.2f days × %s", projected.PayableDays, money(projected.DailyRate))
	if projected.Receivable > rep.Receivable {
		projectedNote = fmt.Sprintf("+%s once %s of drafts pushes · %s",
			money(projected.Receivable-rep.Receivable), backend.HoursMins(draftTotal), projectedNote)
	}
	payableBeforeSupport := "(" + money(rep.Receivable) + " before support)"
	projectedBeforeSupport := "(" + money(projected.Receivable) + " before support)"
	stats := container.New(widgets.NewFlowGrid(220, 0, 126),
		reportMetricWithAside(money(totalReceivable), "Payable", payableNote, payableBeforeSupport, "Converted: "+conv),
		reportMetricTintedWithAside(money(projectedTotalReceivable), "Payable after drafts", projectedNote, projectedBeforeSupport, "Converted: "+projectedConv, draftSurface, draftHeader, draftBorder),
		reportMetricDarkBlue(money(supportBonus), "Support bonus", supportDetail, "Converted: "+supportConv),
	)
	timeStats := container.New(widgets.NewFlowGrid(220, 0, 126),
		reportMetric(fmt.Sprintf("%.1fh", float64(rep.TotalMin)/60), "Time logged",
			fmt.Sprintf("÷ 8h = %.2f days · %d complete / %d logged days",
				float64(rep.TotalMin)/backend.Target, rep.DaysComplete, rep.DaysLogged)),
		reportMetric(backend.HoursMins(draftTotal), "Saved here, not pushed",
			fmt.Sprintf("÷ 8h = %.2f days · Not included in settled figures", float64(draftTotal)/backend.Target)),
	)
	payment := reportSection("Payment breakdown",
		reportFact("Worklog receivable", money(rep.Receivable)),
		reportFact("Weekend support bonus", money(supportBonus)),
		reportText(supportNote),
		widget.NewSeparator(),
		reportFact("Payable days × "+money(rep.DailyRate), fmt.Sprintf("%.2f", rep.PayableDays)),
		reportFactColored("Converted receivable", conv, widgets.LogoGold))
	activity := reportSection("Period at a glance",
		reportFact("Calendar days gone", fmt.Sprintf("%d/%d", daysGone, daysTotal)),
		reportText(fmt.Sprintf("%d calendar days left · includes weekends; today is remaining", daysTotal-daysGone)),
		widget.NewSeparator(),
		reportFact("Complete / logged", fmt.Sprintf("%d/%d", rep.DaysComplete, rep.DaysLogged)),
		reportFact("Working days gone", fmt.Sprintf("%d/%d", wdGone, wdTotal)),
		reportText(wdNote),
		widget.NewSeparator(),
		reportFact("Worklog items", strconv.Itoa(len(items))),
		reportText("Worklog owner: "+backend.OrDefault(ui.Cfg.WorklogOwner, "all")))

	// The chart is per day, so it needs the split per day — the period totals
	// only say who the month belonged to, not which day was whose. Every day in
	// the period gets a slot, logged or not, so the axis is a real calendar.
	byDayOrg := map[string]map[string]int{}
	for _, it := range items {
		if byDayOrg[it.Date] == nil {
			byDayOrg[it.Date] = map[string]int{}
		}
		byDayOrg[it.Date][shell.ItemOrg(it)] += it.Minutes
	}
	var days []ChartDay
	if from, err := time.Parse("2006-01-02", fromDate); err == nil {
		to, _ := time.Parse("2006-01-02", toDate)
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			ds := d.Format("2006-01-02")
			day := ChartDay{Date: ds, ByOrg: byDayOrg[ds], Draft: draftByDay[ds]}
			for _, m := range day.ByOrg {
				day.Total += m
			}
			days = append(days, day)
		}
	}
	chart := ChartWithReadout(ui, days, fromDate, toDate)

	// Where the period's hours actually went. The bar is each org's share of
	// the total, in that org's colour — the same key the calendar and the
	// pending tiles use, so one colour means one employer everywhere.
	//
	// Counted from the unfiltered period, and tapping a row filters the tab.
	// This is the report's colour key, so it has to keep showing the org that
	// was switched off, at the size it really is: shares recomputed against
	// whatever is left would read 100% for one org and call it the whole month.
	byOrg := shell.MinutesByOrg(all)
	allMin := 0
	for _, m := range byOrg {
		allMin += m
	}
	split := []fyne.CanvasObject{shell.Bold("By organisation — tap one to show only the others")}
	for _, org := range shell.OrgsByShare(ui.Cfg, byOrg) {
		org, mins := org, byOrg[org]
		pct := 0
		if allMin > 0 {
			pct = int(float64(mins) / float64(allMin) * 100)
		}
		// A switched-off row keeps its size and its place and goes grey, dot and
		// bar together — the same washed-out colour the key uses, so one look
		// says which employer is currently out of every figure above.
		var fill color.Color = shell.OrgColor(ui.Cfg, org)
		nameColor := theme.ColorNameForeground
		amount := fmt.Sprintf("%s · %d%%", backend.HoursMins(mins), pct)
		if !ui.OrgShown(org) {
			fill = shell.Dimmed(fill)
			nameColor = theme.ColorNamePlaceHolder
			amount = "hidden"
		}
		row := container.New(widgets.NewRatioRow(0.04, 0.22, 0.54, 0.20),
			shell.OrgDotIn(fill),
			container.NewCenter(shell.ColorLabel(backend.OrDefault(org, "unknown org"), nameColor)),
			shell.ShareBar(fill, mins, allMin, 12),
			widget.NewLabelWithStyle(amount, fyne.TextAlignTrailing,
				fyne.TextStyle{Monospace: true}))
		// Same hover and press animation as the legend chips elsewhere.
		split = append(split, shell.NewCardTappable(row, func() { ui.ToggleOrg(org) }))
	}
	if len(byOrg) == 0 {
		split = append(split, widget.NewLabel("Nothing logged in this period."))
	}

	var dayDetails []fyne.CanvasObject
	if len(rep.Incomplete) > 0 {
		rows := []fyne.CanvasObject{container.NewGridWithColumns(3, shell.Bold("Date"), shell.Bold("Logged (min)"), shell.Bold("Missing (min)"))}
		for _, x := range rep.Incomplete {
			rows = append(rows, container.NewGridWithColumns(3,
				widget.NewLabel(x.Date), widget.NewLabel(strconv.Itoa(x.Minutes)),
				widget.NewLabel(strconv.Itoa(backend.Target-x.Minutes))))
		}
		dayDetails = append(dayDetails, reportSection("Days under 480", rows...))
	}
	// Days over 480 are intentionally not listed. Their full time still counts
	// in Time logged while payable time remains capped by the existing pay math.
	// With no salary set every figure on the tab is a real zero, which reads as
	// a month that earned nothing rather than as a setting nobody has filled in.
	summary := "Set your base salary in Settings to see what this period is worth."
	if rep.BaseSalary != 0 {
		summary = fmt.Sprintf("%s over 21 working days, %s per full day.",
			money(rep.BaseSalary), money(rep.DailyRate))
	}
	if supportBonus > 0 {
		summary += fmt.Sprintf(" Worklog receivable %s + weekend support %s = %s.",
			money(rep.Receivable), money(supportBonus), money(totalReceivable))
	}
	if rep.FxRate != 0 {
		summary += fmt.Sprintf(" Rate 1 %s = %s %s, updated %s.",
			rep.Currency, shell.CommaAmount(rep.FxRate), rep.DisplayCurrency, rep.FxUpdated)
	}
	notes := []fyne.CanvasObject{reportText(summary)}
	if draftTotal > 0 {
		notes = append(notes, reportWarning(fmt.Sprintf(
			"%s in this period is saved on this machine and not pushed — drawn yellow on the "+
				"chart and left out of every figure above. Push it from Log work to have it count.",
			backend.HoursMins(draftTotal))))
	}

	ui.RepBox.Objects = []fyne.CanvasObject{
		stats,
		container.NewVBox(chart),
		container.NewVBox(split...),
		timeStats,
		container.New(widgets.NewFlowGrid(300, 0, 0), payment, activity),
	}
	if len(dayDetails) > 0 {
		ui.RepBox.Objects = append(ui.RepBox.Objects, container.New(widgets.NewFlowGrid(300, 0, 0), dayDetails...))
	}
	ui.RepBox.Objects = append(ui.RepBox.Objects, reportSection("How this is calculated", notes...))
	ui.RepBox.Refresh()
	shell.Relayout(ui.RepBody)
}

func exportCSV(ui *shell.UI) {
	// Export the project worklog items shown in this payroll period. The cache
	// behind it reaches months further back — that run-up is only there for the
	// weekend support carry, and exporting it would put three extra months into
	// a file named after one period.
	fromDate, toDate, _ := backend.ReportBounds(ui.RepMonth)
	fetched, _ := ui.ItemsFor(shell.ReportCacheKey(ui.RepMonth))
	var items []backend.WorklogItem
	for _, it := range fetched {
		if it.Date >= fromDate && it.Date <= toDate {
			items = append(items, it)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Date < items[j].Date })
	out := filepath.Join(shell.ExportsDir(), fmt.Sprintf("worklog-%s-to-%s.csv", fromDate, toDate))
	f, err := os.Create(out)
	if err != nil {
		ui.Errf(err)
		return
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"date", "minutes", "owner", "title", "remarks", "url"})
	for _, it := range items {
		_ = w.Write([]string{it.Date, strconv.Itoa(it.Minutes), it.Owner, it.Title, it.Remarks, it.URL})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		ui.Errf(err)
		return
	}
	dialog.ShowInformation("Exported", fmt.Sprintf("Saved %d items to:\n%s", len(items), out), ui.Win)
}
