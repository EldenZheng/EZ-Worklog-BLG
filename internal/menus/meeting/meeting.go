package meeting

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// MeetingWeekDates lists the eight days of a meeting window, previous Thursday
// through this Thursday inclusive. weekDates is Monday-first and returns seven
// days, so the meeting tab has its own helper rather than reusing the strip's.
func MeetingWeekDates(start string) []string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(start))
	if err != nil {
		return nil
	}
	out := make([]string, 8)
	for i := range out {
		out[i] = t.AddDate(0, 0, i).Format("2006-01-02")
	}
	return out
}

// meetingWeekRangeLabel is weekRangeLabel for an eight-day window (start + 7
// days), so a Thu→Thu span reads correctly rather than one day short.
func meetingWeekRangeLabel(start string) string {
	a, err := time.Parse("2006-01-02", strings.TrimSpace(start))
	if err != nil {
		return start
	}
	b := a.AddDate(0, 0, 7)
	switch {
	case a.Year() != b.Year():
		return fmt.Sprintf("%s – %s", a.Format("2 Jan 2006"), b.Format("2 Jan 2006"))
	case a.Month() != b.Month():
		return fmt.Sprintf("%d %s – %d %s %d", a.Day(), a.Format("Jan"), b.Day(), b.Format("Jan"), b.Year())
	default:
		return fmt.Sprintf("%d – %d %s %d", a.Day(), b.Day(), b.Format("Jan"), b.Year())
	}
}

func BuildMeetingTab(ui *shell.UI) fyne.CanvasObject {
	walkTo := func(start string) {
		ui.MeetingStart = start
		LoadMeetingCommits(ui, false)
		DrawMeeting(ui)
	}
	// The visible week on first render is meetingWeekStartOf(today()); the
	// meetingStart field only gets set once the user walks somewhere. So the
	// prev/next arrows have to fall back to the same default as drawMeeting,
	// otherwise a fresh session sees them shift from "" and get nowhere.
	currentStart := func() string {
		return backend.OrDefault(ui.MeetingStart, shell.MeetingWeekStartOf(shell.Today()))
	}
	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		walkTo(shell.ShiftWeek(currentStart(), -1))
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		walkTo(shell.ShiftWeek(currentStart(), 1))
	})
	here := widget.NewButton("This week", func() { walkTo(shell.MeetingWeekStartOf(shell.Today())) })
	refresh := widget.NewButtonWithIcon("Refresh from GitHub", theme.ViewRefreshIcon(), func() {
		LoadMeetingCommits(ui, true)
	})

	ui.MeetingTitle = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	ui.MeetingHere = here
	head := container.NewHBox(prev, ui.MeetingTitle, next, here, layout.NewSpacer(), refresh)

	// Stable holders let redraws swap their contents without rebuilding the
	// rest of the tab. Their parent uses a fixed 90/10 layout, not a split, so
	// the sidebar is always visible and cannot be dragged wider or narrower.
	ui.MeetingCalendarHolder = container.NewStack()
	ui.MeetingRowsHolder = container.NewStack()

	ui.MeetingBox = container.NewStack()
	ui.MeetingProgress = shell.NewLoadingIndicator()
	ui.MeetingBody = container.NewBorder(container.NewVBox(head, ui.MeetingProgress.View), nil, nil, nil, ui.MeetingBox)
	return ui.MeetingBody
}

