package sprintcalendar

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func DrawKeepingCalendarScroll(k *shell.KanbanUI) {
	shell.Hooks.KanbanUIDraw(k)
}

// plannerScrollInset keeps the day frames' top edge inside the scroll: a frame
// drawn at y=0 loses the outer half of its stroke (and all of a 4px drop
// highlight) to the scroll's clip.
func plannerScrollInset(o fyne.CanvasObject) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(4, 0, 0, 0), o)
}

func newPlannerFastScroll(content fyne.CanvasObject) *shell.PlannerFastScroll {
	s := &shell.PlannerFastScroll{Scroll: container.Scroll{Direction: container.ScrollVerticalOnly, Content: content}}
	s.ExtendBaseWidget(s)
	return s
}

func CalendarSprintLabel(s backend.SprintIteration) string {
	label := s.Title + " | " + shell.SprintDateLabel(s)
	if s.Contains(shell.Today()) {
		label += " - Current"
	}
	return label
}

func calendarSprintChoices(k *shell.KanbanUI) []shell.CombinedSprint {
	groups := k.CombinedSprints()
	for _, b := range k.Plan.Boards {
		if !k.VisibleBoard(b) {
			continue
		}
		s := b.Current(shell.Today())
		if s.ID == "" {
			continue
		}
		found := false
		for _, g := range groups {
			if g.Key == backend.SprintKey(s) {
				found = true
			}
		}
		if !found {
			groups = append([]shell.CombinedSprint{{Key: backend.SprintKey(s), Sprint: s}}, groups...)
		}
	}
	return groups
}

func PlannerDayPercentage(minutes int) string {
	return fmt.Sprintf("%d min · %d%%", minutes, shell.PercentOf(minutes))
}

// Each row grows to fit its busiest date. Spare viewport height is distributed
// across rows, so a quiet month fills the canvas while a busy one scrolls.
type PlannerRowsLayout struct{}

func (PlannerRowsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
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

func (l PlannerRowsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
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
