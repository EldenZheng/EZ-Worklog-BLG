package shell

import (
	"fyne.io/fyne/v2"

	"worklog/internal/shared/backend"
)

// Hooks lets shared code reach functions that live in a menu package
// without importing it. main wires every hook before the window opens.
var Hooks struct {
	DrawCalendar                      func(*UI)
	DrawCommitList                    func(*UI)
	DrawDayPanel                      func(*UI)
	DrawLogLegend                     func(*UI)
	DrawLogList                       func(*UI)
	DrawRecent                        func(*UI)
	DrawReport                        func(*UI)
	DrawWeekStrip                     func(*UI)
	DropLoggedSupportWeek             func(*UI, string, map[string][]backend.WorklogItem, string)
	ExportWeekForEmail                func(*UI, []string, map[string][]backend.WorklogItem, string)
	KanbanUIBubbleContents            func(*KanbanUI, fyne.CanvasObject, fyne.CanvasObject, string) fyne.CanvasObject
	KanbanUIBubbleSurface             func(*KanbanUI, backend.SprintBoard, backend.SprintItem, fyne.CanvasObject) fyne.CanvasObject
	KanbanUICalendar                  func(*KanbanUI) fyne.CanvasObject
	KanbanUIDraw                      func(*KanbanUI)
	KanbanKick                        func(*KanbanUI)
	KanbanUIDrawKeepingCalendarScroll func(*KanbanUI)
	KanbanUIEditor                    func(*KanbanUI, backend.SprintBoard, backend.SprintItem)
	KanbanUIMatchesIssueFilter        func(*KanbanUI, backend.SprintBoard, backend.SprintItem) bool
	KanbanUIMove                      func(*KanbanUI, backend.SprintBoard, backend.SprintItem, string, string)
	KanbanUIOpenCandidate             func(*KanbanUI, backend.SprintCandidate)
	KanbanUIPlace                     func(*KanbanUI, backend.SprintBoard, backend.SprintItem, string, KanbanSlot)
	KanbanUIRefreshCalendarSources    func(*KanbanUI)
	KanbanUIShowDropSlot              func(*KanbanUI, *SprintDragCard, *KanbanTarget, fyne.Position)
	KanbanUISourcesChanged            func(*KanbanUI)
	KanbanUIStartPlannedToday         func(*KanbanUI)
	MarkDropTargets                   func(*UI)
	PlannedMinutes                    func(*UI, string, []backend.WorklogItem, []backend.Row) int
	RenderPending                     func(*UI)
	SetSupport                        func(*UI, bool, ...string)
	SupportOn                         func(*UI, string) bool
	WeekBoardsPanel                   func(*UI, string, string) fyne.CanvasObject
	WeekIssuesPanel                   func(*UI, []backend.WorklogItem, string) fyne.CanvasObject
	WeekWorklogs                      func(*UI, []string) (map[string][]backend.WorklogItem, string)
}
