package logwork

import (
	"fmt"
	"image/color"
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

// WorklogDatePicker is the month grid the worklog date is chosen from, with each
// day painted by how full it already is.
//
// fyne's own DateEntry has a calendar, but its cells are plain numbers it draws
// itself — there is no way in. That calendar is the one place the question
// "which day still owes time" is actually being asked, and answering it there
// meant leaving the popup for the Status tab, remembering a number, and coming
// back. So the grid is drawn here instead: the same four states as the Status
// calendar, and the same score under each date.
type WorklogDatePicker struct {
	widget.BaseWidget

	ui    *shell.UI
	date  string // the chosen day, YYYY-MM-DD, empty for none
	Month string // the month on show, YYYY-MM

	// OnChanged fires with the new date whenever the choice moves, so the
	// readout under the field can follow it.
	OnChanged func(string)

	body  *fyne.Container
	title *widget.Label
	Grid  *fyne.Container
	Entry *widget.Entry

	// The month is an overlay, not part of the form. Inline it pushed every
	// field under it down the popup as it opened and closed; over the top it
	// takes no room at all and lands where the eye already is.
	Cal  *fyne.Container
	pop  *widget.PopUp
	Open *widget.Button // the calendar button, inside the field

	// The month unfolds down from the field and folds back up into it.
	unfold *shell.Reveal
	// Each picker owns its calendar entrance and progress-bar animations.
	motion shell.UI
}

func NewWorklogDatePicker(ui *shell.UI, iso string) *WorklogDatePicker {
	p := &WorklogDatePicker{ui: ui, date: normalDate(iso)}
	p.Month = p.date
	if len(p.Month) >= 7 {
		p.Month = p.Month[:7]
	} else {
		p.Month = shell.ThisMonth()
	}
	p.ExtendBaseWidget(p)
	p.build()
	return p
}

// normalDate keeps only what parses as a date, so a half-typed field never
// becomes a worklog dated on nothing.
func normalDate(iso string) string {
	if _, err := time.Parse("2006-01-02", iso); err != nil {
		return ""
	}
	return iso
}

// ISO is the chosen day, or empty. The one way to read this widget — the typed
// text is not the answer, since it can be half a date.
func (p *WorklogDatePicker) ISO() string { return p.date }

// Set moves the choice, and the month with it when the new day is elsewhere.
//
// Something unreadable is ignored rather than taken as a request to clear: the
// caller asked for a date it could not spell, and dropping the one already
// chosen would answer a question nobody asked.
func (p *WorklogDatePicker) Set(iso string) {
	iso = normalDate(iso)
	if iso == "" || iso == p.date {
		return
	}
	p.date = iso
	if len(iso) >= 7 {
		p.Month = iso[:7]
	}
	p.Entry.SetText(iso)
	p.Refresh()
}

func (p *WorklogDatePicker) build() {
	p.Entry = widget.NewEntry()

	// Inside the field, not beside it. Entry.ActionItem is the slot fyne keeps
	// for exactly this — it is where PasswordEntry hangs its reveal button — so
	// the calendar sits in the field's own trailing edge and the row stays one
	// control wide instead of a box with a button stuck on the end.
	//
	// Attached before anything else touches the entry. SetText and
	// SetPlaceHolder both call Refresh, Refresh builds the renderer, and the
	// renderer reads ActionItem once as it is built — hang it on afterwards and
	// there is simply nowhere for it to be drawn.
	p.Open = widget.NewButtonWithIcon("", calendarIcon(), p.toggleCalendar)
	p.Open.Importance = widget.LowImportance
	p.Entry.ActionItem = p.Open

	p.Entry.SetPlaceHolder("YYYY-MM-DD")
	p.Entry.SetText(p.date)
	// Typing is still allowed — it is the fastest way to a date months away —
	// but only a whole one moves the grid.
	p.Entry.OnChanged = func(s string) {
		if iso := normalDate(s); iso != "" && iso != p.date {
			p.date = iso
			p.Month = iso[:7]
			p.Refresh()
			p.fire()
		}
	}

	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		p.Month = shell.ShiftMonth(p.Month, -1)
		p.Refresh()
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		p.Month = shell.ShiftMonth(p.Month, 1)
		p.Refresh()
	})
	p.title = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	p.Grid = container.NewVBox()

	head := container.NewBorder(nil, nil, prev, next, p.title)
	// Held, not shown: this goes into the overlay when the button asks for it.
	p.Cal = container.NewVBox(head, p.Grid)

	p.body = container.NewVBox(p.Entry)
	p.Refresh()
}

