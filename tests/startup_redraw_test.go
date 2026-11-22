package tests

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"worklog/internal/assemble"
	"worklog/internal/menus/loglist"
	"worklog/internal/menus/logwork"
	"worklog/internal/menus/report"
	"worklog/internal/menus/status"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestStartupDataDoesNotRebuildHiddenTimeViews(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(shell.GoldTheme{Theme: theme.DefaultTheme()})
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: a.NewWindow("startup"), HasCfg: true,
		CalMonth: shell.ThisMonth(), RepMonth: shell.CurrentPeriod(), Cfg: backend.Config{Repos: []string{"exampleorg"}, ArchiveDir: tempDir(t)}}
	assemble.BuildAllTabs(ui)
	ui.Win.SetContent(ui.Tabs)
	ui.Win.Resize(fyne.NewSize(1400, 900))
	ui.EnsureProjectCache()
	ui.ProjLoaded[shell.StatusCacheKey(ui.CalMonth)] = true
	ui.ProjLoaded[shell.ReportCacheKey(ui.RepMonth)] = true
	// The application refreshes each time view when its tab is selected.
	ui.Tabs.OnSelected = func(item *container.TabItem) {
		switch item.Text {
		case shell.StatusTabName:
			status.DrawCalendar(ui)
		case shell.ReportTabName:
			report.DrawReport(ui)
		case shell.LogListTabName:
			loglist.DrawLogList(ui)
		}
	}
	for _, view := range []struct {
		name   string
		box    *fyne.Container
		arrive func(*shell.UI)
	}{
		{shell.StatusTabName, ui.CalBox, func(ui *shell.UI) { ui.LoadProject("startup-status", "2026-11-01", "2026-11-30", true) }},
		{shell.ReportTabName, ui.RepBox, func(ui *shell.UI) { ui.LoadProject("startup-report", "2026-11-01", "2026-11-30", true) }},
		{shell.LogListTabName, ui.ListBox, func(ui *shell.UI) { logwork.DrawWeekStrip(ui); logwork.RenderPending(ui) }},
	} {
		ui.Tabs.SelectName(shell.LogWorkTabName)
		marker := widget.NewLabel("not drawn yet")
		view.box.Objects = []fyne.CanvasObject{marker}
		view.arrive(ui)
		if len(view.box.Objects) != 1 || view.box.Objects[0] != marker {
			t.Fatalf("%s rebuilt while Log Work was active", view.name)
		}
		ui.Tabs.SelectName(view.name)
		if len(view.box.Objects) == 0 || view.box.Objects[0] == marker {
			t.Fatalf("%s did not catch up when opened", view.name)
		}
	}
}
