package logwork

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

const (
	DraftSortDuration = "Duration"
	DraftSortCreated  = "Created (earliest first)"
)

// SortDraftRows keeps the waiting list and the cards on each day under one
// user-selected rule. Created order uses logged_at—the moment the draft was
// saved—not its worklog date, which may be changed later by dragging.
func SortDraftRows(rows []backend.Row, mode string) {
	sort.SliceStable(rows, func(i, j int) bool {
		if mode == DraftSortCreated {
			return rows[i]["logged_at"] < rows[j]["logged_at"]
		}
		return rows[i].Minutes() > rows[j].Minutes()
	})
}

// ---- the data behind the strip ----

// monthWorklogs is one month of the project's worklogs, starting that month's
// fetch the first time it is asked for. State is "ok", "loading", "off" (no
// project configured) or "error" — the same words the rest of the app uses.
func monthWorklogs(ui *shell.UI, month string) ([]backend.WorklogItem, string) {
	if len(backend.ProjectURLs(ui.Cfg)) == 0 || len(month) < 7 {
		return nil, "off"
	}
	month = month[:7]
	ui.EnsureProjectCache()
	key := shell.StatusCacheKey(month)
	if items, cached := ui.ItemsFor(key); cached {
		// Filtered here rather than at each caller: this is the one door the Log
		// work tab reads board items through — the week strip and today's score
		// both come from it — so the key governs both by governing this.
		return ui.ShownItems(items), "ok"
	}
	if ui.ProjErr[key] != nil && !ui.ProjLoading[key] {
		return nil, "error"
	}
	if !ui.ProjLoading[key] {
		// loadProject redraws every pane that reads this cache when it lands, so
		// one shot is enough; the error branch above stops a failed month being
		// retried in a loop. Status shares the key, so its tab costs nothing.
		if from, to, err := backend.MonthBounds(month); err == nil {
			ui.LoadProject(key, from, to, false)
		}
	}
	return nil, "loading"
}

func WeekWorklogs(ui *shell.UI, days []string) (map[string][]backend.WorklogItem, string) {
	want := map[string]bool{}
	var months []string
	seen := map[string]bool{}
	for _, d := range days {
		want[d] = true
		if len(d) >= 7 && !seen[d[:7]] {
			seen[d[:7]] = true
			months = append(months, d[:7])
		}
	}

	out := map[string][]backend.WorklogItem{}
	state := "ok"
	for _, m := range months {
		items, st := monthWorklogs(ui, m)
		if shell.StateRank(st) > shell.StateRank(state) {
			state = st
		}
		for _, it := range items {
			if want[it.Date] {
				out[it.Date] = append(out[it.Date], it)
			}
		}
	}
	return out, state
}

// ---- the strip itself ----

// savedReady reports whether the week's worklogs have landed: what the Saved
// locally section's own progress bar tracks. It does not wait on the commit
// sweep at the top of the tab, which takes longer and has its own bar.
func savedReady(ui *shell.UI, days []string, weekState string) bool {
	return weekState != "loading" && !ui.WeekFetching(days)
}

// savedShown reports whether the Saved locally section is drawn now. It is held
// back until it is ready for the week on show; after that a refetch leaves it
// on screen, so it does not arrive a second time.
func savedShown(ui *shell.UI, days []string, weekState string) bool {
	if savedReady(ui, days, weekState) {
		ui.SavedShownWeek = ui.WeekStart
		return true
	}
	return ui.SavedShownWeek == ui.WeekStart
}

