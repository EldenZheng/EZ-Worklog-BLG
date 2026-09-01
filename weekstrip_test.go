package main

import (
	"image/color"
	"strconv"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
)

// The day turns over at 08:00, not at midnight: work carried past midnight is
// still yesterday's, which is the day it has to be logged against.
func TestWorkDayTurnsOverAtEight(t *testing.T) {
	at := func(h int) time.Time {
		return time.Date(2026, 8, 12, h, 30, 0, 0, time.Local)
	}
	if got := workDate(at(1)); got != "2026-08-11" {
		t.Fatalf("1:30am belongs to the day before, got %s", got)
	}
	if got := workDate(at(7)); got != "2026-08-11" {
		t.Fatalf("7:30am is still the night before, got %s", got)
	}
	if got := workDate(at(8)); got != "2026-08-12" {
		t.Fatalf("8:30am starts the new day, got %s", got)
	}
	if got := workDate(at(23)); got != "2026-08-12" {
		t.Fatalf("late evening is its own day, got %s", got)
	}
}

// A week is Monday to Sunday, and a Sunday belongs to the week that started six
// days earlier — not to the one about to start.
func TestWeekRunsMondayToSunday(t *testing.T) {
	if got := weekStartOf("2026-08-16"); got != "2026-08-10" { // a Sunday
		t.Fatalf("Sunday should close the week that began on the 10th, got %s", got)
	}
	if got := weekStartOf("2026-08-10"); got != "2026-08-10" {
		t.Fatalf("a Monday is its own week start, got %s", got)
	}
	days := weekDates("2026-08-10")
	if len(days) != 7 || days[0] != "2026-08-10" || days[6] != "2026-08-16" {
		t.Fatalf("a week is seven days Mon–Sun, got %v", days)
	}
	if got := shiftWeek("2026-08-10", -1); got != "2026-08-03" {
		t.Fatalf("back a week is seven days, got %s", got)
	}

	if got := weekRangeLabel("2026-08-10"); got != "10 – 16 Aug 2026" {
		t.Fatalf("a week inside one month names it once, got %q", got)
	}
	// Crossing a month has to say both, or the label reads as an impossible range.
	if got := weekRangeLabel("2026-08-31"); got != "31 Aug – 6 Sep 2026" {
		t.Fatalf("a week across two months names both, got %q", got)
	}
}

// weekUI is a Log tab with the strip parked on a fixed week, so a test never
// depends on which week it is run in.
//
// No project URL by default: the strip asks GitHub for any month it has not got,
// and a test that wanders into an uncached month would shell out to gh for real
// — in the background, landing on whatever test happened to be running by then.
// A test that wants banked minutes sets the URL and seeds the months it names.
func weekUI(t *testing.T) *UI {
	t.Helper()
	ui := pendingUI(t)
	ui.cfg = Config{Repos: []string{"bigledger"}}
	ui.weekStart = "2026-08-10" // Monday
	return ui
}

// withProject points a test UI at a project and seeds the months it may read,
// this month included: the foot of the tab scores today whatever week is shown.
func withProject(ui *UI, months ...string) {
	ui.cfg.ProjectURL = "https://github.com/orgs/bigledger/projects/9"
	ui.ensureProjectCache()
	for _, m := range append(months, thisMonth()) {
		key := statusCacheKey(m)
		if !ui.projLoaded[key] {
			ui.projLoaded[key] = true
		}
	}
}

