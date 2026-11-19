package projects

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
)

func TestProjectNamesWrapBeforeTruncatingAndRecoverOnResize(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	full := "Release readiness and rollout"
	first := boldText(full, currentPalette().ink)
	box := wrapHeading(first, full)
	oneLineHeight := box.MinSize().Height
	second := box.Objects[1].(*canvas.Text)
	width := fyne.MeasureText("Release readiness", first.TextSize, first.TextStyle).Width + 1
	box.Resize(fyne.NewSize(width, box.MinSize().Height))
	if first.Text != "Release readiness" || second.Text != "and rollout" {
		t.Fatalf("the whole name should fit on two lines: %q / %q", first.Text, second.Text)
	}
	if second.Position().Y < first.Position().Y+first.MinSize().Height {
		t.Fatal("wrapped lines must not overlap")
	}
	if box.MinSize().Height <= oneLineHeight {
		t.Fatal("a wrapped name must reserve its second line")
	}
	box.Resize(fyne.NewSize(60, box.MinSize().Height))
	if !strings.HasSuffix(second.Text, "…") {
		t.Fatal("only text beyond the second line should be truncated")
	}
	box.Resize(fyne.NewSize(600, box.MinSize().Height))
	if first.Text != full || second.Text != "" || box.MinSize().Height != oneLineHeight {
		t.Fatal("widening the panel must recover the original title")
	}
}

func TestStatusLegendWrapsOnlyOverflowingNamesAndKeepsDotBesideText(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	p := currentPalette()
	short := statusLegendEntry(p, p.done, Slice{Label: "Done", Count: 1}, 30).(*fyne.Container)
	long := statusLegendEntry(p, p.review, Slice{Label: "Analysis investigation", Count: 1}, 30).(*fyne.Container)
	rows := container.New(&statusLegendColumn{}, short, long)
	rows.Resize(fyne.NewSize(160, 300))
	info := func(entry *fyne.Container) *statusLegendLayout {
		return entry.Layout.(*statusLegendLayout)
	}
	s, l := info(short), info(long)
	if short.Size().Height >= long.Size().Height || s.label.Layout.(*twoLineHeading).lines[1].Text != "" {
		t.Fatal("only the overflowing name should use a second line")
	}
	for _, entry := range []*statusLegendLayout{s, l} {
		if entry.dot.Size() != fyne.NewSquareSize(10) || entry.dot.Position().X >= entry.label.Position().X {
			t.Fatal("the color must remain a square beside the description")
		}
		if entry.wrapped && entry.figures.Position().Y <= entry.label.Position().Y {
			t.Fatal("only an overflowing entry should move figures to the second row")
		}
	}
	if s.wrapped || !l.wrapped || s.figures.Position().X <= s.label.Position().X {
		t.Fatal("a short entry must keep its figures beside the name on one row")
	}
	bottomGap := rows.Size().Height - long.Position().Y - long.Size().Height
	if delta := short.Position().Y - bottomGap; delta < -.1 || delta > .1 {
		t.Fatal("the legend entries must be vertically centered as a group")
	}
	rows.Resize(fyne.NewSize(400, 300))
	if short.Size().Height != long.Size().Height || s.wrapped || l.wrapped {
		t.Fatal("both descriptions should return to one line when there is room")
	}
}

func TestFinishedBarsStackCountsAndKeepWeekClicks(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	p := currentPalette()
	picked := -1
	c := NewColumnChart(ColumnSpec{
		Marked: -1,
		Stacks: [][]Segment{{{0, p.done}, {4, p.review}}, {{4, p.done}, {4, p.review}}},
		Line:   []float64{1, 2},
		OnTap:  func(i int) { picked = i },
	}, p, nil)
	c.Resize(fyne.NewSize(400, 220))
	l := c.layout
	l.Layout(nil, c.Size())
	if l.top != 8 || len(l.segments) != 2 {
		t.Fatal("bars must stack each person's weekly counts on one scale")
	}
	if l.segments[0][0].Size().Height != 0 || l.segments[1][1].Position().Y != l.plotTop {
		t.Fatal("zero must have no height and the busiest stack must reach the top")
	}
	c.Tapped(&fyne.PointEvent{Position: fyne.NewPos(300, 50)})
	if picked != 1 {
		t.Fatal("clicking a bar should still select its week")
	}
	c.MouseIn(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(300, 50)}})
	if l.pill.Rect.Size().Width <= 0 {
		t.Fatal("bar charts should share the gold hover pill")
	}
	c.MouseOut()
	if _, _, _, alpha := l.pill.Rect.FillColor.RGBA(); alpha != 0 {
		t.Fatal("leaving should fade the pill out")
	}
}

func TestFinishedBarsHandleEmptyAndSingleWeek(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	for _, stacks := range [][][]Segment{nil, {nil}, {{{0, currentPalette().done}}}} {
		c := NewColumnChart(ColumnSpec{Stacks: stacks, Marked: -1}, currentPalette(), nil)
		c.Resize(fyne.NewSize(240, 140))
		c.layout.Layout(nil, c.Size())
		c.MouseOut()
		if c.layout.top < 1 {
			t.Fatal("an empty report needs a nonzero scale")
		}
	}
}