func DrawWeekStrip(ui *shell.UI) {
	if ui.WeekBox == nil {
		return
	}
	if ui.WeekStart == "" {
		ui.WeekStart = backend.WeekStartOf(shell.Today())
	}
	days := shell.WeekDates(ui.WeekStart)
	byDay, state := WeekWorklogs(ui, days)
	shell.Hooks.DropLoggedSupportWeek(ui, ui.WeekStart, byDay, state)
	rota := false
	for _, d := range days[:5] {
		rota = rota || shell.OnSupportRota(ui.Cfg, byDay[d])
	}
	if ui.LogSearch != "" {
		filtered := map[string][]backend.WorklogItem{}
		for date, items := range byDay {
			for _, it := range items {
				if ui.WorklogMatchesSearch(it) {
					filtered[date] = append(filtered[date], it)
				}
			}
		}
		byDay = filtered
	}

	// Everything still on this machine, whatever week it was filed under: the
	// entries that can be dragged are the ones drawn hollow on their day.
	localByDay := map[string][]backend.Row{}
	if rows, err := ui.Store.ReadRows(); err == nil {
		for _, r := range ui.ShownRows(rows) {
			if !ui.RowMatchesSearch(r) {
				continue
			}
			if r["pushed_at"] != "" {
				continue
			}
			localByDay[r["date"]] = append(localByDay[r["date"]], r)
		}
	}

	// The placeholder is saved per week: walking shows the arrived-at week's own
	// setting, so one week's shift is never carried into another.
	walkTo := func(start string) {
		ui.WeekStart = start
		DrawWeekStrip(ui)
	}
	h := weekHeadFor(ui)
	prev, next, here, refresh := h.prev, h.next, h.here, h.refresh
	prev.OnTapped = func() { walkTo(shell.ShiftWeek(ui.WeekStart, -1)) }
	next.OnTapped = func() { walkTo(shell.ShiftWeek(ui.WeekStart, 1)) }
	here.OnTapped = func() { walkTo(backend.WeekStartOf(shell.Today())) }
	if ui.WeekStart == backend.WeekStartOf(shell.Today()) {
		here.Disable()
	} else {
		here.Enable()
	}
	title := widget.NewLabelWithStyle(shell.WeekRangeLabel(ui.WeekStart),
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	// Loading says nothing here: the progress strip under the row shows it.
	note := ""
	switch state {
	case "off":
		note = "Set a project URL in Settings to see what each day already holds."
	case "error":
		note = "could not read the GitHub project."
	}
	// A slim loading strip attached to the week head — the tab-level bar sits
	// on Log Work's top edge and is easy to miss when the eye is already down
	// on the calendar. Reuses the tab strip's look so it reads as one system.
	// State (visible / percent / caption) is driven by syncLoading against the
	// same fetch keys the boards load under; drawWeekStrip just places the
	// widget's view.
	if ui.WeekStripProgress == nil {
		ui.WeekStripProgress = shell.NewLoadingIndicator()
	}
	// Refresh reloads the project data behind the week strip — the boards, the
	// day fills, the score line — and only that. The commit sweep has its own
	// refresh in the pane above, and a second machine's push shows up on the
	// board without waiting for the commit list to be rescanned.
	refresh.OnTapped = func() { ui.RefreshTodayScore() }

	headLeft := container.NewHBox(prev, title, next, here)
	if ui.LogLegend != nil {
		// The legend takes the remaining width and scrolls sideways instead of
		// spending a separate row above the week.
		headLeft = container.NewBorder(nil, nil, headLeft, nil, ui.LogLegend)
	}
	// The note leads the group, so the buttons keep the same right edge
	// whether or not it is showing.
	var right []fyne.CanvasObject
	if note != "" {
		right = append(right, widget.NewLabelWithStyle(note, fyne.TextAlignTrailing, fyne.TextStyle{Italic: true}))
	}
	// Exporting the week for email lives on Log List. The buttons end at the
	// week's own right edge, level with the days under them.
	right = append(right, weekSupportControl(ui, rota), refresh)
	headRight := container.NewHBox(right...)
	sides := widgets.NewSidesLayout(headLeft, headRight, ui.WeekBox.Size().Width)
	// On a row too narrow for both, the buttons go under, still at the right.
	sides.EndAligned = true
	head := container.New(sides, headLeft, headRight)

	// Any column about to be thrown away stops breathing first: an animation left
	// running would keep refreshing a rectangle nothing is showing any more.
	for _, col := range ui.WeekCols {
		col.StopPulse()
	}
	for _, old := range ui.WeekCols {
		shell.FollowFrame(old.Frame, nil)
	}
	ui.WeekCols = map[string]*shell.DayColumn{}
	// A week shown for the first time arrives as Kanban's cards do: each day
	// uncovered from the top down, one after another left to right, its bar
	// filling in as it comes. A day's cards wipe in themselves, one after
	// another: Monday's first cardFirst into the arrival, each later day's as
	// much later as the day itself. Any other redraw leaves the days
	// be. The days are held back until the week's worklogs have landed, and
	// arrive together with the saved list, once.
	shown := savedShown(ui, days, state)
	var arrival *shell.Arrival
	if shown {
		arrival = ui.Arrival("week", ui.WeekStart)
	}
	delays := shell.ArriveDelays(len(days))
	var columns []*shell.DayColumn
	var dayCols []fyne.CanvasObject
	for i, ds := range days {
		var enter func(n int) time.Duration
		// Left is what remains of this day's own uncovering in this draw; the
		// cards start cardFirst after the day began, wherever this draw is.
		if left := arrival.Left(delays[i:i+1], weekArriveEach); left > 0 {
			first := max(0, left-weekArriveEach+cardFirst)
			enter = func(n int) time.Duration { return first + time.Duration(min(n, 12))*cardArriveStep }
		}
		col := weekColumn(ui, ds, byDay[ds], localByDay[ds], enter)
		ui.WeekCols[ds] = col
		columns = append(columns, col)
		dayCols = append(dayCols, col)
	}
	if shown {
		dayCols = arrival.RevealFor(weekArriveEach, dayCols, delays, func(i int, share float32) {
			t, _ := time.Parse("2006-01-02", days[i])
			ui.MeterShare("strip/"+t.Format("Mon"), share)
		})
	}
	grid := container.New(&WeekGrid{columns: columns}, dayCols...)

	objs := []fyne.CanvasObject{head, ui.WeekStripProgress.View, grid}
	// Only the heading and the progress strip while the week is held back.
	if !shown {
		objs = objs[:2]
	}

	ui.WeekBox.Objects = objs
	ui.WeekBox.Refresh()

	// The boards and the week read by issue live on Log List now. They follow the
	// same week as this strip, so walking the arrows here moves both.
	shell.Hooks.DrawLogList(ui)
}

// weekdayHeadingLayout reserves the weekday heading's space and draws it
// last inside its column so the day's cards pass behind it. Objects: the
// cards, the cover above the pinned outline, the clip under the heading, the
// outline, the heading.
type weekdayHeadingLayout struct {
	body fyne.Layout
	cols *dayColumnLayout
}

func (l weekdayHeadingLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	last := len(objects) - 1
	return l.body.MinSize(append(objects[:last-3:last-3], objects[last]))
}

func (l weekdayHeadingLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	last := len(objects) - 1
	heading := objects[last]
	y := heading.Position().Y
	l.body.Layout(append([]fyne.CanvasObject{heading}, objects[:last-3]...), size)
	heading.Move(fyne.NewPos(0, min(y, max(0, size.Height-heading.Size().Height))))
	if l.cols != nil && l.cols.col != nil {
		l.cols.col.FollowHeading()
	}
}

// dayColumnLayout stacks a day's floor, outline, date preview and contents,
// then brings the outline's top down to a pinned heading.
type dayColumnLayout struct{ col *shell.DayColumn }

func (l *dayColumnLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return layout.NewStackLayout().MinSize(objects)
}

func (l *dayColumnLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	layout.NewStackLayout().Layout(objects, size)
	if l.col != nil {
		l.col.FollowHeading()
	}
}

func pinWeekdayHeadings(ui *shell.UI, offset float32) {
	if len(ui.WeekBox.Objects) < 3 {
		return
	}
	// The seven columns share one row, with padding inside each column.
	pad := theme.Padding()
	top := ui.WeekBox.Objects[2].Position().Y + pad
	for _, col := range ui.WeekCols {
		head := col.Heading
		if head == nil {
			continue
		}
		room := max(0, col.Size().Height-2*pad-head.Size().Height)
		// Pinned with the day's outline brought down around it, a padding
		// below the top of the view, as it sits before the page scrolls.
		head.Move(fyne.NewPos(0, min(max(0, offset-top+2*pad), room)))
		col.FollowHeading()
	}
}

// The scroll remeasures its content even on programmatic moves that do not
// call OnScrolled, such as the pan back after the page gets shorter.
type calendarPageLayout struct {
	body fyne.Layout
	pin  func()
}

func (l *calendarPageLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	size := l.body.MinSize(objects)
	l.pin()
	return size
}

func (l *calendarPageLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	l.body.Layout(objects, size)
	l.pin()
}

// weekHead is the buttons at the head of the week strip. Each of them redraws
// the strip, and a button built again by its own click is a new one, at rest:
// the one that was pressed is gone before it has shrunk. So they are built once
// and every draw puts the same ones back, with what they do set for that draw.
type weekHead struct{ prev, next, here, refresh *widget.Button }

var weekHeads = map[*shell.UI]*weekHead{}

func weekHeadFor(ui *shell.UI) *weekHead {
	h := weekHeads[ui]
	if h == nil {
		h = &weekHead{
			prev:    widget.NewButtonWithIcon("", theme.NavigateBackIcon(), nil),
			next:    widget.NewButtonWithIcon("", theme.NavigateNextIcon(), nil),
			here:    widget.NewButton("This week", nil),
			refresh: widget.NewButtonWithIcon("Refresh calendar", theme.ViewRefreshIcon(), nil),
		}
		weekHeads[ui] = h
	}
	return h
}

// WeekBoardsPanel keeps the week's links in a gold bubble at the foot of Log List.
func WeekBoardsPanel(ui *shell.UI, from, to string) fyne.CanvasObject {
	links := backend.WeekBoardLinks(ui.Cfg, ui.Cfg.WorklogOwner, from, to)
	copyAll := widget.NewButtonWithIcon("Copy all", theme.ContentCopyIcon(), nil)
	copyAll.OnTapped = func() {
		app := fyne.CurrentApp()
		app.Clipboard().SetContent(backend.WeekBoardMailText(ui.Cfg, ui.Cfg.WorklogOwner, from, to))
		copyAll.SetText("Copied")
		time.AfterFunc(backend.CopiedFlash, func() {
			if fyne.CurrentApp() != app {
				return
			}
			fyne.Do(func() {
				if fyne.CurrentApp() == app {
					copyAll.SetText("Copy all")
				}
			})
		})
	}
	rows := container.NewVBox()
	for _, raw := range links {
		raw := raw
		open := shell.NewHoverLink(backend.BoardLinkLabel(raw), raw)
		if link, ok := open.(*shell.HoverLink); ok {
			link.Truncation = fyne.TextTruncateEllipsis
		}
		copyBtn := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
			fyne.CurrentApp().Clipboard().SetContent(raw)
		})
		rows.Add(container.NewBorder(nil, nil, nil, boardCopyControl(copyBtn), open))
	}
	if len(links) == 0 {
		rows.Add(widget.NewLabel("Set board view URLs in Settings."))
	}
	return shell.GoldSection("Boards for this week", boardCopyControl(copyAll), rows)
}

