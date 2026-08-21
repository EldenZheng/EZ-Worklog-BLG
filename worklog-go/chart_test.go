package main

import (
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func testChartDays(dates ...string) []chartDay {
	var days []chartDay
	for i, d := range dates {
		mins := 240 + i*60
		days = append(days, chartDay{
			date:  d,
			byOrg: map[string]int{"bigledger": mins},
			total: mins,
		})
	}
	return days
}

// A bar names the day under the pointer at any width: the chart is drawn from
// the size it is given, so the mapping is the same arithmetic the layout uses.
func TestDayChartMapsPointerToDay(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	dates := []string{"2026-08-01", "2026-08-02", "2026-08-03", "2026-08-04"}
	c := newDayChart(Config{Repos: []string{"bigledger"}}, testChartDays(dates...), target)

	for _, w := range []float32{920, 460, 1840} {
		c.Resize(fyne.NewSize(w, 260))
		span := w - chartSideMargin*2

		if got := c.days[c.slotAt(chartSideMargin+span*0.01)].date; got != dates[0] {
			t.Fatalf("at width %g the first bar read as %q", w, got)
		}
		if got := c.days[c.slotAt(chartSideMargin+span*0.99)].date; got != dates[3] {
			t.Fatalf("at width %g the last bar read as %q", w, got)
		}
		if got := c.days[c.slotAt(chartSideMargin+span*0.6)].date; got != dates[2] {
			t.Fatalf("at width %g the third bar read as %q", w, got)
		}
		if got := c.slotAt(1); got != -1 {
			t.Fatalf("the left margin belongs to no day, got slot %d", got)
		}
		if got := c.slotAt(w + 10); got != -1 {
			t.Fatalf("past the right edge belongs to no day, got slot %d", got)
		}
	}
}

// Hovering reports the day and leaving clears it; clicking opens it.
func TestDayChartHoverAndClick(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	c := newDayChart(Config{Repos: []string{"bigledger"}},
		testChartDays("2026-08-01", "2026-08-02"), target)
	c.Resize(fyne.NewSize(920, 260))

	var hovered, clicked string
	c.onHover = func(d string) { hovered = d }
	c.onSelect = func(d string) { clicked = d }

	mid := fyne.NewPos(500, 90)
	c.MouseIn(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: mid}})
	if hovered == "" {
		t.Fatal("hovering a bar should name its day")
	}
	c.MouseOut()
	if hovered != "" {
		t.Fatalf("leaving the chart should clear the readout, still %q", hovered)
	}
	c.Tapped(&fyne.PointEvent{Position: mid})
	if clicked == "" {
		t.Fatal("clicking a bar should open its day")
	}

	clicked = ""
	c.Tapped(&fyne.PointEvent{Position: fyne.NewPos(2, 90)})
	if clicked != "" {
		t.Fatalf("the margin is not a day, opened %q", clicked)
	}
}

// The bars are laid out from the widget's own size, so they scale with the
// window instead of being a fixed drawing pulled out of shape.
func TestDayChartBarsFollowTheWidgetSize(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	days := testChartDays("2026-08-01", "2026-08-02", "2026-08-03")
	days[2].byOrg = map[string]int{"bigledger": 480, "bigledger-support": 240}
	days[2].total = 720
	c := newDayChart(Config{Repos: []string{"bigledger", "BigLedger-Support"}}, days, target)
	r := c.CreateRenderer().(*dayChartRenderer)

	measure := func(w, h float32) (barW, tall float32) {
		c.Resize(fyne.NewSize(w, h))
		r.Layout(fyne.NewSize(w, h))
		return r.slots[0].segments[0].Size().Width, r.slots[2].segments[0].Size().Height
	}
	narrowW, narrowH := measure(600, 260)
	wideW, wideH := measure(1800, 400)

	if wideW <= narrowW {
		t.Fatalf("bars must widen with the chart: %g then %g", narrowW, wideW)
	}
	if wideH <= narrowH {
		t.Fatalf("bars must grow with the height: %g then %g", narrowH, wideH)
	}

	// The tallest day owns the scale, so its stack reaches the top of the plot.
	c.Resize(fyne.NewSize(900, 300))
	r.Layout(fyne.NewSize(900, 300))
	plotH := float32(300) - chartAxisHeight - chartTopMargin
	stack := float32(0)
	for i, seg := range r.slots[2].segments {
		h := seg.Size().Height
		if i > 0 {
			h -= chartBarRadius // the overlap that hides the seam
		}
		stack += h
	}
	if stack < plotH-1 || stack > plotH+1 {
		t.Fatalf("the busiest day should fill the plot: %g of %g", stack, plotH)
	}

	// Two orgs, two segments, in configured order from the baseline up.
	if len(r.slots[2].segments) != 2 {
		t.Fatalf("a two-org day needs two segments, got %d", len(r.slots[2].segments))
	}
	if r.slots[2].segments[0].Position().Y <= r.slots[2].segments[1].Position().Y {
		t.Fatal("the first org must sit at the baseline")
	}
	// A day with nothing logged draws nothing.
	empty := newDayChart(Config{}, []chartDay{{date: "2026-08-01"}}, target)
	er := empty.CreateRenderer().(*dayChartRenderer)
	if len(er.slots[0].segments) != 0 {
		t.Fatal("an empty day should draw no bar")
	}
}

