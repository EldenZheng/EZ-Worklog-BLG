package assemble

import (
	_ "embed"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/menus/commitlist"
	"worklog/internal/menus/kanban"
	"worklog/internal/menus/loglist"
	"worklog/internal/menus/logwork"
	"worklog/internal/menus/meeting"
	"worklog/internal/menus/report"
	"worklog/internal/menus/settings"
	"worklog/internal/menus/status"
	"worklog/internal/shared/shell"
)

// BuildAllTabs assembles the tab bar. Kept apart from main so the wiring
// between tabs — a bar on the report opening a day on the calendar — can be
// exercised without a window manager.
func BuildAllTabs(ui *shell.UI) {
	ui.Tabs = shell.NewPillTabs(
		container.NewTabItem(shell.LogWorkTabName, logwork.BuildLogTab(ui)),
		container.NewTabItem(shell.LogListTabName, loglist.BuildLogListTab(ui)),
		container.NewTabItem(shell.CommitListTabName, commitlist.BuildCommitListTab(ui)),
		container.NewTabItem(shell.MeetingTabName, meeting.BuildMeetingTab(ui)),
		container.NewTabItem(shell.StatusTabName, status.BuildStatusTab(ui)),
		container.NewTabItem(shell.ReportTabName, report.BuildReportTab(ui)),
		container.NewTabItemWithIcon(shell.SettingsTabName, theme.SettingsIcon(), settings.BuildSettingsTab(ui)),
		container.NewTabItem(shell.KanbanTabName, kanban.BuildKanbanTab(ui)),
		container.NewTabItem(shell.PlannerTabName, kanban.BuildPlannerTab(ui)),
	)
}
