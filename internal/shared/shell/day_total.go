package shell

import (
	"image/color"

	"worklog/internal/shared/backend"
)

// DayTotalFill is the colour behind a Log List day from everything standing on
// it, the way Log Work and the Sprint Calendar paint theirs: green once logged,
// drafts, the support placeholder and planned time together reach the target.
// items are the day's board worklogs, local its unpushed drafts.
func (ui *UI) DayTotalFill(ds string, items []backend.WorklogItem, local []backend.Row) color.Color {
	mins := 0
	for _, v := range MinutesByOrg(items) {
		mins += v
	}
	waiting := 0
	for _, r := range local {
		waiting += r.Minutes()
	}
	on := Hooks.SupportOn != nil && Hooks.SupportOn(ui, ds)
	waiting += SupportPlaceholderFor(ui.Cfg, on, ds, items)
	if Hooks.PlannedMinutes != nil {
		waiting += Hooks.PlannedMinutes(ui, ds, items, local)
	}
	return DayFill(mins, waiting)
}
