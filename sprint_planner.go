package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type planBlock struct {
	Date           string
	Start, Minutes int
}

const plannerDurationStep = 15

func plannerDurationUp(minutes int) int {
	if minutes < plannerDurationStep {
		return plannerDurationStep
	}
	return ((minutes + plannerDurationStep - 1) / plannerDurationStep) * plannerDurationStep
}
func validPlannerDuration(minutes int) bool {
	return minutes >= plannerDurationStep && minutes%plannerDurationStep == 0
}
func validatePlannerDuration(block planBlock, previous []planBlock) error {
	if validPlannerDuration(block.Minutes) {
		return nil
	}
	// Existing saved durations are preserved until the user changes them.
	for _, old := range previous {
		if old == block {
			return nil
		}
	}
	return fmt.Errorf("duration must use 15-minute increments (15, 30, 45, …)")
}

type sprintTask struct {
	Estimate          int
	Complexity, Notes string
	Done              bool
	Blocks            []planBlock
}

func plannerIssueFinished(it sprintItem, task sprintTask) bool {
	return task.Done || strings.EqualFold(strings.TrimSpace(it.Status), "Done")
}

type sprintPlan struct {
	Boards         []sprintBoard
	Tasks          map[string]sprintTask
	Fetched        string
	CalendarMode   string    `json:",omitempty"`
	CalendarSprint string    `json:",omitempty"`
	KanbanColumns  *[]string `json:",omitempty"`
	KanbanTypes    *[]string `json:",omitempty"`
}

func (s *Store) loadSprintPlan() (sprintPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := sprintPlan{Tasks: map[string]sprintTask{}}
	raw, err := os.ReadFile(filepath.Join(s.dir, "sprint-planner.json"))
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(raw, &p)
	if p.Tasks == nil {
		p.Tasks = map[string]sprintTask{}
	}
	return p, err
}

func (s *Store) saveSprintPlan(p sprintPlan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(s.dir, ".sprint-planner-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir, "sprint-planner.json"))
}

var sprintComplexities = []string{"XS - 10 mins", "S - 30 mins", "M - 1 hour", "L - 2 hours", "XL - 4 hours", "XXL - 8 hours"}

func sprintEstimate(it sprintItem, t sprintTask) int {
	if t.Estimate > 0 {
		return t.Estimate
	}
	if t.Complexity == "" && it.Effort > 0 {
		return it.Effort
	}
	c := orDefault(t.Complexity, it.Complexity)
	for i, v := range sprintComplexities {
		if strings.EqualFold(c, v) {
			return []int{10, 30, 60, 120, 240, 480}[i]
		}
	}
	return 60
}

type sprintCandidate struct {
	Key    string
	Item   sprintItem
	Sprint sprintIteration
}

// Fill free intervals, preserving existing appointments and splitting large
// tasks across weekdays. A sprint's end is an exclusive boundary.
func allocateSprint(p sprintPlan, candidates []sprintCandidate, date string) (map[string]sprintTask, []string) {
	tasks := map[string]sprintTask{}
	busy := map[string][]planBlock{}
	for key, t := range p.Tasks {
		t.Blocks = append([]planBlock{}, t.Blocks...)
		tasks[key] = t
		if !t.Done {
			for _, b := range t.Blocks {
				busy[b.Date] = append(busy[b.Date], b)
			}
		}
	}
	var unplaced []string
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c.Key] {
			continue
		}
		seen[c.Key] = true
		t := tasks[c.Key]
		if plannerIssueFinished(c.Item, t) || len(t.Blocks) > 0 {
			continue
		}
		remaining := plannerDurationUp(sprintEstimate(c.Item, t))
		start := date
		if start < c.Sprint.StartDate {
			start = c.Sprint.StartDate
		}
		day, err := time.Parse("2006-01-02", start)
		if err != nil {
			continue
		}
		for c.Sprint.contains(day.Format("2006-01-02")) && remaining > 0 {
			d := day.Format("2006-01-02")
			if day.Weekday() != time.Saturday && day.Weekday() != time.Sunday {
				blocks := append([]planBlock{}, busy[d]...)
				sort.Slice(blocks, func(i, j int) bool { return blocks[i].Start < blocks[j].Start })
				cursor := 9 * 60
				blocks = append(blocks, planBlock{Start: 17 * 60})
				for _, b := range blocks {
					end := b.Start
					if end > 17*60 {
						end = 17 * 60
					}
					if cursor < end && remaining > 0 {
						n := (end - cursor) / plannerDurationStep * plannerDurationStep
						if n > remaining {
							n = remaining
						}
						if n > 0 {
							block := planBlock{Date: d, Start: cursor, Minutes: n}
							t.Blocks = append(t.Blocks, block)
							busy[d] = append(busy[d], block)
							remaining -= n
						}
					}
					if b.Start+b.Minutes > cursor {
						cursor = b.Start + b.Minutes
					}
				}
			}
			day = day.AddDate(0, 0, 1)
		}
		// Do not save an incomplete allocation: leave it in the unscheduled list.
		if remaining > 0 {
			for _, b := range t.Blocks {
				list := busy[b.Date]
				for i, x := range list {
					if x == b {
						busy[b.Date] = append(list[:i], list[i+1:]...)
						break
					}
				}
			}
			unplaced = append(unplaced, c.Item.Title)
		} else {
			tasks[c.Key] = t
		}
	}
	return tasks, unplaced
}

