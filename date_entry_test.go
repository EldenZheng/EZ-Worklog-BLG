package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The worklog date is chosen from a month grid that paints each day by how full
// it already is — see datepicker.go for why fyne's own DateEntry could not.
func TestWorklogDatePickerScoresTheDaysItOffers(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()

	// The picker reads board minutes through monthWorklogs, which is the filtered
	// door and needs a project to read from — seeded here so nothing shells out.
	ui.cfg.ProjectURL = "https://github.com/orgs/bigledger/projects/9"
	key := statusCacheKey("2026-08")
	ui.projItems[key] = []WorklogItem{
		{Date: "2026-08-11", Minutes: 240, URL: "https://github.com/bigledger/blg-intranet/issues/1"},
		{Date: "2026-08-12", Minutes: 480, URL: "https://github.com/bigledger/blg-intranet/issues/2"},
	}
	ui.projLoaded[key] = true
	ui.projLoaded[statusCacheKey("2026-09")] = true
	ui.projLoaded[statusCacheKey("2026-07")] = true
	ui.projLoaded[statusCacheKey(thisMonth())] = true
	if _, err := ui.store.AppendRows([]Row{
		{"date": "2026-08-13", "minutes": "150", "type": kindCommit, "issue": "bigledger/repo#3"},
	}); err != nil {
		t.Fatal(err)
	}

	p := newWorklogDatePicker(ui, "2026-08-11")
	if got := p.ISO(); got != "2026-08-11" {
		t.Fatalf("the picker should open on the date it was given, got %q", got)
	}

	// Closed until it is asked for, and an overlay when it opens — so it never
	// pushes the fields under it down the form.
	if p.calendarOpen() {
		t.Fatal("the calendar should start closed")
	}
	// The calendar button lives inside the field, in the slot fyne keeps for it,
	// and it has to be there before the entry is first refreshed: the renderer
	// reads ActionItem once as it is built, so one attached later is invisible.
	if p.entry.ActionItem != p.open {
		t.Fatal("the calendar button should sit inside the field, not beside it")
	}
	if r := test.WidgetRenderer(p.entry); r == nil {
		t.Fatal("the entry should have a renderer to draw the action into")
	} else {
		found := false
		for _, o := range r.Objects() {
			if o == fyne.CanvasObject(p.open) {
				found = true
			}
			if c, ok := o.(*fyne.Container); ok {
				for _, in := range c.Objects {
					if in == fyne.CanvasObject(p.open) {
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatal("the calendar button is not among what the entry draws, so it cannot be seen")
		}
	}
	// A half-typed date is simply not taken; the field keeps the last whole one.
	p.entry.SetText("2026-08-1")
	if p.ISO() != "2026-08-11" {
		t.Fatalf("a half-typed date should not move the choice, got %q", p.ISO())
	}
	p.entry.SetText("2026-08-11")

	got := labels(p.grid)
	// Every worked day scores itself and says what it still owes — that is the
	// number the choice is actually made on.
	for _, want := range []string{
		"240/480", "(240m left)", // half a day
		"480/480", "✓", // finished
		"0/480", "+150m (330m left)", // drafts only
	} {
		if !contains(got, want) {
			t.Fatalf("the grid is missing %q: %v", want, got)
		}
	}

	// Moving month keeps the choice but changes what is on show.
	p.month = "2026-09"
	p.refresh()
	if p.ISO() != "2026-08-11" {
		t.Fatal("walking the months should not change the chosen day")
	}
	if contains(labels(p.grid), "240/480") {
		t.Fatalf("September should not be showing August's days: %v", labels(p.grid))
	}

	// Typing a whole date moves the choice; half of one does not.
	p.entry.SetText("2026-09-1")
	if p.ISO() != "2026-08-11" {
		t.Fatalf("a half-typed date is not a date, got %q", p.ISO())
	}
	fired := ""
	p.OnChanged = func(s string) { fired = s }
	p.entry.SetText("2026-09-15")
	if p.ISO() != "2026-09-15" || fired != "2026-09-15" {
		t.Fatalf("a whole typed date should move the choice, got %q (fired %q)", p.ISO(), fired)
	}

	// The button opens the month, and picking a day closes it again — that is
	// the whole of what it was opened for. The picker has to be on a canvas for
	// the overlay to have something to float over.
	fyne.CurrentApp().Driver().AllWindows()[0].SetContent(p)
	openBtn := p.open
	if openBtn == nil || openBtn.Icon.Name() != theme.CalendarIcon().Name() {
		t.Fatal("the field needs a calendar button to open the month")
	}
	openBtn.OnTapped()
	if !p.calendarOpen() {
		t.Fatal("tapping the calendar button should open the month")
	}
	openBtn.OnTapped()
	if p.calendarOpen() {
		t.Fatal("tapping it again should close the month")
	}

	// And a day picked out of it closes it too.
	openBtn.OnTapped()
	if !p.calendarOpen() {
		t.Fatal("expected the month open again")
	}
	// September is what is on show by now, since the typed date above moved it.
	tapDay(t, p, "2026-09-19")
	if p.calendarOpen() {
		t.Fatal("picking a day should close the month")
	}
	if p.ISO() != "2026-09-19" {
		t.Fatalf("picking a day should move the choice, got %q", p.ISO())
	}

	// Set moves the month along with the day.
	p.Set("2026-07-04")
	if p.ISO() != "2026-07-04" || p.month != "2026-07" {
		t.Fatalf("Set should carry the month with it: %q %q", p.ISO(), p.month)
	}
	// And an unreadable one is refused rather than stored.
	p.Set("not a date")
	if p.ISO() != "2026-07-04" {
		t.Fatalf("an unreadable date should not be taken, got %q", p.ISO())
	}
}

// What a day still owes, counting the drafts on it — those are hours already
// worked, so a day they carry over the line owes nothing more.
func TestDayOwedLine(t *testing.T) {
	for _, c := range []struct {
		mins, draft int
		want        string
	}{
		{240, 0, "(240m left)"},
		{0, 0, "(480m left)"},
		{480, 0, "✓"},
		{600, 0, "✓"}, // over target owes nothing
		{240, 150, "+150m (90m left)"},
		{300, 180, "+180m ✓"}, // the drafts carry it over
		{0, 150, "+150m (330m left)"},
	} {
		if got := dayOwedLine(c.mins, c.draft); got != c.want {
			t.Fatalf("dayOwedLine(%d, %d) = %q, want %q", c.mins, c.draft, got, c.want)
		}
	}
}

// tapDay clicks a day in the picker's grid. The cells are added in order after
// the weekday headings and the leading blanks, and only the days are tappable,
// so the nth tappable is the nth of the month.
func tapDay(t *testing.T, p *worklogDatePicker, iso string) {
	t.Helper()
	day, err := strconv.Atoi(iso[8:])
	if err != nil {
		t.Fatalf("bad date %q: %v", iso, err)
	}
	var cells []*tappable
	walk(p.grid, func(o fyne.CanvasObject) {
		if c, ok := o.(*tappable); ok {
			cells = append(cells, c)
		}
	})
	if day > len(cells) {
		t.Fatalf("the month has %d days, cannot tap the %dth", len(cells), day)
	}
	cells[day-1].Tapped(nil)
}

// buttonWithIcon finds the first button carrying a given icon, for controls
// whose buttons are icons rather than words.
func buttonWithIcon(o fyne.CanvasObject, icon fyne.Resource) *widget.Button {
	var found *widget.Button
	walk(o, func(c fyne.CanvasObject) {
		if b, ok := c.(*widget.Button); ok && found == nil &&
			b.Icon != nil && b.Icon.Name() == icon.Name() {
			found = b
		}
	})
	return found
}

func TestDateEntryAlwaysYieldsISO(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")

	e := newDateEntryISO("2026-08-06")
	w.SetContent(e) // force a renderer so the validator/OnChanged wiring exists
	fmt.Printf("seeded: visible text=%q  ISO=%q\n", e.Text, isoDate(e))
	if got := isoDate(e); got != "2026-08-06" {
		t.Fatalf("seeded date round-trip failed: %q", got)
	}

	// Picking from the calendar goes through SetDate.
	pick := time.Date(2026, 12, 31, 0, 0, 0, 0, time.Local)
	e.SetDate(&pick)
	fmt.Printf("picked: visible text=%q  ISO=%q\n", e.Text, isoDate(e))
	if got := isoDate(e); got != "2026-12-31" {
		t.Fatalf("picked date round-trip failed: %q", got)
	}

	// Cleared field must yield "" so the save path can refuse it.
	e.SetDate(nil)
	fmt.Printf("cleared: visible text=%q ISO=%q\n", e.Text, isoDate(e))
	if got := isoDate(e); got != "" {
		t.Fatalf("cleared date should be empty, got %q", got)
	}

	// A garbage seed must not crash or invent a date.
	bad := newDateEntryISO("not-a-date")
	fmt.Printf("bad seed: visible text=%q ISO=%q\n", bad.Text, isoDate(bad))
	if got := isoDate(bad); got != "" {
		t.Fatalf("bad seed produced a date: %q", got)
	}
}

// A picked date has to survive the CSV and still satisfy every consumer that
// assumes ISO: the month filter, the today() comparison, the lexicographic
// sort, and the value handed to `gh project item-edit --date`.
func TestPickedDateSurvivesStoreRoundTrip(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")

	e := newDateEntryISO("2026-08-06")
	w.SetContent(e)
	picked := isoDate(e)

	store := newStore(t.TempDir())
	if _, err := store.AppendRows([]Row{{
		"date": picked, "minutes": "480", "type": "commit", "refs": "05c9cae",
	}}); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ReadRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}
	got := rows[0]["date"]
	fmt.Printf("round-trip: widget=%q csv=%q\n", e.Text, got)

	if got != "2026-08-06" {
		t.Fatalf("date changed through the store: %q", got)
	}
	// The month report filters with a plain prefix match.
	if !strings.HasPrefix(got, "2026-08") {
		t.Fatalf("month filter would miss this row: %q", got)
	}
	// gh requires YYYY-MM-DD for --date; anything else is rejected upstream.
	if _, err := time.Parse("2006-01-02", got); err != nil {
		t.Fatalf("stored date is not the format gh accepts: %v", err)
	}
	// Recent entries sort dates as strings, which only works when ISO.
	if !("2026-08-06" < "2026-08-07") || !("2026-08-06" > "2026-07-31") {
		t.Fatal("lexicographic date ordering assumption broken")
	}
}