func DrawMeeting(ui *shell.UI) {
	defer ui.SyncLoading()
	if ui.MeetingBox == nil {
		return
	}
	ui.RefreshLoggedShas()
	weekStart := backend.OrDefault(ui.MeetingStart, shell.MeetingWeekStartOf(shell.Today()))
	days := MeetingWeekDates(weekStart)

	ui.MeetingTitle.SetText(meetingWeekRangeLabel(weekStart))
	if ui.MeetingHere != nil {
		if weekStart == shell.MeetingWeekStartOf(shell.Today()) {
			ui.MeetingHere.Disable()
		} else {
			ui.MeetingHere.Enable()
		}
	}

	if len(ui.Cfg.Repos) == 0 {
		ui.MeetingBox.Objects = []fyne.CanvasObject{
			widget.NewLabel("Add a repo or org to scan in Settings."),
		}
		ui.MeetingBox.Refresh()
		shell.Relayout(ui.MeetingBody)
		return
	}

	EnsureMeetingCommitCache(ui)
	loading := ui.MeetingCommitLoad[weekStart]
	commits, cached := ui.MeetingCommits[weekStart]
	errs := ui.MeetingCommitErrs[weekStart]

	topRows := []fyne.CanvasObject{ui.CommitWeekSummary(commits, loading, cached)}
	if len(errs) > 0 {
		for _, e := range errs {
			topRows = append(topRows, shell.ColorLabel(e, theme.ColorNameError))
		}
	}
	top := container.NewVBox(topRows...)

	byDay := map[string][]backend.Commit{}
	for _, c := range commits {
		byDay[c.Date] = append(byDay[c.Date], c)
	}
	// The sidebar is a fixed ten percent of the tab. A custom layout keeps that
	// ratio exact without exposing the draggable divider of an HSplit.
	calendarBody := container.NewBorder(top, nil, nil, nil,
		ui.CommitDaysPanelWithIssueLabel(days, byDay, MeetingIssueLabel))
	// Vertical scrolling only: the calendar and the sidebar each fit their own
	// column's width, so neither slides under the other.
	calendarPane := container.NewVScroll(calendarBody)
	rowsPane := meetingRowsPanel(ui, days)

	// Swap only the inner content of the two persistent holders. The logged
	// rows are their own menu, so they sit in a rounded panel like Settings
	// and scroll inside it while the panel stays put.
	ui.MeetingCalendarHolder.Objects = []fyne.CanvasObject{calendarPane}
	ui.MeetingCalendarHolder.Refresh()
	ui.MeetingRowsHolder.Objects = []fyne.CanvasObject{shell.PanelFrame(rowsPane)}
	ui.MeetingRowsHolder.Refresh()

	ui.MeetingBox.Objects = []fyne.CanvasObject{
		container.New(MeetingColumnsLayout{}, ui.MeetingCalendarHolder, ui.MeetingRowsHolder),
	}
	ui.MeetingBox.Refresh()
	shell.Relayout(ui.MeetingBody)
}

// MeetingIssueLabel omits the owner because the Meeting calendar already
// identifies it with the bubble colour. Commit List deliberately keeps its
// full owner/repo#number reference.
func MeetingIssueLabel(issue string) string {
	if issue == "" {
		return shell.IssueTag(issue)
	}
	_, repo, number, err := backend.SplitIssue(issue)
	if err != nil {
		return issue
	}
	return fmt.Sprintf("%s#%d", repo, number)
}

const MeetingRowsFraction float32 = 0.10

// MeetingColumnsLayout fixes the calendar/sidebar ratio at 90/10. Unlike an
// HSplit it has no divider interaction, so the user cannot resize the sidebar.
type MeetingColumnsLayout struct{}

// meetingColumnGap keeps the two column panels apart.
func meetingColumnGap() float32 { return theme.InnerPadding() }

func (MeetingColumnsLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	if len(objs) < 2 {
		if len(objs) == 1 {
			return objs[0].MinSize()
		}
		return fyne.NewSize(0, 0)
	}
	calendarMin := objs[0].MinSize()
	rowsMin := objs[1].MinSize()
	width := max(calendarMin.Width/(1-MeetingRowsFraction), rowsMin.Width/MeetingRowsFraction) + meetingColumnGap()
	return fyne.NewSize(width, max(calendarMin.Height, rowsMin.Height))
}

func (MeetingColumnsLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) < 2 {
		return
	}
	gap := meetingColumnGap()
	rowsX := size.Width * (1 - MeetingRowsFraction)
	rowsWidth := size.Width - rowsX
	objs[0].Move(fyne.NewPos(0, 0))
	objs[0].Resize(fyne.NewSize(max(rowsX-gap, 0), size.Height))
	objs[1].Move(fyne.NewPos(rowsX, 0))
	objs[1].Resize(fyne.NewSize(rowsWidth, size.Height))
}

