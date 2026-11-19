package projects

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/widgets"
)

func TestGanttWrapsOnlyOverflowingMilestoneTitles(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	c := NewGanttChart([]Item{
		{Milestone: "Release", MilestoneDue: now, Created: now},
		{Milestone: "Server update required before release", MilestoneDue: now.AddDate(0, 0, 1), Created: now},
	}, now, currentPalette(), nil)
	c.Resize(fyne.NewSize(1000, c.MinSize().Height))
	c.layout.Layout(nil, c.Size())
	var headings []ganttRow
	for _, row := range c.layout.rows {
		if row.rangeIndex < 0 {
			headings = append(headings, row)
		}
	}
	if len(headings) != 2 {
		t.Fatal("expected both milestone headings")
	}
	short, long := headings[0], headings[1]
	if short.heading.Objects[1].(*canvas.Text).Text != "" || long.heading.Objects[1].(*canvas.Text).Text == "" || short.height >= long.height {
		t.Fatal("only the overflowing Gantt title should grow to a second line")
	}
	if long.when.Position().Y < long.heading.Position().Y+long.heading.MinSize().Height {
		t.Fatal("the due date must remain below the wrapped title")
	}
}

func TestGanttUsesSprintDatesAndDoesNotInventSchedules(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 11, n, 0, 0, 0, 0, time.UTC) }
	items := []Item{
		{Number: 1, Milestone: "Release", MilestoneDue: day(25), SprintStart: day(10), SprintDays: 7, Created: day(1)},
		{Number: 2, Created: day(2), Closed: true, ClosedAt: day(5)},
		{Number: 3, Milestone: "Release", MilestoneDue: day(25)},
	}
	ranges := GanttRanges(items, day(14))
	if ranges[0].Item.Number != 1 || !ranges[0].Scheduled || !ranges[0].Start.Equal(day(10)) || !ranges[0].End.Equal(day(16)) {
		t.Fatal("a sprint range must preserve its actual inclusive dates", ranges[0])
	}
	if ranges[1].Item.Number != 3 || !ranges[1].Start.IsZero() || !ranges[1].End.IsZero() {
		t.Fatal("a milestone due date must not invent task dates", ranges[1])
	}
	if ranges[2].Item.Number != 2 || ranges[2].Scheduled || !ranges[2].End.Equal(day(5)) {
		t.Fatal("closed issues without a sprint show their actual lifetime", ranges[2])
	}
}

