//go:build demo

package main

// Renders each tab off screen, against made-up data, into PNG frames for the
// README animation. Nothing here reaches GitHub and nothing personal is drawn.
//
//	DEMO_OUT=docs/frames go test -tags demo -run TestReadmeDemoFrames
//	python docs/make_demo_gif.py docs/frames docs/demo.gif

import (
	"fmt"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// darkGold pins the app's own theme to its dark variant: the test app reports
// light, and the app is only ever seen dark.
type darkGold struct{ goldTheme }

func (t darkGold) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return t.goldTheme.Color(n, theme.VariantDark)
}

var demoIssues = []IssueInfo{
	{Org: "acme-corp", Repo: "web-app", Number: 412, Title: "Invoice export to PDF"},
	{Org: "acme-corp", Repo: "web-app", Number: 398, Title: "Customer search is slow on large tenants"},
	{Org: "acme-corp", Repo: "api", Number: 87, Title: "Rate-limit the public webhooks"},
	{Org: "acme-support", Repo: "helpdesk", Number: 1203, Title: "Weekend support shift"},
	{Org: "acme-support", Repo: "helpdesk", Number: 1188, Title: "Fix stuck payment sync for tenant 42"},
}

func demoRef(i IssueInfo) string { return fmt.Sprintf("%s/%s#%d", i.Org, i.Repo, i.Number) }

func demoURL(i IssueInfo) string {
	return fmt.Sprintf("https://github.com/%s/%s/issues/%d", i.Org, i.Repo, i.Number)
}

func TestReadmeDemoFrames(t *testing.T) {
	out := os.Getenv("DEMO_OUT")
	if out == "" {
		t.Skip("DEMO_OUT not set")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}

	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, darkGold{goldTheme{Theme: theme.DefaultTheme()}})
	w := a.NewWindow(windowTitle)
	w.Resize(fyne.NewSize(1280, 800))

	ui := &UI{store: newStore(t.TempDir()), win: w,
		calMonth: thisMonth(), repMonth: currentPeriod()}
	ui.cfg = Config{
		Repos:           []string{"acme-corp", "acme-support"},
		WorklogOwner:    "demo-user",
		ProjectURL:      "https://github.com/orgs/acme-corp/projects/1",
		BaseSalary:      4200,
		Currency:        "USD",
		DisplayCurrency: "EUR",
		FxRate:          0.92,
		FxUpdated:       today(),
		LookbackDays:    14,
		DefaultMode:     "subissue",
	}
	ui.hasCfg = true
	ui.profileLoaded = true

	seedDemoData(t, ui)

	ui.buildAllTabs()
	w.SetContent(ui.tabs)
	ui.renderPending()
	ui.drawRecent()
	ui.drawLogList()
	ui.drawCommitList()
	ui.drawMeeting()
	ui.drawCalendar()
	ui.drawReport()

	shots := []struct {
		name string
		tab  int
		prep func()
	}{
		{"01-log-work", 0, nil},
		{"02-log-list", 1, ui.drawLogList},
		{"03-commit-list", 2, ui.drawCommitList},
		{"04-meeting", 3, ui.drawMeeting},
		{"05-status", statusTabIndex, ui.drawCalendar},
		{"06-status-day", statusTabIndex, func() {
			ui.selDay = demoWorkdays(today())[len(demoWorkdays(today()))-3]
			ui.drawCalendar()
			ui.drawDayPanel()
		}},
		{"07-report", statusTabIndex + 1, ui.drawReport},
	}
	for _, s := range shots {
		ui.tabs.SelectIndex(s.tab)
		if s.prep != nil {
			s.prep()
		}
		w.Content().Refresh()
		time.Sleep(50 * time.Millisecond)
		img := w.Canvas().Capture()
		f, err := os.Create(filepath.Join(out, s.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}

// demoWorkdays is every weekday from the start of the report period up to and
// including the day given.
func demoWorkdays(upTo string) []string {
	end, _ := time.Parse("2006-01-02", upTo)
	start := end.AddDate(0, -1, 0).AddDate(0, 0, -5)
	var days []string
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			days = append(days, d.Format("2006-01-02"))
		}
	}
	return days
}

