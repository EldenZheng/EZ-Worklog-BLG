package settings

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func storageBreakdown(ui *shell.UI, cfg backend.Config, usage backend.StorageUsage, busy bool, clear func(backend.Config, backend.StorageCategory)) (fyne.CanvasObject, []*widget.Button) {
	var parts []shell.MeterPart
	var rows []fyne.CanvasObject
	var actions []*widget.Button
	for i, category := range usage.Categories {
		ink := shell.OrgPalette[i%len(shell.OrgPalette)]
		share := float32(0)
		if usage.StoredDataBytes > 0 {
			share = float32(float64(category.Bytes) / float64(usage.StoredDataBytes))
		}
		if category.Bytes > 0 {
			parts = append(parts, shell.MeterPart{Key: string(category.Kind), Fill: ink, Weight: share})
		}
		name := widget.NewLabelWithStyle(category.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		name.Wrapping = fyne.TextWrapWord
		dot := canvas.NewRectangle(ink)
		dot.CornerRadius = 3
		dot.SetMinSize(fyne.NewSize(6, 6))
		detail := fmt.Sprintf("%s · %.1f%%", backend.FormatBytes(uint64(category.Bytes)), float64(share)*100)
		if category.Unit != "" {
			detail += fmt.Sprintf(" · %d %s", category.Count, category.Unit)
		}
		readout := widget.NewLabel(detail)
		readout.Wrapping = fyne.TextWrapWord
		var action fyne.CanvasObject
		if category.CanClear {
			category := category
			button := settingsButton("Clear", func() { clear(cfg, category) })
			if busy || category.Bytes == 0 {
				button.Disable()
			}
			if category.Bytes > 0 {
				actions = append(actions, button)
			}
			action = container.NewCenter(shell.DestructiveButton(button))
		} else {
			action = container.NewCenter(widget.NewLabel("Kept"))
		}
		left := container.NewVBox(name, readout)
		rows = append(rows, container.NewBorder(nil, nil, container.NewCenter(dot), action, left))
	}
	caption := widget.NewLabel("Percentages of local data + backups (app executable excluded).")
	caption.Wrapping = fyne.TextWrapWord
	header := widget.NewLabelWithStyle("Data + backups: "+backend.FormatBytes(uint64(usage.StoredDataBytes)), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	header.Wrapping = fyne.TextWrapWord
	bar := ui.ProgressBarParts("settings/storage-breakdown", ui.Store.Dir, parts, 6)
	return container.NewVBox(append([]fyne.CanvasObject{header, bar, caption}, rows...)...), actions
}

func confirmClearStorage(ui *shell.UI, cfg backend.Config, category backend.StorageCategory, busy func() bool, setBusy func(bool), status *widget.Label, measure func()) {
	if busy() {
		return
	}
	var d dialog.Dialog
	cancel := settingsButton("Cancel", func() { shell.CloseDialog(d, ui.Win) })
	confirm := settingsButton("Clear "+category.Name, func() {
		if busy() {
			return
		}
		shell.CloseDialog(d, ui.Win)
		setBusy(true)
		status.SetText("Clearing " + category.Name + "…")
		var operationErr error
		ui.Async(func() error {
			if category.Kind == backend.StorageProjectHistory {
				if shell.Hooks.ClearProjectHistory == nil {
					operationErr = fmt.Errorf("project history clearing is unavailable")
					return nil
				}
				operationErr = shell.Hooks.ClearProjectHistory(ui.Store)
			} else {
				operationErr = ui.Store.ClearStorageCategory(cfg, category.Kind)
			}
			return nil
		}, func() {
			setBusy(false)
			if operationErr != nil {
				status.SetText("Could not clear " + category.Name + ": " + operationErr.Error())
				measure()
				return
			}
			refreshAfterStorageClear(ui, category.Kind)
			status.SetText(category.Name + " cleared locally.")
			measure()
		})
	})
	amountText := fmt.Sprintf("Clear %s?\n%s · %d %s", category.Name, backend.FormatBytes(uint64(category.Bytes)), category.Count, category.Unit)
	if category.Kind == backend.StorageHistory {
		amountText += "\nOwner: " + cfg.WorklogOwner
	}
	amount := widget.NewLabel(amountText)
	amount.Wrapping = fyne.TextWrapWord
	message := widget.NewLabel(category.Consequence)
	message.Wrapping = fyne.TextWrapWord
	body := container.NewVBox(amount, message, shell.RemovalButtons(cancel, confirm))
	d = dialog.NewCustomWithoutButtons("Clear local data", body, ui.Win)
	d.Resize(fyne.NewSize(550, body.MinSize().Height))
	shell.ShowDialog(d, ui.Win)
}

func refreshAfterStorageClear(ui *shell.UI, kind backend.StorageKind) {
	if kind == backend.StorageProjectHistory && shell.Hooks.ReloadProjectHistory != nil {
		shell.Hooks.ReloadProjectHistory(ui)
	}
	if kind == backend.StorageProjectHistory || kind == backend.StorageExports || kind == backend.StorageLegacyBackup {
		return
	}
	if kind == backend.StorageSupport {
		ui.SupportWeeks = map[string]bool{}
	}
	if ui.Kanban != nil {
		if kind == backend.StoragePlans {
			ui.Kanban.Plan.Tasks = map[string]backend.SprintTask{}
		}
		if kind == backend.StorageIssueCache {
			ui.Kanban.Plan.PRIssues = nil
			ui.Kanban.PRAsked = map[string]bool{}
		}
		if (kind == backend.StoragePlans || kind == backend.StorageIssueCache) && ui.Kanban.Body != nil && shell.Hooks.KanbanUIDraw != nil {
			shell.Hooks.KanbanUIDraw(ui.Kanban)
		}
	}
	if kind == backend.StorageIgnored {
		ui.Pending.Count = 0
		for i := range ui.Pending.Groups {
			ui.Pending.Groups[i].Ignored = false
			ui.Pending.Count += len(ui.Pending.Groups[i].Commits)
		}
		ui.ShowIgnored = false
	}
	if kind == backend.StorageLogged || kind == backend.StorageDrafts || kind == backend.StorageHistory {
		ui.ProjItems = nil
		ui.EnsureProjectCache()
		ui.RefreshLoggedShas()
	}
	if ui.RecentBox != nil && shell.Hooks.DrawRecent != nil {
		shell.Hooks.DrawRecent(ui)
	}
	if ui.PendingLoaded && ui.PendingBox != nil && shell.Hooks.RenderPending != nil {
		shell.Hooks.RenderPending(ui)
	}
	if ui.ListBox != nil && shell.Hooks.DrawLogList != nil {
		shell.Hooks.DrawLogList(ui)
	}
}
