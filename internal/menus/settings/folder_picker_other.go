//go:build !windows

package settings

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"

	"worklog/internal/shared/shell"
)

func pickBackupFolder(ui *shell.UI, initial string, done func(string, error)) {
	picker := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			done("", err)
		} else if uri != nil {
			done(uri.Path(), nil)
		} else {
			done("", nil)
		}
	}, ui.Win)
	if folder, err := storage.ListerForURI(storage.NewFileURI(initial)); err == nil {
		picker.SetLocation(folder)
	}
	shell.ShowStockDialog(picker, ui.Win, "Cancel", "Open")
}