// meetingRowsPanel is the right column: the eight days of logged work rolled
// up to one line per issue, showing total minutes and a link to the issue.
// Same pattern Log List's pendingIssuesPanel uses — the meeting update reads
// as "here is where the week went, per issue" without repeating a row for
// every day the same issue was touched.
func meetingRowsPanel(ui *shell.UI, days []string) fyne.CanvasObject {
	inDays := map[string]bool{}
	for _, d := range days {
		inDays[d] = true
	}
	rows, _ := ui.Store.ReadRows()

	type bucket struct {
		key     string // the issue ref or unfiled label
		title   string
		mins    int
		entries int
	}
	byKey := map[string]*bucket{}
	total := 0
	entries := 0
	for _, r := range rows {
		if !inDays[r["date"]] {
			continue
		}
		mins := r.Minutes()
		total += mins
		entries++
		key := strings.TrimSpace(r["issue"])
		if key == "" {
			key = ui.UnfiledLabel(r)
		}
		b, ok := byKey[key]
		if !ok {
			b = &bucket{key: key}
			byKey[key] = b
		}
		b.mins += mins
		b.entries++
	}
	// Titles come from the issueInfo cache (populated by the tab's title
	// fetch), falling back to the raw ref for anything still unresolved.
	for _, b := range byKey {
		if info, ok := ui.IssueInfo[b.key]; ok && strings.TrimSpace(info.Title) != "" {
			b.title = info.Title
		} else {
			b.title = b.key
		}
	}
	// Biggest chunks first — the meeting update reads top-down as time-spent.
	pairs := make([]*bucket, 0, len(byKey))
	for _, b := range byKey {
		pairs = append(pairs, b)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].mins != pairs[j].mins {
			return pairs[i].mins > pairs[j].mins
		}
		return pairs[i].title < pairs[j].title
	})

	items := []fyne.CanvasObject{
		widget.NewLabelWithStyle("Logged this week", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	}
	if len(pairs) == 0 {
		items = append(items, widget.NewLabel("Nothing logged yet."))
		return shell.PanelScroll(compactVBox(items...), true)
	}
	for _, p := range pairs {
		items = append(items, meetingRowLine(ui, p.key, p.title, p.mins))
	}
	items = append(items, widget.NewSeparator())
	totalLine := widget.NewLabelWithStyle(
		fmt.Sprintf("Total: %dm · %d entries · %d issues", total, entries, len(pairs)),
		fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	totalLine.Wrapping = fyne.TextWrapWord
	items = append(items, totalLine)
	return shell.PanelScroll(compactVBox(items...), true)
}

// meetingRowLine is one row on the rows panel: "<minutes> · <issue title>" as
// a single RichText paragraph so minutes and title stay inline on the same
// line, and the whole line wraps as one block when the title runs long. The
// title carries the hyperlink; nothing gets cut off, and the amount never
// floats above a wrapped second line.
//
// Inline: true on every segment is what keeps them on the same visual line —
// without it, each RichTextSegment starts on a new line and the amount ends
// up on a row of its own above the title.
func meetingRowLine(ui *shell.UI, issue, title string, mins int) fyne.CanvasObject {
	head := &widget.TextSegment{
		Text: fmt.Sprintf("%dm · ", mins),
		Style: widget.RichTextStyle{
			Inline:    true,
			TextStyle: fyne.TextStyle{Monospace: true, Bold: true},
		},
	}
	segments := []widget.RichTextSegment{head}
	if owner, repo, num, err := backend.SplitIssue(issue); err == nil {
		if u, e := url.Parse(fmt.Sprintf(
			"https://github.com/%s/%s/issues/%d", owner, repo, num)); e == nil {
			segments = append(segments, &widget.HyperlinkSegment{Text: title, URL: u})
		} else {
			segments = append(segments, &widget.TextSegment{Text: title,
				Style: widget.RichTextStyle{Inline: true}})
		}
	} else {
		segments = append(segments, &widget.TextSegment{Text: title,
			Style: widget.RichTextStyle{Inline: true}})
	}
	rt := widget.NewRichText(segments...)
	rt.Wrapping = fyne.TextWrapWord
	return rt
}

// compactVBox is a VBox with a shrunk padding — the labels sit close together
// while still respecting each child's real (wrap-aware) height, so wrapping
// RichText rows can grow onto a second line without the row below overlapping
// them.
func compactVBox(objects ...fyne.CanvasObject) fyne.CanvasObject {
	return container.NewThemeOverride(container.NewVBox(objects...),
		tightListTheme{Theme: theme.Current()})
}

// tightListTheme shrinks VBox spacing and label inner padding so the
// "Logged this week" column reads compact without dropping down to a
// zero-gap layout that fights wrapping widgets over their real height.
type tightListTheme struct{ fyne.Theme }

func (t tightListTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 3
	case theme.SizeNameInnerPadding:
		return 4
	}
	return t.Theme.Size(name)
}

