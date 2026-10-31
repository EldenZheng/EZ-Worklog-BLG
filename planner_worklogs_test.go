package main

import (
	"fmt"
	"reflect"
	"testing"
)

func TestSprintWorklogsUseBothMonthCachesAndCalendarFilters(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.kanban
	k.week = "2026-10-26"
	ui.cfg.ProjectURL = sprintBoardURLs[0]
	ui.ensureProjectCache()
	if !reflect.DeepEqual(k.calendarMonths(), []string{"2026-10", "2026-11"}) {
		t.Fatal("cross-month week missed a cache")
	}
	oct := statusCacheKey("2026-10")
	nov := statusCacheKey("2026-11")
	ui.projLoaded[oct] = true
	ui.projLoaded[nov] = true
	logged := WorklogItem{Date: "2026-10-31", Minutes: 90, URL: "https://github.com/bigledger/example/issues/100", ParentURL: "https://github.com/bigledger/example/issues/1"}
	ui.projItems[oct] = []WorklogItem{logged}
	ui.projItems[nov] = []WorklogItem{logged, {Date: "2026-11-01", Minutes: 60, URL: "https://github.com/BigLedger-Support/example/issues/200"}, {Date: "2026-11-02", Minutes: 120, URL: "https://github.com/bigledger/example/issues/300"}}
	if err := ui.store.WriteRows([]Row{
		{"id": "draft", "date": "2026-10-31", "minutes": "30", "issue": "bigledger/example#1"},
		{"id": "partial", "date": "2026-10-31", "minutes": "90", "issue": "bigledger/example#1", "issue_url": logged.URL},
		{"id": "pushed", "date": "2026-10-31", "minutes": "40", "issue": "bigledger/example#1", "pushed_at": "already pushed"},
	}); err != nil {
		t.Fatal(err)
	}
	// Log Work's filter is separate from the Sprint Calendar project legend.
	ui.orgOff = map[string]bool{"bigledger": true}
	data := k.calendarWorkData()
	if data.state != "ok" || len(data.days["2026-10-31"].logged) != 1 || len(data.days["2026-10-31"].drafts) != 1 || len(data.days["2026-11-01"].logged) != 1 || len(data.days["2026-11-02"].logged) != 0 {
		t.Fatal("cached worklogs/drafts were missing or counted twice", data.state)
	}
	k.projectOff = map[string]bool{"p": true}
	data = k.calendarWorkData()
	if len(data.days["2026-10-31"].logged) != 0 || len(data.days["2026-10-31"].drafts) != 0 {
		t.Fatal("calendar project filter did not hide its worklogs/drafts")
	}
	ui.projErr[oct] = fmt.Errorf("read failed")
	ui.projLoading[nov] = true
	if k.calendarWorkData().state != "error" {
		t.Fatal("a loading month concealed a worklog error")
	}
}

func TestSprintRemainingPlansCreditLoggedAndDraftMinutesOnce(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.kanban
	c.Item.URL = "https://github.com/bigledger/example/issues/42"
	c.Key = c.Item.URL
	date := k.week
	k.plan.Tasks[c.Key] = sprintTask{Blocks: []planBlock{{date, 540, 60}, {date, 660, 120}}}
	work := plannerDayWork{
		logged: []WorklogItem{{Date: date, Minutes: 40, URL: "https://github.com/bigledger/example/issues/100", ParentURL: c.Item.URL}},
		drafts: []Row{{"date": date, "minutes": "20", "issue": "BigLedger/example#42"}},
	}
	totals := k.dayTotals(date, []sprintCandidate{c, c}, work)
	if totals.logged != 40 || totals.draft != 20 || totals.planned != 120 {
		t.Fatal("logged/draft credit or duplicate project issue was counted twice", totals)
	}
	if got := plannerIssueRef(c.Item.URL + "?from=project#comment"); got != "bigledger/example#42" {
		t.Fatal("issue normalization did not match URL to reference", got)
	}
	// A local completion flag is not a substitute for actual GitHub worklogs.
	task := k.plan.Tasks[c.Key]
	task.Done = true
	k.plan.Tasks[c.Key] = task
	totals = k.dayTotals(date, []sprintCandidate{c}, work)
	if totals.logged != 40 || totals.draft != 20 || totals.planned != 0 {
		t.Fatal("local Done fabricated logged time", totals)
	}
}