// calendarIcon is the glyph on the button that opens the month — the same one
// fyne's own DateEntry uses, so the control reads as the thing it replaces.
func calendarIcon() fyne.Resource { return theme.CalendarIcon() }

// CalendarOpen reports whether the month is on screen.
func (p *WorklogDatePicker) CalendarOpen() bool { return p.pop != nil && p.pop.Visible() }

func (p *WorklogDatePicker) toggleCalendar() {
	if p.CalendarOpen() {
		p.closeCalendar()
		return
	}
	p.openCalendar()
}

// openCalendar floats the month over the form, under the field it belongs to.
//
// A popup rather than a panel in the form: the editor is already a popup with a
// column of fields in it, and a calendar that opened inline shoved everything
// below it down the screen and pulled it back up again on every click.
func (p *WorklogDatePicker) openCalendar() {
	drv := fyne.CurrentApp().Driver()
	canvas := drv.CanvasForObject(p)
	if canvas == nil {
		return // not on screen yet; nothing to float over
	}
	// Opened on the chosen day's month, not on wherever it was last left.
	if len(p.date) >= 7 {
		p.Month = p.date[:7]
	}
	p.Refresh()

	shell.FinishDropdownClosings()
	p.unfold = shell.NewReveal(container.NewPadded(p.Cal))
	p.unfold.Fold = true
	p.unfold.OnDismiss = p.closeCalendar
	p.pop = widget.NewPopUp(p.unfold, canvas)
	at := drv.AbsolutePositionForObject(p)
	p.pop.ShowAtPosition(fyne.NewPos(at.X, at.Y+p.Size().Height))

	// Squared off against the field it drops from, so the two read as one
	// control rather than a box of some other width hanging under it. Never
	// narrower than the month needs, though — a width that cannot hold seven
	// columns does not hide them, it prints them over each other.
	want := p.pop.MinSize()
	if p.Size().Width > want.Width {
		want.Width = p.Size().Width
	}
	p.pop.Resize(want)
	p.motion.ReplayArrival("picker")
	p.Refresh()
	shell.UnfoldPopUp(p.pop, p.unfold, want)
}

func (p *WorklogDatePicker) closeCalendar() {
	if p.pop != nil {
		shell.FoldPopUp(p.pop, p.unfold)
		p.pop = nil
	}
}

func (p *WorklogDatePicker) fire() {
	if p.OnChanged != nil {
		p.OnChanged(p.date)
	}
}

func (p *WorklogDatePicker) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.body)
}

// Refresh redraws the month. Called on every move, so the day fills follow a
// fetch landing as well as a click.
func (p *WorklogDatePicker) Refresh() {
	if p.Grid == nil {
		return
	}
	p.title.SetText(shell.MonthName(p.Month))

	// What the board holds for this month, and what is saved here for it. Both
	// are read through the same doors the rest of the app uses, so a month
	// already fetched costs nothing and one that is not kicks its fetch off.
	byDay := map[string]map[string]int{}
	items, _ := monthWorklogs(p.ui, p.Month)
	for _, it := range items {
		if byDay[it.Date] == nil {
			byDay[it.Date] = map[string]int{}
		}
		byDay[it.Date][shell.ItemOrg(it)] += it.Minutes
	}
	drafts, supportByDay := map[string]int{}, map[string]int{}
	if from, to, err := backend.MonthBounds(p.Month); err == nil {
		drafts = p.ui.DraftMinutesByDay(from, to)
		supportByDay = p.ui.SupportPlaceholdersByDay(from, to, items)
	}
	planned := p.plannedByDay(items)

	t, err := time.Parse("2006-01", p.Month)
	if err != nil {
		return
	}
	year, mon := t.Year(), int(t.Month())
	daysIn := time.Date(year, time.Month(mon)+1, 0, 0, 0, 0, 0, time.UTC).Day()
	firstWd := (int(time.Date(year, time.Month(mon), 1, 0, 0, 0, 0, time.UTC).Weekday()) + 6) % 7

	grid := container.NewGridWithColumns(7)
	for _, d := range []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"} {
		grid.Add(widget.NewLabelWithStyle(d, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}))
	}
	for i := 0; i < firstWd; i++ {
		grid.Add(widget.NewLabel(""))
	}
	for day := 1; day <= daysIn; day++ {
		ds := fmt.Sprintf("%s-%s", p.Month, shell.Pad2(day))
		grid.Add(p.dayCell(ds, day, byDay[ds], drafts[ds], planned[ds], supportByDay[ds]))
	}
	if p.CalendarOpen() {
		// The blank weekday slots already occupy the grid's leading columns.
		// Animate only real days, so firstWd is applied exactly once.
		firstDay := 7 + firstWd
		days := grid.Objects[firstDay:]
		arrive := p.motion.Arrival("picker", p.Month)
		days = arrive.Reveal(days, shell.GridDelays(len(days), 7, firstWd), func(i int, share float32) {
			p.motion.MeterShare(strconv.Itoa(i+1), share)
		})
		grid.Objects = append(grid.Objects[:firstDay], days...)
	}
	p.Grid.Objects = []fyne.CanvasObject{grid}
	p.Grid.Refresh()
}

