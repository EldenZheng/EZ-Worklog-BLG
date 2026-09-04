package main

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// Status and Report read the GitHub project, so until an entry is pushed it is
// worked time showing up nowhere: the day reads empty and is indistinguishable
// from a day nobody worked. draftMinutesByDay is what those tabs paint from.
func TestDraftMinutesCountOnlyUnpushedRowsInRange(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &UI{store: newStore(t.TempDir()), win: w, calMonth: "2026-08", repMonth: "2026-08"}
	ui.cfg = Config{Repos: []string{"bigledger"}}

	if _, err := ui.store.AppendRows([]Row{
		{"date": "2026-08-11", "minutes": "120", "issue": "bigledger/repo#1"},
		{"date": "2026-08-11", "minutes": "60", "issue": "bigledger/repo#2"},
		// Already on GitHub: the board reports it, so counting it here would
		// draw it twice.
		{"date": "2026-08-12", "minutes": "480", "issue": "bigledger/repo#3",
			"pushed_at": "2026-08-12T10:00:00"},
		// Outside the month being drawn.
		{"date": "2026-07-30", "minutes": "90", "issue": "bigledger/repo#4"},
	}); err != nil {
		t.Fatal(err)
	}

	got := ui.draftMinutesByDay("2026-08-01", "2026-08-31")
	if got["2026-08-11"] != 180 {
		t.Fatalf("two unpushed entries on one day should sum, got %d", got["2026-08-11"])
	}
	if _, ok := got["2026-08-12"]; ok {
		t.Fatal("a pushed entry is the board's to report, not a draft")
	}
	if _, ok := got["2026-07-30"]; ok {
		t.Fatal("a date outside the range asked for should not be counted")
	}

	// Switching an org off on the tab takes its drafts with it, the same as it
	// takes that org's items.
	ui.orgOff = map[string]bool{"bigledger": true}
	if len(ui.draftMinutesByDay("2026-08-01", "2026-08-31")) != 0 {
		t.Fatal("a filtered-out org's drafts should leave the view with its items")
	}
}

// A day with work saved locally and nothing pushed used to read as a day nobody
// worked. It now carries the draft's own yellow, in the cell and in its number.
func TestCalendarShowsUnpushedWorkInYellow(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()

	if _, err := ui.store.AppendRows([]Row{
		{"date": "2026-08-13", "minutes": "150", "issue": "bigledger/blg-intranet#7"},
	}); err != nil {
		t.Fatal(err)
	}
	ui.drawCalendar()

	var draftText *canvas.Text
	walk(ui.calBox, func(o fyne.CanvasObject) {
		if txt, ok := o.(*canvas.Text); ok && txt.Text == "+150m · 31%" {
			draftText = txt
		}
	})
	if draftText == nil {
		t.Fatalf("a day with only unpushed work should still say so: %v", labels(ui.calBox))
	}
	// And the day still scores itself against the target, at nothing banked.
	if !contains(labels(ui.calBox), "0/480") {
		t.Fatalf("the cell should say what the board holds: %v", labels(ui.calBox))
	}
	if !sameColor(draftText.Color, theme.Color(theme.ColorNameWarning)) {
		t.Fatalf("the draft readout should carry the warning colour, got %v", draftText.Color)
	}

	// And the fill under it, so the month can be read without the numbers.
	found := false
	walk(ui.calBox, func(o fyne.CanvasObject) {
		if r, ok := o.(*canvas.Rectangle); ok && sameColor(r.FillColor, draftYellow(meterTint)) {
			found = true
		}
	})
	if !found {
		t.Fatal("the cell should fill in the draft colour, not stay empty")
	}

	// The month's own line keeps the two apart: the total is what the board
	// holds, and the drafts are named beside it rather than added into it.
	if ui.calSummary.Text != "180 min logged this month · 150 min saved here, waiting to push" {
		t.Fatalf("drafts must be named without being banked, got %q", ui.calSummary.Text)
	}
}

// A day that misses the target on the board but clears it once the drafts are
// counted is not a short day: the hours were worked, only the push is left.
// Yellow there would send you hunting for work already done, so it goes green —
// the chart washes the column, the calendar colours the number.
func TestADayTheDraftsCompleteReadsGreenNotShort(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()

	key := statusCacheKey("2026-08")
	ui.projItems[key] = []WorklogItem{
		{Date: "2026-08-14", Minutes: 360, Title: "most of a day",
			URL: "https://github.com/bigledger/blg-intranet/issues/9"},
	}
	if _, err := ui.store.AppendRows([]Row{
		// 360 + 180 clears 480; on the board alone the day is short.
		{"date": "2026-08-14", "minutes": "180", "issue": "bigledger/blg-intranet#9"},
	}); err != nil {
		t.Fatal(err)
	}
	ui.drawCalendar()

	var readout *canvas.Text
	walk(ui.calBox, func(o fyne.CanvasObject) {
		// 360 banked plus 180 waiting clears the target: 540 of 480 is 112%.
		if txt, ok := o.(*canvas.Text); ok && txt.Text == "+180m · 112%" {
			readout = txt
		}
	})
	if readout == nil {
		t.Fatalf("the day should show what it holds and what is waiting: %v", labels(ui.calBox))
	}
	if !contains(labels(ui.calBox), "360/480") {
		t.Fatalf("the cell should score the board against the target: %v", labels(ui.calBox))
	}
	if !sameColor(readout.Color, theme.Color(theme.ColorNameSuccess)) {
		t.Fatalf("a day the drafts complete should read green, got %v", readout.Color)
	}

	// The chart says the same thing behind the bar.
	days := []chartDay{
		{date: "2026-08-14", byOrg: map[string]int{"bigledger": 360}, total: 360, draft: 180},
		{date: "2026-08-15", byOrg: map[string]int{"bigledger": 360}, total: 360, draft: 60},
	}
	c := newDayChart(Config{Repos: []string{"bigledger"}}, days, target)
	r := c.CreateRenderer().(*dayChartRenderer)
	c.Resize(fyne.NewSize(900, 300))
	r.Layout(fyne.NewSize(900, 300))

	if !sameColor(r.slots[0].highlight.FillColor, draftDoneFill()) {
		t.Fatalf("a completed-by-draft column should be washed green, got %v",
			r.slots[0].highlight.FillColor)
	}
	// Drafts that do not reach the target change nothing: still short, still
	// yellow, because the day really is owed more time.
	if !sameColor(r.slots[1].highlight.FillColor, shortFill()) {
		t.Fatalf("drafts that fall short leave the day short, got %v",
			r.slots[1].highlight.FillColor)
	}
}

