package tests

import (
	"image/color"
	"math"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/test"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// Fyne's test driver completes animations immediately. Keep them pending here
// so intermediate frames and cancellation can be checked deterministically.
type motionTestDriver struct{ fyne.Driver }

func (*motionTestDriver) StartAnimation(*fyne.Animation) {}

func (*motionTestDriver) StopAnimation(*fyne.Animation) {}

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
	fill := shell.OrgPalette[0]
	bg := canvas.NewRectangle(fill)
	calls := 0
	card := shell.NewCardTappable(bg, func() { calls++ })
	card.Resize(fyne.NewSize(340, 152))
	card.MouseIn(nil)
	if card.Feedback.Animation.Duration != 180*time.Millisecond {
		t.Fatal("ordinary issue card feedback should keep its original 180ms speed")
	}
	card.Feedback.Animation.Tick(1)
	card.Tapped(nil)
	if calls != 1 {
		t.Fatal("tap must run immediately, before feedback finishes")
	}
	if bg.FillColor != color.Color(fill) {
		t.Fatal("feedback changed the card colour")
	}
	card.Hide()
	if card.Feedback.Animation != nil || card.Feedback.Level != 0 {
		t.Fatal("hiding the card must cancel and clear feedback")
	}
	card.Show()
	card.MouseIn(nil)
	card.CreateRenderer().Destroy()
	if card.Feedback.Animation != nil {
		t.Fatal("destroyed renderer retained animation")
	}
}

func TestIssueBubbleHoverKeepsOriginalSpeedWhileFiltersStaySlower(t *testing.T) {
	base := test.NewApp()
	shell.ReleaseBubble()
	d := &collectDriver{Driver: base.Driver()}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: d})
	defer func() { fyne.SetCurrentApp(base); base.Quit() }()
	issue := shell.HoverBubble(shell.OrgPalette[0], canvas.NewText("Issue", shell.OrgPalette[0]))
	issue.MouseIn(nil)
	if len(d.anims) != 1 || d.anims[0].Duration != 180*time.Millisecond {
		t.Fatal("shared worklog, Kanban, meeting, list and calendar bubbles must return to 180ms")
	}
	filter := shell.NewFilterTappable(canvas.NewText("Org", shell.OrgPalette[0]), nil)
	filter.MouseIn(nil)
	if filter.Feedback.Animation.Duration != 600*time.Millisecond {
		t.Fatal("org filters must retain their separate 600ms motion")
	}
}

