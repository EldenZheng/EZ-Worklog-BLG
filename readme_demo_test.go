//go:build demo

package main

// Renders the app off screen, against made-up data, into the README's
// screenshots and the frames of its animations. Nothing here reaches GitHub
// and nothing personal is drawn.
//
//	DEMO_OUT=docs/screenshots DEMO_FRAMES=<scratch dir> go test -tags demo -run TestReadmeDemoFrames .
//	python docs/make_readme_media.py docs/screenshots <scratch dir> docs

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
	"fyne.io/fyne/v2/driver/desktop"
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
	{Org: "acme-corp", Repo: "api", Number: 91, Title: "Document the webhook retry policy"},
}

func demoRef(i IssueInfo) string { return fmt.Sprintf("%s/%s#%d", i.Org, i.Repo, i.Number) }

func demoURL(i IssueInfo) string {
	return fmt.Sprintf("https://github.com/%s/%s/issues/%d", i.Org, i.Repo, i.Number)
}

// demoFrame is one step of an animation: a capture, how long it stays up, and
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

// frameRecorder collects captures and the pointer track laid over them.
type frameRecorder struct {
	t   *testing.T
	w   fyne.Window
	dir string
	n   int
	at  [2]float32
	seq []demoFrame
}

func newFrameRecorder(t *testing.T, w fyne.Window, dir string, start [2]float32) *frameRecorder {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &frameRecorder{t: t, w: w, dir: dir, at: start}
}

func (r *frameRecorder) shot() string {
	r.n++
	name := fmt.Sprintf("f%02d.png", r.n)
	capturePNG(r.t, r.w, filepath.Join(r.dir, name))
	return name
}

// glide moves the pointer from where it last was to `to` over a few steps on
// the same capture, then holds it there.
func (r *frameRecorder) glide(file string, to [2]float32, hold int) {
	from := r.at
	for k := 1; k <= 6; k++ {
		f := float32(k) / 7
		p := [2]float32{from[0] + (to[0]-from[0])*f, from[1] + (to[1]-from[1])*f}
		r.seq = append(r.seq, demoFrame{File: file, Ms: 40, Cursor: &p})
	}
	r.at = to
	r.stay(file, hold, false)
}

func (r *frameRecorder) stay(file string, ms int, click bool) {
	p := r.at
	r.seq = append(r.seq, demoFrame{File: file, Ms: ms, Cursor: &p, Click: click})
}

// finish writes the track, with the part of the window the animation shows.
func (r *frameRecorder) finish(crop [4]float32) {
	writeJSON(r.t, filepath.Join(r.dir, "frames.json"),
		map[string]any{"crop": crop, "frames": r.seq})
}

func TestReadmeDemoFrames(t *testing.T) {
	out, frames := os.Getenv("DEMO_OUT"), os.Getenv("DEMO_FRAMES")
	if out == "" || frames == "" {
		t.Skip("DEMO_OUT and DEMO_FRAMES not set")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}

	a := test.NewApp()
	defer a.Quit()
	test.ApplyTheme(t, darkGold{goldTheme{Theme: theme.DefaultTheme()}})
	w := a.NewWindow(windowTitle)
	// A maximised window on a 1080p screen. Anything narrower and the week
	// strip's seven columns no longer fit side by side.
	w.Resize(fyne.NewSize(1920, 1080))

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

	recordLoggingPopup(t, ui, w, filepath.Join(frames, "popup"), out)
	recordDraftDrag(t, ui, w, filepath.Join(frames, "drag"), out)
}

