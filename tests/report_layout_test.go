package tests

import (
	"fmt"
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/menus/report"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestReportPutsMoneyAboveTheChartAndTimeBelowIt(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())
	w := a.NewWindow("t")
	store := backend.NewStore(t.TempDir())
	if _, err := store.AppendRows([]backend.Row{{
		"date": "2026-08-12", "minutes": "240", "owner": "sample-owner",
		"issue_url": "https://github.com/exampleorg/work/issues/2",
	}}); err != nil {
		t.Fatal(err)
	}

	ui := &shell.UI{Store: store, Win: w, HasCfg: true, RepMonth: "2026-08",
		Cfg: backend.Config{Repos: []string{"exampleorg"}, WorklogOwner: "sample-owner",
			SupportSetPay: 125, BaseSalary: 2793, Currency: "RM", DisplayCurrency: "IDR", FxRate: 3.8}}
	w.SetContent(report.BuildReportTab(ui))
	key := shell.ReportCacheKey("2026-08")
	ui.EnsureProjectCache()
	ui.ProjItems[key] = []backend.WorklogItem{{
		Date: "2026-08-11", Minutes: 600, Owner: "sample-owner",
		URL: "https://github.com/exampleorg/work/issues/1",
	}}
	for i := 0; i < backend.SupportSetSize; i++ {
		ui.ProjItems[key] = append(ui.ProjItems[key], backend.WorklogItem{
			Date: "2026-08-16", Owner: "sample-owner", Title: "Weekend support",
			URL: fmt.Sprintf("https://github.com/exampleorg/support/issues/%d", i),
		})
	}
	ui.ProjLoaded[key] = true
	report.DrawReport(ui)

	top, ok := ui.RepBox.Objects[0].(*fyne.Container)
	if !ok || len(top.Objects) != 3 {
		t.Fatalf("top row should contain the three money cards, got %T", ui.RepBox.Objects[0])
	}
	wants := [][]string{
		{"Payable", "RM 258.00", "1.00 days × RM 133.00", "(RM 133.00 before support)", "Converted: IDR 980.40"},
		{"Payable after drafts", "RM 324.50", "1.50 days × RM 133.00", "(RM 199.50 before support)", "Converted: IDR 1,233.10"},
		{"Support bonus", "RM 125.00", "Converted: IDR 475.00"},
	}
	for i, want := range wants {
		got := labels(top.Objects[i])
		for _, text := range want {
			if !contains(got, text) {
				t.Fatalf("top card %d should contain %q: %v", i, text, got)
			}
		}
	}
	darkBlue := false
	walk(top.Objects[2], func(o fyne.CanvasObject) {
		if rect, ok := o.(*canvas.Rectangle); ok && sameColor(rect.FillColor, report.ReportSupportSurface) {
			darkBlue = true
		}
	})
	if !darkBlue {
		t.Fatal("support bonus card should use the dark-blue report surface")
	}
	beforeSupport := []string{"(RM 133.00 before support)", "(RM 199.50 before support)"}
	for i, card := range top.Objects[:2] {
		blueAside := false
		walk(card, func(o fyne.CanvasObject) {
			if text, ok := o.(*canvas.Text); ok && text.Text == beforeSupport[i] && sameColor(text.Color, report.ReportSupportAccent()) {
				blueAside = text.Alignment == fyne.TextAlignTrailing && text.TextSize == theme.Size(theme.SizeNameCaptionText)
			}
		})
		if !blueAside {
			t.Fatal("each payable card should inline its pre-support value in small blue text")
		}
	}
	conversions := []struct {
		text  string
		color color.Color
	}{
		{"Converted: IDR 980.40", report.ReportConversionAccent()},
		{"Converted: IDR 1,233.10", report.ReportConversionAccent()},
		{"Converted: IDR 475.00", report.ReportSupportConversionAccent},
	}
	for i, want := range conversions {
		highlighted := false
		walk(top.Objects[i], func(o fyne.CanvasObject) {
			if text, ok := o.(*canvas.Text); ok && text.Text == want.text && sameColor(text.Color, want.color) {
				highlighted = text.TextStyle.Bold && text.TextSize == theme.Size(theme.SizeNameSubHeadingText)
			}
		})
		if !highlighted {
			t.Fatalf("card %d should emphasize its converted amount in green", i)
		}
	}
	inlineDetails := [][2]string{
		{"1.00 days × RM 133.00", "Converted: IDR 980.40"},
		{"1.50 days × RM 133.00", "Converted: IDR 1,233.10"},
		{"7 issues, 1 set(s)", "Converted: IDR 475.00"},
	}
	for i, detail := range inlineDetails {
		if !hasInlineReportDetail(top.Objects[i], detail[0], detail[1]) {
			t.Fatalf("card %d should keep %q inline with %q", i, detail[0], detail[1])
		}
	}
	if got := labels(top.Objects[2]); !inOrder(got, "7 issues, 1 set(s)", "Converted: IDR 475.00") {
		t.Fatalf("support conversion should follow the set count: %v", got)
	}
	if got := labels(top); contains(got, "Time logged") || contains(got, "Saved here, not pushed") {
		t.Fatalf("time cards should no longer be above the chart: %v", got)
	}
	if got := labels(ui.RepBox.Objects[2]); !contains(got, "By organisation") {
		t.Fatalf("the organisation legend should immediately follow the chart: %v", got)
	}
	if got := labels(ui.RepBox.Objects[3]); !contains(got, "Time logged") ||
		!contains(got, "÷ 8h = 1.25 days") || !contains(got, "Saved here, not pushed") ||
		!contains(got, "÷ 8h = 0.50 days") {
		t.Fatalf("time cards should sit below the legend with 8-hour day equivalents: %v", got)
	}
	if contains(labels(ui.RepBox), "Days over 480") {
		t.Fatal("days over 480 should not be rendered")
	}
}

func hasInlineReportDetail(o fyne.CanvasObject, left, right string) bool {
	if themed, ok := o.(*container.ThemeOverride); ok {
		return hasInlineReportDetail(themed.Content, left, right)
	}
	c, ok := o.(*fyne.Container)
	if !ok {
		return false
	}
	if len(c.Objects) == 2 && contains(labels(c.Objects[0]), left) && contains(labels(c.Objects[1]), right) {
		return true
	}
	if len(c.Objects) == 2 && contains(labels(c.Objects[1]), left) && contains(labels(c.Objects[0]), right) {
		return true
	}
	for _, child := range c.Objects {
		if hasInlineReportDetail(child, left, right) {
			return true
		}
	}
	return false
}
