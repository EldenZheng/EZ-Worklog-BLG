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

// savedCommitDate uses the stored author date, or a commit already in memory
// when a row predates that field.
func savedCommitDate(ui *shell.UI, saved string, refs []backend.CommitRef) string {
	if backend.ValidPlannerDate(saved) {
		return saved
	}
	if len(refs) == 0 {
		return ""
	}
	dates := make(map[string]string)
	for _, commits := range ui.WeekCommits {
		for _, commit := range commits {
			if backend.ValidPlannerDate(commit.Date) {
				dates[backend.CommitRef{Repo: commit.Repo, Sha: backend.Short(commit.Sha)}.Key()] = commit.Date
			}
		}
	}
	for _, ref := range refs {
		if date := dates[backend.CommitRef{Repo: ref.Repo, Sha: backend.Short(ref.Sha)}.Key()]; date != "" {
			return date
		}
	}
	return ""
}

func localCommitDate(folders []string, refs []backend.CommitRef) string {
	clones := backend.RepoClones(folders)
	for _, ref := range refs {
		dir := clones[strings.ToLower(ref.Repo)]
		if dir == "" {
			continue
		}
		out, err := backend.Git(dir, nil, "", "show", "-s", "--format=%aI", "--end-of-options", ref.Sha+"^{commit}", "--")
		if date := backend.CommitWorkDate(strings.TrimSpace(out)); err == nil && backend.ValidPlannerDate(date) {
			return date
		}
	}
	return ""
}

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

	var headRow fyne.CanvasObject = widget.NewLabelWithStyle(head, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	if suggest := suggestTitlesButton(ui, rows); suggest != nil {
		headRow = container.NewHBox(headRow, suggest)
	}
	objs := []fyne.CanvasObject{headRow}
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
	dup := duplicateRows(rows)
	for _, r := range rows {
		tiles = append(tiles, rowTile(ui, r, refresh, dup[r["id"]]))
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
	if title := strings.TrimSpace(r["display_title"]); title != "" {
		return title
	}
	if r["type"] == backend.KindMeeting && strings.TrimSpace(r["meeting_title"]) != "" {
		return strings.TrimSpace(r["meeting_title"])
	}
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
		return backend.BulkReviewLabel(ui.Cfg, r)
	case backend.KindCodeReview:
		// Interim bulk drafts used kindCodeReview with no issue ref. Keep their
		// daily title dynamic while preserving normal single-review rows.
		if r["issue"] == "" {
			if _, err := backend.ParseCodeReviewItems(r["remarks"]); err == nil {
				return backend.BulkReviewLabel(ui.Cfg, r)
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
	return rowTile(ui, r, refresh, false)
}

// rowTile is RowTile, tagged when another entry on the same day goes to the
// same issue (or is another bulk review).
func rowTile(ui *shell.UI, r backend.Row, refresh func(), duplicate bool) fyne.CanvasObject {
	accent := shell.RowAccentColor(ui.Cfg, r)
	muted := theme.Color(theme.ColorNamePlaceHolder)
	caption := func(s string) fyne.CanvasObject { return fitCaption(shell.Truncate(s, 56), muted) }

	what := RowTitle(ui, r)
	if what == r["issue"] {
		what = backend.OrDefault(shell.ShortIssueRef(what), what)
	}
	title := widget.NewLabelWithStyle(shell.Truncate(what, BubbleTitleChars),
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Truncation = fyne.TextTruncateEllipsis // one line, so every tile keeps the same height

	fallback := ""
	if r["issue"] == "" {
		switch {
		case r["type"] == backend.KindMeeting:
			fallback = "meeting — " + backend.MeetingTitle(ui.Cfg, r["date"])
		case backend.RowIsBulkCodeReview(r):
			fallback = "bulk review — " + backend.BulkReviewLabel(ui.Cfg, r)
		case r["type"] == backend.KindCodeReview:
			fallback = "code review — no issue ref"
		case r["type"] == backend.KindIndependent && r["parent_repo"] != "":
			fallback = "new issue in " + r["parent_repo"]
		default:
			fallback = shell.OrDash(r["type"]) + " — no issue ref"
		}
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
	summaryText := fmt.Sprintf("%s  ·  %d min", r["date"], r.Minutes())
	summary := caption(summaryText)
	if tag := minutesTag(ui, r); tag != nil {
		full := fyne.MeasureText(summaryText, theme.CaptionTextSize(), fyne.TextStyle{}).Width + minutesTagGap()
		summary = container.New(&RefTagLayout{Full: full}, summary, tag)
	}
	footLines := container.NewVBox(savedReferenceLine(r, fallback), summary, caption(state))
	if line := refStateLine(r); line != "" && !backend.RefsLocked(r) {
		footLines.Add(caption(line))
	}
	foot := container.New(layout.NewCustomPaddedLayout(0, 0, theme.InnerPadding(), theme.InnerPadding()), footLines)

	push, edit, del := rowActions(ui, r, refresh)
	var actionItems []fyne.CanvasObject
	if push != nil {
		actionItems = append(actionItems, shell.CompactGitHubHoverButton(push))
	}
	if add := addRefsButton(ui, r, refresh); add != nil {
		actionItems = append(actionItems, shell.CompactGitHubHoverButton(add))
	}
	actionItems = append(actionItems, rowTags(ui, r, duplicate, false)...)
	if tag := titleTag(ui, r, refresh); tag != nil {
		actionItems = append(actionItems, tag)
	}
	actionItems = append(actionItems, layout.NewSpacer())
	if copyRef := copyRefButton(r); copyRef != nil {
		actionItems = append(actionItems, widgets.WithPointerCursor(copyRef))
	}
	actions := container.NewHBox(append(actionItems, widgets.WithPointerCursor(edit), widgets.WithPointerCursor(del))...)

	// Nothing is cut: the grid grows every tile to the tallest one, and the
	// title is held to two lines so one long name does not stretch them all.
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
	content := container.NewStack(bg, container.NewPadded(inner))
	if r["pushed_at"] == "" {
		tile := newDragTile(ui, r, content)
		tile.OnTap = edit.OnTapped
		return tile
	}
	return shell.NewCardTappable(content, edit.OnTapped)
}

// minutesTag is what stands beside an entry's minutes: Legacy for one pending
// at the SOP cutover, Already merged for one whose commits can no longer name
// its Worklog, Pending Push for one whose commits are only on this machine,
// and Code review for a single review. Nil when there is none.
func minutesTag(ui *shell.UI, r backend.Row) fyne.CanvasObject {
	var tags []fyne.CanvasObject
	switch {
	case backend.RowIsLegacy(r):
		tags = append(tags, shell.LegacyTag())
	case backend.RefsLocked(r):
		tags = append(tags, shell.AlreadyMergedTag())
	case rowWaitsForPush(ui, r):
		tags = append(tags, shell.PendingPushTag())
	}
	if backend.RowIsSingleCodeReview(r) {
		tags = append(tags, shell.CodeReviewTag())
	}
	switch len(tags) {
	case 0:
		return nil
	case 1:
		return tags[0]
	}
	return container.NewHBox(tags...)
}

// minutesTagGap is the space between the minutes and the tag beside them.
func minutesTagGap() float32 { return 2.5 * theme.Padding() }

// savedReferenceLine is shared by the saved list and the week's day cards.
func savedReferenceLine(r backend.Row, fallback string) fyne.CanvasObject {
	label, issueURL := backend.OrDefault(r["issue"], fallback), ""
	issue, _ := backend.IssueRefFromAny(r["issue"])
	if owner, repo, num, err := backend.SplitIssue(issue); err == nil {
		label = fmt.Sprintf("%s#%d", repo, num)
		issueURL = fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, num)
	}
	var where fyne.CanvasObject
	var full float32
	if issueURL != "" {
		// A plain link like the commits beside it, held at its text's width so
		// the rest of the row still opens the editor.
		link := shell.NewHoverLink(label, issueURL)
		full = link.MinSize().Width + theme.Padding()
		where = container.NewHBox(link)
		// Too long for its bubble, it ends in an ellipsis instead of
		// running out past the bubble's edge.
		if h, ok := link.(*shell.HoverLink); ok {
			where = container.New(&textWidthLayout{link: h, full: h.MinSize().Width}, h)
		}
	} else {
		plain := widget.NewLabel(label)
		plain.Wrapping = fyne.TextWrapBreak
		where = plain
		full = fyne.MeasureText(label, theme.TextSize(), plain.TextStyle).Width + 2*theme.InnerPadding() + 1
	}

	var beside []fyne.CanvasObject
	if r["type"] == backend.KindCommit || r["type"] == "" {
		for _, ref := range backend.ParseCommitRefs(r["refs"]) {
			if ref.Sha == "" {
				continue
			}
			// Older rows have only a SHA. The issue can live in a different repo.
			commitURL := ""
			if ref.Repo != "" {
				commitURL = fmt.Sprintf("https://github.com/%s/commit/%s", ref.Repo, ref.Sha)
			}
			beside = append(beside, shell.NewHoverLink(backend.Short(ref.Sha), commitURL))
		}
	}
	// A single review's Code review tag stands beside its minutes (minutesTag).
	if backend.MissingMeetingRecording(r) {
		beside = append(beside, shell.MissingMeetingRecordingTag())
	}
	if len(beside) == 0 {
		return where
	}
	next := beside[0]
	if len(beside) > 1 {
		next = container.NewVBox(beside...)
	}
	return container.New(&RefTagLayout{Full: full}, where, next)
}

// textWidthLayout holds a link at the width of its whole text. Only when the
// row is narrower than that is it cut short with an ellipsis, rather than
// pushing out of its bubble; a link that fits is never cut.
type textWidthLayout struct {
	link *shell.HoverLink
	full float32
}

func (l *textWidthLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, objects[0].MinSize().Height)
}

func (l *textWidthLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	cut := fyne.TextTruncateOff
	if size.Width < l.full {
		cut = fyne.TextTruncateEllipsis
	}
	if l.link.Truncation != cut {
		l.link.Truncation = cut
		l.link.Refresh()
	}
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(min(l.full, size.Width), size.Height))
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
	// Future drafts wait until their day: see backend.RowPushable.
	if PushDay == nil || backend.RowPushable(r, PushDay()) {
		push = shell.NewHoverButton(label, nil, func() { pushRow(ui, r, refresh) })
		if r["issue"] == "" && !backend.PushableWithoutIssue(r) {
			push.Disable() // nothing to push to; the editor is where a ref is added
		}
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
	const message = "Delete this entry? Its commits become pending again. (Anything already pushed to GitHub stays there.)"
	ui.ConfirmRemoval("Delete entry", message, "Delete", func() {
		if err := ui.Store.DeleteRow(r["id"]); err != nil {
			ui.Errf(err)
			return
		}
		ui.ReplayWorkArrivals()
		ui.ReplayArrival("planner")
		refresh()
	})
}

// pushRow sends one saved row to GitHub and reports what landed.
func pushRow(ui *shell.UI, r backend.Row, refresh func()) {
	pushSavedEntry(ui, r, func(updated backend.Row, res backend.PushResult, plans []backend.PullRequestPlan, perr error) {
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
		showPushResultDialog(ui, title, res, plans, repush)
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
// A complete push lists any pull request its branch already has, and offers
// Push to PR for one it could have, in this same popup.
func showPushResultDialog(ui *shell.UI, title string, res backend.PushResult, plans []backend.PullRequestPlan, repush func()) {
	rows := []fyne.CanvasObject{}
	if res.URL != "" {
		if u, err := url.Parse(res.URL); err == nil {
			rows = append(rows, container.NewHBox(
				widget.NewLabelWithStyle("Pushed:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				shell.HyperlinkWithIcon(res.URL, u),
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
	for _, p := range plans {
		if p.Existing != "" {
			rows = append(rows, container.NewVBox(
				widget.NewLabelWithStyle("Pull request on "+p.Branch+":", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				linkWithCopy(p.Existing)))
		}
	}
	fresh := newPullRequests(plans)
	if len(fresh) == 0 {
		shell.ShowStockDialog(dialog.NewCustom(title, "OK", container.NewVBox(rows...), ui.Win), ui.Win, "OK")
		return
	}
	rows = append(rows, wrapped("Every commit of yours on "+fresh[0].Branch+" is logged, so it is ready for a pull request."))
	var popup *widget.PopUp
	ok := widget.NewButton("OK", func() { shell.ClosePopUp(popup) })
	toPR := widget.NewButton("Push to PR", func() {
		shell.ClosePopUp(popup)
		offerPlans(ui, fresh)
	})
	popup = ui.ShowConfirmation(title, container.NewVBox(rows...), shell.DialogButtons(ok, toPR))
}

// rowWaitsForPush reports whether any of r's commits is only on this machine,
// as the last pending fetch found them in the clones.
func rowWaitsForPush(ui *shell.UI, r backend.Row) bool {
	if ui == nil || len(ui.Pending.Local) == 0 {
		return false
	}
	for _, c := range backend.RowCommitRefs(r) {
		if ui.Pending.Local[c.Key()] {
			return true
		}
	}
	return false
}