func boardCopyControl(button *widget.Button) fyne.CanvasObject {
	row := shell.NewTappable(shell.QuietButton(button), nil)
	pill := widgets.NewHoverPillWithOpacity(0.36, 0.9)
	return shell.HoverRowsWithPill(container.NewStack(row), []*shell.Tappable{row}, pill)
}

// WeekIssuesPanel keeps its gold heading fixed above a compact bubble grid.
// arrive, if set, brings the bubbles in one after another.
func WeekIssuesPanel(ui *shell.UI, items []backend.WorklogItem, state string, arrive *shell.Arrival) fyne.CanvasObject {
	empty := container.NewVBox()
	copySummary := copySummaryButton(ui, items, state)
	switch state {
	case "loading":
		return shell.IssueSummaryPanel("By Issue", "Reading the week from GitHub\u2026", empty, copySummary)
	case "off":
		return shell.IssueSummaryPanel("By Issue", "Set a project URL in Settings to see the week by issue.", empty, copySummary)
	case "error":
		return shell.IssueSummaryPanel("By Issue", "Could not read the GitHub project.", empty, copySummary)
	}
	totals := backend.WeekByIssue(items)
	summary := fmt.Sprintf("%d min \u00b7 %s across %d issues", backend.WeekTotalMinutes(totals), backend.HoursMins(backend.WeekTotalMinutes(totals)), len(totals))
	if len(totals) == 0 {
		summary = "Nothing on the board for this week."
	}
	cards := make([]fyne.CanvasObject, 0, len(totals))
	rows, _ := ui.Store.ReadRows()
	days := map[string]bool{}
	for _, it := range items {
		days[it.Date] = true
	}
	var logged []backend.Row
	for _, row := range rows {
		if days[row["date"]] && row["pushed_at"] != "" {
			logged = append(logged, row)
		}
	}
	for _, t := range totals {
		cards = append(cards, shell.IssueSummaryCardForRows(ui.Cfg, t.URL, t.Title, fmt.Sprintf("%dm", t.Minutes), logged, t.CodeReview))
	}
	cards = arrive.Reveal(cards, shell.ArriveDelays(len(cards)), nil)
	return shell.IssueSummaryPanel("By Issue", summary, shell.IssueSummaryGrid(cards...), copySummary)
}

