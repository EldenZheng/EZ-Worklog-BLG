package main

import (
	"fmt"
	"image/color"
	"sort"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The Commit List tab is the week read from the other end. Log List asks "what
// have I logged"; this asks "what have I committed", which is a question that
// used to require jumping to GitHub for. Same week strip so both tabs move
// together, seven day columns underneath, and each column is the day's commits
// bucketed by the parent issue they touch.
//
// Bubbles look like Log Work's pending tiles but read-only: the sha is a
// hyperlink to the commit, the issue heading is a hyperlink to the issue, and
// nothing here writes to the worklog. Turning a commit into an entry still
// happens on Log Work.

func (ui *UI) buildCommitListTab() fyne.CanvasObject {
	walkTo := func(start string) {
		ui.weekStart = start
		ui.drawWeekStrip() // shared with Log Work
		ui.loadWeekCommits(false)
		ui.drawCommitList()
	}
	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		walkTo(shiftWeek(ui.weekStart, -1))
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		walkTo(shiftWeek(ui.weekStart, 1))
	})
	here := widget.NewButton("This week", func() { walkTo(weekStartOf(today())) })
	refresh := widget.NewButtonWithIcon("Refresh from GitHub", theme.ViewRefreshIcon(), func() {
		ui.loadWeekCommits(true)
	})

	ui.commitTitle = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	ui.commitHere = here
	head := container.NewHBox(prev, ui.commitTitle, next, here, layout.NewSpacer(), refresh)

	// Border layout so the day grid stretches to fill the tab — GridWithColumns
	// splits width evenly, and each cell's VScroll then absorbs the tab's height.
	// Without Border the VBox around commitBox let each child claim only its
	// natural height and the grid never stretched.
	ui.commitBox = container.NewStack()
	ui.commitProgress = newLoadingIndicator()
	ui.commitBody = container.NewBorder(container.NewVBox(head, ui.commitProgress.view), nil, nil, nil, ui.commitBox)
	return ui.commitBody
}

func (ui *UI) drawCommitList() {
	defer ui.syncLoading()
	if ui.commitBox == nil {
		return
	}
	weekStart := orDefault(ui.weekStart, weekStartOf(today()))
	days := weekDates(weekStart)

	ui.commitTitle.SetText(weekRangeLabel(weekStart))
	if ui.commitHere != nil {
		if weekStart == weekStartOf(today()) {
			ui.commitHere.Disable()
		} else {
			ui.commitHere.Enable()
		}
	}

	if len(ui.cfg.Repos) == 0 {
		ui.commitBox.Objects = []fyne.CanvasObject{
			widget.NewLabel("Add a repo or org to scan in Settings (e.g. bigledger)."),
		}
		ui.commitBox.Refresh()
		relayout(ui.commitBody)
		return
	}

	ui.ensureWeekCommitCache()
	loading := ui.weekCommitLoad[weekStart]
	commits, cached := ui.weekCommits[weekStart]
	errs := ui.weekCommitErrs[weekStart]

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
	// Border again: top holds the summary and any errors at their natural
	// height, and the grid takes every remaining pixel to the bottom of the tab.
	content := container.NewBorder(top, nil, nil, nil, ui.commitDaysPanel(days, byDay))

	ui.commitBox.Objects = []fyne.CanvasObject{content}
	ui.commitBox.Refresh()
	relayout(ui.commitBody)
}

// commitWeekSummary is the line above the day grid: how many commits landed and
// on how many issues, plus a note if the fetch is still running.
func (ui *UI) commitWeekSummary(commits []Commit, loading, cached bool) fyne.CanvasObject {
	if loading && !cached {
		return widget.NewLabel("Reading your commits from GitHub…")
	}
	issues := map[string]bool{}
	for _, c := range commits {
		issues[c.Issue] = true
	}
	line := fmt.Sprintf("%d commit%s across %d issue%s",
		len(commits), plural(len(commits)), len(issues), plural(len(issues)))
	if loading {
		line += " · refreshing…"
	}
	return container.NewHBox(bold("This week"),
		widget.NewLabelWithStyle(line, fyne.TextAlignLeading, fyne.TextStyle{Monospace: true}))
}

func (ui *UI) commitDaysPanel(days []string, byDay map[string][]Commit) fyne.CanvasObject {
	grid := container.NewGridWithColumns(len(days))
	for _, ds := range days {
		grid.Add(ui.commitDayColumn(ds, byDay[ds]))
	}
	return grid
}

