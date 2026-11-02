package tests

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
)

// waitForButton polls the window's popups for a button with the given label.
func waitForButton(t *testing.T, w fyne.Window, label string) *widget.Button {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, o := range w.Canvas().Overlays().List() {
			if b := deepButton(o, label); b != nil {
				return b
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no %q button appeared", label)
	return nil
}

// deepButton finds a button by label anywhere under o, inside widgets such
// as dialogs too.
func deepButton(o fyne.CanvasObject, label string) *widget.Button {
	if b, ok := o.(*widget.Button); ok && b.Text == label {
		return b
	}
	var kids []fyne.CanvasObject
	switch x := o.(type) {
	case *fyne.Container:
		kids = x.Objects
	case fyne.Widget:
		kids = test.WidgetRenderer(x).Objects()
	}
	for _, k := range kids {
		if b := deepButton(k, label); b != nil {
			return b
		}
	}
	return nil
}

// A push that fails says so in a popup with a button to push the same entry
// again, so a retry does not mean hunting for the entry on the week.
func TestFailedPushPopupOffersPushAgain(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	if _, err := ui.Store.AppendRows([]backend.Row{{
		"date": "2026-08-10", "minutes": "60", "issue": "exampleorg/app#1",
		"mode": "issue", "remarks": "Fix the export", "owner": "team-alex",
	}}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	old := backend.PushEntry
	backend.PushEntry = func(backend.Config, string, string, string, int, string, string, string, string, string, ...func(backend.FetchProgress)) (backend.PushResult, error) {
		if calls.Add(1) == 1 {
			return backend.PushResult{}, errors.New("GitHub is down")
		}
		return backend.PushResult{URL: "https://github.com/exampleorg/app/issues/2"}, nil
	}
	t.Cleanup(func() { backend.PushEntry = old })

	logwork.DrawRecent(ui)
	push := buttonNamed(ui.WeekBox, "Push")
	if push == nil {
		t.Fatalf("saved entry has no push button: %v", labels(ui.WeekBox))
	}
	test.Tap(push)
	again := waitForButton(t, ui.Win, "Push again")
	test.Tap(again)
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if calls.Load() != 2 {
		t.Fatalf("Push again should push the entry a second time, got %d pushes", calls.Load())
	}
}
