package main

import (
	_ "embed"
	"time"
	"worklog/internal/assemble"
	"worklog/internal/menus/commitlist"
	"worklog/internal/menus/kanban"
	"worklog/internal/menus/loglist"
	"worklog/internal/menus/logwork"
	"worklog/internal/menus/meeting"
	"worklog/internal/menus/report"
	"worklog/internal/menus/settings"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/menus/status"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

func main() {
	assemble.WireHooks()
	a := app.NewWithID("com.eldenz.worklog")
	a.Settings().SetTheme(shell.GoldTheme{Theme: theme.DefaultTheme()})
	a.SetIcon(fyne.NewStaticResource("icon.png", shell.AppIconPNG))
	w := a.NewWindow(shell.WindowTitle)
	// The size to come back to if the window is un-maximised.
	w.Resize(fyne.NewSize(1440, 920))
	w.CenterOnScreen()

	store := backend.NewStore(shell.DataDir())
	cfg, has := store.LoadConfig()
	ui := &shell.UI{
		Store:    store,
		Cfg:      cfg,
		HasCfg:   has,
		Win:      w,
		CalMonth: shell.ThisMonth(),
		RepMonth: shell.CurrentPeriod(),
	}

	assemble.BuildAllTabs(ui)
	// Matched against the tab names, so the names and these have to stay in
	// step: renaming "Log work" to "Log Work" quietly stopped the commit list
	// refreshing when its tab was opened, because nothing matched any more.
	ui.Tabs.OnSelected = func(ti *container.TabItem) {
		if ui.Kanban != nil {
			ui.Kanban.CalendarActive = ti.Text == shell.PlannerTabName
		}
		ui.FinishProgress()
		ui.SyncLoading()
		switch ti.Text {
		case shell.KanbanTabName:
			sprintcalendar.PruneSupportWeeks(ui)
			ui.Kanban.PrunePlan()
			ui.Kanban.Load(false)
		case shell.PlannerTabName:
			sprintcalendar.PruneSupportWeeks(ui)
			sprintcalendar.Maintain(ui.Kanban)
			ui.Kanban.Load(false)
			kanban.Draw(ui.Kanban)
		case shell.LogWorkTabName:
			// Both week tabs carry the support checkbox: drop finished weeks
			// before it is read, and redraw if one went.
			if sprintcalendar.PruneSupportWeeks(ui) {
				logwork.DrawWeekStrip(ui)
			}
			ui.LoadPending(false)
		case shell.LogListTabName:
			if sprintcalendar.PruneSupportWeeks(ui) {
				logwork.DrawWeekStrip(ui)
			}
			ui.LoadPending(false)
			loglist.DrawLogList(ui)
		case shell.CommitListTabName:
			ui.LoadWeekCommits(false)
			commitlist.DrawCommitList(ui)
		case shell.MeetingTabName:
			meeting.LoadMeetingCommits(ui, false)
			meeting.DrawMeeting(ui)
		case shell.StatusTabName:
			status.DrawCalendar(ui)
			ui.LoadStatus(false)
		case shell.ReportTabName:
			ui.RefreshRate(false)
			report.DrawReport(ui)
			ui.LoadReport(false)
		case shell.SettingsTabName:
			settings.LoadGitHubProfile(ui, false)
		}
	}
	w.SetContent(ui.Tabs)

	logwork.DrawRecent(ui)
	if !has {
		ui.Tabs.SelectIndex(shell.SettingsTabIndex) // Settings first run
	}
	// "Log Work" is the tab shown at startup, so OnSelected never fires for it.
	// Kick the first fetch off here, after a beat, so the window is up before
	// the list starts changing under it.
	go func() {
		time.Sleep(750 * time.Millisecond)
		fyne.Do(func() {
			ui.LoadSavedIssueInfos()
			ui.LoadPending(false)
			if sprintcalendar.PruneSupportWeeks(ui) {
				logwork.DrawWeekStrip(ui)
			}
			if ui.Kanban != nil {
				sprintcalendar.Maintain(ui.Kanban)
			}
		})
	}()
	// Maximise once the window is up. fyne 2.8 has no Maximise method, so this
	// asks Windows directly by title — the same effect as clicking the box in
	// the title bar. Left as the ordinary un-maximised size everywhere else,
	// where the platform's own gesture already does the job.
	go func() {
		time.Sleep(200 * time.Millisecond)
		fyne.Do(func() { backend.MaximizeAppWindow(shell.WindowTitle) })
	}()
	w.ShowAndRun()
}