func TestGanttRendersMilestonesDependenciesAndIssueClicks(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	items := []Item{
		{Number: 1, URL: "https://github.com/o/r/issues/1", Title: "First", Created: now.AddDate(0, 0, -7), Closed: true, ClosedAt: now.AddDate(0, 0, -4), Milestone: "Release", MilestoneDue: now.AddDate(0, 0, 7)},
		{Number: 2, URL: "https://github.com/o/r/issues/2", Title: "Second", Created: now.AddDate(0, 0, -3), Milestone: "Release", MilestoneDue: now.AddDate(0, 0, 7), Blockers: []Dependency{{Number: 1, URL: "https://github.com/o/r/issues/1", Closed: true}, {Number: 99, URL: "https://github.com/o/other/issues/99"}}},
		{Number: 3, Title: "No dates"},
	}
	picked := 0
	c := NewGanttChart(items, now, currentPalette(), func(it Item) { picked = it.Number })
	c.Resize(fyne.NewSize(1000, c.MinSize().Height))
	c.layout.Layout(nil, c.Size())
	if len(c.layout.edges) != 1 {
		t.Fatal("only visible, dated blocker endpoints should be connected")
	}
	if c.layout.rows[0].due == nil || c.layout.rows[0].label.TextSize <= 14 {
		t.Fatal("milestones need a prominent heading and due marker")
	}
	marker := c.layout.rows[0]
	if marker.dueLabel == nil || marker.dueLabel.Text != "Due 21 Nov 2026" || marker.dueLabel.Position().Y > marker.top+marker.height {
		t.Fatal("each gold milestone marker must identify its due date at the top")
	}
	if !c.layout.todayLabel.Visible() || c.layout.todayLabel.Text != "Today 14 Nov 2026" || c.layout.todayLabel.Position().Y >= ganttAxisHeight {
		t.Fatal("the gold today marker must have a separate date label above the timeline")
	}
	var second ganttRow
	for _, row := range c.layout.rows {
		if row.rangeIndex >= 0 && c.Ranges[row.rangeIndex].Item.Number == 2 {
			second = row
		}
		if row.rangeIndex >= 0 && c.Ranges[row.rangeIndex].Item.Number == 3 && (!row.unscheduled.Visible() || row.bar.Visible()) {
			t.Fatal("undated issues must stay explicitly unscheduled")
		}
	}
	linkShown := func() bool {
		for _, line := range c.layout.edges[0].lines {
			if line.Visible() {
				return true
			}
		}
		return false
	}
	if !c.layout.edges[0].critical || !linkShown() {
		t.Fatal("the only blocker chain is the critical path, drawn without hovering")
	}
	point := fyne.NewPos(ganttLabelWidth+12, second.top+12)
	c.MouseIn(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: point}})
	if !linkShown() {
		t.Fatal("hovering a blocked issue must draw its blocker link")
	}
	if !strings.Contains(c.layout.detail, "blocked by #99 (open)") {
		t.Fatal("external blockers must remain visible in the readout")
	}
	c.Tapped(&fyne.PointEvent{Position: point})
	if picked != 2 {
		t.Fatal("clicking a Gantt row must open its issue")
	}
	c.MouseOut()
	if _, _, _, alpha := c.layout.pill.Rect.FillColor.RGBA(); alpha != 0 {
		t.Fatal("leaving must clear the highlight")
	}
}

func TestProjectFetchReadsNativeBlockers(t *testing.T) {
	old := backend.Gh
	defer func() { backend.Gh = old }()
	backend.Gh = func(args []string, _ ...string) (string, error) {
		if !strings.Contains(strings.Join(args, " "), "blockedBy(first:100)") {
			t.Fatal("the project query must request actual blocker relationships")
		}
		return `{"data":{"repository":{"issue":{"title":"Epic","subIssues":{"nodes":[{"number":2,"title":"Child","blockedBy":{"totalCount":1,"nodes":[{"number":1,"title":"Predecessor","url":"https://github.com/o/r/issues/1","closed":true}]}}]}}}}}`, nil
	}
	p := fetchParent("o/r#9")
	if p.Err != "" || len(p.Items) != 1 || len(p.Items[0].Blockers) != 1 || !p.Items[0].Blockers[0].Closed || p.Items[0].BlockerCount != 1 {
		t.Fatal("the Gantt must receive blocker identities and resolved states", p)
	}
}

func TestGanttOrdersBlockersAboveWhatTheyBlock(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	url := func(n int) string { return fmt.Sprintf("https://github.com/o/r/issues/%d", n) }
	items := []Item{
		{Number: 1, URL: url(1), Title: "Loose", Created: now.AddDate(0, 0, -9)},
		{Number: 2, URL: url(2), Title: "Blocked", Created: now.AddDate(0, 0, -8), Blockers: []Dependency{{Number: 3, URL: url(3)}}},
		{Number: 3, URL: url(3), Title: "Blocker", Created: now.AddDate(0, 0, -7)},
	}
	c := NewGanttChart(items, now, currentPalette(), nil)
	var order []int
	var rows []ganttRow
	for _, row := range c.layout.rows {
		if row.rangeIndex >= 0 {
			order = append(order, c.Ranges[row.rangeIndex].Item.Number)
			rows = append(rows, row)
		}
	}
	if fmt.Sprint(order) != "[1 3 2]" {
		t.Fatalf("issues go by start date, each blocker above what it blocks: got %v", order)
	}
	if rows[1].top != rows[0].top+ganttRowHeight {
		t.Fatal("rows stay packed; sprints are set apart across, not down")
	}
}

