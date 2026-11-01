package status

import (
	_ "embed"
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

// ============================ Status (calendar) ============================

func BuildStatusTab(ui *shell.UI) fyne.CanvasObject {
	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		ui.CalMonth = shell.ShiftMonth(ui.CalMonth, -1)
		ui.SelDay = ""
		DrawCalendar(ui)
		ui.LoadStatus(false)
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		ui.CalMonth = shell.ShiftMonth(ui.CalMonth, 1)
		ui.SelDay = ""
		DrawCalendar(ui)
		ui.LoadStatus(false)
	})
	ui.CalTitle = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	ui.CalSummary = widget.NewLabel("")
	refresh := widget.NewButtonWithIcon("Refresh from GitHub", theme.ViewRefreshIcon(), func() {
		ui.EnsureProjectCache()
		delete(ui.ProjLoaded, shell.StatusCacheKey(ui.CalMonth))
		DrawCalendar(ui)
		ui.LoadStatus(true)
	})
	head := container.NewHBox(prev, ui.CalTitle, next, layout.NewSpacer(), ui.CalSummary, refresh)

	// Stack, not VBox: a VBox gives the grid its minimum height and parks the
	// rest of the tab empty. The grid has to take everything going, since a
	// month of cells is the whole point of this tab.
	ui.CalBox = container.NewStack()
	ui.CalLegend = container.NewVBox()
	ui.DayPanel = container.NewVBox()
	// A Stack, so the prompt can be centred against the panel's full height.
	// Inside a scroll it would sit at the top, because a scroll gives its
	// content the content's own height and nothing more.
	ui.DaySwap = container.NewStack(dayPanelHint())

	// The day's detail rides beside the calendar, not under it. Underneath, a
	// day picked in the last week sat a full month's scroll below the click
	// that opened it. Its own scroll keeps a long day off the calendar's back.
	width := canvas.NewRectangle(color.Transparent)
	width.SetMinSize(fyne.NewSize(dayPanelWidth, 0))
	ui.DaySide = container.NewStack(width, ui.DaySwap)

	// No scroll around the calendar: a scroll sizes its content to the content's
	// minimum, which is what kept the month to half the window. Border hands the
	// grid every pixel the header and legend do not use.
	ui.StatusProgress = shell.NewLoadingIndicator()
	ui.CalBody = container.NewBorder(container.NewVBox(head, ui.StatusProgress.View), ui.CalLegend, nil, nil, ui.CalBox)
	calCard := widget.NewCard("", "", ui.CalBody)
	return container.NewBorder(nil, nil, nil, ui.DaySide, calCard)
}

// dayPanelWidth is the side panel's share of the window. Wide enough for a
// remark to wrap at a readable measure, narrow enough that the calendar it sits
// beside keeps seven usable columns.
const dayPanelWidth = 380

