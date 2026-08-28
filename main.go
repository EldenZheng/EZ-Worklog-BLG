package main

import (
	_ "embed"
	"encoding/csv"
	"fmt"
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ---- small tappable card, used for calendar cells ----

type tappable struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onTap   func()
}

func newTappable(content fyne.CanvasObject, onTap func()) *tappable {
	t := &tappable{content: content, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tappable) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.content)
}
func (t *tappable) Tapped(_ *fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

// ---- app state ----

type UI struct {
	store  *Store
	cfg    Config
	hasCfg bool
	win    fyne.Window

	calMonth string // YYYY-MM
	repMonth string
	selDay   string

	// containers refreshed on demand
	pendingBox *fyne.Container
	recentBox  *fyne.Container
	weekBox    *fyne.Container // the week a saved entry can be dropped onto
	calBox     *fyne.Container
	calLegend  *fyne.Container
	logLegend  *fyne.Container // the same key, at the foot of the Log work tab
	dayPanel   *fyne.Container
	daySide    *fyne.Container // the panel's shell: fixed width
	daySwap    *fyne.Container // holds either the day's detail or the prompt
	repBox     *fyne.Container
	tabs       *container.AppTabs
	prof       *profileMgr // who Status and Report are about — see profiles.go

	// The column each tab's panels stand in. Held so a panel that changed height
	// can have its column re-measure it — see relayout.
	logBody *fyne.Container
	calBody *fyne.Container
	repBody *fyne.Container

	// The bottom list is a to-do, not a history: it shows what is still waiting
	// to reach GitHub. This ticks over to the full month when the pushed ones
	// are wanted back.
	showPushed *widget.Check

	calTitle   *widget.Label
	calSummary *widget.Label
	repTitle   *widget.Label

	// One rate fetch in flight at a time: opening the Report tab twice in a row
	// should not queue two.
	rateLoading bool

	// Status + Report read from GitHub one server-filtered month at a time.
	// projStale marks a range a push has invalidated while its fetch was still
	// in the air, so the answer that lands knowingly out of date is thrown away.
	projItems   map[string][]WorklogItem
	projLoaded  map[string]bool
	projLoading map[string]bool
	projErr     map[string]error
	projStale   map[string]bool

	// The week on show above the saved entries, its columns kept so a drop
	// target can be lit, and the card currently in the air over them.
	weekStart string // Monday, YYYY-MM-DD
	weekCols  map[string]*dayColumn
	hoverDay  string
	dragRow   Row
	dragLayer *fyne.Container // above the tab, so the carried card is never buried
	dragGhost *fyne.Container

	// Pending commits, held so a bubble can be re-rendered without refetching,
	// plus the issue titles their refs resolve to.
	pending        PendingResult
	pendingLoaded  bool
	pendingLoading bool
	pendingAt      time.Time // when the list last landed, for staleness
	issueInfo      map[string]IssueInfo
	showIgnored    bool

	// Organisations tapped out of the colour key, lowercased. One filter for the
	// whole app rather than one per tab: hiding an employer is a decision about
	// what you are looking at, and it would have to be made three times over if
	// switching tabs quietly brought the other one back.
	orgOff map[string]bool
}

// relayout re-measures the column a panel stands in, after the panel's contents
// changed how much room they need.
//
// Replacing a container's Objects and refreshing it lays that container out
// again inside the space it was already given. Nothing tells the container above
// it that the space needed has changed — so a pending list that went from one
// line of "No unlogged commits" to a dozen tiles is drawn into one line's worth
// of height, and reads as a refresh that returned nothing. Refreshing the column
// re-runs its layout against the panel's new MinSize.
//
// Until this existed the only thing that did that was resizing the window.
func relayout(column *fyne.Container) {
	if column != nil {
		column.Refresh()
	}
}

// orgShown reports whether an organisation's work belongs in the current view.
func (ui *UI) orgShown(org string) bool {
	return !ui.orgOff[strings.ToLower(strings.TrimSpace(org))]
}

// toggleOrg flips one organisation in the key and redraws everything that
// carries a colour, so the filter lands on every tab at once rather than only
// on the one that was clicked.
func (ui *UI) toggleOrg(org string) {
	org = strings.ToLower(strings.TrimSpace(org))
	if ui.orgOff == nil {
		ui.orgOff = map[string]bool{}
	}
	if ui.orgOff[org] {
		delete(ui.orgOff, org)
	} else {
		ui.orgOff[org] = true
	}
	ui.drawCalendar()
	ui.drawReport()
	// The saved list, the week strip and the key itself all come off this.
	ui.drawRecent()
	// Only once there is a list: rendering an empty result would replace the
	// "loading" line with "no unlogged commits", which is a different claim.
	if ui.pendingLoaded {
		ui.renderPending()
	}
	ui.drawLogLegend()
}

// shownItems drops the worklog items belonging to a filtered-out organisation.
func (ui *UI) shownItems(items []WorklogItem) []WorklogItem {
	if len(ui.orgOff) == 0 {
		return items
	}
	out := make([]WorklogItem, 0, len(items))
	for _, it := range items {
		if ui.orgShown(itemOrg(it)) {
			out = append(out, it)
		}
	}
	return out
}

// rowOrg is the organisation a saved entry belongs to, read off the issue it is
// filed against. An entry with no issue yet has no org, and is only ever hidden
// by switching off the key's "unknown org".
func rowOrg(r Row) string { return strings.ToLower(orgOf(r["issue"])) }

// shownRows drops the saved entries belonging to a filtered-out organisation.
func (ui *UI) shownRows(rows []Row) []Row {
	if len(ui.orgOff) == 0 {
		return rows
	}
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		if ui.orgShown(rowOrg(r)) {
			out = append(out, r)
		}
	}
	return out
}

// itemOrg is the organisation a worklog item belongs to: its own issue's owner,
// or the parent issue's when the item is a sub-issue with no URL of its own.
func itemOrg(it WorklogItem) string {
	org := strings.ToLower(orgOf(it.URL))
	if org == "" {
		org = strings.ToLower(orgOf(it.ParentURL))
	}
	return org
}

// pendingFresh is how long a fetched list is trusted. The app is left open for
// days, so a list held for the whole session showed a morning's commits all
// afternoon and made every commit since look like it had gone missing.
const pendingFresh = 10 * time.Minute

// pendingIsStale reports whether a list fetched at that moment is old enough to
// be worth asking GitHub again.
func pendingIsStale(at time.Time) bool { return time.Since(at) >= pendingFresh }

// loadPending fetches unlogged commits, reusing the last answer while it is
// still fresh unless forced. The Log tab is the default tab, so this also runs
// at startup rather than waiting for a tab change that never fires.
func (ui *UI) loadPending(force bool) {
	if ui.pendingBox == nil || ui.pendingLoading {
		return
	}
	if ui.pendingLoaded && !force && !pendingIsStale(ui.pendingAt) {
		return
	}
	if len(ui.cfg.Repos) == 0 {
		ui.pendingBox.Objects = []fyne.CanvasObject{
			widget.NewLabel("Add a repo or org to scan in Settings (e.g. bigledger)."),
		}
		ui.pendingBox.Refresh()
		relayout(ui.logBody)
		return
	}
	if force {
		// Drop titles that never resolved so a manual refresh retries them;
		// successful ones are kept, since issue titles rarely change.
		for ref, info := range ui.issueInfo {
			if info.Title == "" {
				delete(ui.issueInfo, ref)
			}
		}
	}
	ui.pendingLoading = true
	// A stale list is left on screen while the new one is fetched; only a first
	// load or a button press has nothing worth looking at behind the wait.
	if force || !ui.pendingLoaded {
		ui.pendingBox.Objects = []fyne.CanvasObject{widget.NewLabel("Fetching from GitHub…")}
		ui.pendingBox.Refresh()
		relayout(ui.logBody)
	}

	var res PendingResult
	var ferr error
	ui.async(func() error {
		// Errors are carried out rather than returned so the loading flag is
		// always cleared; a stuck flag would block every later fetch.
		res, ferr = ui.store.FetchPending(ui.cfg)
		return nil
	}, func() {
		ui.pendingLoading = false
		if ferr != nil {
			ui.pendingBox.Objects = []fyne.CanvasObject{colorLabel(ferr.Error(), theme.ColorNameError)}
			ui.pendingBox.Refresh()
			relayout(ui.logBody)
			return
		}
		ui.pendingLoaded = true
		ui.pendingAt = time.Now()
		ui.pending = res
		ui.renderPending()
		ui.loadIssueInfos()
	})
}

// loadIssueInfos fills in the issue titles behind the pending refs, then
// re-renders. Bubbles appear immediately and gain their title a moment later.
func (ui *UI) loadIssueInfos() {
	if ui.issueInfo == nil {
		ui.issueInfo = map[string]IssueInfo{}
	}
	var refs []string
	for _, g := range ui.pending.Groups {
		if g.Issue == "" {
			continue
		}
		if _, done := ui.issueInfo[g.Issue]; !done {
			refs = append(refs, g.Issue)
		}
	}
	if len(refs) == 0 {
		return
	}
	var infos map[string]IssueInfo
	ui.async(func() error {
		infos, _ = FetchIssueInfos(refs)
		return nil
	}, func() {
		for ref, info := range infos {
			ui.issueInfo[ref] = info
		}
		ui.renderPending()
	})
}

func (ui *UI) ensureProjectCache() {
	if ui.projItems == nil {
		ui.projItems = map[string][]WorklogItem{}
		ui.projLoaded = map[string]bool{}
		ui.projLoading = map[string]bool{}
		ui.projErr = map[string]error{}
		ui.projStale = map[string]bool{}
	}
	if ui.projStale == nil {
		ui.projStale = map[string]bool{}
	}
}

const statusKeyPrefix = "status:"
const reportKeyPrefix = "report:"

func statusCacheKey(month string) string { return statusKeyPrefix + month }
func reportCacheKey(month string) string { return reportKeyPrefix + month }

// cacheBounds is the span of dates a cached range holds, read back out of its
// key. A push only makes the ranges covering its own date wrong; this is how
// they are told apart from the ones it cannot have touched.
func cacheBounds(key string) (string, string, bool) {
	if month, ok := strings.CutPrefix(key, statusKeyPrefix); ok {
		from, to, err := monthBounds(month)
		return from, to, err == nil
	}
	if month, ok := strings.CutPrefix(key, reportKeyPrefix); ok {
		// The fetched range, not the period: a push into the run-up changes the
		// support carry, so this cache really is wrong when one lands there.
		from, to, err := reportFetchBounds(month)
		return from, to, err == nil
	}
	return "", "", false
}

func (ui *UI) itemsFor(key string) ([]WorklogItem, bool) {
	ui.ensureProjectCache()
	if !ui.projLoaded[key] {
		return nil, false
	}
	return ui.projItems[key], true
}

// loadProject fetches only the requested date range and caches it by screen.
func (ui *UI) loadProject(key, fromDate, toDate string, force bool) {
	ui.ensureProjectCache()
	if ui.projLoading[key] {
		return
	}
	if ui.projLoaded[key] && !force {
		return
	}
	// Nothing to fetch from, so nothing is sent to fetch it: a goroutine here
	// could only come back with this same sentence, and it came back onto the
	// widgets while the caller was still drawing them.
	if len(projectURLs(ui.cfg)) == 0 {
		ui.projErr[key] = ghErr(
			"Set the Worklog project URL in Settings to load Status and Report from GitHub.")
		ui.drawCalendar()
		ui.drawReport()
		return
	}
	ui.projLoading[key] = true
	ui.projErr[key] = nil
	go func() {
		defer ui.recoverToDialog("load project")
		items, err := FetchProjectWorklogs(ui.cfg, ui.cfg.WorklogOwner, fromDate, toDate)
		fyne.Do(func() {
			ui.projLoading[key] = false
			ui.projErr[key] = err
			if err == nil {
				ui.projItems[key] = items
				ui.projLoaded[key] = true
			}
			ui.drawCalendar()
			ui.drawReport()
			// The Log tab scores today against this same data, so it has to be
			// redrawn when a month lands or it stays stuck on "checking…".
			ui.drawRecent()
			// A push landed while this fetch was in the air, so what came back is
			// already known to be one worklog short. It is drawn — it is still the
			// best answer on hand — and then asked for again.
			if ui.projStale[key] {
				delete(ui.projStale, key)
				delete(ui.projLoaded, key)
				ui.loadProject(key, fromDate, toDate, true)
			}
		})
	}()
}

func (ui *UI) loadStatus(force bool) {
	ui.ensureProjectCache()
	from, to, err := monthBounds(ui.calMonth)
	if err != nil {
		ui.projErr[statusCacheKey(ui.calMonth)] = err
		return
	}
	ui.loadProject(statusCacheKey(ui.calMonth), from, to, force)
}

func (ui *UI) loadReport(force bool) {
	ui.ensureProjectCache()
	from, to, err := reportFetchBounds(ui.repMonth)
	if err != nil {
		ui.projErr[reportCacheKey(ui.repMonth)] = err
		return
	}
	ui.loadProject(reportCacheKey(ui.repMonth), from, to, force)
}

// refreshTodayScore refetches the month the foot of the Log tab is scored
// against, and with it the Status calendar whenever that is showing the same
// month — one cache, one fetch, both up to date.
//
// The month is named here rather than going through loadStatus, which keys off
// whatever month the calendar happens to be left on. Today is always this one.
func (ui *UI) refreshTodayScore() {
	if len(projectURLs(ui.cfg)) == 0 {
		return // nothing to score against
	}
	month := thisMonth()
	from, to, err := monthBounds(month)
	if err != nil {
		return
	}
	ui.ensureProjectCache()
	key := statusCacheKey(month)
	delete(ui.projLoaded, key)
	ui.loadProject(key, from, to, true)
}

