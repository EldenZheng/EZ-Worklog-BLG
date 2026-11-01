package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
)

// ---- housekeeping for the local sprint plan ----

// Status names on the sprint boards. A planned issue still sitting in Created
// on the day it is planned for is started automatically.
const (
	plannerCreatedStatus    = "Created"
	plannerInProgressStatus = "In Progress"
)

// pruneSprintPlan drops what can no longer be used: time blocks on days that
// have passed, and task entries left with nothing in them.
//
// An entry with local metadata (estimate, complexity, notes, done) is kept while
// its issue is still on a cached board, because the tray and the allocator read
// it. Once the issue has left every board (Done, Live, closed) and has no block
// today or later, nothing will read it again. The orphan rule is skipped when no
// boards are cached at all, so a failed first load cannot wipe every estimate.
func pruneSprintPlan(p sprintPlan, date string) (sprintPlan, bool) {
	onBoard := map[string]bool{}
	for _, b := range p.Boards {
		for _, it := range b.Items {
			onBoard[it.key(b.ID)] = true
		}
	}
	changed := false
	tasks := make(map[string]sprintTask, len(p.Tasks))
	for key, t := range p.Tasks {
		kept := make([]planBlock, 0, len(t.Blocks))
		for _, b := range t.Blocks {
			if b.Date >= date {
				kept = append(kept, b)
			}
		}
		if len(kept) != len(t.Blocks) {
			changed = true
			t.Blocks = kept
		}
		hasMeta := t.Estimate != 0 || t.Complexity != "" || t.Notes != "" || t.Done
		switch {
		case len(t.Blocks) > 0:
		case !hasMeta:
			changed = true
			continue
		case len(p.Boards) > 0 && !onBoard[key]:
			changed = true
			continue
		}
		tasks[key] = t
	}
	p.Tasks = tasks
	return p, changed
}

// plannerStartCandidate is a board item planned for a day and still in Created.
type plannerStartCandidate struct {
	board sprintBoard
	item  sprintItem
}