// ExportWeekForEmail saves the shown week as TSV without changing the clipboard.
//
// Board items only, deliberately: the mail carries links to those boards, and a
// figure in the file that the CEO cannot find on the board it links to is worse
// than one that is missing. Anything still saved here is named as outstanding
// instead, so a short week is explained rather than quietly padded.
func ExportWeekForEmail(ui *shell.UI, days []string, byDay map[string][]backend.WorklogItem, state string) {
	if len(days) == 0 {
		return
	}
	from, to := days[0], days[len(days)-1]

	switch state {
	case "loading":
		ui.Errf(backend.GhErr("Still reading the week from GitHub — try again in a moment."))
		return
	case "off":
		ui.Errf(backend.GhErr("Set a project URL in Settings before exporting a week."))
		return
	case "error":
		ui.Errf(backend.GhErr("Could not read the GitHub project, so the week would be exported short."))
		return
	}

	var items []backend.WorklogItem
	for _, d := range days {
		items = append(items, byDay[d]...)
	}
	path, n, _, err := backend.ExportWeek(
		ui.Cfg, backend.DownloadsDir(shell.DataDir()), ui.Cfg.WorklogOwner, from, to, items)
	if err != nil {
		ui.Errf(err)
		return
	}

	// Short, because the week itself is already on the tab behind this. All the
	// dialog has to say is where the file went and what was left out of it.
	msg := fmt.Sprintf("%d entries saved to:\n%s", n, path)
	if waiting := ui.DraftMinutesByDay(from, to); len(waiting) > 0 {
		mins := 0
		for _, m := range waiting {
			mins += m
		}
		// Said rather than folded in: the mail links to the boards, and a figure
		// the reader cannot find on the board it links to is worse than one that
		// is missing.
		msg += fmt.Sprintf("\n\nNot included: %s saved here and not pushed. "+
			"Push it and export again to count it.", backend.HoursMins(mins))
	}
	shell.ShowInformation(fmt.Sprintf("Week of %s to %s", from, to), msg, ui.Win)
}

// WeekColumn is one day: how full it is, who the minutes were for, and what is
// waiting on it. items are already on GitHub — they are counted and coloured
// into the bar, but they are not listed, because a day's worth of finished work
// filled the column and buried the only thing there is anything left to do to.
// weekArriveEach is how long one day takes to uncover as the week arrives:
// slower than a list's rows, so the tall columns glide down. A day's cards
// start their own wipe cardFirst after the day starts, cardArriveStep apart,
// each taking cardArriveSpan, longer than a Kanban card's, so they settle
// softly.
const (
	weekArriveEach = 640 * time.Millisecond
	cardFirst      = 200 * time.Millisecond
	cardArriveStep = 70 * time.Millisecond
	cardArriveSpan = 520 * time.Millisecond
)

// WeekColumn is one day of the week strip.
func WeekColumn(ui *shell.UI, ds string, items []backend.WorklogItem, local []backend.Row) *shell.DayColumn {
	return weekColumn(ui, ds, items, local, nil)
}