// refreshAfterPush refetches the GitHub data a push has just made wrong.
//
// Everything the app says about pushed work — the calendar, the report, the
// week strip, the score at the foot of the Log tab — is served out of ranges
// cached by date, and a push wrote a worklog item into one of them. Nothing
// dropped those, so a pushed entry stayed invisible on GitHub's side of the app
// until the month was reloaded by hand. Every cached range covering the pushed
// date goes; ranges it cannot have touched are left alone rather than costing a
// fetch each. A range still in the air is marked instead, and reloaded by
// loadProject when the answer that predates the push comes back.
func (ui *UI) refreshAfterPush(date string) {
	if date == "" || len(projectURLs(ui.cfg)) == 0 {
		return
	}
	ui.ensureProjectCache()
	for key, loading := range ui.projLoading {
		if _, _, ok := keyCovers(key, date); loading && ok {
			ui.projStale[key] = true
		}
	}
	// Picked first, refetched second: loadProject writes the very map being
	// walked to choose them.
	for key, span := range staleRanges(ui.projLoaded, ui.projLoading, date) {
		delete(ui.projLoaded, key)
		ui.loadProject(key, span[0], span[1], true)
	}
}

// keyCovers reports whether a cached range holds date, and what its span is.
func keyCovers(key, date string) (string, string, bool) {
	from, to, ok := cacheBounds(key)
	return from, to, ok && from <= date && date <= to
}

// staleRanges is the set of ranges a push on date has made wrong: the ones
// already sitting in the cache, holding that date, with no fetch of their own
// already in the air. Each maps to the span to ask GitHub for again.
func staleRanges(loaded, loading map[string]bool, date string) map[string][2]string {
	out := map[string][2]string{}
	for key, ok := range loaded {
		if !ok || loading[key] {
			continue
		}
		if from, to, covers := keyCovers(key, date); covers {
			out[key] = [2]string{from, to}
		}
	}
	return out
}

// today is the working day, not the calendar one: it turns over at 08:00 so
// work carried past midnight still lands on the day it belongs to. See
// workDayStart in weekstrip.go for why.
func today() string     { return workDate(time.Now()) }
func thisMonth() string { return today()[:7] }

// currentPeriod is the payroll period today falls in, named by the month it
// ends in — the same name reportBounds takes.
//
// A period runs the 21st to the 20th, so it is not this month. On the 21st the
// old period closed the day before and today's work belongs to the next one;
// opening the tab to a finished period reads as the app being a month behind,
// and the days worked since are nowhere on it.
//
// The month is stepped by rebuilding the date rather than by adding a month to
// today: adding one to the 31st of January lands on the 3rd of March, which
// would skip February's period entirely.
func currentPeriod() string { return periodOf(time.Now()) }

func periodOf(t time.Time) string {
	m := t.Month()
	if t.Day() >= 21 {
		m++ // time.Date normalises December + 1 into January of the next year
	}
	return time.Date(t.Year(), m, 1, 0, 0, 0, 0, t.Location()).Format("2006-01")
}
func pad2(n int) string { return fmt.Sprintf("%02d", n) }

func shiftMonth(m string, delta int) string {
	t, err := time.Parse("2006-01", m)
	if err != nil {
		return m
	}
	return t.AddDate(0, delta, 0).Format("2006-01")
}

func monthName(m string) string {
	t, err := time.Parse("2006-01", m)
	if err != nil {
		return m
	}
	return t.Format("January 2006")
}

func reportPeriodName(month string) string {
	from, to, err := reportBounds(month)
	if err != nil {
		return month
	}
	start, _ := time.Parse("2006-01-02", from)
	end, _ := time.Parse("2006-01-02", to)
	return fmt.Sprintf("%s – %s", start.Format("Jan 2"), end.Format("Jan 2, 2006"))
}

func commaAmount(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	parts := strings.SplitN(s, ".", 2)
	whole := parts[0]
	sign := ""
	if strings.HasPrefix(whole, "-") {
		sign, whole = "-", strings.TrimPrefix(whole, "-")
	}
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	return sign + whole + "." + parts[1]
}

func dataDir() string {
	if d := os.Getenv("WORKLOG_DIR"); d != "" {
		return d
	}
	exe, err := os.Executable()
	if err == nil {
		return filepath.Dir(exe)
	}
	wd, _ := os.Getwd()
	return wd
}

//go:embed icon.png
var appIconPNG []byte

func main() {
	a := app.NewWithID("com.eldenz.worklog")
	a.SetIcon(fyne.NewStaticResource("icon.png", appIconPNG))
	w := a.NewWindow("Worklog")
	w.Resize(fyne.NewSize(1440, 920))

	store := newStore(dataDir())
	cfg, has := store.LoadConfig()
	ui := &UI{
		store:    store,
		cfg:      cfg,
		hasCfg:   has,
		win:      w,
		calMonth: thisMonth(),
		repMonth: currentPeriod(),
	}

	ui.buildAllTabs()
	ui.tabs.OnSelected = func(ti *container.TabItem) {
		switch ti.Text {
		case "Status":
			ui.drawCalendar()
			ui.loadStatus(false)
		case "Report":
			ui.drawReport()
			ui.loadReport(false)
		case "Settings":
			ui.prof.loadMembers()
		}
	}
	w.SetContent(ui.prof.wrap(ui.tabs))

	ui.drawRecent()
	if !has {
		ui.tabs.SelectIndex(settingsTabIndex) // Settings first run
	}
	if os.Getenv("WORKLOG_AUTOFETCH") == "1" {
		ui.cfg.Repos = []string{"bigledger"} // smoke-test override
	}
	// Status is the tab shown at startup, so OnSelected never fires for it.
	// Kick the first fetch off here, after a beat, so the window is up before
	// the calendar starts changing under it.
	go func() {
		time.Sleep(750 * time.Millisecond)
		fyne.Do(func() {
			ui.drawCalendar()
			ui.loadStatus(false)
		})
	}()
	w.ShowAndRun()
}

// refreshRate re-reads the exchange rate behind the Report tab.
//
// force is the Settings button; opening the tab passes false, which fetches at
// most once a day. The rate moves slowly and the report is opened often, so a
// fetch per visit would be a request per glance for a figure that would not
// have changed — but a rate stamped last month quietly misstates the pay.
func (ui *UI) refreshRate(force bool) {
	if !ui.hasCfg || ui.rateLoading {
		return
	}
	base := strings.ToUpper(strings.TrimSpace(ui.cfg.Currency))
	disp := strings.TrimSpace(ui.cfg.DisplayCurrency)
	if base == "" || disp == "" {
		return // nothing to convert between
	}
	if !force && ui.cfg.FxUpdated == today() {
		return
	}
	if base == "RM" {
		base = "MYR"
	}
	ui.rateLoading = true
	var r float64
	ui.async(func() error {
		v, _ := fetchRate(base, disp)
		r = v
		return nil // a rate that will not load must not pop a dialog over the tab
	}, func() {
		ui.rateLoading = false
		if r <= 0 {
			return // keep yesterday's rate rather than zeroing the conversion
		}
		ui.cfg.FxRate = r
		ui.cfg.FxUpdated = today()
		_ = ui.store.SaveConfig(ui.cfg)
		ui.drawReport()
	})
}

// buildAllTabs assembles the tab bar. Kept apart from main so the wiring
// between tabs — a bar on the report opening a day on the calendar — can be
// exercised without a window manager.
func (ui *UI) buildAllTabs() {
	// Resolved before the tabs are built: they preload from ui.cfg, so the
	// active profile's login and salary have to already be in it.
	ui.prof = newProfileMgr(ui)
	// "Log work" is built but not shown. This fork only reads the boards, and
	// the rest of the app still draws into the tab's containers — the recent
	// list, the day scoring — so building it keeps those non-nil.
	ui.buildLogTab()
	ui.tabs = container.NewAppTabs(
		container.NewTabItem("Status", ui.buildStatusTab()),
		container.NewTabItem("Report", ui.buildReportTab()),
		container.NewTabItem("Settings", ui.buildForkSettingsTab()),
	)
}

// error dialog helper
func (ui *UI) errf(err error) {
	dialog.ShowError(err, ui.win)
}

// run a slow func off the UI thread, then apply result on the UI thread
func (ui *UI) async(work func() error, done func()) {
	go func() {
		defer ui.recoverToDialog("background task")
		err := work()
		fyne.Do(func() {
			defer ui.recoverToDialog("UI update")
			if err != nil {
				ui.errf(err)
				return
			}
			if done != nil {
				done()
			}
		})
	}()
}

// pushSpinner puts a turning wheel in front of the window while a push runs.
//
// A push is several round trips — the sub-issue, then a board write per field —
// and the only sign it was running was a line of text in the popup. The window
// looked hung, and the button looked ready for a second click. The modal says
// wait and takes the clicks in the meantime.
//
// The returned func stops the wheel and takes the modal down. It is safe to
// call twice, and must be called on the UI thread — which is where async runs
// its done func.
func (ui *UI) pushSpinner(label string) func() {
	wheel := widget.NewActivity()
	wheel.Start()
	d := dialog.NewCustomWithoutButtons(label,
		container.NewPadded(container.NewCenter(wheel)), ui.win)
	d.Show()

	stopped := false
	return func() {
		if stopped {
			return
		}
		stopped = true
		wheel.Stop()
		d.Hide()
	}
}

// recoverToDialog turns a panic into a logged crash.log entry + error dialog,
// so a rendering bug no longer takes the whole window down.
func (ui *UI) recoverToDialog(where string) {
	r := recover()
	if r == nil {
		return
	}
	msg := fmt.Sprintf("panic in %s: %v\n%s", where, r, debug.Stack())
	_ = os.WriteFile(filepath.Join(dataDir(), "crash.log"),
		[]byte(time.Now().Format(time.RFC3339)+"\n"+msg+"\n"), 0o644)
	fyne.Do(func() { ui.errf(fmt.Errorf("Something went wrong (%s). Details saved to crash.log.", where)) })
}

// ============================ Log tab ============================

func (ui *UI) buildLogTab() fyne.CanvasObject {
	kind := "commit"

	// -- commit pane --
	// The same button the other tabs carry, in the same corner: one way to ask
	// GitHub again, wherever you happen to be standing.
	fetchBtn := widget.NewButtonWithIcon("Refresh from GitHub", theme.ViewRefreshIcon(), nil)
	ui.pendingBox = container.NewVBox(
		widget.NewLabel("Loading your commits from GitHub…"),
	)
	commitPane := container.NewVBox(
		container.NewHBox(layout.NewSpacer(), fetchBtn), ui.pendingBox)

	// The list loads itself on first open; this button is the manual refresh.
	// Both halves of this tab come from GitHub — the commits waiting here, and
	// the line at the foot scoring today against the project — and that line
	// reads the Status cache. Refreshing only the commits left it stale.
	fetchBtn.OnTapped = func() {
		ui.loadPending(true)
		ui.refreshTodayScore()
	}

	// -- manual pane (meeting / other) --
	mDate := newDateEntryISO(today())
	mDesc := widget.NewEntry()
	mDesc.SetPlaceHolder("What was it?")
	mMin := widget.NewEntry()
	mMin.SetPlaceHolder("minutes")
	mMsg := widget.NewLabel("")
	mBtn := widget.NewButton("Log it", func() {
		mins, _ := strconv.Atoi(strings.TrimSpace(mMin.Text))
		if mins <= 0 {
			mMsg.SetText("Minutes must be more than zero.")
			return
		}
		date := isoDate(mDate)
		if date == "" {
			mMsg.SetText("Pick a date.")
			return
		}
		_, err := ui.store.AppendRows([]Row{{
			"date": date, "minutes": strconv.Itoa(mins),
			"type": kind, "description": mDesc.Text,
		}})
		if err != nil {
			ui.errf(err)
			return
		}
		mMsg.SetText("Logged.")
		mDesc.SetText("")
		mMin.SetText("")
		ui.drawRecent()
	})
	manualPane := container.NewVBox(
		container.New(newRatioRow(0.22, 0.56, 0.22),
			labeled("Date", mDate),
			labeled("Description", mDesc),
			labeled("Minutes", mMin),
		),
		mBtn, mMsg,
	)
	manualPane.Hide()

	// -- kind selector --
	seg := widget.NewRadioGroup([]string{"Commits", "Meeting", "Other"}, func(s string) {
		switch s {
		case "Commits":
			kind = "commit"
			commitPane.Show()
			manualPane.Hide()
		case "Meeting":
			kind = "meeting"
			commitPane.Hide()
			manualPane.Show()
		case "Other":
			kind = "other"
			commitPane.Hide()
			manualPane.Show()
		}
	})
	seg.Horizontal = true
	seg.SetSelected("Commits")

	ui.recentBox = container.NewVBox()
	// The week sits under the saved entries: its columns grow as tall as the
	// cards standing in them, so putting it first pushed the list itself off the
	// screen on a busy week.
	ui.weekBox = container.NewVBox()
	ui.showPushed = widget.NewCheck("Also show entries already pushed this month", func(bool) {
		ui.drawRecent()
	})
	logCard := widget.NewCard("", "", container.NewVBox(seg, commitPane, manualPane))
	// The key sits at the foot of the tab, where the Status tab keeps its own:
	// it governs everything above it — the commits, the saved entries and the
	// week — so it belongs under the lot rather than over one of them.
	ui.logLegend = container.NewVBox()
	recentCard := widget.NewCard("Saved locally, not pushed yet", "",
		container.NewVBox(ui.showPushed, ui.recentBox, widget.NewSeparator(),
			ui.weekBox, ui.logLegend))

	// The dragging layer sits above the scroll rather than inside it: the card
	// being carried has to pass over everything, and inside the scroll it would
	// be painted over by whatever the tab drew after it.
	ui.dragLayer = container.NewWithoutLayout()
	ui.logBody = container.NewVBox(logCard, recentCard)
	return container.NewStack(container.NewVScroll(ui.logBody), ui.dragLayer)
}

// newDateEntryISO builds a calendar-backed date field seeded from a YYYY-MM-DD
// string. Pair it with isoDate — never read .Text.
func newDateEntryISO(iso string) *widget.DateEntry {
	e := widget.NewDateEntry()
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(iso)); err == nil {
		e.SetDate(&t)
	}
	return e
}

