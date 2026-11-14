package tests

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// Log Work's kinds are tabs: the picked one bright with the gold line under it,
// which moves to whichever is picked next.
func TestUnderlineTabsPickAndMoveTheLine(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	var picked []string
	tabs := shell.NewUnderlineTabs([]string{"Commits", "Worklog", "Meeting"}, func(s string) { picked = append(picked, s) })
	w := a.NewWindow("t")
	w.SetContent(tabs)
	w.Resize(fyne.NewSize(600, 100))
	tabs.SetSelected("Commits")
	tabs.SetSelected("Commits")
	tabs.SetSelected("Nonsense")
	if len(picked) != 1 || tabs.Selected != "Commits" {
		t.Fatalf("only a real change should be reported: %v", picked)
	}

	objects := test.WidgetRenderer(tabs).Objects()
	line := objects[len(objects)-1].(*canvas.Rectangle)
	var items []fyne.CanvasObject
	for _, o := range objects[1 : len(objects)-1] {
		items = append(items, o)
	}
	under := func(item fyne.CanvasObject) bool {
		return line.Position().X >= item.Position().X && line.Position().X+line.Size().Width <= item.Position().X+item.Size().Width
	}
	if !under(items[0]) {
		t.Fatal("the line should sit under the picked tab")
	}
	test.Tap(items[2].(fyne.Tappable))
	if tabs.Selected != "Meeting" || picked[len(picked)-1] != "Meeting" || !under(items[2]) {
		t.Fatalf("tapping a tab should pick it and move the line: %q", tabs.Selected)
	}
}

func TestScaleFirstNumberKeepsTheFigureFormat(t *testing.T) {
	cases := []struct {
		text  string
		share float32
		want  string
	}{
		{"RM 1,234.50", 0.5, "RM 617.25"},
		{"RM 1,234.50", 1, "RM 1,234.50"},
		{"12.5h", 0, "0.0h"},
		{"14/21 days", 0.5, "7/21 days"},
		{"Not configured", 0.5, "Not configured"},
	}
	for _, c := range cases {
		if got := shell.ScaleFirstNumber(c.text, c.share); got != c.want {
			t.Errorf("%q at %v = %q, want %q", c.text, c.share, got, c.want)
		}
	}
}

func TestCountUpEndsOnTheFigure(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	figure := canvas.NewText("RM 2,400.00", nil)
	(&shell.UI{}).Arrival("report", "2026-08").CountUp([]*canvas.Text{figure})
	if figure.Text != "RM 2,400.00" {
		t.Fatalf("counting up should end on the figure, got %q", figure.Text)
	}
}

func TestArrivalPlaysOncePerThingShown(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := &shell.UI{}
	first := ui.Arrival("week", "2026-08-10")
	if first == nil {
		t.Fatal("a week arrives the first time it is shown")
	}
	// A redraw before the arrival has played joins it rather than dropping it.
	again := ui.Arrival("week", "2026-08-10")
	if again == nil {
		t.Fatal("a redraw mid-arrival should keep arriving")
	}
	again.Cascade(shell.ArriveDelays(3), func(int, float32) {})
	if ui.Arrival("week", "2026-08-10") != nil {
		t.Fatal("once played, the same week does not arrive again")
	}
	if ui.Arrival("week", "2026-08-17") == nil {
		t.Fatal("another week arrives")
	}
	ui.ReplayArrival("week")
	if ui.Arrival("week", "2026-08-17") == nil {
		t.Fatal("a replayed arrival plays again")
	}
	if d := shell.ArriveDelays(31); d[30] > 700*1e6 {
		t.Fatalf("a long cascade should be squeezed, last starts at %v", d[30])
	}
}

// A reveal a redraw replaced before it was ever shown must not resize what it
// held: a meter is kept from draw to draw and shown by the newer view.
func TestUnshownRevealLeavesItsContentAlone(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	bar := canvas.NewRectangle(nil)
	bar.Resize(fyne.NewSize(300, 12))
	r := shell.NewReveal(bar)
	r.SetShare(0.3)
	r.SetShare(1)
	if bar.Size() != fyne.NewSize(300, 12) {
		t.Fatalf("an unshown reveal resized its content to %v", bar.Size())
	}
}