// weekColumn is WeekColumn whose cards, when enter is set, each wipe in after
// enter(their place in the day).
func weekColumn(ui *shell.UI, ds string, items []backend.WorklogItem, local []backend.Row, enter func(n int) time.Duration) *shell.DayColumn {
	t, _ := time.Parse("2006-01-02", ds)
	byOrg := shell.MinutesByOrg(items)
	mins := 0
	for _, v := range byOrg {
		mins += v
	}
	// One rule for these day cards and the saved-entry list above them. Duration
	// remains the default; creation order is useful when clearing drafts FIFO.
	SortDraftRows(local, ui.DraftSort)

	// Keep local whole for the shared totals; the draft key controls the yellow
	// minutes independently of the purple plan.
	drafts := ui.ShownDrafts(local)
	draftByOrg := map[string]int{}
	waiting := 0
	for _, r := range drafts {
		draftByOrg[strings.ToLower(shell.OrgOf(r["issue"]))] += r.Minutes()
		waiting += r.Minutes()
	}
	// The support shift pencilled in, if the placeholder is on and this day has
	// not already got the real thing. Turquoise keeps it distinct from drafts.
	placeholder := shell.SupportPlaceholderFor(ui.Cfg, shell.Hooks.SupportOn(ui, ds), ds, items)
	if placeholder > 0 {
		draftByOrg[shell.SupportPendingKey] += placeholder
		waiting += placeholder
	}
	planned := shell.Hooks.PlannedMinutes(ui, ds, items, local)

	name := fmt.Sprintf("%s %d", t.Format("Mon"), t.Day())
	if ds == shell.Today() {
		name += " · Today"
	}
	dayLabel := widget.NewLabelWithStyle(name, fyne.TextAlignLeading,
		fyne.TextStyle{Bold: ds == shell.Today()})
	dayLabel.Truncation = fyne.TextTruncateEllipsis
	var head fyne.CanvasObject = dayLabel
	if ds == shell.Today() {
		// Today's heading is gold, like its outline.
		head = container.NewThemeOverride(head, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	}
	var col *shell.DayColumn
	head = ui.DayLink(ds, dayLabel, head, func(_ fyne.CanvasObject, on bool) {
		col.DateHovered = on
		MarkDropTargets(ui)
	})
	// Time planned on the Sprint Calendar, in its purple, beside the day's name
	// as on the calendar: on the heading's line, so a day with a plan starts its
	// cards level with one without.
	if planned > 0 {
		// What the day still owes from its logged and drafted time alone, the
		// plans left out: said in the tooltip, not on the day.
		free := max(0, backend.Target-mins-waiting)
		plan := shell.WeekCaption(fmt.Sprintf("%dm planned", planned))
		plan.Color = shell.PlannerPlanColor
		// Under the pointer it lists the planned blocks; a click opens the
		// Sprint Calendar on this day's week.
		day := ds
		trigger := shell.NewTipCaption(container.New(&fitTextLayout{full: plan.Text, reserveWidth: true}, plan), "", func() { ui.ShowPlannerDate(day) })
		if shell.Hooks.PlannedLines != nil {
			if lines := shell.Hooks.PlannedLines(ui, ds, items, local); len(lines) > 0 {
				trigger.Body = shell.PlannedTipWithFree(free, lines)
			}
		}
		head = container.New(widgets.NewSidesLayout(head, trigger, 0), head, trigger)
	}
	// Left of the line is what the day actually holds and what is queued for it,
	// plans included; right of it is what those come to together — the number the
	// day would score if everything standing on it were done and pushed.
	banked := "nothing logged"
	if mins > 0 {
		banked = DayScoreLine(mins)
	}
	caption := container.New(&boundedDayCaptions{base: shell.SplitCaption{}}, shell.WeekCaption(banked), shell.WeekCaption(""))
	if queuedMins := waiting + planned; queuedMins > 0 {
		queued := fmt.Sprintf("+%d min", queuedMins)
		if placeholder > 0 {
			queued += " (inc. support)"
		}
		score, added := shell.WeekCaption(banked), shell.WeekCaption(queued)
		score.Hide()
		added.Hide()
		caption = container.New(&boundedDayCaptions{base: &dayCaptionLayout{}}, shell.WeekCaption(banked+" · "+queued),
			shell.WeekCaption(DayScoreLine(mins+queuedMins)), score, added)
	}
	baseFill := shell.DayFill(mins, waiting+planned)
	headingFill := canvas.NewRectangle(weekdayHeadingFill(baseFill, mins >= backend.Target))
	headingFill.CornerRadius = shell.CellCornerRadius
	progress := ui.ProgressBarParts("strip/"+t.Format("Mon"), ds, shell.PlannedBarParts(ui.Cfg, byOrg, draftByOrg, planned), 8)
	pad := theme.Padding()
	headingContent := container.New(&dayHeadingLayout{}, head, progress, caption)
	headingInset := container.New(layout.NewCustomPaddedLayout(0, 0, pad, pad), headingContent)
	heading := shell.NewTappable(container.NewStack(headingFill, headingInset), nil)
	heading.Look = desktop.DefaultCursor

	// What is still waiting is the card itself, standing on the day it will be
	// pushed with — every one of them, however long that makes the column. The
	// list above is for the entries that belong to some other week.
	dup := duplicateRows(drafts)
	var body []fyne.CanvasObject
	for _, r := range drafts {
		refresh := func() { DrawRecent(ui) }
		area := dayRowCard(ui, r, refresh, dup[r["id"]])
		area.SetHoverParent(func(bool) {
			if ui.DragRow == nil {
				MarkDropTargets(ui)
			}
		})
		card := newDragTile(ui, r, area)
		// A click opens the entry in the editor Saved locally opens it in.
		card.OnTap = func() { openRowEditor(ui, r, refresh) }
		if enter != nil {
			body = append(body, shell.NewWipeFor(card, enter(len(body)), cardArriveSpan))
			continue
		}
		body = append(body, card)
	}

	// The frame is kept to hand so a drop target can be lit without rebuilding
	// the column under the pointer that is aiming at it.
	frame := canvas.NewRectangle(color.Transparent)
	frame.CornerRadius = shell.CellCornerRadius
	dateHover := canvas.NewRectangle(color.Transparent)
	dateHover.StrokeColor = color.Transparent
	dateHover.CornerRadius = shell.CellCornerRadius
	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(0, backend.WeekColMinHeight))

	// The outline drawn again over the cards while the heading is pinned,
	// between them and the heading.
	edge := canvas.NewRectangle(color.Transparent)
	edge.Hide()
	cover := canvas.NewRectangle(color.Transparent)
	cover.Hide()
	clip := canvas.NewRectangle(color.Transparent)
	clip.Hide()
	colLayout := &dayColumnLayout{}
	contents := container.New(weekdayHeadingLayout{body: layout.NewVBoxLayout(), cols: colLayout},
		append(body, cover, clip, edge, heading)...)
	col = newDayColumn(ui, ds, container.New(colLayout, floor, frame, dateHover,
		container.NewPadded(contents)), frame)
	colLayout.col = col
	col.Edge, col.Cover, col.Clip = edge, cover, clip
	shell.FollowFrame(frame, col.PaintEdge)
	col.Heading = heading
	col.HeadingContent = headingContent
	col.HeadingFill = headingFill
	col.DateHoverFrame = dateHover
	col.Mins, col.Waiting = mins, waiting
	// The day turns green once logged + drafts + planned reach the target, as
	// on the Sprint Calendar. Waiting stays drafts and support only, the minutes
	// a drop or a push can move; Planned carries the plans.
	col.Planned = planned
	col.BaseFill = baseFill
	frame.FillColor = col.BaseFill
	frame.StrokeColor, frame.StrokeWidth = dayEdge(ds)
	return col
}

// DayScoreLine is one day's minutes against the target: what it holds, what
// share of the day that is, and what it still owes.
func DayScoreLine(mins int) string {
	s := fmt.Sprintf("%d min · %d%%", mins, shell.PercentOf(mins))
	if mins >= backend.Target {
		return s + " ✓"
	}
	return s + fmt.Sprintf(" (%dm left)", backend.Target-mins)
}

// DayRowCard is a saved entry standing on the day it will be pushed with. It
// carries the same three actions in the same row as the tile in the list, so
// the two read as the same card wherever it happens to be standing.
func DayRowCard(ui *shell.UI, r backend.Row, refresh func()) fyne.CanvasObject {
	return dayRowCard(ui, r, refresh, false)
}

