package assemble

import (
	"worklog/internal/menus/commitlist"
	"worklog/internal/menus/kanban"
	"worklog/internal/menus/loglist"
	"worklog/internal/menus/logwork"
	"worklog/internal/menus/meeting"
	"worklog/internal/menus/report"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/menus/status"
	"worklog/internal/shared/shell"
)

// WireHooks connects shell.Hooks to the menus that implement them. It must
// run before any window is built.
func WireHooks() {
	shell.Hooks.DrawCalendar = status.DrawCalendar
	shell.Hooks.DrawCommitList = commitlist.DrawCommitList
	shell.Hooks.DrawDayPanel = status.DrawDayPanel
	shell.Hooks.DrawLogLegend = logwork.DrawLogLegend
	shell.Hooks.DrawLogList = loglist.DrawLogList
	shell.Hooks.DrawMeeting = meeting.DrawMeeting
	shell.Hooks.DrawRecent = logwork.DrawRecent
	shell.Hooks.DrawReport = report.DrawReport
	shell.Hooks.DrawWeekStrip = logwork.DrawWeekStrip
	shell.Hooks.OpenPendingIssue = logwork.OpenPendingIssue
	shell.Hooks.DropLoggedSupportWeek = sprintcalendar.DropLoggedSupportWeek
	shell.Hooks.ExportWeekForEmail = logwork.ExportWeekForEmail
	shell.Hooks.KanbanUIBubbleContents = kanban.BubbleContents
	shell.Hooks.KanbanUIBubbleSurface = kanban.BubbleSurface
	shell.Hooks.KanbanUICalendar = sprintcalendar.Calendar
	shell.Hooks.KanbanUIDraw = kanban.Draw
	shell.Hooks.KanbanKick = kanban.Kick
	shell.Hooks.KanbanUIDrawKeepingCalendarScroll = sprintcalendar.DrawKeepingCalendarScroll
	shell.Hooks.KanbanUIEditor = kanban.Editor
	shell.Hooks.KanbanUIMatchesIssueFilter = kanban.MatchesIssueFilter
	shell.Hooks.KanbanUIMove = kanban.Move
	shell.Hooks.KanbanUIOpenCandidate = kanban.OpenCandidate
	shell.Hooks.KanbanUIPlace = kanban.Place
	shell.Hooks.KanbanUIRefreshCalendarSources = sprintcalendar.RefreshCalendarSources
	shell.Hooks.KanbanUIShowDropSlot = kanban.ShowDropSlot
	shell.Hooks.KanbanUISourcesChanged = kanban.SourcesChanged
	shell.Hooks.KanbanUIStartPlannedToday = sprintcalendar.StartPlannedToday
	shell.Hooks.MarkDropTargets = logwork.MarkDropTargets
	shell.Hooks.PlannedMinutes = sprintcalendar.PlannedMinutes
	shell.Hooks.PlannedLines = sprintcalendar.PlannedLines
	shell.Hooks.RenderPending = logwork.RenderPending
	shell.Hooks.SetSupport = sprintcalendar.SetSupport
	shell.Hooks.SupportOn = sprintcalendar.SupportOn
	shell.Hooks.WeekBoardsPanel = logwork.WeekBoardsPanel
	shell.Hooks.WeekIssuesPanel = logwork.WeekIssuesPanel
	shell.Hooks.WeekWorklogs = logwork.WeekWorklogs
}
