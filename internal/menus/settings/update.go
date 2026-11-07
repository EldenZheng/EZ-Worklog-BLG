package settings

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// QuitForUpdate closes the app once the updated copy has been started. A
// variable so tests can see it called without stopping the test app.
var QuitForUpdate = func() { fyne.CurrentApp().Quit() }

// updatePanel is the foot of the Settings sidebar: the running version, what
// the last check found, the green Update button when there is something to
// install, and a button to check again.
func updatePanel(ui *shell.UI) fyne.CanvasObject {
	u := &ui.Update
	u.Status = widget.NewLabel("")
	u.Status.Wrapping = fyne.TextWrapWord
	u.Check = widget.NewButtonWithIcon("Check for updates", theme.ViewRefreshIcon(), func() {
		CheckForUpdates(ui, true)
	})
	u.Install = widget.NewButtonWithIcon("Update", theme.DownloadIcon(), func() { InstallUpdate(ui) })
	u.InstallView = shell.ConfirmButton(u.Install)
	version := widget.NewLabel("Version " + backend.Version)
	version.Wrapping = fyne.TextWrapBreak
	showUpdateState(ui)
	return container.NewVBox(
		container.NewThemeOverride(widget.NewSeparator(), sidebarLineTheme{Theme: theme.Current()}),
		shell.Bold("Updates"),
		version,
		u.Status,
		u.InstallView,
		u.Check,
	)
}

// showUpdateState paints what is known about updates onto the panel and the
// gear. Safe to call before Settings is built: the check at start-up does.
func showUpdateState(ui *shell.UI) {
	u := &ui.Update
	if ui.Tabs != nil {
		ui.Tabs.ShowUpdate(u.Available)
	}
	if u.Status == nil {
		return
	}
	shell.ShowIf(u.InstallView, u.Available)
	if u.Available {
		u.Install.SetText("Update to " + u.Latest.Tag)
		u.Status.SetText(fmt.Sprintf("%s is available (%s).",
			u.Latest.Tag, u.Latest.Published.Local().Format("2 Jan 2006")))
	}
	if u.Busy {
		u.Check.Disable()
		u.Install.Disable()
	} else {
		u.Check.Enable()
		u.Install.Enable()
	}
	if u.Frame != nil {
		u.Frame.Refresh()
	}
}

// CheckForUpdates asks GitHub for the latest release and compares it with this
// build. manual is a press of Check for updates: it reports "up to date" and
// failures, where the quiet check at start-up only speaks up when there is an
// update to offer.
func CheckForUpdates(ui *shell.UI, manual bool) {
	u := &ui.Update
	if u.Busy {
		return
	}
	u.Busy = true
	if manual && u.Status != nil {
		u.Status.SetText("Checking for updates…")
	}
	showUpdateState(ui)
	var latest backend.Release
	var err error
	ui.Async(func() error {
		latest, err = backend.LatestRelease()
		return nil
	}, func() {
		u.Busy = false
		if err != nil {
			if manual && u.Status != nil {
				u.Status.SetText("Could not check for updates: " + err.Error())
			}
			showUpdateState(ui)
			return
		}
		u.Latest = latest
		u.Available = backend.NewerRelease(latest.Tag, backend.Version)
		if !u.Available && u.Status != nil {
			u.Status.SetText("Up to date. Latest release: " + latest.Tag + ".")
		}
		showUpdateState(ui)
	})
}

// InstallUpdate downloads the latest release, swaps it in for the running
// binary and restarts into it. Kanban changes still on their way to GitHub
// would be lost by the restart, so it waits for those.
func InstallUpdate(ui *shell.UI) {
	u := &ui.Update
	if u.Busy || !u.Available {
		return
	}
	if ui.Kanban != nil {
		if n := ui.Kanban.PendingWrites(); n > 0 {
			u.Status.SetText(fmt.Sprintf("%d Kanban change(s) are still saving to GitHub. Update once they finish.", n))
			return
		}
	}
	exe, err := backend.ExecutablePath()
	if err != nil {
		u.Status.SetText("Could not find the running app to update: " + err.Error())
		return
	}
	tag := u.Latest.Tag
	u.Busy = true
	u.Status.SetText("Downloading " + tag + "…")
	showUpdateState(ui)
	ui.Async(func() error {
		var path string
		path, err = backend.DownloadRelease(tag)
		if err == nil {
			err = backend.InstallUpdate(path, exe)
		}
		return nil
	}, func() {
		u.Busy = false
		if err != nil {
			u.Status.SetText("Update failed, nothing was changed: " + err.Error())
			showUpdateState(ui)
			return
		}
		u.Available = false
		if err := backend.Relaunch(exe); err != nil {
			u.Status.SetText("Installed " + tag + ". Close and reopen the app to start using it.")
			showUpdateState(ui)
			return
		}
		u.Status.SetText("Installed " + tag + ". Restarting…")
		QuitForUpdate()
	})
}
