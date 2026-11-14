package kanban

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// ColumnCard is one card a status column shows.
type ColumnCard struct {
	Board backend.SprintBoard
	Item  backend.SprintItem
}

// ColumnOrder lists a status column's cards merged by position: a project's
// position is its place among its own cards in this column.
func ColumnOrder(k *shell.KanbanUI, status string) []ColumnCard {
	var projects [][]ColumnCard
	for _, b := range k.OrderedBoards() {
		var cards []ColumnCard
		for _, it := range b.Items {
			if PendingStatus(k, b, it) == status && Matches(k, b, it) {
				cards = append(cards, ColumnCard{b, it})
			}
		}
		projects = append(projects, cards)
	}
	return shell.MergeByPosition(projects)
}

var PrimaryKanbanStatuses = []string{"Created", "Analysis - Investigation", "In Progress", "Pending Customer Feedback", "Pending Test", "Server Update Required", "In Review", "Done"}

func KanbanStatusColor(name string) color.Color {
	if hue, ok := shell.StatusHue(name); ok {
		return hue
	}
	return theme.Color(theme.ColorNameOverlayBackground)
}

// Keep the established status hues, but distribute them as soft surfaces and
// quiet borders instead of bright full-height outlines.
func KanbanColumnFill(name string) color.Color {
	return shell.BlendColor(theme.Color(theme.ColorNameInputBackground), KanbanStatusColor(name), 0.14)
}

// KanbanHeaderFill is the column title's capsule: the column's own surface,
// a little brighter.
func KanbanHeaderFill(name string) color.Color {
	return shell.BlendColor(theme.Color(theme.ColorNameInputBackground), KanbanStatusColor(name), 0.30)
}

// KanbanHeaderHover is the capsule while the pointer is in its column: more of
// the status hue, lifted a touch toward white so even the dark hues (navy, plum)
// visibly brighten.
func KanbanHeaderHover(name string) color.Color {
	hue := shell.BlendColor(theme.Color(theme.ColorNameInputBackground), KanbanStatusColor(name), 0.56)
	return shell.BlendColor(hue, color.White, 0.08)
}

func KanbanColumnBorder(name string) color.Color {
	return shell.BlendColor(theme.Color(theme.ColorNameInputBorder), KanbanStatusColor(name), 0.28)
}

func StatusColumns(k *shell.KanbanUI) (shown, extra []string) {
	available := map[string]bool{}
	for _, b := range k.Plan.Boards {
		for _, o := range b.Field("Status").Options {
			available[o.Name] = true
		}
	}
	var ordered []string
	primary := map[string]bool{}
	for _, name := range PrimaryKanbanStatuses {
		primary[name] = true
		if available[name] {
			ordered = append(ordered, name)
			delete(available, name)
		}
	}
	seen := map[string]bool{}
	for _, b := range k.Plan.Boards {
		for _, o := range b.Field("Status").Options {
			if available[o.Name] && !seen[o.Name] {
				extra = append(extra, o.Name)
				seen[o.Name] = true
			}
		}
	}
	extra = append(extra, "No status")
	ordered = OrderColumns(k.Plan.KanbanColumnOrder, append(ordered, extra...))
	for _, name := range ordered {
		if k.ExtraStatuses == nil && primary[name] || k.ExtraStatuses != nil && k.ExtraStatuses[name] {
			shown = append(shown, name)
		}
	}
	return shown, extra
}

func columnControls(k *shell.KanbanUI, extra []string) fyne.CanvasObject {
	shown, _ := StatusColumns(k)
	var options []string
	available := map[string]bool{}
	for _, b := range k.Plan.Boards {
		for _, o := range b.Field("Status").Options {
			available[o.Name] = true
		}
	}
	for _, name := range PrimaryKanbanStatuses {
		if available[name] {
			options = append(options, name)
		}
	}
	options = OrderColumns(k.Plan.KanbanColumnOrder, append(options, extra...))
	if k.StatusChoices == nil {
		group := widgets.NewCompactCheckGroup(options, func(selected []string) {
			columns := append([]string{}, selected...)
			p := k.Plan
			p.KanbanColumns = &columns
			if !k.Save(p) {
				previous, _ := StatusColumns(k)
				k.StatusChoices.Selected = previous
				k.StatusChoices.Refresh()
				return
			}
			k.ExtraStatuses = map[string]bool{}
			for _, name := range selected {
				k.ExtraStatuses[name] = true
			}
			Draw(k)
		})
		// Dragging a check along the row reorders the columns, as dragging a
		// column's heading does.
		group.OnReorder = func(order []string) { MoveColumns(k, order) }
		// The check being carried follows the pointer as a ghost in its
		// column's colour, as a column's heading does when dragged.
		var ghost fyne.CanvasObject
		group.OnLift = func(name string, at fyne.Position) {
			layer := k.GhostLayer
			if layer == nil {
				return
			}
			if ghost == nil {
				k.Dragging = true
				ghost = shell.DragGhost(name, 0, KanbanStatusColor(name))
				layer.Objects = []fyne.CanvasObject{ghost}
			}
			origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(layer)
			ghost.Move(at.Subtract(origin).Add(fyne.NewPos(12, 12)))
			layer.Refresh()
		}
		group.OnLand = func(moved bool) {
			ghost = nil
			k.Dragging = false
			if k.GhostLayer != nil {
				k.GhostLayer.Objects = nil
				k.GhostLayer.Refresh()
			}
			// A save that finished mid-drag held its redraw back.
			if !moved && k.RedrawPending {
				Draw(k)
			}
		}
		k.StatusChoices = &group.CheckGroup
		// The checks can be dragged; the menu icon at the far right opens the
		// column order, the plain way.
		reorder := shell.QuietButton(widget.NewButtonWithIcon("", theme.MenuIcon(), func() { ShowColumnOrder(k) }))
		k.StatusChoiceView = container.NewBorder(nil, nil, nil, container.NewCenter(reorder),
			container.NewHScroll(shell.GoldChoice(group)))
	}
	k.StatusChoices.Options = options
	k.StatusChoices.Selected = append([]string{}, shown...)
	k.StatusChoices.Refresh()
	return k.StatusChoiceView
}

func statusHeaderInk(name string) color.Color {
	if name == "No status" {
		return theme.Color(theme.ColorNameForeground)
	}
	accent := KanbanStatusColor(name)
	r, g, b, _ := accent.RGBA()
	if r+g+b < 3*0x7000 && shell.DarkSurface(theme.Current(), fyne.CurrentApp().Settings().ThemeVariant()) {
		return shell.BlendColor(accent, theme.Color(theme.ColorNameForeground), 0.36)
	}
	return accent
}
