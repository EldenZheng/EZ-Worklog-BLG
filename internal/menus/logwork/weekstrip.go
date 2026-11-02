package logwork

import (
	"fmt"
	"image/color"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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
	waiting := 0
	if rows, err := ui.Store.ReadRows(); err == nil {
		for _, r := range ui.ShownRows(rows) {
			if !ui.RowMatchesSearch(r) {
				continue
			}
			if r["pushed_at"] != "" {
				continue
			}
			waiting++
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

	head := container.NewHBox(prev, title, next, here, layout.NewSpacer(), weekSupportControl(ui), mail, refresh,
		widget.NewLabelWithStyle(note, fyne.TextAlignTrailing, fyne.TextStyle{Italic: true}))

	// Any column about to be thrown away stops breathing first: an animation left
	// running would keep refreshing a rectangle nothing is showing any more.
	for _, col := range ui.WeekCols {
		col.StopPulse()
	}
	ui.WeekCols = map[string]*shell.DayColumn{}
	grid := container.NewGridWithColumns(7)
	for _, ds := range days {
		col := WeekColumn(ui, ds, byDay[ds], localByDay[ds])
		ui.WeekCols[ds] = col
		grid.Add(col)
	}

	objs := []fyne.CanvasObject{head, ui.WeekStripProgress.View, grid}
	if waiting > 0 {
		objs = append(objs, widget.NewLabelWithStyle(
			"Drag a card onto another day to move it — the date it is pushed with follows the drop.",
			fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
	}

	ui.WeekBox.Objects = objs
	ui.WeekBox.Refresh()

	// The boards and the week read by issue live on Log List now. They follow the
	// same week as this strip, so walking the arrows here moves both.
	shell.Hooks.DrawLogList(ui)
}

// WeekBoardsPanel is the two saved board views, filtered to the shown week.
//
// Each one is a link to click and a button to copy, because those are two
// different jobs: opening it here to check the week, and pasting the URL into a
// mail. A hyperlink alone can only do the first — the text it shows is a label,
// not the address, so there is nothing to select.
func WeekBoardsPanel(ui *shell.UI, from, to string) fyne.CanvasObject {
	rows := []fyne.CanvasObject{shell.Bold("Boards for this week")}
	for _, raw := range backend.WeekBoardLinks(ui.Cfg, ui.Cfg.WorklogOwner, from, to) {
		raw := raw
		var open fyne.CanvasObject = widget.NewLabel(raw)
		if u, err := url.Parse(raw); err == nil {
			link := widget.NewHyperlink(backend.BoardLinkLabel(raw), u)
			link.Truncation = fyne.TextTruncateEllipsis
			open = link
		}
		var copyBtn *widget.Button
		copyBtn = widget.NewButtonWithIcon("Copy link", theme.ContentCopyIcon(), func() {
			fyne.CurrentApp().Clipboard().SetContent(raw)
			// The clipboard gives no sign it took anything, so the button says
			// so itself and puts its own label back.
			copyBtn.SetText("Copied")
			time.AfterFunc(backend.CopiedFlash, func() {
				fyne.Do(func() { copyBtn.SetText("Copy link") })
			})
		})
		copyBtn.Importance = widget.LowImportance
		rows = append(rows, container.NewBorder(nil, nil, nil, copyBtn, open))
	}
	return container.NewVBox(rows...)
}

// WeekIssuesPanel is what the week was spent on: one line per issue, its
// minutes, and a link to it. This is the list the weekly mail is written from,
// so it is worth being able to read it without exporting anything.
func WeekIssuesPanel(ui *shell.UI, items []backend.WorklogItem, state string) fyne.CanvasObject {
	head := shell.Bold("By issue")
	switch state {
	case "loading":
		return container.NewVBox(head, widget.NewLabel("Reading the week from GitHub…"))
	case "off":
		return container.NewVBox(head,
			widget.NewLabel("Set a project URL in Settings to see the week by issue."))
	case "error":
		return container.NewVBox(head,
			shell.ColorLabel("Could not read the GitHub project.", theme.ColorNameError))
	}

	totals := backend.WeekByIssue(items)
	total := backend.WeekTotalMinutes(totals)
	rows := []fyne.CanvasObject{
		container.NewHBox(head, widget.NewLabelWithStyle(
			fmt.Sprintf("%d min · %s across %d issues", total, backend.HoursMins(total), len(totals)),
			fyne.TextAlignLeading, fyne.TextStyle{Italic: true})),
	}
	if len(totals) == 0 {
		rows = append(rows, widget.NewLabel("Nothing on the board for this week."))
	}
	for _, t := range totals {
		// The minutes are read down a column, so they get their own cell in a
		// fixed-width font rather than being run into the title.
		mins := widget.NewLabelWithStyle(strconv.Itoa(t.Minutes)+"m",
			fyne.TextAlignTrailing, fyne.TextStyle{Monospace: true})
		var name fyne.CanvasObject = widget.NewLabel(t.Title)
		if t.URL != "" {
			if u, err := url.Parse(t.URL); err == nil {
				link := widget.NewHyperlink(t.Title, u)
				link.Truncation = fyne.TextTruncateEllipsis
				name = link
			}
		}
		rows = append(rows, container.New(widgets.NewRatioRow(0.10, 0.90), mins, name))
	}
	return container.NewVBox(rows...)
}

// ExportWeekForEmail writes the shown week's TSV and puts the mail text on the
// clipboard, then says what it did.
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
	path, n, mailText, err := backend.ExportWeek(
		ui.Cfg, backend.DownloadsDir(shell.DataDir()), ui.Cfg.WorklogOwner, from, to, items)
	if err != nil {
		ui.Errf(err)
		return
	}
	fyne.CurrentApp().Clipboard().SetContent(mailText)

	// Short, because the week itself is already on the tab behind this. All the
	// dialog has to say is where the file went and what was left out of it.
	msg := fmt.Sprintf(
		"Boards and the summary are on the clipboard — paste them into the mail.\n\n"+
			"%d entries saved to:\n%s", n, path)
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
	dialog.ShowInformation(fmt.Sprintf("Week of %s to %s", from, to), msg, ui.Win)
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
		name += " · today"
	}
	head := widget.NewLabelWithStyle(name, fyne.TextAlignLeading,
		fyne.TextStyle{Bold: ds == shell.Today()})

	// Left of the line is what the day actually holds and what is queued for it;
	// right of it is what those two come to together — the number the day would
	// score if everything standing on it were pushed.
	banked := "nothing logged"
	if mins > 0 {
		banked = DayScoreLine(mins)
	}
	if waiting > 0 {
		banked += fmt.Sprintf(" · +%d min", waiting)
		if placeholder > 0 {
			banked += " (inc. support)"
		}
	}
	projected := ""
	if waiting > 0 {
		projected = DayScoreLine(mins + waiting)
	}
	body := []fyne.CanvasObject{
		head,
		ui.ProgressBarParts("strip/"+t.Format("Mon"), ds, shell.PlannedBarParts(ui.Cfg, byOrg, draftByOrg, planned), 8),
		container.New(shell.SplitCaption{}, shell.WeekCaption(banked), shell.WeekCaption(projected)),
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
	frame.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	frame.StrokeWidth = 1
	frame.CornerRadius = shell.CellCornerRadius
	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(0, backend.WeekColMinHeight))

	col := newDayColumn(ui, ds, container.NewStack(floor, frame,
		container.NewPadded(container.NewVBox(body...))), frame)
	col.Mins, col.Waiting = mins, waiting
	// The day turns green once logged + drafts + planned reach the target, as
	// on the Sprint Calendar. Planned time only colours the day; the captions
	// still read logged and drafts alone.
	col.Planned = planned
	col.BaseFill = shell.DayFill(mins, waiting+col.Planned)
	frame.FillColor = col.BaseFill
	return col
}

// DayScoreLine is one day's minutes against the target: what it holds, what
// share of the day that is, and what it still owes.
//
// The percentage alone made the arithmetic the reader's job — "68%" of 480 is
// how many minutes short? The number that decides whether to log more is the
// one that was missing, so it is spelled out.
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
		where = widget.NewLabel(r["issue"])
	}
	where.Wrapping = fyne.TextWrapBreak

	push, edit, del := rowActions(ui, r, refresh)
	leadingItems := []fyne.CanvasObject{shell.CompactPush(push)}
	if shell.RowPartlySaved(r) {
		leadingItems = append(leadingItems, shell.SavedTag("partly saved"))
	}
	if ui.IssueHasPending(r["issue"]) {
		leadingItems = append(leadingItems, shell.SavedTag("commits remaining"))
	}
	leading := container.NewHBox(leadingItems...)
	body := container.NewVBox(title, mins, where,
		container.NewBorder(nil, nil, leading, container.NewHBox(widgets.WithPointerCursor(edit), widgets.WithPointerCursor(del))))

	bg := canvas.NewRectangle(shell.BlendColor(
		theme.Color(theme.ColorNameInputBackground), accent, 0.10))
	bg.StrokeColor = shell.BlendColor(theme.Color(theme.ColorNameInputBorder), accent, 0.5)
	bg.StrokeWidth = 1
	bg.CornerRadius = 10
	stripe := canvas.NewRectangle(accent)
	stripe.SetMinSize(fyne.NewSize(4, 0))
	stripe.CornerRadius = 2
	return container.NewStack(bg, container.NewPadded(
		container.NewBorder(nil, nil, stripe, nil, container.NewPadded(body))))
}