// dayRowCard is DayRowCard, tagged when another entry on the same day goes to
// the same issue (or is another bulk review).
func dayRowCard(ui *shell.UI, r backend.Row, refresh func(), duplicate bool) *shell.HoverArea {
	accent := shell.RowAccentColor(ui.Cfg, r)
	title := widget.NewLabelWithStyle(weekRowTitle(ui, r),
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	// One line, so every card keeps the same height.
	title.Truncation = fyne.TextTruncateEllipsis

	// The minutes are the number this card exists to carry — how much of the day
	// it would take — so they are said out loud in the org's own colour rather
	// than folded into a grey caption with everything else.
	minsText := canvas.NewText(fmt.Sprintf("%d min", r.Minutes()), accent)
	minsText.TextStyle = fyne.TextStyle{Bold: true}
	var mins fyne.CanvasObject = minsText
	if tag := minutesTag(ui, r); tag != nil {
		full := fyne.MeasureText(minsText.Text, theme.TextSize(), minsText.TextStyle).Width + minutesTagGap()
		mins = container.New(&RefTagLayout{Full: full}, minsText, tag)
	}
	// Every card keeps room for a tag here, so a tagged card is no taller.
	mins = container.NewStack(shell.VGap(shell.CodeReviewTag().MinSize().Height), mins)

	fallback := "no issue ref"
	if backend.RowIsBulkCodeReview(r) {
		fallback = ""
	}
	whereLine := savedReferenceLine(r, fallback)
	// Reserve the same metadata line for bare issues, reviews and ref-less drafts.
	whereLine = container.NewStack(shell.VGap(widget.NewLabel("").MinSize().Height), whereLine)

	// A click on the card opens it in the editor (see DragTile.OnTap), so it
	// carries no Edit; its Delete icon shows only while the card is lit,
	// fading in and out with the light.
	push, _, _ := rowActions(ui, r, refresh)
	// The layout keeps Push's place first; off the entry's day it is empty.
	actionItems := []fyne.CanvasObject{shell.VGap(0)}
	if push != nil {
		actionItems[0] = shell.CompactGitHubHoverButton(push)
	}
	actionItems = append(actionItems, rowTags(ui, r, duplicate, true)...)
	// The Delete icon's own hover fades as gently as the icon itself.
	del := shell.NewFadeIconButton(theme.DeleteIcon(), func() { ConfirmDelete(ui, r, refresh) })
	fade := shell.NewFader(del)
	actionItems = append(actionItems, fade.View)
	body := container.NewVBox(title, mins, whereLine, container.New(&cardActionsLayout{}, actionItems...))

	area := shell.HoverBubble(accent, body)
	del.SetHoverParent(area.Bubble().ChildHover) // the card stays lit while on its Delete
	area.Bubble().OnLight = fade.FadeTo
	return area
}

// RefTagLayout stands a tag right after a reference. The objects are the
// reference and the tag. The reference takes Full, the width of its whole text,
// and wraps when the row has less. Given under half of that it would break into
// a tall sliver, so the tag drops below it instead.
type RefTagLayout = widgets.RefTagLayout

// weekRowTitle names a locally saved row the same way its tile does.
func weekRowTitle(ui *shell.UI, r backend.Row) string {
	title := RowTitle(ui, r)
	if title == r["issue"] {
		return backend.OrDefault(shell.ShortIssueRef(title), title)
	}
	return title
}

func newDayColumn(ui *shell.UI, date string, body fyne.CanvasObject, frame *canvas.Rectangle) *shell.DayColumn {
	c := &shell.DayColumn{Ui: ui, Date: date, Body: body, Frame: frame}
	c.ExtendBaseWidget(c)
	return c
}

// MarkDropTargets updates duplicate outlines and previews the score of a drop.
func MarkDropTargets(ui *shell.UI) {
	matchedDays := markWeekMatches(ui)
	for ds, col := range ui.WeekCols {
		stroke, width := dayEdge(ds)
		if ink := paintDateHover(col, ui.DragRow != nil); ink != nil {
			stroke, width = ink, 2
		}
		if ui.DragRow != nil && ds == ui.HoverDay {
			adding := 0
			if ui.DragRow["date"] != ds {
				adding = ui.DragRow.Minutes()
			}
			stroke, width = widgets.LogoGold, 2
			if col.MeetsTarget(adding) {
				stroke = theme.Color(theme.ColorNameSuccess)
			}
			col.StartPulse(adding)
		} else {
			col.StopPulse()
		}
		if matchedDays[ds] {
			stroke, width = widgets.LogoGold, 2
		}
		shell.FadeEdge(col.Frame, stroke, width)
	}
}

// HeadingFoot is the space under the tallest weekday heading's caption.
func HeadingFoot() float32 { return theme.InnerPadding() }

// dayHeadingLayout stacks a weekday heading: the day's name, the bar, then
// the caption, each a small gap under the last, the same on every day of the
// week. The caption is measured at the heading's width, so captions that sit
// side by side on one line take one line. Headings share the tallest one's
// height, so a shorter day keeps the spare room at its foot. Objects: name,
// bar, caption.
type dayHeadingLayout struct{ width float32 }

func (l *dayHeadingLayout) captionHeight(caption fyne.CanvasObject) float32 {
	if c, ok := caption.(*fyne.Container); ok && l.width > 0 {
		if m, ok := c.Layout.(interface {
			heightAt([]fyne.CanvasObject, float32) float32
		}); ok {
			return m.heightAt(c.Objects, l.width)
		}
	}
	return caption.MinSize().Height
}

func (l *dayHeadingLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	w := float32(0)
	for _, o := range objects {
		w = max(w, o.MinSize().Width)
	}
	h := objects[0].MinSize().Height + objects[1].MinSize().Height + l.captionHeight(objects[2])
	return fyne.NewSize(w, h+headingGap(0)+headingGap(1)+HeadingFoot())
}

// headingGap is the space under a heading's i-th line: a small one under the
// day's name, a little more under the bar so the caption does not crowd it.
func headingGap(i int) float32 {
	if i == 1 {
		return theme.Padding() * 1.5
	}
	return theme.Padding() / 2
}

func (l *dayHeadingLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	l.width = size.Width
	y := float32(0)
	for i, o := range objects {
		h := o.MinSize().Height
		if i == 2 {
			h = l.captionHeight(o)
		}
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, h))
		y += h + headingGap(i)
	}
}

// weekdayHeadingFill is the heading's surface: lighter than an empty day; on
// a green day, greener still while drafts fill it, and a shade darker than
// the day once the minutes are logged, so a finished day stays calm.
func weekdayHeadingFill(base color.Color, done bool) color.Color {
	if _, _, _, alpha := base.RGBA(); alpha == 0 {
		return shell.BlendColor(theme.Color(theme.ColorNameBackground), color.White, 0.12)
	}
	if !done {
		return shell.BlendColor(base, theme.Color(theme.ColorNameSuccess), 0.22)
	}
	return shell.BlendColor(base, color.Black, HeadingShade)
}

