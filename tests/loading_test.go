package tests

import (
	"testing"
	"worklog/internal/shared/shell"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func TestLoadingBarsStopWhenLeavingAndWhenFetchCompletes(t *testing.T) {
	newMotionTestApp(t)
	ui := &shell.UI{WeekStart: "2026-09-07", RepMonth: "2026-09",
		WeekCommitLoad: map[string]bool{"2026-09-07": true},
		ProjLoading:    map[string]bool{shell.ReportCacheKey("2026-09"): true},
		CommitProgress: shell.NewLoadingIndicator(), ReportProgress: shell.NewLoadingIndicator()}
	defer ui.CommitProgress.Bar.Stop()
	defer ui.ReportProgress.Bar.Stop()
	if ui.CommitProgress.Bar.Running() || ui.ReportProgress.Bar.Running() {
		t.Fatal("hidden bars must start stopped")
	}
	ui.Tabs = shell.NewPillTabs(container.NewTabItem(shell.CommitListTabName, widget.NewLabel("commits")),
		container.NewTabItem(shell.ReportTabName, widget.NewLabel("report")), container.NewTabItem(shell.SettingsTabName, widget.NewLabel("settings")))
	ui.Tabs.OnSelected = func(*container.TabItem) { ui.SyncLoading() }
	ui.SyncLoading()
	if !ui.CommitProgress.Bar.Running() || ui.ReportProgress.Bar.Running() {
		t.Fatal("only active Commit List should animate")
	}
	ui.Tabs.SelectIndex(1)
	if ui.CommitProgress.Bar.Running() || !ui.ReportProgress.Bar.Running() {
		t.Fatal("switching must transfer the loading indicator")
	}
	ui.Tabs.SelectIndex(2)
	if ui.CommitProgress.Bar.Running() || ui.ReportProgress.Bar.Running() {
		t.Fatal("offscreen bars must not animate")
	}
	ui.Tabs.SelectIndex(1)
	ui.ProjLoading[shell.ReportCacheKey("2026-09")] = false
	ui.RateLoading = true
	ui.SyncLoading()
	if !ui.ReportProgress.Bar.Running() {
		t.Fatal("report still awaits exchange rate")
	}
	ui.RateLoading = false
	ui.SyncLoading()
	if ui.ReportProgress.Bar.Running() || ui.ReportProgress.View.Visible() {
		t.Fatal("completed loading must stop and hide")
	}
}
