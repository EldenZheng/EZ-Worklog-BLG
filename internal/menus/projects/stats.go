package projects

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
)

// Status classes say what kind of state a slice of the status chart is. The
// chart colours a slice by its class, so "In progress" is the same colour in
// every project.
const (
	ClassDone = iota
	ClassProgress
	ClassReview
	ClassToDo
	ClassBacklog
	ClassBlocked
	ClassOpen // open with nothing more known
	ClassOther
	ClassNotPlanned
)

// Slice is one share of the status chart, with the issues in it.
type Slice struct {
	Label string
	Count int
	Class int
	Items []Item
}

// PersonLoad is what one person carries: child issues assigned to them, split
// into finished and still open.
type PersonLoad struct {
	Name       string
	Done, Open int
}

func (p PersonLoad) Total() int { return p.Done + p.Open }

// WeekPoint is the project at the end of one week (Monday to Sunday): how
// many child issues were in scope by then, how many finished, and how many
// finished and were opened within that week alone.
type WeekPoint struct {
	Start          time.Time
	Scope, Done    int
	Closed, Opened int
}

// EpicProgress is one parent issue's share of the work.
type EpicProgress struct {
	Parent      Parent
	Done, Total int
}

// Summary is everything the page draws for one project.
type Summary struct {
	// Total is the project's scope: Done plus Open. Issues closed as not
	// planned are counted apart and are in neither.
	Total, Done, Open, NotPlanned int
	// BoardDone is the open issues a board already calls Done. The status ring
	// counts them as Done with the closed ones; everything else counts them as
	// open, since they have no closing date to chart.
	BoardDone                int
	UnassignedOpen           int
	ClosedLast7, OpenedLast7 int

	Statuses []Slice
	// People are in name order; Unassigned is the work nobody holds.
	People     []PersonLoad
	Unassigned PersonLoad
	Weeks      []WeekPoint
	// Velocity is issues finished per week over the last four weeks, and
	// Finish the day the open ones run out at that pace (zero when unknown).
	Velocity float64
	Finish   time.Time
	// PaceSpan is how many weeks Velocity was taken over: four, or fewer for a
	// younger project. PrevVelocity is the pace over the four weeks before
	// those, when the project is old enough to have had them (HasPrev).
	PaceSpan     float64
	PrevVelocity float64
	HasPrev      bool
	// OpenedPace is how many issues joined the scope a week over the same
	// span as Velocity.
	OpenedPace float64

	// Sprints and Priorities split the scope by the board's Sprint and
	// Priority: sprints in date order, priorities most urgent first, and
	// the issues with none last.
	Sprints, Priorities []Bucket
	// Milestones split the scope by milestone, by due date.
	Milestones []Milestone
	// Credit is who finished the issues in each recent week.
	Credit Credit
	Epics  []EpicProgress
	// Oldest are the open issues that have waited longest.
	Oldest []Item
	// Items is every child issue, those closed as not planned included.
	Items []Item
}

// Where picks the issues in scope (not planned ones left out) that keep
// passes, in the order GitHub listed them.
func (s Summary) Where(keep func(Item) bool) []Item {
	var out []Item
	for _, it := range s.Items {
		if !it.NotPlanned && keep(it) {
			out = append(out, it)
		}
	}
	return out
}

// Held is what one person holds, open issues first. An empty name is the
// issues nobody holds.
func (s Summary) Held(name string) []Item {
	held := s.Where(func(it Item) bool {
		if name == "" {
			return len(it.Assignees) == 0
		}
		return slices.Contains(it.Assignees, name)
	})
	sort.SliceStable(held, func(i, j int) bool { return !held[i].Closed && held[j].Closed })
	return held
}

// Bucket is a share of the scope: one sprint, or one priority.
type Bucket struct {
	Name       string
	Done, Open int
	Items      []Item
	// Current marks the sprint running today.
	Current bool
}

func (b Bucket) Total() int { return b.Done + b.Open }

// Credit is the issues finished each week, split by who finished them: the
// issue's first assignee. The busiest few have a name each and the rest are
// counted together as Others, so the columns stay readable.
type Credit struct {
	Weeks []time.Time
	Names []string
	// Counts[w][n] is how many issues Names[n] finished in Weeks[w].
	Counts [][]int
	// Pace is the four-week average of issues finished, ending each week: the
	// same figure as Velocity, drawn week by week to show whether it is
	// rising or falling.
	Pace []float64
}