// plannedByDay is the time planned on the Sprint Calendar for each day of the
// month on show, less what is already logged or saved against the same issue,
// as Log Work's week reads it.
func (p *WorklogDatePicker) plannedByDay(items []backend.WorklogItem) map[string]int {
	out := map[string]int{}
	if shell.Hooks.PlannedMinutes == nil {
		return out
	}
	logged := map[string][]backend.WorklogItem{}
	for _, it := range items {
		logged[it.Date] = append(logged[it.Date], it)
	}
	saved := map[string][]backend.Row{}
	if rows, err := p.ui.Store.ReadRows(); err == nil {
		for _, r := range rows {
			if r["pushed_at"] == "" && strings.HasPrefix(r["date"], p.Month) {
				saved[r["date"]] = append(saved[r["date"]], r)
			}
		}
	}
	from, to, err := backend.MonthBounds(p.Month)
	if err != nil {
		return out
	}
	for ds := from; ds <= to; ds = backend.AddDays(ds, 1) {
		if m := shell.Hooks.PlannedMinutes(p.ui, ds, logged[ds], saved[ds]); m > 0 {
			out[ds] = m
		}
	}
	return out
}

// pickerCellMinHeight is a floor, not a size: the date, its score and what it
// still owes, with enough room left that a day is a comfortable target for the
// pointer.
const pickerCellMinHeight = 74

// DayOwedLine is the second caption under a date: what that day still owes,
// counting the drafts, because those are hours already worked.
//
// A day carrying drafts says so as well as saying what is left, since the two
// answer different questions — how much more to do, and how much of it is
// already sitting here unpushed.
func DayOwedLine(mins, draft int) string {
	left := backend.Target - mins - draft
	switch {
	case left <= 0 && draft > 0:
		return fmt.Sprintf("+%dm ✓", draft)
	case left <= 0:
		return "✓"
	case draft > 0:
		return fmt.Sprintf("+%dm (%dm left)", draft, left)
	default:
		return fmt.Sprintf("(%dm left)", left)
	}
}

