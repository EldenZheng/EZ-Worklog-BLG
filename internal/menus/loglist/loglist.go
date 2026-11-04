package loglist

import (
	"fmt"
	"image/color"
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

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// The Log List tab answers two questions side by side: what the week already
// holds on the board, and what has not got there yet.
//
// They were one column on the Log work tab, under the day strip, which put the
// week's summary at the bottom of a tab whose whole job is entering work — you
// had to scroll past the thing you were doing to see what you had done. Here
// they sit together, and the pending column beside the banked one makes the gap
// between them the point rather than something to work out.

func BuildLogListTab(ui *shell.UI) fyne.CanvasObject {
	// The support placeholder is saved per week, so walking shows whatever the
	// week arrived at has ticked rather than carrying one week's into another.
	walkTo := func(start string) {
		ui.WeekStart = start
		shell.Hooks.DrawWeekStrip(ui) // shared week: the strip on Log work follows this
	}
	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		walkTo(shell.ShiftWeek(ui.WeekStart, -1))
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		walkTo(shell.ShiftWeek(ui.WeekStart, 1))
	})
	here := widget.NewButton("This week", func() { walkTo(backend.WeekStartOf(shell.Today())) })
	refresh := widget.NewButtonWithIcon("Refresh from GitHub", theme.ViewRefreshIcon(), func() {
		ui.LoadPending(true)
		ui.RefreshTodayScore()
	})
	mail := widget.NewButtonWithIcon("Export week", theme.MailComposeIcon(), func() {
		days := shell.WeekDates(backend.OrDefault(ui.WeekStart, backend.WeekStartOf(shell.Today())))
		byDay, state := shell.Hooks.WeekWorklogs(ui, days)
		shell.Hooks.ExportWeekForEmail(ui, days, byDay, state)
	})

	// Same checkbox as Log Work, on the tab the week is read from. Display only,
	// no entry written, and cleared when the week walks off — the flag is one
	// piece of state shared between tabs, so ticking either one lights up both.
	support := widget.NewCheck(
		fmt.Sprintf("+%d min weekday support (display only)", backend.SupportPlaceholderMins), nil)
	support.SetChecked(shell.Hooks.SupportOn(ui, backend.OrDefault(ui.WeekStart, backend.WeekStartOf(shell.Today()))))
	support.OnChanged = func(on bool) {
		shell.Hooks.SetSupport(ui, on, backend.OrDefault(ui.WeekStart, backend.WeekStartOf(shell.Today())))
		shell.Hooks.DrawWeekStrip(ui)
	}
	ui.ListSupport = support
	ui.ListSupportView = shell.SupportChoice(support, false)

	ui.ListTitle = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	ui.ListHere = here
	// Support sits on the left, before the spacer: on a smaller window the
	// trailing buttons can push the right-hand end out to where the scroll
	// clipped it, taking the checkbox with them if it stood over there.
	head := container.NewHBox(prev, ui.ListTitle, next, here, ui.ListSupportView.View,
		layout.NewSpacer(), refresh, mail)

	ui.ListBox = container.NewStack()
	ui.ListProgress = shell.NewLoadingIndicator()
	ui.ListBody = container.NewBorder(container.NewVBox(head, ui.ListProgress.View), nil, nil, nil, ui.ListBox)
	return shell.PageInset(ui.ListBody)
}

