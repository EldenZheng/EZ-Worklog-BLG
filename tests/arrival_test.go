package tests

import (
	"testing"

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
	ui, w, _ := plannerFixture(t)
	reveals := func() int {
		n := 0
		walkCanvas(w.Content(), func(o fyne.CanvasObject) {
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