// A folding reveal asks only for its share of the content's height, so a
// drop-down holding it opens and closes with it.
func TestFoldingRevealAsksForItsShare(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	content := widget.NewLabel("month")
	r := shell.NewReveal(content)
	r.Fold = true
	full := content.MinSize().Height
	r.SetShare(0.5)
	if got := r.MinSize().Height; got != full/2 {
		t.Fatalf("half folded should ask for half the height: %v of %v", got, full)
	}
	r.SetShare(1)
	if r.MinSize().Height != full {
		t.Fatal("unfolded should ask for the full height")
	}
}

// A week shown for the first time arrives day by day; redrawing the same week
// leaves its days be.
func TestWeekArrivesOnlyWhenItChanges(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	withProject(ui, "2026-08")
	grid := func() []fyne.CanvasObject {
		return ui.WeekBox.Objects[len(ui.WeekBox.Objects)-1].(*fyne.Container).Objects
	}
	logwork.DrawWeekStrip(ui)
	if _, ok := grid()[0].(*shell.Reveal); !ok {
		t.Fatalf("a new week should arrive: %T", grid()[0])
	}
	if r := grid()[0].(*shell.Reveal); r.Share() != 1 {
		t.Fatal("the arrival should end fully shown")
	}
	logwork.DrawWeekStrip(ui)
	if _, ok := grid()[0].(*shell.DayColumn); !ok {
		t.Fatalf("redrawing the same week should not replay it: %T", grid()[0])
	}
}

// The date picker reads planned time like Log Work's week: in purple under the
// day, and counted in what the day still owes.
func TestDatePickerShowsPlannedTime(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	ds := ui.Kanban.Week
	ui.Kanban.Plan.Tasks[shell.PlannerMeetingKey] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: ds, Start: 540, Minutes: 180}}}
	p := logwork.NewWorklogDatePicker(ui, ds)
	p.Refresh()
	got := labels(p.Grid)
	if !contains(got, "180m planned") || !contains(got, "+180m (300m left)") {
		t.Fatalf("the picker should show and count the plan: %v", got)
	}
}

func TestDatePickerArrivalFollowsWeekdayColumns(t *testing.T) {
	ui := statusUI(t)
	base := fyne.CurrentApp()
	driver := &cascadeFrameDriver{Driver: base.Driver()}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: driver})
	defer func() { fyne.SetCurrentApp(base); base.Quit() }()

	p := logwork.NewWorklogDatePicker(ui, "2026-08-01") // Saturday: five blank slots.
	ui.Win.SetContent(p)
	p.Open.OnTapped()
	p.Refresh() // Capture the day cascade after the popup's own unfolding.
	grid := p.Grid.Objects[0].(*fyne.Container)
	for _, blank := range grid.Objects[7:12] {
		if _, ok := blank.(*shell.Reveal); ok {
			t.Fatal("blank weekday slots must not consume day animation positions")
		}
	}
	if driver.animation == nil {
		t.Fatal("missing calendar arrival")
	}
	driver.animation.Tick(.2)
	share := func(day int) float32 {
		return grid.Objects[12+day-1].(*shell.Reveal).Share()
	}
	// The following week's Monday and Tuesday arrive before its Wednesday,
	// while the first Saturday has not started. A doubled offset breaks this.
	if !(share(3) > share(4) && share(4) > share(5) && share(1) == 0) {
		t.Fatalf("wrong column order: Mon=%v Tue=%v Wed=%v Sat=%v", share(3), share(4), share(5), share(1))
	}
	driver.animation.Tick(1)
	if p.ISO() != "2026-08-01" || share(31) != 1 {
		t.Fatal("calendar arrival must finish without changing the picked date")
	}
	p.Open.OnTapped()
}

// Switching kinds slides a picture of the old form out while the new one
// slides in, and leaves no picture behind.
func TestKindSwitchSlidesTheOldFormOut(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	ui.Win.Resize(fyne.NewSize(1400, 900))
	var morph *shell.Morph
	var tabs *shell.UnderlineTabs
	walk(ui.Win.Content(), func(o fyne.CanvasObject) {
		switch x := o.(type) {
		case *shell.Morph:
			morph = x
		case *shell.UnderlineTabs:
			tabs = x
		}
	})
	if morph == nil || tabs == nil {
		t.Fatal("the Log Work form or its tabs are missing")
	}
	morph.Snapshot()
	if n := len(test.WidgetRenderer(morph).Objects()); n != 2 {
		t.Fatalf("a snapshot should be drawn beside the form, got %d objects", n)
	}
	tabs.SetSelected("Meeting")
	if n := len(test.WidgetRenderer(morph).Objects()); n != 1 {
		t.Fatalf("the old form's picture should be gone once the slide ends, got %d objects", n)
	}
}