func seedDemoData(t *testing.T, ui *UI) {
	ui.issueInfo = map[string]IssueInfo{}
	for _, i := range demoIssues {
		i.URL = demoURL(i)
		ui.issueInfo[demoRef(i)] = i
	}

	// What the board holds: most days a full eight hours, split across the two
	// orgs, with the odd short day and the odd long one.
	pattern := [][2]int{{360, 120}, {480, 0}, {300, 180}, {420, 60}, {240, 120},
		{480, 120}, {390, 90}, {300, 0}, {420, 120}, {480, 60}}
	var items []WorklogItem
	days := demoWorkdays(today())
	for n, d := range days[:len(days)-1] {
		p := pattern[n%len(pattern)]
		core := demoIssues[n%3]
		items = append(items, WorklogItem{Date: d, Minutes: p[0], Owner: "demo-user",
			Title: core.Title, URL: demoURL(core) + "1", Status: "Done",
			ParentTitle: core.Title, ParentURL: demoURL(core)})
		if p[1] > 0 {
			sup := demoIssues[3+n%2]
			items = append(items, WorklogItem{Date: d, Minutes: p[1], Owner: "demo-user",
				Title: sup.Title, URL: demoURL(sup) + "1", Status: "Done",
				ParentTitle: sup.Title, ParentURL: demoURL(sup)})
		}
	}
	ui.ensureProjectCache()
	for _, key := range []string{statusCacheKey(thisMonth()), statusCacheKey(shiftMonth(thisMonth(), -1)),
		reportCacheKey(currentPeriod())} {
		from, to, _ := cacheBounds(key)
		var in []WorklogItem
		for _, it := range items {
			if it.Date >= from && it.Date <= to {
				in = append(in, it)
			}
		}
		ui.projItems[key] = in
		ui.projLoaded[key] = true
	}

	// Saved here and not pushed yet: today's work.
	td := today()
	if _, err := ui.store.AppendRows([]Row{
		{"date": td, "minutes": "150", "type": "commit", "issue": demoRef(demoIssues[0]),
			"issue_title": demoIssues[0].Title, "owner": "demo-user", "mode": "subissue",
			"remarks": "Render line items through the shared PDF template; paginate long invoices."},
		{"date": td, "minutes": "90", "type": "manual", "issue": demoRef(demoIssues[4]),
			"issue_title": demoIssues[4].Title, "owner": "demo-user", "mode": "subissue",
			"remarks": "Replayed the failed sync jobs and added a retry guard."},
	}); err != nil {
		t.Fatal(err)
	}

	// Pushed but not logged: the bubbles on Log Work.
	msgs := []string{
		"Add PDF renderer for invoices", "Paginate invoice line items",
		"Index customer name for search", "Cache tenant lookup",
		"Throttle webhook deliveries per endpoint", "Retry stuck payment sync",
	}
	var groups []Group
	var commits []Commit
	for n, m := range msgs {
		iss := demoIssues[[]int{0, 0, 1, 1, 2, 4}[n]]
		d := days[len(days)-1-n%3]
		c := Commit{Sha: fmt.Sprintf("%07x", 0xa1b2c3+n*7919), Repo: iss.Org + "/" + iss.Repo,
			Date: d, Message: m, Full: m, Issue: demoRef(iss)}
		commits = append(commits, c)
		if n%2 == 0 || n == 5 {
			groups = append(groups, Group{Date: d, Issue: c.Issue, Commits: []Commit{c}})
		} else {
			groups[len(groups)-1].Commits = append(groups[len(groups)-1].Commits, c)
		}
	}
	ui.pending = PendingResult{Groups: groups, Count: len(commits), Range: []string{days[0], td}}
	ui.pendingLoaded = true
	ui.pendingAt = time.Now()

	// The week's commits, for Commit List and the meeting update.
	ui.ensureWeekCommitCache()
	ws := weekStartOf(td)
	var week []Commit
	for n, d := range weekDates(weekStartOf(days[len(days)-6])) {
		if n >= 5 {
			break // nobody commits at the weekend in the demo
		}
		for k := 0; k < 1+n%3; k++ {
			iss := demoIssues[(n+k)%len(demoIssues)]
			m := msgs[(n+k)%len(msgs)]
			week = append(week, Commit{Sha: fmt.Sprintf("%07x", 0x5e1f00+n*31+k), Repo: iss.Org + "/" + iss.Repo,
				Date: d, Message: m, Full: m, Issue: demoRef(iss)})
		}
	}
	week = append(week, commits...)
	ui.weekStart = weekStartOf(days[len(days)-6])
	ui.weekCommits[ui.weekStart] = week
	ui.weekCommits[ws] = commits
	ui.weekCommitAt[ui.weekStart] = time.Now()

	ui.ensureMeetingCommitCache()
	ms := meetingWeekStartOf(td)
	ui.meetingStart = ms
	ui.meetingCommits[ms] = week
	ui.meetingCommitAt[ms] = time.Now()
}
