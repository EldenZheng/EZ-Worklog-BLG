package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PruneSprintPlan drops what can no longer be used: time blocks on days that
// have passed, and task entries left with nothing in them.
//
// An entry with local metadata (estimate, complexity, notes, done) is kept while
// its issue is still on a cached board, because the tray and the allocator read
// it. Once the issue has left every board (Done, Live, closed) and has no block
// today or later, nothing will read it again. The orphan rule is skipped when no
// boards are cached at all, so a failed first load cannot wipe every estimate.
func PruneSprintPlan(p SprintPlan, date string) (SprintPlan, bool) {
	onBoard := map[string]bool{}
	for _, b := range p.Boards {
		for _, it := range b.Items {
			onBoard[it.Key(b.ID)] = true
		}
	}
	changed := false
	tasks := make(map[string]SprintTask, len(p.Tasks))
	for key, t := range p.Tasks {
		kept := make([]PlanBlock, 0, len(t.Blocks))
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

type PlanBlock struct {
	Date           string
	Start, Minutes int
}

const PlannerDurationStep = 15

func PlannerDurationUp(minutes int) int {
	if minutes < PlannerDurationStep {
		return PlannerDurationStep
	}
	return ((minutes + PlannerDurationStep - 1) / PlannerDurationStep) * PlannerDurationStep
}

func ValidPlannerDuration(minutes int) bool {
	return minutes >= PlannerDurationStep && minutes%PlannerDurationStep == 0
}

func ValidatePlannerDuration(block PlanBlock, previous []PlanBlock) error {
	if ValidPlannerDuration(block.Minutes) {
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

type SprintTask struct {
	Estimate          int
	Complexity, Notes string
	Done              bool
	Blocks            []PlanBlock
}

func PlannerIssueFinished(it SprintItem, task SprintTask) bool {
	return task.Done || strings.EqualFold(strings.TrimSpace(it.Status), "Done")
}

type SprintPlan struct {
	// Boards and Fetched live in memory only: the boards are refetched from
	// GitHub each run, so saving them would only grow sprint-planner.json.
	Boards         []SprintBoard `json:"-"`
	Tasks          map[string]SprintTask
	Fetched        string    `json:"-"`
	CalendarMode   string    `json:",omitempty"`
	CalendarSprint string    `json:",omitempty"`
	KanbanColumns  *[]string `json:",omitempty"`
	KanbanTypes    *[]string `json:",omitempty"`
	// ProjectOrder is the board IDs in the order the Kanban and Sprint Calendar
	// legends show them.
	ProjectOrder []string `json:",omitempty"`
}

func (s *Store) LoadSprintPlan() (SprintPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := SprintPlan{Tasks: map[string]SprintTask{}}
	raw, err := os.ReadFile(filepath.Join(s.Dir, "sprint-planner.json"))
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(raw, &p)
	if p.Tasks == nil {
		p.Tasks = map[string]SprintTask{}
	}
	return p, err
}

func (s *Store) SaveSprintPlan(p SprintPlan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(s.Dir, ".sprint-planner-*")
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
	return os.Rename(tmp, filepath.Join(s.Dir, "sprint-planner.json"))
}

var SprintComplexities = []string{"XS - 10 mins", "S - 30 mins", "M - 1 hour", "L - 2 hours", "XL - 4 hours", "XXL - 8 hours"}

func SprintEstimate(it SprintItem, t SprintTask) int {
	if t.Estimate > 0 {
		return t.Estimate
	}
	if t.Complexity == "" && it.Effort > 0 {
		return it.Effort
	}
	c := OrDefault(t.Complexity, it.Complexity)
	for i, v := range SprintComplexities {
		if strings.EqualFold(c, v) {
			return []int{10, 30, 60, 120, 240, 480}[i]
		}
	}
	return 60
}

type SprintCandidate struct {
	Key    string
	Item   SprintItem
	Sprint SprintIteration
}

// Fill free intervals, preserving existing appointments and splitting large
// tasks across weekdays. A sprint's end is an exclusive boundary.
func AllocateSprint(p SprintPlan, candidates []SprintCandidate, date string) (map[string]SprintTask, []string) {
	tasks := map[string]SprintTask{}
	busy := map[string][]PlanBlock{}
	for key, t := range p.Tasks {
		t.Blocks = append([]PlanBlock{}, t.Blocks...)
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
		if PlannerIssueFinished(c.Item, t) || len(t.Blocks) > 0 {
			continue
		}
		remaining := PlannerDurationUp(SprintEstimate(c.Item, t))
		start := date
		if start < c.Sprint.StartDate {
			start = c.Sprint.StartDate
		}
		day, err := time.Parse("2006-01-02", start)
		if err != nil {
			continue
		}
		for c.Sprint.Contains(day.Format("2006-01-02")) && remaining > 0 {
			d := day.Format("2006-01-02")
			if day.Weekday() != time.Saturday && day.Weekday() != time.Sunday {
				blocks := append([]PlanBlock{}, busy[d]...)
				sort.Slice(blocks, func(i, j int) bool { return blocks[i].Start < blocks[j].Start })
				cursor := 9 * 60
				blocks = append(blocks, PlanBlock{Start: 17 * 60})
				for _, b := range blocks {
					end := b.Start
					if end > 17*60 {
						end = 17 * 60
					}
					if cursor < end && remaining > 0 {
						n := (end - cursor) / PlannerDurationStep * PlannerDurationStep
						if n > remaining {
							n = remaining
						}
						if n > 0 {
							block := PlanBlock{Date: d, Start: cursor, Minutes: n}
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

func ValidatePlanBlock(key string, block PlanBlock, tasks map[string]SprintTask) error {
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

func PlanClock(mins int) string { return fmt.Sprintf("%02d:%02d", mins/60, mins%60) }

func AddDays(date string, days int) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, days).Format("2006-01-02")
}

// A drag is accepted only inside the issue's sprint and when its complete
// block fits in a free 09:00–17:00 interval. The lifted block is excluded.
func PlannerSlot(p SprintPlan, c SprintCandidate, index int, date string) (PlanBlock, bool) {
	t := p.Tasks[c.Key]
	if !c.Sprint.Contains(date) || PlannerIssueFinished(c.Item, t) {
		return PlanBlock{}, false
	}
	mins := PlannerDurationUp(SprintEstimate(c.Item, t))
	if index >= 0 {
		if index >= len(t.Blocks) {
			return PlanBlock{}, false
		}
		mins = PlannerDurationUp(t.Blocks[index].Minutes)
	}
	if mins <= 0 || mins > 480 {
		return PlanBlock{}, false
	}
	var occupied []PlanBlock
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
	occupied = append(occupied, PlanBlock{Start: 17 * 60})
	cursor := 9 * 60
	for _, b := range occupied {
		end := b.Start
		if end > 17*60 {
			end = 17 * 60
		}
		if end-cursor >= mins {
			return PlanBlock{Date: date, Start: cursor, Minutes: mins}, true
		}
		if b.Start+b.Minutes > cursor {
			cursor = b.Start + b.Minutes
		}
	}
	return PlanBlock{}, false
}

func MovePlannerIssue(p SprintPlan, c SprintCandidate, index int, date string) (SprintPlan, error) {
	block, ok := PlannerSlot(p, c, index, date)
	if !ok {
		return p, fmt.Errorf("choose a highlighted date inside the issue's sprint with enough free time")
	}
	tasks := map[string]SprintTask{}
	for key, t := range p.Tasks {
		tasks[key] = t
	}
	t := tasks[c.Key]
	t.Blocks = append([]PlanBlock{}, t.Blocks...)
	if index >= 0 {
		t.Blocks[index] = block
	} else {
		t.Blocks = append(t.Blocks, block)
	}
	tasks[c.Key] = t
	p.Tasks = tasks
	return p, nil
}

func ResizePlannerBlock(p SprintPlan, key string, index, minutes int) (SprintPlan, error) {
	if !ValidPlannerDuration(minutes) {
		return p, fmt.Errorf("duration must use 15-minute increments")
	}
	t := p.Tasks[key]
	if index < 0 || index >= len(t.Blocks) || t.Done {
		return p, fmt.Errorf("this time block cannot be resized")
	}
	block := t.Blocks[index]
	block.Minutes = minutes
	if err := ValidatePlanBlock(key, block, p.Tasks); err != nil {
		return p, err
	}
	for i, b := range t.Blocks {
		if i != index && b.Date == block.Date && b.Start < block.Start+block.Minutes && block.Start < b.Start+b.Minutes {
			return p, fmt.Errorf("this duration overlaps another block for the same issue")
		}
	}
	t.Blocks = append([]PlanBlock{}, t.Blocks...)
	t.Blocks[index] = block
	tasks := map[string]SprintTask{}
	for k, v := range p.Tasks {
		tasks[k] = v
	}
	tasks[key] = t
	p.Tasks = tasks
	return p, nil
}

// Clear only the selected sprint's visible candidates, retaining local metadata,
// other sprint appointments, and the original plan until Save succeeds.
func ClearSprintSchedule(p SprintPlan, candidates []SprintCandidate) (SprintPlan, int) {
	return clearPlannerSchedule(p, candidates, "")
}

func ClearPlannerDay(p SprintPlan, candidates []SprintCandidate, date string) (SprintPlan, int) {
	return clearPlannerSchedule(p, candidates, date)
}

func clearPlannerSchedule(p SprintPlan, candidates []SprintCandidate, date string) (SprintPlan, int) {
	tasks := make(map[string]SprintTask, len(p.Tasks))
	for key, t := range p.Tasks {
		tasks[key] = t
	}
	count := 0
	for _, c := range candidates {
		t := tasks[c.Key]
		kept := make([]PlanBlock, 0, len(t.Blocks))
		for _, b := range t.Blocks {
			if date != "" && b.Date == date || date == "" && c.Sprint.Contains(b.Date) {
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
