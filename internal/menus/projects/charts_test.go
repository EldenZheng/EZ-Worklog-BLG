package projects

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
)

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
