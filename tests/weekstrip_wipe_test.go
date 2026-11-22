package tests

import (
	"reflect"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// Hold animation frames, while observing delayed starts across the timer/UI
// boundary without sharing an unsynchronised slice with the timer goroutine.
type wipeStartDriver struct {
	fyne.Driver
	starts chan *fyne.Animation
}

func (d *wipeStartDriver) StartAnimation(a *fyne.Animation) { d.starts <- a }
func (d *wipeStartDriver) StopAnimation(*fyne.Animation)    {}

type wipePaintCanvas struct {
	fyne.Canvas
	target           fyne.CanvasObject
	refreshes, dirty int
}

func (c *wipePaintCanvas) Refresh(o fyne.CanvasObject) {
	if o == c.target {
		c.refreshes++
	}
	c.Canvas.Refresh(o)
}
func (c *wipePaintCanvas) SetDirty() { c.dirty++ }

type wipePaintDriver struct {
	fyne.Driver
	canvas    *wipePaintCanvas
	animation *fyne.Animation
}

func (d *wipePaintDriver) CanvasForObject(fyne.CanvasObject) fyne.Canvas { return d.canvas }
func (d *wipePaintDriver) StartAnimation(a *fyne.Animation)              { d.animation = a }
func (d *wipePaintDriver) StopAnimation(*fyne.Animation)                 {}

func TestWipeFramesOnlyRepaintMovingClip(t *testing.T) {
	base := test.NewApp()
	w := base.NewWindow("wipe")
	paint := &wipePaintCanvas{Canvas: w.Canvas()}
	driver := &wipePaintDriver{Driver: base.Driver(), canvas: paint}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: driver})
	defer func() { fyne.SetCurrentApp(base); base.Quit() }()
	wipe := shell.NewWipeFor(canvas.NewRectangle(nil), 0, 520*time.Millisecond)
	defer wipe.Stop()
	w.SetContent(wipe)
	w.Resize(fyne.NewSize(200, 100))
	paint.target = wipe
	paint.refreshes, paint.dirty = 0, 0
	for _, share := range []float32{.2, .4, .6, .8} {
		driver.animation.Tick(share)
	}
	if paint.refreshes != 0 {
		t.Fatalf("four clip frames invalidated the full card %d times", paint.refreshes)
	}
	if paint.dirty == 0 {
		t.Fatal("the moving clip was not repainted")
	}
	clip := test.WidgetRenderer(wipe).Objects()[0]
	if clip.Size().Height <= 0 || clip.Size().Height >= wipe.Size().Height {
		t.Fatal("repaint did not preserve the partially revealed card")
	}
}

// Earlier tests can still have delayed animations arriving on Fyne's current
// driver. Only the exact wipe under test counts towards its regression check.
func wipeAnimationID(w *shell.Wipe) uintptr {
	return reflect.ValueOf(w).Elem().FieldByName("animation").Pointer()
}

func TestWipeStopCancelsItsDelayedStart(t *testing.T) {
	base := test.NewApp()
	d := &wipeStartDriver{Driver: base.Driver(), starts: make(chan *fyne.Animation, 32)}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: d})
	defer func() { fyne.SetCurrentApp(base); base.Quit() }()
	wipe := shell.NewWipeFor(canvas.NewRectangle(nil), 20*time.Millisecond, 520*time.Millisecond)
	want := wipeAnimationID(wipe)
	wipe.Stop()
	deadline := time.NewTimer(60 * time.Millisecond)
	defer deadline.Stop()
	for {
		select {
		case anim := <-d.starts:
			if reflect.ValueOf(anim).Pointer() == want {
				t.Fatal("discarded wipe started after being stopped")
			}
		case <-deadline.C:
			return
		}
	}
}

func TestWipeStopIgnoresAFrameAlreadyQueued(t *testing.T) {
	base := test.NewApp()
	defer base.Quit()
	driver := collectMotion(t)
	w := base.NewWindow("wipe")
	wipe := shell.NewWipeFor(canvas.NewRectangle(nil), 0, 520*time.Millisecond)
	w.SetContent(wipe)
	w.Resize(fyne.NewSize(200, 100))
	var anim *fyne.Animation
	for _, candidate := range driver.anims {
		if reflect.ValueOf(candidate).Pointer() == wipeAnimationID(wipe) {
			anim = candidate
		}
	}
	if anim == nil {
		t.Fatal("wipe animation missing")
	}
	anim.Tick(.4)
	clip := test.WidgetRenderer(wipe).Objects()[0]
	before := clip.Size()
	wipe.Stop()
	anim.Tick(1)
	if clip.Size() != before {
		t.Fatal("a discarded wipe still changed its clipping after Stop")
	}
}

func TestWeekRedrawCancelsDiscardedCardWipes(t *testing.T) {
	ui := weekUI(t)
	base := fyne.CurrentApp()
	withProject(ui, "2026-08")
	if _, err := ui.Store.AppendRows([]backend.Row{{"date": "2026-08-10", "minutes": "30", "issue": "exampleorg/app#42"}}); err != nil {
		t.Fatal(err)
	}
	d := &wipeStartDriver{Driver: base.Driver(), starts: make(chan *fyne.Animation, 256)}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: d})
	defer func() {
		walk(ui.WeekBox, func(o fyne.CanvasObject) {
			if wipe, ok := o.(*shell.Wipe); ok {
				wipe.Stop()
			}
		})
		fyne.SetCurrentApp(base)
		base.Quit()
	}()
	logwork.DrawWeekStrip(ui)
	oldIDs := map[uintptr]bool{}
	walk(ui.WeekBox, func(o fyne.CanvasObject) {
		if wipe, ok := o.(*shell.Wipe); ok {
			oldIDs[wipeAnimationID(wipe)] = true
		}
	})
	logwork.DrawWeekStrip(ui) // issue titles or board data arrives during startup
	currentIDs := map[uintptr]bool{}
	walk(ui.WeekBox, func(o fyne.CanvasObject) {
		if wipe, ok := o.(*shell.Wipe); ok {
			currentIDs[wipeAnimationID(wipe)] = true
		}
	})
	started := func(anim *fyne.Animation) bool {
		id := reflect.ValueOf(anim).Pointer()
		if oldIDs[id] {
			t.Fatal("a discarded card's wipe started after the redraw")
		}
		return currentIDs[id]
	}
	count := 0
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for count == 0 {
		select {
		case anim := <-d.starts:
			if started(anim) {
				count++
			}
		case <-deadline.C:
			t.Fatal("the current card's wipe never started")
		}
	}
	// Both draws schedule the same day/card delay. If the obsolete timer was
	// left running, it starts alongside the replacement's animation.
	settle := time.NewTimer(150 * time.Millisecond)
	defer settle.Stop()
	for {
		select {
		case anim := <-d.starts:
			if started(anim) {
				count++
			}
		case <-settle.C:
			if count != 1 {
				t.Fatalf("one visible card started %d wipes after the redraw", count)
			}
			return
		}
	}
}
