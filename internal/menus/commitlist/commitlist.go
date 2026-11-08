package commitlist

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
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

func BuildCommitListTab(ui *shell.UI) fyne.CanvasObject {
	walkTo := func(start string) {
		ui.WeekStart = start
		shell.Hooks.DrawWeekStrip(ui) // shared with Log Work
		ui.LoadWeekCommits(false)
		DrawCommitList(ui)
	}
	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		walkTo(shell.ShiftWeek(ui.WeekStart, -1))
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		walkTo(shell.ShiftWeek(ui.WeekStart, 1))
	})
	here := widget.NewButton("This week", func() { walkTo(backend.WeekStartOf(shell.Today())) })
	refresh := widget.NewButtonWithIcon("Refresh from GitHub", theme.ViewRefreshIcon(), func() {
		ui.LoadWeekCommits(true)
	})

	ui.CommitTitle = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	ui.CommitHere = here
	head := container.NewHBox(prev, ui.CommitTitle, next, here, layout.NewSpacer(), refresh)

	// Border layout so the day grid stretches to fill the tab — GridWithColumns
	// splits width evenly, and each cell's VScroll then absorbs the tab's height.
	// Without Border the VBox around commitBox let each child claim only its
	// natural height and the grid never stretched.
	ui.CommitBox = container.NewStack()
	ui.CommitProgress = shell.NewLoadingIndicator()
	ui.CommitBody = container.NewBorder(container.NewVBox(head, ui.CommitProgress.View), nil, nil, nil, ui.CommitBox)
	return ui.CommitBody
}

func DrawCommitList(ui *shell.UI) {
	defer ui.SyncLoading()
	if ui.CommitBox == nil {
		return
	}
	ui.RefreshLoggedShas()
	weekStart := backend.OrDefault(ui.WeekStart, backend.WeekStartOf(shell.Today()))
	days := shell.WeekDates(weekStart)

	ui.CommitTitle.SetText(shell.WeekRangeLabel(weekStart))
	if ui.CommitHere != nil {
		if weekStart == backend.WeekStartOf(shell.Today()) {
			ui.CommitHere.Disable()
		} else {
			ui.CommitHere.Enable()
		}
	}

	if len(ui.Cfg.Repos) == 0 {
		ui.CommitBox.Objects = []fyne.CanvasObject{
			widget.NewLabel("Add a repo or org to scan in Settings (e.g. exampleorg)."),
		}
		ui.CommitBox.Refresh()
		shell.Relayout(ui.CommitBody)
		return
	}

	ui.EnsureWeekCommitCache()
	loading := ui.WeekCommitLoad[weekStart]
	if loading {
		ui.CommitBox.Objects = []fyne.CanvasObject{widget.NewLabel("Loading commits and issue details...")}
		ui.CommitBox.Refresh()
		shell.Relayout(ui.CommitBody)
		return
	}
	commits, cached := ui.WeekCommits[weekStart]
	errs := ui.WeekCommitErrs[weekStart]

	topRows := []fyne.CanvasObject{ui.CommitWeekSummary(commits, loading, cached)}
	if len(errs) > 0 {
		for _, e := range errs {
			topRows = append(topRows, shell.ColorLabel(e, theme.ColorNameError))
		}
	}
	top := container.NewVBox(topRows...)

	byDay := map[string][]backend.Commit{}
	for _, c := range commits {
		byDay[c.Date] = append(byDay[c.Date], c)
	}
	// Border again: top holds the summary and any errors at their natural
	// height, and the grid takes every remaining pixel to the bottom of the tab.
	daysPanel := ui.CommitDaysPanel(days, byDay)
	// A week shown for the first time, or the tab opened, arrives as Kanban's
	// cards do: day after day, left to right, each from the top down.
	if cached && !loading {
		ui.Arrival("commits", weekStart).RevealIn(daysPanel)
	}
	content := container.NewBorder(top, nil, nil, nil, daysPanel)

	ui.CommitBox.Objects = []fyne.CanvasObject{content}
	ui.CommitBox.Refresh()
	shell.Relayout(ui.CommitBody)
}