// isoDate reads a DateEntry back as YYYY-MM-DD.
//
// The widget formats and parses in the system locale — "02/01/2006" by default
// — so writing its visible text into the CSV or the project's date field would
// silently record the wrong day. The picked *time.Time is the source of truth.
func isoDate(e *widget.DateEntry) string {
	if e.Date != nil {
		return e.Date.Format("2006-01-02")
	}
	// .Date is only populated once the widget has a renderer wired up, so also
	// accept plain ISO text, which is what the rest of the app speaks.
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(e.Text)); err == nil {
		return t.Format("2006-01-02")
	}
	return ""
}

func labeled(label string, obj fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(widget.NewLabelWithStyle(label, fyne.TextAlignLeading,
		fyne.TextStyle{}), obj)
}

func (ui *UI) renderPending() {
	res := ui.pending
	var objs []fyne.CanvasObject
	for _, e := range res.Errors {
		objs = append(objs, colorLabel(e, theme.ColorNameError))
	}

	// Ignored commits are still fetched, so they can be handed back without a
	// second round trip. They just do not belong in the count of work to do.
	//
	// The key counts every commit fetched, including the orgs switched off: it
	// is how they get switched back on, and "bigledger (0)" beside a list that
	// is only hiding them would be wrong twice over.
	byOrg := map[string]int{}
	var live, hidden []Group
	filteredOut := 0
	for _, g := range res.Groups {
		org := strings.ToLower(orgOf(g.Issue))
		byOrg[org] += len(g.Commits)
		if !ui.orgShown(org) {
			filteredOut += len(g.Commits)
			continue
		}
		if g.Ignored {
			hidden = append(hidden, g)
			continue
		}
		live = append(live, g)
	}
	remaining := 0
	for _, g := range live {
		remaining += len(g.Commits)
	}

	switch {
	case remaining > 0:
		rangeNote := ""
		if len(res.Range) == 2 {
			rangeNote = fmt.Sprintf(" from %s to %s", res.Range[0], res.Range[1])
		}
		objs = append(objs, widget.NewLabel(fmt.Sprintf(
			"%d unlogged commit(s)%s. Tap one to log it.", remaining, rangeNote)))
		objs = append(objs, ui.sections(live))
	// An empty list because of the filter is not an empty list. Saying there is
	// nothing to log, while the key at the foot of the tab counts commits
	// waiting under an org that was switched off, is the tab contradicting
	// itself.
	case filteredOut > 0:
		objs = append(objs, widget.NewLabel(fmt.Sprintf(
			"%d unlogged commit(s) hidden by the filter at the bottom of this tab.",
			filteredOut)))
	case len(res.Range) == 2:
		objs = append(objs, widget.NewLabel(fmt.Sprintf(
			"No unlogged commits between %s and %s.", res.Range[0], res.Range[1])))
	default:
		objs = append(objs, widget.NewLabel("No unlogged commits."))
	}

	// Ignoring is one click, so undoing it has to be too: the ignored tiles stay
	// one toggle away instead of only in a file on disk.
	if len(hidden) > 0 {
		label := fmt.Sprintf("Show %d ignored", len(hidden))
		if ui.showIgnored {
			label = fmt.Sprintf("Hide %d ignored", len(hidden))
		}
		toggle := widget.NewButton(label, func() {
			ui.showIgnored = !ui.showIgnored
			ui.renderPending()
		})
		toggle.Importance = widget.LowImportance
		objs = append(objs, container.NewHBox(toggle))
		if ui.showIgnored {
			objs = append(objs, ui.sections(hidden))
		}
	}

	ui.pendingBox.Objects = objs
	ui.pendingBox.Refresh()
	relayout(ui.logBody)
	ui.drawLogLegend()
}

// pendingSection is one issue's worth of tiles. Every commit still gets its own
// tile — several commits on one issue are related work, not one job, and the
// minutes are logged per commit — but they belong side by side under the name
// they are all filed against instead of scattered through the list.
//
// A tile that is the only one on its issue has nothing to be grouped with, so
// heading it would be a line of chrome per tile. Those fall into the last
// section, which carries no heading.
type pendingSection struct {
	issue  string // "" for the catch-all at the foot
	groups []Group
}

// pendingSections buckets tiles by issue, keeping the order they arrived in —
// newest first — both for the sections and for the tiles inside each one.
func pendingSections(groups []Group) []pendingSection {
	var order []string
	byIssue := map[string][]Group{}
	for _, g := range groups {
		if g.Issue == "" {
			continue
		}
		if _, seen := byIssue[g.Issue]; !seen {
			order = append(order, g.Issue)
		}
		byIssue[g.Issue] = append(byIssue[g.Issue], g)
	}

	var out []pendingSection
	for _, issue := range order {
		if len(byIssue[issue]) > 1 {
			out = append(out, pendingSection{issue: issue, groups: byIssue[issue]})
		}
	}
	var loose []Group
	for _, g := range groups {
		if len(byIssue[g.Issue]) < 2 { // includes the commits with no ref at all
			loose = append(loose, g)
		}
	}
	if len(loose) > 0 {
		out = append(out, pendingSection{groups: loose})
	}
	return out
}

// sections lays the tiles out an issue at a time: its name, its tiles, then a
// rule before the next one.
func (ui *UI) sections(groups []Group) fyne.CanvasObject {
	all := pendingSections(groups)
	var objs []fyne.CanvasObject
	for i, s := range all {
		if i > 0 {
			objs = append(objs, widget.NewSeparator())
		}
		// The loose tiles are "other" only when something named stands above
		// them. With nothing grouped they are the whole list, and a heading
		// over the only section says nothing.
		if s.issue != "" || len(all) > 1 {
			objs = append(objs, ui.sectionHeading(s))
		}
		objs = append(objs, ui.bubbleGrid(s.groups, s.issue != ""))
	}
	return container.NewVBox(objs...)
}

