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

// meetingWeekStartOf returns the Thursday on or before date — the first day of
// the meeting window. When date itself falls on a Thursday, that Thursday is
// the start: it is where the previous meeting sat, so the update naturally
// begins there.
func meetingWeekStartOf(date string) string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(date))
	if err != nil {
		t = time.Now()
	}
	off := (int(t.Weekday()) - int(time.Thursday) + 7) % 7
	return t.AddDate(0, 0, -off).Format("2006-01-02")
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
	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		walkTo(shiftWeek(ui.meetingStart, -1))
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		walkTo(shiftWeek(ui.meetingStart, 1))
	})
	here := widget.NewButton("This week", func() { walkTo(meetingWeekStartOf(today())) })
	refresh := widget.NewButtonWithIcon("Refresh from GitHub", theme.ViewRefreshIcon(), func() {
		ui.loadMeetingCommits(true)
	})
	// The drawer arrow — a floating button that lives in the middle of the tab
	// vertically, on the seam between the calendar and the rows pane. Hidden
	// state hugs the right edge and opens the drawer when tapped; open state
	// sits on the split and closes it. Built once and kept, since the icon
	// swap and OnTapped state need to survive a redraw.
	ui.meetingArrow = widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		ui.meetingRowsVisible = !ui.meetingRowsVisible
		ui.drawMeeting()
	})
	ui.meetingArrow.Importance = widget.LowImportance

	ui.meetingTitle = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	ui.meetingHere = here
	head := container.NewHBox(prev, ui.meetingTitle, next, here, layout.NewSpacer(), refresh)

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
	// The rows pane is a drawer: hidden by default so the calendar owns the
	// whole tab, opened by tapping the arrow that floats on the divider
	// vertically centered. Once open the divider is draggable — the initial
	// position is 80/20 so the rows pane has room to read.
	calendarBody := container.NewBorder(top, nil, nil, nil,
		ui.commitDaysPanel(days, byDay))
	calendarPane := container.NewScroll(calendarBody)
	rowsPane := container.NewScroll(ui.meetingRowsPanel(days))

	var content fyne.CanvasObject
	if ui.meetingRowsVisible {
		if ui.meetingSplit == nil {
			ui.meetingSplit = container.NewHSplit(calendarPane, rowsPane)
			ui.meetingSplit.SetOffset(openMeetingOffset)
		} else {
			ui.meetingSplit.Leading = calendarPane
			ui.meetingSplit.Trailing = rowsPane
			ui.meetingSplit.Refresh()
		}
		content = ui.meetingSplit
		ui.meetingArrow.SetIcon(theme.NavigateNextIcon())
	} else {
		content = calendarPane
		ui.meetingArrow.SetIcon(theme.NavigateBackIcon())
	}

	// Stack the drawer arrow over the content with a custom layout so it lands
	// on the split's seam vertically centered — content underneath, arrow on
	// top, positioned at whatever offset the HSplit is currently at.
	ui.meetingBox.Objects = []fyne.CanvasObject{
		container.New(&meetingArrowLayout{ui: ui}, content, withPointerCursor(ui.meetingArrow)),
	}
	ui.meetingBox.Refresh()
	relayout(ui.meetingBody)
}

// openMeetingOffset is where the HSplit's divider lands when the drawer opens.
// 0.80 (rows pane at 20%) leaves the calendar dominant while giving a busy
// week's rows enough room to read; drag from here in either direction.
const openMeetingOffset = 0.80

// meetingArrowLayout stacks the drawer arrow over the tab content. objs[0] is
// the content (calendar alone, or the split), objs[1] is the arrow button.
// The arrow's centre lands on the divider line — at the right edge of the tab
// when the drawer is closed, or on the split's own seam when it is open — and
// it is vertically centred against the tab's height.
type meetingArrowLayout struct{ ui *UI }

func (l *meetingArrowLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	if len(objs) == 0 {
		return fyne.NewSize(0, 0)
	}
	return objs[0].MinSize()
}

func (l *meetingArrowLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) < 2 {
		return
	}
	content, arrow := objs[0], objs[1]
	content.Resize(size)
	content.Move(fyne.NewPos(0, 0))
	// Divider position: right edge of the tab when the drawer is closed,
	// otherwise the split's own Offset — following the pointer if the user is
	// dragging the divide themselves.
	divider := size.Width
	if l.ui != nil && l.ui.meetingRowsVisible && l.ui.meetingSplit != nil {
		divider = size.Width * float32(l.ui.meetingSplit.Offset)
	}
	m := arrow.MinSize()
	x := divider - m.Width/2
	if x < 0 {
		x = 0
	}
	if maxX := size.Width - m.Width; x > maxX {
		x = maxX
	}
	arrow.Resize(m)
	arrow.Move(fyne.NewPos(x, (size.Height-m.Height)/2))
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