func TestGanttNeverStepsBackInTimeDownAMilestone(t *testing.T) {
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	url := func(n int) string { return fmt.Sprintf("https://github.com/o/r/issues/%d", n) }
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	// As Angular Upgrade has it: a November chain, and loose issues from May
	// and October listed after it.
	items := []Item{
		{Number: 10, URL: url(10), SprintStart: day(11, 5), SprintDays: 14},
		{Number: 11, URL: url(11), SprintStart: day(11, 19), SprintDays: 14, Blockers: []Dependency{{Number: 10, URL: url(10)}}},
		{Number: 20, URL: url(20), SprintStart: day(10, 22), SprintDays: 14},
		{Number: 21, URL: url(21), SprintStart: day(5, 7), SprintDays: 14},
	}
	var order []int
	for _, r := range GanttRanges(items, now) {
		order = append(order, r.Item.Number)
	}
	if fmt.Sprint(order) != "[21 20 10 11]" {
		t.Fatalf("bars must step forward down the chart: got %v", order)
	}
}

func TestGanttSeparatesSprintsAndShowsOnlyTheCriticalPathAtRest(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	start := now.AddDate(0, 0, -28)
	url := func(n int) string { return fmt.Sprintf("https://github.com/o/r/issues/%d", n) }
	sprint := func(n, offset int, blockers ...int) Item {
		it := Item{Number: n, URL: url(n), Title: fmt.Sprint("Issue ", n), SprintStart: start.AddDate(0, 0, offset), SprintDays: 14}
		for _, b := range blockers {
			it.Blockers = append(it.Blockers, Dependency{Number: b, URL: url(b)})
		}
		return it
	}
	// 1 > 2 > 3 across three sprints is the long chain; 4 > 5 the short one.
	items := []Item{sprint(1, 0), sprint(2, 14, 1), sprint(3, 28, 2), sprint(4, 0), sprint(5, 14, 4)}
	items[3].SprintDays, items[4].SprintDays = 3, 3
	c := NewGanttChart(items, now, currentPalette(), nil)
	c.Resize(fyne.NewSize(1000, c.MinSize().Height))
	c.layout.Layout(nil, c.Size())
	bar := map[int]*canvas.Rectangle{}
	rowOf := map[int]ganttRow{}
	for _, row := range c.layout.rows {
		if row.rangeIndex >= 0 {
			n := c.Ranges[row.rangeIndex].Item.Number
			bar[n], rowOf[n] = row.bar, row
		}
	}
	gap := bar[2].Position().X - (bar[1].Position().X + bar[1].Size().Width)
	if gap < 2*ganttBarInset-1 {
		t.Fatalf("back-to-back sprints need a gap between their bars, got %g", gap)
	}
	for _, n := range []int{1, 2, 3} {
		if bar[n].FillColor != widgets.LogoGold || bar[n].StrokeColor != currentPalette().done || bar[n].StrokeWidth == 0 {
			t.Fatalf("#%d is on the critical path: a gold bar with a green outline", n)
		}
	}
	if bar[4].FillColor != currentPalette().stateColor(items[3]) || bar[5].StrokeWidth != 0 {
		t.Fatal("bars off the critical path keep their usual colour")
	}
	shown := func(e ganttEdge) int {
		n := 0
		for _, line := range e.lines {
			if line.Visible() {
				n++
			}
		}
		return n
	}
	var short ganttEdge
	for _, e := range c.layout.edges {
		if c.Ranges[e.to].Item.Number == 5 {
			short = e
		} else if e.lines[0].StrokeColor != currentPalette().done {
			t.Fatal("critical path links are green")
		} else if shown(e) != 5 {
			t.Fatalf("a critical link between neighbouring sprints is a plain elbow and arrowhead, got %d lines", shown(e))
		}
	}
	if shown(short) != 0 {
		t.Fatal("a link off the critical path stays hidden until hovered")
	}
	c.MouseIn(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(ganttLabelWidth+12, rowOf[5].top+12)}})
	if shown(short) == 0 {
		t.Fatal("hovering an issue draws its blocker links")
	}
	c.MouseOut()
	if shown(short) != 0 {
		t.Fatal("leaving hides the links off the critical path again")
	}
}
