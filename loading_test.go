package main

import (
	"testing"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func TestLoadingBarsStopWhenLeavingAndWhenFetchCompletes(t *testing.T) {
	newMotionTestApp(t)
	ui := &UI{weekStart: "2026-09-07", repMonth: "2026-09",
		weekCommitLoad: map[string]bool{"2026-09-07": true},
		projLoading:    map[string]bool{reportCacheKey("2026-09"): true},
		commitProgress: newLoadingIndicator(), reportProgress: newLoadingIndicator()}
	defer ui.commitProgress.bar.Stop()
	defer ui.reportProgress.bar.Stop()
	if ui.commitProgress.bar.Running() || ui.reportProgress.bar.Running() {
		t.Fatal("hidden bars must start stopped")
	}
	ui.tabs = newPillTabs(container.NewTabItem(commitListTabName, widget.NewLabel("commits")),
		container.NewTabItem(reportTabName, widget.NewLabel("report")), container.NewTabItem(settingsTabName, widget.NewLabel("settings")))
	ui.tabs.OnSelected = func(*container.TabItem) { ui.syncLoading() }
	ui.syncLoading()
	if !ui.commitProgress.bar.Running() || ui.reportProgress.bar.Running() {
		t.Fatal("only active Commit List should animate")
	}
	ui.tabs.SelectIndex(1)
	if ui.commitProgress.bar.Running() || !ui.reportProgress.bar.Running() {
		t.Fatal("switching must transfer the loading indicator")
	}
	ui.tabs.SelectIndex(2)
	if ui.commitProgress.bar.Running() || ui.reportProgress.bar.Running() {
		t.Fatal("offscreen bars must not animate")
	}
	ui.tabs.SelectIndex(1)
	ui.projLoading[reportCacheKey("2026-09")] = false
	ui.rateLoading = true
	ui.syncLoading()
	if !ui.reportProgress.bar.Running() {
		t.Fatal("report still awaits exchange rate")
	}
	ui.rateLoading = false
	ui.syncLoading()
	if ui.reportProgress.bar.Running() || ui.reportProgress.view.Visible() {
		t.Fatal("completed loading must stop and hide")
	}
}