func DrawCalendar(ui *shell.UI) {
	if ui.CalBox == nil {
		return
	}
	ui.CalTitle.SetText(shell.MonthName(ui.CalMonth))
	// Status reflects the GitHub project, filtered to the worklog owner.
	key := shell.StatusCacheKey(ui.CalMonth)
	items, cached := ui.ItemsFor(key)
	if !cached {
		ui.EnsureProjectCache()
		if ui.ProjErr[key] != nil && !ui.ProjLoading[key] {
			ui.CalBox.Objects = []fyne.CanvasObject{shell.ColorLabel(ui.ProjErr[key].Error(), theme.ColorNameError)}
			ui.CalBox.Refresh()
			shell.Relayout(ui.CalBody)
			ui.CalSummary.SetText("")
			return
		}
		ui.CalBox.Objects = []fyne.CanvasObject{widget.NewLabel("Loading worklogs from GitHub…")}
		ui.CalBox.Refresh()
		shell.Relayout(ui.CalBody)
		ui.CalSummary.SetText("")
		return
	}
	// Split each day by org so the cells can be coloured. The key is counted from
	// everything in the month, filtered or not — it is what the filter is worked
	// from, so an org that has been switched off still has to appear in it with
	// the hours it would be worth switched back on.
	byDayOrg := map[string]map[string]int{}
	monthByOrg := map[string]int{}
	monthTotal := 0
	for _, it := range items {
		if !strings.HasPrefix(it.Date, ui.CalMonth) {
			continue
		}
		org := shell.ItemOrg(it)
		monthByOrg[org] += it.Minutes
		if !ui.OrgShown(org) {
			continue
		}
		if byDayOrg[it.Date] == nil {
			byDayOrg[it.Date] = map[string]int{}
		}
		byDayOrg[it.Date][org] += it.Minutes
		monthTotal += it.Minutes
	}
	t, _ := time.Parse("2006-01", ui.CalMonth)
	year, mon := t.Year(), int(t.Month())
	daysIn := time.Date(year, time.Month(mon)+1, 0, 0, 0, 0, 0, time.UTC).Day()
	// Monday-first offset
	firstWd := (int(time.Date(year, time.Month(mon), 1, 0, 0, 0, 0, time.UTC).Weekday()) + 6) % 7

	// What is saved here and not pushed yet. The board cannot know about it, so
	// without this the month shows holes on days that were worked.
	monthFrom, monthTo, _ := backend.MonthBounds(ui.CalMonth)
	draftByDay := ui.DraftMinutesByDay(monthFrom, monthTo)
	monthDraft := 0
	for _, m := range draftByDay {
		monthDraft += m
	}

	grid := container.NewGridWithColumns(7)
	for _, d := range []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"} {
		grid.Add(widget.NewLabelWithStyle(d, fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}))
	}
	for i := 0; i < firstWd; i++ {
		grid.Add(widget.NewLabel(""))
	}
	for day := 1; day <= daysIn; day++ {
		ds := fmt.Sprintf("%s-%s", ui.CalMonth, shell.Pad2(day))
		grid.Add(dayCell(ui, ds, day, byDayOrg[ds], draftByDay[ds]))
	}
	ui.CalBox.Objects = []fyne.CanvasObject{grid}
	ui.CalBox.Refresh()
	shell.Relayout(ui.CalBody)
	ui.CalLegend.Objects = nil
	if key := shell.OrgLegend(ui.Cfg, monthByOrg, ui.OrgOff, ui.ToggleOrg); key != nil {
		ui.CalLegend.Objects = []fyne.CanvasObject{key}
	}
	ui.CalLegend.Refresh()
	shell.Relayout(ui.CalBody)
	summary := fmt.Sprintf("%d min logged this month", monthTotal)
	if monthDraft > 0 {
		// Kept out of the total on purpose: the number before the dot is what
		// the board holds, and folding drafts into it would claim work that has
		// not been sent.
		summary += fmt.Sprintf(" · %d min saved here, waiting to push", monthDraft)
	}
	ui.CalSummary.SetText(summary)
	// Always redrawn: with no day picked it shows the prompt to pick one.
	DrawDayPanel(ui)
}