// Clicking a bar switches to the Status tab with that day selected, loading a
// different month if the report was looking at one.
func TestOpenDayOnCalendarSwitchesTabs(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w,
		calMonth: "2026-08", repMonth: "2026-08"}
	ui.cfg = Config{Repos: []string{"bigledger"}} // no project URL: stays offline
	ui.buildAllTabs()

	ui.openDayOnCalendar("2026-07-15")
	if ui.calMonth != "2026-07" {
		t.Fatalf("the calendar should follow the bar's month, got %q", ui.calMonth)
	}
	if ui.selDay != "2026-07-15" {
		t.Fatalf("the day should be selected, got %q", ui.selDay)
	}
	if ui.tabs.SelectedIndex() != statusTabIndex {
		t.Fatalf("the Status tab should be showing, got index %d", ui.tabs.SelectedIndex())
	}
	if !contains(labels(ui.daySwap), "2026-07-15") {
		t.Fatalf("the side panel should be showing that day: %v", labels(ui.daySwap))
	}
}

// Each item in the day panel names the parent issue it is filed under, right
// by its duration, and the name is the link — the panel says what the work was
// and opens it in one click, rather than offering a nameless "Parent issue".
func TestDayPanelNamesTheParentIssue(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	// The test theme defines no bold monospace face, and the panel's duration
	// asks for one: laying it out under that theme panics inside the painter.
	test.ApplyTheme(t, theme.DefaultTheme())
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: "2026-08", repMonth: "2026-08"}
	ui.cfg = Config{Repos: []string{"bigledger"}}
	w.SetContent(ui.buildStatusTab())

	ui.ensureProjectCache()
	key := statusCacheKey("2026-08")
	ui.projItems[key] = []WorklogItem{{
		Date: "2026-08-11", Minutes: 90, Title: "Worklog 2026-08-11",
		URL:         "https://github.com/bigledger/blg-intranet/issues/900",
		ParentTitle: "DOC ITEM ENHANCEMENT",
		ParentURL:   "https://github.com/bigledger/blg-intranet/issues/42",
	}}
	ui.projLoaded[key] = true
	ui.selDay = "2026-08-11"
	ui.drawDayPanel()

	got := labels(ui.daySwap)
	if !contains(got, "DOC ITEM ENHANCEMENT") {
		t.Fatalf("the parent issue should be named on the card: %v", got)
	}
	if !contains(got, "90 min") {
		t.Fatalf("the duration should still be there: %v", got)
	}
	if contains(got, "Parent issue") {
		t.Fatalf("the nameless link should be gone once the name is the link: %v", got)
	}

	// The name itself is the link, pointing at the parent and not the worklog.
	var link *widget.Hyperlink
	walk(ui.daySwap, func(c fyne.CanvasObject) {
		if h, ok := c.(*widget.Hyperlink); ok && h.Text == "DOC ITEM ENHANCEMENT" {
			link = h
		}
	})
	if link == nil {
		t.Fatalf("the parent name should be clickable: %v", got)
	}
	if link.URL == nil || link.URL.String() != "https://github.com/bigledger/blg-intranet/issues/42" {
		t.Fatalf("the parent link points at %v", link.URL)
	}
}

// A day is opened to see where its hours went, so the panel leads with the
// worklog that took the most of them rather than with whatever the project
// happened to return first.
func TestDayPanelPutsTheBiggestWorklogOnTop(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: "2026-08", repMonth: "2026-08"}
	ui.cfg = Config{Repos: []string{"bigledger"}}
	w.SetContent(ui.buildStatusTab())

	ui.ensureProjectCache()
	key := statusCacheKey("2026-08")
	ui.projItems[key] = []WorklogItem{
		{Date: "2026-08-11", Minutes: 120, Title: "smallest",
			URL: "https://github.com/bigledger/repo/issues/3"},
		{Date: "2026-08-11", Minutes: 240, Title: "biggest",
			URL: "https://github.com/bigledger/repo/issues/1"},
		{Date: "2026-08-11", Minutes: 220, Title: "middling",
			URL: "https://github.com/bigledger/repo/issues/2"},
	}
	ui.projLoaded[key] = true
	ui.selDay = "2026-08-11"
	ui.drawDayPanel()

	got := labels(ui.daySwap)
	if !inOrder(got, "240 min", "220 min", "120 min") {
		t.Fatalf("the day should read 240, 220, 120 down the panel: %v", got)
	}
	// The day's own total is above all of them and is not one of the items.
	if !inOrder(got, "580/480 min", "240 min") {
		t.Fatalf("the day's total should still head the panel: %v", got)
	}
}