// The strip is the week as it stands: seven days, what GitHub already holds on
// each, how much of the 480 that is, and the entries themselves.
func TestWeekStripShowsWhatEachDayHolds(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	withProject(ui, "2026-08")
	key := statusCacheKey("2026-08")
	ui.projItems[key] = []WorklogItem{
		{Date: "2026-08-12", Minutes: 240, ParentTitle: "DOC ITEM ENHANCEMENT",
			URL: "https://github.com/bigledger/blg-intranet/issues/42"},
		{Date: "2026-08-13", Minutes: 480, Title: "Support rota",
			URL: "https://github.com/bigledger/blg-intranet/issues/9"},
	}
	ui.projLoaded[key] = true
	if thisMonth() != "2026-08" {
		// The line at the foot of the tab scores today's month, which the seeded
		// August above only covers while the clock says August.
		ui.projLoaded[statusCacheKey(thisMonth())] = true
	}

	// One entry saved here and never pushed, sitting on the Monday.
	if _, err := ui.store.AppendRows([]Row{{
		"date": "2026-08-10", "minutes": "45", "description": "standup notes",
	}}); err != nil {
		t.Fatal(err)
	}
	ui.drawRecent()

	got := labels(ui.weekBox)
	if !contains(got, "10 – 16 Aug 2026") {
		t.Fatalf("the strip should name the week it is showing: %v", got)
	}
	for _, day := range []string{"Mon 10", "Tue 11", "Sun 16"} {
		if !contains(got, day) {
			t.Fatalf("the week is missing %s: %v", day, got)
		}
	}
	if n := len(ui.weekCols); n != 7 {
		t.Fatalf("a week is seven columns, got %d", n)
	}

	// 240 of 480 is half the day, and a full day says so.
	if !contains(got, "240 min · 50%") {
		t.Fatalf("a day should score itself against the target: %v", got)
	}
	if !contains(got, "480 min · 100% ✓") {
		t.Fatalf("a day that made target should be marked: %v", got)
	}
	if !contains(got, "nothing logged") {
		t.Fatalf("an empty day should say so rather than read as broken: %v", got)
	}
	// Work already on GitHub is counted, not listed: it is the bar and the score,
	// and listing it again buried the one card there is anything left to do to.
	//
	// Checked against the day columns alone. The by-issue panel below the strip
	// does name that work, on purpose — it is the week's summary — so scanning
	// the whole tab would now catch it there and say nothing about the columns.
	var inColumns []string
	for _, col := range ui.weekCols {
		inColumns = append(inColumns, labels(col)...)
	}
	if contains(inColumns, "DOC ITEM") || contains(inColumns, "Support rota") {
		t.Fatalf("pushed worklogs should not be listed on the day: %v", inColumns)
	}
	// The unpushed entry shows on its day too, counted apart from the banked
	// minutes so the day's real total is never overstated, and totalled beside
	// them so the day it would come to is there to read.
	if !contains(got, "+45 min") {
		t.Fatalf("locally saved minutes should be called out separately: %v", got)
	}
	if !contains(got, "45 min · 9%") {
		t.Fatalf("the day should also score what it would come to: %v", got)
	}
	// The waiting entry is not a chip but the card itself, standing on its day
	// with the buttons that act on it.
	if !contains(got, "standup notes") {
		t.Fatalf("an unpushed entry should stand on its day: %v", got)
	}
	if !contains(got, "Push") {
		t.Fatalf("the card on the day should carry its own Push: %v", got)
	}
	if n := len(dragTilesIn(ui.weekBox)); n != 1 {
		t.Fatalf("the card on the day should be the draggable one, got %d", n)
	}
	// And it is not repeated in the list underneath.
	if below := labels(ui.recentBox); contains(below, "standup notes") {
		t.Fatalf("an entry on the week above should not be listed twice: %v", below)
	}
	if !contains(labels(ui.recentBox), "1 on the week below") {
		t.Fatalf("the list should account for what it is no longer showing: %v", labels(ui.recentBox))
	}
}

