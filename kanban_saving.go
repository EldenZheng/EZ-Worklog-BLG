package main

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ---- making GitHub writes from the Kanban visible while they run ----

// pushSprintItem sends one field change to GitHub. A variable so tests can
// stand in for the gh CLI.
var pushSprintItem = updateSprintItem

// kanbanPending is a status or sprint change on its way to GitHub. While it
// is in flight the card is drawn where it is headed (status) or where it is
// (sprint), dimmed, with what is being saved written across it.
type kanbanPending struct {
	kind, value string
	label       string // "In Progress", "Sprint 5", "No status"
}

func (k *kanbanUI) setPending(b sprintBoard, it sprintItem, kind, value string) {
	if k.pending == nil {
		k.pending = map[string]kanbanPending{}
	}
	k.pending[it.key(b.ID)] = kanbanPending{kind: kind, value: value, label: fieldValueLabel(b, kind, value)}
}

func (k *kanbanUI) clearPending(b sprintBoard, it sprintItem) {
	delete(k.pending, it.key(b.ID))
}

// fieldValueLabel names a status or sprint value the way the board shows it.
func fieldValueLabel(b sprintBoard, kind, value string) string {
	if kind == "position" {
		return ""
	}
	if kind == "status" {
		if value == "" {
			return "No status"
		}
		return value
	}
	if value == "" {
		return "No sprint"
	}
	for _, s := range b.iterations() {
		if s.ID == value {
			return s.Title
		}
	}
	return value
}

// pendingStatus is the column a card is drawn in: where it is being moved
// while that save runs, otherwise its board status.
func (k *kanbanUI) pendingStatus(b sprintBoard, it sprintItem) string {
	if p, ok := k.pending[it.key(b.ID)]; ok && p.kind == "status" {
		return p.value
	}
	return it.Status
}

func savingText(p kanbanPending) string {
	if p.kind == "position" {
		return "Saving order…"
	}
	if p.kind == "status" {
		return "Saving to " + p.label + "…"
	}
	return "Moving to " + p.label + "…"
}

// savingOverlay dims a card and states the save in flight on top of it.
func savingOverlay(card fyne.CanvasObject, p kanbanPending) fyne.CanvasObject {
	r, g, b, _ := theme.Color(theme.ColorNameBackground).RGBA()
	shade := canvas.NewRectangle(color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 0xa8})
	shade.CornerRadius = 10
	shade.StrokeColor = logoGold
	shade.StrokeWidth = 2
	// Gold like the outline, and wrapped to the card's width: the label sits
	// in a VBox, which hands it the full width, rather than in a Center, which
	// would let a long sprint name run past the card's edges.
	text := widget.NewLabelWithStyle(savingText(p), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	text.Wrapping = fyne.TextWrapWord
	gold := container.NewThemeOverride(text, statusHeaderTheme{Theme: theme.Current(), ink: logoGold})
	return container.NewStack(card, shade, container.NewPadded(container.NewVBox(layout.NewSpacer(), gold, layout.NewSpacer())))
}

// syncActivity runs the app's loading strip on Kanban and Sprint Calendar
// while a fetch or a write is running (only on the tab in view, as the other
// tabs do), and disables Refresh GitHub meanwhile.
func (k *kanbanUI) syncActivity() {
	on := k.busy || k.loading
	stage := "Loading project boards"
	if k.busy {
		stage = "Saving to GitHub"
	}
	selected := ""
	if k.ui.tabs != nil && k.ui.tabs.Selected() != nil {
		selected = k.ui.tabs.Selected().Text
	}
	for _, page := range []struct {
		name      string
		indicator *loadingIndicator
		page      *fyne.Container
	}{{kanbanTabName, k.activity, k.root}, {plannerTabName, k.calendarActivity, k.calendarPage}} {
		if page.indicator == nil {
			continue
		}
		active := on && (k.ui.tabs == nil || selected == page.name)
		if !active && !page.indicator.view.Visible() {
			continue
		}
		if page.indicator.showProgress(active, fetchProgress{Stage: stage}) {
			relayout(page.page)
		}
	}
	for _, b := range k.refreshButtons {
		if on {
			b.Disable()
		} else {
			b.Enable()
		}
	}
}

// moveResult is the line shown once a change has been saved or has failed.
func moveResult(it sprintItem, p kanbanPending, err error) string {
	if err != nil {
		return fmt.Sprintf("GitHub update failed for %q: %v", it.Title, err)
	}
	if p.kind == "status" {
		return fmt.Sprintf("Saved %q: status is now %s.", it.Title, p.label)
	}
	return fmt.Sprintf("Saved %q: sprint is now %s.", it.Title, p.label)
}