// sectionHeading names the issue its tiles are filed against, with the ref and
// the tally beside it. That name is what each tile would otherwise have printed
// for itself; saying it once above them is the point of the section. The last
// section is named for what it is: whatever had nothing to group with.
func (ui *UI) sectionHeading(s pendingSection) fyne.CanvasObject {
	commits := 0
	for _, g := range s.groups {
		commits += len(g.Commits)
	}
	tally := fmt.Sprintf("%d commits", commits)
	if commits == 1 {
		tally = "1 commit"
	}

	title, side := "Other commits", tally
	if s.issue != "" {
		info := ui.issueInfo[s.issue]
		if title = info.Title; title == "" {
			title = s.issue
		}
		side = fmt.Sprintf("%s  ·  %s", shortIssueLabel(info, s.issue), tally)
	}
	return container.NewHBox(
		widget.NewLabelWithStyle(truncate(title, bubbleTitleChars),
			fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle(side, fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
}

// bubbleGrid is one block of tiles, wrapped into as many rows as the width
// needs. underHeading travels down to the tiles so one in a named section does
// not repeat the name printed above it.
func (ui *UI) bubbleGrid(groups []Group, underHeading bool) fyne.CanvasObject {
	bubbles := make([]fyne.CanvasObject, 0, len(groups))
	for i := range groups {
		bubbles = append(bubbles, ui.groupBubble(groups[i], underHeading))
	}
	return container.New(newFlowGrid(bubbleMinWidth, bubbleMaxWidth, bubbleHeight), bubbles...)
}

// setIgnored hides a commit from the pending list, or brings it back. The
// cached result is patched in place: a refetch costs seconds and would return
// exactly the same commits.
func (ui *UI) setIgnored(g Group, ignored bool) {
	touched := map[string]bool{}
	for _, c := range g.Commits {
		if err := ui.store.SetIgnored(c.Sha, ignored); err != nil {
			ui.errf(err)
			return
		}
		touched[c.Sha] = true
	}
	for i := range ui.pending.Groups {
		cur := &ui.pending.Groups[i]
		if len(cur.Commits) > 0 && touched[cur.Commits[0].Sha] {
			cur.Ignored = ignored
		}
	}
	live := 0
	for _, cur := range ui.pending.Groups {
		if !cur.Ignored {
			live += len(cur.Commits)
		}
	}
	ui.pending.Count = live
	ui.renderPending()
}

// Pending tiles size themselves off the window: bubbleMinWidth only decides how
// many fit per row, and flowGrid then stretches them to use the full width, so
// there is no leftover strip on the right at an awkward window size.
// bubbleMaxWidth keeps a single pending tile from spanning a maximised screen.
const (
	bubbleMinWidth = 340
	bubbleMaxWidth = 560
	bubbleHeight   = 152
)

// bubbleTitleChars is what fits in two wrapped lines of the *narrowest* tile.
// Past that the title is elided, because fyne containers do not clip: a tile
// that overflows its height prints on top of the one below it. A wider tile
// simply runs out of text early — the full subject is in the popup.
const bubbleTitleChars = 72

// A pending tile gives part of its first line to the Ignore button, so its
// heading is capped shorter than a saved row's.
const pendingTitleChars = 60

// blendColor mixes b into a by fraction f, returning an opaque colour. Used to
// tint a theme background towards the accent without hard-coding either.
func blendColor(a, b color.Color, f float32) color.NRGBA {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	mix := func(x, y uint32) uint8 {
		v := float32(x>>8)*(1-f) + float32(y>>8)*f
		switch {
		case v < 0:
			v = 0
		case v > 255:
			v = 255
		}
		return uint8(v)
	}
	return color.NRGBA{R: mix(ar, br), G: mix(ag, bg), B: mix(ab, bb), A: 0xff}
}

// groupBubble is the compact summary of one pending group — one commit, since
// FetchPending no longer merges them. The issue's title leads: that is the name
// the work is filed and pushed under, and it is the same name the saved row
// will carry. The commit's own line sits under it, where it still tells two
// tiles on the same issue and day apart.
//
// underHeading flips that around: in a section already headed by the issue, the
// title would be the same on every tile, so the commit leads instead — that is
// the only thing telling those tiles apart.
func (ui *UI) groupBubble(g Group, underHeading bool) fyne.CanvasObject {
	info := ui.issueInfo[g.Issue]
	muted := theme.Color(theme.ColorNamePlaceHolder)
	caption := func(s string) *canvas.Text {
		t := canvas.NewText(truncate(s, 56), muted)
		t.TextSize = theme.CaptionTextSize()
		return t
	}

	// Heading: the issue this is filed against. The ref stands in while the
	// title is still loading, and the commit speaks for the tile only when
	// there is no issue at all.
	title := "No issue ref found"
	switch {
	case underHeading && len(g.Commits) == 1:
		title = commitSubject(g.Commits[0])
	case info.Title != "":
		title = info.Title
	case g.Issue != "":
		title = g.Issue
	case len(g.Commits) == 1:
		title = commitSubject(g.Commits[0])
	}
	titleLbl := widget.NewLabelWithStyle(
		truncate(title, pendingTitleChars), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	titleLbl.Wrapping = fyne.TextWrapWord

	// What the commit did, then where it came from. Stacked, not side by side:
	// on one row a long repo name and the date printed over each other.
	commitLine := ""
	if len(g.Commits) > 0 {
		commitLine = commitSubject(g.Commits[0])
	}
	foot := shortIssueLabel(info, g.Issue)
	if len(g.Commits) == 1 {
		foot += "  ·  " + g.Commits[0].Sha
	} else {
		plural := "commits"
		if len(g.Commits) == 1 {
			plural = "commit"
		}
		foot += fmt.Sprintf("  ·  %d %s", len(g.Commits), plural)
	}
	var lines []fyne.CanvasObject
	if commitLine != "" && commitLine != title { // in a section the heading is already the commit
		lines = append(lines, caption(commitLine))
	}
	lines = append(lines, caption(foot+"  ·  "+g.Date))
	footer := container.NewVBox(lines...)

	// Ignore takes a commit out of the list without inventing minutes for it —
	// a revert, a formatting sweep, work already covered by another entry. It
	// is reversible, so it asks nothing before acting.
	action := "Ignore"
	if g.Ignored {
		action = "Restore"
	}
	skip := widget.NewButton(action, func() { ui.setIgnored(g, !g.Ignored) })
	skip.Importance = widget.LowImportance
	// A spacer under it: in the right slot of a Border the button would
	// otherwise be stretched down the whole height of the tile.
	corner := container.NewVBox(skip, layout.NewSpacer())

	// Border, not VBox: the footer stays pinned to the bottom of the fixed-size
	// tile and the title takes whatever height is left, so tiles line up whether
	// their heading wraps to one line or two.
	body := container.NewBorder(nil, footer, nil, corner, titleLbl)

	// A plain Card is near-invisible on the dark theme — dark text on a dark
	// surface against a dark window. Tint the surface towards the org's colour
	// and give it a matching stripe, so each tile reads as its own tappable
	// thing and says which organisation it belongs to without a word.
	accent := orgColor(ui.cfg, orgOf(g.Issue))
	bg := canvas.NewRectangle(blendColor(
		theme.Color(theme.ColorNameInputBackground), accent, 0.16))
	bg.StrokeColor = blendColor(theme.Color(theme.ColorNameInputBorder), accent, 0.5)
	bg.StrokeWidth = 1
	bg.CornerRadius = 10

	stripe := canvas.NewRectangle(accent)
	stripe.SetMinSize(fyne.NewSize(4, 0))
	stripe.CornerRadius = 2

	// No inset here: flowGrid puts real space between cells, so the tile fills
	// the one it is given rather than shrinking inside it.
	inner := container.NewBorder(nil, nil, stripe, nil, container.NewPadded(body))
	tile := container.NewStack(bg, container.NewPadded(inner))
	return newTappable(tile, func() { ui.openGroupEditor(g) })
}

// shortIssueLabel drops the org from a ref. Every repo in view belongs to the
// same org, so "blg-int-general-task #6511" says the same as the full label in
// half the tile width.
func shortIssueLabel(info IssueInfo, ref string) string {
	if info.Repo != "" {
		return fmt.Sprintf("%s #%d", info.Repo, info.Number)
	}
	if _, repo, num, err := splitIssue(ref); err == nil {
		return fmt.Sprintf("%s #%d", repo, num)
	}
	return ref
}

// popupSize fills as much of the window as a dialog is allowed to take. Fyne
// clamps a dialog to its parent, so this is measured from the live canvas
// instead of hard-coded — resize the window and the next popup follows.
func (ui *UI) popupSize() fyne.Size {
	const minW, minH = 780, 540
	w, h := float32(minW), float32(minH)
	if ui.win != nil && ui.win.Canvas() != nil {
		if sz := ui.win.Canvas().Size(); sz.Width > 0 && sz.Height > 0 {
			w = sz.Width * 0.80
			h = sz.Height * 0.80
		}
	}
	// A freshly built window can measure smaller than its own content; keep a
	// usable floor so the form is never cramped.
	if w < minW {
		w = minW
	}
	if h < minH {
		h = minH
	}
	return fyne.NewSize(w, h)
}

// openGroupEditor shows the full logging form for one group in a popup.
func (ui *UI) openGroupEditor(g Group) {
	var d dialog.Dialog
	form := ui.groupEditor(g,
		// The list behind updates as soon as the row is written…
		func(logged []Commit) { ui.dropLoggedCommits(g, logged) },
		// …but the popup only closes once there is no result left to read, so
		// a push error or its URL is never hidden before it is seen.
		func() {
			if d != nil {
				d.Hide()
			}
		})
	heading := g.Date
	if info, ok := ui.issueInfo[g.Issue]; ok && info.Title != "" {
		heading = g.Date + "  ·  " + truncate(info.Title, 100)
	} else if g.Issue != "" {
		heading = g.Date + "  ·  " + g.Issue
	}
	// No dismiss button: dialog.NewCustom parks it centred at the bottom, past
	// the end of a long form. The back arrow rides at the top left instead,
	// where it is reachable without scrolling.
	back := widget.NewButtonWithIcon("Back", theme.NavigateBackIcon(), func() {
		if d != nil {
			d.Hide()
		}
	})
	back.Importance = widget.LowImportance
	d = dialog.NewCustomWithoutButtons(heading, popupShell(back, form), ui.win)
	d.Resize(ui.popupSize())
	d.Show()
}

// popupShell puts Back and the form's actions on one line above the body, with
// the result line under them. Both stay put while the form scrolls, so a push
// result is readable from wherever the remarks were left.
//
// Below the bar the fields take the height they need and the remarks take the
// rest, down to the bottom edge. The remarks used to sit inside the one scroll
// that held the whole form, which nested the entry's own scroller inside
// another — the wheel was swallowed by whichever one the pointer happened to be
// over, and over the remarks that meant the form would not move at all. Now no
// scroller contains another. topFill caps the fields at a share of the popup so
// a group with a long commit list scrolls rather than eating the remarks.
func popupShell(back fyne.CanvasObject, form editorForm) fyne.CanvasObject {
	fields := container.NewVScroll(form.body)
	bar := container.NewBorder(nil, nil, back, form.actions)
	return container.NewBorder(
		container.NewVBox(bar, form.msg), nil, nil, nil,
		container.New(newTopFill(0.45, form.body), fields, form.remarks))
}

// dropLoggedCommits removes the commits just written from the cached result, so
// the list updates instantly instead of paying for another fetch. Only the
// ticked commits go, since the rest are still unlogged.
func (ui *UI) dropLoggedCommits(g Group, logged []Commit) {
	gone := map[string]bool{}
	for _, c := range logged {
		gone[c.Sha] = true
	}
	var groups []Group
	for _, cur := range ui.pending.Groups {
		if cur.Date == g.Date && cur.Issue == g.Issue {
			var kept []Commit
			for _, c := range cur.Commits {
				if !gone[c.Sha] {
					kept = append(kept, c)
				}
			}
			if len(kept) == 0 {
				continue
			}
			cur.Commits = kept
		}
		groups = append(groups, cur)
	}
	ui.pending.Groups = groups
	ui.pending.Count = 0
	for _, cur := range groups {
		if cur.Ignored {
			continue // the count is work still to do, and this is not
		}
		ui.pending.Count += len(cur.Commits)
	}
	ui.renderPending()
	ui.drawRecent()
}

// editorForm is what a popup editor hands back in pieces, so the popup can
// place them itself. The action bar and the result line used to sit at the foot
// of the form, where a full set of remarks pushed them under the fold and left
// Back holding a line of its own at the top. Now Back, the buttons and the mode
// dropdown share that one line, and neither the buttons nor the push result can
// scroll out of sight. The remarks come out separately because they are the one
// part sized to the popup rather than to its content.
type editorForm struct {
	body    fyne.CanvasObject // the fields, sized to fit
	remarks fyne.CanvasObject // stretches to the foot of the popup
	actions fyne.CanvasObject
	msg     fyne.CanvasObject
}

// all is the whole form as one object, for callers that place it themselves
// rather than splitting it across a popup shell.
func (f editorForm) all() fyne.CanvasObject {
	return container.NewVBox(f.actions, f.msg, f.body, f.remarks)
}

// remarksEditor builds the remarks box both editors share: a multi-line entry
// that fills the foot of the popup, a live count against remarkCap, and the AI
// compaction button. The count hangs off OnChanged rather than being poked by
// every caller, so text written from code — a rebuilt set of remarks, an AI
// result — updates it too. The middle return is the whole pane, caption and
// box, ready to be handed the bottom of the popup.
func (ui *UI) remarksEditor(text string, msg *widget.Label) (*widget.Entry, fyne.CanvasObject, *widget.Button) {
	remE := widget.NewMultiLineEntry()
	// A floor, not a height: the pane hands the box whatever room is left below
	// the divider, which on any normal window is far more than six rows. Asking
	// for sixteen here would only stop the divider being dragged up.
	remE.SetMinRowsVisible(6)
	// A multi-line entry clips by default, so a long bullet ran off to the right
	// and scrolling down dragged the view sideways with it. Wrap instead: every
	// line stays inside the box and the only scrolling left is vertical.
	remE.Wrapping = fyne.TextWrapWord
	counter := widget.NewLabel("")
	update := func() { counter.SetText(fmt.Sprintf("%d / %d", len(remE.Text), remarkCap)) }
	remE.OnChanged = func(string) { update() }
	remE.SetText(text)
	update() // empty text is not a change, so OnChanged never fired

	aiBtn := widget.NewButton("Compact with AI", func() {
		before := len(remE.Text)
		text := remE.Text
		msg.SetText("Compacting…")
		var out string
		ui.async(func() error {
			o, err := aiCompact(ui.cfg, text)
			out = o
			return err
		}, func() {
			remE.SetText(out)
			msg.SetText(fmt.Sprintf("Compacted %d → %d chars. Check before pushing.", before, len(out)))
		})
	})
	// Border, not VBox: the caption keeps its own line and the box takes every
	// pixel under it, however tall the pane turns out to be.
	pane := container.NewBorder(
		container.NewHBox(widget.NewLabel("Worklog remarks"), counter),
		nil, nil, nil, remE)
	return remE, pane, aiBtn
}

// applyPushResult records what a push achieved on the row and returns a summary
// worth showing. Only a push with nothing outstanding sets pushed_at — a
// partial one has to stay retryable — so the caller is told which it was.
func (ui *UI) applyPushResult(id string, res PushResult) (string, bool) {
	patch := Row{"issue_url": res.URL, "item_id": res.ItemID}
	if res.Complete() {
		patch["pushed_at"] = time.Now().Format("2006-01-02T15:04:05")
	}
	if err := ui.store.UpdateRow(id, patch); err != nil {
		ui.errf(err)
	}
	summary := "Pushed → " + res.URL
	if len(res.FieldsSet) > 0 {
		summary += "\n\nSet: " + strings.Join(res.FieldsSet, ", ")
	}
	if len(res.Notes) > 0 {
		summary += "\n\n" + strings.Join(res.Notes, "\n")
	}
	if !res.Complete() {
		summary += "\n\nNot finished:\n" + strings.Join(res.Problems, "\n") +
			"\n\nThe row keeps its push button so you can retry."
	}
	return summary, res.Complete()
}

// openRowEditor opens a saved row in the same popup shell as a pending group,
// so an entry can be corrected before it is pushed — or after, if the wrong
// minutes went up.
func (ui *UI) openRowEditor(r Row, refresh func()) {
	var d dialog.Dialog
	form := ui.rowEditor(r, refresh, func() {
		if d != nil {
			d.Hide()
		}
	})
	heading := r["date"]
	if r["issue"] != "" {
		heading += "  ·  " + r["issue"]
	}
	back := widget.NewButtonWithIcon("Back", theme.NavigateBackIcon(), func() {
		if d != nil {
			d.Hide()
		}
	})
	back.Importance = widget.LowImportance
	d = dialog.NewCustomWithoutButtons(heading, popupShell(back, form), ui.win)
	d.Resize(ui.popupSize())
	d.Show()
}

// rowEditor is the group editor minus the commit picker: which commits the row
// covers is settled the moment it exists, so what is left to change is the
// worklog itself. Saving rewrites the row in place — no second entry is made,
// and the commits stay accounted for.
func (ui *UI) rowEditor(r Row, refresh func(), onFinished func()) editorForm {
	dateE := newDateEntryISO(r["date"])
	ownE := widget.NewEntry()
	ownE.SetText(orDefault(r["owner"], ui.cfg.WorklogOwner))
	minE := widget.NewEntry()
	minE.SetText(r["minutes"])
	minE.SetPlaceHolder("480")
	issE := widget.NewEntry()
	issE.SetText(r["issue"])
	issE.SetPlaceHolder("owner/repo#123")
	descE := widget.NewEntry()
	descE.SetText(r["description"])

	msg := widget.NewLabel("")
	// A manual entry carries its text in description and has no remarks yet;
	// seeding from it beats handing over an empty box that pushes as nothing.
	remE, remPane, aiBtn := ui.remarksEditor(orDefault(r["remarks"], r["description"]), msg)

	modeSel := widget.NewSelect([]string{"Worklog sub-issue", "The issue itself"}, nil)
	if r["mode"] == "issue" {
		modeSel.SetSelected("The issue itself")
	} else {
		modeSel.SetSelected("Worklog sub-issue")
	}
	modeVal := func() string {
		if modeSel.Selected == "The issue itself" {
			return "issue"
		}
		return "subissue"
	}

	save := func(push bool) {
		date := isoDate(dateE)
		if date == "" {
			msg.SetText("Pick a worklog date.")
			return
		}
		remarks := strings.TrimSpace(remE.Text)
		if len(remarks) > remarkCap {
			msg.SetText(fmt.Sprintf("Remarks are %d chars — trim to %d first.", len(remarks), remarkCap))
			return
		}
		mins, _ := strconv.Atoi(strings.TrimSpace(minE.Text))
		if push && mins <= 0 {
			msg.SetText("Enter the minutes before pushing.")
			return
		}
		patch := Row{
			"date": date, "minutes": strconv.Itoa(mins),
			"description": strings.TrimSpace(descE.Text),
			"issue":       strings.TrimSpace(issE.Text),
			"owner":       strings.TrimSpace(ownE.Text),
			"remarks":     remarks, "mode": modeVal(),
		}
		if err := ui.store.UpdateRow(r["id"], patch); err != nil {
			ui.errf(err)
			return
		}
		// Keep the copy this popup holds in step with the file, so a push that
		// follows a save uses the edited values and not the ones it opened with.
		for k, v := range patch {
			r[k] = v
		}
		refresh()
		if !push {
			msg.SetText("Saved.")
			if onFinished != nil {
				onFinished()
			}
			return
		}
		if r["issue"] == "" {
			msg.SetText("Saved — no issue ref to push to.")
			return
		}
		body := remarks
		if body == "" {
			body = r["description"]
		}
		if body == "" {
			msg.SetText("Saved — nothing to push: remarks and description are both empty.")
			return
		}
		msg.SetText("Pushing…")
		stop := ui.pushSpinner("Pushing to GitHub…")
		var res PushResult
		var perr error
		ui.async(func() error {
			res, perr = PushEntry(ui.cfg, r["issue"], r["date"], r["owner"],
				mins, body, orDefault(r["mode"], "issue"))
			return nil // handle the push error inline so the edit is not lost
		}, func() {
			stop()
			if perr != nil {
				msg.SetText("Saved, but push failed: " + perr.Error() + " Retry from this popup or the entry's push button.")
				refresh()
				return
			}
			summary, complete := ui.applyPushResult(r["id"], res)
			refresh()
			ui.refreshAfterPush(r["date"])
			msg.SetText(strings.ReplaceAll(summary, "\n\n", "  |  "))
			if complete && onFinished != nil {
				onFinished()
			}
		})
	}

	saveBtn := widget.NewButton("Save", func() { save(false) })
	pushBtn := widget.NewButton("Save & push", func() { save(true) })
	pushBtn.Importance = widget.HighImportance

	header := container.NewHBox(
		widget.NewLabelWithStyle(r["date"], fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		issueHyperlink(r["issue"]),
	)
	// The shas are stored bare, so the repo has to come off the issue ref for
	// them to link anywhere. Without a parseable ref they stay plain text.
	repo := ""
	if owner, name, _, err := splitIssue(r["issue"]); err == nil {
		repo = owner + "/" + name
	}
	for _, sha := range strings.Split(r["refs"], ",") {
		if sha = strings.TrimSpace(sha); sha != "" {
			header.Add(commitHyperlink(Commit{Repo: repo, Sha: sha}))
		}
	}

	var notes []fyne.CanvasObject
	if r["pushed_at"] != "" {
		notes = append(notes, colorLabel(
			"Already pushed on "+r["pushed_at"]+". Saving changes this copy only — "+
				"Save & push writes the edits over the same worklog item on GitHub.",
			theme.ColorNameWarning))
	}

	// Weighted, not quartered: an owner/repo#1234 ref needs the room a minutes
	// box does not, and the split holds as the popup follows the window.
	fields := container.NewVBox(
		container.New(newRatioRow(0.22, 0.22, 0.14, 0.42),
			labeled("Worklog date", dateE),
			labeled("Worklog owner", ownE),
			labeled("Worklog mins", minE),
			labeled("Issue", issE),
		),
		ui.dayFillLabel(dateE),
	)
	// Caption beside the dropdown rather than stacked over it: the bar shares a
	// line with Back, and a two-row label would drag that whole line taller.
	actions := container.NewHBox(
		widget.NewLabel("Push as"), wideSelect(modeSel), aiBtn, saveBtn, pushBtn,
	)
	return editorForm{
		body: container.NewVBox(append(notes,
			header,
			fields,
			labeled("Description (kept locally, not pushed)", descE))...),
		remarks: remPane,
		actions: actions,
		msg:     msg,
	}
}

// groupEditor is the logging form for one group. onLogged fires with the
// commits written to the store; onFinished fires when nothing is left on screen
// worth reading and the popup can close.
func (ui *UI) groupEditor(g Group, onLogged func([]Commit), onFinished func()) editorForm {
	// commit checkboxes
	checks := make([]*widget.Check, len(g.Commits))
	var commitRows []fyne.CanvasObject
	for i, c := range g.Commits {
		chk := widget.NewCheck("", nil)
		chk.SetChecked(true)
		checks[i] = chk
		row := container.NewBorder(nil, nil,
			container.NewHBox(chk, commitHyperlink(c)),
			widget.NewLabel(c.Repo),
			widget.NewLabel(truncate(c.Message, 70)))
		commitRows = append(commitRows, row)
	}

	dateE := newDateEntryISO(g.Date)
	ownE := widget.NewEntry()
	ownE.SetText(ui.cfg.WorklogOwner)
	minE := widget.NewEntry()
	minE.SetPlaceHolder("480")
	issE := widget.NewEntry()
	issE.SetText(g.Issue)
	issE.SetPlaceHolder("owner/repo#123")

	msg := widget.NewLabel("")
	remE, remPane, aiBtn := ui.remarksEditor(g.Remarks, msg)

	modeSel := widget.NewSelect([]string{"Worklog sub-issue", "The issue itself"}, nil)
	if ui.cfg.DefaultMode == "issue" {
		modeSel.SetSelected("The issue itself")
	} else {
		modeSel.SetSelected("Worklog sub-issue")
	}

	pickedCommits := func() []Commit {
		var picked []Commit
		for i, chk := range checks {
			if chk.Checked {
				picked = append(picked, g.Commits[i])
			}
		}
		return picked
	}
	modeVal := func() string {
		if modeSel.Selected == "The issue itself" {
			return "issue"
		}
		return "subissue"
	}

	doSave := func(push bool) {
		picked := pickedCommits()
		if len(picked) == 0 {
			msg.SetText("Tick at least one commit.")
			return
		}
		remarks := strings.TrimSpace(remE.Text)
		if remarks == "" {
			remarks = buildRemarks(picked)
			remE.SetText(remarks)
		}
		if remarks == "" {
			msg.SetText("Could not build remarks from the selected commit messages.")
			return
		}
		if len(remarks) > remarkCap {
			msg.SetText(fmt.Sprintf("Remarks are %d chars — trim to %d first.", len(remarks), remarkCap))
			return
		}
		date := isoDate(dateE)
		if date == "" {
			msg.SetText("Pick a worklog date.")
			return
		}
		shas := make([]string, 0, len(picked))
		mins, _ := strconv.Atoi(strings.TrimSpace(minE.Text))
		for _, c := range picked {
			shas = append(shas, c.Sha)
		}
		// The row's label is the issue it belongs to. It used to be every picked
		// commit's subject joined with semicolons, which read as noise in the
		// list and repeated what the remarks already say properly.
		desc := truncate(ui.rowLabel(strings.TrimSpace(issE.Text), picked), 200)
		made, err := ui.store.AppendRows([]Row{{
			"date": date, "minutes": strconv.Itoa(mins), "type": "commit",
			"description": desc, "refs": strings.Join(shas, ","),
			"issue": strings.TrimSpace(issE.Text), "owner": ownE.Text,
			"remarks": remarks, "mode": modeVal(),
		}})
		if err != nil {
			ui.errf(err)
			return
		}
		row := made[0]
		if onLogged != nil {
			onLogged(picked)
		}
		if !push {
			msg.SetText("Saved locally.")
			ui.drawRecent()
			if onFinished != nil {
				onFinished()
			}
			return
		}
		if row["issue"] == "" {
			// Nothing to push and nothing to read, so treat it as done.
			msg.SetText("Saved locally — no issue ref to push to.")
			ui.drawRecent()
			if onFinished != nil {
				onFinished()
			}
			return
		}
		msg.SetText("Pushing…")
		stop := ui.pushSpinner("Pushing to GitHub…")
		var res PushResult
		var perr error
		ui.async(func() error {
			res, perr = PushEntry(ui.cfg, row["issue"], row["date"], row["owner"], mins, row["remarks"], row["mode"])
			return nil // handle push error inline so the saved row is not lost
		}, func() {
			stop()
			if perr != nil {
				msg.SetText("Saved locally, but push failed: " + perr.Error() + " Use the push button on the entry to retry.")
				ui.drawRecent()
				return
			}
			// Only a clean push marks the row done; otherwise it stays
			// retryable from Recent entries.
			patch := Row{"issue_url": res.URL, "item_id": res.ItemID}
			if res.Complete() {
				patch["pushed_at"] = time.Now().Format("2006-01-02T15:04:05")
			}
			_ = ui.store.UpdateRow(row["id"], patch)
			ui.refreshAfterPush(row["date"])
			note := "Pushed → " + res.URL
			if len(res.FieldsSet) > 0 {
				note += "  |  Set: " + strings.Join(res.FieldsSet, ", ")
			}
			if len(res.Notes) > 0 {
				note += "  |  " + strings.Join(res.Notes, " · ")
			}
			if !res.Complete() {
				note += "  |  NOT FINISHED: " + strings.Join(res.Problems, " · ") +
					"  |  Retry from Recent entries."
			}
			msg.SetText(note)
			ui.drawRecent()
			if res.Complete() && onFinished != nil {
				onFinished()
			}
		})
	}

	saveBtn := widget.NewButton("Save only", func() { doSave(false) })
	pushBtn := widget.NewButton("Save & push", func() { doSave(true) })
	pushBtn.Importance = widget.HighImportance

	header := container.NewHBox(
		widget.NewLabelWithStyle(g.Date, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		issueHyperlink(g.Issue),
	)
	if info, ok := ui.issueInfo[g.Issue]; ok && info.Title != "" {
		header.Add(widget.NewLabel("·"))
		header.Add(widget.NewLabel(truncate(info.Title, 100)))
	}
	// Weighted, not quartered: an owner/repo#1234 ref needs the room a minutes
	// box does not, and the split holds as the popup follows the window.
	fields := container.NewVBox(
		container.New(newRatioRow(0.22, 0.22, 0.14, 0.42),
			labeled("Worklog date", dateE),
			labeled("Worklog owner", ownE),
			labeled("Worklog mins", minE),
			labeled("Issue", issE),
		),
		ui.dayFillLabel(dateE),
	)
	actions := container.NewHBox(
		widget.NewLabel("Push as"), wideSelect(modeSel), aiBtn, saveBtn, pushBtn,
	)
	return editorForm{
		body: container.NewVBox(
			header,
			container.NewVBox(commitRows...),
			fields,
		),
		remarks: remPane,
		actions: actions,
		msg:     msg,
	}
}

func issueTag(issue string) string {
	if issue == "" {
		return "no issue ref found"
	}
	return issue
}

func githubHyperlink(label, rawURL string) fyne.CanvasObject {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return widget.NewLabel(label)
	}
	return widget.NewHyperlink(label, u)
}

// commitHyperlink turns a commit's sha into a link to it on GitHub. The short
// sha resolves fine, so there is no need to carry the full one around.
func commitHyperlink(c Commit) fyne.CanvasObject {
	if c.Repo == "" || c.Sha == "" {
		return widget.NewLabel(c.Sha)
	}
	return githubHyperlink(c.Sha, fmt.Sprintf("https://github.com/%s/commit/%s", c.Repo, c.Sha))
}

func issueHyperlink(issue string) fyne.CanvasObject {
	if issue == "" {
		return widget.NewLabel(issueTag(issue))
	}
	owner, repo, number, err := splitIssue(issue)
	if err != nil {
		return widget.NewLabel(issue)
	}
	return githubHyperlink(issue, fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, number))
}

// ============================ recent entries ============================

func (ui *UI) drawRecent() {
	// The strip is part of this list, not a separate view: whatever changes the
	// saved entries — a drop, a push, an edit — changes what the week shows.
	ui.drawWeekStrip()
	if ui.recentBox == nil {
		return
	}
	rows, err := ui.store.ReadRows()
	if err != nil {
		ui.recentBox.Objects = []fyne.CanvasObject{colorLabel(err.Error(), theme.ColorNameError)}
		ui.recentBox.Refresh()
		relayout(ui.logBody)
		return
	}
	// The key at the foot of the tab governs this list too, not only the commits
	// above it: filtering the pending tiles to one employer and then listing the
	// other's saved entries underneath would defeat the point of filtering.
	rows = ui.shownRows(rows)
	localToday, localWaiting := 0, 0
	for _, r := range rows {
		if r["date"] != today() {
			continue
		}
		localToday += r.Minutes()
		if r["pushed_at"] == "" {
			localWaiting += r.Minutes()
		}
	}

	// Default view is the to-do list: rows saved locally that GitHub has not
	// seen yet, whatever month they belong to — an entry left unpushed since
	// last month is exactly the one worth surfacing. Ticking the box brings the
	// pushed ones back, scoped to this month so the list stays finite.
	//
	// An entry standing on a day of the week below is not repeated here: it is
	// already on show, on the day it will be pushed with. What is left in this
	// list is the work belonging to some other week — walk the strip to it, or
	// drag it onto a day in view.
	showAll := ui.showPushed != nil && ui.showPushed.Checked
	inStrip := map[string]bool{}
	for _, d := range weekDates(ui.weekStart) {
		inStrip[d] = true
	}
	var shown []Row
	pendingCount, onStrip := 0, 0
	for _, r := range rows {
		waiting := r["pushed_at"] == ""
		if waiting {
			pendingCount++
			if inStrip[r["date"]] {
				onStrip++
				continue
			}
		}
		switch {
		case showAll:
			if strings.HasPrefix(r["date"], thisMonth()) {
				shown = append(shown, r)
			}
		case waiting:
			shown = append(shown, r)
		}
	}
	sort.Slice(shown, func(i, j int) bool {
		return shown[i]["date"]+shown[i]["logged_at"] > shown[j]["date"]+shown[j]["logged_at"]
	})
	const maxShown = 25
	hidden := 0
	if len(shown) > maxShown {
		hidden = len(shown) - maxShown
		shown = shown[:maxShown]
	}

	head := fmt.Sprintf("%d waiting to push", pendingCount)
	if showAll {
		head = fmt.Sprintf("%s so far — %d still waiting to push",
			monthName(thisMonth()), pendingCount)
	}
	if onStrip > 0 {
		head += fmt.Sprintf("   ·   %d on the week below", onStrip)
	}

	objs := []fyne.CanvasObject{
		widget.NewLabelWithStyle(head, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel(ui.todayProgress(localToday, localWaiting)),
	}
	switch {
	case len(shown) > 0 || showAll:
		objs = append(objs, ui.entryTable(shown, ui.drawRecent))
	case onStrip > 0:
		// Not "nothing to do": everything waiting is standing on its day below.
		objs = append(objs, widget.NewLabel(
			"Everything still waiting is on the week below, on the day it will be pushed with."))
	default:
		objs = append(objs, widget.NewLabel("Everything logged has been pushed to GitHub."))
	}
	if hidden > 0 {
		objs = append(objs, widget.NewLabel(fmt.Sprintf("…and %d older entr(y/ies) not shown.", hidden)))
	}
	ui.recentBox.Objects = objs
	ui.recentBox.Refresh()
	relayout(ui.logBody)
	// The key counts this list as well as the commits, so it is redrawn whenever
	// either of them changes.
	ui.drawLogLegend()
}

// drawLogLegend fills the key at the foot of the Log work tab.
//
// It counts everything the key governs on that tab — commits waiting to be
// logged, and entries saved but not yet pushed — because the two empty at
// different times. Counting only the commits meant the key vanished the moment
// the last one was logged, while a list of saved entries was still sitting
// under it with no way left to filter them.
func (ui *UI) drawLogLegend() {
	if ui.logLegend == nil {
		return
	}
	byOrg := map[string]int{}
	for _, g := range ui.pending.Groups {
		byOrg[strings.ToLower(orgOf(g.Issue))] += len(g.Commits)
	}
	if rows, err := ui.store.ReadRows(); err == nil {
		for _, r := range rows {
			if r["pushed_at"] == "" {
				byOrg[rowOrg(r)]++
			}
		}
	}
	// And the worklogs already pushed, standing on the week below. They are the
	// third thing this key governs and the one that outlives the other two: a
	// commit stops being pending once it is logged and an entry stops waiting
	// once it is pushed, but it goes on showing on its day. Leaving it out meant
	// an org could be on screen with no way to switch it off.
	//
	// Counted straight from the cache, not through monthWorklogs, because that
	// applies the filter — and an org counted as zero because it is hidden is an
	// org that can never be brought back.
	weekStart := ui.weekStart
	if weekStart == "" {
		weekStart = weekStartOf(today())
	}
	onWeek := map[string]bool{}
	for _, d := range weekDates(weekStart) {
		onWeek[d] = true
	}
	for _, m := range weekMonths(weekStart) {
		items, ok := ui.itemsFor(statusCacheKey(m))
		if !ok {
			continue
		}
		for _, it := range items {
			if onWeek[it.Date] {
				byOrg[itemOrg(it)]++
			}
		}
	}
	ui.logLegend.Objects = nil
	if key := orgLegendWith(ui.cfg, byOrg, ui.orgOff,
		strconv.Itoa, ui.toggleOrg); key != nil {
		ui.logLegend.Objects = []fyne.CanvasObject{key}
	}
	ui.logLegend.Refresh()
	relayout(ui.logBody)
}

// todayProgress describes how much of the daily target is actually banked.
//
// It scores against the GitHub project, not the local CSV: a row saved here but
// never pushed is not on the board, and a worklog pushed from another machine
// is. Minutes still sitting locally are reported separately rather than folded
// in, so the gap between "typed it" and "it counts" stays visible.
func (ui *UI) todayProgress(localToday, localWaiting int) string {
	banked, state := ui.githubMinutesToday()

	var s string
	switch state {
	case "ok":
		s = fmt.Sprintf("Today on GitHub: %d/%d min", banked, target)
		if banked >= target {
			s += " ✓"
		} else {
			s += fmt.Sprintf(" (%d left)", target-banked)
		}
	case "loading":
		return fmt.Sprintf("Today: %d/%d min saved locally — checking GitHub…", localToday, target)
	case "off":
		return fmt.Sprintf("Today: %d/%d min saved locally. Set a project URL in Settings to score against GitHub.",
			localToday, target)
	default: // "error"
		return fmt.Sprintf("Today: %d/%d min saved locally — could not read the GitHub project.",
			localToday, target)
	}
	if localWaiting > 0 {
		s += fmt.Sprintf("   ·   %d min saved locally, not pushed yet", localWaiting)
	}
	return s
}

// githubMinutesToday sums today's worklog minutes from the project.
func (ui *UI) githubMinutesToday() (int, string) { return ui.githubMinutesOn(today()) }

// githubMinutesOn sums one day's worklog minutes from the project, kicking that
// month's fetch off the first time it is asked. State is one of "ok",
// "loading", "off" (no project configured) or "error".
func (ui *UI) githubMinutesOn(date string) (int, string) {
	if len(date) < 7 {
		return 0, "off"
	}
	items, state := ui.monthWorklogs(date[:7])
	if state != "ok" {
		return 0, state
	}
	total := 0
	for _, it := range items {
		if it.Date == date {
			total += it.Minutes
		}
	}
	return total, "ok"
}

// dayFillLabel is the readout beside a worklog date: how much of that day is
// already on GitHub. Logging is done a date at a time, often days late, so the
// question the form has to answer is "how much does this day still owe" — not
// answering it meant opening the Status tab to find out.
func (ui *UI) dayFillLabel(dateE *widget.DateEntry) fyne.CanvasObject {
	lbl := widget.NewLabel("")
	update := func() {
		date := isoDate(dateE)
		if date == "" {
			lbl.SetText("Pick a date to see how full it is.")
			return
		}
		mins, state := ui.githubMinutesOn(date)
		switch state {
		case "ok":
			pct := int(float64(mins) / float64(target) * 100)
			txt := fmt.Sprintf("%s already on GitHub: %s of %s (%d%%)",
				date, hoursMins(mins), hoursMins(target), pct)
			switch {
			case mins >= target:
				txt += " — full"
			default:
				txt += fmt.Sprintf(", %s left", hoursMins(target-mins))
			}
			lbl.SetText(txt)
		case "loading":
			lbl.SetText(date + ": checking GitHub…")
		case "off":
			lbl.SetText("Set a project URL in Settings to see how full a day is.")
		default:
			lbl.SetText(date + ": could not read the GitHub project.")
		}
	}
	update()
	// The date can be typed or picked off the calendar; OnChanged covers both.
	dateE.OnChanged = func(*time.Time) { update() }
	return lbl
}

// entryTable lays the saved rows out as tiles, the same shape the pending
// commits use above them. A row is a thing you act on — push it, fix it, drop
// it — and the buttons were the narrowest column of the table that replaced.
func (ui *UI) entryTable(rows []Row, refresh func()) fyne.CanvasObject {
	if len(rows) == 0 {
		return widget.NewLabel("Nothing logged yet.")
	}
	tiles := make([]fyne.CanvasObject, 0, len(rows))
	for _, r := range rows {
		tile := ui.rowTile(r, refresh)
		if r["pushed_at"] == "" {
			// Still ours to move: it can be dragged onto a day in the strip
			// above. A pushed entry is already a date on the board, so it stays
			// put and is edited the long way if it really has to change.
			tile = newDragTile(ui, r, tile)
		}
		tiles = append(tiles, tile)
	}
	return container.New(newFlowGrid(bubbleMinWidth, bubbleMaxWidth, rowTileHeight), tiles...)
}

// rowTileHeight fits two wrapped lines of description over a footer and a row
// of buttons. Taller than a pending tile, which carries no buttons.
const rowTileHeight = 176

// rowLabel names a saved row: the issue's own title where it is known, and the
// commit's first real line where it is not — a row logged before the title
// arrived, or one with no issue ref at all.
func (ui *UI) rowLabel(issue string, commits []Commit) string {
	if info, ok := ui.issueInfo[issue]; ok && strings.TrimSpace(info.Title) != "" {
		return info.Title
	}
	if len(commits) == 1 {
		return commitSubject(commits[0])
	}
	if len(commits) > 1 {
		return fmt.Sprintf("%s (%d commits)", commitSubject(commits[0]), len(commits))
	}
	return issue
}

// rowTile is one locally saved entry: what it is, when, how long, and what can
// still be done to it.
func (ui *UI) rowTile(r Row, refresh func()) fyne.CanvasObject {
	accent := orgColor(ui.cfg, orgOf(r["issue"]))
	muted := theme.Color(theme.ColorNamePlaceHolder)
	caption := func(s string) *canvas.Text {
		t := canvas.NewText(truncate(s, 56), muted)
		t.TextSize = theme.CaptionTextSize()
		return t
	}

	// The issue title wins when it is known, so a row saved before the title
	// loaded still reads as the issue rather than as whatever was to hand.
	what := ""
	if info, ok := ui.issueInfo[r["issue"]]; ok {
		what = strings.TrimSpace(info.Title)
	}
	if what == "" {
		what = strings.TrimSpace(r["description"])
	}
	if what == "" {
		what = strings.TrimSpace(r["remarks"])
	}
	if what == "" {
		what = "(no description)"
	}
	title := widget.NewLabelWithStyle(truncate(what, bubbleTitleChars),
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Wrapping = fyne.TextWrapWord

	issue := r["issue"]
	if issue == "" {
		issue = orDash(r["type"]) + " — no issue ref"
	} else {
		issue = shortIssueLabel(ui.issueInfo[r["issue"]], r["issue"])
	}
	state := "waiting to push"
	if r["pushed_at"] != "" {
		state = "pushed " + strings.Replace(r["pushed_at"], "T", " ", 1)
	}
	// Minutes, not hours and minutes: this is the number that goes in the
	// worklog field, and reading "1h 30m" here meant converting it back to 90
	// every time before pushing.
	foot := container.NewVBox(
		caption(fmt.Sprintf("%s  ·  %d min  ·  %s", r["date"], r.Minutes(), issue)),
		caption(state))

	push, edit, del := ui.rowActions(r, refresh)
	actions := container.NewHBox(push, layout.NewSpacer(), edit, del)

	body := container.NewBorder(nil, container.NewVBox(foot, actions), nil, nil, title)
	bg := canvas.NewRectangle(blendColor(
		theme.Color(theme.ColorNameInputBackground), accent, 0.10))
	bg.StrokeColor = blendColor(theme.Color(theme.ColorNameInputBorder), accent, 0.5)
	bg.StrokeWidth = 1
	bg.CornerRadius = 10
	stripe := canvas.NewRectangle(accent)
	stripe.SetMinSize(fyne.NewSize(4, 0))
	stripe.CornerRadius = 2
	inner := container.NewBorder(nil, nil, stripe, nil, container.NewPadded(body))
	return container.NewStack(bg, container.NewPadded(inner))
}

// rowActions are the three things that can be done to a saved entry. The same
// buttons serve the tile in the list and the card sitting on its day in the week
// strip, so the two never drift apart.
//
// Push doubles as re-push once a row has been sent: the fields on the board are
// overwritten, and the sub-issue is reused rather than made twice.
func (ui *UI) rowActions(r Row, refresh func()) (push, edit, del *widget.Button) {
	label := "Push"
	if r["pushed_at"] != "" {
		label = "Re-push"
	}
	push = widget.NewButton(label, func() { ui.pushRow(r, refresh) })
	push.Importance = widget.HighImportance
	if r["issue"] == "" {
		push.Disable() // nothing to push to; the editor is where a ref is added
	}
	edit = widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		ui.openRowEditor(r, refresh)
	})
	edit.Importance = widget.LowImportance
	del = widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		dialog.ShowConfirm("Delete entry",
			"Delete this entry? Its commits become pending again. (Anything already pushed to GitHub stays there.)",
			func(ok bool) {
				if !ok {
					return
				}
				if err := ui.store.DeleteRow(r["id"]); err != nil {
					ui.errf(err)
					return
				}
				refresh()
			}, ui.win)
	})
	del.Importance = widget.LowImportance
	return push, edit, del
}

// pushRow sends one saved row to GitHub and reports what landed.
func (ui *UI) pushRow(r Row, refresh func()) {
	remarks := strings.TrimSpace(r["remarks"])
	if remarks == "" {
		remarks = strings.TrimSpace(r["description"])
	}
	stop := ui.pushSpinner("Pushing to GitHub…")
	var res PushResult
	var perr error
	ui.async(func() error {
		res, perr = PushEntry(ui.cfg, r["issue"], r["date"], r["owner"],
			r.Minutes(), remarks, orDefault(r["mode"], "issue"))
		// Reported here rather than handed to async: a failure has to take the
		// spinner down with it, and async skips its done func on an error.
		return nil
	}, func() {
		stop()
		if perr != nil {
			ui.errf(perr)
			return
		}
		summary, complete := ui.applyPushResult(r["id"], res)
		refresh()
		ui.refreshAfterPush(r["date"])
		title := "Push incomplete"
		if complete {
			title = "Push complete"
		}
		dialog.ShowInformation(title, summary, ui.win)
	})
}

// ============================ Status (calendar) ============================

func (ui *UI) buildStatusTab() fyne.CanvasObject {
	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		ui.calMonth = shiftMonth(ui.calMonth, -1)
		ui.selDay = ""
		ui.drawCalendar()
		ui.loadStatus(false)
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		ui.calMonth = shiftMonth(ui.calMonth, 1)
		ui.selDay = ""
		ui.drawCalendar()
		ui.loadStatus(false)
	})
	ui.calTitle = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	ui.calSummary = widget.NewLabel("")
	refresh := widget.NewButtonWithIcon("Refresh from GitHub", theme.ViewRefreshIcon(), func() {
		ui.ensureProjectCache()
		delete(ui.projLoaded, statusCacheKey(ui.calMonth))
		ui.drawCalendar()
		ui.loadStatus(true)
	})
	head := container.NewHBox(prev, ui.calTitle, next, layout.NewSpacer(), ui.calSummary, refresh)

	// Stack, not VBox: a VBox gives the grid its minimum height and parks the
	// rest of the tab empty. The grid has to take everything going, since a
	// month of cells is the whole point of this tab.
	ui.calBox = container.NewStack()
	ui.calLegend = container.NewVBox()
	ui.dayPanel = container.NewVBox()
	// A Stack, so the prompt can be centred against the panel's full height.
	// Inside a scroll it would sit at the top, because a scroll gives its
	// content the content's own height and nothing more.
	ui.daySwap = container.NewStack(dayPanelHint())

	// The day's detail rides beside the calendar, not under it. Underneath, a
	// day picked in the last week sat a full month's scroll below the click
	// that opened it. Its own scroll keeps a long day off the calendar's back.
	width := canvas.NewRectangle(color.Transparent)
	width.SetMinSize(fyne.NewSize(dayPanelWidth, 0))
	ui.daySide = container.NewStack(width, ui.daySwap)

	// No scroll around the calendar: a scroll sizes its content to the content's
	// minimum, which is what kept the month to half the window. Border hands the
	// grid every pixel the header and legend do not use.
	ui.calBody = container.NewBorder(head, ui.calLegend, nil, nil, ui.calBox)
	calCard := widget.NewCard("", "", ui.calBody)
	return container.NewBorder(nil, nil, nil, ui.daySide, calCard)
}