// A day holding several waiting cards stacks them biggest first, so the one
// worth the most minutes is the one at the top of the column.
func TestTheBiggestCardStandsAtTheTopOfItsDay(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	// Saved in the order they were typed, which is not the order they are worth.
	if _, err := ui.store.AppendRows([]Row{
		{"date": "2026-08-11", "minutes": "120", "description": "smallest of three"},
		{"date": "2026-08-11", "minutes": "240", "description": "biggest of three"},
		{"date": "2026-08-11", "minutes": "220", "description": "middling of three"},
	}); err != nil {
		t.Fatal(err)
	}
	ui.drawRecent()

	got := labels(ui.weekCols["2026-08-11"])
	if !inOrder(got, "240 min", "220 min", "120 min") {
		t.Fatalf("the day should stack its cards 240, 220, 120: %v", got)
	}
	// Descending by minutes, not by what the card says: the descriptions follow
	// their own cards rather than the order they were saved in.
	if !inOrder(got, "biggest of three", "middling of three", "smallest of three") {
		t.Fatalf("each card should carry its own description down the column: %v", got)
	}
}

// The card standing on a day says what it is worth and what it points at, both
// in full: a cut-off issue ref names no repo in particular.
func TestTheCardOnADaySpellsItselfOut(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	const ref = "bigledger/blg-sd-senwave-senheng#490"
	if _, err := ui.store.AppendRows([]Row{{
		"date": "2026-08-11", "minutes": "90", "description": "senheng fixes",
		"issue": ref,
	}}); err != nil {
		t.Fatal(err)
	}
	ui.drawRecent()

	got := labels(ui.weekBox)
	if !contains(got, ref) {
		t.Fatalf("the issue ref should be readable in full, not elided: %v", got)
	}
	if !contains(got, "90 min") {
		t.Fatalf("the minutes are the number the card carries: %v", got)
	}
	for _, want := range []string{"Push", "senheng fixes"} {
		if !contains(got, want) {
			t.Fatalf("the card is missing %q: %v", want, got)
		}
	}
}

// greenness is how far a colour leans towards green, used to tell a finished day
// from an unfinished one without pinning the test to an exact theme colour.
func greenness(c color.Color) float64 {
	r, g, b, a := c.RGBA()
	if a == 0 {
		return 0
	}
	return float64(g) - float64(r+b)/2
}

// A day that made its target is green, so a week reads without arithmetic. One
// that only makes it by counting what is still waiting is a paler green, because
// that is a paler claim.
func TestFinishedDaysGoGreen(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	withProject(ui, "2026-08")
	key := statusCacheKey("2026-08")
	ui.projItems[key] = []WorklogItem{
		{Date: "2026-08-12", Minutes: 480, Title: "a full day",
			URL: "https://github.com/bigledger/blg-intranet/issues/9"},
	}
	ui.projLoaded[key] = true
	// The Monday reaches the target only on drafts.
	if _, err := ui.store.AppendRows([]Row{{
		"date": "2026-08-10", "minutes": "480", "description": "not sent yet",
	}}); err != nil {
		t.Fatal(err)
	}
	ui.drawRecent()

	pushed := greenness(ui.weekCols["2026-08-12"].baseFill)
	draft := greenness(ui.weekCols["2026-08-10"].baseFill)
	empty := greenness(ui.weekCols["2026-08-11"].baseFill)
	if pushed <= 0 {
		t.Fatalf("a day banked on GitHub should read as done, got %v", pushed)
	}
	if draft <= 0 || draft >= pushed {
		t.Fatalf("a day finished only on drafts should be a lighter green: draft %v, pushed %v",
			draft, pushed)
	}
	if empty != 0 {
		t.Fatalf("a day with nothing on it should stay unpainted, got %v", empty)
	}
}