func dayCell(ui *shell.UI, ds string, day int, byOrg map[string]int, draft int) fyne.CanvasObject {
	m := 0
	for _, v := range byOrg {
		m += v
	}
	pc := 0
	if m > 0 {
		pc = int(float64(m) / float64(backend.Target) * 100)
	}
	dayLbl := widget.NewLabelWithStyle(strconv.Itoa(day), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	// coloured is the readout drawn as its own text rather than as a label, so
	// the colour can carry the state along with the words.
	coloured := func(txt string, name fyne.ThemeColorName) *canvas.Text {
		c := canvas.NewText(txt, theme.Color(name))
		c.TextSize = theme.CaptionTextSize()
		c.TextStyle = fyne.TextStyle{Monospace: true}
		return c
	}

	state := shell.DayState(m, draft)
	// The score under the number, the way the report reads it: what the day has
	// against what it owes, so a glance down the month compares like with like
	// instead of asking which percentage belonged to which target.
	var lines []fyne.CanvasObject
	if m > 0 || draft > 0 {
		lines = append(lines, coloured(fmt.Sprintf("%d/%d", m, backend.Target), state.TextColor))
		switch {
		case draft > 0:
			lines = append(lines, coloured(fmt.Sprintf("+%dm · %d%%",
				draft, shell.PercentOf(m+draft)), state.TextColor))
		default:
			lines = append(lines, coloured(fmt.Sprintf("%d%%", pc), state.TextColor))
		}
	}

	body := container.NewVBox(append([]fyne.CanvasObject{dayLbl, layout.NewSpacer()}, lines...)...)
	border := canvas.NewRectangle(color.Transparent)
	border.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	border.StrokeWidth = 1
	border.CornerRadius = shell.CellCornerRadius
	if ds == ui.SelDay {
		border.StrokeColor = theme.Color(theme.ColorNamePrimary)
		border.StrokeWidth = 2
	}
	// A floor, not a size: the grid hands every cell an equal share of the
	// month's height, and this only stops that share collapsing to nothing on
	// a short window.
	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(0, shell.DayCellMinHeight))

	// Three layers, outermost first: the day's state as a flat wash, the org
	// split filling from the bottom over it, then the frame and the numbers.
	//
	// The wash is what makes the month readable at arm's length — a done day is
	// green, a short one yellow, an untouched one the background — while the
	// meter over it still says how full the day was and who it was for.
	wash := canvas.NewRectangle(shell.DayState(m, draft).Fill)
	wash.CornerRadius = shell.CellCornerRadius
	stack := container.NewStack(floor, wash,
		shell.VMeterFill(ui.Cfg, byOrg, draft, backend.Target, shell.CellCornerRadius), border,
		container.NewPadded(body))
	return shell.NewTappable(stack, func() {
		ui.SelDay = ds
		DrawCalendar(ui)
		DrawDayPanel(ui)
	})
}