// With no day picked the panel keeps its place and says what to do, so the
// calendar does not resize itself every time a day is opened or closed.
func TestDayPanelHoldsItsPlaceWithNoSelection(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: "2026-08", repMonth: "2026-08"}
	ui.cfg = Config{Repos: []string{"bigledger"}}
	w.SetContent(ui.buildStatusTab())

	if !ui.daySide.Visible() {
		t.Fatal("the side panel must stay in the layout with nothing selected")
	}
	ui.drawDayPanel()
	got := strings.Join(labels(ui.daySwap), " ")
	if !strings.Contains(got, "Select any date") {
		t.Fatalf("expected the prompt to pick a date, got %q", got)
	}
	// Centred, not stacked at the top: the prompt is the panel's whole content.
	if findCenter(ui.daySwap) == nil {
		t.Fatal("the prompt should be centred in the panel")
	}
}

// findCenter returns the first container laid out by fyne's centre layout.
func findCenter(o fyne.CanvasObject) *fyne.Container {
	c, ok := o.(*fyne.Container)
	if !ok {
		return nil
	}
	if fmt.Sprintf("%T", c.Layout) == fmt.Sprintf("%T", container.NewCenter().Layout) {
		return c
	}
	for _, child := range c.Objects {
		if g := findCenter(child); g != nil {
			return g
		}
	}
	return nil
}

// The readout under the chart names the day and its split, and goes back to the
// caption when the pointer leaves.
func TestChartReadoutNamesTheDay(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w,
		cfg: Config{Repos: []string{"bigledger", "BigLedger-Support"}}}

	days := []chartDay{{
		date:  "2026-08-03",
		byOrg: map[string]int{"bigledger": 300, "bigledger-support": 180},
		total: 480,
	}}
	body := ui.chartWithReadout(days, "2026-08-01", "2026-08-31")

	var chart *dayChart
	walk(body, func(o fyne.CanvasObject) {
		if c, ok := o.(*dayChart); ok {
			chart = c
		}
	})
	if chart == nil {
		t.Fatal("no chart was built")
	}
	chart.onHover("2026-08-03")
	got := strings.Join(labels(body), " ")
	for _, want := range []string{"Mon 3 Aug", "8h of 8h", "bigledger 5h", "bigledger-support 3h"} {
		if !strings.Contains(got, want) {
			t.Fatalf("readout missing %q: %q", want, got)
		}
	}
	chart.onHover("")
	if !strings.Contains(strings.Join(labels(body), " "), "Minutes per day") {
		t.Fatal("leaving the chart should restore the caption")
	}
	// A day with nothing on it says so rather than showing a zero split.
	chart.onHover("2026-08-04")
	if !strings.Contains(strings.Join(labels(body), " "), "nothing logged") {
		t.Fatal("an empty day should be named as empty")
	}
}

// Opening the Report tab re-reads the exchange rate at most once a day: the
// rate barely moves, the tab gets opened constantly, but a stamp from last
// month quietly misstates the pay.
func TestRefreshRateOnlyOncePerDay(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, hasCfg: true,
		calMonth: thisMonth(), repMonth: thisMonth()}
	ui.cfg = Config{Currency: "RM", DisplayCurrency: "USD", FxRate: 4.2, FxUpdated: today()}

	ui.refreshRate(false)
	if ui.rateLoading {
		t.Fatal("a rate fetched today should not be fetched again on open")
	}

	// Settings has no rate to convert to: nothing to fetch, nothing to do.
	ui.cfg = Config{Currency: "RM"}
	ui.refreshRate(true)
	if ui.rateLoading {
		t.Fatal("with no display currency there is nothing to fetch")
	}

	// Unconfigured app: the tab must not reach for the network at all.
	ui.hasCfg = false
	ui.cfg = Config{Currency: "RM", DisplayCurrency: "USD", FxUpdated: "2026-01-01"}
	ui.refreshRate(false)
	if ui.rateLoading {
		t.Fatal("an unconfigured app should not fetch a rate")
	}
}