// The Sprint Calendar's days arrive when a period is first shown, and not on
// a redraw of the same one.
func TestSprintCalendarDaysArriveOnce(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	reveals := func() int {
		n := 0
		// The days only: the issue tray arrives on its own.
		walkCanvas(ui.Kanban.CalendarClip, func(o fyne.CanvasObject) {
			if _, ok := o.(*shell.Reveal); ok {
				n++
			}
		})
		return n
	}
	ui.ReplayArrival("planner")
	kanban.Draw(ui.Kanban)
	if reveals() != 7 {
		t.Fatalf("a week's seven days should arrive, got %d", reveals())
	}
	kanban.Draw(ui.Kanban)
	if reveals() != 0 {
		t.Fatal("redrawing the same week should not replay its arrival")
	}
	ui.Kanban.ProjectOff = map[string]bool{"p": true}
	kanban.Draw(ui.Kanban)
	if reveals() != 7 {
		t.Fatalf("a filter change should replay the calendar entrance, got %d reveals", reveals())
	}
}

// Kanban's loading strip counts its requests and shows the share done, as the
// other tabs' loads do.
func TestKanbanLoadShowsItsPercentage(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.Kanban
	k.Loading = true
	k.LoadProgress = backend.FetchProgress{Done: 1, Total: 4}
	k.SyncActivity()
	if got := labels(k.Activity.View); !contains(got, "25%") {
		t.Fatalf("the Kanban load should show its percentage: %v", got)
	}
	k.Loading = false
	k.SyncActivity()
}

// The calendar's small Delete fades in and out, and is gone once faded out.
func TestSmallDeleteFadesInAndOut(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	b := widget.NewButton("Delete", nil)
	d := shell.SmallDeleteButton(b, b)
	changes := 0
	d.OnChange = func() { changes++ }
	if d.View.Visible() || d.Shown() {
		t.Fatal("the Delete should start hidden")
	}
	d.FadeTo(true)
	if !d.View.Visible() || !b.Visible() {
		t.Fatal("fading in should show it")
	}
	d.FadeTo(false)
	if d.Shown() {
		t.Fatal("a Delete on its way out should not count as shown")
	}
	// It lingers a moment, then fades: wait for it to be gone.
	for deadline := time.Now().Add(2 * time.Second); d.View.Visible() && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if d.View.Visible() || b.Visible() || changes != 2 {
		t.Fatalf("fading out should hide it once done, and say so each time: %d changes", changes)
	}
}

// The pending tiles arrive one after another when the list is first shown, and
// not again when it is redrawn unchanged.
func TestPendingBubblesArriveOnce(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()
	ui.PendingLoaded = true
	ui.Pending = backend.PendingResult{Count: 2, Range: []string{"2026-08-10", "2026-08-12"}, Groups: []backend.Group{
		{Date: "2026-08-10", Issue: "exampleorg/app#1", Commits: []backend.Commit{{Sha: "a", Repo: "exampleorg/app", Message: "one"}}},
		{Date: "2026-08-11", Issue: "exampleorg/app#2", Commits: []backend.Commit{{Sha: "b", Repo: "exampleorg/app", Message: "two"}}},
	}}
	count := func() int {
		n := 0
		walk(ui.PendingBox, func(o fyne.CanvasObject) {
			if _, ok := o.(*shell.Reveal); ok {
				n++
			}
		})
		return n
	}
	logwork.RenderPending(ui)
	if count() != 2 {
		t.Fatalf("both tiles should arrive, got %d", count())
	}
	logwork.RenderPending(ui)
	if count() != 0 {
		t.Fatal("redrawing the same list should not replay its arrival")
	}
}

