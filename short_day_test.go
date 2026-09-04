package main

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// A day cell holds three lines now, so the floor has to leave room for them.
func TestDayCellIsTallEnoughForItsScore(t *testing.T) {
	if dayCellMinHeight < 90 {
		t.Fatalf("a cell carrying a date and two score lines needs the height, got %d",
			dayCellMinHeight)
	}
}

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

	// Every worked day scores itself against the target now; the colour is what
	// says which kind of day it was. A day with nothing on it has no readout at
	// all — untouched is not the same as short.
	var shortText, fullText *canvas.Text
	walk(ui.calBox, func(o fyne.CanvasObject) {
		txt, ok := o.(*canvas.Text)
		if !ok {
			return
		}
		switch txt.Text {
		case "240/480":
			shortText = txt
		case "480/480":
			fullText = txt
		}
	})
	if shortText == nil {
		t.Fatalf("the short day's readout should be its own coloured text: %v", labels(ui.calBox))
	}
	if !sameColor(shortText.Color, theme.Color(theme.ColorNameWarning)) {
		t.Fatalf("a short day's readout should carry the warning colour, got %v", shortText.Color)
	}
	if fullText == nil {
		t.Fatalf("a finished day scores itself too: %v", labels(ui.calBox))
	}
	if !sameColor(fullText.Color, theme.Color(theme.ColorNameSuccess)) {
		t.Fatalf("a finished day should read green, not warning: %v", fullText.Color)
	}
	// An untouched day is left unwritten, so a month not yet worked does not
	// read as a failed one. Matched exactly: "0/480" is a substring of the
	// "240/480" two cells along.
	walk(ui.calBox, func(o fyne.CanvasObject) {
		if txt, ok := o.(*canvas.Text); ok && txt.Text == "0/480" {
			t.Fatalf("a day with nothing on it should carry no score: %v", labels(ui.calBox))
		}
	})
}

// The four states the month is read by, and the one rule that sorts them.
func TestDayStateSortsTheFourKindsOfDay(t *testing.T) {
	test.NewApp()
	defer fyne.CurrentApp().Quit()
	test.ApplyTheme(t, theme.DefaultTheme())

	for _, c := range []struct {
		name        string
		mins, draft int
		wantFill    color.Color
		wantText    fyne.ThemeColorName
	}{
		{"done on the board", 480, 0,
			blendColor(theme.Color(theme.ColorNameBackground),
				theme.Color(theme.ColorNameSuccess), weekDoneTint), theme.ColorNameSuccess},
		{"over the target", 600, 0,
			blendColor(theme.Color(theme.ColorNameBackground),
				theme.Color(theme.ColorNameSuccess), weekDoneTint), theme.ColorNameSuccess},
		// Finished, but only once the drafts count: the same green, lighter,
		// because the sending is still outstanding.
		{"done by drafts", 360, 180, draftDoneFill(), theme.ColorNameSuccess},
		{"short even with drafts", 360, 60, shortFill(), theme.ColorNameWarning},
		{"worked and short", 240, 0, shortFill(), theme.ColorNameWarning},
		{"drafts only, still short", 0, 60, shortFill(), theme.ColorNameWarning},
		// Untouched is not short.
		{"nothing at all", 0, 0, color.Transparent, theme.ColorNameForeground},
	} {
		got := dayState(c.mins, c.draft)
		if !sameColor(got.fill, c.wantFill) {
			t.Fatalf("%s: fill = %v, want %v", c.name, got.fill, c.wantFill)
		}
		if got.textColor != c.wantText {
			t.Fatalf("%s: text = %v, want %v", c.name, got.textColor, c.wantText)
		}
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
