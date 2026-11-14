package shell

import (
	"image/color"

	"worklog/internal/shared/backend"
)

// PlannedByDay is the Sprint Calendar's purple figure for each day from..to.
// items are the board worklogs for those days; only those logged minutes cover
// planned time. Unpushed drafts remain separate from the plan.
func (ui *UI) PlannedByDay(from, to string, items []backend.WorklogItem) map[string]int {
	out := map[string]int{}
	if Hooks.PlannedMinutes == nil {
		return out
	}
	logged := itemsByDay(items)
	saved := map[string][]backend.Row{}
	if rows, err := ui.Store.ReadRows(); err == nil {
		for _, r := range ui.ShownRows(rows) {
			if r["pushed_at"] == "" {
				saved[r["date"]] = append(saved[r["date"]], r)
			}
		}
	}
	for ds := from; ds <= to; ds = backend.AddDays(ds, 1) {
		if m := Hooks.PlannedMinutes(ui, ds, logged[ds], saved[ds]); m > 0 {
			out[ds] = m
		}
	}
	return out
}

// SupportPlaceholdersByDay is the support shift pencilled onto each day from..to.
func (ui *UI) SupportPlaceholdersByDay(from, to string, items []backend.WorklogItem) map[string]int {
	out := map[string]int{}
	if Hooks.SupportOn == nil {
		return out
	}
	logged := itemsByDay(items)
	for ds := from; ds <= to; ds = backend.AddDays(ds, 1) {
		if m := SupportPlaceholderFor(ui.Cfg, Hooks.SupportOn(ui, ds), ds, logged[ds]); m > 0 {
			out[ds] = m
		}
	}
	return out
}

func itemsByDay(items []backend.WorklogItem) map[string][]backend.WorklogItem {
	out := map[string][]backend.WorklogItem{}
	for _, it := range items {
		out[it.Date] = append(out[it.Date], it)
	}
	return out
}

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
	for _, r := range ui.ShownDrafts(local) {
		waiting += r.Minutes()
	}
	on := Hooks.SupportOn != nil && Hooks.SupportOn(ui, ds)
	waiting += SupportPlaceholderFor(ui.Cfg, on, ds, items)
	if Hooks.PlannedMinutes != nil {
		waiting += Hooks.PlannedMinutes(ui, ds, items, local)
	}
	return DayFill(mins, waiting)
}
