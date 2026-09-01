package main

// The week strip answers the question the saved-but-unpushed list could not:
// which day does this belong on? Seven columns, Monday to Sunday, each showing
// what GitHub already has banked on that day against the 480-minute target — so
// the day with room left is the one you can see. A locally saved entry is then
// dragged onto it, which is the whole of "set the date": no typing, no calendar
// popup, and the date the Push button sends is the one it was dropped on.

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
)

const (
	// workDayStart is the hour at which the app rolls over to a new day. Work
	// that runs past midnight belongs to the day it started on, not to the one
	// the clock has just turned over into: at 1am the day still owing minutes is
	// yesterday, and that is the day a new entry should default to.
	workDayStart = 8

	weekColMinHeight = 168 // a floor for an empty day, not a ceiling for a full one
	weekCardChars    = 44  // the card's own title, which wraps rather than elides

	// How much green a finished day takes. A day finished on GitHub is stated;
	// one only finished by counting drafts is a paler claim, because it is one.
	weekDoneTint  = 0.22
	weekDraftTint = 0.10

	// How long a Copy button says "Copied" before going back to its own label.
	copiedFlash = 1500 * time.Millisecond
)

// workDate is the working day a moment belongs to. See workDayStart.
func workDate(t time.Time) string {
	if t.Hour() < workDayStart {
		t = t.AddDate(0, 0, -1)
	}
	return t.Format("2006-01-02")
}

// weekStartOf is the Monday of the week a date falls in.
func weekStartOf(date string) string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(date))
	if err != nil {
		t = time.Now()
	}
	off := (int(t.Weekday()) + 6) % 7 // Sunday is 0 in Go, and last here
	return t.AddDate(0, 0, -off).Format("2006-01-02")
}

// weekDates lists the seven days of a week, Monday first.
func weekDates(start string) []string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(start))
	if err != nil {
		return nil
	}
	out := make([]string, 7)
	for i := range out {
		out[i] = t.AddDate(0, 0, i).Format("2006-01-02")
	}
	return out
}

func shiftWeek(start string, delta int) string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(start))
	if err != nil {
		return start
	}
	return t.AddDate(0, 0, 7*delta).Format("2006-01-02")
}

// weekRangeLabel names a week the way a person would say it, only spelling out
// the month or the year twice when the week actually crosses one.
func weekRangeLabel(start string) string {
	a, err := time.Parse("2006-01-02", strings.TrimSpace(start))
	if err != nil {
		return start
	}
	b := a.AddDate(0, 0, 6)
	switch {
	case a.Year() != b.Year():
		return fmt.Sprintf("%s – %s", a.Format("2 Jan 2006"), b.Format("2 Jan 2006"))
	case a.Month() != b.Month():
		return fmt.Sprintf("%d %s – %d %s %d", a.Day(), a.Format("Jan"), b.Day(), b.Format("Jan"), b.Year())
	default:
		return fmt.Sprintf("%d – %d %s %d", a.Day(), b.Day(), b.Format("Jan"), b.Year())
	}
}

// ---- the data behind the strip ----

// monthWorklogs is one month of the project's worklogs, starting that month's
// fetch the first time it is asked for. State is "ok", "loading", "off" (no
// project configured) or "error" — the same words the rest of the app uses.
func (ui *UI) monthWorklogs(month string) ([]WorklogItem, string) {
	if len(projectURLs(ui.cfg)) == 0 || len(month) < 7 {
		return nil, "off"
	}
	month = month[:7]
	ui.ensureProjectCache()
	key := statusCacheKey(month)
	if items, cached := ui.itemsFor(key); cached {
		// Filtered here rather than at each caller: this is the one door the Log
		// work tab reads board items through — the week strip and today's score
		// both come from it — so the key governs both by governing this.
		return ui.shownItems(items), "ok"
	}
	if ui.projErr[key] != nil && !ui.projLoading[key] {
		return nil, "error"
	}
	if !ui.projLoading[key] {
		// loadProject redraws every pane that reads this cache when it lands, so
		// one shot is enough; the error branch above stops a failed month being
		// retried in a loop. Status shares the key, so its tab costs nothing.
		if from, to, err := monthBounds(month); err == nil {
			ui.loadProject(key, from, to, false)
		}
	}
	return nil, "loading"
}