func DrawDayPanel(ui *shell.UI) {
	// The panel keeps its place whether or not a day is picked: hiding it made
	// the calendar jump a column wider and back every time one was opened.
	if ui.SelDay == "" {
		ui.DaySwap.Objects = []fyne.CanvasObject{dayPanelHint()}
		ui.DaySwap.Refresh()
		return
	}
	monthItems, _ := ui.ItemsFor(shell.StatusCacheKey(ui.CalMonth))
	var dayItems []backend.WorklogItem
	for _, it := range monthItems {
		// A day opened from a filtered calendar shows the same work the cell was
		// drawn from; the panel is that cell spelled out, not a second opinion.
		if it.Date == ui.SelDay && ui.OrgShown(shell.ItemOrg(it)) {
			dayItems = append(dayItems, it)
		}
	}
	// Biggest first. The day is opened to see where its hours went, and the item
	// that took the most of them is the answer; fetch order buried it wherever
	// the project happened to return it. Stable, so equal items keep that order
	// rather than shuffling between redraws.
	sort.SliceStable(dayItems, func(i, j int) bool {
		return dayItems[i].Minutes > dayItems[j].Minutes
	})

	byOrg := shell.MinutesByOrg(dayItems)
	total := 0
	for _, v := range byOrg {
		total += v
	}
	pc := 0
	if total > 0 {
		pc = int(float64(total) / float64(backend.Target) * 100)
	}

	close := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		ui.SelDay = ""
		DrawCalendar(ui)
		DrawDayPanel(ui)
	})
	close.Importance = widget.LowImportance
	head := container.NewBorder(nil, nil, nil, close,
		widget.NewLabelWithStyle(ui.SelDay, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))

	// Same meter as the calendar cell, at a size worth reading: the day against
	// the target, split by org, with the split spelled out underneath.
	body := []fyne.CanvasObject{
		head,
		widget.NewLabelWithStyle(fmt.Sprintf("%d/%d min (%d%%)", total, backend.Target, pc),
			fyne.TextAlignLeading, fyne.TextStyle{Monospace: true}),
		ui.ProgressBar("detail", ui.SelDay, byOrg, nil, backend.Target, 12),
	}
	for _, org := range shell.OrgsByShare(ui.Cfg, byOrg) {
		body = append(body, container.New(widgets.NewRatioRow(0.08, 0.52, 0.40),
			shell.OrgDot(ui.Cfg, org),
			widget.NewLabel(org),
			widget.NewLabelWithStyle(backend.HoursMins(byOrg[org]),
				fyne.TextAlignTrailing, fyne.TextStyle{Monospace: true})))
	}
	if len(dayItems) == 0 {
		body = append(body, widget.NewLabel("No worklog items on this day for "+
			backend.OrDefault(ui.Cfg.WorklogOwner, "you")+"."))
	}

	for _, it := range dayItems {
		it := it
		org := shell.ItemOrg(it)
		title := widget.NewLabel(shell.Truncate(it.Title, 80))
		title.Wrapping = fyne.TextWrapWord
		head := container.New(widgets.NewRatioRow(0.10, 0.24, 0.66),
			shell.OrgDot(ui.Cfg, org),
			widget.NewLabelWithStyle(fmt.Sprintf("%d min", it.Minutes),
				fyne.TextAlignLeading, fyne.TextStyle{Bold: true, Monospace: true}),
			title)
		// The parent issue is the name the work is filed under, so it sits with
		// the duration rather than as a bare "Parent issue" link at the foot of
		// the card: the day panel says what was worked on, and one click on the
		// name opens it on GitHub.
		var parent fyne.CanvasObject
		if it.ParentTitle != "" || it.ParentURL != "" {
			parent = container.NewHBox(
				widget.NewLabelWithStyle("Parent", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
				shell.GithubHyperlink(shell.Truncate(backend.OrDefault(it.ParentTitle, "issue"), 60), it.ParentURL))
		}

		var links []fyne.CanvasObject
		if it.URL != "" {
			links = append(links, shell.GithubHyperlink("Worklog", it.URL))
		}
		var link fyne.CanvasObject = widget.NewLabel("")
		if len(links) > 0 {
			link = container.NewHBox(links...)
		}
		rem := widget.NewLabel(it.Remarks)
		rem.Wrapping = fyne.TextWrapWord

		card := []fyne.CanvasObject{head}
		if parent != nil {
			card = append(card, parent)
		}
		card = append(card, rem, link)

		// A stripe in the org's colour, the same cue the pending tiles carry.
		stripe := canvas.NewRectangle(shell.OrgColor(ui.Cfg, org))
		stripe.SetMinSize(fyne.NewSize(3, 0))
		body = append(body, widget.NewCard("", "", container.NewBorder(nil, nil, stripe, nil,
			container.NewPadded(container.NewVBox(card...)))))
	}

	ui.DayPanel.Objects = []fyne.CanvasObject{container.NewVBox(body...)}
	ui.DayPanel.Refresh()
	// A day's detail can run past the panel, so it gets the scroll the prompt
	// does not want.
	ui.DaySwap.Objects = []fyne.CanvasObject{
		container.NewVScroll(container.NewPadded(ui.DayPanel))}
	ui.DaySwap.Refresh()
}

// dayPanelHint is what the side panel shows with no day picked: centred, so an
// empty panel reads as waiting rather than as a heading with nothing under it.
func dayPanelHint() fyne.CanvasObject {
	hint := widget.NewLabelWithStyle("Select any date to show its details.",
		fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	hint.Wrapping = fyne.TextWrapWord
	return container.NewCenter(hint)
}
