package shell

import (
	"slices"
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/widgets"
)

// ---- the project order, set by dragging either menu's legend ----
//
// A Kanban column and the Sprint Issues list show each project's first card,
// then each project's second, and so on; the legend decides which project comes
// first within each position. Kanban and the Sprint Calendar share one order.
// k.Plan.Boards keeps its own, which everything else reads.

// OrderedBoards lists the boards in the legends' order. A board the saved order
// does not know yet goes last.
func (k *KanbanUI) OrderedBoards() []backend.SprintBoard {
	rank := map[string]int{}
	for i, id := range k.Plan.ProjectOrder {
		rank[id] = i
	}
	at := func(id string) int {
		if i, ok := rank[id]; ok {
			return i
		}
		return len(rank)
	}
	boards := append([]backend.SprintBoard{}, k.Plan.Boards...)
	sort.SliceStable(boards, func(i, j int) bool { return at(boards[i].ID) < at(boards[j].ID) })
	return boards
}

// MergeByPosition takes each project's cards, the projects in legend order, and
// lists every first card, then every second, and so on. A project that runs out
// leaves the rest to the others.
func MergeByPosition[T any](projects [][]T) []T {
	var out []T
	for position := 0; ; position++ {
		added := false
		for _, cards := range projects {
			if position < len(cards) {
				out = append(out, cards[position])
				added = true
			}
		}
		if !added {
			return out
		}
	}
}

// ChipSpan is a drawn legend chip's horizontal extent, in absolute coordinates.
type ChipSpan struct {
	ID          string
	Left, Right float32
}

// LegendSlot works out the order a chip dropped at x makes, and where the drop
// line goes for it. Spans are in drawn order.
func LegendSlot(spans []ChipSpan, dragID string, x, gap float32) (order []string, lineX float32) {
	var others []ChipSpan
	for _, s := range spans {
		if s.ID != dragID {
			others = append(others, s)
		}
	}
	slot := 0
	for slot < len(others) && (others[slot].Left+others[slot].Right)/2 < x {
		slot++
	}
	for i, s := range others {
		if i == slot {
			order = append(order, dragID)
		}
		order = append(order, s.ID)
	}
	if slot == len(others) {
		order = append(order, dragID)
	}
	switch {
	case len(others) == 0:
	case slot == 0:
		lineX = others[0].Left - gap/2
	default:
		lineX = others[slot-1].Right + gap/2
	}
	return order, lineX
}

// legendRow is the chips of one drawn legend, and the page parts it drags over.
type legendRow struct {
	chips []*ProjectChip
	layer *fyne.Container
	clip  fyne.CanvasObject
}

// ProjectChip is a legend chip that can be dragged along the row. Tapping and
// hover stay with the chip inside it.
type ProjectChip struct {
	widget.BaseWidget
	K      *KanbanUI
	Board  backend.SprintBoard
	Chip   *Tappable
	row    *legendRow
	lifted bool
	order  []string // the order a drop here makes; nil when it changes nothing
}

func (c *ProjectChip) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.Chip)
}

func (c *ProjectChip) Dragged(e *fyne.DragEvent) {
	k, layer := c.K, c.row.layer
	if !c.lifted {
		c.lifted = true
		k.Dragging = true
		k.DropLine = newDropLine()
		layer.Objects = []fyne.CanvasObject{DragGhost(c.Board.Title, 0, k.Accent(c.Board, backend.SprintItem{})), k.DropLine}
	}
	driver := fyne.CurrentApp().Driver()
	origin := driver.AbsolutePositionForObject(layer)
	layer.Objects[0].Move(e.AbsolutePosition.Subtract(origin).Add(fyne.NewPos(12, 12)))
	var spans []ChipSpan
	var current []string
	for _, chip := range c.row.chips {
		left := driver.AbsolutePositionForObject(chip).X
		spans = append(spans, ChipSpan{chip.Board.ID, left, left + chip.Size().Width})
		current = append(current, chip.Board.ID)
	}
	order, lineX := LegendSlot(spans, c.Board.ID, e.AbsolutePosition.X, theme.Padding())
	c.order = nil
	k.DropLine.Hide()
	if pointIn(c.row.clip, e.AbsolutePosition) && !slices.Equal(order, current) {
		c.order = order
		top := driver.AbsolutePositionForObject(c).Y
		k.DropLine.Move(fyne.NewPos(lineX-origin.X-1.5, top-origin.Y))
		k.DropLine.Resize(fyne.NewSize(3, c.Size().Height))
		k.DropLine.Show()
	}
	layer.Refresh()
}

func (c *ProjectChip) DragEnd() {
	if !c.lifted {
		return
	}
	c.lifted = false
	k := c.K
	k.Dragging = false
	k.DropLine = nil
	c.row.layer.Objects = nil
	c.row.layer.Refresh()
	order := c.order
	c.order = nil
	if order != nil {
		p := k.Plan
		p.ProjectOrder = order
		if k.Save(p) {
			Hooks.KanbanUIDraw(k)
			return
		}
	}
	// A save that finished mid-drag held its redraw back.
	if k.RedrawPending {
		Hooks.KanbanUIDraw(k)
	}
}

// ProjectLegend is a menu's project key: a filter, and the order projects stack
// in. The chips count what that menu shows.
func (k *KanbanUI) ProjectLegend(calendar bool) fyne.CanvasObject {
	box := FilterGroup(layout.NewHBoxLayout(), widget.NewLabelWithStyle("Projects", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
	row := &legendRow{layer: k.GhostLayer}
	if calendar {
		row.layer = k.CalendarGhost
	}
	for _, b := range k.OrderedBoards() {
		chip := &ProjectChip{K: k, Board: b, Chip: k.projectTappable(b, calendar), row: row}
		chip.Chip.Look = widgets.GrabCursor{}
		chip.ExtendBaseWidget(chip)
		row.chips = append(row.chips, chip)
		box.Add(chip)
	}
	scroll := container.NewHScroll(box)
	previous := k.LegendClip
	if calendar {
		previous = k.CalendarLegendClip
	}
	if old, ok := previous.(*container.Scroll); ok {
		scroll.Resize(old.Size())
		scroll.ScrollToOffset(old.Offset)
	}
	row.clip = scroll
	if calendar {
		k.CalendarLegendClip = scroll
	} else {
		k.LegendClip = scroll
	}
	return scroll
}