const (
	// creditNames is how many people get a colour of their own.
	creditNames  = 5
	othersName   = "Others"
	noSprint     = "No sprint"
	noPriority   = "No priority"
	unassignedAs = "Unassigned"
)

// Percent is how much of the scope is finished, 0 to 100.
func (s Summary) Percent() int {
	if s.Total == 0 {
		return 0
	}
	return int(math.Round(float64(s.Done) / float64(s.Total) * 100))
}

// RingPercent is the status ring's Done share of the scope, 0 to 100: closed
// issues and those a board calls Done.
func (s Summary) RingPercent() int {
	if s.Total == 0 {
		return 0
	}
	return int(math.Round(float64(s.Done+s.BoardDone) / float64(s.Total) * 100))
}

const (
	velocityWeeks = 4
	// maxWeeks is how far back the weekly history goes.
	maxWeeks    = 104
	oldestShown = 8
	// maxOpenStatuses is how many open statuses get a slice of their own
	// before the rest fold into "Other".
	maxOpenStatuses = 6
)

// weekStart is the Monday the day of t falls in.
func weekStart(t time.Time) time.Time {
	y, m, d := t.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	return day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
}

// statusClass files a board status under the kind of state it names.
func statusClass(name string) int {
	l := strings.ToLower(name)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(l, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("block", "hold", "stuck"):
		return ClassBlocked
	case has("review", "test", "qa", "verif"):
		return ClassReview
	case has("progress", "doing", "active", "wip", "develop"):
		return ClassProgress
	case has("backlog", "icebox", "triage", "later"):
		return ClassBacklog
	case has("todo", "to do", "to-do", "ready", "planned", "next", "new"):
		return ClassToDo
	}
	return ClassOther
}

// Summarize works a fetched project into the page's figures, as of now.
func Summarize(snap Snapshot, now time.Time) Summary {
	var s Summary
	items := snap.Items()
	s.Items = items
	anyStatus := false
	for _, it := range items {
		if !it.Closed && it.Status != "" {
			anyStatus = true
		}
	}

	people := map[string]*PersonLoad{}
	open := map[string][]Item{}
	var done, dropped []Item
	weekAgo := now.AddDate(0, 0, -7)
	paceFrom := now.AddDate(0, 0, -7*velocityWeeks)
	recent, before, opened := 0, 0, 0
	prevFrom := now.AddDate(0, 0, -14*velocityWeeks)
	var first time.Time
	for _, it := range items {
		if it.NotPlanned {
			s.NotPlanned++
			dropped = append(dropped, it)
			continue
		}
		s.Total++
		if first.IsZero() || (!it.Created.IsZero() && it.Created.Before(first)) {
			first = it.Created
		}
		if it.Created.After(paceFrom) {
			opened++
		}
		if it.Closed && it.ClosedAt.After(prevFrom) && !it.ClosedAt.After(paceFrom) {
			before++
		}
		if it.Created.After(weekAgo) {
			s.OpenedLast7++
		}
		holders := it.Assignees
		if it.Closed {
			s.Done++
			done = append(done, it)
			if it.ClosedAt.After(weekAgo) {
				s.ClosedLast7++
			}
			if it.ClosedAt.After(paceFrom) {
				recent++
			}
		} else {
			s.Open++
			label := it.Status
			switch {
			case label == "" && anyStatus:
				label = "No status"
			case label == "":
				label = "Open"
			}
			// Done on the board but not closed yet: the ring counts it as Done.
			if strings.EqualFold(strings.TrimSpace(label), "done") {
				s.BoardDone++
				done = append(done, it)
			} else {
				open[label] = append(open[label], it)
			}
			if len(holders) == 0 {
				s.UnassignedOpen++
			}
		}
		if len(holders) == 0 {
			count(&s.Unassigned, it.Closed)
		}
		for _, name := range holders {
			if people[name] == nil {
				people[name] = &PersonLoad{Name: name}
			}
			count(people[name], it.Closed)
		}
	}
	s.Unassigned.Name = "Unassigned"

	for _, p := range people {
		s.People = append(s.People, *p)
	}
	sort.Slice(s.People, func(i, j int) bool {
		return strings.ToLower(s.People[i].Name) < strings.ToLower(s.People[j].Name)
	})

	s.Statuses = statusSlices(done, dropped, open)
	s.Weeks = weekPoints(items, first, now)

	// A project younger than the pace window is paced over the weeks it has had.
	span := float64(velocityWeeks)
	if !first.IsZero() {
		if age := now.Sub(first).Hours() / (24 * 7); age < span {
			span = math.Max(age, 1)
		}
	}
	s.Velocity = float64(recent) / span
	s.PaceSpan = span
	s.OpenedPace = float64(opened) / span
	if !first.IsZero() && !first.After(paceFrom) {
		s.HasPrev = true
		s.PrevVelocity = float64(before) / velocityWeeks
	}
	s.Sprints = sprintBuckets(items, now)
	s.Priorities = priorityBuckets(items)
	s.Milestones = milestones(items)
	s.Credit = credit(items, s.Weeks)
	if s.Velocity > 0 && s.Open > 0 {
		days := float64(s.Open) / s.Velocity * 7
		s.Finish = now.AddDate(0, 0, int(math.Ceil(days)))
	}

	for _, parent := range snap.Parents {
		s.Epics = append(s.Epics, Epic(parent))
	}

	for _, it := range items {
		if !it.Closed {
			s.Oldest = append(s.Oldest, it)
		}
	}
	sort.SliceStable(s.Oldest, func(i, j int) bool { return s.Oldest[i].Created.Before(s.Oldest[j].Created) })
	if len(s.Oldest) > oldestShown {
		s.Oldest = s.Oldest[:oldestShown]
	}
	return s
}

