package main

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
)

// The Meeting tab is the same commit sweep as Commit List, but on a Thursday-
// through-Thursday window — the reporting span for the Thursday weekly update,
// with the previous meeting's own Thursday included so the report reads as
// "everything since we last met, through today". Left 90% is the calendar of
// commits; right 10% is the local worklog entries filed against those same
// eight days.

// meetingWeekStartOf returns the previous Thursday that begins the meeting
// window covering date. Thursday itself still belongs to the just-closed
// window — the meeting on Thursday reports on the week ending that day — so
// the roll-over to a new window happens the next morning, on Friday.
func meetingWeekStartOf(date string) string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(date))
	if err != nil {
		t = time.Now()
	}
	// Shift a day back so Thursday reports on the just-finished window instead
	// of opening a fresh one; the offset math then finds the Thursday on or
	// before yesterday, and the 8-day span meetingWeekDates renders lands on
	// Thu → Thu inclusive of today.
	prior := t.AddDate(0, 0, -1)
	off := (int(prior.Weekday()) - int(time.Thursday) + 7) % 7
	return prior.AddDate(0, 0, -off).Format("2006-01-02")
}

// meetingWeekDates lists the eight days of a meeting window, previous Thursday
// through this Thursday inclusive. weekDates is Monday-first and returns seven
// days, so the meeting tab has its own helper rather than reusing the strip's.
func meetingWeekDates(start string) []string {
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

func (ui *UI) buildMeetingTab() fyne.CanvasObject {
	walkTo := func(start string) {
		ui.meetingStart = start
		ui.loadMeetingCommits(false)
		ui.drawMeeting()
	}
	// The visible week on first render is meetingWeekStartOf(today()); the
	// meetingStart field only gets set once the user walks somewhere. So the
	// prev/next arrows have to fall back to the same default as drawMeeting,
	// otherwise a fresh session sees them shift from "" and get nowhere.
	currentStart := func() string {
		return orDefault(ui.meetingStart, meetingWeekStartOf(today()))
	}
	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		walkTo(shiftWeek(currentStart(), -1))
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		walkTo(shiftWeek(currentStart(), 1))
	})
	here := widget.NewButton("This week", func() { walkTo(meetingWeekStartOf(today())) })
	refresh := widget.NewButtonWithIcon("Refresh from GitHub", theme.ViewRefreshIcon(), func() {
		ui.loadMeetingCommits(true)
	})

	ui.meetingTitle = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	ui.meetingHere = here
	head := container.NewHBox(prev, ui.meetingTitle, next, here, layout.NewSpacer(), refresh)

	// Stable holders let redraws swap their contents without rebuilding the
	// rest of the tab. Their parent uses a fixed 90/10 layout, not a split, so
	// the sidebar is always visible and cannot be dragged wider or narrower.
	ui.meetingCalendarHolder = container.NewStack()
	ui.meetingRowsHolder = container.NewStack()

	ui.meetingBox = container.NewStack()
	ui.meetingProgress = newLoadingIndicator()
	ui.meetingBody = container.NewBorder(container.NewVBox(head, ui.meetingProgress.view), nil, nil, nil, ui.meetingBox)
	return ui.meetingBody
}

