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
	if len(p.Boards) > 0 && len(p.TrayOrder) > 0 {
		var order []string
		for _, key := range p.TrayOrder {
			if onBoard[key] {
				order = append(order, key)
			}
		}
		if len(order) != len(p.TrayOrder) {
			changed = true
			p.TrayOrder = order
		}
	}
	return p, changed
}

// MoveTrayIssue puts key just after afterKey ("" for the top) in the Sprint
// Calendar tray's order. shown is the tray's order now; issues of other
// sprints keep their places after it.
func MoveTrayIssue(order, shown []string, key, afterKey string) []string {
	var next []string
	placed := afterKey == ""
	if placed {
		next = append(next, key)
	}
	for _, k := range shown {
		if k == key {
			continue
		}
		next = append(next, k)
		if k == afterKey {
			next = append(next, key)
			placed = true
		}
	}
	if !placed {
		next = append(next, key)
	}
	inShown := map[string]bool{}
	for _, k := range next {
		inShown[k] = true
	}
	for _, k := range order {
		if !inShown[k] {
			next = append(next, k)
		}
	}
	return next
}

// ApplyTrayOrder sorts the tray's issues into the order the user dragged them
// to. Issues not yet in it (new to the sprint) follow, in the board's order.
func ApplyTrayOrder(items []SprintCandidate, order []string) []SprintCandidate {
	if len(order) == 0 {
		return items
	}
	rank := map[string]int{}
	for i, k := range order {
		rank[k] = i
	}
	out := append([]SprintCandidate{}, items...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, iok := rank[out[i].Key]
		rj, jok := rank[out[j].Key]
		if iok != jok {
			return iok
		}
		return iok && ri < rj
	})
	return out
}

type PlanBlock struct {
	Date string
	// Start stays in the file but is always 0: a block has no time of day,
	// only its minutes. Its place in the day is Pos.
	Start, Minutes int
	// Note describes the block: what a meeting is for. Shown with its title.
	Note string `json:",omitempty"`
	// Pos is the block's place among its day's blocks, 0 at the top.
	Pos int `json:",omitempty"`
}

// PlannerDayCapacity is how many minutes of planned work a day takes when a
// block is dropped or allocated onto it.
const PlannerDayCapacity = 8 * 60

// plannerDayLimit is the most a day can hold once blocks are resized.
const plannerDayLimit = 24 * 60

// PlanBlockBefore orders a day's blocks by Pos. A plan saved before positions
// existed is ordered by its old start times until it is read again.
func PlanBlockBefore(a, b PlanBlock) bool {
	if a.Start != b.Start {
		return a.Start < b.Start
	}
	return a.Pos < b.Pos
}

// PlannerDayUsed is the minutes planned on date, including finished issues,
// leaving out one block (skipIndex of skipKey; -1 leaves none out).
func PlannerDayUsed(tasks map[string]SprintTask, date, skipKey string, skipIndex int) int {
	used := 0
	for key, t := range tasks {
		for i, b := range t.Blocks {
			if b.Date == date && !(key == skipKey && i == skipIndex) {
				used += b.Minutes
			}
		}
	}
	return used
}

// nextPlannerPos is the position after the last block on date.
func nextPlannerPos(tasks map[string]SprintTask, date string) int {
	next := 0
	for _, t := range tasks {
		for _, b := range t.Blocks {
			if b.Date == date && b.Pos >= next {
				next = b.Pos + 1
			}
		}
	}
	return next
}