// isDraftComplete is the one rule both tabs ask, so it is worth pinning down.
func TestOnlyADayCarriedOverTheLineIsDraftComplete(t *testing.T) {
	for _, c := range []struct {
		mins, draft int
		want        bool
	}{
		{360, 180, true},  // short on the board, carried over by the drafts
		{0, target, true}, // nothing pushed at all, but the day is done
		{360, 60, false},  // still owed time even counting the drafts
		{360, 0, false},   // plainly short: there is nothing waiting
		{target, 60, false},
		{target + 60, 60, false}, // already past the line without them
		{0, 0, false},
	} {
		if got := isDraftComplete(c.mins, c.draft); got != c.want {
			t.Fatalf("isDraftComplete(%d, %d) = %v, want %v", c.mins, c.draft, got, c.want)
		}
	}
}

// On a day that already has pushed work, the draft caps the bar rather than
// replacing it — and the scale has to hold both or it would draw off the plot.
func TestChartStacksDraftOnTopOfBankedWork(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())

	// The first day is already at target and still has work waiting, so the
	// draft is what pushes the plot past the goal — which is the case that used
	// to draw off the top of it.
	days := []chartDay{
		{date: "2026-08-24", byOrg: map[string]int{"bigledger": 480}, total: 480, draft: 120},
		{date: "2026-08-25", byOrg: map[string]int{"bigledger": 480}, total: 480},
	}
	c := newDayChart(Config{Repos: []string{"bigledger"}}, days, target)
	r := c.CreateRenderer().(*dayChartRenderer)
	c.Resize(fyne.NewSize(900, 300))
	r.Layout(fyne.NewSize(900, 300))

	if r.slots[1].draft != nil {
		t.Fatal("a day with nothing waiting should not carry a draft segment")
	}
	d := r.slots[0].draft
	if d == nil {
		t.Fatal("a day with work waiting should carry a draft segment")
	}
	if !sameColor(d.FillColor, draftYellow(draftSolidTint)) {
		t.Fatalf("the draft segment should be the draft yellow, got %v", d.FillColor)
	}
	// Above the banked segment, which is the floor it stands on.
	if banked := r.slots[0].segments[0]; d.Position().Y >= banked.Position().Y {
		t.Fatalf("the draft should cap the stack: draft at %g, banked at %g",
			d.Position().Y, banked.Position().Y)
	}

	// 480 banked + 120 waiting: the scale tops out at 600, not the 480 goal, so
	// nothing pokes out of the plot.
	if got := c.maxValue(); got != 600 {
		t.Fatalf("the scale should hold banked plus draft, got %d", got)
	}
}

// The week strip's meter already carried drafts; what changed is the colour.
// One translucent yellow block, whatever orgs the drafts belong to.
func TestWeekMeterDrawsDraftsAsOneTranslucentBlock(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, theme.DefaultTheme())

	cfg := Config{Repos: []string{"bigledger", "BigLedger-Support"}}
	bar := meterBarWithDraft(cfg,
		map[string]int{"bigledger": 120},
		map[string]int{"bigledger": 60, "bigledger-support": 60}, target, 8)
	bar.Resize(fyne.NewSize(480, 8))

	var drafts []*canvas.Rectangle
	walk(bar, func(o fyne.CanvasObject) {
		if r, ok := o.(*canvas.Rectangle); ok && sameColor(r.FillColor, draftWash()) {
			drafts = append(drafts, r)
		}
	})
	if len(drafts) != 1 {
		t.Fatalf("two orgs' drafts are one block on the bar, got %d", len(drafts))
	}
	if drafts[0].FillColor.(color.NRGBA).A == 0xff {
		t.Fatal("the draft block should be translucent so the track reads through it")
	}
	// 120 banked + 120 waiting of a 480 target is half the bar between them.
	if w := drafts[0].Size().Width; w < 110 || w > 130 {
		t.Fatalf("120 waiting minutes should take a quarter of the bar, got %g of 480", w)
	}
}