func (ui *UI) drawMeeting() {
	defer ui.syncLoading()
	if ui.meetingBox == nil {
		return
	}
	ui.refreshLoggedShas()
	weekStart := orDefault(ui.meetingStart, meetingWeekStartOf(today()))
	days := meetingWeekDates(weekStart)

	ui.meetingTitle.SetText(meetingWeekRangeLabel(weekStart))
	if ui.meetingHere != nil {
		if weekStart == meetingWeekStartOf(today()) {
			ui.meetingHere.Disable()
		} else {
			ui.meetingHere.Enable()
		}
	}

	if len(ui.cfg.Repos) == 0 {
		ui.meetingBox.Objects = []fyne.CanvasObject{
			widget.NewLabel("Add a repo or org to scan in Settings."),
		}
		ui.meetingBox.Refresh()
		relayout(ui.meetingBody)
		return
	}

	ui.ensureMeetingCommitCache()
	loading := ui.meetingCommitLoad[weekStart]
	commits, cached := ui.meetingCommits[weekStart]
	errs := ui.meetingCommitErrs[weekStart]

	topRows := []fyne.CanvasObject{ui.commitWeekSummary(commits, loading, cached)}
	if len(errs) > 0 {
		for _, e := range errs {
			topRows = append(topRows, colorLabel(e, theme.ColorNameError))
		}
	}
	top := container.NewVBox(topRows...)

	byDay := map[string][]Commit{}
	for _, c := range commits {
		byDay[c.Date] = append(byDay[c.Date], c)
	}
	// The sidebar is a fixed ten percent of the tab. A custom layout keeps that
	// ratio exact without exposing the draggable divider of an HSplit.
	calendarBody := container.NewBorder(top, nil, nil, nil,
		ui.commitDaysPanelWithIssueLabel(days, byDay, meetingIssueLabel))
	calendarPane := container.NewScroll(calendarBody)
	rowsPane := container.NewScroll(ui.meetingRowsPanel(days))

	// Swap only the inner content of the two persistent holders.
	ui.meetingCalendarHolder.Objects = []fyne.CanvasObject{calendarPane}
	ui.meetingCalendarHolder.Refresh()
	ui.meetingRowsHolder.Objects = []fyne.CanvasObject{rowsPane}
	ui.meetingRowsHolder.Refresh()

	ui.meetingBox.Objects = []fyne.CanvasObject{
		container.New(meetingColumnsLayout{}, ui.meetingCalendarHolder, ui.meetingRowsHolder),
	}
	ui.meetingBox.Refresh()
	relayout(ui.meetingBody)
}

// meetingIssueLabel omits the owner because the Meeting calendar already
// identifies it with the bubble colour. Commit List deliberately keeps its
// full owner/repo#number reference.
func meetingIssueLabel(issue string) string {
	if issue == "" {
		return issueTag(issue)
	}
	_, repo, number, err := splitIssue(issue)
	if err != nil {
		return issue
	}
	return fmt.Sprintf("%s#%d", repo, number)
}

const meetingRowsFraction float32 = 0.10

// meetingColumnsLayout fixes the calendar/sidebar ratio at 90/10. Unlike an
// HSplit it has no divider interaction, so the user cannot resize the sidebar.
type meetingColumnsLayout struct{}

func (meetingColumnsLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	if len(objs) < 2 {
		if len(objs) == 1 {
			return objs[0].MinSize()
		}
		return fyne.NewSize(0, 0)
	}
	calendarMin := objs[0].MinSize()
	rowsMin := objs[1].MinSize()
	width := max(calendarMin.Width/(1-meetingRowsFraction), rowsMin.Width/meetingRowsFraction)
	return fyne.NewSize(width, max(calendarMin.Height, rowsMin.Height))
}

func (meetingColumnsLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) < 2 {
		return
	}
	calendarWidth := size.Width * (1 - meetingRowsFraction)
	rowsWidth := size.Width - calendarWidth
	objs[0].Move(fyne.NewPos(0, 0))
	objs[0].Resize(fyne.NewSize(calendarWidth, size.Height))
	objs[1].Move(fyne.NewPos(calendarWidth, 0))
	objs[1].Resize(fyne.NewSize(rowsWidth, size.Height))
}