func LoadMeetingCommits(ui *shell.UI, force bool) {
	if ui.MeetingBox == nil {
		return
	}
	EnsureMeetingCommitCache(ui)
	weekStart := backend.OrDefault(ui.MeetingStart, shell.MeetingWeekStartOf(shell.Today()))
	days := MeetingWeekDates(weekStart)
	from, to := days[0], days[len(days)-1]

	if ui.MeetingCommitLoad[weekStart] {
		return
	}
	if !force {
		if _, cached := ui.MeetingCommits[weekStart]; cached {
			if !shell.PendingIsStale(ui.MeetingCommitAt[weekStart]) {
				return
			}
		}
	}
	if len(ui.Cfg.Repos) == 0 {
		return
	}
	ui.MeetingCommitLoad[weekStart] = true
	progress, finish := ui.BeginFetch("meeting:"+weekStart, "Discovering repositories and branches")
	DrawMeeting(ui)

	composer := shell.NewProgressComposer(progress)
	const commitsWeight, titlesWeight = 80.0, 20.0

	var commits []backend.Commit
	var errs []string
	var ferr error
	ui.Async(func() error {
		commits, errs, ferr = ui.Store.FetchWeekCommits(ui.Cfg, from, to, composer.Phase(commitsWeight))
		return nil
	}, func() {
		ui.MeetingCommitLoad[weekStart] = false
		composer.Advance(commitsWeight)
		if ferr != nil {
			finish()
			ui.MeetingCommitErrs[weekStart] = []string{ferr.Error()}
			DrawMeeting(ui)
			return
		}
		ui.MeetingCommits[weekStart] = commits
		ui.MeetingCommitErrs[weekStart] = errs
		ui.MeetingCommitAt[weekStart] = time.Now()
		loadMeetingIssueInfos(ui, commits, weekStart, composer, titlesWeight, finish)
		DrawMeeting(ui)
	})
}

func loadMeetingIssueInfos(ui *shell.UI, commits []backend.Commit, week string, composer *shell.ProgressComposer, weight float64, finish func()) {
	if ui.IssueInfo == nil {
		ui.IssueInfo = map[string]backend.IssueInfo{}
	}
	seen := map[string]bool{}
	var refs []string
	add := func(ref string) {
		if ref == "" || seen[ref] {
			return
		}
		seen[ref] = true
		if _, done := ui.IssueInfo[ref]; !done {
			refs = append(refs, ref)
		}
	}
	for _, c := range commits {
		add(c.Issue)
	}
	// Local rows on the same eight days: their issue titles feed the right
	// column, so fetching them here means the panel opens with real names
	// rather than bare "owner/repo#123" refs.
	if rows, err := ui.Store.ReadRows(); err == nil {
		inDays := map[string]bool{}
		for _, d := range MeetingWeekDates(week) {
			inDays[d] = true
		}
		for _, r := range rows {
			if inDays[r["date"]] {
				add(strings.TrimSpace(r["issue"]))
			}
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
		DrawMeeting(ui)
	})
}

func EnsureMeetingCommitCache(ui *shell.UI) {
	if ui.MeetingCommits == nil {
		ui.MeetingCommits = map[string][]backend.Commit{}
		ui.MeetingCommitErrs = map[string][]string{}
		ui.MeetingCommitLoad = map[string]bool{}
		ui.MeetingCommitAt = map[string]time.Time{}
	}
}