// dayPanelWidth is the side panel's share of the window. Wide enough for a
// remark to wrap at a readable measure, narrow enough that the calendar it sits
// beside keeps seven usable columns.
const dayPanelWidth = 380

func (ui *UI) drawCalendar() {
	if ui.calBox == nil {
		return
	}
	ui.calTitle.SetText(monthName(ui.calMonth))
	// Status reflects the GitHub project, filtered to the worklog owner.
	key := statusCacheKey(ui.calMonth)
	items, cached := ui.itemsFor(key)
	if !cached {
		ui.ensureProjectCache()
		if ui.projErr[key] != nil && !ui.projLoading[key] {
			ui.calBox.Objects = []fyne.CanvasObject{colorLabel(ui.projErr[key].Error(), theme.ColorNameError)}
			ui.calBox.Refresh()
			relayout(ui.calBody)
			ui.calSummary.SetText("")
			return
		}
		ui.calBox.Objects = []fyne.CanvasObject{widget.NewLabel("Loading worklogs from GitHub…")}
		ui.calBox.Refresh()
		relayout(ui.calBody)
		ui.calSummary.SetText("")
		return
	}
	// Split each day by org so the cells can be coloured. The key is counted from
	// everything in the month, filtered or not — it is what the filter is worked
	// from, so an org that has been switched off still has to appear in it with
	// the hours it would be worth switched back on.
	byDayOrg := map[string]map[string]int{}
	monthByOrg := map[string]int{}
	monthTotal := 0
	for _, it := range items {
		if !strings.HasPrefix(it.Date, ui.calMonth) {
			continue
		}
		org := itemOrg(it)
		monthByOrg[org] += it.Minutes
		if !ui.orgShown(org) {
			continue
		}
		if byDayOrg[it.Date] == nil {
			byDayOrg[it.Date] = map[string]int{}
		}
		byDayOrg[it.Date][org] += it.Minutes
		monthTotal += it.Minutes
	}
	t, _ := time.Parse("2006-01", ui.calMonth)
	year, mon := t.Year(), int(t.Month())
	daysIn := time.Date(year, time.Month(mon)+1, 0, 0, 0, 0, 0, time.UTC).Day()
	// Monday-first offset
	firstWd := (int(time.Date(year, time.Month(mon), 1, 0, 0, 0, 0, time.UTC).Weekday()) + 6) % 7

	grid := container.NewGridWithColumns(7)
	for _, d := range []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"} {
		grid.Add(widget.NewLabelWithStyle(d, fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}))
	}
	for i := 0; i < firstWd; i++ {
		grid.Add(widget.NewLabel(""))
	}
	for day := 1; day <= daysIn; day++ {
		ds := fmt.Sprintf("%s-%s", ui.calMonth, pad2(day))
		grid.Add(ui.dayCell(ds, day, byDayOrg[ds]))
	}
	ui.calBox.Objects = []fyne.CanvasObject{grid}
	ui.calBox.Refresh()
	relayout(ui.calBody)
	ui.calLegend.Objects = nil
	if key := orgLegend(ui.cfg, monthByOrg, ui.orgOff, ui.toggleOrg); key != nil {
		ui.calLegend.Objects = []fyne.CanvasObject{key}
	}
	ui.calLegend.Refresh()
	relayout(ui.calBody)
	ui.calSummary.SetText(fmt.Sprintf("%d min logged this month", monthTotal))
	// Always redrawn: with no day picked it shows the prompt to pick one.
	ui.drawDayPanel()
}

