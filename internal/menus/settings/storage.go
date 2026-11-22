package settings

import (
	"fmt"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// PickBackupFolder opens the platform picker; tests replace it without opening
// windows on the user's desktop.
var PickBackupFolder = pickBackupFolder

// goldText shows a section's total in gold.
func goldText(l *widget.Label) fyne.CanvasObject {
	l.TextStyle.Bold = true
	return container.NewThemeOverride(l, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
}

// tightLines stacks labels as the lines of one label: the gap between them is
// a label's own line spacing, not two labels' padding.
func tightLines(lines ...fyne.CanvasObject) *fyne.Container {
	return container.New(layout.NewCustomPaddedVBoxLayout(theme.LineSpacing()-2*theme.InnerPadding()), lines...)
}

func sidebarSectionTop(content fyne.CanvasObject) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(theme.Padding()*4, 0, 0, 0), content)
}

func storagePanel(ui *shell.UI, scroll *container.Scroll) (fyne.CanvasObject, func()) {
	label := func(text string) *widget.Label { l := widget.NewLabel(text); l.Wrapping = fyne.TextWrapWord; return l }
	usageText := label("Measuring app storage…")
	freeText := label("")
	breakdown := label("")
	categoryList := container.NewVBox()
	var panel *fyne.Container
	breakdownOpen := false
	bar := ui.ProgressBarParts("settings/storage", "storage", []shell.MeterPart{{Key: "app", Fill: widgets.LogoGold}}, 6)
	barCaption := label("App / available space")
	barCaption.TextStyle.Italic = true
	historyTotal := label("")
	historyTotal.Hide()
	historyText := label("No history backup yet.")
	location := label(backend.HistoryDirectory(ui.Cfg))
	location.Wrapping = fyne.TextWrapBreak
	status := label("")
	note := label("Backs up 1 July 2025 through 20 November 2026. Refresh on or after the 20th to include the full cutoff day.")
	var fetch, refresh, more *widget.Button
	var move, clear func()
	busy := false
	buttons := func(on bool) {
		busy = on
		for _, b := range []*widget.Button{fetch, refresh, more} {
			if b != nil {
				if on {
					b.Disable()
				} else {
					b.Enable()
				}
			}
		}
	}
	invalidate := func() { ui.ProjItems = nil; ui.ProjLoaded = nil; ui.EnsureProjectCache() }
	var measure func()
	clearCategory := func(cfg backend.Config, category backend.StorageCategory) {
		confirmClearStorage(ui, cfg, category, func() bool { return busy }, buttons, status, func() { measure() })
	}
	measure = func() {
		var usage backend.StorageUsage
		var archive backend.HistoryArchive
		var usageErr, archiveErr error
		cfg := ui.Cfg
		ui.Async(func() error {
			executable, err := os.Executable()
			if err != nil {
				usageErr = err
				return nil
			}
			usage, usageErr = ui.Store.MeasureStorage(cfg, executable)
			archive, archiveErr = backend.LoadHistoryArchive(cfg)
			return nil
		}, func() {
			location.SetText(backend.HistoryDirectory(cfg))
			if usageErr != nil {
				usageText.SetText("Cannot measure storage: " + usageErr.Error())
			} else {
				owned := usage.AppBytes + usage.DataBytes
				usageText.SetText("App + local data: " + backend.FormatBytes(uint64(owned)))
				freeText.SetText("Free on app drive: " + backend.FormatBytes(usage.FreeBytes))
				breakdown.SetText(fmt.Sprintf("App %s · Data %s\nBackup %s · Free at backup location %s", backend.FormatBytes(uint64(usage.AppBytes)), backend.FormatBytes(uint64(usage.DataBytes)), backend.FormatBytes(uint64(usage.ArchiveBytes)), backend.FormatBytes(usage.ArchiveFreeBytes)))
				denominator := float64(owned) + float64(usage.FreeBytes)
				if denominator > 0 {
					fraction := float64(owned) / denominator
					ui.ProgressBarParts("settings/storage", "storage", []shell.MeterPart{{Key: "app", Fill: widgets.LogoGold, Weight: float32(fraction)}}, 6)
					barCaption.SetText(fmt.Sprintf("App / available space · %.3f%%", fraction*100))
				}
				content := storageBreakdown(ui, cfg, usage, breakdownOpen, clearCategory,
					func(open bool) { breakdownOpen = open },
					func(final bool) { shell.Reflow(final, scroll, panel, categoryList) })
				categoryList.Objects = []fyne.CanvasObject{content}
				categoryList.Refresh()
			}
			historyTotal.Hide()
			if os.IsNotExist(archiveErr) {
				historyText.SetText("No history backup yet.")
			} else if archiveErr != nil {
				historyText.SetText("Cannot read backup: " + archiveErr.Error())
			} else {
				rangeText := "No matching records"
				if len(archive.Items) > 0 {
					first, last := archive.Items[0].Date, archive.Items[0].Date
					for _, it := range archive.Items {
						first = min(first, it.Date)
						last = max(last, it.Date)
					}
					rangeText = first + " to " + last
				}
				historyTotal.SetText(fmt.Sprintf("%d records · %s", len(archive.Items), rangeText))
				historyTotal.Show()
				historyText.SetText(fmt.Sprintf("Backed up through %s\nLast fetched %s", archive.Through, archive.FetchedAt))
			}
		})
	}
	fetch = settingsButton("Back up from GitHub", func() {
		if busy {
			return
		}
		buttons(true)
		status.SetText("Fetching all historical worklogs…")
		progress, finish := ui.BeginFetch("history-backup", "Backing up worklog history")
		cfg := ui.Cfg
		var count int
		var operationErr error
		ui.Async(func() error {
			a, err := backend.FetchHistoryArchive(cfg, progress)
			if err != nil {
				operationErr = err
				return nil
			}
			count = len(a.Items)
			operationErr = backend.SaveHistoryArchive(cfg, a)
			if operationErr == nil {
				stored, err := backend.LoadHistoryArchive(cfg)
				operationErr = err
				count = len(stored.Items)
			}
			return nil
		}, func() {
			defer finish()
			buttons(false)
			invalidate()
			if operationErr != nil {
				status.SetText("Backup failed: " + operationErr.Error())
				return
			}
			status.SetText(fmt.Sprintf("Backed up %d records.", count))
			measure()
		})
	})
	move = func() {
		if busy {
			return
		}
		entry := widget.NewEntry()
		entry.SetText(backend.HistoryDirectory(ui.Cfg))
		free := label("")
		entry.OnChanged = func(path string) {
			if available, _, err := backend.DiskSpace(strings.TrimSpace(path)); err == nil {
				free.SetText("Free at this location: " + backend.FormatBytes(available))
			} else {
				free.SetText(err.Error())
			}
		}
		entry.OnChanged(entry.Text)
		var browse *widget.Button
		browse = settingsButton("Choose folder", func() {
			browse.Disable()
			PickBackupFolder(ui, entry.Text, func(folder string, err error) {
				browse.Enable()
				if err != nil {
					free.SetText(err.Error())
				} else if folder != "" {
					entry.SetText(folder)
				}
			})
		})
		var popup *widget.PopUp
		cancel := settingsButton("Cancel", func() { shell.ClosePopUp(popup) })
		confirm := settingsButton("Reallocate", func() {
			if busy {
				return
			}
			old := ui.Cfg
			destination := strings.TrimSpace(entry.Text)
			buttons(true)
			cancel.Disable()
			var next backend.Config
			var operationErr error
			var saved bool
			ui.Async(func() error {
				next, operationErr = backend.CopyHistoryArchive(old, destination)
				if operationErr != nil {
					return nil
				}
				if operationErr = ui.Store.SaveConfig(next); operationErr != nil {
					if !strings.EqualFold(backend.HistoryArchivePath(old), backend.HistoryArchivePath(next)) {
						_ = backend.ClearHistoryArchive(next)
					}
					return nil
				}
				saved = true
				if !strings.EqualFold(backend.HistoryArchivePath(old), backend.HistoryArchivePath(next)) {
					operationErr = backend.ClearHistoryArchive(old)
				}
				return nil
			}, func() {
				buttons(false)
				cancel.Enable()
				if saved {
					ui.Cfg = next
				}
				if operationErr != nil {
					free.SetText(operationErr.Error())
					if saved {
						status.SetText("New location saved; the old backup could not be removed.")
						measure()
					}
					return
				}
				ui.Cfg = next
				shell.ClosePopUp(popup)
				status.SetText("Backup location saved.")
				measure()
			})
		})
		body := container.NewVBox(label("Move this history backup to another folder. The new copy is verified before the old file is removed."), entry, browse, free)
		popup = ui.ShowConfirmation("Reallocate history backup", body, shell.DialogButtons(cancel, confirm))
	}
	clear = func() {
		if busy {
			return
		}
		var popup *widget.PopUp
		cfg := ui.Cfg
		cancel := settingsButton("Cancel", func() { shell.ClosePopUp(popup) })
		confirm := settingsButton("Clear backup", func() {
			shell.ClosePopUp(popup)
			buttons(true)
			var operationErr error
			ui.Async(func() error { operationErr = backend.ClearHistoryArchive(cfg); return nil }, func() {
				buttons(false)
				if operationErr != nil {
					ui.Errf(operationErr)
					return
				}
				invalidate()
				status.SetText("History backup cleared.")
				measure()
			})
		})
		body := label("Delete this owner's local history backup? Historical reports will need GitHub again. Your drafts, settings, exports and GitHub records are kept.")
		popup = ui.ShowConfirmation("Clear history backup?", body, shell.RemovalButtons(cancel, confirm))
	}
	refresh = settingsIconButton("", theme.ViewRefreshIcon(), func() {
		if !busy {
			measure()
		}
	})
	var menu *widget.PopUp
	more = settingsButton("…", func() {
		if busy {
			return
		}
		if menu != nil && menu.Visible() {
			shell.ClosePopUp(menu)
			menu = nil
			return
		}
		closeMenu := func() {
			if menu != nil {
				shell.ClosePopUp(menu)
				menu = nil
			}
		}
		menuMove := settingsButton("Reallocate backup", func() { closeMenu(); move() })
		menuClear := settingsButton("Clear backup", func() { closeMenu(); clear() })
		body := container.NewPadded(container.NewVBox(menuMove, shell.DestructiveButton(menuClear)))
		content := shell.NewReveal(body)
		content.OnDismiss = closeMenu
		menu = widget.NewPopUp(content, ui.Win.Canvas())
		menu.Resize(body.MinSize())
		at := fyne.CurrentApp().Driver().AbsolutePositionForObject(more)
		menu.ShowAtPosition(at.Add(fyne.NewPos(more.Size().Width-menu.Size().Width, more.Size().Height)))
		shell.AnimateOpening(ui.Win.Canvas())
	})
	backupActions := container.NewBorder(nil, nil, nil, shell.QuietButton(more), fetch)
	goldRefresh := container.NewThemeOverride(shell.QuietButton(refresh), widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	// Storage, Data + Backups and History Backup share one heading: gold,
	// folding, lit under the pointer, folded at first. Each total is in gold.
	var storage, history *fyne.Container
	var storageTop, historyTop fyne.CanvasObject
	totals := tightLines(goldText(usageText), freeText)
	storage = storageFold("Storage", container.NewVBox(container.NewBorder(nil, nil, nil, goldRefresh, totals), bar, barCaption, breakdown), false, nil,
		func(final bool) { shell.Reflow(final, scroll, panel, storageTop, storage) })
	storageTop = sidebarSectionTop(storage)
	// Like Data + Backups, History Backup starts folded.
	history = storageFold("History Backup", container.NewVBox(note, tightLines(goldText(historyTotal), historyText), location, backupActions), false, nil,
		func(final bool) { shell.Reflow(final, scroll, panel, historyTop, history) })
	historyTop = sidebarSectionTop(history)
	// The status stays outside the fold: it also reports Data + Backups clears.
	panel = container.NewVBox(storageTop, sidebarSectionTop(categoryList), historyTop, status)
	measure()
	return panel, measure
}
