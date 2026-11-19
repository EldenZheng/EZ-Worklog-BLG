package projects

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"worklog/internal/shared/widgets"
)

func TestSprintRangeReadsFirstToLastDay(t *testing.T) {
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.Local)
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.Local) }
	for _, c := range []struct {
		b    Bucket
		want string
	}{
		{Bucket{Start: day(2026, 11, 3), Days: 12}, "3 – 14 Nov"},
		{Bucket{Start: day(2026, 10, 28), Days: 12}, "28 Oct – 8 Nov"},
		{Bucket{Start: day(2026, 12, 28), Days: 14}, "28 Dec 2026 – 10 Jan 2027"},
		{Bucket{Start: day(2026, 11, 3), Days: 1}, "3 Nov"},
		{Bucket{}, ""},
	} {
		if got := c.b.Range(now); got != c.want {
			t.Errorf("Range(%v, %d) = %q, want %q", c.b.Start, c.b.Days, got, c.want)
		}
	}
}

func TestSprintBucketsKeepTheirDates(t *testing.T) {
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.Local)
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, time.Local)
	list := sprintBuckets([]Item{{Sprint: "Sprint 35", SprintStart: start, SprintDays: 14}}, now)
	if len(list) != 1 || !list[0].Start.Equal(start) || list[0].Days != 14 || list[0].Range(now) != "9 – 22 Nov" {
		t.Fatalf("sprint bucket lost its dates: %+v", list)
	}
}

// Under the pointer a column with a tip shows a box naming it and its line;
// off the chart the box goes.
func TestColumnTipShowsUnderThePointer(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	spec := ColumnSpec{
		Stacks: [][]Segment{{{Value: 1}}, {{Value: 2}}},
		Labels: []string{"35", "36"},
		Marked: -1,
		Tips:   []ColumnTip{{"Sprint 35", "3 – 14 Nov"}, {"Sprint 36", "17 – 28 Nov"}},
	}
	c := NewColumnChart(spec, currentPalette(), nil)
	w := test.NewWindow(c)
	defer w.Close()
	w.Resize(fyne.NewSize(400, 240))
	c.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(300, 100)}})
	l := c.layout
	if !l.tipBg.Visible() || l.tipTitle.Text != "Sprint 36" || l.tipLine.Text != "17 – 28 Nov" {
		t.Fatalf("tip should show the second sprint, got visible=%v %q %q", l.tipBg.Visible(), l.tipTitle.Text, l.tipLine.Text)
	}
	c.MouseOut()
	if l.tipBg.Visible() || l.tipTitle.Visible() {
		t.Fatal("tip should hide when the pointer leaves")
	}
}

// pendingDriver keeps animations waiting, so their frames can be stepped.
type pendingDriver struct{ fyne.Driver }

func (*pendingDriver) StartAnimation(*fyne.Animation) {}
func (*pendingDriver) StopAnimation(*fyne.Animation)  {}

type pendingApp struct {
	fyne.App
	driver fyne.Driver
}

func (a *pendingApp) Driver() fyne.Driver { return a.driver }

// The box moves as the gold hover does: it fades in, slides from one column
// to the next, and fades out where it stood.
func TestColumnTipSlidesAndFadesLikeTheGoldHover(t *testing.T) {
	base := test.NewApp()
	fyne.SetCurrentApp(&pendingApp{App: base, driver: &pendingDriver{base.Driver()}})
	defer func() { fyne.SetCurrentApp(base); base.Quit() }()
	spec := ColumnSpec{
		Stacks: [][]Segment{{{Value: 1}}, {{Value: 2}}, {{Value: 1}}},
		Labels: []string{"35", "36", "37"},
		Marked: -1,
		Tips:   []ColumnTip{{"Sprint 35", "a"}, {"Sprint 36", "b"}, {"Sprint 37 with a much longer name", "c"}},
	}
	c := NewColumnChart(spec, currentPalette(), nil)
	c.Resize(fyne.NewSize(900, 240))
	c.Content.Resize(fyne.NewSize(900, 240))
	l := c.layout
	move := func(x float32) {
		c.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(x, 100)}})
	}

	move(50)
	if l.tipAnim == nil || l.tipAnim.Duration != widgets.HoverDuration {
		t.Fatal("the box should arrive on the gold hover's timing")
	}
	l.tipAnim.Tick(0.5)
	if l.tipShown <= 0 || l.tipShown >= 1 {
		t.Fatalf("the box should be fading in, at %g", l.tipShown)
	}
	first := l.tipAt.X // fades in where it stands, no slide
	l.tipAnim.Tick(1)
	if l.tipAt.X != first {
		t.Fatal("the first box should fade in in place")
	}

	move(650)
	inside := func() {
		t.Helper()
		if w := l.tipTitle.MinSize().Width; w > l.tipBox.Width-tipPad*2+0.5 {
			t.Fatalf("the text (%g wide) runs out of its %g wide box", w, l.tipBox.Width)
		}
	}
	l.tipAnim.Tick(0.25)
	if l.tipCur.Title != "Sprint 35" || l.tipInkShown <= 0 || l.tipInkShown >= 1 {
		t.Fatalf("the old text should be fading out first, got %q at %g", l.tipCur.Title, l.tipInkShown)
	}
	inside()
	l.tipAnim.Tick(0.5)
	mid := l.tipAt.X
	l.tipAnim.Tick(0.75)
	if l.tipCur.Title != "Sprint 37 with a much longer name" || l.tipInkShown <= 0 || l.tipInkShown >= 1 {
		t.Fatalf("the new text should be fading in after, got %q at %g", l.tipCur.Title, l.tipInkShown)
	}
	inside()
	l.tipAnim.Tick(1)
	if l.tipInkShown != 1 || l.tipTitle.Text != "Sprint 37 with a much longer name" {
		t.Fatalf("the new text should end fully shown, got %q at %g", l.tipTitle.Text, l.tipInkShown)
	}
	if mid <= first || mid >= l.tipAt.X {
		t.Fatalf("the box should slide between columns: %g, %g, %g", first, mid, l.tipAt.X)
	}

	stood := l.tipAt
	c.MouseOut()
	l.tipAnim.Tick(0.5)
	if l.tipShown <= 0 || l.tipShown >= 1 || l.tipAt != stood || !l.tipBg.Visible() {
		t.Fatalf("leaving should fade the box where it stood, at %g", l.tipShown)
	}
	l.tipAnim.Tick(1)
	if l.tipBg.Visible() {
		t.Fatal("the box should be gone once faded")
	}
}