func (ui *UI) dayCell(ds string, day int, byOrg map[string]int) fyne.CanvasObject {
	m := 0
	for _, v := range byOrg {
		m += v
	}
	pc := 0
	if m > 0 {
		pc = int(float64(m) / float64(target) * 100)
	}
	dayLbl := widget.NewLabelWithStyle(strconv.Itoa(day), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	var info fyne.CanvasObject = widget.NewLabel("")
	if m > 0 {
		txt := fmt.Sprintf("%dm  %d%%", m, pc)
		if m > target {
			txt += " +"
		}
		if isShortDay(m) {
			// The same warning colour as the report's short-day rows and the
			// chart's target line. A cell's fill already says how full the day
			// is, but only against itself — the colour is what says the day is
			// short without the percentage having to be read.
			short := canvas.NewText(txt, theme.Color(theme.ColorNameWarning))
			short.TextSize = theme.TextSize()
			short.TextStyle = fyne.TextStyle{Monospace: true}
			info = short
		} else {
			info = widget.NewLabelWithStyle(txt, fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
		}
	}

	body := container.NewVBox(dayLbl, layout.NewSpacer(), info)
	border := canvas.NewRectangle(color.Transparent)
	border.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	border.StrokeWidth = 1
	border.CornerRadius = cellCornerRadius
	if ds == ui.selDay {
		border.StrokeColor = theme.Color(theme.ColorNamePrimary)
		border.StrokeWidth = 2
	}
	// A floor, not a size: the grid hands every cell an equal share of the
	// month's height, and this only stops that share collapsing to nothing on
	// a short window.
	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(0, dayCellMinHeight))

	// The fill is the day itself — the cell fills from the bottom in each org's
	// colour, so a glance down the month shows both how full a day was and who
	// it was for.
	stack := container.NewStack(floor,
		vMeterFill(ui.cfg, byOrg, target, cellCornerRadius), border,
		container.NewPadded(body))
	return newTappable(stack, func() {
		ui.selDay = ds
		ui.drawCalendar()
		ui.drawDayPanel()
	})
}

// dayCellMinHeight keeps a cell legible when the window is too short for the
// calendar to take its full height. cellCornerRadius is shared by the cell's
// frame and the fill inside it — two different radii would leave the fill
// showing at the corners of the frame it is meant to sit in.
const (
	dayCellMinHeight = 58
	cellCornerRadius = 8
)

func (ui *UI) drawDayPanel() {
	// The panel keeps its place whether or not a day is picked: hiding it made
	// the calendar jump a column wider and back every time one was opened.
	if ui.selDay == "" {
		ui.daySwap.Objects = []fyne.CanvasObject{dayPanelHint()}
		ui.daySwap.Refresh()
		return
	}
	monthItems, _ := ui.itemsFor(statusCacheKey(ui.calMonth))
	var dayItems []WorklogItem
	for _, it := range monthItems {
		// A day opened from a filtered calendar shows the same work the cell was
		// drawn from; the panel is that cell spelled out, not a second opinion.
		if it.Date == ui.selDay && ui.orgShown(itemOrg(it)) {
			dayItems = append(dayItems, it)
		}
	}
	// Biggest first. The day is opened to see where its hours went, and the item
	// that took the most of them is the answer; fetch order buried it wherever
	// the project happened to return it. Stable, so equal items keep that order
	// rather than shuffling between redraws.
	sort.SliceStable(dayItems, func(i, j int) bool {
		return dayItems[i].Minutes > dayItems[j].Minutes
	})

	byOrg := minutesByOrg(dayItems)
	total := 0
	for _, v := range byOrg {
		total += v
	}
	pc := 0
	if total > 0 {
		pc = int(float64(total) / float64(target) * 100)
	}

	close := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		ui.selDay = ""
		ui.drawCalendar()
		ui.drawDayPanel()
	})
	close.Importance = widget.LowImportance
	head := container.NewBorder(nil, nil, nil, close,
		widget.NewLabelWithStyle(ui.selDay, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))

	// Same meter as the calendar cell, at a size worth reading: the day against
	// the target, split by org, with the split spelled out underneath.
	body := []fyne.CanvasObject{
		head,
		widget.NewLabelWithStyle(fmt.Sprintf("%d/%d min (%d%%)", total, target, pc),
			fyne.TextAlignLeading, fyne.TextStyle{Monospace: true}),
		meterBar(ui.cfg, byOrg, target, 12),
	}
	for _, org := range orgsByShare(ui.cfg, byOrg) {
		body = append(body, container.New(newRatioRow(0.08, 0.52, 0.40),
			orgDot(ui.cfg, org),
			widget.NewLabel(org),
			widget.NewLabelWithStyle(hoursMins(byOrg[org]),
				fyne.TextAlignTrailing, fyne.TextStyle{Monospace: true})))
	}
	if len(dayItems) == 0 {
		body = append(body, widget.NewLabel("No worklog items on this day for "+
			orDefault(ui.cfg.WorklogOwner, "you")+"."))
	}

	for _, it := range dayItems {
		it := it
		org := itemOrg(it)
		title := widget.NewLabel(truncate(it.Title, 80))
		title.Wrapping = fyne.TextWrapWord
		head := container.New(newRatioRow(0.10, 0.24, 0.66),
			orgDot(ui.cfg, org),
			widget.NewLabelWithStyle(fmt.Sprintf("%d min", it.Minutes),
				fyne.TextAlignLeading, fyne.TextStyle{Bold: true, Monospace: true}),
			title)
		// The parent issue is the name the work is filed under, so it sits with
		// the duration rather than as a bare "Parent issue" link at the foot of
		// the card: the day panel says what was worked on, and one click on the
		// name opens it on GitHub.
		var parent fyne.CanvasObject
		if it.ParentTitle != "" || it.ParentURL != "" {
			parent = container.NewHBox(
				widget.NewLabelWithStyle("Parent", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
				githubHyperlink(truncate(orDefault(it.ParentTitle, "issue"), 60), it.ParentURL))
		}

		var links []fyne.CanvasObject
		if it.URL != "" {
			links = append(links, githubHyperlink("Worklog", it.URL))
		}
		var link fyne.CanvasObject = widget.NewLabel("")
		if len(links) > 0 {
			link = container.NewHBox(links...)
		}
		rem := widget.NewLabel(it.Remarks)
		rem.Wrapping = fyne.TextWrapWord

		card := []fyne.CanvasObject{head}
		if parent != nil {
			card = append(card, parent)
		}
		card = append(card, rem, link)

		// A stripe in the org's colour, the same cue the pending tiles carry.
		stripe := canvas.NewRectangle(orgColor(ui.cfg, org))
		stripe.SetMinSize(fyne.NewSize(3, 0))
		body = append(body, widget.NewCard("", "", container.NewBorder(nil, nil, stripe, nil,
			container.NewPadded(container.NewVBox(card...)))))
	}

	ui.dayPanel.Objects = []fyne.CanvasObject{container.NewVBox(body...)}
	ui.dayPanel.Refresh()
	// A day's detail can run past the panel, so it gets the scroll the prompt
	// does not want.
	ui.daySwap.Objects = []fyne.CanvasObject{
		container.NewVScroll(container.NewPadded(ui.dayPanel))}
	ui.daySwap.Refresh()
}

