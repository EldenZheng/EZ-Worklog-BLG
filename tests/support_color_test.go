package tests

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"worklog/internal/menus/logwork"
	"worklog/internal/menus/report"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestSupportHasItsOwnMeterPartAndTurquoiseCheckbox(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	parts := shell.PlannedBarParts(backend.Config{}, map[string]int{"org": 120}, map[string]int{"org": 60, shell.SupportPendingKey: 180}, 120)
	var draft, support, plan bool
	for _, part := range parts {
		switch part.Key {
		case "@draft":
			draft = part.Weight == float32(60)/480 && sameColor(part.Fill, shell.DraftWash())
		case shell.SupportPendingKey:
			support = part.Weight == float32(180)/480 && sameColor(part.Fill, shell.SupportFill(shell.DraftSolidTint))
		case "@plan":
			plan = part.Weight == float32(120)/480
		}
	}
	if !draft || !support || !plan {
		t.Fatal("draft, weekday support, and planned minutes must remain separate")
	}
	// A day with support alone must still have a visible vertical meter.
	fill := shell.VMeterFillPlanned(backend.Config{}, nil, 0, 0, 480, 8, 180)
	found := false
	walk(fill, func(o fyne.CanvasObject) {
		if r, ok := o.(*canvas.Rectangle); ok && sameColor(r.FillColor, shell.SupportFill(shell.MeterTint)) {
			found = true
		}
	})
	if !found {
		t.Fatal("support alone must not disappear from calendar fills")
	}
}

func TestReportAndDatePickerKeepSupportSeparateFromDrafts(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())
	w := a.NewWindow("test")
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w, HasCfg: true, RepMonth: "2026-08", Cfg: backend.Config{WorklogOwner: "alex", Repos: []string{"org"}}}
	ui.EnsureProjectCache()
	items := []backend.WorklogItem{{Date: "2026-08-10", Minutes: 240, URL: "https://github.com/org/repo/issues/1"}}
	for _, key := range []string{shell.ReportCacheKey("2026-08"), shell.StatusCacheKey("2026-08")} {
		ui.ProjItems[key] = items
		ui.ProjLoaded[key] = true
	}
	if _, err := ui.Store.AppendRows([]backend.Row{{"date": "2026-08-10", "minutes": "120", "type": "commit", "issue": "org/repo#2"}}); err != nil {
		t.Fatal(err)
	}
	oldSupport, oldPlan := shell.Hooks.SupportOn, shell.Hooks.PlannedMinutes
	shell.Hooks.SupportOn = func(_ *shell.UI, date string) bool { return date >= "2026-08-10" && date <= "2026-08-16" }
	shell.Hooks.PlannedMinutes = nil
	defer func() { shell.Hooks.SupportOn = oldSupport; shell.Hooks.PlannedMinutes = oldPlan }()
	w.SetContent(report.BuildReportTab(ui))
	w.Resize(fyne.NewSize(1400, 1000))
	report.DrawReport(ui)
	var chart *report.DayChart
	walk(ui.RepBox, func(o fyne.CanvasObject) {
		if c, ok := o.(*report.DayChart); ok {
			chart = c
		}
	})
	if chart == nil {
		t.Fatal("report chart missing")
	}
	matched := false
	for _, day := range chart.Days {
		if day.Date == "2026-08-10" {
			matched = day.Total == 240 && day.Draft == 120 && day.Support == 180
		}
		if day.Date == "2026-08-15" && day.Support != 0 {
			t.Fatal("weekday support must not spill onto weekends")
		}
	}
	if !matched || chart.MaxValue() < 540 {
		t.Fatal("the +180m checkbox must reach the report chart without becoming a draft")
	}
	chart.OnHover("2026-08-10")
	if !contains(labels(ui.RepBox), "+180 min weekday support (display only)") {
		t.Fatal("report hover must explain the checkbox contribution")
	}
	r := test.WidgetRenderer(chart).(*report.DayChartRenderer)
	turquoise := false
	for _, object := range r.Objects() {
		if rect, ok := object.(*canvas.Rectangle); ok && sameColor(rect.FillColor, shell.SupportFill(shell.DraftSolidTint)) {
			turquoise = true
		}
	}
	if !turquoise {
		t.Fatal("support chart segments must be turquoise")
	}
	picker := logwork.NewWorklogDatePicker(ui, "2026-08-10")
	picker.Refresh()
	if ink := inkOf(t, picker.Grid, "+180m support"); !sameColor(ink, shell.WeekdaySupportColor()) {
		t.Fatal("the datepicker must name support in turquoise")
	}
}