// The burn-up's guide, dots and box move as the gold hover does: fading in,
// sliding from week to week with the text crossfading, and fading out where
// they stood.
func TestBurnUpHoverSlidesAndFades(t *testing.T) {
	base := test.NewApp()
	fyne.SetCurrentApp(&pendingApp{App: base, driver: &pendingDriver{base.Driver()}})
	defer func() { fyne.SetCurrentApp(base); base.Quit() }()
	start := time.Date(2026, 10, 5, 0, 0, 0, 0, time.Local)
	var weeks []WeekPoint
	for i := range 6 {
		weeks = append(weeks, WeekPoint{Start: start.AddDate(0, 0, 7*i), Scope: 10 + i, Done: i * 2, Closed: 2, Opened: 1})
	}
	c := BurnUp(weeks, 0, currentPalette(), nil).(*burnUpChart)
	c.Resize(fyne.NewSize(900, 260))
	c.box.Resize(fyne.NewSize(900, 260))
	l := c.layout
	l.share = 1
	l.Layout(c.box.Objects, c.box.Size())
	step := l.plotWidth / float32(l.span)
	move := func(week int) {
		c.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(l.left+step*float32(week), 100)}})
	}

	move(1)
	if l.hvAnim == nil || l.hvAnim.Duration != widgets.HoverDuration {
		t.Fatal("the hover should arrive on the gold hover's timing")
	}
	l.hvAnim.Tick(0.5)
	first := l.hv.at
	if l.hvShown <= 0 || l.hvShown >= 1 {
		t.Fatalf("the hover should be fading in, at %g", l.hvShown)
	}
	l.hvAnim.Tick(1)
	if l.hv.at != first {
		t.Fatal("the first week's hover should fade in in place")
	}

	move(4)
	l.hvAnim.Tick(0.25)
	if l.hvWeek != 1 || l.hvInk <= 0 || l.hvInk >= 1 {
		t.Fatalf("the old week's text should fade out first, week %d at %g", l.hvWeek, l.hvInk)
	}
	for _, text := range append([]*canvas.Text{l.tipTitle}, l.tipLines...) {
		if text.Position().X+text.MinSize().Width > l.hv.boxAt.X+l.hv.box.Width {
			t.Fatalf("%q runs out of the box", text.Text)
		}
	}
	l.hvAnim.Tick(0.5)
	mid := l.hv.at
	l.hvAnim.Tick(0.75)
	if l.hvWeek != 4 || l.hvInk <= 0 || l.hvInk >= 1 {
		t.Fatalf("the new week's text should fade in after, week %d at %g", l.hvWeek, l.hvInk)
	}
	l.hvAnim.Tick(1)
	if mid <= first || mid >= l.hv.at {
		t.Fatalf("the guide should slide between weeks: %g, %g, %g", first, mid, l.hv.at)
	}

	stood := l.hv
	c.MouseOut()
	l.hvAnim.Tick(0.5)
	if l.hvShown <= 0 || l.hvShown >= 1 || l.hv != stood || !l.tipBg.Visible() {
		t.Fatalf("leaving should fade the hover where it stood, at %g", l.hvShown)
	}
	l.hvAnim.Tick(1)
	if l.tipBg.Visible() || l.guide.Visible() {
		t.Fatal("the hover should be gone once faded")
	}
}
