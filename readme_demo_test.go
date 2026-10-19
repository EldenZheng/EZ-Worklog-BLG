//go:build demo

package main

// Renders the app off screen, against made-up data, into the README's
// screenshots and the frames of its animation. Nothing here reaches GitHub and
// nothing personal is drawn.
//
//	DEMO_OUT=docs/screenshots DEMO_FRAMES=<scratch dir> go test -tags demo -run TestReadmeDemoFrames .
//	python docs/make_readme_media.py docs/screenshots <scratch dir> docs/demo.gif

import (
	"encoding/json"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
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

// demoFrame is one step of the animation: a capture, how long it stays up, and
// where the drawn pointer sits on it. The pointer is added afterwards by the
// script, so one capture can carry the pointer through several positions.
type demoFrame struct {
	File   string      `json:"file"`
	Ms     int         `json:"ms"`
	Cursor *[2]float32 `json:"cursor,omitempty"`
	Click  bool        `json:"click,omitempty"`
}

// demoBox is a rectangle on a capture, for the script to draw a callout around.
type demoBox struct {
	X, Y, W, H float32
	Label      string
}

func capturePNG(t *testing.T, w fyne.Window, path string) {
	t.Helper()
	w.Content().Refresh()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, w.Canvas().Capture()); err != nil {
		t.Fatal(err)
	}
}

func centreOf(o fyne.CanvasObject) [2]float32 {
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(o)
	return [2]float32{at.X + o.Size().Width/2, at.Y + o.Size().Height/2}
}

func boxOf(o fyne.CanvasObject, label string) demoBox {
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(o)
	return demoBox{at.X, at.Y, o.Size().Width, o.Size().Height, label}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, _ := json.MarshalIndent(v, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyFile(from, to string) error {
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, b, 0o644)
}

func TestReadmeDemoFrames(t *testing.T) {
	out, frames := os.Getenv("DEMO_OUT"), os.Getenv("DEMO_FRAMES")
	if out == "" || frames == "" {
		t.Skip("DEMO_OUT and DEMO_FRAMES not set")
	}
	for _, d := range []string{out, frames} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, darkGold{goldTheme{Theme: theme.DefaultTheme()}})
	w := a.NewWindow(windowTitle)
	// About a maximised window on a 1080p screen: anything smaller wraps the
	// tiles and the week columns into each other.
	w.Resize(fyne.NewSize(1680, 1000))

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
		capturePNG(t, w, filepath.Join(out, s.name+".png"))
	}

	recordLoggingPopup(t, ui, w, out, frames)
}

// recordLoggingPopup walks one commit group through the logging popup: open it,
// drop the calendar down, pick the day that is still short, type the minutes.
func recordLoggingPopup(t *testing.T, ui *UI, w fyne.Window, out, frames string) {
	ui.tabs.SelectIndex(0)
	ui.renderPending()
	g := ui.pending.Groups[0]

	var seq []demoFrame
	n := 0
	shot := func() string {
		n++
		name := fmt.Sprintf("f%02d.png", n)
		capturePNG(t, w, filepath.Join(frames, name))
		return name
	}
	// Moves the pointer from where it last was to `to` over a few steps on the
	// same capture, then holds it there.
	at := [2]float32{1100, 820}
	glide := func(file string, to [2]float32, hold int) {
		from := at
		for k := 1; k <= 6; k++ {
			f := float32(k) / 7
			p := [2]float32{from[0] + (to[0]-from[0])*f, from[1] + (to[1]-from[1])*f}
			seq = append(seq, demoFrame{File: file, Ms: 40, Cursor: &p})
		}
		at = to
		p := at
		seq = append(seq, demoFrame{File: file, Ms: hold, Cursor: &p})
	}
	stay := func(file string, ms int, click bool) {
		p := at
		seq = append(seq, demoFrame{File: file, Ms: ms, Cursor: &p, Click: click})
	}

	// The Log Work tab, the pointer travelling to the tile it is about to open.
	var tile fyne.CanvasObject
	walk(ui.pendingBox, func(o fyne.CanvasObject) {
		if tp, ok := o.(*tappable); ok && tile == nil {
			tile = tp
		}
	})
	logTab := shot()
	stay(logTab, 900, false)
	if tile != nil {
		glide(logTab, centreOf(tile), 400)
		stay(logTab, 200, true)
	}

	// The popup, built the way openGroupEditor builds it.
	form := ui.groupEditor(g, nil, nil)
	back := widget.NewButtonWithIcon("Back", theme.NavigateBackIcon(), nil)
	back.Importance = widget.LowImportance
	heading := g.Date + "  ·  " + ui.issueInfo[g.Issue].Title
	d := dialog.NewCustomWithoutButtons(heading, popupShell(back, form), w)
	d.Resize(ui.popupSize())
	d.Show()
	defer d.Hide()

	var picker *worklogDatePicker
	walk(form.body, func(o fyne.CanvasObject) {
		if p, ok := o.(*worklogDatePicker); ok {
			picker = p
		}
	})
	minE := entryByPlaceholder(form.body, "480")
	if picker == nil || minE == nil {
		t.Fatal("the popup should hold a date picker and a minutes field")
	}

	popup := shot()
	stay(popup, 1000, false)
	glide(popup, centreOf(picker.open), 400)
	stay(popup, 200, true)

	// Drop the month down.
	test.Tap(picker.open)
	calendar := shot()
	stay(calendar, 1800, false)
	if err := copyFile(filepath.Join(frames, calendar), filepath.Join(out, "09-date-calendar.png")); err != nil {
		t.Fatal(err)
	}

	// The short day: the one that still owes time.
	days := demoWorkdays(today())
	short, _ := time.Parse("2006-01-02", days[len(days)-2])
	var cell *tappable
	walk(picker.cal, func(o fyne.CanvasObject) {
		tp, ok := o.(*tappable)
		if !ok {
			return
		}
		walk(tp.content, func(c fyne.CanvasObject) {
			if txt, ok := c.(*canvas.Text); ok && txt.Text == fmt.Sprint(short.Day()) {
				cell = tp
			}
		})
	})
	if cell == nil {
		t.Fatalf("no cell for %s in the calendar", short.Format("2006-01-02"))
	}
	glide(calendar, centreOf(cell), 700)
	stay(calendar, 200, true)
	test.Tap(cell)
	picked := shot()
	stay(picked, 1000, false)

	// And the one number the popup needs from you.
	glide(picked, centreOf(minE), 300)
	stay(picked, 200, true)
	for _, r := range "120" {
		test.Type(minE, string(r))
		stay(shot(), 220, false)
	}
	done := shot()
	stay(done, 3200, false)
	writeJSON(t, filepath.Join(frames, "frames.json"), seq)

	// The still for the README, with the minutes field called out.
	if err := copyFile(filepath.Join(frames, done), filepath.Join(out, "08-log-popup.png")); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(out, "08-log-popup.json"), []demoBox{
		boxOf(minE, "The only field to fill in"),
	})
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
	// The remarks arrive written from the commit messages, as they do for real.
	for i := range groups {
		groups[i].Remarks = buildRemarks(groups[i].Commits)
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