func TestProgressMotionKeepsSharesAndColorsThroughInterruptedUpdates(t *testing.T) {
	a := newMotionTestApp(t)
	defer a.Quit()
	a.NewWindow("test")
	ui := &shell.UI{Cfg: backend.Config{Repos: []string{"exampleorg", "example-support"}}}
	defer ui.FinishProgress()
	bar := ui.ProgressBar("strip/Mon", "2026-09-07", map[string]int{"exampleorg": 120}, nil, backend.Target, 8)
	bar.Resize(fyne.NewSize(480, 8))
	m := ui.Meters["strip/Mon"]
	if m.Animation != nil {
		t.Fatal("first display should be immediate")
	}
	ui.ProgressBar("strip/Mon", "2026-09-07", map[string]int{"exampleorg": 120}, nil, backend.Target, 8)
	if m.Animation != nil {
		t.Fatal("unchanged refresh should not animate")
	}
	ui.ProgressBar("strip/Mon", "2026-09-07", map[string]int{"exampleorg": 240, "example-support": 120}, map[string]int{"exampleorg": 120}, backend.Target, 8)
	m.Animation.Tick(0.5)
	check := func() {
		t.Helper()
		m.Layout(m.Fill.Objects, fyne.NewSize(480, 8))
		end := float32(0)
		for i, o := range m.Fill.Objects {
			if o.Size().Width < 0 || math.Abs(float64(o.Position().X-end)) > 0.01 {
				t.Fatal("segments overlap or leave gaps")
			}
			end += o.Size().Width
			want := color.Color(shell.DraftWash())
			if m.Parts[i].Key != "@draft" {
				want = shell.OrgColor(ui.Cfg, m.Parts[i].Key)
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
	ui.ProgressBar("strip/Mon", "2026-09-07", map[string]int{"example-support": 240}, nil, backend.Target, 8)
	if m.Parts[0].Key != "exampleorg" || m.Parts[1].Key != "example-support" {
		t.Fatal("removing the leading segment must not jump the remaining segment to the left")
	}
	currentBlue := m.Parts[0].Weight
	ui.ProgressBar("strip/Mon", "2026-09-07", map[string]int{"exampleorg": 600}, nil, backend.Target, 8)
	if m.Parts[0].Weight != currentBlue {
		t.Fatal("interrupted animation jumped to a previous target")
	}
	m.Animation.Tick(0.5)
	check()
	m.Animation.Tick(1)
	check()
	if m.Animation != nil || len(m.Parts) != 1 || m.Parts[0].Weight != 1 {
		t.Fatal("completion did not settle to the capped final reading")
	}
	ui.ProgressBar("strip/Mon", "2026-09-07", nil, nil, backend.Target, 8)
	m.Animation.Tick(0.5)
	check()
	ui.FinishProgress()
	if m.Animation != nil || len(m.Parts) != 0 {
		t.Fatal("empty reading did not settle")
	}
	ui.ProgressBar("strip/Mon", "2026-09-14", map[string]int{"exampleorg": 60}, nil, backend.Target, 8)
	if len(ui.Meters) != 1 || ui.Meters["strip/Mon"].Animation != nil {
		t.Fatal("navigation must reuse a bounded slot without animating unrelated dates")
	}
}

func TestProgressDoesNotAnimateHiddenTabs(t *testing.T) {
	a := newMotionTestApp(t)
	defer a.Quit()
	a.NewWindow("test")
	ui := &shell.UI{Cfg: backend.Config{Repos: []string{"exampleorg"}}}
	ui.Tabs = shell.NewPillTabs(container.NewTabItem(shell.StatusTabName, canvas.NewRectangle(nil)))
	ui.ProgressBar("strip/Mon", "2026-09-07", nil, nil, backend.Target, 8)
	ui.ProgressBar("strip/Mon", "2026-09-07", map[string]int{"exampleorg": 240}, nil, backend.Target, 8)
	m := ui.Meters["strip/Mon"]
	if m.Animation != nil || m.Parts[0].Weight != 0.5 {
		t.Fatal("hidden view should show final values without an animation")
	}
}

func TestUnderlineSurvivesFinishedSlideAndRendererRebuild(t *testing.T) {
	a := newMotionTestApp(t).(*motionTestApp)
	driver := &cascadeFrameDriver{Driver: a.driver}
	a.driver = driver
	tabs := shell.NewUnderlineTabs([]string{"Commits", "Worklog"}, nil)
	tabs.Resize(fyne.NewSize(500, 70))
	tabs.SetSelected("Commits")
	test.WidgetRenderer(tabs).Layout(tabs.Size())
	tabs.SetSelected("Worklog")
	if driver.animation == nil {
		t.Fatal("selection should animate")
	}
	driver.animation.Tick(1)
	tabs.Resize(fyne.NewSize(500, 90))
	renderer := test.WidgetRenderer(tabs)
	objects := renderer.Objects()
	line := objects[len(objects)-1].(*canvas.Rectangle)
	if line.Size().Width <= 0 || line.Position().Y != 87 {
		t.Fatalf("lost underline after layout: %v %v", line.Position(), line.Size())
	}
	tabs.SetSelected("Commits")
	renderer.Destroy()
	renderer = tabs.CreateRenderer()
	renderer.Layout(tabs.Size())
	objects = renderer.Objects()
	line = objects[len(objects)-1].(*canvas.Rectangle)
	if line.Size().Width <= 0 || line.Position().Y != 87 {
		t.Fatal("lost underline on renderer rebuild")
	}
}

func TestDropdownOutsideClickFoldsAndReopeningCancelsOldClose(t *testing.T) {
	a := newMotionTestApp(t).(*motionTestApp)
	driver := &cascadeFrameDriver{Driver: a.driver}
	a.driver = driver
	defer shell.FinishDropdownClosings()
	w := a.NewWindow("dropdown")
	selectBox := shell.NewGoldSelect([]string{"One", "Two"}, nil)
	w.SetContent(selectBox)
	w.Resize(fyne.NewSize(400, 300))
	selectBox.Tapped(nil)
	opening := driver.animation
	if opening == nil {
		t.Fatal("dropdown did not animate")
	}
	opening.Tick(1)
	old := selectBox.Popup
	full := old.Size().Height
	w.Canvas().Overlays().Top().(fyne.Tappable).Tapped(nil)
	closing := driver.animation
	if selectBox.Popup != nil || closing == opening {
		t.Fatal("outside click skipped closing motion")
	}
	closing.Tick(.5)
	if !old.Visible() || old.Size().Height >= full {
		t.Fatal("dropdown should fold before disappearing")
	}
	selectBox.Tapped(nil)
	current := selectBox.Popup
	if current == nil || !current.Visible() || old.Visible() {
		t.Fatal("reopen must retire old popup")
	}
	closing.Tick(1)
	if !current.Visible() || w.Canvas().Overlays().Top() == nil {
		t.Fatal("stale closing animation hid the new dropdown")
	}
	selectBox.CloseChoices()
	driver.animation.Tick(1)
}

// A drop-down with no room below its field rises above it instead, growing
// up from the field's top edge.
func TestDropdownOpensUpwardNearTheBottom(t *testing.T) {
	a := newMotionTestApp(t).(*motionTestApp)
	driver := &cascadeFrameDriver{Driver: a.driver}
	a.driver = driver
	defer shell.FinishDropdownClosings()
	w := a.NewWindow("dropdown")
	options := []string{"One", "Two", "Three", "Four", "Five", "Six"}
	selectBox := shell.NewGoldSelect(options, nil)
	selectBox.Searchable = true
	w.SetContent(container.NewBorder(nil, selectBox, nil, nil, layout.NewSpacer()))
	w.Resize(fyne.NewSize(400, 400))
	selectBox.Tapped(nil)
	field := fyne.CurrentApp().Driver().AbsolutePositionForObject(selectBox)
	for _, step := range []float32{.5, 1} {
		driver.animation.Tick(step)
		popup := selectBox.Popup
		if bottom := popup.Position().Y + popup.Size().Height; bottom > field.Y+0.5 || bottom < field.Y-0.5 {
			t.Fatalf("at %v the drop-down should end at the field's top %v, not %v", step, field.Y, bottom)
		}
	}
	selectBox.CloseChoices()
	driver.animation.Tick(1)
}
