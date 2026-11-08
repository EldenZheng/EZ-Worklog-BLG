package shell

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/widgets"
)

// ItemTypeControls is the Issues | Pull Request key that Kanban and the Sprint
// Calendar keep by their project legends: gold while shown, grey while
// hidden, and a click flips one. visible says what is shown now; set gets the
// item types to show after a click, to save and redraw.
func ItemTypeControls(visible func(backend.SprintItem) bool, set func([]string)) *fyne.Container {
	// A wide gap keeps the two filters reading as separate toggles.
	row := container.New(layout.NewCustomPaddedHBoxLayout(theme.Padding() * 8))
	for _, kind := range []string{backend.KanbanIssues, backend.KanbanPullRequests} {
		kind := kind
		it := backend.SprintItem{Kind: "Issue"}
		label := "Issues"
		if kind == backend.KanbanPullRequests {
			it.Kind = "PullRequest"
			label = "Pull Request"
		}
		ink := color.Color(widgets.LogoGold)
		if !visible(it) {
			ink = theme.Color(theme.ColorNamePlaceHolder)
		}
		// Plain text, not labels: a label's own inset left a wide gap either
		// side of the bar. The padding goes round the whole chip instead, so
		// the hover outline sits clear of the icon and the words.
		bar := canvas.NewText("|", ink)
		name := canvas.NewText(label, ink)
		content := container.NewPadded(container.NewHBox(
			container.NewCenter(container.NewGridWrap(fyne.NewSize(16, 16), widget.NewIcon(it.TypeIcon()))),
			container.NewCenter(bar), container.NewCenter(name)))
		row.Add(NewCardTappable(container.NewThemeOverride(content, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: ink}), func() {
			var selected []string
			for _, candidate := range []backend.SprintItem{{Kind: "Issue"}, {Kind: "PullRequest"}} {
				shown := visible(candidate)
				if candidate.ItemType() == kind {
					shown = !shown
				}
				if shown {
					selected = append(selected, candidate.ItemType())
				}
			}
			set(selected)
		}))
	}
	return row
}

// VisibleCalendarType reports whether the Sprint Calendar tray shows it.
func (k *KanbanUI) VisibleCalendarType(it backend.SprintItem) bool {
	if it.Kind == "DraftIssue" {
		return false
	}
	if k.Plan.CalendarTypes == nil {
		return true
	}
	for _, selected := range *k.Plan.CalendarTypes {
		if selected == it.ItemType() {
			return true
		}
	}
	return false
}
