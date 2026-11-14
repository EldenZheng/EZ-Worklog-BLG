package logwork

import (
	_ "embed"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// ============================ recent entries ============================

func DrawRecent(ui *shell.UI) {
	// The strip is part of this list, not a separate view: whatever changes the
	// saved entries — a drop, a push, an edit — changes what the week shows.
	ui.RefreshLoggedShas()
	DrawWeekStrip(ui)
	if ui.Kanban != nil && ui.Kanban.CalendarActive && !ui.Kanban.CalendarResizing && ui.Kanban.CalendarDrag == nil {
		shell.Hooks.KanbanUIDraw(ui.Kanban)
	}
	if ui.RecentBox == nil {
		return
	}
	rows, err := ui.Store.ReadRows()
	if err != nil {
		ui.RecentBox.Objects = []fyne.CanvasObject{shell.ColorLabel(err.Error(), theme.ColorNameError)}
		ui.RecentBox.Refresh()
		shell.Relayout(ui.LogBody)
		return
	}
	// The key above the week governs this list too, not only the commits
	// above it: filtering the pending tiles to one employer and then listing the
	// other's saved entries underneath would defeat the point of filtering.
	rows = ui.ShownRows(rows)
	if strings.TrimSpace(ui.LogSearch) != "" {
		var matching []backend.Row
		for _, r := range rows {
			if ui.RowMatchesSearch(r) {
				matching = append(matching, r)
			}
		}
		rows = matching
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
	showAll := ui.ShowPushed != nil && ui.ShowPushed.Checked
	inStrip := map[string]bool{}
	for _, d := range shell.WeekDates(ui.WeekStart) {
		inStrip[d] = true
	}
	var shown []backend.Row
	pendingCount, onStrip := 0, 0
	for _, r := range rows {
		waiting := r["pushed_at"] == ""
		if waiting {
			if !ui.DraftsShown() {
				continue
			}
			pendingCount++
			if inStrip[r["date"]] {
				onStrip++
				continue
			}
		}
		switch {
		case showAll:
			if strings.HasPrefix(r["date"], shell.ThisMonth()) {
				shown = append(shown, r)
			}
		case waiting:
			shown = append(shown, r)
		}
	}
	SortDraftRows(shown, ui.DraftSort)
	const maxShown = 25
	hidden := 0
	if len(shown) > maxShown {
		hidden = len(shown) - maxShown
		shown = shown[:maxShown]
	}

	head := fmt.Sprintf("%d waiting to push", pendingCount)
	if showAll {
		head = fmt.Sprintf("%s so far — %d still waiting to push",
			shell.MonthName(shell.ThisMonth()), pendingCount)
	}
	if onStrip > 0 {
		head += fmt.Sprintf("   ·   %d on the week below", onStrip)
	}
	if !ui.DraftsShown() {
		head = "Drafts hidden by the filter"
	}

	objs := []fyne.CanvasObject{
		widget.NewLabelWithStyle(head, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	}
	// Nothing is drawn while GitHub is still fetching the week's worklogs or
	// the pending commits and their issue titles, which name these entries:
	// the list arrives once, when they have landed.
	days := shell.WeekDates(ui.WeekStart)
	_, weekState := WeekWorklogs(ui, days)
	if !savedShown(ui, days, weekState) {
		ui.RecentBox.Objects = []fyne.CanvasObject{widget.NewLabelWithStyle("Loading from GitHub…", fyne.TextAlignLeading, fyne.TextStyle{Italic: true})}
		ui.RecentBox.Refresh()
		shell.Relayout(ui.LogBody)
		DrawLogLegend(ui)
		return
	}
	switch {
	case len(shown) > 0 || showAll:
		entries := entryTable(ui, shown, func() { DrawRecent(ui) })
		if grid, ok := entries.(*fyne.Container); ok {
			ui.Arrival("recent", ui.WeekStart).RevealIn(grid)
		}
		objs = append(objs, entries)
	case onStrip > 0:
		// Nothing to list: everything waiting is standing on its day below.
	case !ui.DraftsShown():
		// The heading says why; "everything has been pushed" may not be true.
	default:
		objs = append(objs, widget.NewLabel("Everything logged has been pushed to GitHub."))
	}
	if hidden > 0 {
		objs = append(objs, widget.NewLabel(fmt.Sprintf("…and %d older entr(y/ies) not shown.", hidden)))
	}
	ui.RecentBox.Objects = objs
	ui.RecentBox.Refresh()
	shell.Relayout(ui.LogBody)
	// The key counts this list as well as the commits, so it is redrawn whenever
	// either of them changes.
	DrawLogLegend(ui)
}

// DrawLogLegend fills the key beside the Log work week's navigation.
//
// It counts everything the key governs on that tab — commits waiting to be
// logged, and entries saved but not yet pushed — because the two empty at
// different times. Counting only the commits meant the key vanished the moment
// the last one was logged, while a list of saved entries was still sitting
// under it with no way left to filter them.
func DrawLogLegend(ui *shell.UI) {
	if ui.LogLegend == nil {
		return
	}
	byOrg := map[string]int{}
	for _, g := range ui.Pending.Groups {
		byOrg[strings.ToLower(shell.OrgOf(g.Issue))] += len(g.Commits)
	}
	if rows, err := ui.Store.ReadRows(); err == nil {
		for _, r := range rows {
			if r["pushed_at"] == "" {
				byOrg[shell.RowOrg(r)]++
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
	weekStart := ui.WeekStart
	if weekStart == "" {
		weekStart = backend.WeekStartOf(shell.Today())
	}
	onWeek := map[string]bool{}
	for _, d := range shell.WeekDates(weekStart) {
		onWeek[d] = true
	}
	for _, m := range shell.WeekMonths(weekStart) {
		items, ok := ui.ItemsFor(shell.StatusCacheKey(m))
		if !ok {
			continue
		}
		for _, it := range items {
			if onWeek[it.Date] {
				byOrg[shell.ItemOrg(it)]++
			}
		}
	}
	var offset fyne.Position
	if len(ui.LogLegend.Objects) > 0 {
		if previous, ok := ui.LogLegend.Objects[0].(*container.Scroll); ok {
			offset = previous.Offset
		}
	}
	scroll := container.NewHScroll(shell.OrgLegendWith(ui.Cfg, byOrg, ui.OrgOff, strconv.Itoa, ui.ToggleOrg))
	scroll.Resize(ui.LogLegend.Size())
	scroll.ScrollToOffset(offset)
	ui.LogLegend.Objects = []fyne.CanvasObject{scroll}
	ui.LogLegend.Refresh()
	shell.Relayout(ui.LogBody)
}

// githubItemsOn is one day's worklogs from the project, kicking that month's
// fetch off the first time it is asked. State is one of "ok", "loading", "off"
// (no project configured) or "error".
func githubItemsOn(ui *shell.UI, date string) ([]backend.WorklogItem, string) {
	if len(date) < 7 {
		return nil, "off"
	}
	items, state := monthWorklogs(ui, date[:7])
	if state != "ok" {
		return nil, state
	}
	var day []backend.WorklogItem
	for _, it := range items {
		if it.Date == date {
			day = append(day, it)
		}
	}
	return day, "ok"
}

// The readout that used to live here is now dayFillReadout, beside the picker
// it reports on — see datepicker.go.

// entryTable lays the saved rows out as tiles, the same shape the pending
// commits use above them. A row is a thing you act on — push it, fix it, drop
// it — and the buttons were the narrowest column of the table that replaced.
func entryTable(ui *shell.UI, rows []backend.Row, refresh func()) fyne.CanvasObject {
	if len(rows) == 0 {
		return widget.NewLabel("Nothing logged yet.")
	}
	tiles := make([]fyne.CanvasObject, 0, len(rows))
	for _, r := range rows {
		tile := RowTile(ui, r, refresh)
		if r["pushed_at"] == "" {
			// Still ours to move: it can be dragged onto a day in the strip
			// above. A pushed entry is already a date on the board, so it stays
			// put and is edited the long way if it really has to change.
			tile = newDragTile(ui, r, tile)
		}
		tiles = append(tiles, tile)
	}
	return container.New(widgets.NewFlowGrid(BubbleMinWidth, BubbleMaxWidth, rowTileHeight).
		Compact(BubbleCompactBelow, BubbleCompactMinWidth), tiles...)
}

// rowTileHeight fits two wrapped lines of description over a footer and a row
// of buttons. Taller than a pending tile, which carries no buttons.
const rowTileHeight = 176

// RowLabel names a saved row: the issue's own title where it is known, and the
// commit's first real line where it is not — a row logged before the title
// arrived, or one with no issue ref at all.
func RowLabel(ui *shell.UI, issue string, commits []backend.Commit) string {
	if info, ok := ui.IssueInfo[issue]; ok && strings.TrimSpace(info.Title) != "" {
		return info.Title
	}
	if len(commits) == 1 {
		return backend.CommitSubject(commits[0])
	}
	if len(commits) > 1 {
		return fmt.Sprintf("%s (%d commits)", backend.CommitSubject(commits[0]), len(commits))
	}
	return issue
}

// RowTitle derives a manual row's current display name instead of trusting the
// description captured when it was first saved. That matters when a waiting
// row is dragged to another day: its Meeting/Review title must follow the new
// date. Issue-backed rows read like the issue list, using the loaded title and
// falling back to the ref while that title is unavailable.
func RowTitle(ui *shell.UI, r backend.Row) string {
	if info, ok := ui.IssueInfo[r["issue"]]; ok && strings.TrimSpace(info.Title) != "" {
		return strings.TrimSpace(info.Title)
	}
	if title := strings.TrimSpace(r["issue_title"]); title != "" {
		return title
	}
	switch r["type"] {
	case backend.KindMeeting:
		return backend.MeetingTitle(ui.Cfg, r["date"])
	case backend.KindBulkReview:
		return backend.DailyCodeReviewTitle(ui.Cfg, r["date"])
	case backend.KindCodeReview:
		// Interim bulk drafts used kindCodeReview with no issue ref. Keep their
		// daily title dynamic while preserving normal single-review rows.
		if r["issue"] == "" {
			if _, err := backend.ParseCodeReviewItems(r["remarks"]); err == nil {
				return backend.DailyCodeReviewTitle(ui.Cfg, r["date"])
			}
		}
		if r["issue"] != "" {
			return r["issue"]
		}
		return backend.CodeReviewTitle(r["date"])
	case backend.KindOther:
		if r["issue"] != "" {
			return r["issue"]
		}
		return "Worklog: " + r["date"]
	case backend.KindIndependent:
		if title := strings.TrimSpace(r["parent_title"]); title != "" {
			return title
		}
	}
	if what := strings.TrimSpace(r["description"]); what != "" {
		return what
	}
	if what := strings.TrimSpace(r["remarks"]); what != "" {
		return what
	}
	return "(no description)"
}

// RowTile is one locally saved entry: what it is, when, how long, and what can
// still be done to it.
func RowTile(ui *shell.UI, r backend.Row, refresh func()) fyne.CanvasObject {
	accent := shell.RowAccentColor(ui.Cfg, r)
	muted := theme.Color(theme.ColorNamePlaceHolder)
	caption := func(s string) fyne.CanvasObject { return fitCaption(shell.Truncate(s, 56), muted) }

	what := RowTitle(ui, r)
	if what == r["issue"] {
		what = backend.OrDefault(shell.ShortIssueRef(what), what)
	}
	title := widget.NewLabelWithStyle(shell.Truncate(what, BubbleTitleChars),
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Wrapping = fyne.TextWrapWord

	issue := r["issue"]
	switch {
	case issue != "":
		issue = ShortIssueLabel(ui.IssueInfo[r["issue"]], r["issue"])
	case r["type"] == backend.KindMeeting:
		// Ref-less on disk but not unfiled: it goes to the day's meeting issue,
		// which is found or created when it is pushed.
		issue = "meeting — " + backend.MeetingTitle(ui.Cfg, r["date"])
	case r["type"] == backend.KindBulkReview:
		issue = "bulk review — " + backend.DailyCodeReviewTitle(ui.Cfg, r["date"])
	case r["type"] == backend.KindCodeReview:
		if _, err := backend.ParseCodeReviewItems(r["remarks"]); err == nil {
			issue = "bulk review — " + backend.DailyCodeReviewTitle(ui.Cfg, r["date"])
		} else {
			issue = "code review — no issue ref"
		}
	case r["type"] == backend.KindIndependent && r["parent_repo"] != "":
		issue = "new issue in " + r["parent_repo"]
	default:
		issue = shell.OrDash(r["type"]) + " — no issue ref"
	}
	state := "waiting to push"
	if shell.RowPartlySaved(r) {
		state = "partly saved on GitHub — retry push"
	} else if r["pushed_at"] != "" {
		state = "pushed " + strings.Replace(r["pushed_at"], "T", " ", 1)
	}
	// Minutes, not hours and minutes: this is the number that goes in the
	// worklog field, and reading "1h 30m" here meant converting it back to 90
	// every time before pushing.
	summary := caption(fmt.Sprintf("%s  ·  %d min  ·  %s", r["date"], r.Minutes(), issue))
	if backend.RowIsSingleCodeReview(r) {
		summary = container.NewBorder(nil, nil, nil, container.NewCenter(shell.CodeReviewTag()), summary)
	}
	foot := container.New(layout.NewCustomPaddedLayout(0, 0, theme.InnerPadding(), theme.InnerPadding()), container.NewVBox(
		summary,
		caption(state)))

	push, edit, del := rowActions(ui, r, refresh)
	actionItems := []fyne.CanvasObject{shell.CompactGitHubHoverButton(push)}
	if shell.RowPartlySaved(r) {
		actionItems = append(actionItems, shell.SavedTag("partly saved"))
	}
	actions := container.NewHBox(append(actionItems, layout.NewSpacer(), widgets.WithPointerCursor(edit), widgets.WithPointerCursor(del))...)

	body := container.NewBorder(nil, container.NewVBox(foot, actions), nil, nil, title)
	bg := canvas.NewRectangle(shell.BlendColor(
		theme.Color(theme.ColorNameInputBackground), accent, 0.10))
	bg.StrokeColor = shell.BlendColor(theme.Color(theme.ColorNameInputBorder), accent, 0.5)
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
// overwritten, and the sub-issue is reused rather than made twice. All three
// keep the card they sit on lit while the pointer is on them.
func rowActions(ui *shell.UI, r backend.Row, refresh func()) (push, edit, del *shell.HoverButton) {
	label := "Push"
	if r["pushed_at"] != "" {
		label = "Re-push"
	}
	push = shell.NewHoverButton(label, nil, func() { pushRow(ui, r, refresh) })
	if r["issue"] == "" && !backend.PushableWithoutIssue(r) {
		push.Disable() // nothing to push to; the editor is where a ref is added
	}
	edit = shell.NewHoverButton("", theme.DocumentCreateIcon(), func() {
		openRowEditor(ui, r, refresh)
	})
	edit.Importance = widget.LowImportance
	del = shell.NewHoverButton("", theme.DeleteIcon(), func() {
		ConfirmDelete(ui, r, refresh)
	})
	del.Importance = widget.LowImportance
	return push, edit, del
}

// ConfirmDelete puts up the two-button "really delete this?" popup for a saved
// row, using the same red action and gold Cancel as Sprint Calendar Clear.
func ConfirmDelete(ui *shell.UI, r backend.Row, refresh func()) {
	body := widget.NewLabel(
		"Delete this entry? Its commits become pending again. (Anything already pushed to GitHub stays there.)")
	body.Wrapping = fyne.TextWrapWord

	var pop *widget.PopUp
	cancel := widget.NewButton("Cancel", func() { shell.ClosePopUp(pop) })
	confirm := widget.NewButton("Delete", func() {
		shell.ClosePopUp(pop)
		if err := ui.Store.DeleteRow(r["id"]); err != nil {
			ui.Errf(err)
			return
		}
		ui.ReplayWorkArrivals()
		ui.ReplayArrival("planner")
		refresh()
	})
	buttons := shell.RemovalButtons(cancel, confirm)
	content := container.NewBorder(container.NewVBox(widget.NewLabelWithStyle("Delete entry", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), body), buttons, nil, nil, nil)
	padded := shell.RemovalPopupContent(content)
	pop = widget.NewModalPopUp(padded, ui.Win.Canvas())
	sz := ui.RemovalPopupSize(padded)
	pop.Resize(sz)
	pop.Show()
	// Resizing keeps the old top-left position, so centre the compact confirmation.
	canvasSize := ui.Win.Canvas().Size()
	pop.Move(fyne.NewPos(
		(canvasSize.Width-sz.Width)/2,
		(canvasSize.Height-sz.Height)/2))
	shell.AnimateOpening(ui.Win.Canvas())
}

// pushRow sends one saved row to GitHub and reports what landed.
func pushRow(ui *shell.UI, r backend.Row, refresh func()) {
	pushSavedEntry(ui, r, func(updated backend.Row, res backend.PushResult, perr error) {
		r = updated
		if perr == errReviewPushCancelled {
			refresh()
			return
		}
		repush := func() { repushRow(ui, r["id"], refresh) }
		if perr != nil {
			msg := widget.NewLabel(perr.Error())
			msg.Wrapping = fyne.TextWrapWord
			showPushFailedDialog(ui, "Push failed", container.NewVBox(msg,
				widget.NewLabel("The entry is still saved locally.")), repush)
			return
		}
		_, complete := applyPushResult(ui, r["id"], res)
		refresh()
		ui.RefreshAfterPush(r["date"])
		title := "Push incomplete"
		if complete {
			title = "Push complete"
		}
		showPushResultDialog(ui, title, res, repush)
	})
}

// repushRow pushes a saved row again, read fresh from disk so a retry sends
// whatever the failed attempt already recorded on it (the issue URL, the item
// it created) rather than starting over.
func repushRow(ui *shell.UI, id string, refresh func()) {
	rows, err := ui.Store.ReadRows()
	if err != nil {
		ui.Errf(err)
		return
	}
	for _, r := range rows {
		if r["id"] == id {
			pushRow(ui, r, refresh)
			return
		}
	}
	ui.Errf(fmt.Errorf("that entry is no longer saved, so there is nothing to push again"))
}

// showPushFailedDialog is the popup for a push that did not finish: what went
// wrong, and a button to push the same entry again. Push again writes to
// GitHub, so it wears the same green pill as every other push button; the
// stock confirm dialog would have painted it Fyne's default blue.
func showPushFailedDialog(ui *shell.UI, title string, content fyne.CanvasObject, repush func()) {
	var d dialog.Dialog
	closeBtn := widget.NewButton("Close", func() { shell.CloseDialog(d, ui.Win) })
	again := widget.NewButton("Push again", func() {
		shell.CloseDialog(d, ui.Win)
		repush()
	})
	d = dialog.NewCustomWithoutButtons(title,
		container.NewVBox(content, shell.DialogButtons(closeBtn, shell.GitHubButton(again))), ui.Win)
	shell.ShowDialog(d, ui.Win)
}

// showPushResultDialog puts up a compact popup with "Pushed:" and a clickable
// link to the worklog on GitHub. Incomplete pushes carry their problem lines
// beneath the link so nothing is silently swallowed.
func showPushResultDialog(ui *shell.UI, title string, res backend.PushResult, repush func()) {
	rows := []fyne.CanvasObject{}
	if res.URL != "" {
		if u, err := url.Parse(res.URL); err == nil {
			rows = append(rows, container.NewHBox(
				widget.NewLabelWithStyle("Pushed:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				widget.NewHyperlink(res.URL, u),
			))
		} else {
			rows = append(rows, widget.NewLabel("Pushed: "+res.URL))
		}
	}
	if !res.Complete() {
		rows = append(rows, widget.NewLabel(strings.Join(res.Problems, "\n")))
		rows = append(rows, widget.NewLabel("Push again to finish it, or later from the entry's push button."))
		showPushFailedDialog(ui, title, container.NewVBox(rows...), repush)
		return
	}
	shell.ShowStockDialog(dialog.NewCustom(title, "OK", container.NewVBox(rows...), ui.Win), ui.Win, "OK")
}