// dayCell is one day in the picker: the date, what it holds against the target,
// and the wash that says which kind of day it is.
func (p *WorklogDatePicker) dayCell(ds string, day int, byOrg map[string]int, draft, planned int, supportMinutes ...int) fyne.CanvasObject {
	support := 0
	if len(supportMinutes) > 0 {
		support = supportMinutes[0]
	}
	mins := 0
	for _, m := range byOrg {
		mins += m
	}
	// Planned time counts toward the day as it does on Log Work's week.
	state := shell.DayState(mins, draft+support+planned)

	num := canvas.NewText(strconv.Itoa(day), theme.Color(theme.ColorNameForeground))
	num.TextStyle = fyne.TextStyle{Bold: ds == p.date}
	num.Alignment = fyne.TextAlignCenter

	body := []fyne.CanvasObject{num}
	if mins > 0 || draft > 0 || support > 0 || planned > 0 {
		caption := func(s string) *canvas.Text {
			c := canvas.NewText(s, theme.Color(state.TextColor))
			c.TextSize = theme.CaptionTextSize()
			c.Alignment = fyne.TextAlignCenter
			return c
		}
		body = append(body, caption(fmt.Sprintf("%d/%d", mins, backend.Target)))
		// What the day still owes, which is the number the choice is actually
		// made on — "240/480" left you to work it out every time. Counted after
		// the drafts where there are any, since those are hours already worked.
		body = append(body, caption(DayOwedLine(mins, draft+support+planned)))
		if support > 0 {
			note := caption(fmt.Sprintf("+%dm support", support))
			note.Color = shell.WeekdaySupportColor()
			body = append(body, note)
		}
		if planned > 0 {
			plan := caption(fmt.Sprintf("%dm planned", planned))
			plan.Color = shell.PlannerPlanColor
			body = append(body, plan)
		}
	}

	wash := canvas.NewRectangle(state.Fill)
	wash.CornerRadius = shell.CellCornerRadius
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = shell.CellCornerRadius
	border.StrokeWidth = 1
	border.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	if ds == p.date {
		border.StrokeColor = theme.Color(theme.ColorNamePrimary)
		border.StrokeWidth = 2
	}
	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(0, pickerCellMinHeight))

	// Use the Status cell's org fill and the Log tab's progress bar. Both read
	// the same minutes; drafts stay yellow and never count as pushed work.
	bar := p.motion.ProgressBarParts(strconv.Itoa(day), ds, shell.PlannedBarParts(p.ui.Cfg, byOrg, map[string]int{"@draft": draft, shell.SupportPendingKey: support}, planned), 6)
	stack := container.NewStack(floor, wash,
		shell.VMeterFillPlanned(p.ui.Cfg, byOrg, draft, planned, backend.Target, shell.CellCornerRadius, support), border,
		container.NewPadded(container.NewBorder(nil, bar, nil, nil, container.NewVBox(body...))))
	return shell.NewTappable(stack, func() {
		p.date = ds
		p.Entry.SetText(ds)
		p.Refresh()
		p.fire()
		// Picking a day is the end of what the calendar was opened for.
		p.closeCalendar()
	})
}

// dayFillReadout is the line under the picker: how full the chosen day already
// is, and what it would come to once what is saved here goes up.
//
// What the day holds stands at the left end of the line and what it would come
// to at the right, under the minutes field, rather than being joined by an
// arrow. An arrow is one more glyph to find in whatever font the system hands
// us, and the one that was there rendered as a box. A right-hand section that
// does not fit moves to the next line whole, still against the right edge, so a
// long readout cannot force the popup wider.
//
// Drafts are gold and plans purple, as on Log List. One more section says what
// the day would score once the number typed in the Worklog mins field lands on
// it, so the answer to "does this entry finish the day" is on the same line as
// the picker rather than a tab away. Each section counts the ones before it, and
// plans come last because they are the only part not yet worked. Passing a nil
// minE leaves the entry's section out.
//
// excludeRowID names the draft the editor is looking at, so its saved minutes
// stop being counted as pending the moment the field is on screen — otherwise
// a 300-minute draft opened at 300 would read as 600 waiting the second the
// popup drew.
func dayFillReadout(ui *shell.UI, p *WorklogDatePicker, minE *widget.Entry, excludeRowID string) fyne.CanvasObject {
	fg := theme.Color(theme.ColorNameForeground)
	left := canvas.NewText("", fg)
	right := container.New(&TrailingSectionsLayout{})
	// In a VBox so it stays on the first line when the right end wraps.
	line := container.NewBorder(nil, nil, container.NewVBox(left), nil, right)
	show := func(held string, sections ...shell.TextSection) {
		left.Text = held
		right.Objects = shell.SectionTexts(fyne.TextStyle{}, sections)
		line.Refresh()
	}

	parseMins := func() int {
		if minE == nil {
			return 0
		}
		n, err := strconv.Atoi(strings.TrimSpace(minE.Text))
		if err != nil || n <= 0 {
			return 0
		}
		return n
	}

	update := func() {
		date := p.ISO()
		if date == "" {
			show("Pick a date to see how full it is.")
			return
		}
		items, state := githubItemsOn(ui, date)
		switch state {
		case "ok":
			mins := 0
			for _, it := range items {
				mins += it.Minutes
			}
			var sections []shell.TextSection
			waiting := ui.DraftMinutesOn(date, excludeRowID)
			if waiting > 0 {
				sections = append(sections, shell.TextSection{Text: fmt.Sprintf("%d min on pending log, then %s",
					waiting, DayScoreLine(mins+waiting)), Ink: widgets.LogoGold})
			}
			if shell.Hooks.SupportOn != nil {
				if support := shell.SupportPlaceholderFor(ui.Cfg, shell.Hooks.SupportOn(ui, date), date, items); support > 0 {
					waiting += support
					sections = append(sections, shell.TextSection{Text: fmt.Sprintf("+%d min support (display only), then %s",
						support, DayScoreLine(mins+waiting)), Ink: shell.WeekdaySupportColor()})
				}
			}
			entry := parseMins()
			if entry > 0 {
				sections = append(sections, shell.TextSection{Text: fmt.Sprintf("+%d min this entry, then %s",
					entry, DayScoreLine(mins+waiting+entry)), Ink: fg})
			}
			if planned := plannedOn(ui, date, items); planned > 0 {
				sections = append(sections, shell.TextSection{Text: fmt.Sprintf("+%d min planned, then %s",
					planned, DayScoreLine(mins+waiting+entry+planned)), Ink: shell.PlannerPlanColor})
			}
			show(fmt.Sprintf("%s already on GitHub: %s", date, DayScoreLine(mins)), sections...)
		case "loading":
			show(date + ": checking GitHub…")
		case "off":
			show("Set a project URL in Settings to see how full a day is.")
		default:
			show(date + ": could not read the GitHub project.")
		}
	}
	update()
	p.OnChanged = func(string) { update() }
	if minE != nil {
		// Chain onto whatever the caller has already wired: validators, tests,
		// autosave — none of those should stop firing because this readout also
		// wants to know when the number changed.
		prev := minE.OnChanged
		minE.OnChanged = func(s string) {
			if prev != nil {
				prev(s)
			}
			update()
		}
	}
	inset := theme.InnerPadding()
	return container.New(layout.NewCustomPaddedLayout(inset, inset, inset, inset), line)
}

