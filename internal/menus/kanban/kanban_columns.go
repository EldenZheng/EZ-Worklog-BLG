package kanban

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

var PrimaryKanbanStatuses = []string{"Created", "Analysis - Investigation", "In Progress", "Pending Customer Feedback", "Pending Test", "Server Update Required", "In Review", "Done"}

func KanbanStatusColor(name string) color.Color {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "created":
		return shell.OrgUnknown
	case "analysis - investigation":
		return color.NRGBA{R: 22, G: 52, B: 88, A: 255}
	case "in progress":
		return color.NRGBA{R: 220, G: 183, B: 40, A: 255}
	case "pending customer feedback":
		return color.NRGBA{R: 58, G: 30, B: 82, A: 255}
	case "pending test":
		return color.NRGBA{R: 202, G: 104, B: 31, A: 255}
	case "server update required":
		return color.NRGBA{R: 205, G: 112, B: 161, A: 255}
	case "in review":
		return color.NRGBA{R: 126, G: 76, B: 177, A: 255}
	case "done":
		return color.NRGBA{R: 42, G: 127, B: 74, A: 255}
	default:
		return theme.Color(theme.ColorNameOverlayBackground)
	}
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

// KanbanHeaderHover is the capsule while the pointer is in its column.
func KanbanHeaderHover(name string) color.Color {
	return shell.BlendColor(theme.Color(theme.ColorNameInputBackground), KanbanStatusColor(name), 0.48)
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
	ordered = append(ordered, extra...)
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
	options = append(options, extra...)
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
		k.StatusChoices = &group.CheckGroup
		k.StatusChoiceView = container.NewHScroll(shell.GoldChoice(group))
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