// recordLoggingPopup walks one commit group through the logging popup: open it,
// drop the calendar down, pick the day that is still short, type the minutes.
func recordLoggingPopup(t *testing.T, ui *UI, w fyne.Window, dir, out string) {
	ui.tabs.SelectIndex(0)
	ui.renderPending()
	g := ui.pending.Groups[0]
	rec := newFrameRecorder(t, w, dir, [2]float32{1100, 820})

	// The Log Work tab, the pointer travelling to the tile it is about to open.
	var tile fyne.CanvasObject
	walk(ui.pendingBox, func(o fyne.CanvasObject) {
		if tp, ok := o.(*tappable); ok && tile == nil {
			tile = tp
		}
	})
	logTab := rec.shot()
	rec.stay(logTab, 900, false)
	if tile != nil {
		rec.glide(logTab, centreOf(tile), 400)
		rec.stay(logTab, 200, true)
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

	popup := rec.shot()
	rec.stay(popup, 1000, false)
	rec.glide(popup, centreOf(picker.open), 400)
	rec.stay(popup, 200, true)

	// Drop the month down.
	test.Tap(picker.open)
	calendar := rec.shot()
	rec.stay(calendar, 1800, false)
	if err := copyFile(filepath.Join(dir, calendar), filepath.Join(out, "09-date-calendar.png")); err != nil {
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
	rec.glide(calendar, centreOf(cell), 700)
	rec.stay(calendar, 200, true)
	test.Tap(cell)
	picked := rec.shot()
	rec.stay(picked, 1000, false)

	// And the one number the popup needs from you.
	rec.glide(picked, centreOf(minE), 300)
	rec.stay(picked, 200, true)
	for _, r := range "120" {
		test.Type(minE, string(r))
		rec.stay(rec.shot(), 220, false)
	}
	done := rec.shot()
	rec.stay(done, 3200, false)
	// The popup is 80% of the window, centred; the calendar drops a little past
	// its bottom edge, so the crop leaves room for it.
	sz := w.Canvas().Size()
	rec.finish([4]float32{sz.Width*0.1 - 8, sz.Height*0.1 - 8, sz.Width*0.9 + 8, sz.Height*0.9 + 48})

	// The still for the README, with the minutes field called out.
	if err := copyFile(filepath.Join(dir, done), filepath.Join(out, "08-log-popup.png")); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(out, "08-log-popup.json"), []demoBox{
		boxOf(minE, "The only field to fill in"),
	})
}

// recordDraftDrag carries a saved entry across the week strip under the drafts:
// it was saved on a day that is already full, and belongs on the short one
// beside it.
func recordDraftDrag(t *testing.T, ui *UI, w fyne.Window, dir, out string) {
	days := demoWorkdays(today())
	full, short := days[len(days)-3], days[len(days)-2]
	if _, err := ui.store.AppendRows([]Row{
		{"date": full, "minutes": "120", "type": "manual", "issue": demoRef(demoIssues[5]),
			"issue_title": demoIssues[5].Title, "owner": "demo-user", "mode": "subissue",
			"remarks": "Wrote up the backoff schedule and the dead-letter queue."},
	}); err != nil {
		t.Fatal(err)
	}
	ui.weekStart = weekStartOf(short)
	ui.tabs.SelectIndex(0)
	// Tall enough that the strip under the drafts is on screen whole.
	w.Resize(fyne.NewSize(1920, 1400))
	ui.drawRecent()

	from, to := ui.weekCols[full], ui.weekCols[short]
	if from == nil || to == nil {
		t.Fatal("the week strip should show both days")
	}
	tiles := dragTilesIn(from)
	if len(tiles) == 0 {
		t.Fatal("the full day should carry the card to move")
	}
	tile := tiles[0]

	strip, drafts := boxOf(ui.weekBox, ""), boxOf(ui.recentBox, "")
	crop := [4]float32{0, drafts.Y - 120, strip.X + strip.W + 12, strip.Y + strip.H + 12}
	rec := newFrameRecorder(t, w, dir, [2]float32{strip.X + strip.W*0.75, strip.Y + strip.H*0.8})

	still := rec.shot()
	rec.stay(still, 1200, false)
	grab := centreOf(tile)
	rec.glide(still, grab, 400)
	rec.stay(still, 200, true)

	// Carried across, a capture per step so the card in the air follows. The
	// path bows upward a little, the way a hand moves.
	aim := centreOf(to)
	aim[1] = grab[1]
	entered := false
	for k := 1; k <= 10; k++ {
		f := float32(k) / 10
		p := [2]float32{grab[0] + (aim[0]-grab[0])*f, grab[1] - 120*f*(1-f)}
		tile.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: fyne.NewPos(p[0], p[1])}})
		if !entered && p[0] >= fyne.CurrentApp().Driver().AbsolutePositionForObject(to).X {
			from.MouseOut()
			to.MouseIn(&desktop.MouseEvent{})
			entered = true
		}
		rec.at = p
		rec.stay(rec.shot(), 60, false)
	}

	// Held over the short day, which breathes towards the green it would turn.
	// The test driver runs no animations, so the breath is stepped by hand.
	to.stopPulse()
	green := blendColor(theme.Color(theme.ColorNameBackground), theme.Color(theme.ColorNameSuccess), weekDoneTint)
	for _, f := range []float32{0.3, 0.6, 0.85, 1, 0.85, 0.6, 0.3, 0.6, 0.85, 1} {
		to.frame.FillColor = blendColor(to.baseFill, green, f)
		to.frame.Refresh()
		rec.stay(rec.shot(), 90, false)
	}

	tile.DragEnd()
	landed := rec.shot()
	rec.stay(landed, 200, true)
	rec.stay(landed, 3000, false)
	rec.finish(crop)
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
