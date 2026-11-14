package projects

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"worklog/internal/shared/backend"
)

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
	var second ganttRow
	for _, row := range c.layout.rows {
		if row.rangeIndex >= 0 && c.Ranges[row.rangeIndex].Item.Number == 2 {
			second = row
		}
		if row.rangeIndex >= 0 && c.Ranges[row.rangeIndex].Item.Number == 3 && (!row.unscheduled.Visible() || row.bar.Visible()) {
			t.Fatal("undated issues must stay explicitly unscheduled")
		}
	}
	point := fyne.NewPos(ganttLabelWidth+12, second.top+12)
	c.MouseIn(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: point}})
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