// commitDayColumn is one day of the week: header, then a bubble per parent
// issue holding the commits that touched it. Fills whatever height the grid
// hands the cell — bubbles scroll internally when they overflow, so a heavy
// day never pushes the rest of the week off the screen.
func (ui *UI) commitDayColumn(ds string, commits []Commit) fyne.CanvasObject {
	t, _ := time.Parse("2006-01-02", ds)
	name := fmt.Sprintf("%s %d", t.Format("Mon"), t.Day())
	if ds == today() {
		name += " · today"
	}
	if n := len(commits); n > 0 {
		name += fmt.Sprintf(" · %d commit%s", n, plural(n))
	}
	head := widget.NewLabelWithStyle(name, fyne.TextAlignLeading,
		fyne.TextStyle{Bold: ds == today()})

	inner := container.NewVBox()
	if len(commits) == 0 {
		inner.Add(widget.NewLabelWithStyle("—", fyne.TextAlignLeading,
			fyne.TextStyle{Italic: true}))
	} else {
		for _, group := range groupCommitsByIssue(commits) {
			inner.Add(ui.commitIssueBubble(group.Issue, group.Commits))
		}
	}
	scroll := container.NewVScroll(inner)

	frame := canvas.NewRectangle(color.Transparent)
	frame.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	frame.StrokeWidth = 1
	frame.CornerRadius = cellCornerRadius

	body := container.NewBorder(head, nil, nil, nil, scroll)
	return container.NewStack(frame, container.NewPadded(body))
}

// commitIssueBubble is one issue's share of a day: the issue label on top and
// each commit under it as a subject + short-sha hyperlink. Subjects wrap so a
// long commit message wraps to a second line inside its bubble rather than
// pushing the sha off the right edge of the column.
//
// The header is two lines: the issue title on top (bold, wrapping, with a full
// tooltip on hover so a long title read in full without clipping the column),
// and the issue reference underneath as a hyperlink to the issue on GitHub.
// One glance says what the bubble is about; the ref is there when you need it.
func (ui *UI) commitIssueBubble(issue string, commits []Commit) fyne.CanvasObject {
	title := commitBubbleTitle(ui, issue)
	titleLabel := newHoverText(ui.win, title, title, fyne.TextStyle{Bold: true})
	ref := issueHyperlink(issue)
	head := container.NewVBox(titleLabel, ref)

	rows := []fyne.CanvasObject{head}
	for _, c := range commits {
		subject := widget.NewLabel("• " + commitSubject(c))
		subject.Wrapping = fyne.TextWrapWord
		sha := commitHyperlink(c)
		// Sha on its own line under the subject: with the sha as a Border-right
		// element the wrapped subject fought it for width and truncated.
		rows = append(rows, subject, container.NewHBox(layout.NewSpacer(), sha))
	}

	frame := canvas.NewRectangle(ui.bubbleBackground(issue))
	frame.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	frame.StrokeWidth = 1
	frame.CornerRadius = cellCornerRadius
	return container.NewStack(frame, container.NewPadded(container.NewVBox(rows...)))
}

// bubbleBackground tints a bubble by its org, so a day of mixed employers reads
// as such at a glance. Falls back to the theme's card colour for commits with
// no parseable issue.
func (ui *UI) bubbleBackground(issue string) color.Color {
	if issue == "" {
		return theme.Color(theme.ColorNameOverlayBackground)
	}
	if owner, _, _, err := splitIssue(issue); err == nil {
		c := orgColor(ui.cfg, owner)
		return color.NRGBA{R: c.R, G: c.G, B: c.B, A: 40}
	}
	return theme.Color(theme.ColorNameOverlayBackground)
}

// commitBubbleTitle is the human title on the top line of a bubble. Falls back
// to the raw ref while the title is still resolving, or to a placeholder for
// commits with no parseable issue.
func commitBubbleTitle(ui *UI, issue string) string {
	if issue == "" {
		return "no issue ref"
	}
	if info, ok := ui.issueInfo[issue]; ok && info.Title != "" {
		return info.Title
	}
	return issue
}

// commitIssueGroup is one bubble's worth of commits: an issue plus the commits
// on that issue for one day.
type commitIssueGroup struct {
	Issue   string
	Commits []Commit
}

// groupCommitsByIssue buckets commits by their parent issue while keeping the
// original ordering — commits already come newest-first from FetchWeekCommits.
// Issues with no ref bucket under an empty string and sort to the end.
func groupCommitsByIssue(commits []Commit) []commitIssueGroup {
	order := []string{}
	byIssue := map[string][]Commit{}
	for _, c := range commits {
		if _, seen := byIssue[c.Issue]; !seen {
			order = append(order, c.Issue)
		}
		byIssue[c.Issue] = append(byIssue[c.Issue], c)
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if (a == "") != (b == "") {
			return a != "" // real issues first, "no issue" bubble last
		}
		return a < b
	})
	out := make([]commitIssueGroup, 0, len(order))
	for _, issue := range order {
		out = append(out, commitIssueGroup{Issue: issue, Commits: byIssue[issue]})
	}
	return out
}
