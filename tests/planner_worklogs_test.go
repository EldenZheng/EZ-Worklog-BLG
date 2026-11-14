package tests

import (
	"fmt"
	"reflect"
	"testing"

	"worklog/internal/menus/logwork"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestOneHourDraftAndOneHourPlanRemainSeparate(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	c.Item.URL = "https://github.com/bigledger/example/issues/42"
	c.Key = c.Item.Key(k.Plan.Boards[0].ID)
	k.Plan.Boards[0].Items[0] = c.Item
	date := k.Week
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: date, Start: 540, Minutes: 60}}}
	draft := backend.Row{"date": date, "minutes": "60", "issue": "BigLedger/example#42", "description": "Waiting work"}
	if _, err := ui.Store.AppendRows([]backend.Row{draft}); err != nil {
		t.Fatal(err)
	}
	drafts := []backend.Row{draft}
	totals := sprintcalendar.DayTotals(k, date, []backend.SprintCandidate{c}, sprintcalendar.PlannerDayWork{Drafts: drafts})
	if totals.Draft != 60 || totals.Planned != 60 || totals.Logged != 0 {
		t.Fatalf("an hour saved and an hour planned must both count: %+v", totals)
	}
	col := logwork.WeekColumn(ui, date, nil, drafts)
	if col.Waiting != 60 || col.Planned != 60 || !contains(labels(col), "+120 min") {
		t.Fatalf("Log Work must retain both hours: drafts=%d planned=%d labels=%v", col.Waiting, col.Planned, labels(col))
	}
	if got := ui.PlannedByDay(date, date, nil)[date]; got != 60 {
		t.Fatalf("the shared Status and Report totals must retain the plan: %d", got)
	}
	if lines := sprintcalendar.PlannedLines(ui, date, nil, drafts); len(lines) != 1 || lines[0].Minutes != 60 {
		t.Fatalf("the plan must remain in the tooltip: %+v", lines)
	}
	ui.OrgOff = map[string]bool{shell.DraftsKey: true}
	if got := sprintcalendar.PlannedMinutes(ui, date, nil, drafts); got != 60 {
		t.Fatalf("hiding drafts must not affect the independent plan: %d", got)
	}
}

func TestSprintWorklogsUseBothMonthCachesAndCalendarFilters(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.Kanban
	k.Week = "2026-10-26"
	ui.Cfg.ProjectURL = backend.SprintBoardURLs[0]
	ui.EnsureProjectCache()
	if !reflect.DeepEqual(sprintcalendar.CalendarMonths(k), []string{"2026-10", "2026-11"}) {
		t.Fatal("cross-month week missed a cache")
	}
	oct := shell.StatusCacheKey("2026-10")
	nov := shell.StatusCacheKey("2026-11")
	ui.ProjLoaded[oct] = true
	ui.ProjLoaded[nov] = true
	logged := backend.WorklogItem{Date: "2026-10-31", Minutes: 90, URL: "https://github.com/bigledger/example/issues/100", ParentURL: "https://github.com/bigledger/example/issues/1"}
	ui.ProjItems[oct] = []backend.WorklogItem{logged}
	ui.ProjItems[nov] = []backend.WorklogItem{logged, {Date: "2026-11-01", Minutes: 60, URL: "https://github.com/BigLedger-Support/example/issues/200"}, {Date: "2026-11-02", Minutes: 120, URL: "https://github.com/bigledger/example/issues/300"}}
	if err := ui.Store.WriteRows([]backend.Row{
		{"id": "draft", "date": "2026-10-31", "minutes": "30", "issue": "bigledger/example#1"},
		{"id": "partial", "date": "2026-10-31", "minutes": "90", "issue": "bigledger/example#1", "issue_url": logged.URL},
		{"id": "pushed", "date": "2026-10-31", "minutes": "40", "issue": "bigledger/example#1", "pushed_at": "already pushed"},
	}); err != nil {
		t.Fatal(err)
	}
	// Log Work's filter is separate from the Sprint Calendar project legend.
	ui.OrgOff = map[string]bool{"bigledger": true}
	data := sprintcalendar.CalendarWorkData(k)
	if data.State != "ok" || len(data.Days["2026-10-31"].Logged) != 1 || len(data.Days["2026-10-31"].Drafts) != 1 || len(data.Days["2026-11-01"].Logged) != 1 || len(data.Days["2026-11-02"].Logged) != 0 {
		t.Fatal("cached worklogs/drafts were missing or counted twice", data.State)
	}
	k.ProjectOff = map[string]bool{"p": true}
	data = sprintcalendar.CalendarWorkData(k)
	if len(data.Days["2026-10-31"].Logged) != 0 || len(data.Days["2026-10-31"].Drafts) != 0 {
		t.Fatal("calendar project filter did not hide its worklogs/drafts")
	}
	ui.ProjErr[oct] = fmt.Errorf("read failed")
	ui.ProjLoading[nov] = true
	if sprintcalendar.CalendarWorkData(k).State != "error" {
		t.Fatal("a loading month concealed a worklog error")
	}
}

func TestSprintPlansAreNotReducedByLoggedMinutes(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	c.Item.URL = "https://github.com/bigledger/example/issues/42"
	c.Key = c.Item.URL
	date := k.Week
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: date, Start: 540, Minutes: 60}, {Date: date, Start: 660, Minutes: 120}}}
	work := sprintcalendar.PlannerDayWork{
		Logged: []backend.WorklogItem{{Date: date, Minutes: 40, URL: "https://github.com/bigledger/example/issues/100", ParentURL: c.Item.URL}},
		Drafts: []backend.Row{{"date": date, "minutes": "20", "issue": "BigLedger/example#42"}},
	}
	totals := sprintcalendar.DayTotals(k, date, []backend.SprintCandidate{c, c}, work)
	// Logged and draft time on the same issue leave the plan whole, and a
	// duplicate project issue counts once.
	if totals.Logged != 40 || totals.Draft != 20 || totals.Planned != 180 {
		t.Fatal("logged or draft time reduced the plan, or a duplicate issue counted twice", totals)
	}
	if got := sprintcalendar.PlannerIssueRef(c.Item.URL + "?from=project#comment"); got != "bigledger/example#42" {
		t.Fatal("issue normalization did not match URL to reference", got)
	}
	// A local completion flag is not a substitute for actual GitHub worklogs.
	task := k.Plan.Tasks[c.Key]
	task.Done = true
	k.Plan.Tasks[c.Key] = task
	totals = sprintcalendar.DayTotals(k, date, []backend.SprintCandidate{c}, work)
	if totals.Logged != 40 || totals.Draft != 20 || totals.Planned != 180 {
		t.Fatal("local Done changed logged, draft or planned time", totals)
	}
}