// Carrying a card over a day makes that day answer: it breathes towards the
// colour it would take if the card landed there, and settles back when it does
// not.
func TestTheDayUnderACardBreathes(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	if _, err := ui.store.AppendRows([]Row{{
		"date": "2026-08-10", "minutes": "60", "description": "triage",
	}}); err != nil {
		t.Fatal(err)
	}
	ui.drawRecent()
	tile := dragTilesIn(ui.weekBox)[0]

	wed := ui.weekCols["2026-08-12"]
	if wed.pulse != nil {
		t.Fatal("a day nothing is being carried over should sit still")
	}
	tile.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(4, -20)})
	wed.MouseIn(&desktop.MouseEvent{})
	if wed.pulse == nil {
		t.Fatal("the day under the carried card should be animating")
	}
	if other := ui.weekCols["2026-08-13"]; other.pulse != nil {
		t.Fatal("only the day under the card should be animating")
	}

	wed.MouseOut()
	if wed.pulse != nil {
		t.Fatal("the card moved on, so the day should have settled")
	}
	if wed.frame.FillColor != wed.baseFill {
		t.Fatal("a day that settles goes back to the colour it earned on its own")
	}
	tile.DragEnd()
}

// Nothing configured to score against: the strip still draws, and says why it
// is empty rather than showing every day as a day with nothing done.
func TestWeekStripWithNoProjectSaysSo(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()
	ui.weekStart = "2026-08-10"
	ui.drawWeekStrip()

	if got := labels(ui.weekBox); !contains(got, "Set a project URL in Settings") {
		t.Fatalf("with no project the strip should explain itself: %v", got)
	}
}

// dragTilesIn collects the cards that can be picked up.
func dragTilesIn(o fyne.CanvasObject) []*dragTile {
	var out []*dragTile
	walk(o, func(c fyne.CanvasObject) {
		if d, ok := c.(*dragTile); ok {
			out = append(out, d)
		}
	})
	return out
}

// Dragging a saved card onto a day is how its date is set: the drop writes the
// date, so the Push button sends the day it was dropped on.
func TestDroppingACardMovesItToThatDay(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	made, err := ui.store.AppendRows([]Row{{
		"date": "2026-08-10", "minutes": "90", "description": "fix the picker",
		"issue": "bigledger/blg-intranet#42",
	}})
	if err != nil {
		t.Fatal(err)
	}
	ui.drawRecent()

	// The card stands on the Monday it is filed under, in the strip itself.
	tiles := dragTilesIn(ui.weekBox)
	if len(tiles) != 1 {
		t.Fatalf("an unpushed entry should be a card that can be picked up, got %d", len(tiles))
	}
	tile := tiles[0]

	// Picked up, carried over the Wednesday, dropped.
	tile.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(4, -60)})
	if ui.dragRow == nil {
		t.Fatal("the strip has to know a card is in the air, or nothing lights up")
	}
	if ui.dragGhost == nil {
		t.Fatal("the carried card should be drawn above the tab while it is in the air")
	}
	target := ui.weekCols["2026-08-12"]
	target.MouseIn(&desktop.MouseEvent{})
	if target.frame.StrokeColor != theme.Color(theme.ColorNamePrimary) {
		t.Fatal("the day under the card should be lit while it is being carried")
	}
	if other := ui.weekCols["2026-08-13"]; other.frame.StrokeColor == theme.Color(theme.ColorNamePrimary) {
		t.Fatal("only the day under the card should be lit")
	}
	tile.DragEnd()

	rows, err := ui.store.ReadRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["date"] != "2026-08-12" {
		t.Fatalf("the drop should have moved the entry to the Wednesday, got %v", rows)
	}
	// Everything else about the entry is untouched — a drop sets a date, no more.
	if rows[0]["minutes"] != "90" || rows[0]["id"] != made[0]["id"] {
		t.Fatalf("a drop should change the date and nothing else, got %v", rows[0])
	}
	if ui.dragRow != nil || ui.hoverDay != "" {
		t.Fatal("the drop should have put the card down")
	}
	if ui.dragGhost != nil || len(ui.dragLayer.Objects) != 0 {
		t.Fatal("the carried card should be gone once it lands")
	}
}