// stateRank orders the load states by how much they need saying: a week that
// straddles two months reports the worse of the two.
func stateRank(s string) int {
	switch s {
	case "ok":
		return 0
	case "loading":
		return 1
	case "error":
		return 2
	default: // "off"
		return 3
	}
}

// weekWorklogs buckets a week's pushed worklogs by date. A week can straddle two
// months, and the cache is a month at a time, so both are asked for.
// weekMonths is the one or two calendar months a week falls across — a week
// straddling the 1st is served by two of the month-sized caches.
func weekMonths(weekStart string) []string {
	if weekStart == "" {
		weekStart = weekStartOf(today())
	}
	var months []string
	seen := map[string]bool{}
	for _, d := range weekDates(weekStart) {
		if len(d) >= 7 && !seen[d[:7]] {
			seen[d[:7]] = true
			months = append(months, d[:7])
		}
	}
	return months
}

func (ui *UI) weekWorklogs(days []string) (map[string][]WorklogItem, string) {
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

	out := map[string][]WorklogItem{}
	state := "ok"
	for _, m := range months {
		items, st := ui.monthWorklogs(m)
		if stateRank(st) > stateRank(state) {
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

func (ui *UI) drawWeekStrip() {
	if ui.weekBox == nil {
		return
	}
	if ui.weekStart == "" {
		ui.weekStart = weekStartOf(today())
	}
	days := weekDates(ui.weekStart)
	byDay, state := ui.weekWorklogs(days)

	// Everything still on this machine, whatever week it was filed under: the
	// entries that can be dragged are the ones drawn hollow on their day.
	localByDay := map[string][]Row{}
	waiting := 0
	if rows, err := ui.store.ReadRows(); err == nil {
		for _, r := range ui.shownRows(rows) {
			if r["pushed_at"] != "" {
				continue
			}
			waiting++
			localByDay[r["date"]] = append(localByDay[r["date"]], r)
		}
	}

	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		ui.weekStart = shiftWeek(ui.weekStart, -1)
		ui.drawWeekStrip()
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		ui.weekStart = shiftWeek(ui.weekStart, 1)
		ui.drawWeekStrip()
	})
	here := widget.NewButton("This week", func() {
		ui.weekStart = weekStartOf(today())
		ui.drawWeekStrip()
	})
	if ui.weekStart == weekStartOf(today()) {
		here.Disable()
	}
	title := widget.NewLabelWithStyle(weekRangeLabel(ui.weekStart),
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
	// The week that is on screen is the week that gets sent — no month picker,
	// no date range to fill in twice.
	mail := widget.NewButtonWithIcon("Export week for email", theme.MailComposeIcon(), func() {
		ui.exportWeekForEmail(days, byDay, state)
	})

	head := container.NewHBox(prev, title, next, here, layout.NewSpacer(), mail,
		widget.NewLabelWithStyle(note, fyne.TextAlignTrailing, fyne.TextStyle{Italic: true}))

	// Any column about to be thrown away stops breathing first: an animation left
	// running would keep refreshing a rectangle nothing is showing any more.
	for _, col := range ui.weekCols {
		col.stopPulse()
	}
	ui.weekCols = map[string]*dayColumn{}
	grid := container.NewGridWithColumns(7)
	for _, ds := range days {
		col := ui.weekColumn(ds, byDay[ds], localByDay[ds])
		ui.weekCols[ds] = col
		grid.Add(col)
	}

	objs := []fyne.CanvasObject{head, grid}
	if waiting > 0 {
		objs = append(objs, widget.NewLabelWithStyle(
			"Drag a card onto another day to move it — the date it is pushed with follows the drop.",
			fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))
	}

	// The boards and the week's issues stand on the tab in their own right. They
	// used to appear only inside the export dialog, which meant the one view
	// worth glancing at — what this week was actually spent on — could only be
	// had by exporting something.
	var weekItems []WorklogItem
	for _, d := range days {
		weekItems = append(weekItems, byDay[d]...)
	}
	objs = append(objs, widget.NewSeparator(),
		ui.weekBoardsPanel(days[0], days[len(days)-1]),
		ui.weekIssuesPanel(weekItems, state))

	ui.weekBox.Objects = objs
	ui.weekBox.Refresh()
}

// weekBoardsPanel is the two saved board views, filtered to the shown week.
//
// Each one is a link to click and a button to copy, because those are two
// different jobs: opening it here to check the week, and pasting the URL into a
// mail. A hyperlink alone can only do the first — the text it shows is a label,
// not the address, so there is nothing to select.
func (ui *UI) weekBoardsPanel(from, to string) fyne.CanvasObject {
	rows := []fyne.CanvasObject{bold("Boards for this week")}
	for _, raw := range weekBoardLinks(ui.cfg.WorklogOwner, from, to) {
		raw := raw
		var open fyne.CanvasObject = widget.NewLabel(raw)
		if u, err := url.Parse(raw); err == nil {
			link := widget.NewHyperlink(boardLinkLabel(raw), u)
			link.Truncation = fyne.TextTruncateEllipsis
			open = link
		}
		var copyBtn *widget.Button
		copyBtn = widget.NewButtonWithIcon("Copy link", theme.ContentCopyIcon(), func() {
			fyne.CurrentApp().Clipboard().SetContent(raw)
			// The clipboard gives no sign it took anything, so the button says
			// so itself and puts its own label back.
			copyBtn.SetText("Copied")
			time.AfterFunc(copiedFlash, func() {
				fyne.Do(func() { copyBtn.SetText("Copy link") })
			})
		})
		copyBtn.Importance = widget.LowImportance
		rows = append(rows, container.NewBorder(nil, nil, nil, copyBtn, open))
	}
	return container.NewVBox(rows...)
}

// weekIssuesPanel is what the week was spent on: one line per issue, its
// minutes, and a link to it. This is the list the weekly mail is written from,
// so it is worth being able to read it without exporting anything.
func (ui *UI) weekIssuesPanel(items []WorklogItem, state string) fyne.CanvasObject {
	head := bold("By issue")
	switch state {
	case "loading":
		return container.NewVBox(head, widget.NewLabel("Reading the week from GitHub…"))
	case "off":
		return container.NewVBox(head,
			widget.NewLabel("Set a project URL in Settings to see the week by issue."))
	case "error":
		return container.NewVBox(head,
			colorLabel("Could not read the GitHub project.", theme.ColorNameError))
	}

	totals := weekByIssue(items)
	total := weekTotalMinutes(totals)
	rows := []fyne.CanvasObject{
		container.NewHBox(head, widget.NewLabelWithStyle(
			fmt.Sprintf("%d min · %s across %d issues", total, hoursMins(total), len(totals)),
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
		rows = append(rows, container.New(newRatioRow(0.10, 0.90), mins, name))
	}
	return container.NewVBox(rows...)
}

// exportWeekForEmail writes the shown week's TSV and puts the mail text on the
// clipboard, then says what it did.
//
// Board items only, deliberately: the mail carries links to those boards, and a
// figure in the file that the CEO cannot find on the board it links to is worse
// than one that is missing. Anything still saved here is named as outstanding
// instead, so a short week is explained rather than quietly padded.
func (ui *UI) exportWeekForEmail(days []string, byDay map[string][]WorklogItem, state string) {
	if len(days) == 0 {
		return
	}
	from, to := days[0], days[len(days)-1]

	switch state {
	case "loading":
		ui.errf(ghErr("Still reading the week from GitHub — try again in a moment."))
		return
	case "off":
		ui.errf(ghErr("Set a project URL in Settings before exporting a week."))
		return
	case "error":
		ui.errf(ghErr("Could not read the GitHub project, so the week would be exported short."))
		return
	}

	var items []WorklogItem
	for _, d := range days {
		items = append(items, byDay[d]...)
	}
	path, n, mailText, err := exportWeek(
		downloadsDir(dataDir()), ui.cfg.WorklogOwner, from, to, items)
	if err != nil {
		ui.errf(err)
		return
	}
	fyne.CurrentApp().Clipboard().SetContent(mailText)

	// Short, because the week itself is already on the tab behind this. All the
	// dialog has to say is where the file went and what was left out of it.
	msg := fmt.Sprintf(
		"Boards and the summary are on the clipboard — paste them into the mail.\n\n"+
			"%d entries saved to:\n%s", n, path)
	if waiting := ui.draftMinutesByDay(from, to); len(waiting) > 0 {
		mins := 0
		for _, m := range waiting {
			mins += m
		}
		// Said rather than folded in: the mail links to the boards, and a figure
		// the reader cannot find on the board it links to is worse than one that
		// is missing.
		msg += fmt.Sprintf("\n\nNot included: %s saved here and not pushed. "+
			"Push it and export again to count it.", hoursMins(mins))
	}
	dialog.ShowInformation(fmt.Sprintf("Week of %s to %s", from, to), msg, ui.win)
}

// weekColumn is one day: how full it is, who the minutes were for, and what is
// waiting on it. items are already on GitHub — they are counted and coloured
// into the bar, but they are not listed, because a day's worth of finished work
// filled the column and buried the only thing there is anything left to do to.
func (ui *UI) weekColumn(ds string, items []WorklogItem, local []Row) *dayColumn {
	t, _ := time.Parse("2006-01-02", ds)
	byOrg := minutesByOrg(items)
	mins := 0
	for _, v := range byOrg {
		mins += v
	}
	// Biggest first, the same order the calendar's day panel uses: the card
	// carrying the most minutes is the one worth landing on, and store order put
	// it wherever it happened to be typed.
	sort.SliceStable(local, func(i, j int) bool {
		return local[i].Minutes() > local[j].Minutes()
	})

	draftByOrg := map[string]int{}
	waiting := 0
	for _, r := range local {
		draftByOrg[strings.ToLower(orgOf(r["issue"]))] += r.Minutes()
		waiting += r.Minutes()
	}

	name := fmt.Sprintf("%s %d", t.Format("Mon"), t.Day())
	if ds == today() {
		name += " · today"
	}
	head := widget.NewLabelWithStyle(name, fyne.TextAlignLeading,
		fyne.TextStyle{Bold: ds == today()})

	// Left of the line is what the day actually holds and what is queued for it;
	// right of it is what those two come to together — the number the day would
	// score if everything standing on it were pushed.
	banked := "nothing logged"
	if mins > 0 {
		banked = fmt.Sprintf("%d min · %d%%", mins, percentOf(mins))
		if mins >= target {
			banked += " ✓"
		}
	}
	if waiting > 0 {
		banked += fmt.Sprintf(" · +%d min", waiting)
	}
	projected := ""
	if waiting > 0 {
		projected = fmt.Sprintf("%d min · %d%%", mins+waiting, percentOf(mins+waiting))
		if mins+waiting >= target {
			projected += " ✓"
		}
	}
	body := []fyne.CanvasObject{
		head,
		meterBarWithDraft(ui.cfg, byOrg, draftByOrg, target, 8),
		container.New(splitCaption{}, weekCaption(banked), weekCaption(projected)),
	}

	// What is still waiting is the card itself, standing on the day it will be
	// pushed with — every one of them, however long that makes the column. The
	// list above is for the entries that belong to some other week.
	for _, r := range local {
		body = append(body, newDragTile(ui, r, ui.dayRowCard(r, ui.drawRecent)))
	}

	// The frame is kept to hand so a drop target can be lit without rebuilding
	// the column under the pointer that is aiming at it.
	frame := canvas.NewRectangle(color.Transparent)
	frame.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	frame.StrokeWidth = 1
	frame.CornerRadius = cellCornerRadius
	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(0, weekColMinHeight))

	col := newDayColumn(ui, ds, container.NewStack(floor, frame,
		container.NewPadded(container.NewVBox(body...))), frame)
	col.mins, col.waiting = mins, waiting
	col.baseFill = dayFill(mins, waiting)
	frame.FillColor = col.baseFill
	return col
}

// percentOf scores minutes against the daily target.
func percentOf(mins int) int { return int(float64(mins) / float64(target) * 100) }

// dayFill is the colour behind a day. A finished day is green so the week can
// be read without counting: full green once GitHub holds the target, a paler
// green while the day only reaches it by counting what is still waiting.
func dayFill(mins, waiting int) color.Color {
	bg := theme.Color(theme.ColorNameBackground)
	switch {
	case mins >= target:
		return blendColor(bg, theme.Color(theme.ColorNameSuccess), weekDoneTint)
	case mins+waiting >= target:
		return blendColor(bg, theme.Color(theme.ColorNameSuccess), weekDraftTint)
	default:
		return color.Transparent
	}
}

// shortTint is how much warning colour a day worked but left short of the
// target takes. Lighter than the finished green: "not done yet" should not
// shout louder than "done".
const shortTint = 0.18

// shortFill is the wash behind a day that has work on it and did not reach the
// target. It is the same warning colour the chart draws its target line in, so
// "short of 480" looks the same wherever it turns up — a row on the report, a
// cell on the calendar, the line a bar failed to reach.
//
// A day with nothing on it is not short, it is untouched, and painting the two
// alike would make an empty period look like a failed one.
func shortFill() color.Color {
	return blendColor(theme.Color(theme.ColorNameBackground),
		theme.Color(theme.ColorNameWarning), shortTint)
}

// isShortDay reports whether a day was worked and left under the target.
func isShortDay(mins int) bool { return mins > 0 && mins < target }

// isDraftComplete reports whether a day misses the target on the board but makes
// it once the entries saved on this machine are counted.
//
// This is not a short day, and colouring it like one is wrong about the work:
// the hours were done, it is only the sending that is outstanding. Yellow says
// "you owe this day more time", which would send you looking for work you have
// already finished.
func isDraftComplete(mins, draft int) bool {
	return draft > 0 && mins < target && mins+draft >= target
}

// draftDoneFill is the wash behind a day that only reaches the target by
// counting what is waiting to be pushed. Green, at the same weight as the
// short-day yellow it replaces, and paler than the week strip's finished green
// for the same reason that one is: a day finished on GitHub is stated, one
// finished by counting drafts is a claim with a step left in it.
func draftDoneFill() color.Color {
	return blendColor(theme.Color(theme.ColorNameBackground),
		theme.Color(theme.ColorNameSuccess), shortTint)
}

// splitCaption puts one caption at each end of the same line.
//
// Its own minimum is the wider of the two rather than their sum: a day column is
// a seventh of the window, and a row that insisted on both widths at once would
// have widened the strip, the tab, and the window's smallest usable size with it.
type splitCaption struct{}

func (splitCaption) MinSize(objs []fyne.CanvasObject) fyne.Size {
	size := fyne.NewSize(0, 0)
	for _, o := range objs {
		size = size.Max(o.MinSize())
	}
	return size
}

func (splitCaption) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for i, o := range objs {
		m := o.MinSize()
		o.Resize(m)
		x := float32(0)
		if i > 0 { // everything after the first is flush to the right edge
			x = size.Width - m.Width
			if x < 0 {
				x = 0
			}
		}
		o.Move(fyne.NewPos(x, (size.Height-m.Height)/2))
	}
}

func weekCaption(s string) *canvas.Text {
	t := canvas.NewText(s, theme.Color(theme.ColorNamePlaceHolder))
	t.TextSize = theme.CaptionTextSize()
	return t
}

// dayRowCard is a saved entry standing on the day it will be pushed with. It
// carries the same three actions in the same row as the tile in the list, so
// the two read as the same card wherever it happens to be standing.
func (ui *UI) dayRowCard(r Row, refresh func()) fyne.CanvasObject {
	accent := orgColor(ui.cfg, orgOf(r["issue"]))
	title := widget.NewLabelWithStyle(truncate(ui.weekRowTitle(r), weekCardChars),
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Wrapping = fyne.TextWrapWord

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

	push, edit, del := ui.rowActions(r, refresh)
	body := container.NewVBox(title, mins, where,
		container.NewBorder(nil, nil, push, container.NewHBox(edit, del)))

	bg := canvas.NewRectangle(blendColor(
		theme.Color(theme.ColorNameInputBackground), accent, 0.10))
	bg.StrokeColor = blendColor(theme.Color(theme.ColorNameInputBorder), accent, 0.5)
	bg.StrokeWidth = 1
	bg.CornerRadius = 10
	stripe := canvas.NewRectangle(accent)
	stripe.SetMinSize(fyne.NewSize(4, 0))
	stripe.CornerRadius = 2
	return container.NewStack(bg, container.NewPadded(
		container.NewBorder(nil, nil, stripe, nil, container.NewPadded(body))))
}

// weekRowTitle names a locally saved row the same way its tile does.
func (ui *UI) weekRowTitle(r Row) string {
	if info, ok := ui.issueInfo[r["issue"]]; ok && strings.TrimSpace(info.Title) != "" {
		return info.Title
	}
	for _, s := range []string{r["description"], r["remarks"], r["issue"]} {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return "entry"
}

// ---- drop targets ----

// dayColumn is a day in the strip and the thing a dragged card lands on. The
// driver keeps sending hover events to everything except the card being dragged,
// so hovering is what says where a drop would go.
type dayColumn struct {
	widget.BaseWidget
	ui    *UI
	date  string
	body  fyne.CanvasObject
	frame *canvas.Rectangle

	mins     int // already on GitHub
	waiting  int // saved here, standing on this day
	baseFill color.Color
	pulse    *fyne.Animation // running only while a card hovers this day
}

func newDayColumn(ui *UI, date string, body fyne.CanvasObject, frame *canvas.Rectangle) *dayColumn {
	c := &dayColumn{ui: ui, date: date, body: body, frame: frame}
	c.ExtendBaseWidget(c)
	return c
}

func (c *dayColumn) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.body)
}

func (c *dayColumn) MouseIn(*desktop.MouseEvent) {
	c.ui.hoverDay = c.date
	c.ui.markDropTargets()
}

func (c *dayColumn) MouseMoved(*desktop.MouseEvent) {}

func (c *dayColumn) MouseOut() {
	// Only clear what this column set: leaving one column for the next fires the
	// new column's MouseIn first, and blindly clearing would undo it.
	if c.ui.hoverDay == c.date {
		c.ui.hoverDay = ""
		c.ui.markDropTargets()
	}
}

// markDropTargets lights the day the pointer is over, and only while a card is
// actually being dragged — otherwise every passing mouse would look like a drop.
func (ui *UI) markDropTargets() {
	for ds, col := range ui.weekCols {
		if ui.dragRow != nil && ds == ui.hoverDay {
			col.frame.StrokeColor = theme.Color(theme.ColorNamePrimary)
			col.frame.StrokeWidth = 2
			col.frame.Refresh()
			col.startPulse(ui.dragRow.Minutes())
			continue
		}
		col.stopPulse()
		if col.frame.StrokeColor == theme.Color(theme.ColorNameInputBorder) &&
			col.frame.StrokeWidth == 1 {
			continue
		}
		col.frame.StrokeColor = theme.Color(theme.ColorNameInputBorder)
		col.frame.StrokeWidth = 1
		col.frame.Refresh()
	}
}

// pulsePeriod is one breath of the day under a carried card. Slow enough to read
// as the day answering, fast enough to answer before the card is let go.
const pulsePeriod = 700 * time.Millisecond

// startPulse breathes the day the card is being held over between its own colour
// and the colour it would take if the card were dropped there — green when those
// minutes would finish the day. The drop is a guess until it lands, so the
// preview is shown as a pulse rather than painted on as though it already had.
func (c *dayColumn) startPulse(dropping int) {
	if c.pulse != nil {
		return // already breathing for this same card
	}
	bg := theme.Color(theme.ColorNameBackground)
	// Green only where the drop would actually finish the day. Otherwise the day
	// still lifts, towards the accent rather than towards done — a card has to
	// look like it can land somewhere even when landing there settles nothing.
	to := blendColor(bg, theme.Color(theme.ColorNamePrimary), weekDraftTint)
	if c.mins+c.waiting+dropping >= target {
		to = blendColor(bg, theme.Color(theme.ColorNameSuccess), weekDoneTint)
	}
	c.pulse = canvas.NewColorRGBAAnimation(c.baseFill, to, pulsePeriod, func(col color.Color) {
		c.frame.FillColor = col
		c.frame.Refresh()
	})
	c.pulse.AutoReverse = true
	c.pulse.RepeatCount = fyne.AnimationRepeatForever
	c.pulse.Start()
}

// stopPulse puts the day back to the colour it earned on its own.
func (c *dayColumn) stopPulse() {
	if c.pulse == nil {
		return
	}
	c.pulse.Stop()
	c.pulse = nil
	c.frame.FillColor = c.baseFill
	c.frame.Refresh()
}

// ---- the card in the air ----

// The card being carried is drawn in a layer of its own above the whole tab,
// rather than by moving the card itself. A moved card is still a child of the
// day it came from, so dragging it rightwards slid it *behind* the column it was
// aimed at — painted over by a day drawn after its own.

func (ui *UI) showGhost(r Row) {
	if ui.dragLayer == nil {
		return
	}
	label := canvas.NewText(fmt.Sprintf("%s · %dm",
		truncate(ui.weekRowTitle(r), 24), r.Minutes()), theme.Color(theme.ColorNameForeground))
	label.TextSize = theme.CaptionTextSize()

	bg := canvas.NewRectangle(blendColor(theme.Color(theme.ColorNameInputBackground),
		orgColor(ui.cfg, orgOf(r["issue"])), 0.35))
	bg.CornerRadius = 8
	bg.StrokeColor = theme.Color(theme.ColorNamePrimary)
	bg.StrokeWidth = 1

	ghost := container.NewStack(bg, container.NewPadded(label))
	ghost.Resize(ghost.MinSize()) // nothing lays this out; it sizes itself
	ui.dragGhost = ghost
	ui.dragLayer.Objects = []fyne.CanvasObject{ghost}
	ui.dragLayer.Refresh()
}

// moveGhost puts the carried card under the pointer. The position is canvas-wide
// and the layer is not, so it is taken back to the layer's own origin.
func (ui *UI) moveGhost(at fyne.Position) {
	if ui.dragGhost == nil || ui.dragLayer == nil {
		return
	}
	origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(ui.dragLayer)
	// Below and right of the pointer, so the card never covers the day it is
	// being aimed at.
	ui.dragGhost.Move(at.Subtract(origin).Add(fyne.NewPos(12, 12)))
}

func (ui *UI) hideGhost() {
	if ui.dragLayer == nil {
		return
	}
	ui.dragGhost = nil
	ui.dragLayer.Objects = nil
	ui.dragLayer.Refresh()
}

// moveRowToDay is the drop: the row keeps everything else and changes its date,
// which is the date the Push button will send.
func (ui *UI) moveRowToDay(r Row, date string) {
	if r["id"] == "" || date == "" || date == r["date"] {
		return
	}
	if err := ui.store.UpdateRow(r["id"], Row{"date": date}); err != nil {
		ui.errf(err)
		return
	}
	ui.drawRecent() // redraws the strip too, so the card appears on its new day
}

// ---- the card being dragged ----

// dragTile wraps a saved entry so it can be picked up and dropped on a day. Only
// unpushed entries are wrapped: a pushed one is already a fact on GitHub, and
// moving it here would only disagree with the board.
type dragTile struct {
	widget.BaseWidget
	ui      *UI
	row     Row
	content fyne.CanvasObject
	lifted  bool
}

func newDragTile(ui *UI, r Row, content fyne.CanvasObject) *dragTile {
	t := &dragTile{ui: ui, row: r, content: content}
	t.ExtendBaseWidget(t)
	return t
}

func (t *dragTile) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.content)
}

func (t *dragTile) Dragged(e *fyne.DragEvent) {
	if !t.lifted {
		t.lifted = true
		t.ui.dragRow = t.row
		t.ui.showGhost(t.row)
		t.ui.markDropTargets()
	}
	t.ui.moveGhost(e.AbsolutePosition)
}

func (t *dragTile) DragEnd() {
	if !t.lifted {
		return
	}
	t.lifted = false
	t.ui.hideGhost()

	day := t.ui.hoverDay
	t.ui.dragRow = nil
	t.ui.hoverDay = ""
	t.ui.markDropTargets()
	// Dropped on nothing, or on the day it already sits on: the card just goes
	// back where it came from.
	t.ui.moveRowToDay(t.row, day)
}
