package settings

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// Stable category colours keep the legend and bar in sync without cycling
// through the six organisation colours when there are more storage categories.
var storageCategoryColours = map[backend.StorageKind]color.NRGBA{
	backend.StorageLogged:         {R: 0x3b, G: 0x82, B: 0xf6, A: 0xff},
	backend.StorageDrafts:         {R: 0xcd, G: 0xa2, B: 0x36, A: 0xff},
	backend.StorageIgnored:        {R: 0x9c, G: 0xa3, B: 0xaf, A: 0xff},
	backend.StorageProjectHistory: {R: 0xa7, G: 0x8b, B: 0xfa, A: 0xff},
	backend.StoragePlans:          {R: 0x22, G: 0xb8, B: 0xcf, A: 0xff},
	backend.StorageIssueCache:     {R: 0xf9, G: 0x73, B: 0x16, A: 0xff},
	backend.StorageSupport:        {R: 0x4a, G: 0xde, B: 0x80, A: 0xff},
	backend.StorageExports:        {R: 0xfb, G: 0x71, B: 0x85, A: 0xff},
	backend.StorageHistory:        {R: 0x14, G: 0xb8, B: 0xa6, A: 0xff},
	backend.StorageLegacyBackup:   {R: 0xc0, G: 0x84, B: 0x57, A: 0xff},
	backend.StorageOther:          {R: 0xe8, G: 0x79, B: 0xf9, A: 0xff},
}

func storageBreakdown(ui *shell.UI, cfg backend.Config, usage backend.StorageUsage, busy, open bool, clear func(backend.Config, backend.StorageCategory), toggled func(bool), reflow func(bool)) (fyne.CanvasObject, []*widget.Button) {
	var parts []shell.MeterPart
	var rows []fyne.CanvasObject
	var actions []*widget.Button
	for _, category := range usage.Categories {
		ink := storageCategoryColours[category.Kind]
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
	header := canvas.NewText("Data + backups", widgets.LogoGold)
	header.TextSize, header.TextStyle.Bold = theme.TextSize()+4, true
	total := widget.NewLabelWithStyle("Total: "+backend.FormatBytes(uint64(usage.StoredDataBytes)), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	total.Wrapping = fyne.TextWrapWord
	bar := ui.ProgressBarParts("settings/storage-breakdown", ui.Store.Dir, parts, 6)
	body := container.NewVBox(append([]fyne.CanvasObject{total, bar, caption}, rows...)...)
	head, area := shell.CollapseLine(header, nil, body, open, 0, toggled, reflow)
	heading := container.NewThemeOverride(container.NewVBox(head, widget.NewSeparator()), chunkySettingsTheme{Theme: theme.Current()})
	return container.NewVBox(heading, area), actions
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
