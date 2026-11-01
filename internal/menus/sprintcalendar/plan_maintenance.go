package sprintcalendar

import (
	"fmt"
	"strings"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"

	"fyne.io/fyne/v2"
)

// ---- housekeeping for the local sprint plan ----

// Status names on the sprint boards. A planned issue still sitting in Created
// on the day it is planned for is started automatically.
const (
	plannerCreatedStatus    = "Created"
	plannerInProgressStatus = "In Progress"
)

// plannerStartCandidate is a board item planned for a day and still in Created.
type plannerStartCandidate struct {
	board backend.SprintBoard
	Item  backend.SprintItem
}

// PlannedTodayStillCreated lists items with a block on date whose board status
// is Created, and whose board offers In Progress to move them to.
func PlannedTodayStillCreated(p backend.SprintPlan, date string) []plannerStartCandidate {
	var out []plannerStartCandidate
	for _, b := range p.Boards {
		if !boardHasStatus(b, plannerInProgressStatus) {
			continue
		}
		for _, it := range b.Items {
			if !strings.EqualFold(strings.TrimSpace(it.Status), plannerCreatedStatus) {
				continue
			}
			t, ok := p.Tasks[it.Key(b.ID)]
			if !ok || t.Done {
				continue
			}
			for _, block := range t.Blocks {
				if block.Date == date {
					out = append(out, plannerStartCandidate{b, it})
					break
				}
			}
		}
	}
	return out
}

func boardHasStatus(b backend.SprintBoard, name string) bool {
	for _, o := range b.Field("Status").Options {
		if o.Name == name {
			return true
		}
	}
	return false
}

// Maintain runs when the app opens and whenever Sprint Calendar is opened (so
// an app left open overnight catches up too). It prunes the local plan and,
// when an issue planned for today is still Created and has not been tried this
// session, fetches the boards so the status change is decided on fresh data
// rather than on whatever was cached last time. Kanban only prunes: there,
// status changes come from dragging a card.
func Maintain(k *shell.KanbanUI) {
	k.PrunePlan()
	for _, c := range PlannedTodayStillCreated(k.Plan, shell.Today()) {
		if !k.AutoStarted[c.Item.Key(c.board.ID)] {
			// A GitHub write in flight blocks the fetch; leave nothing pending
			// that a later Kanban refresh could pick up.
			k.AutoStartPending = !k.Busy
			k.Load(true)
			return
		}
	}
}

// StartPlannedToday moves issues planned for today from Created to In Progress
// on GitHub. Called only after a fresh board fetch, so a status someone changed
// since the cache was written is never overwritten. Each issue is tried at most
// once per session: if it is moved back to Created by hand, that stands.
func StartPlannedToday(k *shell.KanbanUI) {
	if k.Busy || k.StorageErr != nil {
		return
	}
	if k.AutoStarted == nil {
		k.AutoStarted = map[string]bool{}
	}
	var todo []plannerStartCandidate
	for _, c := range PlannedTodayStillCreated(k.Plan, shell.Today()) {
		key := c.Item.Key(c.board.ID)
		if !k.AutoStarted[key] {
			k.AutoStarted[key] = true
			todo = append(todo, c)
		}
	}
	if len(todo) == 0 {
		return
	}
	k.Busy = true
	for _, c := range todo {
		k.SetPending(c.board, c.Item, "status", plannerInProgressStatus)
	}
	k.SetMessage(fmt.Sprintf("Moving %d issue(s) planned for today to %s on GitHub…", len(todo), plannerInProgressStatus))
	shell.Hooks.KanbanUIDraw(k)
	go func() {
		var moved []plannerStartCandidate
		var failures []string
		for _, c := range todo {
			if err := backend.PushSprintItem(c.board, c.Item, "status", plannerInProgressStatus); err != nil {
				failures = append(failures, c.Item.Title+": "+err.Error())
				continue
			}
			moved = append(moved, c)
		}
		fyne.Do(func() {
			k.Busy = false
			for _, c := range todo {
				k.ClearPending(c.board, c.Item)
			}
			for _, c := range moved {
				for bi := range k.Plan.Boards {
					if k.Plan.Boards[bi].ID != c.board.ID {
						continue
					}
					for ii := range k.Plan.Boards[bi].Items {
						if k.Plan.Boards[bi].Items[ii].ID == c.Item.ID {
							k.Plan.Boards[bi].Items[ii].Status = plannerInProgressStatus
						}
					}
				}
			}
			if len(moved) > 0 {
				k.Save(k.Plan)
			}
			msg := fmt.Sprintf("Moved %d issue(s) planned for today to %s.", len(moved), plannerInProgressStatus)
			if len(failures) > 0 {
				msg += " Failed: " + strings.Join(failures, "; ")
			}
			k.SetMessage(msg)
			shell.Hooks.KanbanUIDraw(k)
		})
	}()
}