// Coming back to Commits in Log Work's tabs brings the pending bubbles in again.
func TestCommitsTabReplaysThePendingArrival(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()
	ui.PendingLoaded = true
	ui.Pending = backend.PendingResult{Count: 1, Range: []string{"2026-08-10", "2026-08-12"}, Groups: []backend.Group{
		{Date: "2026-08-10", Issue: "exampleorg/app#1", Commits: []backend.Commit{{Sha: "a", Repo: "exampleorg/app", Message: "one"}}},
	}}
	logwork.RenderPending(ui)
	var tabs *shell.UnderlineTabs
	walk(ui.Win.Content(), func(o fyne.CanvasObject) {
		if x, ok := o.(*shell.UnderlineTabs); ok {
			tabs = x
		}
	})
	tabs.SetSelected("Worklog")
	tabs.SetSelected("Commits")
	arrived := false
	walk(ui.PendingBox, func(o fyne.CanvasObject) {
		if _, ok := o.(*shell.Reveal); ok {
			arrived = true
		}
	})
	if !arrived {
		t.Fatal("switching back to Commits should replay the bubbles' arrival")
	}
}

// A week drawn while its month is still being refreshed holds its arrival back,
// and plays it on the draw that comes when the fetch lands.
func TestArrivalWaitsForTheFetchToLand(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	withProject(ui, "2026-08")
	ui.ReplayArrival("week")
	key := shell.StatusCacheKey("2026-08")
	first := func() fyne.CanvasObject {
		return ui.WeekBox.Objects[len(ui.WeekBox.Objects)-1].(*fyne.Container).Objects[0]
	}
	ui.ProjLoading[key] = true
	logwork.DrawWeekStrip(ui)
	if _, ok := first().(*shell.Reveal); ok {
		t.Fatal("a week still being fetched should not arrive yet")
	}
	ui.ProjLoading[key] = false
	logwork.DrawWeekStrip(ui)
	if _, ok := first().(*shell.Reveal); !ok {
		t.Fatalf("the week should arrive once the fetch lands: %T", first())
	}
}

// Completed columns must not rebuild their meter on every remaining cascade frame.
func TestCompletedMeterShareRetainsSegments(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := &shell.UI{}
	bar := ui.ProgressBarParts("strip/Mon", "day", []shell.MeterPart{{Key: "work", Weight: .5}}, 8).(*fyne.Container)
	fill := bar.Objects[1].(*fyne.Container)
	ui.MeterShare("strip/Mon", .5)
	ui.MeterShare("strip/Mon", 1)
	segment := fill.Objects[0]
	for i := 0; i < 30; i++ {
		ui.MeterShare("strip/Mon", 1)
	}
	if fill.Objects[0] != segment {
		t.Fatal("finished meter recreated its segment")
	}
	bar.Resize(fyne.NewSize(200, 8))
	if fill.Objects[0].Size().Width != 100 {
		t.Fatalf("final width = %v", fill.Objects[0].Size().Width)
	}
}

// Charts flush each animation frame from the last piece's callback, even
// while its delayed share is still zero.
type cascadeFrameDriver struct {
	fyne.Driver
	animation *fyne.Animation
}

func (d *cascadeFrameDriver) StartAnimation(a *fyne.Animation) { d.animation = a }
func (d *cascadeFrameDriver) StopAnimation(*fyne.Animation)    {}
func TestCascadeFlushesFramesBeforeLastBarStarts(t *testing.T) {
	base := test.NewApp()
	driver := &cascadeFrameDriver{Driver: base.Driver()}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: driver})
	defer func() { fyne.SetCurrentApp(base); base.Quit() }()
	var first, last float32
	flushes := 0
	(&shell.UI{}).Arrival("report", "period").Cascade([]time.Duration{0, 650 * time.Millisecond}, func(i int, share float32) {
		if i == 0 {
			first = share
		} else {
			last = share
			flushes++
		}
	})
	flushes = 0
	driver.animation.Tick(.1)
	before := first
	driver.animation.Tick(.2)
	if flushes != 2 || last != 0 || first <= before {
		t.Fatalf("frames=%d first=%v before=%v last=%v", flushes, first, before, last)
	}
	driver.animation.Tick(1)
	if first != 1 || last != 1 {
		t.Fatal("cascade did not finish")
	}
}
