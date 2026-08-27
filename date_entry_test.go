package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

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