// meetingRowsPanel is the right column: the eight days of logged work rolled
// up to one line per issue, showing total minutes and a link to the issue.
// Same pattern Log List's pendingIssuesPanel uses — the meeting update reads
// as "here is where the week went, per issue" without repeating a row for
// every day the same issue was touched.
func (ui *UI) meetingRowsPanel(days []string) fyne.CanvasObject {
	inDays := map[string]bool{}
	for _, d := range days {
		inDays[d] = true
	}
	rows, _ := ui.store.ReadRows()

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
			key = ui.unfiledLabel(r)
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
		if info, ok := ui.issueInfo[b.key]; ok && strings.TrimSpace(info.Title) != "" {
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
		return container.NewVScroll(compactVBox(items...))
	}
	for _, p := range pairs {
		items = append(items, ui.meetingRowLine(p.key, p.title, p.mins))
	}
	items = append(items, widget.NewSeparator())
	items = append(items, widget.NewLabelWithStyle(
		fmt.Sprintf("Total: %dm · %d entries · %d issues", total, entries, len(pairs)),
		fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
	return container.NewVScroll(compactVBox(items...))
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
func (ui *UI) meetingRowLine(issue, title string, mins int) fyne.CanvasObject {
	head := &widget.TextSegment{
		Text: fmt.Sprintf("%dm · ", mins),
		Style: widget.RichTextStyle{
			Inline:    true,
			TextStyle: fyne.TextStyle{Monospace: true, Bold: true},
		},
	}
	segments := []widget.RichTextSegment{head}
	if owner, repo, num, err := splitIssue(issue); err == nil {
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

func (ui *UI) loadMeetingCommits(force bool) {
	if ui.meetingBox == nil {
		return
	}
	ui.ensureMeetingCommitCache()
	weekStart := orDefault(ui.meetingStart, meetingWeekStartOf(today()))
	days := meetingWeekDates(weekStart)
	from, to := days[0], days[len(days)-1]

	if ui.meetingCommitLoad[weekStart] {
		return
	}
	if !force {
		if _, cached := ui.meetingCommits[weekStart]; cached {
			if !pendingIsStale(ui.meetingCommitAt[weekStart]) {
				return
			}
		}
	}
	if len(ui.cfg.Repos) == 0 {
		return
	}
	ui.meetingCommitLoad[weekStart] = true
	progress, finish := ui.beginFetch("meeting:"+weekStart, "Discovering repositories and branches")
	ui.drawMeeting()

	composer := newProgressComposer(progress)
	const commitsWeight, titlesWeight = 80.0, 20.0

	var commits []Commit
	var errs []string
	var ferr error
	ui.async(func() error {
		commits, errs, ferr = ui.store.FetchWeekCommits(ui.cfg, from, to, composer.phase(commitsWeight))
		return nil
	}, func() {
		ui.meetingCommitLoad[weekStart] = false
		composer.advance(commitsWeight)
		if ferr != nil {
			finish()
			ui.meetingCommitErrs[weekStart] = []string{ferr.Error()}
			ui.drawMeeting()
			return
		}
		ui.meetingCommits[weekStart] = commits
		ui.meetingCommitErrs[weekStart] = errs
		ui.meetingCommitAt[weekStart] = time.Now()
		ui.loadMeetingIssueInfos(commits, weekStart, composer, titlesWeight, finish)
		ui.drawMeeting()
	})
}

func (ui *UI) loadMeetingIssueInfos(commits []Commit, week string, composer *progressComposer, weight float64, finish func()) {
	if ui.issueInfo == nil {
		ui.issueInfo = map[string]IssueInfo{}
	}
	seen := map[string]bool{}
	var refs []string
	add := func(ref string) {
		if ref == "" || seen[ref] {
			return
		}
		seen[ref] = true
		if _, done := ui.issueInfo[ref]; !done {
			refs = append(refs, ref)
		}
	}
	for _, c := range commits {
		add(c.Issue)
	}
	// Local rows on the same eight days: their issue titles feed the right
	// column, so fetching them here means the panel opens with real names
	// rather than bare "owner/repo#123" refs.
	if rows, err := ui.store.ReadRows(); err == nil {
		inDays := map[string]bool{}
		for _, d := range meetingWeekDates(week) {
			inDays[d] = true
		}
		for _, r := range rows {
			if inDays[r["date"]] {
				add(strings.TrimSpace(r["issue"]))
			}
		}
	}
	if len(refs) == 0 {
		composer.advance(weight)
		finish()
		return
	}
	var infos map[string]IssueInfo
	ui.async(func() error {
		infos, _ = FetchIssueInfos(refs, composer.phase(weight))
		return nil
	}, func() {
		composer.advance(weight)
		defer finish()
		for ref, info := range infos {
			ui.issueInfo[ref] = info
		}
		ui.drawMeeting()
	})
}

func (ui *UI) ensureMeetingCommitCache() {
	if ui.meetingCommits == nil {
		ui.meetingCommits = map[string][]Commit{}
		ui.meetingCommitErrs = map[string][]string{}
		ui.meetingCommitLoad = map[string]bool{}
		ui.meetingCommitAt = map[string]time.Time{}
	}
}
