package main

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// sameColor compares two colours by value. The report is full of coloured
// rectangles — an org's bar is as far from the background as the short-day wash
// is — so "looks warm" cannot tell them apart. Only the exact wash counts.
func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return false
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// A day worked and left short of the target keeps its column on the chart
// washed in the target line's own colour, so the short days can be picked out
// of a month without measuring any bar against the line.
func TestTheChartWashesColumnsThatFellShort(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())

	days := []chartDay{
		{date: "2026-08-24", byOrg: map[string]int{"bigledger": 240}, total: 240},
		{date: "2026-08-25", byOrg: map[string]int{"bigledger": 480}, total: 480},
		{date: "2026-08-26"}, // nothing logged
		{date: "2026-08-27", byOrg: map[string]int{"bigledger": 600}, total: 600},
	}
	c := newDayChart(Config{Repos: []string{"bigledger"}}, days, target)
	r := c.CreateRenderer().(*dayChartRenderer)
	c.Resize(fyne.NewSize(900, 300))
	r.Layout(fyne.NewSize(900, 300))

	want := []bool{true, false, false, false}
	for i, w := range want {
		washed := sameColor(r.slots[i].highlight.FillColor, shortFill())
		if washed != w {
			t.Fatalf("%s (%d min) washed=%v, want %v",
				days[i].date, days[i].total, washed, w)
		}
	}

	// Hovering a short column still lights it up, over the wash rather than
	// instead of it — the pointer must not make a short day look finished.
	c.hovered = 0
	r.Layout(fyne.NewSize(900, 300))
	hovered := r.slots[0].highlight.FillColor
	if sameColor(hovered, shortFill()) {
		t.Fatal("a hovered column should light up, not stay flat")
	}
	if sameColor(hovered, r.slots[1].highlight.FillColor) {
		t.Fatal("a hovered short day must not look like a finished one")
	}
}

// The calendar says the same thing in the same colour, in the readout rather
// than behind it: the cell's fill already shows how full a day is, but only
// against itself.
func TestTheCalendarColoursAShortDaysReadout(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()

	key := statusCacheKey("2026-08")
	ui.projItems[key] = []WorklogItem{
		{Date: "2026-08-11", Minutes: 240, Title: "half a day",
			URL: "https://github.com/bigledger/blg-intranet/issues/1"},
		{Date: "2026-08-12", Minutes: 480, Title: "a full day",
			URL: "https://github.com/bigledger/blg-intranet/issues/2"},
	}
	ui.drawCalendar()

	// "240m  50%" is warned about; "480m  100%" is not, and neither is a day
	// with nothing on it, which has no readout at all.
	var shortText, fullText *canvas.Text
	walk(ui.calBox, func(o fyne.CanvasObject) {
		txt, ok := o.(*canvas.Text)
		if !ok {
			return
		}
		switch txt.Text {
		case "240m  50%":
			shortText = txt
		case "480m  100%":
			fullText = txt
		}
	})
	if shortText == nil {
		t.Fatalf("the short day's readout should be its own coloured text: %v", labels(ui.calBox))
	}
	if !sameColor(shortText.Color, theme.Color(theme.ColorNameWarning)) {
		t.Fatalf("a short day's readout should carry the warning colour, got %v", shortText.Color)
	}
	if fullText != nil {
		t.Fatal("a finished day needs no warning, so it is not drawn as coloured text")
	}
}

// isShortDay is the one rule both of them ask, so it is worth pinning down: a
// day with nothing on it is untouched, not short, and painting the two alike
// would make an empty period look like a failed one.
func TestOnlyAWorkedDayCanBeShort(t *testing.T) {
	cases := map[int]bool{0: false, 1: true, 479: true, target: false, target + 60: false}
	for mins, want := range cases {
		if got := isShortDay(mins); got != want {
			t.Fatalf("isShortDay(%d) = %v, want %v", mins, got, want)
		}
	}
}
