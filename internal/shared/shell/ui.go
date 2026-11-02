package shell

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
)

// ---- small tappable card, used for calendar cells ----

type Tappable struct {
	widget.BaseWidget
	Content  fyne.CanvasObject
	onTap    func()
	Feedback *cardFeedback
}

func NewTappable(content fyne.CanvasObject, onTap func()) *Tappable {
	t := &Tappable{Content: content, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *Tappable) CreateRenderer() fyne.WidgetRenderer {
	if t.Feedback != nil {
		return &feedbackRenderer{WidgetRenderer: widget.NewSimpleRenderer(t.Content), feedback: t.Feedback}
	}
	return widget.NewSimpleRenderer(t.Content)
}

func (t *Tappable) Tapped(_ *fyne.PointEvent) {
	if t.Feedback != nil {
		t.Feedback.press()
	}
	if t.onTap != nil {
		t.onTap()
	}
}

// ---- app state ----

type UI struct {
	LogSearch string
	Kanban    *KanbanUI
	Store     *backend.Store
	Cfg       backend.Config
	HasCfg    bool
	Win       fyne.Window

	CalMonth string // YYYY-MM
	RepMonth string
	SelDay   string

	// containers refreshed on demand
	PendingBox *fyne.Container
	RecentBox  *fyne.Container
	WeekBox    *fyne.Container // the week a saved entry can be dropped onto
	CalBox     *fyne.Container
	CalLegend  *fyne.Container
	LogLegend  *fyne.Container // the same key, at the foot of the Log work tab
	DayPanel   *fyne.Container
	DaySide    *fyne.Container // the panel's shell: fixed width
	DaySwap    *fyne.Container // holds either the day's detail or the prompt
	RepBox     *fyne.Container
	ListBox    *fyne.Container // the Log List tab: boards, the week by issue, what is pending
	CommitBox  *fyne.Container // the Commit List tab: this week's commits by day and parent issue
	Tabs       *pillTabs

	// The column each tab's panels stand in. Held so a panel that changed height
	// can have its column re-measure it — see relayout.
	LogBody    *fyne.Container
	CalBody    *fyne.Container
	RepBody    *fyne.Container
	ListBody   *fyne.Container
	CommitBody *fyne.Container

	// The bottom list is a to-do, not a history: it shows what is still waiting
	// to reach GitHub. This ticks over to the full month when the pushed ones
	// are wanted back.
	ShowPushed *widget.Check
	DraftSort  string // Duration or Created (earliest first)

	CalTitle    *widget.Label
	CalSummary  *widget.Label
	RepTitle    *widget.Label
	ListTitle   *widget.Label
	ListHere    *widget.Button // "This week" on Log List, greyed when already there
	ListSupport *widget.Check  // mirror of the Log Work support checkbox, kept in sync

	CommitTitle *widget.Label
	CommitHere  *widget.Button // "This week" on Commit List

	// Commits per week, keyed by weekStart (YYYY-MM-DD Monday). Fetched on demand
	// when the tab is opened or the week is walked. Loading and error state are
	// held beside the results so the UI can show "fetching…" or the last error
	// without leaving the tab.
	WeekCommits    map[string][]backend.Commit
	WeekCommitErrs map[string][]string
	WeekCommitLoad map[string]bool
	WeekCommitAt   map[string]time.Time

	// Meeting tab: same commit fetch as Commit List, but on a Friday-through-
	// Thursday window (the weekly update on Thursday). Kept separate from the
	// weekCommits maps so walking one tab does not throw off the other.
	MeetingBox        *fyne.Container
	MeetingBody       *fyne.Container
	MeetingTitle      *widget.Label
	MeetingHere       *widget.Button
	MeetingStart      string // Thursday, YYYY-MM-DD
	MeetingCommits    map[string][]backend.Commit
	MeetingCommitErrs map[string][]string
	MeetingCommitLoad map[string]bool
	MeetingCommitAt   map[string]time.Time
	MeetingProgress   *loadingIndicator
	// Stable holders for the Meeting calendar (90%) and logged rows (10%).
	// Their fixed layout has no arrow or draggable divider.
	MeetingCalendarHolder *fyne.Container
	MeetingRowsHolder     *fyne.Container

	// Shas of commits already stitched into a saved worklog row. Refreshed
	// before each Commit List / Meeting redraw so a fresh save shows up as
	// a "saved" tag on the bubble the next time the calendar renders.
	loggedShas map[string]bool

	// One rate fetch in flight at a time: opening the Report tab twice in a row
	// should not queue two.
	RateLoading bool

	// Status + Report read from GitHub one server-filtered month at a time.
	// projStale marks a range a push has invalidated while its fetch was still
	// in the air, so the answer that lands knowingly out of date is thrown away.
	ProjItems   map[string][]backend.WorklogItem
	ProjLoaded  map[string]bool
	ProjLoading map[string]bool
	ProjErr     map[string]error
	ProjStale   map[string]bool

	// The week on show above the saved entries, its columns kept so a drop
	// target can be lit, and the card currently in the air over them.
	WeekStart string // Monday, YYYY-MM-DD
	WeekCols  map[string]*DayColumn
	HoverDay  string
	DragRow   backend.Row
	DragLayer *fyne.Container // above the tab, so the carried card is never buried
	DragGhost *fyne.Container

	// Pending commits, held so a bubble can be re-rendered without refetching,
	// plus the issue titles their refs resolve to.
	Pending            backend.PendingResult
	PendingLoaded      bool
	PendingLoading     bool
	PendingAt          time.Time // when the list last landed, for staleness
	IssueInfo          map[string]backend.IssueInfo
	savedTitlesLoading bool
	ShowIgnored        bool

	// supportWeeks are the weeks (by Monday) the weekday support placeholder is
	// ticked for. Saved in support-weeks.json, pruned once a week's Friday has
	// passed or its weekdays all hold logged support. Never pushed anywhere.
	SupportWeeks map[string]bool
	// WeekSupport is Log Work's support checkbox, kept across redraws, and
	// WeekSupportView the gold wrapper it is shown in.
	WeekSupport     *widget.Check
	WeekSupportView fyne.CanvasObject

	// Organisations tapped out of the colour key, lowercased. One filter for the
	// whole app rather than one per tab: hiding an employer is a decision about
	// what you are looking at, and it would have to be made three times over if
	// switching tabs quietly brought the other one back.
	OrgOff                                                      map[string]bool
	Meters                                                      map[string]*meterMotion
	CommitProgress                                              *loadingIndicator
	ReportProgress                                              *loadingIndicator
	LogProgress, ListProgress, StatusProgress, SettingsProgress *loadingIndicator
	WeekStripProgress                                           *loadingIndicator
	fetchJobs                                                   map[*fetchJob]bool
	ProfileBox                                                  *fyne.Container
	ProfileLoaded, ProfileLoading                               bool
}

// Relayout re-measures the column a panel stands in, after the panel's contents
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
func Relayout(column *fyne.Container) {
	if column != nil {
		column.Refresh()
	}
}

func (ui *UI) ShowDraftSavedInfo() {
	if ui.Win == nil {
		return
	}
	dialog.ShowInformation("Draft saved locally",
		"This worklog is saved on this computer and has not been pushed to GitHub yet.", ui.Win)
}

// OrgShown reports whether an organisation's work belongs in the current view.
func (ui *UI) OrgShown(org string) bool {
	return !ui.OrgOff[strings.ToLower(strings.TrimSpace(org))]
}

// ToggleOrg flips one organisation in the key and redraws everything that
// carries a colour, so the filter lands on every tab at once rather than only
// on the one that was clicked.
func (ui *UI) ToggleOrg(org string) {
	org = strings.ToLower(strings.TrimSpace(org))
	if ui.OrgOff == nil {
		ui.OrgOff = map[string]bool{}
	}
	if ui.OrgOff[org] {
		delete(ui.OrgOff, org)
	} else {
		ui.OrgOff[org] = true
	}
	Hooks.DrawCalendar(ui)
	Hooks.DrawReport(ui)
	// The saved list, the week strip and the key itself all come off this.
	Hooks.DrawRecent(ui)
	// Only once there is a list: rendering an empty result would replace the
	// "loading" line with "no unlogged commits", which is a different claim.
	if ui.PendingLoaded {
		Hooks.RenderPending(ui)
	}
	Hooks.DrawLogLegend(ui)
}

// ShownItems drops the worklog items belonging to a filtered-out organisation.
func (ui *UI) ShownItems(items []backend.WorklogItem) []backend.WorklogItem {
	if len(ui.OrgOff) == 0 {
		return items
	}
	out := make([]backend.WorklogItem, 0, len(items))
	for _, it := range items {
		if ui.OrgShown(ItemOrg(it)) {
			out = append(out, it)
		}
	}
	return out
}

// RowOrg is the organisation a saved entry belongs to, read off the issue it is
// filed against. An entry with no issue yet has no org, and is only ever hidden
// by switching off the key's "unknown org".
func RowOrg(r backend.Row) string { return strings.ToLower(OrgOf(r["issue"])) }

// ShownRows drops the saved entries belonging to a filtered-out organisation.
func (ui *UI) ShownRows(rows []backend.Row) []backend.Row {
	if len(ui.OrgOff) == 0 {
		return rows
	}
	out := make([]backend.Row, 0, len(rows))
	for _, r := range rows {
		if ui.OrgShown(RowOrg(r)) {
			out = append(out, r)
		}
	}
	return out
}

// DraftMinutesByDay totals what is saved on this machine and not yet pushed,
// per day, across the dates from..to inclusive.
//
// Status and Report both read the GitHub project, so until an entry is pushed it
// is worked time that shows up nowhere: the day reads empty on the calendar and
// the bar is missing from the chart, which is indistinguishable from not having
// done the work. This is what those two tabs paint yellow — shown, but never
// counted into the banked figures, since nothing has been sent yet.
//
// Filtered by org like everything else on those tabs, so switching an employer
// off takes its drafts with it.
func (ui *UI) DraftMinutesByDay(fromDate, toDate string) map[string]int {
	out := map[string]int{}
	rows, err := ui.Store.ReadRows()
	if err != nil {
		return out
	}
	for _, r := range ui.ShownRows(rows) {
		if r["pushed_at"] != "" {
			continue
		}
		d := r["date"]
		if d < fromDate || d > toDate {
			continue
		}
		out[d] += r.Minutes()
	}
	return out
}

// DraftMinutesOn sums the unpushed drafts standing on one day, skipping a row
// by id when one is given — the row the editor is looking at. Without the skip
// its old value would be counted as pending AND the field would be counted as
// new, so a 300 min draft opened at 300 would read as 600 waiting.
func (ui *UI) DraftMinutesOn(date, excludeRowID string) int {
	sum := 0
	rows, err := ui.Store.ReadRows()
	if err != nil {
		return 0
	}
	for _, r := range ui.ShownRows(rows) {
		if r["pushed_at"] != "" || r["date"] != date {
			continue
		}
		if excludeRowID != "" && r["id"] == excludeRowID {
			continue
		}
		sum += r.Minutes()
	}
	return sum
}

// ItemOrg is the organisation a worklog item belongs to: its own issue's owner,
// or the parent issue's when the item is a sub-issue with no URL of its own.
func ItemOrg(it backend.WorklogItem) string {
	org := strings.ToLower(OrgOf(it.URL))
	if org == "" {
		org = strings.ToLower(OrgOf(it.ParentURL))
	}
	return org
}

// PendingFresh is how long a fetched list is trusted. The app is left open for
// days, so a list held for the whole session showed a morning's commits all
// afternoon and made every commit since look like it had gone missing.
const PendingFresh = 10 * time.Minute

// PendingIsStale reports whether a list fetched at that moment is old enough to
// be worth asking GitHub again.
func PendingIsStale(at time.Time) bool { return time.Since(at) >= PendingFresh }

// LoadPending fetches unlogged commits, reusing the last answer while it is
// still fresh unless forced. The Log tab is the default tab, so this also runs
// at startup rather than waiting for a tab change that never fires.
func (ui *UI) LoadPending(force bool) {
	if ui.PendingBox == nil || ui.PendingLoading {
		return
	}
	if ui.PendingLoaded && !force && !PendingIsStale(ui.PendingAt) {
		return
	}
	if len(ui.Cfg.Repos) == 0 {
		ui.PendingBox.Objects = []fyne.CanvasObject{
			widget.NewLabel("Add a repo or org to scan in Settings (e.g. exampleorg)."),
		}
		ui.PendingBox.Refresh()
		Relayout(ui.LogBody)
		return
	}
	if force {
		// Drop titles that never resolved so a manual refresh retries them;
		// successful ones are kept, since issue titles rarely change.
		for ref, info := range ui.IssueInfo {
			if info.Title == "" {
				delete(ui.IssueInfo, ref)
			}
		}
	}
	ui.PendingLoading = true
	progress, finish := ui.BeginFetch("pending", "Discovering repositories and branches")
	// A stale list is left on screen while the new one is fetched; only a first
	// load or a button press has nothing worth looking at behind the wait.
	if force || !ui.PendingLoaded {
		ui.PendingBox.Objects = []fyne.CanvasObject{widget.NewLabel("Fetching from GitHub…")}
		ui.PendingBox.Refresh()
		Relayout(ui.LogBody)
	}

	// One combined bar for commits + issue titles. Weights are wall-clock
	// approximations: the commit sweep dominates, so it takes 80 of 100 slots
	// and the title lookup fills the last 20. That way the bar climbs to 100%
	// exactly once, at the end of the title lookup — never overshoots halfway
	// then falls back.
	composer := NewProgressComposer(progress)
	const pendingWeight, titlesWeight = 80.0, 20.0

	var res backend.PendingResult
	var ferr error
	ui.Async(func() error {
		res, ferr = ui.Store.FetchPending(ui.Cfg, composer.Phase(pendingWeight))
		return nil
	}, func() {
		ui.PendingLoading = false
		composer.Advance(pendingWeight)
		if ferr != nil {
			finish()
			ui.PendingBox.Objects = []fyne.CanvasObject{ColorLabel(ferr.Error(), theme.ColorNameError)}
			ui.PendingBox.Refresh()
			Relayout(ui.LogBody)
			return
		}
		ui.PendingLoaded = true
		ui.PendingAt = time.Now()
		ui.Pending = res
		Hooks.RenderPending(ui)
		ui.loadIssueInfos(composer, titlesWeight, finish)
	})
}

// LoadWeekCommits fetches every commit the author authored during the shown
// week (weekStart..weekStart+6) and hands drawCommitList the result. Each week
// is cached so walking back to a week already fetched is instant; force skips
// the cache. Refetches whenever the tab is opened on a stale entry.
func (ui *UI) LoadWeekCommits(force bool) {
	if ui.CommitBox == nil {
		return
	}
	ui.EnsureWeekCommitCache()
	weekStart := backend.OrDefault(ui.WeekStart, backend.WeekStartOf(Today()))
	days := WeekDates(weekStart)
	from, to := days[0], days[len(days)-1]

	if ui.WeekCommitLoad[weekStart] {
		return
	}
	if !force {
		if _, cached := ui.WeekCommits[weekStart]; cached {
			if !PendingIsStale(ui.WeekCommitAt[weekStart]) {
				return
			}
		}
	}
	if len(ui.Cfg.Repos) == 0 {
		return // buildCommitListTab shows the same nudge Log Work does
	}
	ui.WeekCommitLoad[weekStart] = true
	progress, finish := ui.BeginFetch("commits:"+weekStart, "Discovering repositories and branches")
	Hooks.DrawCommitList(ui) // repaint so the header can say "fetching…"

	composer := NewProgressComposer(progress)
	const commitsWeight, titlesWeight = 80.0, 20.0

	var (
		commits []backend.Commit
		errs    []string
		ferr    error
	)
	ui.Async(func() error {
		commits, errs, ferr = ui.Store.FetchWeekCommits(ui.Cfg, from, to, composer.Phase(commitsWeight))
		return nil
	}, func() {
		ui.WeekCommitLoad[weekStart] = false
		composer.Advance(commitsWeight)
		if ferr != nil {
			finish()
			ui.WeekCommitErrs[weekStart] = []string{ferr.Error()}
			Hooks.DrawCommitList(ui)
			return
		}
		ui.WeekCommits[weekStart] = commits
		ui.WeekCommitErrs[weekStart] = errs
		ui.WeekCommitAt[weekStart] = time.Now()
		// Seed issue titles for the commits' parent issues so bubbles show a
		// title rather than a bare owner/repo#123. Reuses loadPending's cache
		// and rides on the same progress job so the strip carries through.
		ui.loadCommitIssueInfos(commits, weekStart, composer, titlesWeight, finish)
		Hooks.DrawCommitList(ui)
	})
}

func (ui *UI) EnsureWeekCommitCache() {
	if ui.WeekCommits == nil {
		ui.WeekCommits = map[string][]backend.Commit{}
		ui.WeekCommitErrs = map[string][]string{}
		ui.WeekCommitLoad = map[string]bool{}
		ui.WeekCommitAt = map[string]time.Time{}
	}
}

// loadCommitIssueInfos resolves the parent-issue titles behind the fetched
// commits, so a bubble labelled "exampleorg/sample-intranet#42" gains its title.
// Piggybacks the same issueInfo cache pendings use, so an issue seen on both
// tabs only fetches once. Rides on the caller's composer as its final phase.
func (ui *UI) loadCommitIssueInfos(commits []backend.Commit, week string, composer *ProgressComposer, weight float64, finish func()) {
	if ui.IssueInfo == nil {
		ui.IssueInfo = map[string]backend.IssueInfo{}
	}
	seen := map[string]bool{}
	var refs []string
	for _, c := range commits {
		if c.Issue == "" || seen[c.Issue] {
			continue
		}
		seen[c.Issue] = true
		if _, done := ui.IssueInfo[c.Issue]; !done {
			refs = append(refs, c.Issue)
		}
	}
	if len(refs) == 0 {
		composer.Advance(weight)
		finish()
		return
	}
	var infos map[string]backend.IssueInfo
	ui.Async(func() error {
		infos, _ = backend.FetchIssueInfos(refs, composer.Phase(weight))
		return nil
	}, func() {
		composer.Advance(weight)
		defer finish()
		for ref, info := range infos {
			ui.IssueInfo[ref] = info
		}
		Hooks.DrawCommitList(ui)
	})
}

// loadIssueInfos fills in the issue titles behind the pending refs, then
// re-renders. Bubbles appear immediately and gain their title a moment later.
//
// Runs inside the pending fetch's own progress job as a final phase in the
// caller's composer — the bar reads as one continuous 0-to-100 climb rather
// than restarting at zero for the title lookup.
func (ui *UI) loadIssueInfos(composer *ProgressComposer, weight float64, finish func()) {
	if ui.IssueInfo == nil {
		ui.IssueInfo = map[string]backend.IssueInfo{}
	}
	var refs []string
	for _, g := range ui.Pending.Groups {
		if g.Issue == "" {
			continue
		}
		if _, done := ui.IssueInfo[g.Issue]; !done {
			refs = append(refs, g.Issue)
		}
	}
	if len(refs) == 0 {
		composer.Advance(weight)
		finish()
		return
	}
	var infos map[string]backend.IssueInfo
	ui.Async(func() error {
		infos, _ = backend.FetchIssueInfos(refs, composer.Phase(weight))
		return nil
	}, func() {
		composer.Advance(weight)
		defer finish()
		for ref, info := range infos {
			ui.IssueInfo[ref] = info
		}
		Hooks.RenderPending(ui)
	})
}

// LoadSavedIssueInfos migrates the durable rows that predate issue_title. The
// saved list used to borrow titles only from the pending-commit fetch, so a
// code-review draft could resolve its parent issue correctly and still show a
// bare owner/repo#number. Persisting the title makes that row self-contained on
// the next launch and avoids repeating the GitHub request forever.
func (ui *UI) LoadSavedIssueInfos() {
	if ui.savedTitlesLoading {
		return
	}
	ui.savedTitlesLoading = true
	var infos map[string]backend.IssueInfo
	var changed int
	var loadErr error
	ui.Async(func() error {
		rows, err := ui.Store.ReadRows()
		if err != nil {
			loadErr = err
			return nil
		}
		seen := map[string]bool{}
		var refs []string
		for _, r := range rows {
			ref := strings.TrimSpace(r["issue"])
			if ref == "" || strings.TrimSpace(r["issue_title"]) != "" {
				continue
			}
			key := strings.ToLower(ref)
			if seen[key] {
				continue
			}
			if _, _, _, err := backend.SplitIssue(ref); err != nil {
				continue
			}
			seen[key] = true
			refs = append(refs, ref)
		}
		if len(refs) == 0 {
			infos = map[string]backend.IssueInfo{}
			return nil
		}
		infos, _ = backend.FetchIssueInfos(refs)
		titles := map[string]string{}
		for ref, info := range infos {
			if strings.TrimSpace(info.Title) != "" {
				titles[ref] = info.Title
			}
		}
		changed, loadErr = ui.Store.UpdateIssueTitles(titles)
		return nil
	}, func() {
		ui.savedTitlesLoading = false
		if loadErr != nil {
			ui.Errf(loadErr)
			return
		}
		if ui.IssueInfo == nil {
			ui.IssueInfo = map[string]backend.IssueInfo{}
		}
		for ref, info := range infos {
			if strings.TrimSpace(info.Title) != "" {
				ui.IssueInfo[ref] = info
			}
		}
		if changed > 0 {
			Hooks.DrawRecent(ui)
		}
	})
}

// RefreshLoggedShas rereads the local rows' saved commit shas so the calendar
// can mark rows whose issue still carries other unlogged commits. A cheap file
// read; called on each Log Work redraw.
func (ui *UI) RefreshLoggedShas() {
	set, err := ui.Store.LoggedShas()
	if err != nil || set == nil {
		set = map[string]bool{}
	}
	ui.loggedShas = set
}

// IssueHasPending reports whether the given issue still has non-ignored
// commits waiting on the pending list. This is separate from a partially
// completed push: it means more source commits remain, not that saving failed.
func (ui *UI) IssueHasPending(issue string) bool {
	issue = strings.TrimSpace(issue)
	if issue == "" {
		return false
	}
	for _, g := range ui.Pending.Groups {
		if g.Issue == issue && !g.Ignored && len(g.Commits) > 0 {
			return true
		}
	}
	return false
}

// RowPartlySaved reports that a push reached GitHub far enough to leave an
// issue/project item behind, but did not complete every required field/status
// update. Daily issues created by the app keep their issue ref on the row even
// if the later board write failed before issue_url or item_id was recorded.
func RowPartlySaved(r backend.Row) bool {
	return strings.TrimSpace(r["pushed_at"]) == "" &&
		(strings.TrimSpace(r["issue_url"]) != "" || strings.TrimSpace(r["item_id"]) != "" ||
			(backend.RowCreatesOwnIssue(r) && strings.TrimSpace(r["issue"]) != ""))
}

func (ui *UI) EnsureProjectCache() {
	if ui.ProjItems == nil {
		ui.ProjItems = map[string][]backend.WorklogItem{}
		ui.ProjLoaded = map[string]bool{}
		ui.ProjLoading = map[string]bool{}
		ui.ProjErr = map[string]error{}
		ui.ProjStale = map[string]bool{}
	}
	if ui.ProjStale == nil {
		ui.ProjStale = map[string]bool{}
	}
}

const statusKeyPrefix = "status:"

const reportKeyPrefix = "report:"

func StatusCacheKey(month string) string { return statusKeyPrefix + month }

func ReportCacheKey(month string) string { return reportKeyPrefix + month }

// CacheBounds is the span of dates a cached range holds, read back out of its
// key. A push only makes the ranges covering its own date wrong; this is how
// they are told apart from the ones it cannot have touched.
func CacheBounds(key string) (string, string, bool) {
	if month, ok := strings.CutPrefix(key, statusKeyPrefix); ok {
		from, to, err := backend.MonthBounds(month)
		return from, to, err == nil
	}
	if month, ok := strings.CutPrefix(key, reportKeyPrefix); ok {
		// The fetched range, not the period: a push into the run-up changes the
		// support carry, so this cache really is wrong when one lands there.
		from, to, err := backend.ReportFetchBounds(month)
		return from, to, err == nil
	}
	return "", "", false
}

func (ui *UI) ItemsFor(key string) ([]backend.WorklogItem, bool) {
	ui.EnsureProjectCache()
	if !ui.ProjLoaded[key] {
		return nil, false
	}
	return ui.ProjItems[key], true
}

// LoadProject fetches only the requested date range and caches it by screen.
func (ui *UI) LoadProject(key, fromDate, toDate string, force bool) {
	ui.EnsureProjectCache()
	if ui.ProjLoading[key] {
		return
	}
	if ui.ProjLoaded[key] && !force {
		return
	}
	// Nothing to fetch from, so nothing is sent to fetch it: a goroutine here
	// could only come back with this same sentence, and it came back onto the
	// widgets while the caller was still drawing them.
	if len(backend.ProjectURLs(ui.Cfg)) == 0 {
		ui.ProjErr[key] = backend.GhErr(
			"Set the Worklog project URL in Settings to load Status and Report from GitHub.")
		Hooks.DrawCalendar(ui)
		Hooks.DrawReport(ui)
		return
	}
	ui.ProjLoading[key] = true
	ui.ProjErr[key] = nil
	progress, finish := ui.BeginFetch(key, "Loading project boards")
	go func() {
		defer ui.recoverToDialog("load project")
		items, err := backend.FetchProjectWorklogs(ui.Cfg, ui.Cfg.WorklogOwner, fromDate, toDate, progress)
		fyne.Do(func() {
			defer finish()
			ui.ProjLoading[key] = false
			ui.ProjErr[key] = err
			if err == nil {
				ui.ProjItems[key] = items
				ui.ProjLoaded[key] = true
			}
			Hooks.DrawCalendar(ui)
			Hooks.DrawReport(ui)
			// The Log tab scores today against this same data, so it has to be
			// redrawn when a month lands or it stays stuck on "checking…".
			Hooks.DrawRecent(ui)
			// A push landed while this fetch was in the air, so what came back is
			// already known to be one worklog short. It is drawn — it is still the
			// best answer on hand — and then asked for again.
			if ui.ProjStale[key] {
				delete(ui.ProjStale, key)
				delete(ui.ProjLoaded, key)
				ui.LoadProject(key, fromDate, toDate, true)
			}
		})
	}()
}

func (ui *UI) LoadStatus(force bool) {
	ui.EnsureProjectCache()
	from, to, err := backend.MonthBounds(ui.CalMonth)
	if err != nil {
		ui.ProjErr[StatusCacheKey(ui.CalMonth)] = err
		return
	}
	ui.LoadProject(StatusCacheKey(ui.CalMonth), from, to, force)
}

func (ui *UI) LoadReport(force bool) {
	ui.EnsureProjectCache()
	from, to, err := backend.ReportFetchBounds(ui.RepMonth)
	if err != nil {
		ui.ProjErr[ReportCacheKey(ui.RepMonth)] = err
		return
	}
	ui.LoadProject(ReportCacheKey(ui.RepMonth), from, to, force)
}

// RefreshTodayScore refetches every month the Log tab is showing GitHub data
// for, and with it the Status calendar wherever the two share a month — one
// cache, one fetch, both up to date.
//
// That is today's month *and* whatever months the week strip is standing on,
// which is not the same thing. A week runs Monday to Sunday and the cache is a
// month at a time, so the week of the 31st is served by two of them. Refreshing
// only today's month left the other half of that strip showing whatever it had
// cached: work pushed onto the 31st stayed invisible on the Log tab, and the
// only way to see it was to open Status, walk back a month and refresh there.
//
// The months are named here rather than going through loadStatus, which keys off
// whatever month the calendar happens to be left on.
func (ui *UI) RefreshTodayScore() {
	if len(backend.ProjectURLs(ui.Cfg)) == 0 {
		return // nothing to score against
	}
	ui.EnsureProjectCache()
	for _, month := range ui.LogTabMonths() {
		from, to, err := backend.MonthBounds(month)
		if err != nil {
			continue
		}
		key := StatusCacheKey(month)
		delete(ui.ProjLoaded, key)
		ui.LoadProject(key, from, to, true)
	}
}

// LogTabMonths is every month the Log tab reads board data for: today's, for the
// score at its foot, and the one or two the shown week falls across.
func (ui *UI) LogTabMonths() []string {
	months := []string{ThisMonth()}
	seen := map[string]bool{ThisMonth(): true}
	for _, m := range WeekMonths(ui.WeekStart) {
		if !seen[m] {
			seen[m] = true
			months = append(months, m)
		}
	}
	return months
}

// RefreshAfterPush refetches the GitHub data a push has just made wrong.
//
// Everything the app says about pushed work — the calendar, the report, the
// week strip, the score at the foot of the Log tab — is served out of ranges
// cached by date, and a push wrote a worklog item into one of them. Nothing
// dropped those, so a pushed entry stayed invisible on GitHub's side of the app
// until the month was reloaded by hand. Every cached range covering the pushed
// date goes; ranges it cannot have touched are left alone rather than costing a
// fetch each. A range still in the air is marked instead, and reloaded by
// loadProject when the answer that predates the push comes back.
func (ui *UI) RefreshAfterPush(date string) {
	if date == "" || len(backend.ProjectURLs(ui.Cfg)) == 0 {
		return
	}
	ui.EnsureProjectCache()
	for key, loading := range ui.ProjLoading {
		if _, _, ok := keyCovers(key, date); loading && ok {
			ui.ProjStale[key] = true
		}
	}
	// Picked first, refetched second: loadProject writes the very map being
	// walked to choose them.
	for key, span := range StaleRanges(ui.ProjLoaded, ui.ProjLoading, date) {
		delete(ui.ProjLoaded, key)
		ui.LoadProject(key, span[0], span[1], true)
	}
}

// keyCovers reports whether a cached range holds date, and what its span is.
func keyCovers(key, date string) (string, string, bool) {
	from, to, ok := CacheBounds(key)
	return from, to, ok && from <= date && date <= to
}

// StaleRanges is the set of ranges a push on date has made wrong: the ones
// already sitting in the cache, holding that date, with no fetch of their own
// already in the air. Each maps to the span to ask GitHub for again.
func StaleRanges(loaded, loading map[string]bool, date string) map[string][2]string {
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

// Today is the working day, not the calendar one: it turns over at 08:00 so
// work carried past midnight still lands on the day it belongs to. See
// workDayStart in weekstrip.go for why.
func Today() string { return backend.WorkDate(time.Now()) }

func ThisMonth() string { return Today()[:7] }

// CurrentPeriod is the payroll period today falls in, named by the month it
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
func CurrentPeriod() string { return PeriodOf(time.Now()) }

func PeriodOf(t time.Time) string {
	m := t.Month()
	if t.Day() >= 21 {
		m++ // time.Date normalises December + 1 into January of the next year
	}
	return time.Date(t.Year(), m, 1, 0, 0, 0, 0, t.Location()).Format("2006-01")
}

func Pad2(n int) string { return fmt.Sprintf("%02d", n) }

func ShiftMonth(m string, delta int) string {
	t, err := time.Parse("2006-01", m)
	if err != nil {
		return m
	}
	return t.AddDate(0, delta, 0).Format("2006-01")
}

func MonthName(m string) string {
	t, err := time.Parse("2006-01", m)
	if err != nil {
		return m
	}
	return t.Format("January 2006")
}

func ReportPeriodName(month string) string {
	from, to, err := backend.ReportBounds(month)
	if err != nil {
		return month
	}
	start, _ := time.Parse("2006-01-02", from)
	end, _ := time.Parse("2006-01-02", to)
	return fmt.Sprintf("%s – %s", start.Format("Jan 2"), end.Format("Jan 2, 2006"))
}

func CommaAmount(v float64) string {
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

// DataDir is where the log, settings and planner live: a data folder beside
// the executable. An install from before that folder existed keeps its files
// next to the executable, so those are still read from there.
func DataDir() string {
	if d := os.Getenv("WORKLOG_DIR"); d != "" {
		return d
	}
	base, err := os.Executable()
	if err == nil {
		base = filepath.Dir(base)
	} else {
		base, _ = os.Getwd()
	}
	return DataDirIn(base)
}

func DataDirIn(base string) string {
	data := filepath.Join(base, "data")
	if info, err := os.Stat(data); err == nil && info.IsDir() {
		return data
	}
	for _, name := range []string{"worklog.csv", "config.json"} {
		if _, err := os.Stat(filepath.Join(base, name)); err == nil {
			return base
		}
	}
	if err := os.MkdirAll(data, 0o755); err != nil {
		return base
	}
	return data
}

// ExportsDir is the folder report exports are written to, inside dataDir.
func ExportsDir() string {
	dir := filepath.Join(DataDir(), "exports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return DataDir()
	}
	return dir
}

//go:embed icon.png
var AppIconPNG []byte

// WindowTitle is the app window's title. Named once because the Windows
// maximise call finds the window by it — a rename here that missed there would
// leave the window at 1440x920.
const WindowTitle = "Worklog"

// RefreshRate re-reads the exchange rate behind the Report tab.
//
// force is the Settings button; opening the tab passes false, which fetches at
// most once a day. The rate moves slowly and the report is opened often, so a
// fetch per visit would be a request per glance for a figure that would not
// have changed — but a rate stamped last month quietly misstates the pay.
func (ui *UI) RefreshRate(force bool) {
	if !ui.HasCfg || ui.RateLoading {
		return
	}
	base := strings.ToUpper(strings.TrimSpace(ui.Cfg.Currency))
	disp := strings.TrimSpace(ui.Cfg.DisplayCurrency)
	if base == "" || disp == "" {
		return // nothing to convert between
	}
	if !force && ui.Cfg.FxUpdated == Today() {
		return
	}
	if base == "RM" {
		base = "MYR"
	}
	ui.RateLoading = true
	_, finish := ui.BeginFetch("rate", "Fetching exchange rate")
	var r float64
	ui.Async(func() error {
		v, _ := backend.FetchRate(base, disp)
		r = v
		return nil // a rate that will not load must not pop a dialog over the tab
	}, func() {
		ui.RateLoading = false
		defer finish()
		if r <= 0 {
			return // keep yesterday's rate rather than zeroing the conversion
		}
		ui.Cfg.FxRate = r
		ui.Cfg.FxUpdated = Today()
		_ = ui.Store.SaveConfig(ui.Cfg)
		Hooks.DrawReport(ui)
	})
}

// error dialog helper
func (ui *UI) Errf(err error) {
	dialog.ShowError(err, ui.Win)
}

// run a slow func off the UI thread, then apply result on the UI thread
func (ui *UI) Async(work func() error, done func()) {
	go func() {
		defer ui.recoverToDialog("background task")
		err := work()
		fyne.Do(func() {
			defer ui.recoverToDialog("UI update")
			if err != nil {
				ui.Errf(err)
				return
			}
			if done != nil {
				done()
			}
		})
	}()
}

// PushSpinner puts a thin progress bar with a caption and percentage in front
// of the window while a push runs. Callers get back two funcs: a progress
// callback the push emits to as each step lands, and a stop that closes the
// modal.
//
// A push is several round trips — the sub-issue, then a board write per field —
// and the only sign it was running was a line of text in the popup. The window
// looked hung, and the button looked ready for a second click. The modal says
// wait, shows what step is running, and takes the clicks in the meantime.
//
// The returned stop is safe to call twice, and must be called on the UI thread
// — which is where async runs its done func. The progress callback is safe to
// call from a background goroutine; it hops onto the UI thread on its own.
func (ui *UI) PushSpinner(label string) (func(backend.FetchProgress), func()) {
	ind := NewLoadingIndicator()
	ind.SetActive(true)
	// A fixed narrow width keeps the modal compact — the bar is a signal that
	// something is running, not a canvas to be filled. GridWrap enforces the
	// min size on the strip while letting its height come from the indicator.
	sized := container.New(layout.NewGridWrapLayout(fyne.NewSize(320, ind.View.MinSize().Height)), ind.View)
	d := dialog.NewCustomWithoutButtons(label,
		container.NewPadded(sized), ui.Win)
	d.Show()

	stopped := false
	progress := func(p backend.FetchProgress) {
		fyne.Do(func() {
			if stopped {
				return
			}
			ind.ShowProgress(true, p)
		})
	}
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		ind.SetActive(false)
		d.Hide()
	}
	return progress, stop
}

// recoverToDialog turns a panic into a logged crash.log entry + error dialog,
// so a rendering bug no longer takes the whole window down.
func (ui *UI) recoverToDialog(where string) {
	r := recover()
	if r == nil {
		return
	}
	msg := fmt.Sprintf("panic in %s: %v\n%s", where, r, debug.Stack())
	_ = os.WriteFile(filepath.Join(DataDir(), "crash.log"),
		[]byte(time.Now().Format(time.RFC3339)+"\n"+msg+"\n"), 0o644)
	fyne.Do(func() { ui.Errf(fmt.Errorf("Something went wrong (%s). Details saved to crash.log.", where)) })
}