// Epic is how far along one parent issue's children are, those closed as not
// planned left out.
func Epic(parent Parent) EpicProgress {
	e := EpicProgress{Parent: parent}
	for _, it := range parent.Items {
		if it.NotPlanned {
			continue
		}
		e.Total++
		if it.Closed {
			e.Done++
		}
	}
	return e
}

func count(p *PersonLoad, closed bool) {
	if closed {
		p.Done++
	} else {
		p.Open++
	}
}

// statusSlices orders the status chart: finished first, then the open states
// from the furthest along to the least, then what was dropped.
func statusSlices(done, dropped []Item, open map[string][]Item) []Slice {
	var all []Slice
	if len(done) > 0 {
		all = append(all, Slice{Label: "Done", Count: len(done), Class: ClassDone, Items: done})
	}
	var states []Slice
	for label, items := range open {
		class := statusClass(label)
		if label == "Open" || label == "No status" {
			class = ClassOpen
		}
		states = append(states, Slice{Label: label, Count: len(items), Class: class, Items: items})
	}
	sort.Slice(states, func(i, j int) bool {
		if states[i].Class != states[j].Class {
			return states[i].Class < states[j].Class
		}
		return strings.ToLower(states[i].Label) < strings.ToLower(states[j].Label)
	})
	if len(states) > maxOpenStatuses {
		other := Slice{Label: "Other", Class: ClassOther}
		for _, st := range states[maxOpenStatuses-1:] {
			other.Count += st.Count
			other.Items = append(other.Items, st.Items...)
		}
		states = append(states[:maxOpenStatuses-1], other)
	}
	all = append(all, states...)
	if len(dropped) > 0 {
		all = append(all, Slice{Label: "Not planned", Count: len(dropped), Class: ClassNotPlanned, Items: dropped})
	}
	return all
}

// weekPoints is the project's history a week at a time, from the week its
// first child issue was opened to this one.
func weekPoints(items []Item, first, now time.Time) []WeekPoint {
	if first.IsZero() {
		return nil
	}
	last := weekStart(now)
	start := weekStart(first)
	if earliest := last.AddDate(0, 0, -7*(maxWeeks-1)); start.Before(earliest) {
		start = earliest
	}
	var weeks []WeekPoint
	for w := start; !w.After(last); w = w.AddDate(0, 0, 7) {
		end := w.AddDate(0, 0, 7)
		point := WeekPoint{Start: w}
		for _, it := range items {
			if it.NotPlanned {
				continue
			}
			if it.Created.Before(end) {
				point.Scope++
				if !it.Created.Before(w) {
					point.Opened++
				}
			}
			if it.Closed && it.ClosedAt.Before(end) {
				point.Done++
				if !it.ClosedAt.Before(w) {
					point.Closed++
				}
			}
		}
		weeks = append(weeks, point)
	}
	return weeks
}

