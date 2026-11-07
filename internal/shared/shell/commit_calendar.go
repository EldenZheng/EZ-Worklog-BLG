package shell

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

	"worklog/internal/shared/backend"
	"worklog/internal/shared/widgets"
)

// CommitWeekSummary is the line above the day grid: how many commits landed and
// on how many issues, plus a note if the fetch is still running.
func (ui *UI) CommitWeekSummary(commits []backend.Commit, loading, cached bool) fyne.CanvasObject {
	if loading && !cached {
		return widget.NewLabel("Reading your commits from GitHub…")
	}
	issues := map[string]bool{}
	for _, c := range commits {
		issues[c.Issue] = true
	}
	line := fmt.Sprintf("%d commit%s across %d issue%s",
		len(commits), Plural(len(commits)), len(issues), Plural(len(issues)))
	if loading {
		line += " · refreshing…"
	}
	return container.NewHBox(Bold("This week"),
		widget.NewLabelWithStyle(line, fyne.TextAlignLeading, fyne.TextStyle{Monospace: true}))
}

func (ui *UI) CommitDaysPanel(days []string, byDay map[string][]backend.Commit) *fyne.Container {
	return ui.CommitDaysPanelWithIssueLabel(days, byDay, IssueTag)
}

// CommitDaysPanelWithIssueLabel lets views that share the commit calendar
// choose how the issue reference is written without changing the Commit List.
func (ui *UI) CommitDaysPanelWithIssueLabel(days []string, byDay map[string][]backend.Commit,
	issueLabel func(string) string) *fyne.Container {
	grid := container.NewGridWithColumns(len(days))
	for _, ds := range days {
		grid.Add(ui.commitDayColumn(ds, byDay[ds], issueLabel))
	}
	return grid
}

// commitDayColumn is one day of the week: header, then a bubble per parent
// issue holding the commits that touched it. Fills whatever height the grid
// hands the cell — bubbles scroll internally when they overflow, so a heavy
// day never pushes the rest of the week off the screen.
func (ui *UI) commitDayColumn(ds string, commits []backend.Commit,
	issueLabel func(string) string) fyne.CanvasObject {
	t, _ := time.Parse("2006-01-02", ds)
	name := fmt.Sprintf("%s %d", t.Format("Mon"), t.Day())
	if ds == Today() {
		name += " · Today"
	}
	if n := len(commits); n > 0 {
		name += fmt.Sprintf(" · %d commit%s", n, Plural(n))
	}
	label := widget.NewLabelWithStyle(name, fyne.TextAlignLeading,
		fyne.TextStyle{Bold: ds == Today()})
	label.Truncation = fyne.TextTruncateEllipsis
	var head fyne.CanvasObject = label
	if ds == Today() {
		// Today is outlined and titled in gold, like Log Work's day cards and
		// the Sprint Calendar.
		head = container.NewThemeOverride(label, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	}

	inner := container.NewVBox()
	if len(commits) == 0 {
		inner.Add(widget.NewLabelWithStyle("—", fyne.TextAlignLeading,
			fyne.TextStyle{Italic: true}))
	} else {
		for _, group := range groupCommitsByIssue(commits) {
			inner.Add(ui.commitIssueBubble(group.Issue, group.Commits, issueLabel))
		}
	}
	scroll := container.NewVScroll(inner)

	frame := canvas.NewRectangle(color.Transparent)
	frame.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	frame.StrokeWidth = 1
	if ds == Today() {
		frame.StrokeColor, frame.StrokeWidth = widgets.LogoGold, 2
	}
	frame.CornerRadius = CellCornerRadius

	body := container.NewBorder(head, nil, nil, nil, scroll)
	return container.NewStack(frame, container.NewPadded(body))
}

// commitIssueBubble is one issue's share of a day: the issue label on top and
// each commit under it as a subject + short-sha hyperlink. Subjects wrap so a
// long commit message wraps to a second line inside its bubble rather than
// pushing the sha off the right edge of the column.
//
// The header is two lines: the issue title on top (bold, wrapping, so a long
// title reads in full without clipping the column), and the issue reference
// underneath as a hyperlink to the issue on GitHub.
// One glance says what the bubble is about; the ref is there when you need it.
func (ui *UI) commitIssueBubble(issue string, commits []backend.Commit,
	issueLabel func(string) string) fyne.CanvasObject {
	title := commitBubbleTitle(ui, issue)
	if title == issue {
		title = issueLabel(issue)
	}
	titleLabel := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	titleLabel.Wrapping = fyne.TextWrapWord
	// A long owner/repo#number must not set the column's width: one wide
	// reference would push the whole week past the edge of the window.
	ref := truncateText(issueHyperlinkWithLabel(issue, issueLabel(issue)))
	head := container.NewVBox(titleLabel, ref)

	rows := []fyne.CanvasObject{head}
	for _, c := range commits {
		subject := widget.NewLabel("• " + backend.CommitSubject(c))
		subject.Wrapping = fyne.TextWrapWord
		sha := CommitHyperlink(c)
		// Sha on its own line under the subject: with the sha as a Border-right
		// element the wrapped subject fought it for width and truncated.
		shaLine := container.NewHBox(layout.NewSpacer(), sha)
		rows = append(rows, subject, shaLine)
	}

	return HoverBubble(ui.bubbleAccent(issue), container.NewVBox(rows...))
}

// truncateText makes a one-line label or link shorten with an ellipsis rather
// than demand its full width.
func truncateText(o fyne.CanvasObject) fyne.CanvasObject {
	switch t := o.(type) {
	case *widget.Hyperlink:
		t.Truncation = fyne.TextTruncateEllipsis
	case *widget.Label:
		t.Truncation = fyne.TextTruncateEllipsis
	}
	return o
}

// bubbleAccent is the org colour a bubble is tinted with, so a day of mixed
// employers reads as such at a glance. Commits with no parseable issue get the
// unknown-org grey.
func (ui *UI) bubbleAccent(issue string) color.Color {
	if owner, _, _, err := backend.SplitIssue(issue); issue != "" && err == nil {
		return OrgColor(ui.Cfg, owner)
	}
	return OrgUnknown
}

// commitBubbleTitle is the human title on the top line of a bubble. Falls back
// to the raw ref while the title is still resolving, or to a placeholder for
// commits with no parseable issue.
func commitBubbleTitle(ui *UI, issue string) string {
	if issue == "" {
		return "no issue ref"
	}
	if info, ok := ui.IssueInfo[issue]; ok && info.Title != "" {
		return info.Title
	}
	return issue
}

// commitIssueGroup is one bubble's worth of commits: an issue plus the commits
// on that issue for one day.
type commitIssueGroup struct {
	Issue   string
	Commits []backend.Commit
}

// groupCommitsByIssue buckets commits by their parent issue while keeping the
// original ordering — commits already come newest-first from FetchWeekCommits.
// Issues with no ref bucket under an empty string and sort to the end.
func groupCommitsByIssue(commits []backend.Commit) []commitIssueGroup {
	order := []string{}
	byIssue := map[string][]backend.Commit{}
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
