package main

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
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// issueLink names an issue as a link where there is somewhere to link to, and
// as plain text where there is not: a meeting that has not been pushed has a
// title and no issue behind it yet.
func (ui *UI) issueLink(issue, title string) fyne.CanvasObject {
	if owner, repo, num, err := splitIssue(issue); err == nil {
		if u, e := url.Parse(fmt.Sprintf(
			"https://github.com/%s/%s/issues/%d", owner, repo, num)); e == nil {
			link := widget.NewHyperlink(title, u)
			link.Truncation = fyne.TextTruncateEllipsis
			return link
		}
	}
	lbl := widget.NewLabel(title)
	lbl.Truncation = fyne.TextTruncateEllipsis
	return lbl
}

// The Log List tab answers two questions side by side: what the week already
// holds on the board, and what has not got there yet.
//
// They were one column on the Log work tab, under the day strip, which put the
// week's summary at the bottom of a tab whose whole job is entering work — you
// had to scroll past the thing you were doing to see what you had done. Here
// they sit together, and the pending column beside the banked one makes the gap
// between them the point rather than something to work out.

func (ui *UI) buildLogListTab() fyne.CanvasObject {
	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		ui.weekStart = shiftWeek(ui.weekStart, -1)
		ui.drawWeekStrip() // shared week: the strip on Log work follows this
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		ui.weekStart = shiftWeek(ui.weekStart, 1)
		ui.drawWeekStrip()
	})
	here := widget.NewButton("This week", func() {
		ui.weekStart = weekStartOf(today())
		ui.drawWeekStrip()
	})
	refresh := widget.NewButtonWithIcon("Refresh from GitHub", theme.ViewRefreshIcon(), func() {
		ui.loadPending(true)
		ui.refreshTodayScore()
	})
	mail := widget.NewButtonWithIcon("Export week for email", theme.MailComposeIcon(), func() {
		days := weekDates(orDefault(ui.weekStart, weekStartOf(today())))
		byDay, state := ui.weekWorklogs(days)
		ui.exportWeekForEmail(days, byDay, state)
	})

	ui.listTitle = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	ui.listHere = here
	head := container.NewHBox(prev, ui.listTitle, next, here,
		layout.NewSpacer(), refresh, mail)

	ui.listBox = container.NewVBox()
	ui.listBody = container.NewVBox(head, ui.listBox)
	return container.NewVScroll(widget.NewCard("", "", ui.listBody))
}

func (ui *UI) drawLogList() {
	if ui.listBox == nil {
		return
	}
	weekStart := orDefault(ui.weekStart, weekStartOf(today()))
	days := weekDates(weekStart)
	from, to := days[0], days[len(days)-1]

	ui.listTitle.SetText(weekRangeLabel(weekStart))
	if ui.listHere != nil {
		if weekStart == weekStartOf(today()) {
			ui.listHere.Disable()
		} else {
			ui.listHere.Enable()
		}
	}

	byDay, state := ui.weekWorklogs(days)
	var weekItems []WorklogItem
	for _, d := range days {
		weekItems = append(weekItems, byDay[d]...)
	}

	// Side by side, equal width: neither column is the footnote of the other.
	columns := container.NewGridWithColumns(2,
		ui.weekIssuesPanel(weekItems, state),
		ui.pendingIssuesPanel(),
	)

	ui.listBox.Objects = []fyne.CanvasObject{
		ui.weekProgressPanel(days, weekItems),
		ui.listDaysPanel(days, byDay),
		widget.NewSeparator(),
		ui.weekBoardsPanel(from, to),
		widget.NewSeparator(),
		columns,
	}
	ui.listBox.Refresh()
	relayout(ui.listBody)
}

// listDayMinHeight is the floor for a day on this tab. Shorter than the strip's,
// because these columns hold no cards — only the score — and a column sized for
// cards it will never carry is a band of empty across the top of the tab.
const listDayMinHeight = 92

// listDaysPanel is the week read a day at a time, in the same colours the strip
// on Log Work uses: green behind a day the board has finished, a paler green
// where it only gets there by counting drafts, the org split in the bar, and the
// draft yellow on the end of it.
//
// Read-only, and that is the difference between the two. The strip is where
// entries are dragged onto days, so its columns are drop targets that grow to
// hold the cards standing on them; here nothing is dragged and nothing stands,
// so the same reading fits in a band a third the height. One glance says which
// day of the week still owes time.
func (ui *UI) listDaysPanel(days []string, byDay map[string][]WorklogItem) fyne.CanvasObject {
	localByDay := map[string][]Row{}
	if rows, err := ui.store.ReadRows(); err == nil {
		for _, r := range ui.shownRows(rows) {
			if r["pushed_at"] == "" {
				localByDay[r["date"]] = append(localByDay[r["date"]], r)
			}
		}
	}
	grid := container.NewGridWithColumns(len(days))
	for _, ds := range days {
		grid.Add(ui.listDayColumn(ds, byDay[ds], localByDay[ds]))
	}
	return grid
}

