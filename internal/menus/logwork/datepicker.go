package logwork

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"time"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
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

	p.pop = widget.NewPopUp(container.NewPadded(p.Cal), canvas)
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
}

func (p *WorklogDatePicker) closeCalendar() {
	if p.pop != nil {
		p.pop.Hide()
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
	drafts := map[string]int{}
	if from, to, err := backend.MonthBounds(p.Month); err == nil {
		drafts = p.ui.DraftMinutesByDay(from, to)
	}

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
		grid.Add(p.dayCell(ds, day, byDay[ds], drafts[ds]))
	}
	p.Grid.Objects = []fyne.CanvasObject{grid}
	p.Grid.Refresh()
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
func (p *WorklogDatePicker) dayCell(ds string, day int, byOrg map[string]int, draft int) fyne.CanvasObject {
	mins := 0
	for _, m := range byOrg {
		mins += m
	}
	state := shell.DayState(mins, draft)

	num := canvas.NewText(strconv.Itoa(day), theme.Color(theme.ColorNameForeground))
	num.TextStyle = fyne.TextStyle{Bold: ds == p.date}
	num.Alignment = fyne.TextAlignCenter

	body := []fyne.CanvasObject{num}
	if mins > 0 || draft > 0 {
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
		body = append(body, caption(DayOwedLine(mins, draft)))
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
	bar := shell.MeterBarWithDraft(p.ui.Cfg, byOrg, map[string]int{"@draft": draft}, backend.Target, 6)
	stack := container.NewStack(floor, wash,
		shell.VMeterFill(p.ui.Cfg, byOrg, draft, backend.Target, shell.CellCornerRadius), border,
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
// The two halves sit at either end of the line rather than being joined by an
// arrow. An arrow is one more glyph to find in whatever font the system hands
// us, and the one that was there rendered as a box.
//
// The right end grows a third piece — what the day would score once the number
// typed in the Worklog mins field lands on it — so the answer to "does this
// entry finish the day" is on the same line as the picker rather than a tab
// away. Passing a nil minE keeps the old two-piece line.
//
// excludeRowID names the draft the editor is looking at, so its saved minutes
// stop being counted as pending the moment the field is on screen — otherwise
// a 300-minute draft opened at 300 would read as 600 waiting the second the
// popup drew.
func dayFillReadout(ui *shell.UI, p *WorklogDatePicker, minE *widget.Entry, excludeRowID string) fyne.CanvasObject {
	left := widget.NewLabel("")
	right := widget.NewLabelWithStyle("", fyne.TextAlignTrailing, fyne.TextStyle{})

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
		right.SetText("")
		if date == "" {
			left.SetText("Pick a date to see how full it is.")
			return
		}
		mins, state := githubMinutesOn(ui, date)
		switch state {
		case "ok":
			left.SetText(fmt.Sprintf("%s already on GitHub: %s", date, DayScoreLine(mins)))
			waiting := ui.DraftMinutesOn(date, excludeRowID)
			entry := parseMins()
			var parts []string
			if waiting > 0 {
				parts = append(parts, fmt.Sprintf("%d min on pending log, then %s",
					waiting, DayScoreLine(mins+waiting)))
			}
			if entry > 0 {
				parts = append(parts, fmt.Sprintf("+%d min this entry, then %s",
					entry, DayScoreLine(mins+waiting+entry)))
			}
			right.SetText(strings.Join(parts, "  ·  "))
		case "loading":
			left.SetText(date + ": checking GitHub…")
		case "off":
			left.SetText("Set a project URL in Settings to see how full a day is.")
		default:
			left.SetText(date + ": could not read the GitHub project.")
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
	return container.NewBorder(nil, nil, nil, right, left)
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