// bucket files the issues in scope under the name each is given.
func bucket(items []Item, name func(Item) string) map[string]*Bucket {
	out := map[string]*Bucket{}
	for _, it := range items {
		if it.NotPlanned {
			continue
		}
		key := name(it)
		b := out[key]
		if b == nil {
			b = &Bucket{Name: key}
			out[key] = b
		}
		b.Items = append(b.Items, it)
		if it.Closed {
			b.Done++
		} else {
			b.Open++
		}
	}
	return out
}

// sprintBuckets splits the scope by sprint, in date order, the issues in no
// sprint last. None at all when no board gives these issues a sprint.
func sprintBuckets(items []Item, now time.Time) []Bucket {
	starts := map[string]time.Time{}
	days := map[string]int{}
	byName := bucket(items, func(it Item) string {
		if it.Sprint == "" {
			return noSprint
		}
		starts[it.Sprint], days[it.Sprint] = it.SprintStart, it.SprintDays
		return it.Sprint
	})
	if len(byName) == 1 && byName[noSprint] != nil {
		return nil
	}
	var out []Bucket
	for name, b := range byName {
		if name != noSprint {
			start := starts[name]
			b.Current = !start.IsZero() && !now.Before(start) && now.Before(start.AddDate(0, 0, max(days[name], 1)))
		}
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Name == noSprint) != (b.Name == noSprint) {
			return b.Name == noSprint
		}
		if !starts[a.Name].Equal(starts[b.Name]) {
			return starts[a.Name].Before(starts[b.Name])
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out
}

// priorityRank orders priorities, most urgent first. Names it does not know
// come after the ones it does.
func priorityRank(name string) int {
	l := strings.ToLower(strings.TrimSpace(name))
	if len(l) == 2 && l[0] == 'p' && l[1] >= '0' && l[1] <= '4' {
		return int(l[1] - '0')
	}
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(l, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("urgent", "critical", "blocker", "highest"):
		return 0
	case has("high"):
		return 1
	case has("medium", "normal", "moderate"):
		return 2
	case has("lowest", "trivial"):
		return 4
	case has("low", "minor"):
		return 3
	}
	return 5
}

// priorityBuckets splits the scope by priority, most urgent first, the
// issues with none last. None at all when nothing carries a priority.
func priorityBuckets(items []Item) []Bucket {
	byName := bucket(items, func(it Item) string { return cmp.Or(it.Priority, noPriority) })
	if len(byName) == 1 && byName[noPriority] != nil {
		return nil
	}
	var out []Bucket
	for _, b := range byName {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Name == noPriority) != (b.Name == noPriority) {
			return b.Name == noPriority
		}
		if ra, rb := priorityRank(a.Name), priorityRank(b.Name); ra != rb {
			return ra < rb
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out
}

// credit is the last weeks' finished issues, split by who finished them, with
// the four-week pace ending each week.
func credit(items []Item, weeks []WeekPoint) Credit {
	var c Credit
	if len(weeks) == 0 {
		return c
	}
	from := 0
	index := map[time.Time]int{}
	for i, w := range weeks[from:] {
		c.Weeks = append(c.Weeks, w.Start)
		index[w.Start] = i
		// The average of up to four weeks ending with this one.
		sum, n := 0, 0
		for back := from + i; back >= 0 && n < velocityWeeks; back-- {
			sum += weeks[back].Closed
			n++
		}
		c.Pace = append(c.Pace, float64(sum)/float64(n))
	}

	who := func(it Item) string {
		if len(it.Assignees) == 0 {
			return unassignedAs
		}
		return it.Assignees[0]
	}
	totals := map[string]int{}
	type closed struct {
		week int
		name string
	}
	var finished []closed
	for _, it := range items {
		if it.NotPlanned || !it.Closed {
			continue
		}
		week, ok := index[weekStart(it.ClosedAt)]
		if !ok {
			continue
		}
		totals[who(it)]++
		finished = append(finished, closed{week, who(it)})
	}
	for name := range totals {
		c.Names = append(c.Names, name)
	}
	sort.Slice(c.Names, func(i, j int) bool {
		a, b := c.Names[i], c.Names[j]
		if totals[a] != totals[b] {
			return totals[a] > totals[b]
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
	if len(c.Names) > creditNames {
		c.Names = append(c.Names[:creditNames], othersName)
	}
	column := map[string]int{}
	for i, name := range c.Names {
		column[name] = i
	}
	c.Counts = make([][]int, len(c.Weeks))
	for i := range c.Counts {
		c.Counts[i] = make([]int, len(c.Names))
	}
	for _, f := range finished {
		n, ok := column[f.name]
		if !ok {
			n = column[othersName]
		}
		c.Counts[f.week][n]++
	}
	return c
}

// StateCounts is how many open issues in scope are in review and in
// progress on their boards.
func StateCounts(items []Item) WeekCounts {
	var c WeekCounts
	for _, it := range items {
		if it.Closed || it.NotPlanned {
			continue
		}
		switch statusClass(it.Status) {
		case ClassReview:
			c.Review++
		case ClassProgress:
			c.Progress++
		}
	}
	return c
}

// WeekStart is the Monday of the week t falls in.
func WeekStart(t time.Time) time.Time { return weekStart(t) }

// Scenario is one way the project could go: a pace, and when the open issues
// run out at it.
type Scenario struct {
	Name   string
	Pace   float64
	Finish time.Time
}

// FinishAt is when open issues run out at pace a week, from now; zero when
// pace is nothing.
func FinishAt(open int, pace float64, now time.Time) time.Time {
	if pace <= 0 || open <= 0 {
		return time.Time{}
	}
	return now.AddDate(0, 0, int(math.Ceil(float64(open)/pace*7)))
}

// Scenarios are the finish at the current pace, at the best and the slowest
// four weeks the project has had, and at its average since it began.
func (s Summary) Scenarios(now time.Time) []Scenario {
	add := func(out []Scenario, name string, pace float64) []Scenario {
		return append(out, Scenario{Name: name, Pace: pace, Finish: FinishAt(s.Open, pace, now)})
	}
	out := add(nil, fmt.Sprintf("Current pace, last %d weeks", velocityWeeks), s.Velocity)
	if len(s.Weeks) >= velocityWeeks*2 {
		best, slowest := 0.0, math.Inf(1)
		for end := velocityWeeks - 1; end < len(s.Weeks); end++ {
			sum := 0
			for i := end - velocityWeeks + 1; i <= end; i++ {
				sum += s.Weeks[i].Closed
			}
			pace := float64(sum) / velocityWeeks
			best = max(best, pace)
			if pace > 0 {
				slowest = min(slowest, pace)
			}
		}
		out = add(out, fmt.Sprintf("Best %d weeks so far", velocityWeeks), best)
		if !math.IsInf(slowest, 1) {
			out = add(out, fmt.Sprintf("Slowest %d weeks so far", velocityWeeks), slowest)
		}
	}
	if weeks := len(s.Weeks); weeks > 0 {
		out = add(out, "Average since the first issue", float64(s.Done)/float64(weeks))
	}
	return out
}

// NeededPace is how many issues a week finish the open ones by date: zero
// when the date has passed or nothing is open.
func (s Summary) NeededPace(by, now time.Time) float64 {
	weeks := by.Sub(now).Hours() / (24 * 7)
	if weeks <= 0 || s.Open == 0 {
		return 0
	}
	return float64(s.Open) / weeks
}

// ProjectionWeeks is how many weeks past this one the burn-up runs on to
// reach the finish, or 0 when there is no finish worth drawing: none known,
// or one so far off the history would shrink to a sliver beside it.
func (s Summary) ProjectionWeeks(now time.Time) int {
	if s.Finish.IsZero() || len(s.Weeks) == 0 {
		return 0
	}
	weeks := int(math.Ceil(s.Finish.Sub(weekStart(now)).Hours() / (24 * 7)))
	limit := max(8, 2*len(s.Weeks))
	if weeks < 1 || weeks > min(limit, 52) {
		return 0
	}
	return weeks
}