// listDayColumn is one day: how full it is, who the minutes were for, and what
// it would come to if everything waiting on it were pushed.
func (ui *UI) listDayColumn(ds string, items []WorklogItem, local []Row) fyne.CanvasObject {
	t, _ := time.Parse("2006-01-02", ds)
	byOrg := minutesByOrg(items)
	mins := 0
	for _, v := range byOrg {
		mins += v
	}
	draftByOrg := map[string]int{}
	waiting := 0
	for _, r := range local {
		draftByOrg[rowOrg(r)] += r.Minutes()
		waiting += r.Minutes()
	}

	name := fmt.Sprintf("%s %d", t.Format("Mon"), t.Day())
	if ds == today() {
		name += " · today"
	}
	head := widget.NewLabelWithStyle(name, fyne.TextAlignLeading,
		fyne.TextStyle{Bold: ds == today()})

	// Left of the line is what the day holds and what is queued for it; right of
	// it is what the two come to together — the same split the strip uses, so a
	// day reads the same on either tab.
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

	frame := canvas.NewRectangle(dayFill(mins, waiting))
	frame.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	frame.StrokeWidth = 1
	frame.CornerRadius = cellCornerRadius
	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(0, listDayMinHeight))

	body := container.NewVBox(
		head,
		meterBarWithDraft(ui.cfg, byOrg, draftByOrg, target, 8),
		container.New(splitCaption{}, weekCaption(banked), weekCaption(projected)),
	)
	return container.NewStack(floor, frame, container.NewPadded(body))
}

// weekProgressPanel is the week scored the way a day is scored on the strip:
// one bar, split by org, with what is saved but not pushed carried on the end in
// the draft yellow.
//
// The strip answers "how full is each day"; this answers "how full is the week",
// which is the number the weekly mail is written around and the one that was
// only ever available by adding seven columns up by eye.
//
// Weekdays only in the goal. A week is seven days on the strip because work
// lands on weekends, but nobody is owed 480 minutes on a Sunday, and scoring
// against 7 × 480 made a finished week read as two-thirds done.
func (ui *UI) weekProgressPanel(days []string, items []WorklogItem) fyne.CanvasObject {
	byOrg := minutesByOrg(items)
	banked := 0
	for _, m := range byOrg {
		banked += m
	}

	draftByOrg := map[string]int{}
	waiting := 0
	if rows, err := ui.store.ReadRows(); err == nil {
		inWeek := map[string]bool{}
		for _, d := range days {
			inWeek[d] = true
		}
		for _, r := range ui.shownRows(rows) {
			if r["pushed_at"] != "" || !inWeek[r["date"]] {
				continue
			}
			draftByOrg[rowOrg(r)] += r.Minutes()
			waiting += r.Minutes()
		}
	}

	goal := weekGoal(days)
	line := fmt.Sprintf("%d min · %s of %s", banked, hoursMins(banked), hoursMins(goal))
	if goal > 0 {
		line += fmt.Sprintf(" · %d%%", banked*100/goal)
	}
	if banked >= goal && goal > 0 {
		line += " ✓"
	}
	if waiting > 0 {
		// What the week would score if everything saved here went up. Without
		// it the drafts were a number with nothing to measure against, and the
		// question they actually raise — does this week get there once I push —
		// still had to be done in your head.
		projected := banked + waiting
		line += fmt.Sprintf("   ·   +%d min waiting · %d/%d min", waiting, projected, goal)
		// How much the week would still owe after the push. Only while it owes
		// something: "(0m left)" beside a tick is the same fact twice.
		if left := goal - projected; left > 0 {
			line += fmt.Sprintf(" (%dm left)", left)
		}
		if goal > 0 {
			line += fmt.Sprintf(" %d%%", projected*100/goal)
		}
		if projected >= goal && goal > 0 {
			line += " ✓"
		}
	}

	return container.NewVBox(
		container.NewHBox(bold("This week"), widget.NewLabelWithStyle(line,
			fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})),
		meterBarWithDraft(ui.cfg, byOrg, draftByOrg, goal, 10),
	)
}

// weekGoal is what the week is measured against: the daily target for every day
// in it, weekends included — 8 hours × 7 = 56 for a full week.
//
// Weekends count because on this account they are worked: weekend support is a
// paid rota with its own bonus, and a Saturday's minutes go on the board like
// any other day's. Scoring against weekdays alone let a week that included a
// support shift read as over 100%, which says the week is more than done when
// what actually happened is that the goal was too small.
//
// Read off the dates rather than assumed to be seven, so a part-week scores
// against the days it is actually showing.
func weekGoal(days []string) int {
	goal := 0
	for _, d := range days {
		if _, err := time.Parse("2006-01-02", d); err == nil {
			goal += target
		}
	}
	return goal
}

// pendingIssue is one issue's share of everything not on the board yet.
type pendingIssue struct {
	Issue   string
	Title   string
	Commits int // unlogged commits waiting to be turned into an entry
	Minutes int // saved locally against this issue and not pushed
	Entries int
}

