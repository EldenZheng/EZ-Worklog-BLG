package main

import (
	_ "embed"
	"fmt"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

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
)

func main() {
	assemble.WireHooks()
	// The binary the last update replaced was kept running until this one
	// started; nothing uses it now.
	if exe, err := backend.ExecutablePath(); err == nil {
		backend.RemovePreviousBinary(exe)
	}
	a := app.NewWithID("com.eldenz.worklog")
	a.Settings().SetTheme(shell.GoldTheme{Theme: theme.DefaultTheme()})
	a.SetIcon(fyne.NewStaticResource("icon.png", shell.AppIconPNG))
	w := a.NewWindow(shell.WindowTitle)
	// The size to come back to if the window is un-maximised. Only Windows gets
	// maximised below, so elsewhere this is the size it opens at: small enough
	// for a laptop's screen, since Fyne cannot say how big the screen is.
	size := fyne.NewSize(1440, 920)
	if runtime.GOOS != "windows" {
		size = fyne.NewSize(1200, 720)
	}
	w.Resize(size)
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
			// Opening the tab brings the cards in again, top to bottom. Boards
			// still loading bring them in when they land.
			ui.Kanban.CardsShown = ""
			ui.Kanban.Load(false)
			if ui.Kanban.Loaded {
				kanban.Draw(ui.Kanban)
			}
		case shell.PlannerTabName:
			sprintcalendar.PruneSupportWeeks(ui)
			sprintcalendar.Maintain(ui.Kanban)
			ui.Kanban.Load(false)
			ui.ReplayArrival("planner") // its days arrive again
			kanban.Draw(ui.Kanban)
		case shell.LogWorkTabName:
			// Both week tabs carry the support checkbox: drop finished weeks
			// before it is read. Redrawn either way, so the week arrives again.
			sprintcalendar.PruneSupportWeeks(ui)
			ui.ReplayArrival("week")
			logwork.DrawWeekStrip(ui)
			if ui.PendingLoaded {
				ui.ReplayArrival("pending")
				logwork.RenderPending(ui)
			}
			ui.LoadPending(false)
		case shell.LogListTabName:
			if sprintcalendar.PruneSupportWeeks(ui) {
				logwork.DrawWeekStrip(ui)
			}
			ui.LoadPending(false)
			ui.ReplayArrival("loglist")
			loglist.DrawLogList(ui)
		case shell.CommitListTabName:
			ui.LoadWeekCommits(false)
			ui.ReplayArrival("commits")
			commitlist.DrawCommitList(ui)
		case shell.MeetingTabName:
			meeting.LoadMeetingCommits(ui, false)
			ui.ReplayArrival("meeting")
			meeting.DrawMeeting(ui)
		case shell.StatusTabName:
			// Opening the tab plays the month's arrival again, and the day's.
			ui.ReplayArrival("status")
			ui.ReplayArrival("statusday")
			status.DrawCalendar(ui)
			ui.LoadStatus(false)
		case shell.ReportTabName:
			ui.RefreshRate(false)
			ui.ReplayArrival("report")
			report.DrawReport(ui)
			ui.LoadReport(false)
		case shell.SettingsTabName:
			settings.LoadGitHubProfile(ui, false)
		}
	}
	shell.FitScreen(w, ui.Tabs)
	// Kanban changes still queued for GitHub are lost if the app quits, so
	// ask first.
	w.SetCloseIntercept(func() {
		n := 0
		if ui.Kanban != nil {
			n = ui.Kanban.PendingWrites()
		}
		if n == 0 {
			w.Close()
			return
		}
		// Built from the shared buttons rather than Fyne's stock confirm, whose Yes
		// is Fyne's default blue. Closing anyway throws the changes away, so it
		// is the red removal pill.
		msg := widget.NewLabel(fmt.Sprintf("%d Kanban change(s) have not reached GitHub yet. Close anyway and lose them?", n))
		msg.Wrapping = fyne.TextWrapWord
		var d dialog.Dialog
		keep := widget.NewButton("Keep waiting", func() { shell.CloseDialog(d, w) })
		quit := widget.NewButton("Close anyway", func() { w.Close() })
		d = dialog.NewCustomWithoutButtons("Changes still saving",
			container.NewVBox(msg, shell.DialogButtons(keep, shell.DestructiveButton(quit))), w)
		d.Resize(fyne.NewSize(480, d.MinSize().Height))
		shell.ShowDialog(d, w)
	})

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
			// Quiet: it only shows itself if there is an update to offer.
			settings.CheckForUpdates(ui, false)
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
