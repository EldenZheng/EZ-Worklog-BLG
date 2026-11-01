package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
)

func (k *kanbanUI) drawKeepingCalendarScroll() {
	k.draw()
}

// plannerScrollInset keeps the day frames' top edge inside the scroll: a frame
// drawn at y=0 loses the outer half of its stroke (and all of a 4px drop
// highlight) to the scroll's clip.
func plannerScrollInset(o fyne.CanvasObject) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(4, 0, 0, 0), o)
}

func plannerScroll(o fyne.CanvasObject) *container.Scroll {
	switch scroll := o.(type) {
	case *container.Scroll:
		return scroll
	case *plannerFastScroll:
		return &scroll.Scroll
	}
	return nil
}
func scrollOffset(o fyne.CanvasObject) fyne.Position {
	if scroll := plannerScroll(o); scroll != nil {
		return scroll.Offset
	}
	return fyne.Position{}
}
func restoreScroll(o fyne.CanvasObject, offset fyne.Position) {
	if scroll := plannerScroll(o); scroll != nil {
		scroll.ScrollToOffset(offset)
	}
}

// Only Month mode boosts wheel/trackpad movement. Scrollbar dragging remains
// native, and the rest of the app keeps its normal speed.
type plannerFastScroll struct{ container.Scroll }

func newPlannerFastScroll(content fyne.CanvasObject) *plannerFastScroll {
	s := &plannerFastScroll{Scroll: container.Scroll{Direction: container.ScrollVerticalOnly, Content: content}}
	s.ExtendBaseWidget(s)
	return s
}
func (s *plannerFastScroll) Scrolled(e *fyne.ScrollEvent) {
	boosted := *e
	boosted.Scrolled.DY *= 6
	s.Scroll.Scrolled(&boosted)
}

func calendarSprintLabel(s sprintIteration) string {
	label := s.Title + " | " + sprintDateLabel(s)
	if s.contains(today()) {
		label += " - Current"
	}
	return label
}

func (k *kanbanUI) calendarSprintChoices() []combinedSprint {
	groups := k.combinedSprints()
	for _, b := range k.plan.Boards {
		if !k.visibleBoard(b) {
			continue
		}
		s := b.current(today())
		if s.ID == "" {
			continue
		}
		found := false
		for _, g := range groups {
			if g.Key == sprintKey(s) {
				found = true
			}
		}
		if !found {
			groups = append([]combinedSprint{{Key: sprintKey(s), Sprint: s}}, groups...)
		}
	}
	return groups
}

func plannerDayPercentage(minutes int) string {
	return fmt.Sprintf("%d min · %d%%", minutes, percentOf(minutes))
}

// Each row grows to fit its busiest date. Spare viewport height is distributed
// across rows, so a quiet month fills the canvas while a busy one scrolls.
type plannerRowsLayout struct{}

func (plannerRowsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	size := fyne.Size{}
	count := 0
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		m := o.MinSize()
		size.Width = fyne.Max(size.Width, m.Width)
		size.Height += m.Height
		count++
	}
	if count > 1 {
		size.Height += float32(count-1) * theme.Padding()
	}
	return size
}
func (l plannerRowsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	count := 0
	for _, o := range objects {
		if o.Visible() {
			count++
		}
	}
	if count == 0 {
		return
	}
	extra := fyne.Max(0, size.Height-l.MinSize(objects).Height) / float32(count)
	y := float32(0)
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		h := o.MinSize().Height + extra
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, h))
		y += h + theme.Padding()
	}
}
