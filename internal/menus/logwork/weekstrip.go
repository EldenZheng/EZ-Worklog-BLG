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
	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		walkTo(shell.ShiftWeek(ui.WeekStart, -1))
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		walkTo(shell.ShiftWeek(ui.WeekStart, 1))
	})
	here := widget.NewButton("This week", func() { walkTo(backend.WeekStartOf(shell.Today())) })
	if ui.WeekStart == backend.WeekStartOf(shell.Today()) {
		here.Disable()
	}
	title := widget.NewLabelWithStyle(shell.WeekRangeLabel(ui.WeekStart),
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	note := ""
	switch state {
	case "loading":
		note = "checking GitHub…"
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
	// The same export as Log List, on the tab the week is actually worked from:
	// having to change tabs to send the week you are looking at is a step for
	// nothing. Both buttons run the same code on the same shared week.
	mail := widget.NewButtonWithIcon("Export week for email", theme.MailComposeIcon(), func() {
		ExportWeekForEmail(ui, days, byDay, state)
	})

	// Refresh reloads the project data behind the week strip — the boards, the
	// day fills, the score line — and only that. The commit sweep has its own
	// refresh in the pane above, and a second machine's push shows up on the
	// board without waiting for the commit list to be rescanned.
	refresh := widget.NewButtonWithIcon("Refresh calendar", theme.ViewRefreshIcon(), func() {
		ui.RefreshTodayScore()
	})

	headLeft := container.NewHBox(prev, title, next, here)
	headRight := container.NewHBox(weekSupportControl(ui, rota), mail, refresh,
		widget.NewLabelWithStyle(note, fyne.TextAlignTrailing, fyne.TextStyle{Italic: true}))
	head := container.New(widgets.NewSidesLayout(headLeft, headRight, ui.WeekBox.Size().Width), headLeft, headRight)

	// Any column about to be thrown away stops breathing first: an animation left
	// running would keep refreshing a rectangle nothing is showing any more.
	for _, col := range ui.WeekCols {
		col.StopPulse()
	}
	ui.WeekCols = map[string]*shell.DayColumn{}
	var dayCols []fyne.CanvasObject
	for _, ds := range days {
		col := WeekColumn(ui, ds, byDay[ds], localByDay[ds])
		ui.WeekCols[ds] = col
		dayCols = append(dayCols, col)
	}
	// A week shown for the first time arrives as Kanban's cards do: each day
	// uncovered from the top down, one after another left to right, its bar
	// filling in as it comes. Any other redraw leaves the days be. A week still
	// loading arrives when its data lands.
	if state != "loading" && !ui.WeekFetching(days) {
		dayCols = ui.Arrival("week", ui.WeekStart).Reveal(dayCols, shell.ArriveDelays(len(dayCols)), func(i int, share float32) {
			t, _ := time.Parse("2006-01-02", days[i])
			ui.MeterShare("strip/"+t.Format("Mon"), share)
		})
	}
	grid := container.New(&WeekGrid{}, dayCols...)

	objs := []fyne.CanvasObject{head, ui.WeekStripProgress.View, grid}
	// While GitHub is still being read the days would show only the drafts,
	// and then jump: the head and its loading strip stand alone until the week
	// lands.
	if state == "loading" {
		objs = objs[:2]
	}

	ui.WeekBox.Objects = objs
	ui.WeekBox.Refresh()

	// The boards and the week read by issue live on Log List now. They follow the
	// same week as this strip, so walking the arrows here moves both.
	shell.Hooks.DrawLogList(ui)
}

// WeekBoardsPanel keeps the week's links in a gold bubble at the foot of Log List.
func WeekBoardsPanel(ui *shell.UI, from, to string) fyne.CanvasObject {
	links := backend.WeekBoardLinks(ui.Cfg, ui.Cfg.WorklogOwner, from, to)
	copyAll := shell.NewHoverButton("Copy all", theme.ContentCopyIcon(), nil)
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
		copyBtn := shell.NewHoverButton("", theme.ContentCopyIcon(), func() {
			fyne.CurrentApp().Clipboard().SetContent(raw)
		})
		shell.QuietButton(&copyBtn.Button)
		rows.Add(container.NewBorder(nil, nil, nil, copyBtn, open))
	}
	if len(links) == 0 {
		rows.Add(widget.NewLabel("Set board view URLs in Settings."))
	}
	return shell.GoldSection("Boards for this week", copyAll, rows)
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
	for _, t := range totals {
		cards = append(cards, shell.IssueSummaryCardForRows(ui.Cfg, t.URL, t.Title, fmt.Sprintf("%dm", t.Minutes), rows))
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
func WeekColumn(ui *shell.UI, ds string, items []backend.WorklogItem, local []backend.Row) *shell.DayColumn {
	t, _ := time.Parse("2006-01-02", ds)
	byOrg := shell.MinutesByOrg(items)
	mins := 0
	for _, v := range byOrg {
		mins += v
	}
	// One rule for these day cards and the saved-entry list above them. Duration
	// remains the default; creation order is useful when clearing drafts FIFO.
	SortDraftRows(local, ui.DraftSort)

	draftByOrg := map[string]int{}
	waiting := 0
	for _, r := range local {
		draftByOrg[strings.ToLower(shell.OrgOf(r["issue"]))] += r.Minutes()
		waiting += r.Minutes()
	}
	// The support shift pencilled in, if the placeholder is on and this day has
	// not already got the real thing. It rides in the same yellow as the drafts,
	// which is the right colour for it: neither is on the board.
	placeholder := shell.SupportPlaceholderFor(ui.Cfg, shell.Hooks.SupportOn(ui, ds), ds, items)
	if placeholder > 0 {
		draftByOrg[strings.ToLower(shell.OrgOf(ui.Cfg.WorklogOwner))] += placeholder
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
	// Time planned on the Sprint Calendar, in its purple, beside the day's name
	// as on the calendar: on the heading's line, so a day with a plan starts its
	// cards level with one without.
	if planned > 0 {
		// What the day still has free once its logged, drafted and planned time
		// are counted: said in the tooltip, not on the day.
		free := max(0, backend.Target-mins-waiting-planned)
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
	body := []fyne.CanvasObject{
		head,
		ui.ProgressBarParts("strip/"+t.Format("Mon"), ds, shell.PlannedBarParts(ui.Cfg, byOrg, draftByOrg, planned), 8),
		caption,
	}

	// What is still waiting is the card itself, standing on the day it will be
	// pushed with — every one of them, however long that makes the column. The
	// list above is for the entries that belong to some other week.
	for _, r := range local {
		body = append(body, newDragTile(ui, r, DayRowCard(ui, r, func() { DrawRecent(ui) })))
	}

	// The frame is kept to hand so a drop target can be lit without rebuilding
	// the column under the pointer that is aiming at it.
	frame := canvas.NewRectangle(color.Transparent)
	frame.StrokeColor, frame.StrokeWidth = dayEdge(ds)
	frame.CornerRadius = shell.CellCornerRadius
	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(0, backend.WeekColMinHeight))

	col := newDayColumn(ui, ds, container.NewStack(floor, frame,
		container.NewPadded(container.NewVBox(body...))), frame)
	col.Mins, col.Waiting = mins, waiting
	// The day turns green once logged + drafts + planned reach the target, as
	// on the Sprint Calendar. Waiting stays drafts and support only, the minutes
	// a drop or a push can move; Planned carries the plans.
	col.Planned = planned
	col.BaseFill = shell.DayFill(mins, waiting+col.Planned)
	frame.FillColor = col.BaseFill
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
	accent := shell.RowAccentColor(ui.Cfg, r)
	// One line, elided at the width the column actually has.
	//
	// It used to be cut at a character count and then word-wrapped, and a count
	// is not a width: forty-four narrow characters fit on one line while
	// forty-four wide ones need two. Cards on the same row came out different
	// heights for no reason a reader could see, and the columns stopped lining
	// up. Letting fyne elide at the real width makes every card the same height
	// whatever its title says.
	title := widget.NewLabelWithStyle(weekRowTitle(ui, r),
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Wrapping = fyne.TextWrapOff
	title.Truncation = fyne.TextTruncateEllipsis

	// The minutes are the number this card exists to carry — how much of the day
	// it would take — so they are said out loud in the org's own colour rather
	// than folded into a grey caption with everything else.
	mins := canvas.NewText(fmt.Sprintf("%d min", r.Minutes()), accent)
	mins.TextStyle = fyne.TextStyle{Bold: true}

	// The issue ref wraps instead of being cut: two repos in the same org can
	// share their first twenty characters, and an ellipsis there says nothing
	// about which one this is. Broken mid-word, since a ref has no spaces to
	// break on.
	where := widget.NewLabel("no issue ref")
	if r["issue"] != "" {
		where = widget.NewLabel(backend.OrDefault(shell.ShortIssueRef(r["issue"]), r["issue"]))
	}
	where.Wrapping = fyne.TextWrapBreak

	push, edit, del := rowActions(ui, r, refresh)
	actionItems := []fyne.CanvasObject{shell.CompactGitHubHoverButton(push)}
	if shell.RowPartlySaved(r) {
		actionItems = append(actionItems, shell.SavedTag("partly saved"))
	}
	if ui.IssueHasPending(r["issue"]) {
		actionItems = append(actionItems, shell.SavedTag("commits remaining"))
	}
	actionItems = append(actionItems, container.NewHBox(widgets.WithPointerCursor(edit), widgets.WithPointerCursor(del)))
	body := container.NewVBox(title, mins, where, container.New(&cardActionsLayout{}, actionItems...))

	return shell.OrgBubble(accent, body)
}

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

// MarkDropTargets lights the day the pointer is over, and only while a card is
// actually being dragged — otherwise every passing mouse would look like a drop.
func MarkDropTargets(ui *shell.UI) {
	for ds, col := range ui.WeekCols {
		if ui.DragRow != nil && ds == ui.HoverDay {
			col.Frame.StrokeColor = theme.Color(theme.ColorNamePrimary)
			col.Frame.StrokeWidth = 2
			col.Frame.Refresh()
			col.StartPulse(ui.DragRow.Minutes())
			continue
		}
		col.StopPulse()
		stroke, width := dayEdge(ds)
		if col.Frame.StrokeColor == stroke && col.Frame.StrokeWidth == width {
			continue
		}
		col.Frame.StrokeColor, col.Frame.StrokeWidth = stroke, width
		col.Frame.Refresh()
	}
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
}

func newDragTile(ui *shell.UI, r backend.Row, content fyne.CanvasObject) *DragTile {
	t := &DragTile{ui: ui, Row: r, Content: content}
	t.ExtendBaseWidget(t)
	return t
}

func (t *DragTile) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.Content)
}

func (t *DragTile) Cursor() desktop.Cursor { return widgets.GrabCursor{} }

func (t *DragTile) Dragged(e *fyne.DragEvent) {
	if !t.lifted {
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
	t.lifted = false
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

// dayEdge is a day's outline at rest: today edged in gold, as on the Sprint
// Calendar, every other day in the plain input border.
func dayEdge(date string) (color.Color, float32) {
	if date == shell.Today() {
		return widgets.LogoGold, 2
	}
	return theme.Color(theme.ColorNameInputBorder), 1
}
