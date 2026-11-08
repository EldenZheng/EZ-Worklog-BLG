package tests

import (
	"testing"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"worklog/internal/menus/commitlist"
	"worklog/internal/menus/meeting"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
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

func TestCommitMenusWaitForDetailsBeforeEntrance(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	for _, name := range []string{"commits", "meeting"} {
		t.Run(name, func(t *testing.T) {
			const week = "2026-08-20"
			ui := &shell.UI{Store: backend.NewStore(tempDir(t)), WeekStart: week, MeetingStart: week, Cfg: backend.Config{Repos: []string{"o/r"}}}
			var box *fyne.Container
			var draw func()
			if name == "commits" {
				commitlist.BuildCommitListTab(ui)
				ui.EnsureWeekCommitCache()
				ui.WeekCommits[week] = []backend.Commit{}
				ui.WeekCommitLoad[week] = true
				box, draw = ui.CommitBox, func() { commitlist.DrawCommitList(ui) }
			} else {
				meeting.BuildMeetingTab(ui)
				meeting.EnsureMeetingCommitCache(ui)
				ui.MeetingCommits[week] = []backend.Commit{}
				ui.MeetingCommitLoad[week] = true
				box, draw = ui.MeetingBox, func() { meeting.DrawMeeting(ui) }
			}
			draw()
			if len(box.Objects) != 1 {
				t.Fatal("expected loading placeholder")
			}
			if _, ok := box.Objects[0].(*widget.Label); !ok {
				t.Fatal("rendered content before details finished")
			}
			if name == "commits" {
				ui.WeekCommitLoad[week] = false
			} else {
				ui.MeetingCommitLoad[week] = false
			}
			draw()
			reveals := 0
			walk(box, func(o fyne.CanvasObject) {
				if _, ok := o.(*shell.Reveal); ok {
					reveals++
				}
			})
			if reveals == 0 {
				t.Fatal("ready content did not enter")
			}
			draw()
			reveals = 0
			walk(box, func(o fyne.CanvasObject) {
				if _, ok := o.(*shell.Reveal); ok {
					reveals++
				}
			})
			if reveals != 0 {
				t.Fatal("content entered twice")
			}
		})
	}
}
