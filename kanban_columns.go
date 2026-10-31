package main

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

var primaryKanbanStatuses = []string{"Created", "Analysis - Investigation", "In Progress", "Pending Customer Feedback", "Pending Test", "Server Update Required", "In Review", "Done"}

func kanbanStatusColor(name string) color.Color {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "created":
		return orgUnknown
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
func kanbanColumnFill(name string) color.Color {
	return blendColor(theme.Color(theme.ColorNameInputBackground), kanbanStatusColor(name), 0.14)
}
func kanbanColumnBorder(name string) color.Color {
	return blendColor(theme.Color(theme.ColorNameInputBorder), kanbanStatusColor(name), 0.28)
}

func (k *kanbanUI) statusColumns() (shown, extra []string) {
	available := map[string]bool{}
	for _, b := range k.plan.Boards {
		for _, o := range b.field("Status").Options {
			available[o.Name] = true
		}
	}
	var ordered []string
	primary := map[string]bool{}
	for _, name := range primaryKanbanStatuses {
		primary[name] = true
		if available[name] {
			ordered = append(ordered, name)
			delete(available, name)
		}
	}
	seen := map[string]bool{}
	for _, b := range k.plan.Boards {
		for _, o := range b.field("Status").Options {
			if available[o.Name] && !seen[o.Name] {
				extra = append(extra, o.Name)
				seen[o.Name] = true
			}
		}
	}
	extra = append(extra, "No status")
	ordered = append(ordered, extra...)
	for _, name := range ordered {
		if k.extraStatuses == nil && primary[name] || k.extraStatuses != nil && k.extraStatuses[name] {
			shown = append(shown, name)
		}
	}
	return shown, extra
}

func (k *kanbanUI) columnControls(extra []string) fyne.CanvasObject {
	shown, _ := k.statusColumns()
	var options []string
	available := map[string]bool{}
	for _, b := range k.plan.Boards {
		for _, o := range b.field("Status").Options {
			available[o.Name] = true
		}
	}
	for _, name := range primaryKanbanStatuses {
		if available[name] {
			options = append(options, name)
		}
	}
	options = append(options, extra...)
	if k.statusChoices == nil {
		group := newCompactCheckGroup(options, func(selected []string) {
			columns := append([]string{}, selected...)
			p := k.plan
			p.KanbanColumns = &columns
			if !k.save(p) {
				previous, _ := k.statusColumns()
				k.statusChoices.Selected = previous
				k.statusChoices.Refresh()
				return
			}
			k.extraStatuses = map[string]bool{}
			for _, name := range selected {
				k.extraStatuses[name] = true
			}
			k.draw()
		})
		k.statusChoices = &group.CheckGroup
		k.statusChoiceView = container.NewHScroll(goldChoice(group))
	}
	k.statusChoices.Options = options
	k.statusChoices.Selected = append([]string{}, shown...)
	k.statusChoices.Refresh()
	return k.statusChoiceView
}

type statusHeaderTheme struct {
	fyne.Theme
	ink color.Color
}

func (t statusHeaderTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground {
		return t.ink
	}
	return t.Theme.Color(name, v)
}
func statusHeaderInk(name string) color.Color {
	if name == "No status" {
		return theme.Color(theme.ColorNameForeground)
	}
	accent := kanbanStatusColor(name)
	r, g, b, _ := accent.RGBA()
	if r+g+b < 3*0x7000 && darkSurface(theme.Current(), fyne.CurrentApp().Settings().ThemeVariant()) {
		return blendColor(accent, theme.Color(theme.ColorNameForeground), 0.36)
	}
	return accent
}