func DrawLogList(ui *shell.UI) {
	shell.DismissSummaryTooltip()
	if ui.ListBox == nil {
		return
	}
	weekStart := backend.OrDefault(ui.WeekStart, backend.WeekStartOf(shell.Today()))
	days := shell.WeekDates(weekStart)
	from, to := days[0], days[len(days)-1]

	ui.ListTitle.SetText(shell.WeekRangeLabel(weekStart))
	if ui.ListHere != nil {
		if weekStart == backend.WeekStartOf(shell.Today()) {
			ui.ListHere.Disable()
		} else {
			ui.ListHere.Enable()
		}
	}
	// Mirror the flag onto this tab's checkbox when the change came from the
	// other one. The handler is unhooked around SetChecked because it would
	// otherwise fire drawWeekStrip and loop back through here.
	if on := shell.Hooks.SupportOn(ui, weekStart); ui.ListSupport != nil && ui.ListSupport.Checked != on {
		prev := ui.ListSupport.OnChanged
		ui.ListSupport.OnChanged = nil
		ui.ListSupport.SetChecked(on)
		ui.ListSupport.OnChanged = prev
	}

	byDay, state := shell.Hooks.WeekWorklogs(ui, days)
	var weekItems []backend.WorklogItem
	for _, d := range days {
		weekItems = append(weekItems, byDay[d]...)
	}
	if ui.ListSupportView != nil {
		ui.ListSupportView.Set(shell.OnSupportRota(ui.Cfg, weekItems))
	}

	// Side by side, equal width: neither column is the footnote of the other.
	columns := container.NewGridWithColumns(2,
		shell.Hooks.WeekIssuesPanel(ui, weekItems, state),
		pendingIssuesPanel(ui),
	)

	ui.ListBox.Objects = []fyne.CanvasObject{
		container.NewBorder(
			container.NewVBox(weekProgressPanel(ui, days, byDay, weekItems), listDaysPanel(ui, days, byDay), shell.Hooks.WeekBoardsPanel(ui, from, to)),
			nil, nil, nil, columns),
	}
	ui.ListBox.Refresh()
	shell.Relayout(ui.ListBody)
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
func listDaysPanel(ui *shell.UI, days []string, byDay map[string][]backend.WorklogItem) fyne.CanvasObject {
	localByDay := map[string][]backend.Row{}
	if rows, err := ui.Store.ReadRows(); err == nil {
		for _, r := range ui.ShownRows(rows) {
			if r["pushed_at"] == "" {
				localByDay[r["date"]] = append(localByDay[r["date"]], r)
			}
		}
	}
	grid := container.NewGridWithColumns(len(days))
	for _, ds := range days {
		grid.Add(listDayColumn(ui, ds, byDay[ds], localByDay[ds]))
	}
	return grid
}

// listDayColumn is one day: how full it is, who the minutes were for, and what
// it would come to if everything waiting on it were pushed.
func listDayColumn(ui *shell.UI, ds string, items []backend.WorklogItem, local []backend.Row) fyne.CanvasObject {
	t, _ := time.Parse("2006-01-02", ds)
	byOrg := shell.MinutesByOrg(items)
	mins := 0
	for _, v := range byOrg {
		mins += v
	}
	draftByOrg := map[string]int{}
	waiting := 0
	for _, r := range local {
		draftByOrg[shell.RowOrg(r)] += r.Minutes()
		waiting += r.Minutes()
	}
	// The support shift pencilled in — same rule as the strip, same yellow: neither
	// this nor the drafts are on the board yet, and both should count the same way
	// against the day's score.
	placeholder := shell.SupportPlaceholderFor(ui.Cfg, shell.Hooks.SupportOn(ui, ds), ds, items)
	if placeholder > 0 {
		draftByOrg[strings.ToLower(shell.OrgOf(ui.Cfg.WorklogOwner))] += placeholder
		waiting += placeholder
	}

	name := fmt.Sprintf("%s %d", t.Format("Mon"), t.Day())
	if ds == shell.Today() {
		name += " · Today"
	}
	var head fyne.CanvasObject = widget.NewLabelWithStyle(name, fyne.TextAlignLeading,
		fyne.TextStyle{Bold: ds == shell.Today()})
	if ds == shell.Today() {
		// Today is outlined and titled in gold, as on Log Work and the
		// Commit List.
		head = container.NewThemeOverride(head, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	}

	// Left of the line is what the day holds and what is queued for it; right of
	// it is what the two come to together — the same split the strip uses, so a
	// day reads the same on either tab.
	banked := "nothing logged"
	if mins > 0 {
		banked = fmt.Sprintf("%d min · %d%%", mins, shell.PercentOf(mins))
		if mins >= backend.Target {
			banked += " ✓"
		}
	}
	if waiting > 0 {
		banked += fmt.Sprintf(" · +%d min", waiting)
		if placeholder > 0 {
			banked += " (inc. support)"
		}
	}
	projected := ""
	if waiting > 0 {
		projected = fmt.Sprintf("%d min · %d%%", mins+waiting, shell.PercentOf(mins+waiting))
		if mins+waiting >= backend.Target {
			projected += " ✓"
		}
	}

	// Green once logged + drafts + support + plans reach the target, like Log
	// Work and the Sprint Calendar; the captions still read logged and drafts.
	frame := canvas.NewRectangle(ui.DayTotalFill(ds, items, local))
	frame.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	frame.StrokeWidth = 1
	if ds == shell.Today() {
		frame.StrokeColor, frame.StrokeWidth = widgets.LogoGold, 2
	}
	frame.CornerRadius = shell.CellCornerRadius
	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(0, listDayMinHeight))

	body := container.NewVBox(
		head,
		ui.ProgressBarParts("list/"+t.Format("Mon"), ds, shell.PlannedBarParts(ui.Cfg, byOrg, draftByOrg, shell.Hooks.PlannedMinutes(ui, ds, items, local)), 8),
		container.New(shell.SplitCaption{}, shell.WeekCaption(banked), shell.WeekCaption(projected)),
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
func weekProgressPanel(ui *shell.UI, days []string, byDay map[string][]backend.WorklogItem, items []backend.WorklogItem) fyne.CanvasObject {
	byOrg := shell.MinutesByOrg(items)
	banked := 0
	for _, m := range byOrg {
		banked += m
	}

	draftByOrg := map[string]int{}
	waiting := 0
	if rows, err := ui.Store.ReadRows(); err == nil {
		inWeek := map[string]bool{}
		for _, d := range days {
			inWeek[d] = true
		}
		for _, r := range ui.ShownRows(rows) {
			if r["pushed_at"] != "" || !inWeek[r["date"]] {
				continue
			}
			draftByOrg[shell.RowOrg(r)] += r.Minutes()
			waiting += r.Minutes()
		}
	}
	// Support pencilled onto every eligible weekday in the week: same rule as the
	// strip and the day columns, so the week total agrees with the days that sum
	// into it.
	placeholder := 0
	for _, d := range days {
		if m := shell.SupportPlaceholderFor(ui.Cfg, shell.Hooks.SupportOn(ui, d), d, byDay[d]); m > 0 {
			placeholder += m
			draftByOrg[strings.ToLower(shell.OrgOf(ui.Cfg.WorklogOwner))] += m
		}
	}
	waiting += placeholder

	goal := WeekGoal(days)
	line := fmt.Sprintf("%d min · %s of %s", banked, backend.HoursMins(banked), backend.HoursMins(goal))
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

	summary := widget.NewLabelWithStyle(line, fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	summary.Wrapping = fyne.TextWrapWord
	normal := widget.NewLabel(normalWeekProgress(banked, waiting))
	normal.Wrapping = fyne.TextWrapWord
	bar := ui.ProgressBar("week", strings.Join(days, ","), byOrg, draftByOrg, goal, 10)
	return container.NewVBox(shell.Bold("This week"), summary, normal,
		normalWeekMarker(bar, max(goal, banked+waiting)))
}

// WeekGoal is what the week is measured against: the daily target for every day
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
func WeekGoal(days []string) int {
	goal := 0
	for _, d := range days {
		if _, err := time.Parse("2006-01-02", d); err == nil {
			goal += backend.Target
		}
	}
	return goal
}

// PendingIssue is one issue's share of everything not on the board yet.
type PendingIssue struct {
	Issue   string
	Title   string
	Commits int // unlogged commits waiting to be turned into an entry
	Minutes int // saved locally against this issue and not pushed
	Entries int
	Rows    []backend.Row
	Groups  []backend.Group
}

// pendingIssuesPanel is the right-hand column: what is still owed, per issue.
//
// Deliberately not filtered to the shown week. Pending work is pending whenever
// it happened, and a week-scoped version would hide the commit from last
// Thursday that is the whole reason to look — the point of this column is that
// nothing waiting can go unnoticed, which a filter would undo.
func pendingIssuesPanel(ui *shell.UI) fyne.CanvasObject {
	const title = "Pending \u2014 not on the board yet"
	pend := PendingByIssue(ui)
	if len(pend) == 0 {
		note := "Nothing waiting."
		if !ui.PendingLoaded {
			note = "Reading your commits from GitHub\u2026"
		}
		return shell.IssueSummaryPanel(title, note, container.NewVBox())
	}
	commits, mins := 0, 0
	cards := make([]fyne.CanvasObject, 0, len(pend))
	for _, p := range pend {
		commits += p.Commits
		mins += p.Minutes
		card := shell.IssueSummaryCard(ui.Cfg, p.Issue, p.Title, PendingAmount(p), func() {
			shell.Hooks.OpenPendingIssue(ui, p.Rows, p.Groups)
		})
		cards = append(cards, card)
	}
	return shell.IssueSummaryPanel(title, pendingTotalLine(commits, mins, len(pend)), shell.IssueSummaryGrid(cards...))
}

func pendingTotalLine(commits, mins, issues int) string {
	var parts []string
	if mins > 0 {
		parts = append(parts, fmt.Sprintf("%d min saved", mins))
	}
	if commits > 0 {
		parts = append(parts, fmt.Sprintf("%d commit%s unlogged", commits, shell.Plural(commits)))
	}
	return fmt.Sprintf("%s across %d issue%s", strings.Join(parts, " · "), issues, shell.Plural(issues))
}

// PendingAmount says what an issue is waiting on, shortest true thing first.
func PendingAmount(p PendingIssue) string {
	switch {
	case p.Minutes > 0 && p.Commits > 0:
		return fmt.Sprintf("%dm +%dc", p.Minutes, p.Commits)
	case p.Minutes > 0:
		return strconv.Itoa(p.Minutes) + "m"
	default:
		return fmt.Sprintf("%d commit%s", p.Commits, shell.Plural(p.Commits))
	}
}

// PendingByIssue merges the two kinds of waiting work into one list per issue:
// entries saved here and not pushed, and commits with no entry written yet.
//
// One list rather than two, because they are one question — what does this issue
// still owe the board — and an issue often has both: half the week written up
// and this morning's commits not yet.
//
// Minutes first, then commit count, so the issues nearest to being pushable sit
// at the top.
func PendingByIssue(ui *shell.UI) []PendingIssue {
	byIssue := map[string]*PendingIssue{}
	get := func(issue string) *PendingIssue {
		if _, ok := byIssue[issue]; !ok {
			byIssue[issue] = &PendingIssue{Issue: issue}
		}
		return byIssue[issue]
	}

	if rows, err := ui.Store.ReadRows(); err == nil {
		for _, r := range ui.ShownRows(rows) {
			if r["pushed_at"] != "" {
				continue
			}
			// A meeting or an independent entry has no ref until it is pushed;
			// it is named by what it will become rather than lumped under "".
			issue := strings.TrimSpace(r["issue"])
			if issue == "" {
				issue = ui.UnfiledLabel(r)
				if issue == "(no issue ref)" {
					issue = backend.OrDefault(r["description"], backend.OrDefault(r["type"], "Saved work")) + " · " + r["date"]
				}
			}
			p := get(issue)
			p.Minutes += r.Minutes()
			p.Entries++
			p.Rows = append(p.Rows, r)
		}
	}

	for _, g := range ui.Pending.Groups {
		if g.Ignored {
			continue
		}
		issue := strings.TrimSpace(g.Issue)
		if issue == "" {
			issue = "Unfiled commits · " + g.Date
			if len(g.Commits) > 0 {
				issue = backend.CommitSubject(g.Commits[0]) + " · " + g.Date
			}
		}
		p := get(issue)
		p.Commits += len(g.Commits)
		p.Groups = append(p.Groups, g)
	}

	out := make([]PendingIssue, 0, len(byIssue))
	for _, p := range byIssue {
		if info, ok := ui.IssueInfo[p.Issue]; ok {
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
