package main

import (
	"image/color"
	"math"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
)

// Fyne's test driver completes animations immediately. Keep them pending here
// so intermediate frames and cancellation can be checked deterministically.
type motionTestDriver struct{ fyne.Driver }

func (*motionTestDriver) StartAnimation(*fyne.Animation) {}
func (*motionTestDriver) StopAnimation(*fyne.Animation)  {}

type motionTestApp struct {
	fyne.App
	driver fyne.Driver
}

func (a *motionTestApp) Driver() fyne.Driver { return a.driver }
func newMotionTestApp(t *testing.T) fyne.App {
	base := test.NewApp()
	a := &motionTestApp{App: base, driver: &motionTestDriver{base.Driver()}}
	fyne.SetCurrentApp(a)
	t.Cleanup(func() { fyne.SetCurrentApp(base); base.Quit() })
	return a
}

func TestCardFeedbackPreservesFillAndImmediateAction(t *testing.T) {
	a := newMotionTestApp(t)
	defer a.Quit()
	a.NewWindow("test")
	fill := orgPalette[0]
	bg := canvas.NewRectangle(fill)
	calls := 0
	card := newCardTappable(bg, func() { calls++ })
	card.Resize(fyne.NewSize(340, 152))
	card.MouseIn(nil)
	card.feedback.animation.Tick(1)
	card.Tapped(nil)
	if calls != 1 {
		t.Fatal("tap must run immediately, before feedback finishes")
	}
	if bg.FillColor != color.Color(fill) {
		t.Fatal("feedback changed the card colour")
	}
	card.Hide()
	if card.feedback.animation != nil || card.feedback.level != 0 {
		t.Fatal("hiding the card must cancel and clear feedback")
	}
	card.Show()
	card.MouseIn(nil)
	card.CreateRenderer().Destroy()
	if card.feedback.animation != nil {
		t.Fatal("destroyed renderer retained animation")
	}
}

func TestProgressMotionKeepsSharesAndColorsThroughInterruptedUpdates(t *testing.T) {
	a := newMotionTestApp(t)
	defer a.Quit()
	a.NewWindow("test")
	ui := &UI{cfg: Config{Repos: []string{"bigledger", "bigledger-support"}}}
	defer ui.finishProgress()
	bar := ui.progressBar("strip/Mon", "2026-09-07", map[string]int{"bigledger": 120}, nil, target, 8)
	bar.Resize(fyne.NewSize(480, 8))
	m := ui.meters["strip/Mon"]
	if m.animation != nil {
		t.Fatal("first display should be immediate")
	}
	ui.progressBar("strip/Mon", "2026-09-07", map[string]int{"bigledger": 120}, nil, target, 8)
	if m.animation != nil {
		t.Fatal("unchanged refresh should not animate")
	}
	ui.progressBar("strip/Mon", "2026-09-07", map[string]int{"bigledger": 240, "bigledger-support": 120}, map[string]int{"bigledger": 120}, target, 8)
	m.animation.Tick(0.5)
	check := func() {
		t.Helper()
		m.Layout(m.fill.Objects, fyne.NewSize(480, 8))
		end := float32(0)
		for i, o := range m.fill.Objects {
			if o.Size().Width < 0 || math.Abs(float64(o.Position().X-end)) > 0.01 {
				t.Fatal("segments overlap or leave gaps")
			}
			end += o.Size().Width
			want := color.Color(draftWash())
			if m.parts[i].key != "@draft" {
				want = orgColor(ui.cfg, m.parts[i].key)
			}
			if o.(*canvas.Rectangle).FillColor != want {
				t.Fatal("segment colour changed")
			}
		}
		if end > 480.01 {
			t.Fatalf("meter overflow: %g", end)
		}
	}
	check()
	ui.progressBar("strip/Mon", "2026-09-07", map[string]int{"bigledger-support": 240}, nil, target, 8)
	if m.parts[0].key != "bigledger" || m.parts[1].key != "bigledger-support" {
		t.Fatal("removing the leading segment must not jump the remaining segment to the left")
	}
	currentBlue := m.parts[0].weight
	ui.progressBar("strip/Mon", "2026-09-07", map[string]int{"bigledger": 600}, nil, target, 8)
	if m.parts[0].weight != currentBlue {
		t.Fatal("interrupted animation jumped to a previous target")
	}
	m.animation.Tick(0.5)
	check()
	m.animation.Tick(1)
	check()
	if m.animation != nil || len(m.parts) != 1 || m.parts[0].weight != 1 {
		t.Fatal("completion did not settle to the capped final reading")
	}
	ui.progressBar("strip/Mon", "2026-09-07", nil, nil, target, 8)
	m.animation.Tick(0.5)
	check()
	ui.finishProgress()
	if m.animation != nil || len(m.parts) != 0 {
		t.Fatal("empty reading did not settle")
	}
	ui.progressBar("strip/Mon", "2026-09-14", map[string]int{"bigledger": 60}, nil, target, 8)
	if len(ui.meters) != 1 || ui.meters["strip/Mon"].animation != nil {
		t.Fatal("navigation must reuse a bounded slot without animating unrelated dates")
	}
}

func TestProgressDoesNotAnimateHiddenTabs(t *testing.T) {
	a := newMotionTestApp(t)
	defer a.Quit()
	a.NewWindow("test")
	ui := &UI{cfg: Config{Repos: []string{"bigledger"}}}
	ui.tabs = newPillTabs(container.NewTabItem(statusTabName, canvas.NewRectangle(nil)))
	ui.progressBar("strip/Mon", "2026-09-07", nil, nil, target, 8)
	ui.progressBar("strip/Mon", "2026-09-07", map[string]int{"bigledger": 240}, nil, target, 8)
	m := ui.meters["strip/Mon"]
	if m.animation != nil || m.parts[0].weight != 0.5 {
		t.Fatal("hidden view should show final values without an animation")
	}
}