// dayPanelHint is what the side panel shows with no day picked: centred, so an
// empty panel reads as waiting rather than as a heading with nothing under it.
func dayPanelHint() fyne.CanvasObject {
	hint := widget.NewLabelWithStyle("Select any date to show its details.",
		fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	hint.Wrapping = fyne.TextWrapWord
	return container.NewCenter(hint)
}

// ============================ Report ============================

func (ui *UI) buildReportTab() fyne.CanvasObject {
	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		ui.repMonth = shiftMonth(ui.repMonth, -1)
		ui.drawReport()
		ui.loadReport(false)
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		ui.repMonth = shiftMonth(ui.repMonth, 1)
		ui.drawReport()
		ui.loadReport(false)
	})
	ui.repTitle = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	exportBtn := widget.NewButton("Export CSV", func() { ui.exportCSV() })
	refresh := widget.NewButtonWithIcon("Refresh from GitHub", theme.ViewRefreshIcon(), func() {
		ui.ensureProjectCache()
		delete(ui.projLoaded, reportCacheKey(ui.repMonth))
		ui.drawReport()
		ui.loadReport(true)
	})
	head := container.NewHBox(prev, ui.repTitle, next, layout.NewSpacer(), refresh, exportBtn)

	ui.repBox = container.NewVBox()
	ui.repBody = container.NewVBox(head, ui.repBox)
	return container.NewVScroll(widget.NewCard("", "", ui.repBody))
}