// TrailingSectionsLayout sets the children of shell.SectionTexts against the
// right edge, moving a section that does not fit to the next line whole. The
// children alternate section, dot, section; a dot only shows between two
// sections on the same line, so no line starts or ends with one.
type TrailingSectionsLayout struct{ height float32 }

func (l *TrailingSectionsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	pad := theme.Padding()
	y := float32(0)
	for start := 0; start < len(objects); {
		end, width := start, objects[start].MinSize().Width
		for end+2 < len(objects) {
			more := objects[end+1].MinSize().Width + objects[end+2].MinSize().Width
			if width+more > size.Width {
				break
			}
			width += more
			end += 2
		}
		x := max(size.Width-width, 0)
		for _, o := range objects[start : end+1] {
			m := o.MinSize()
			o.Show()
			o.Move(fyne.NewPos(x, y))
			o.Resize(m)
			x += m.Width
		}
		if end+1 < len(objects) {
			objects[end+1].Hide()
		}
		y += objects[start].MinSize().Height + pad
		start = end + 2
	}
	l.height = max(y-pad, 0)
}

// MinSize asks for the widest section, and the height of the lines last laid out.
func (l *TrailingSectionsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for i := 0; i < len(objects); i += 2 {
		m := objects[i].MinSize()
		size.Width = max(size.Width, m.Width)
		size.Height = max(size.Height, m.Height)
	}
	size.Height = max(size.Height, l.height)
	return size
}

// plannedOn is the time planned for one day, less work already on GitHub.
// Drafts, including the one open in the editor, remain separate from the plan.
func plannedOn(ui *shell.UI, date string, items []backend.WorklogItem) int {
	if shell.Hooks.PlannedMinutes == nil {
		return 0
	}
	var drafts []backend.Row
	if rows, err := ui.Store.ReadRows(); err == nil {
		for _, r := range ui.ShownRows(rows) {
			if r["pushed_at"] == "" && r["date"] == date {
				drafts = append(drafts, r)
			}
		}
	}
	return shell.Hooks.PlannedMinutes(ui, date, items, drafts)
}

// labelledPicker is the picker with its caption, laid out like the other fields
// on the form it sits in.
func labelledPicker(name string, p *WorklogDatePicker) fyne.CanvasObject {
	return container.NewVBox(
		widget.NewLabelWithStyle(name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		p,
		layout.NewSpacer(),
	)
}