func ensureSupportWeeks(ui *shell.UI) {
	if ui.SupportWeeks == nil && ui.Store != nil {
		ui.SupportWeeks = ui.Store.LoadSupportWeeks()
	}
	if ui.SupportWeeks == nil {
		ui.SupportWeeks = map[string]bool{}
	}
}

// SupportOn reports whether the placeholder is ticked for the week holding date.
func SupportOn(ui *shell.UI, date string) bool {
	ensureSupportWeeks(ui)
	return ui.SupportWeeks[backend.WeekStartOf(date)]
}

// SetSupport ticks or clears the placeholder for the weeks holding dates.
func SetSupport(ui *shell.UI, on bool, dates ...string) {
	ensureSupportWeeks(ui)
	changed := false
	for _, d := range dates {
		w := backend.WeekStartOf(d)
		if ui.SupportWeeks[w] != on {
			changed = true
			if on {
				ui.SupportWeeks[w] = true
			} else {
				delete(ui.SupportWeeks, w)
			}
		}
	}
	if changed {
		persistSupportWeeks(ui)
	}
}

func persistSupportWeeks(ui *shell.UI) {
	if ui.Store == nil {
		return
	}
	if err := ui.Store.SaveSupportWeeks(ui.SupportWeeks); err != nil && ui.Win != nil {
		ui.Errf(err)
	}
}

// SupportWeekFinished: the rota is weekdays, so a week is over once its Friday
// has passed.
func SupportWeekFinished(week, date string) bool {
	return backend.AddDays(backend.WeekStartOf(week), 4) < date
}

// PruneSupportWeeks drops saved weeks whose Friday has passed, reporting
// whether any went.
func PruneSupportWeeks(ui *shell.UI) bool {
	ensureSupportWeeks(ui)
	changed := false
	for w := range ui.SupportWeeks {
		if SupportWeekFinished(w, shell.Today()) {
			delete(ui.SupportWeeks, w)
			changed = true
		}
	}
	if changed {
		persistSupportWeeks(ui)
	}
	return changed
}

// DropLoggedSupportWeek clears a saved week once every weekday in it already
// holds a logged support shift. Only decided on fully loaded board data.
func DropLoggedSupportWeek(ui *shell.UI, week string, byDay map[string][]backend.WorklogItem, state string) {
	if state != "ok" || !SupportOn(ui, week) {
		return
	}
	for i, d := range shell.WeekDates(backend.WeekStartOf(week)) {
		if i < 5 && !shell.HasWeeklySupport(ui.Cfg, byDay[d]) {
			return
		}
	}
	SetSupport(ui, false, week)
}

// supportDates lists the Mondays from from to to, one per week touched.
func supportDates(from, to string) []string {
	if !backend.ValidPlannerDate(from) || !backend.ValidPlannerDate(to) {
		return nil
	}
	var out []string
	for w := backend.WeekStartOf(from); w <= to; w = backend.AddDays(w, 7) {
		out = append(out, w)
	}
	return out
}