func (ui *UI) drawReport() {
	if ui.repBox == nil {
		return
	}
	ui.repTitle.SetText(reportPeriodName(ui.repMonth))
	if !ui.hasCfg {
		ui.repBox.Objects = []fyne.CanvasObject{widget.NewLabel("Open Settings first.")}
		ui.repBox.Refresh()
		relayout(ui.repBody)
		return
	}
	// Report reflects the GitHub project, filtered to the worklog owner.
	key := reportCacheKey(ui.repMonth)
	fetched, cached := ui.itemsFor(key)
	// The fetch reaches months before the period so the weekend support carry
	// can be seen; everything the report reports on is the period alone.
	fromDate, toDate, _ := reportBounds(ui.repMonth)
	var all, runUp []WorklogItem
	for _, it := range fetched {
		switch {
		case it.Date < fromDate:
			runUp = append(runUp, it)
		case it.Date <= toDate:
			all = append(all, it)
		}
	}
	// Everything below the key reads the filtered set — the money included. The
	// period is worth a different amount depending on whose work is in it, and
	// showing one employer's chart over both employers' pay would be a lie in
	// the only place on the tab that has to be right.
	items := ui.shownItems(all)
	if !cached {
		ui.ensureProjectCache()
		if ui.projErr[key] != nil && !ui.projLoading[key] {
			ui.repBox.Objects = []fyne.CanvasObject{colorLabel(ui.projErr[key].Error(), theme.ColorNameError)}
			ui.repBox.Refresh()
			relayout(ui.repBody)
			return
		}
		ui.repBox.Objects = []fyne.CanvasObject{widget.NewLabel("Loading worklogs from GitHub…")}
		ui.repBox.Refresh()
		relayout(ui.repBody)
		return
	}
	totals := totalsFromItems(items, "")
	rep := reportFromTotals(ui.cfg, totals)
	// What the run-up could not make a set out of comes forward. A remainder,
	// so a period never gets paid for work an earlier report already paid for.
	carried := weekendSupportIssues(ui.shownItems(runUp)) % supportSetSize
	supportIssues := weekendSupportIssues(items)
	supportSets, supportBonus := weekendSupportBonus(carried, supportIssues)
	totalReceivable := rep.Receivable + supportBonus

	money := func(v float64) string { return rep.Currency + " " + commaAmount(v) }

	// Working days are read off the calendar, so the tile is right even for a
	// period nobody has logged a minute into yet.
	wdGone, wdTotal := workingDaysProgress(fromDate, toDate, today())
	wdLeft := wdTotal - wdGone
	wdNote := fmt.Sprintf("Working days gone — %d left", wdLeft)
	if wdTotal != divisor {
		// Pay still divides by 21 whatever the calendar says; say so rather
		// than let a 22-day period read like a miscount.
		wdNote += fmt.Sprintf(" (pay divides by %d)", divisor)
	}

	supportNote := fmt.Sprintf("%d issues → %d set(s)", supportIssues, supportSets)
	if carried > 0 {
		supportNote = fmt.Sprintf("%d carried in + %s", carried, supportNote)
	}
	if left := (carried + supportIssues) % supportSetSize; left > 0 {
		supportNote += fmt.Sprintf(", %d carry to next", left)
	}
	supportNote = "Weekend support bonus (" + supportNote + ")"

	stats := container.NewGridWithColumns(3,
		statTile(money(totalReceivable), "Receivable including support"),
		statTile(money(supportBonus), supportNote),
		statTile(fmt.Sprintf("%.2f", rep.PayableDays), "Payable days × "+money(rep.DailyRate)),
		statTile(fmt.Sprintf("%d/%d", rep.DaysComplete, rep.DaysLogged), "Complete / logged"),
		statTile(fmt.Sprintf("%d/%d", wdGone, wdTotal), wdNote),
		statTile(fmt.Sprintf("%.1fh", float64(rep.TotalMin)/60), "Time logged"),
		statTile(strconv.Itoa(len(items)), "Worklog items ("+orDefault(ui.cfg.WorklogOwner, "all")+")"),
	)

	// The chart is per day, so it needs the split per day — the period totals
	// only say who the month belonged to, not which day was whose. Every day in
	// the period gets a slot, logged or not, so the axis is a real calendar.
	byDayOrg := map[string]map[string]int{}
	for _, it := range items {
		if byDayOrg[it.Date] == nil {
			byDayOrg[it.Date] = map[string]int{}
		}
		byDayOrg[it.Date][itemOrg(it)] += it.Minutes
	}
	var days []chartDay
	if from, err := time.Parse("2006-01-02", fromDate); err == nil {
		to, _ := time.Parse("2006-01-02", toDate)
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			ds := d.Format("2006-01-02")
			day := chartDay{date: ds, byOrg: byDayOrg[ds]}
			for _, m := range day.byOrg {
				day.total += m
			}
			days = append(days, day)
		}
	}
	chart := ui.chartWithReadout(days, fromDate, toDate)

	// Where the period's hours actually went. The bar is each org's share of
	// the total, in that org's colour — the same key the calendar and the
	// pending tiles use, so one colour means one employer everywhere.
	//
	// Counted from the unfiltered period, and tapping a row filters the tab.
	// This is the report's colour key, so it has to keep showing the org that
	// was switched off, at the size it really is: shares recomputed against
	// whatever is left would read 100% for one org and call it the whole month.
	byOrg := minutesByOrg(all)
	allMin := 0
	for _, m := range byOrg {
		allMin += m
	}
	split := []fyne.CanvasObject{bold("By organisation — tap one to show only the others")}
	for _, org := range orgsByShare(ui.cfg, byOrg) {
		org, mins := org, byOrg[org]
		pct := 0
		if allMin > 0 {
			pct = int(float64(mins) / float64(allMin) * 100)
		}
		// A switched-off row keeps its size and its place and goes grey, dot and
		// bar together — the same washed-out colour the key uses, so one look
		// says which employer is currently out of every figure above.
		var fill color.Color = orgColor(ui.cfg, org)
		nameColor := theme.ColorNameForeground
		amount := fmt.Sprintf("%s · %d%%", hoursMins(mins), pct)
		if !ui.orgShown(org) {
			fill = dimmed(fill)
			nameColor = theme.ColorNamePlaceHolder
			amount = "hidden"
		}
		row := container.New(newRatioRow(0.04, 0.22, 0.54, 0.20),
			orgDotIn(fill),
			container.NewCenter(colorLabel(orDefault(org, "unknown org"), nameColor)),
			shareBar(fill, mins, allMin, 12),
			widget.NewLabelWithStyle(amount, fyne.TextAlignTrailing,
				fyne.TextStyle{Monospace: true}))
		split = append(split, newTappable(row, func() { ui.toggleOrg(org) }))
	}
	if len(byOrg) == 0 {
		split = append(split, widget.NewLabel("Nothing logged in this period."))
	}

	var detail []fyne.CanvasObject
	if len(rep.Incomplete) > 0 {
		detail = append(detail, bold("Days under 480"))
		hdr := container.NewGridWithColumns(3, bold("Date"), bold("Logged"), bold("Missing"))
		detail = append(detail, hdr)
		for _, x := range rep.Incomplete {
			detail = append(detail, container.NewGridWithColumns(3,
				widget.NewLabel(x.Date), widget.NewLabel(strconv.Itoa(x.Minutes)),
				widget.NewLabel(strconv.Itoa(target-x.Minutes))))
		}
	}
	if len(rep.Over) > 0 {
		detail = append(detail, bold("Days over 480"))
		hdr := container.NewGridWithColumns(3, bold("Date"), bold("Logged"), bold("Excess"))
		detail = append(detail, hdr)
		for _, x := range rep.Over {
			detail = append(detail, container.NewGridWithColumns(3,
				widget.NewLabel(x.Date), widget.NewLabel(strconv.Itoa(x.Minutes)),
				widget.NewLabel("+"+strconv.Itoa(x.Minutes-target))))
		}
	}
	// With no salary set every figure on the tab is a real zero, which reads as
	// a month that earned nothing rather than as a setting nobody has filled in.
	summary := "Set your base salary in Settings to see what this period is worth."
	if rep.BaseSalary != 0 {
		summary = fmt.Sprintf("%s over 21 working days → %s per full day.",
			money(rep.BaseSalary), money(rep.DailyRate))
	}
	if supportBonus > 0 {
		summary += fmt.Sprintf(" Worklog receivable %s + weekend support %s = %s.",
			money(rep.Receivable), money(supportBonus), money(totalReceivable))
	}
	detail = append(detail, widget.NewLabel(summary))

	ui.repBox.Objects = []fyne.CanvasObject{
		stats,
		container.NewVBox(chart),
		container.NewVBox(split...),
		container.NewVBox(detail...),
	}
	ui.repBox.Refresh()
	relayout(ui.repBody)
}

func (ui *UI) exportCSV() {
	// Export the project worklog items shown in this payroll period. The cache
	// behind it reaches months further back — that run-up is only there for the
	// weekend support carry, and exporting it would put three extra months into
	// a file named after one period.
	fromDate, toDate, _ := reportBounds(ui.repMonth)
	fetched, _ := ui.itemsFor(reportCacheKey(ui.repMonth))
	var items []WorklogItem
	for _, it := range fetched {
		if it.Date >= fromDate && it.Date <= toDate {
			items = append(items, it)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Date < items[j].Date })
	out := filepath.Join(dataDir(), fmt.Sprintf("worklog-%s-to-%s.csv", fromDate, toDate))
	f, err := os.Create(out)
	if err != nil {
		ui.errf(err)
		return
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"date", "minutes", "owner", "title", "remarks", "url"})
	for _, it := range items {
		_ = w.Write([]string{it.Date, strconv.Itoa(it.Minutes), it.Owner, it.Title, it.Remarks, it.URL})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		ui.errf(err)
		return
	}
	dialog.ShowInformation("Exported", fmt.Sprintf("Saved %d items to:\n%s", len(items), out), ui.win)
}

// ============================ Settings ============================

func (ui *UI) buildSettingsTab() fyne.CanvasObject {
	user := widget.NewEntry()
	user.SetPlaceHolder("auto-detected from gh — leave blank")
	owner := widget.NewEntry()
	owner.SetPlaceHolder("blg-elden")
	repos := widget.NewEntry()
	repos.SetPlaceHolder("bigledger  (whole org)  or  bigledger/blg-intranet")
	sal := widget.NewEntry()
	cur := widget.NewEntry()
	cur.SetPlaceHolder("RM")
	disp := widget.NewEntry()
	disp.SetPlaceHolder("USD")
	back := widget.NewEntry()
	mode := widget.NewSelect([]string{"Create Worklog sub-issue", "Write on the issue itself"}, nil)
	key := widget.NewPasswordEntry()
	key.SetPlaceHolder("sk-ant-...")
	// One board per org, one per line. Status and Report read the union of
	// them, so work logged against a second org's project still counts.
	projURL := widget.NewMultiLineEntry()
	projURL.SetMinRowsVisible(3)
	projURL.SetPlaceHolder("https://github.com/orgs/bigledger/projects/9")
	msg := widget.NewLabel("")

	// preload
	c := ui.cfg
	user.SetText(c.GithubAuthor)
	if strings.TrimSpace(c.GithubAuthor) == "" {
		// show who gh is logged in as, so the field can stay blank
		go func() {
			if u, err := ghCurrentUser(); err == nil {
				fyne.Do(func() {
					user.SetPlaceHolder("gh user: " + u + " — leave blank to use it")
					user.Refresh()
				})
			}
		}()
	}
	owner.SetText(c.WorklogOwner)
	repos.SetText(strings.Join(c.Repos, ", "))
	// Left blank when unset rather than pre-filled with a number: a figure
	// already in the box is a figure that gets saved without being read, and
	// nobody else's salary is a sensible starting guess for yours.
	if c.BaseSalary != 0 {
		sal.SetText(strconv.FormatFloat(c.BaseSalary, 'f', -1, 64))
	}
	cur.SetText(orDefault(c.Currency, "RM"))
	disp.SetText(orDefault(c.DisplayCurrency, "USD"))
	if c.LookbackDays > 0 {
		back.SetText(strconv.Itoa(c.LookbackDays))
	} else {
		back.SetText("7")
	}
	if c.DefaultMode == "issue" {
		mode.SetSelected("Write on the issue itself")
	} else {
		mode.SetSelected("Create Worklog sub-issue")
	}
	key.SetText(c.AnthropicAPIKey)
	projURL.SetText(strings.Join(projectURLs(c), "\n"))
	if !ui.hasCfg {
		msg.SetText("Fill this in once to get started.")
	}

	save := widget.NewButton("Save settings", func() {
		salF, _ := strconv.ParseFloat(strings.TrimSpace(sal.Text), 64)
		backN, _ := strconv.Atoi(strings.TrimSpace(back.Text))
		if backN < 1 {
			backN = 7
		}
		m := "subissue"
		if mode.Selected == "Write on the issue itself" {
			m = "issue"
		}
		var repoList []string
		for _, x := range strings.Split(repos.Text, ",") {
			if s := strings.TrimSpace(x); s != "" {
				repoList = append(repoList, s)
			}
		}
		newCfg := ui.cfg
		newCfg.GithubAuthor = strings.TrimSpace(user.Text)
		newCfg.WorklogOwner = strings.TrimSpace(owner.Text)
		newCfg.Repos = repoList
		newCfg.BaseSalary = salF
		newCfg.Currency = orDefault(strings.TrimSpace(cur.Text), "RM")
		newCfg.DisplayCurrency = strings.ToUpper(strings.TrimSpace(disp.Text))
		newCfg.LookbackDays = backN
		newCfg.DefaultMode = m
		newCfg.AnthropicAPIKey = strings.TrimSpace(key.Text)
		// First line stays the main board so an older build of this app, and
		// anything reading project_url, still finds the one it expects.
		var projList []string
		for _, line := range strings.FieldsFunc(projURL.Text, func(r rune) bool {
			return r == '\n' || r == '\r' || r == ',' || r == ' '
		}) {
			if line = strings.TrimSpace(line); line != "" {
				projList = append(projList, line)
			}
		}
		newCfg.ProjectURL, newCfg.ExtraProjectURLs = "", nil
		if len(projList) > 0 {
			newCfg.ProjectURL, newCfg.ExtraProjectURLs = projList[0], projList[1:]
		}
		if newCfg.AnthropicModel == "" {
			newCfg.AnthropicModel = "claude-sonnet-4-6"
		}
		if err := ui.store.SaveConfig(newCfg); err != nil {
			ui.errf(err)
			return
		}
		ui.cfg = newCfg
		ui.hasCfg = true
		ui.projItems = nil // reload against the saved project and owner
		ui.ensureProjectCache()
		msg.SetText("Saved.")
	})
	save.Importance = widget.HighImportance

	rate := widget.NewButton("Fetch exchange rate", func() {
		if !ui.hasCfg {
			msg.SetText("Save settings first.")
			return
		}
		msg.SetText("Fetching rate…")
		base := strings.ToUpper(strings.TrimSpace(ui.cfg.Currency))
		if base == "RM" {
			base = "MYR"
		}
		disp := orDefault(ui.cfg.DisplayCurrency, "USD")
		var r float64
		ui.async(func() error {
			v, err := fetchRate(base, disp)
			r = v
			return err
		}, func() {
			ui.cfg.FxRate = r
			ui.cfg.FxUpdated = today()
			_ = ui.store.SaveConfig(ui.cfg)
			msg.SetText(fmt.Sprintf("1 %s = %g %s (%s)", ui.cfg.Currency, r, disp, ui.cfg.FxUpdated))
		})
	})

	form := container.NewVBox(
		widget.NewLabelWithStyle("Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewGridWithColumns(2, labeled("GitHub username (optional — auto from gh)", user), labeled("Worklog owner", owner)),
		labeled("Repos or orgs to scan (comma-separated; a bare org name scans all its repos)", repos),
		labeled("Worklog project URLs — one per line (Status + Report read all of them, filtered to the owner above)", projURL),
		container.NewGridWithColumns(4,
			labeled("Base salary per 21 days", sal), labeled("Currency", cur),
			labeled("Also show in", disp), labeled("Look back (days)", back)),
		container.NewGridWithColumns(2,
			labeled("Default push mode", wideSelect(mode)),
			labeled("Anthropic API key (optional)", key)),
		container.NewHBox(save, rate),
		msg,
	)
	return container.NewVScroll(widget.NewCard("", "", form))
}

// ============================ helpers ============================

func bold(s string) fyne.CanvasObject {
	return widget.NewLabelWithStyle(s, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
}
func colorLabel(s string, name fyne.ThemeColorName) fyne.CanvasObject {
	l := canvas.NewText(s, theme.Color(name))
	l.TextSize = theme.TextSize()
	return l
}
// wideSelect holds a dropdown open wide enough for its longest option.
//
// fyne sizes a Select from its *placeholder*, not from what it displays:
// selectRenderer.MinSize overwrites the label width with the placeholder's, so
// the default "(Select one)" decides the box and any longer option spills out
// under the arrow. Measure the options and pin the width to the widest.
func wideSelect(sel *widget.Select) fyne.CanvasObject {
	w := float32(0)
	for _, o := range sel.Options {
		if m := fyne.MeasureText(o, theme.TextSize(), fyne.TextStyle{}); m.Width > w {
			w = m.Width
		}
	}
	// The same allowance the renderer adds around its label: inner padding on
	// both sides plus room for the drop-down arrow.
	w += theme.InnerPadding()*4 + theme.IconInlineSize()
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(w, 0))
	return container.NewStack(spacer, sel)
}

func statTile(value, label string) fyne.CanvasObject {
	return widget.NewCard("", "", container.NewVBox(
		widget.NewLabelWithStyle(value, fyne.TextAlignLeading, fyne.TextStyle{Bold: true, Monospace: true}),
		widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{}),
	))
}
// truncate elides to n characters. It counts runes, not bytes: cutting an issue
// title mid-codepoint would render the tail as a replacement glyph.
func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimRight(string(r[:n-1]), " ") + "…"
}
func orDash(s string) string {
	if s == "" {
		return "other"
	}
	return s
}
func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}
func openURL(raw string) {
	if u, err := url.Parse(raw); err == nil {
		_ = fyne.CurrentApp().OpenURL(u)
	}
}