// HeadingShade is how much darker than its logged day a heading is.
const HeadingShade = 0.3

func paintDateHover(col *shell.DayColumn, dragging bool) color.Color {
	base := weekdayHeadingFill(col.BaseFill, col.Mins >= backend.Target)
	if col.DateHovered && !dragging {
		ink, tint, alpha := color.Color(widgets.LogoGold), color.Color(widgets.LogoGold), float32(0.16)
		if col.MeetsTarget(0) {
			ink = theme.Color(theme.ColorNameSuccess)
			tint, alpha = ink, 0.22
		}
		shell.FadeFrame(col.DateHoverFrame, widgets.WithAlpha(tint, alpha), color.Transparent, 0)
		shell.FadeFrame(col.HeadingFill, shell.BlendColor(base, tint, alpha), color.Transparent, 0)
		return ink
	}
	shell.FadeFrame(col.DateHoverFrame, color.Transparent, color.Transparent, 0)
	shell.FadeFrame(col.HeadingFill, base, color.Transparent, 0)
	return nil
}

// markWeekMatches lights every other card for the same issue or bulk review.
// The dragged row takes precedence while the pointer passes over other cards.
func markWeekMatches(ui *shell.UI) map[string]bool {
	var cards []*DragTile
	for _, col := range ui.WeekCols {
		cards = append(cards, dayCards(col.Body)...)
	}
	source := ui.DragRow
	if source == nil {
		for _, card := range cards {
			if area, ok := card.Content.(*shell.HoverArea); ok && area.Hovered() {
				source = card.Row
				break
			}
		}
	}
	place := rowPlace(source)
	matchedDays := map[string]bool{}
	for _, card := range cards {
		area, ok := card.Content.(*shell.HoverArea)
		if !ok {
			continue
		}
		matched := place != "" && card.Row["id"] != source["id"] && rowPlace(card.Row) == place
		area.Bubble().SetMatch(matched)
		if matched {
			matchedDays[card.Row["date"]] = true
		}
	}
	return matchedDays
}

// dayCards are the saved entries standing in a day's column.
func dayCards(obj fyne.CanvasObject) []*DragTile {
	switch o := obj.(type) {
	case *DragTile:
		return []*DragTile{o}
	case *shell.Wipe:
		return dayCards(o.Content)
	case *fyne.Container:
		var cards []*DragTile
		for _, child := range o.Objects {
			cards = append(cards, dayCards(child)...)
		}
		return cards
	}
	return nil
}

// ---- the card in the air ----

// The card being carried is drawn in a layer of its own above the whole tab,
// rather than by moving the card itself. A moved card is still a child of the
// day it came from, so dragging it rightwards slid it *behind* the column it was
// aimed at — painted over by a day drawn after its own.

func showGhost(ui *shell.UI, r backend.Row) {
	if ui.DragLayer == nil {
		return
	}
	ghost := shell.DragGhost(weekRowTitle(ui, r), r.Minutes(), shell.OrgColor(ui.Cfg, shell.OrgOf(r["issue"])))
	ui.DragGhost = ghost
	ui.DragLayer.Objects = []fyne.CanvasObject{ghost}
	ui.DragLayer.Refresh()
}

// moveGhost puts the carried card under the pointer. The position is canvas-wide
// and the layer is not, so it is taken back to the layer's own origin.
func moveGhost(ui *shell.UI, at fyne.Position) {
	if ui.DragGhost == nil || ui.DragLayer == nil {
		return
	}
	origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(ui.DragLayer)
	// Below and right of the pointer, so the card never covers the day it is
	// being aimed at.
	ui.DragGhost.Move(at.Subtract(origin).Add(fyne.NewPos(12, 12)))
}

func hideGhost(ui *shell.UI) {
	if ui.DragLayer == nil {
		return
	}
	ui.DragGhost = nil
	ui.DragLayer.Objects = nil
	ui.DragLayer.Refresh()
}

// moveRowToDay is the drop: the row keeps everything else and changes its date,
// which is the date the Push button will send.
func moveRowToDay(ui *shell.UI, r backend.Row, date string) {
	if r["id"] == "" || date == "" || date == r["date"] {
		return
	}
	if err := ui.Store.UpdateRow(r["id"], backend.Row{"date": date}); err != nil {
		ui.Errf(err)
		return
	}
	DrawRecent(ui) // redraws the strip too, so the card appears on its new day
}

// ---- the card being dragged ----

// DragTile wraps a saved entry so it can be picked up and dropped on a day. Only
// unpushed entries are wrapped: a pushed one is already a fact on GitHub, and
// moving it here would only disagree with the board.
type DragTile struct {
	widget.BaseWidget
	ui      *shell.UI
	Row     backend.Row
	Content fyne.CanvasObject
	lifted  bool
	// OnTap, set before the tile is first shown, runs on a click, and makes
	// the tile press as a Kanban card does.
	OnTap func()
	press *shell.CardPress
	held  bool // the button is down on it
}

func newDragTile(ui *shell.UI, r backend.Row, content fyne.CanvasObject) *DragTile {
	t := &DragTile{ui: ui, Row: r, Content: content}
	t.ExtendBaseWidget(t)
	return t
}

func (t *DragTile) CreateRenderer() fyne.WidgetRenderer {
	inner := widget.NewSimpleRenderer(t.Content)
	if t.OnTap == nil {
		return inner
	}
	if t.press == nil {
		t.press = shell.NewCardPress(t, func() fyne.CanvasObject { return t.Content })
	}
	return t.press.Wrap(inner)
}

// Cursor is the pointing hand, and the grabbing hand while it is held.
func (t *DragTile) Cursor() desktop.Cursor { return shell.CardCursor(t.held || t.lifted) }