func validatePlanBlock(key string, block planBlock, tasks map[string]sprintTask) error {
	if _, err := time.Parse("2006-01-02", block.Date); err != nil {
		return fmt.Errorf("enter a date as YYYY-MM-DD")
	}
	if block.Start < 0 || block.Minutes <= 0 || block.Start+block.Minutes > 24*60 {
		return fmt.Errorf("time and duration must fit within one day")
	}
	for k, t := range tasks {
		if k != key && !t.Done {
			for _, b := range t.Blocks {
				if b.Date == block.Date && block.Start < b.Start+b.Minutes && b.Start < block.Start+block.Minutes {
					return fmt.Errorf("this time overlaps another planned issue")
				}
			}
		}
	}
	return nil
}

func planClock(mins int) string { return fmt.Sprintf("%02d:%02d", mins/60, mins%60) }

func addDays(date string, days int) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, days).Format("2006-01-02")
}

// A drag is accepted only inside the issue's sprint and when its complete
// block fits in a free 09:00–17:00 interval. The lifted block is excluded.
func plannerSlot(p sprintPlan, c sprintCandidate, index int, date string) (planBlock, bool) {
	t := p.Tasks[c.Key]
	if !c.Sprint.contains(date) || plannerIssueFinished(c.Item, t) {
		return planBlock{}, false
	}
	mins := plannerDurationUp(sprintEstimate(c.Item, t))
	if index >= 0 {
		if index >= len(t.Blocks) {
			return planBlock{}, false
		}
		mins = plannerDurationUp(t.Blocks[index].Minutes)
	}
	if mins <= 0 || mins > 480 {
		return planBlock{}, false
	}
	var occupied []planBlock
	for key, task := range p.Tasks {
		if task.Done {
			continue
		}
		for i, b := range task.Blocks {
			if key == c.Key && i == index {
				continue
			}
			if b.Date == date {
				occupied = append(occupied, b)
			}
		}
	}
	sort.Slice(occupied, func(i, j int) bool { return occupied[i].Start < occupied[j].Start })
	occupied = append(occupied, planBlock{Start: 17 * 60})
	cursor := 9 * 60
	for _, b := range occupied {
		end := b.Start
		if end > 17*60 {
			end = 17 * 60
		}
		if end-cursor >= mins {
			return planBlock{Date: date, Start: cursor, Minutes: mins}, true
		}
		if b.Start+b.Minutes > cursor {
			cursor = b.Start + b.Minutes
		}
	}
	return planBlock{}, false
}

func movePlannerIssue(p sprintPlan, c sprintCandidate, index int, date string) (sprintPlan, error) {
	block, ok := plannerSlot(p, c, index, date)
	if !ok {
		return p, fmt.Errorf("choose a highlighted date inside the issue's sprint with enough free time")
	}
	tasks := map[string]sprintTask{}
	for key, t := range p.Tasks {
		tasks[key] = t
	}
	t := tasks[c.Key]
	t.Blocks = append([]planBlock{}, t.Blocks...)
	if index >= 0 {
		t.Blocks[index] = block
	} else {
		t.Blocks = append(t.Blocks, block)
	}
	tasks[c.Key] = t
	p.Tasks = tasks
	return p, nil
}

func resizePlannerBlock(p sprintPlan, key string, index, minutes int) (sprintPlan, error) {
	if !validPlannerDuration(minutes) {
		return p, fmt.Errorf("duration must use 15-minute increments")
	}
	t := p.Tasks[key]
	if index < 0 || index >= len(t.Blocks) || t.Done {
		return p, fmt.Errorf("this time block cannot be resized")
	}
	block := t.Blocks[index]
	block.Minutes = minutes
	if err := validatePlanBlock(key, block, p.Tasks); err != nil {
		return p, err
	}
	for i, b := range t.Blocks {
		if i != index && b.Date == block.Date && b.Start < block.Start+block.Minutes && block.Start < b.Start+b.Minutes {
			return p, fmt.Errorf("this duration overlaps another block for the same issue")
		}
	}
	t.Blocks = append([]planBlock{}, t.Blocks...)
	t.Blocks[index] = block
	tasks := map[string]sprintTask{}
	for k, v := range p.Tasks {
		tasks[k] = v
	}
	tasks[key] = t
	p.Tasks = tasks
	return p, nil
}

// Clear only the selected sprint's visible candidates, retaining local metadata,
// other sprint appointments, and the original plan until Save succeeds.
func clearSprintSchedule(p sprintPlan, candidates []sprintCandidate) (sprintPlan, int) {
	return clearPlannerSchedule(p, candidates, "")
}
func clearPlannerDay(p sprintPlan, candidates []sprintCandidate, date string) (sprintPlan, int) {
	return clearPlannerSchedule(p, candidates, date)
}
func clearPlannerSchedule(p sprintPlan, candidates []sprintCandidate, date string) (sprintPlan, int) {
	tasks := make(map[string]sprintTask, len(p.Tasks))
	for key, t := range p.Tasks {
		tasks[key] = t
	}
	count := 0
	for _, c := range candidates {
		t := tasks[c.Key]
		kept := make([]planBlock, 0, len(t.Blocks))
		for _, b := range t.Blocks {
			if date != "" && b.Date == date || date == "" && c.Sprint.contains(b.Date) {
				count++
			} else {
				kept = append(kept, b)
			}
		}
		if len(kept) != len(t.Blocks) {
			t.Blocks = kept
			tasks[c.Key] = t
		}
	}
	p.Tasks = tasks
	return p, count
}