// plannedTodayStillCreated lists items with a block on date whose board status
// is Created, and whose board offers In Progress to move them to.
func plannedTodayStillCreated(p sprintPlan, date string) []plannerStartCandidate {
	var out []plannerStartCandidate
	for _, b := range p.Boards {
		if !boardHasStatus(b, plannerInProgressStatus) {
			continue
		}
		for _, it := range b.Items {
			if !strings.EqualFold(strings.TrimSpace(it.Status), plannerCreatedStatus) {
				continue
			}
			t, ok := p.Tasks[it.key(b.ID)]
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

func boardHasStatus(b sprintBoard, name string) bool {
	for _, o := range b.field("Status").Options {
		if o.Name == name {
			return true
		}
	}
	return false
}

// maintain runs when the app opens and whenever Sprint Calendar is opened (so
// an app left open overnight catches up too). It prunes the local plan and,
// when an issue planned for today is still Created and has not been tried this
// session, fetches the boards so the status change is decided on fresh data
// rather than on whatever was cached last time. Kanban only prunes: there,
// status changes come from dragging a card.
func (k *kanbanUI) maintain() {
	k.prunePlan()
	for _, c := range plannedTodayStillCreated(k.plan, today()) {
		if !k.autoStarted[c.item.key(c.board.ID)] {
			// A GitHub write in flight blocks the fetch; leave nothing pending
			// that a later Kanban refresh could pick up.
			k.autoStartPending = !k.busy
			k.load(true)
			return
		}
	}
}

// prunePlan saves a pruned plan if pruning removed anything.
func (k *kanbanUI) prunePlan() {
	if k.storageErr != nil {
		return
	}
	if p, changed := pruneSprintPlan(k.plan, today()); changed {
		k.save(p)
	}
}

// startPlannedToday moves issues planned for today from Created to In Progress
// on GitHub. Called only after a fresh board fetch, so a status someone changed
// since the cache was written is never overwritten. Each issue is tried at most
// once per session: if it is moved back to Created by hand, that stands.
func (k *kanbanUI) startPlannedToday() {
	if k.busy || k.storageErr != nil {
		return
	}
	if k.autoStarted == nil {
		k.autoStarted = map[string]bool{}
	}
	var todo []plannerStartCandidate
	for _, c := range plannedTodayStillCreated(k.plan, today()) {
		key := c.item.key(c.board.ID)
		if !k.autoStarted[key] {
			k.autoStarted[key] = true
			todo = append(todo, c)
		}
	}
	if len(todo) == 0 {
		return
	}
	k.busy = true
	for _, c := range todo {
		k.setPending(c.board, c.item, "status", plannerInProgressStatus)
	}
	k.setMessage(fmt.Sprintf("Moving %d issue(s) planned for today to %s on GitHub…", len(todo), plannerInProgressStatus))
	k.draw()
	go func() {
		var moved []plannerStartCandidate
		var failures []string
		for _, c := range todo {
			if err := pushSprintItem(c.board, c.item, "status", plannerInProgressStatus); err != nil {
				failures = append(failures, c.item.Title+": "+err.Error())
				continue
			}
			moved = append(moved, c)
		}
		fyne.Do(func() {
			k.busy = false
			for _, c := range todo {
				k.clearPending(c.board, c.item)
			}
			for _, c := range moved {
				for bi := range k.plan.Boards {
					if k.plan.Boards[bi].ID != c.board.ID {
						continue
					}
					for ii := range k.plan.Boards[bi].Items {
						if k.plan.Boards[bi].Items[ii].ID == c.item.ID {
							k.plan.Boards[bi].Items[ii].Status = plannerInProgressStatus
						}
					}
				}
			}
			if len(moved) > 0 {
				k.save(k.plan)
			}
			msg := fmt.Sprintf("Moved %d issue(s) planned for today to %s.", len(moved), plannerInProgressStatus)
			if len(failures) > 0 {
				msg += " Failed: " + strings.Join(failures, "; ")
			}
			k.setMessage(msg)
			k.draw()
		})
	}()
}

// ---- the weekday support placeholder, remembered per week ----

// The placeholder is saved as the Monday of each week it is ticked for, in a
// file of its own (a few dozen bytes). A week is dropped once its Friday has
// passed, or once every weekday in it already holds a logged support shift —
// past that point it would pencil nothing in. The file is removed when empty.

const supportWeeksFile = "support-weeks.json"

func (s *Store) loadSupportWeeks() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]bool{}
	raw, err := os.ReadFile(filepath.Join(s.dir, supportWeeksFile))
	if err != nil {
		return out
	}
	var weeks []string
	if json.Unmarshal(raw, &weeks) == nil {
		for _, w := range weeks {
			if validPlannerDate(w) {
				out[weekStartOf(w)] = true
			}
		}
	}
	return out
}

func (s *Store) saveSupportWeeks(weeks map[string]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, supportWeeksFile)
	list := make([]string, 0, len(weeks))
	for w, on := range weeks {
		if on {
			list = append(list, w)
		}
	}
	if len(list) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	sort.Strings(list)
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func (ui *UI) ensureSupportWeeks() {
	if ui.supportWeeks == nil && ui.store != nil {
		ui.supportWeeks = ui.store.loadSupportWeeks()
	}
	if ui.supportWeeks == nil {
		ui.supportWeeks = map[string]bool{}
	}
}

// supportOn reports whether the placeholder is ticked for the week holding date.
func (ui *UI) supportOn(date string) bool {
	ui.ensureSupportWeeks()
	return ui.supportWeeks[weekStartOf(date)]
}

// setSupport ticks or clears the placeholder for the weeks holding dates.
func (ui *UI) setSupport(on bool, dates ...string) {
	ui.ensureSupportWeeks()
	changed := false
	for _, d := range dates {
		w := weekStartOf(d)
		if ui.supportWeeks[w] != on {
			changed = true
			if on {
				ui.supportWeeks[w] = true
			} else {
				delete(ui.supportWeeks, w)
			}
		}
	}
	if changed {
		ui.persistSupportWeeks()
	}
}

func (ui *UI) persistSupportWeeks() {
	if ui.store == nil {
		return
	}
	if err := ui.store.saveSupportWeeks(ui.supportWeeks); err != nil && ui.win != nil {
		ui.errf(err)
	}
}

// supportWeekFinished: the rota is weekdays, so a week is over once its Friday
// has passed.
func supportWeekFinished(week, date string) bool {
	return addDays(weekStartOf(week), 4) < date
}

// pruneSupportWeeks drops saved weeks whose Friday has passed, reporting
// whether any went.
func (ui *UI) pruneSupportWeeks() bool {
	ui.ensureSupportWeeks()
	changed := false
	for w := range ui.supportWeeks {
		if supportWeekFinished(w, today()) {
			delete(ui.supportWeeks, w)
			changed = true
		}
	}
	if changed {
		ui.persistSupportWeeks()
	}
	return changed
}

// dropLoggedSupportWeek clears a saved week once every weekday in it already
// holds a logged support shift. Only decided on fully loaded board data.
func (ui *UI) dropLoggedSupportWeek(week string, byDay map[string][]WorklogItem, state string) {
	if state != "ok" || !ui.supportOn(week) {
		return
	}
	for i, d := range weekDates(weekStartOf(week)) {
		if i < 5 && !hasWeeklySupport(ui.cfg, byDay[d]) {
			return
		}
	}
	ui.setSupport(false, week)
}

// supportDates lists the Mondays from from to to, one per week touched.
func supportDates(from, to string) []string {
	if !validPlannerDate(from) || !validPlannerDate(to) {
		return nil
	}
	var out []string
	for w := weekStartOf(from); w <= to; w = addDays(w, 7) {
		out = append(out, w)
	}
	return out
}