// weekRowTitle names a locally saved row the same way its tile does.
func weekRowTitle(ui *shell.UI, r backend.Row) string {
	return RowTitle(ui, r)
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
		if col.Frame.StrokeColor == theme.Color(theme.ColorNameInputBorder) &&
			col.Frame.StrokeWidth == 1 {
			continue
		}
		col.Frame.StrokeColor = theme.Color(theme.ColorNameInputBorder)
		col.Frame.StrokeWidth = 1
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
	label := canvas.NewText(fmt.Sprintf("%s · %dm",
		shell.Truncate(weekRowTitle(ui, r), 24), r.Minutes()), theme.Color(theme.ColorNameForeground))
	label.TextSize = theme.CaptionTextSize()

	bg := canvas.NewRectangle(shell.BlendColor(theme.Color(theme.ColorNameInputBackground),
		shell.OrgColor(ui.Cfg, shell.OrgOf(r["issue"])), 0.35))
	bg.CornerRadius = 8
	bg.StrokeColor = theme.Color(theme.ColorNamePrimary)
	bg.StrokeWidth = 1

	ghost := container.NewStack(bg, container.NewPadded(label))
	ghost.Resize(ghost.MinSize()) // nothing lays this out; it sizes itself
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
		MarkDropTargets(t.ui)
	}
	moveGhost(t.ui, e.AbsolutePosition)
}