func (t *DragTile) MouseDown(e *desktop.MouseEvent) { t.held = true; t.press.Down(e) }
func (t *DragTile) MouseUp(*desktop.MouseEvent)     { t.PressReleased() }
func (t *DragTile) PressReleased()                  { t.held = false; t.press.Up() }

func (t *DragTile) Tapped(*fyne.PointEvent) {
	if t.OnTap == nil {
		return
	}
	t.press.Play()
	t.OnTap()
}

func (t *DragTile) Dragged(e *fyne.DragEvent) {
	if !t.lifted {
		t.press.Cancel() // the ghost carries it now
		t.lifted = true
		t.ui.DragRow = t.Row
		showGhost(t.ui, t.Row)
		shell.HoldBubble(t.Content)
		MarkDropTargets(t.ui)
	}
	moveGhost(t.ui, e.AbsolutePosition)
	// The day is found from the pointer, not from hovering: the cards and
	// links inside a day take the hover, so the day alone heard only the
	// empty space around them. Fyne sends the drag after the hover events of
	// the same move, so this has the last word.
	if day, measured := dayUnder(t.ui, e.AbsolutePosition); measured && day != t.ui.HoverDay {
		t.ui.HoverDay = day
		MarkDropTargets(t.ui)
	}
}

// dayUnder is the day in the strip at the canvas position at, and whether
// the strip is laid out at all (it is not in tests that never show it).
func dayUnder(ui *shell.UI, at fyne.Position) (string, bool) {
	driver := fyne.CurrentApp().Driver()
	measured := false
	for ds, col := range ui.WeekCols {
		size := col.Size()
		if size.IsZero() || !col.Visible() {
			continue
		}
		measured = true
		p := driver.AbsolutePositionForObject(col)
		if at.X >= p.X && at.X < p.X+size.Width && at.Y >= p.Y && at.Y < p.Y+size.Height {
			return ds, true
		}
	}
	return "", measured
}

func (t *DragTile) DragEnd() {
	if !t.lifted {
		return
	}
	t.lifted, t.held = false, false
	shell.ReleaseBubble()
	hideGhost(t.ui)

	day := t.ui.HoverDay
	t.ui.DragRow = nil
	t.ui.HoverDay = ""
	MarkDropTargets(t.ui)
	// Dropped on nothing, or on the day it already sits on: the card just goes
	// back where it came from.
	moveRowToDay(t.ui, t.Row, day)
}

// weekSupportControl is the "+180 min weekday support" checkbox, swapped for a
// bold gold "Support Week" when the rota puts the user on support that week
// (the +180 then goes on by itself, so there is nothing to tick). It is built
// once and kept, like Log Work's other checkboxes, and only re-synced to the
// week on each redraw. A fresh checkbox per redraw drew fine but never got the
// mouse, so it could not be ticked.
//
// Display only, and it says so: nothing is pushed. The tick is remembered for
// its own week (support-weeks.json) until that week's Friday passes.
func weekSupportControl(ui *shell.UI, rota bool) fyne.CanvasObject {
	on := shell.Hooks.SupportOn(ui, ui.WeekStart)
	if ui.WeekSupport == nil {
		ui.WeekSupport = widget.NewCheck(shell.SupportCheckText(false), nil)
		// Seeded before the handler is attached: SetChecked fires OnChanged.
		ui.WeekSupport.SetChecked(on)
		ui.WeekSupport.OnChanged = func(on bool) {
			shell.Hooks.SetSupport(ui, on, ui.WeekStart)
			DrawWeekStrip(ui)
		}
		ui.WeekSupportView = shell.SupportChoice(ui.WeekSupport, rota)
		return ui.WeekSupportView.View
	}
	ui.WeekSupportView.Set(rota)
	if ui.WeekSupport.Checked != on {
		// Re-sync quietly: walking to another week must not tick or untick it.
		changed := ui.WeekSupport.OnChanged
		ui.WeekSupport.OnChanged = nil
		ui.WeekSupport.SetChecked(on)
		ui.WeekSupport.OnChanged = changed
	}
	return ui.WeekSupportView.View
}

// dayEdge is the resting outline, with today's date marked in gold.
func dayEdge(date string) (color.Color, float32) {
	if date == shell.Today() {
		return widgets.LogoGold, 2
	}
	return theme.Color(theme.ColorNameInputBorder), 1
}

// rowTags are the gold tags a saved entry carries: pushed only in part,
// commits on its issue still to log (on a day card), and another entry the
// same day going to the same place.
func rowTags(ui *shell.UI, r backend.Row, duplicate, pending bool) []fyne.CanvasObject {
	var tags []fyne.CanvasObject
	if shell.RowPartlySaved(r) {
		tags = append(tags, shell.SavedTag("Partially pushed"))
	}
	if pending && ui.IssueHasPending(r["issue"]) {
		tags = append(tags, shell.SavedTag("Commits remaining"))
	}
	if duplicate {
		tags = append(tags, shell.SavedTag("Duplicates same day"))
	}
	return tags
}

// rowPlace is where an entry goes: its issue, or the day's bulk review. Two
// entries the same day going to one place are duplicates.
func rowPlace(r backend.Row) string {
	switch {
	case r["type"] == backend.KindBulkReview || backend.RowIsBulkCodeReview(r):
		return "bulk review"
	case r["issue"] != "":
		return "issue " + strings.ToLower(r["issue"])
	}
	return ""
}

// duplicateRows marks, by row ID, the entries that share a day with another
// going to the same place: the same issue, or the day's bulk review.
func duplicateRows(rows []backend.Row) map[string]bool {
	byPlace := map[string][]string{}
	for _, r := range rows {
		if p := rowPlace(r); p != "" {
			key := r["date"] + "|" + p
			byPlace[key] = append(byPlace[key], r["id"])
		}
	}
	dup := map[string]bool{}
	for _, ids := range byPlace {
		if len(ids) > 1 {
			for _, id := range ids {
				dup[id] = true
			}
		}
	}
	return dup
}
