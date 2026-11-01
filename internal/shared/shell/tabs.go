package shell

// The tabs, named once. AppTabs offers no way to reach a tab except by name or
// by position, so both are spelled out here rather than written out again at
// each place that needs one — a rename or a new tab in the middle used to
// silently break whichever switch or SelectIndex was left behind.
const (
	LogWorkTabName    = "Log Work"
	LogListTabName    = "Log List"
	CommitListTabName = "Commit List"
	MeetingTabName    = "Meeting update"
	StatusTabName     = "Status"
	ReportTabName     = "Report"
	SettingsTabName   = "Settings"

	StatusTabIndex   = 4
	SettingsTabIndex = 6
)

const KanbanTabName = "Kanban"

const PlannerTabName = "Sprint Calendar"