// Let go over nothing, or over the day it already sits on: the entry stays as it
// was. A drag that missed must not quietly rewrite a date.
func TestDropOnNothingLeavesTheDateAlone(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()

	if _, err := ui.store.AppendRows([]Row{{
		"date": "2026-08-10", "minutes": "30", "description": "triage",
	}}); err != nil {
		t.Fatal(err)
	}
	ui.drawRecent()
	tile := dragTilesIn(ui.weekBox)[0]

	tile.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(0, -20)})
	tile.DragEnd() // never over a day
	rows, _ := ui.store.ReadRows()
	if rows[0]["date"] != "2026-08-10" {
		t.Fatalf("a drop on nothing should change nothing, got %s", rows[0]["date"])
	}

	tile = dragTilesIn(ui.weekBox)[0]
	tile.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(0, -20)})
	ui.weekCols["2026-08-10"].MouseIn(&desktop.MouseEvent{})
	tile.DragEnd() // back on the day it started on
	rows, _ = ui.store.ReadRows()
	if len(rows) != 1 || rows[0]["date"] != "2026-08-10" {
		t.Fatalf("dropping a card where it already is changes nothing, got %v", rows)
	}
}

// A pushed entry is a date already on the board. It stays where it is rather
// than being dragged into disagreeing with GitHub.
func TestPushedEntriesCannotBeDragged(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	// Parked on a week neither entry belongs to, so both are in the list below
	// and this is a test of the list, not of the strip.
	ui.weekStart = "2020-01-06"

	if _, err := ui.store.AppendRows([]Row{
		{"date": today(), "minutes": "60", "description": "already sent",
			"pushed_at": "2026-08-11T10:00:00"},
		{"date": today(), "minutes": "60", "description": "still here"},
	}); err != nil {
		t.Fatal(err)
	}
	ui.showPushed.SetChecked(true) // both on show
	ui.drawRecent()

	tiles := dragTilesIn(ui.recentBox)
	if len(tiles) != 1 {
		t.Fatalf("only the unpushed entry should be draggable, got %d", len(tiles))
	}
	if tiles[0].row["description"] != "still here" {
		t.Fatalf("the wrong card is draggable: %v", tiles[0].row)
	}
}

// The strip walks the weeks and comes back: a day two weeks out is reachable
// without touching the system clock.
func TestWeekStripWalksTheWeeks(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	// Two weeks back, wherever the clock happens to be standing.
	ui.weekStart = shiftWeek(weekStartOf(today()), -2)
	ui.drawWeekStrip()

	back := buttonNamed(ui.weekBox, "This week")
	if back == nil {
		t.Fatal("the strip needs a way home from wherever it wandered to")
	}
	if back.Disabled() {
		t.Fatal("parked on another week, the way home should be live")
	}
	back.OnTapped()
	if ui.weekStart != weekStartOf(today()) {
		t.Fatalf("home is the week today falls in, got %s", ui.weekStart)
	}
	if b := buttonNamed(ui.weekBox, "This week"); b == nil || !b.Disabled() {
		t.Fatal("already home, the button has nothing left to do")
	}
}

// A week that straddles two months reads both of them, or the days either side
// of the 1st would show as empty when they are not.
func TestWeekAcrossTwoMonthsReadsBoth(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	ui.weekStart = "2026-08-31" // Mon 31 Aug – Sun 6 Sep

	withProject(ui, "2026-08", "2026-09")
	for month, item := range map[string]WorklogItem{
		"2026-08": {Date: "2026-08-31", Minutes: 120, Title: "August tail"},
		"2026-09": {Date: "2026-09-01", Minutes: 300, Title: "September head"},
	} {
		key := statusCacheKey(month)
		ui.projItems[key] = []WorklogItem{item}
		ui.projLoaded[key] = true
	}
	ui.drawWeekStrip()

	got := labels(ui.weekBox)
	if !contains(got, "120 min") || !contains(got, "300 min") {
		t.Fatalf("both months' days should be filled in: %v", got)
	}
	if !contains(got, strconv.Itoa(31)) {
		t.Fatalf("the week should still start on the 31st: %v", got)
	}
}