func (t *DragTile) DragEnd() {
	if !t.lifted {
		return
	}
	t.lifted = false
	hideGhost(t.ui)

	day := t.ui.HoverDay
	t.ui.DragRow = nil
	t.ui.HoverDay = ""
	MarkDropTargets(t.ui)
	// Dropped on nothing, or on the day it already sits on: the card just goes
	// back where it came from.
	moveRowToDay(t.ui, t.Row, day)
}

// weekSupportControl is the "+180 min weekday support" checkbox. It is built
// once and kept, like Log Work's other checkboxes, and only re-synced to the
// week on each redraw. A fresh checkbox per redraw drew fine but never got the
// mouse, so it could not be ticked.
//
// Display only, and it says so: nothing is pushed. The tick is remembered for
// its own week (support-weeks.json) until that week's Friday passes.
func weekSupportControl(ui *shell.UI) fyne.CanvasObject {
	on := shell.Hooks.SupportOn(ui, ui.WeekStart)
	if ui.WeekSupport == nil {
		ui.WeekSupport = widget.NewCheck(
			fmt.Sprintf("+%d min weekday support (display only)", backend.SupportPlaceholderMins), nil)
		// Seeded before the handler is attached: SetChecked fires OnChanged.
		ui.WeekSupport.SetChecked(on)
		ui.WeekSupport.OnChanged = func(on bool) {
			shell.Hooks.SetSupport(ui, on, ui.WeekStart)
			DrawWeekStrip(ui)
		}
		ui.WeekSupportView = shell.GoldChoice(ui.WeekSupport)
		return ui.WeekSupportView
	}
	if ui.WeekSupport.Checked != on {
		// Re-sync quietly: walking to another week must not tick or untick it.
		changed := ui.WeekSupport.OnChanged
		ui.WeekSupport.OnChanged = nil
		ui.WeekSupport.SetChecked(on)
		ui.WeekSupport.OnChanged = changed
	}
	return ui.WeekSupportView
}