// pendingIssuesPanel is the right-hand column: what is still owed, per issue.
//
// Deliberately not filtered to the shown week. Pending work is pending whenever
// it happened, and a week-scoped version would hide the commit from last
// Thursday that is the whole reason to look — the point of this column is that
// nothing waiting can go unnoticed, which a filter would undo.
func (ui *UI) pendingIssuesPanel() fyne.CanvasObject {
	rows := []fyne.CanvasObject{bold("Pending — not on the board yet")}

	pend := ui.pendingByIssue()
	if len(pend) == 0 {
		note := "Nothing waiting."
		if !ui.pendingLoaded {
			note = "Reading your commits from GitHub…"
		}
		rows = append(rows, widget.NewLabel(note))
		return container.NewVBox(rows...)
	}

	commits, mins := 0, 0
	for _, p := range pend {
		commits += p.Commits
		mins += p.Minutes
	}
	rows = append(rows, widget.NewLabelWithStyle(pendingTotalLine(commits, mins, len(pend)),
		fyne.TextAlignLeading, fyne.TextStyle{Italic: true}))

	for _, p := range pend {
		// The amount reads in whatever unit that issue actually has: minutes
		// once an entry has been written, a commit count while it has not.
		amount := widget.NewLabelWithStyle(pendingAmount(p),
			fyne.TextAlignTrailing, fyne.TextStyle{Monospace: true})
		name := ui.issueLink(p.Issue, p.Title)
		rows = append(rows, container.New(newRatioRow(0.28, 0.72), amount, name))
	}
	return container.NewVBox(rows...)
}

func pendingTotalLine(commits, mins, issues int) string {
	var parts []string
	if mins > 0 {
		parts = append(parts, fmt.Sprintf("%d min saved", mins))
	}
	if commits > 0 {
		parts = append(parts, fmt.Sprintf("%d commit%s unlogged", commits, plural(commits)))
	}
	return fmt.Sprintf("%s across %d issue%s", strings.Join(parts, " · "), issues, plural(issues))
}

// pendingAmount says what an issue is waiting on, shortest true thing first.
func pendingAmount(p pendingIssue) string {
	switch {
	case p.Minutes > 0 && p.Commits > 0:
		return fmt.Sprintf("%dm +%dc", p.Minutes, p.Commits)
	case p.Minutes > 0:
		return strconv.Itoa(p.Minutes) + "m"
	default:
		return fmt.Sprintf("%d commit%s", p.Commits, plural(p.Commits))
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// pendingByIssue merges the two kinds of waiting work into one list per issue:
// entries saved here and not pushed, and commits with no entry written yet.
//
// One list rather than two, because they are one question — what does this issue
// still owe the board — and an issue often has both: half the week written up
// and this morning's commits not yet.
//
// Minutes first, then commit count, so the issues nearest to being pushable sit
// at the top.
func (ui *UI) pendingByIssue() []pendingIssue {
	byIssue := map[string]*pendingIssue{}
	get := func(issue string) *pendingIssue {
		if _, ok := byIssue[issue]; !ok {
			byIssue[issue] = &pendingIssue{Issue: issue}
		}
		return byIssue[issue]
	}

	if rows, err := ui.store.ReadRows(); err == nil {
		for _, r := range ui.shownRows(rows) {
			if r["pushed_at"] != "" {
				continue
			}
			// A meeting or an independent entry has no ref until it is pushed;
			// it is named by what it will become rather than lumped under "".
			issue := strings.TrimSpace(r["issue"])
			if issue == "" {
				issue = ui.unfiledLabel(r)
			}
			p := get(issue)
			p.Minutes += r.Minutes()
			p.Entries++
		}
	}

	for _, g := range ui.pending.Groups {
		if g.Ignored {
			continue
		}
		issue := strings.TrimSpace(g.Issue)
		if issue == "" {
			issue = "(no issue ref)"
		}
		get(issue).Commits += len(g.Commits)
	}

	out := make([]pendingIssue, 0, len(byIssue))
	for _, p := range byIssue {
		if info, ok := ui.issueInfo[p.Issue]; ok {
			p.Title = strings.TrimSpace(info.Title)
		}
		if p.Title == "" {
			p.Title = p.Issue
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Minutes != out[j].Minutes {
			return out[i].Minutes > out[j].Minutes
		}
		if out[i].Commits != out[j].Commits {
			return out[i].Commits > out[j].Commits
		}
		return out[i].Title < out[j].Title
	})
	return out
}

// unfiledLabel names a row whose issue is only decided when it is pushed, so it
// is grouped as the thing it will become rather than as nothing.
func (ui *UI) unfiledLabel(r Row) string {
	switch r["type"] {
	case kindMeeting:
		return meetingTitle(ui.cfg, r["date"])
	case kindIndependent:
		if repo := strings.TrimSpace(r["parent_repo"]); repo != "" {
			return "new issue in " + repo
		}
	}
	return "(no issue ref)"
}
