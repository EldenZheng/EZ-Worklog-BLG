package logwork

import (
	"fmt"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// OpenPendingIssue reuses the Log Work editors; an aggregate with several
// entries first lets the user choose the date or draft to edit.
func OpenPendingIssue(ui *shell.UI, rows []backend.Row, groups []backend.Group) {
	shell.DismissSummaryTooltip()
	refresh := func() { DrawRecent(ui); shell.Hooks.DrawLogList(ui) }
	if len(rows) == 1 && len(groups) == 0 {
		openRowEditor(ui, rows[0], refresh)
		return
	}
	if len(groups) == 1 && len(rows) == 0 {
		OpenGroupEditor(ui, groups[0])
		return
	}
	choices := container.NewVBox()
	var popup dialog.Dialog
	for _, row := range rows {
		choices.Add(widget.NewButton(fmt.Sprintf("%s · %dm · %s", row["date"], row.Minutes(), RowTitle(ui, row)), func() {
			popup.Hide()
			openRowEditor(ui, row, refresh)
		}))
	}
	for _, group := range groups {
		choices.Add(widget.NewButton(fmt.Sprintf("%s · %d unlogged commits", group.Date, len(group.Commits)), func() {
			popup.Hide()
			OpenGroupEditor(ui, group)
		}))
	}
	close := widget.NewButton("Close", func() { popup.Hide() })
	popup = dialog.NewCustomWithoutButtons("Pending work", container.NewBorder(nil, shell.DialogButtons(close), nil, nil, container.NewVScroll(choices)), ui.Win)
	popup.Resize(ui.PopupSize())
	popup.Show()
}