// NormalizePlanPositions turns a plan saved with start times into one with
// positions: each day's blocks keep their order, numbered from 0, and every
// Start becomes 0. It reports whether anything changed.
func NormalizePlanPositions(p SprintPlan) (SprintPlan, bool) {
	type ref struct {
		key   string
		index int
	}
	byDate := map[string][]ref{}
	timed := map[string]bool{}
	for key, t := range p.Tasks {
		for i, b := range t.Blocks {
			byDate[b.Date] = append(byDate[b.Date], ref{key, i})
			if b.Start != 0 {
				timed[b.Date] = true
			}
		}
	}
	if len(timed) == 0 {
		return p, false
	}
	tasks := map[string]SprintTask{}
	for key, t := range p.Tasks {
		if t.Blocks != nil {
			t.Blocks = append([]PlanBlock{}, t.Blocks...)
		}
		tasks[key] = t
	}
	for date := range timed {
		refs := byDate[date]
		sort.SliceStable(refs, func(i, j int) bool {
			a, b := tasks[refs[i].key].Blocks[refs[i].index], tasks[refs[j].key].Blocks[refs[j].index]
			if a.Start != b.Start || a.Pos != b.Pos {
				return PlanBlockBefore(a, b)
			}
			if refs[i].key != refs[j].key {
				return refs[i].key < refs[j].key
			}
			return refs[i].index < refs[j].index
		})
		for n, r := range refs {
			tasks[r.key].Blocks[r.index].Start = 0
			tasks[r.key].Blocks[r.index].Pos = n
		}
	}
	p.Tasks = tasks
	return p, true
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

// PRIssue is the issue a pull request works on: its link and title.
type PRIssue struct {
	URL   string
	Title string `json:",omitempty"`
}

// UnmarshalJSON also reads the bare issue link an earlier build saved.
func (i *PRIssue) UnmarshalJSON(data []byte) error {
	var link string
	if err := json.Unmarshal(data, &link); err == nil {
		*i = PRIssue{URL: link}
		return nil
	}
	type plain PRIssue
	return json.Unmarshal(data, (*plain)(i))
}

type SprintTask struct {
	Estimate          int
	Complexity, Notes string
	Done              bool
	Blocks            []PlanBlock
}

func PlannerIssueFinished(it SprintItem, task SprintTask) bool {
	return task.Done || it.Closed || strings.EqualFold(strings.TrimSpace(it.Status), "Done")
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
	// KanbanColumnOrder is the status columns in the order they were dragged
	// to on the Kanban board. A status it does not name keeps its usual place
	// after those it does.
	KanbanColumnOrder []string  `json:",omitempty"`
	KanbanTypes       *[]string `json:",omitempty"`
	// CalendarTypes is the Sprint Calendar tray's Issues / Pull requests
	// choice, kept apart from Kanban's.
	CalendarTypes *[]string `json:",omitempty"`
	// PRIssues remembers the issue each pull request on a board works on, by
	// PR link, looked up once as Log Work's code review does. A zero PRIssue
	// is a PR that names no issue.
	PRIssues map[string]PRIssue `json:",omitempty"`
	// ProjectOrder is the board IDs in the order the Kanban and Sprint Calendar
	// legends show them.
	ProjectOrder []string `json:",omitempty"`
	// TrayOrder is the Sprint Calendar tray's issues, by key, in the order
	// they were dragged to. Local only: GitHub's project order is unchanged.
	TrayOrder []string `json:",omitempty"`
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
	if err != nil {
		p, err = lenientSprintPlan(raw)
	}
	if p.Tasks == nil {
		p.Tasks = map[string]SprintTask{}
	}
	// Blocks saved with start times are read as positions in that order.
	p, _ = NormalizePlanPositions(p)
	return p, err
}

// lenientSprintPlan reads a plan one setting at a time, so a setting this
// version cannot read (a remembered PR lookup, a filter) is left out instead
// of keeping the whole planner shut. Only the tasks, which hold the user's
// estimates, notes and time blocks, are worth refusing the file over: an
// error there still preserves it untouched.
func lenientSprintPlan(raw []byte) (SprintPlan, error) {
	p := SprintPlan{Tasks: map[string]SprintTask{}}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return p, err
	}
	for name, value := range fields {
		one, _ := json.Marshal(map[string]json.RawMessage{name: value})
		// Into a copy, kept only when it reads: a failed read can leave a
		// field half set.
		next := p
		if err := json.Unmarshal(one, &next); err != nil {
			if name == "Tasks" {
				return p, err
			}
			continue
		}
		p = next
	}
	return p, nil
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

// AllocateSprint fills each weekday up to PlannerDayCapacity, after the
// blocks already there, splitting large tasks across days. A sprint's end is
// an exclusive boundary.
func AllocateSprint(p SprintPlan, candidates []SprintCandidate, date string) (map[string]SprintTask, []string) {
	tasks := map[string]SprintTask{}
	used := map[string]int{}
	next := map[string]int{}
	for key, t := range p.Tasks {
		t.Blocks = append([]PlanBlock{}, t.Blocks...)
		tasks[key] = t
		for _, b := range t.Blocks {
			used[b.Date] += b.Minutes
			if b.Pos >= next[b.Date] {
				next[b.Date] = b.Pos + 1
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
		var added []PlanBlock
		for c.Sprint.Contains(day.Format("2006-01-02")) && remaining > 0 {
			d := day.Format("2006-01-02")
			if day.Weekday() != time.Saturday && day.Weekday() != time.Sunday {
				n := min(remaining, (PlannerDayCapacity-used[d])/PlannerDurationStep*PlannerDurationStep)
				if n > 0 {
					added = append(added, PlanBlock{Date: d, Minutes: n, Pos: next[d]})
					used[d] += n
					next[d]++
					remaining -= n
				}
			}
			day = day.AddDate(0, 0, 1)
		}
		// Do not save an incomplete allocation: leave it in the unscheduled list.
		if remaining > 0 {
			for _, b := range added {
				used[b.Date] -= b.Minutes
				next[b.Date]--
			}
			unplaced = append(unplaced, c.Item.Title)
			continue
		}
		t.Blocks = append(t.Blocks, added...)
		tasks[c.Key] = t
	}
	return tasks, unplaced
}

// ValidatePlanBlock checks a block before it is saved: a real date, a length,
// and room for it in the day beside the other issues' blocks.
func ValidatePlanBlock(key string, block PlanBlock, tasks map[string]SprintTask) error {
	if _, err := time.Parse("2006-01-02", block.Date); err != nil {
		return fmt.Errorf("enter a date as YYYY-MM-DD")
	}
	if block.Minutes <= 0 || block.Minutes > plannerDayLimit {
		return fmt.Errorf("the duration must fit within one day")
	}
	others := map[string]SprintTask{}
	for k, t := range tasks {
		if k != key {
			others[k] = t
		}
	}
	if PlannerDayUsed(others, block.Date, "", -1)+block.Minutes > PlannerDayCapacity {
		return fmt.Errorf("the day has no room for this block")
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
// block fits in what the day has left of PlannerDayCapacity. The lifted block
// is left out. The new block goes after the day's last one.
func PlannerSlot(p SprintPlan, c SprintCandidate, index int, date string) (PlanBlock, bool) {
	t := p.Tasks[c.Key]
	if !c.Sprint.Contains(date) || index < 0 && PlannerIssueFinished(c.Item, t) {
		return PlanBlock{}, false
	}
	mins := PlannerDurationUp(SprintEstimate(c.Item, t))
	if index >= 0 {
		if index >= len(t.Blocks) {
			return PlanBlock{}, false
		}
		mins = PlannerDurationUp(t.Blocks[index].Minutes)
	}
	if mins <= 0 || mins > PlannerDayCapacity {
		return PlanBlock{}, false
	}
	if PlannerDayUsed(p.Tasks, date, c.Key, index)+mins > PlannerDayCapacity {
		return PlanBlock{}, false
	}
	return PlanBlock{Date: date, Minutes: mins, Pos: nextPlannerPos(p.Tasks, date)}, true
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
		block.Note = t.Blocks[index].Note // a moved block keeps its description
		t.Blocks[index] = block
	} else {
		t.Blocks = append(t.Blocks, block)
	}
	tasks[c.Key] = t
	p.Tasks = tasks
	return p, nil
}

// PlanBlockRef names one block: an issue's key and the block's index in its
// task. Index -1 is a block not yet planned.
type PlanBlockRef struct {
	Key   string
	Index int
}

// ArrangePlannerDay sets date's blocks in order, numbering their positions
// from 0. A ref with Index -1 adds a block of minutes for that issue; a ref to
// a block on another date moves it here. The blocks keep their lengths and
// notes.
func ArrangePlannerDay(p SprintPlan, date string, order []PlanBlockRef, minutes int) (SprintPlan, error) {
	tasks := map[string]SprintTask{}
	for k, v := range p.Tasks {
		if v.Blocks != nil {
			v.Blocks = append([]PlanBlock{}, v.Blocks...)
		}
		tasks[k] = v
	}
	total := 0
	for pos, r := range order {
		t, ok := tasks[r.Key]
		if r.Index >= 0 && (!ok || r.Index >= len(t.Blocks)) {
			return p, fmt.Errorf("that time block is no longer planned")
		}
		if r.Index < 0 {
			if minutes <= 0 {
				return p, fmt.Errorf("the issue needs an estimate before it can be planned")
			}
			t.Blocks = append(t.Blocks, PlanBlock{Date: date, Minutes: minutes, Pos: pos})
			total += minutes
		} else {
			t.Blocks[r.Index].Date = date
			t.Blocks[r.Index].Start = 0
			t.Blocks[r.Index].Pos = pos
			total += t.Blocks[r.Index].Minutes
		}
		tasks[r.Key] = t
	}
	if total > plannerDayLimit {
		return p, fmt.Errorf("the day cannot hold these blocks")
	}
	p.Tasks = tasks
	return p, nil
}

// ResizePlannerBlock sets one block's length, as long as the day can hold
// it beside its other blocks. Nothing else on the day moves.
func ResizePlannerBlock(p SprintPlan, key string, index, minutes int) (SprintPlan, error) {
	if !ValidPlannerDuration(minutes) {
		return p, fmt.Errorf("duration must use 15-minute increments")
	}
	t := p.Tasks[key]
	if index < 0 || index >= len(t.Blocks) {
		return p, fmt.Errorf("this time block cannot be resized")
	}
	if PlannerDayUsed(p.Tasks, t.Blocks[index].Date, key, index)+minutes > plannerDayLimit {
		return p, fmt.Errorf("the day cannot hold this block")
	}
	tasks := map[string]SprintTask{}
	for k, v := range p.Tasks {
		tasks[k] = v
	}
	t.Blocks = append([]PlanBlock{}, t.Blocks...)
	t.Blocks[index].Minutes = minutes
	tasks[key] = t
	p.Tasks = tasks
	return p, nil
}

// SetPlannerBlockNote describes one block, as a meeting's purpose. want is the
// block as it was shown, so a plan that changed meanwhile is not written over.
func SetPlannerBlockNote(p SprintPlan, key string, index int, want PlanBlock, note string) (SprintPlan, error) {
	t := p.Tasks[key]
	if index < 0 || index >= len(t.Blocks) || t.Blocks[index] != want {
		return p, fmt.Errorf("this time block has changed; look at the calendar again")
	}
	t.Blocks = append([]PlanBlock{}, t.Blocks...)
	t.Blocks[index].Note = strings.TrimSpace(note)
	tasks := make(map[string]SprintTask, len(p.Tasks))
	for k, v := range p.Tasks {
		tasks[k] = v
	}
	tasks[key] = t
	p.Tasks = tasks
	return p, nil
}

// RemovePlannerBlock takes one time block off an issue, keeping its other blocks,
// estimate and notes. want is the block as it was shown, so a plan that changed
// while the confirmation was open does not lose a different block.
func RemovePlannerBlock(p SprintPlan, key string, index int, want PlanBlock) (SprintPlan, error) {
	t := p.Tasks[key]
	if index < 0 || index >= len(t.Blocks) || t.Blocks[index] != want {
		return p, fmt.Errorf("this time block has changed; look at the calendar again")
	}
	t.Blocks = append(append([]PlanBlock{}, t.Blocks[:index]...), t.Blocks[index+1:]...)
	tasks := make(map[string]SprintTask, len(p.Tasks))
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
