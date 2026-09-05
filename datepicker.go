package main

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
)

// worklogDatePicker is the month grid the worklog date is chosen from, with each
// day painted by how full it already is.
//
// fyne's own DateEntry has a calendar, but its cells are plain numbers it draws
// itself — there is no way in. That calendar is the one place the question
// "which day still owes time" is actually being asked, and answering it there
// meant leaving the popup for the Status tab, remembering a number, and coming
// back. So the grid is drawn here instead: the same four states as the Status
// calendar, and the same score under each date.
type worklogDatePicker struct {
	widget.BaseWidget

	ui    *UI
	date  string // the chosen day, YYYY-MM-DD, empty for none
	month string // the month on show, YYYY-MM

	// OnChanged fires with the new date whenever the choice moves, so the
	// readout under the field can follow it.
	OnChanged func(string)

	body  *fyne.Container
	title *widget.Label
	grid  *fyne.Container
	entry *widget.Entry

	// The month is an overlay, not part of the form. Inline it pushed every
	// field under it down the popup as it opened and closed; over the top it
	// takes no room at all and lands where the eye already is.
	cal  *fyne.Container
	pop  *widget.PopUp
	open *widget.Button // the calendar button, inside the field
}

func newWorklogDatePicker(ui *UI, iso string) *worklogDatePicker {
	p := &worklogDatePicker{ui: ui, date: normalDate(iso)}
	p.month = p.date
	if len(p.month) >= 7 {
		p.month = p.month[:7]
	} else {
		p.month = thisMonth()
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
func (p *worklogDatePicker) ISO() string { return p.date }

// Set moves the choice, and the month with it when the new day is elsewhere.
//
// Something unreadable is ignored rather than taken as a request to clear: the
// caller asked for a date it could not spell, and dropping the one already
// chosen would answer a question nobody asked.
func (p *worklogDatePicker) Set(iso string) {
	iso = normalDate(iso)
	if iso == "" || iso == p.date {
		return
	}
	p.date = iso
	if len(iso) >= 7 {
		p.month = iso[:7]
	}
	p.entry.SetText(iso)
	p.refresh()
}

func (p *worklogDatePicker) build() {
	p.entry = widget.NewEntry()

	// Inside the field, not beside it. Entry.ActionItem is the slot fyne keeps
	// for exactly this — it is where PasswordEntry hangs its reveal button — so
	// the calendar sits in the field's own trailing edge and the row stays one
	// control wide instead of a box with a button stuck on the end.
	//
	// Attached before anything else touches the entry. SetText and
	// SetPlaceHolder both call Refresh, Refresh builds the renderer, and the
	// renderer reads ActionItem once as it is built — hang it on afterwards and
	// there is simply nowhere for it to be drawn.
	p.open = widget.NewButtonWithIcon("", calendarIcon(), p.toggleCalendar)
	p.open.Importance = widget.LowImportance
	p.entry.ActionItem = p.open

	p.entry.SetPlaceHolder("YYYY-MM-DD")
	p.entry.SetText(p.date)
	// Typing is still allowed — it is the fastest way to a date months away —
	// but only a whole one moves the grid.
	p.entry.OnChanged = func(s string) {
		if iso := normalDate(s); iso != "" && iso != p.date {
			p.date = iso
			p.month = iso[:7]
			p.refresh()
			p.fire()
		}
	}

	prev := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		p.month = shiftMonth(p.month, -1)
		p.refresh()
	})
	next := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		p.month = shiftMonth(p.month, 1)
		p.refresh()
	})
	p.title = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	p.grid = container.NewVBox()

	head := container.NewBorder(nil, nil, prev, next, p.title)
	// Held, not shown: this goes into the overlay when the button asks for it.
	p.cal = container.NewVBox(head, p.grid)

	p.body = container.NewVBox(p.entry)
	p.refresh()
}

// calendarIcon is the glyph on the button that opens the month — the same one
// fyne's own DateEntry uses, so the control reads as the thing it replaces.
func calendarIcon() fyne.Resource { return theme.CalendarIcon() }

// calendarOpen reports whether the month is on screen.
func (p *worklogDatePicker) calendarOpen() bool { return p.pop != nil && p.pop.Visible() }

func (p *worklogDatePicker) toggleCalendar() {
	if p.calendarOpen() {
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
func (p *worklogDatePicker) openCalendar() {
	drv := fyne.CurrentApp().Driver()
	canvas := drv.CanvasForObject(p)
	if canvas == nil {
		return // not on screen yet; nothing to float over
	}
	// Opened on the chosen day's month, not on wherever it was last left.
	if len(p.date) >= 7 {
		p.month = p.date[:7]
	}
	p.refresh()

	p.pop = widget.NewPopUp(container.NewPadded(p.cal), canvas)
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

func (p *worklogDatePicker) closeCalendar() {
	if p.pop != nil {
		p.pop.Hide()
		p.pop = nil
	}
}

func (p *worklogDatePicker) fire() {
	if p.OnChanged != nil {
		p.OnChanged(p.date)
	}
}

func (p *worklogDatePicker) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.body)
}

// refresh redraws the month. Called on every move, so the day fills follow a
// fetch landing as well as a click.
func (p *worklogDatePicker) refresh() {
	if p.grid == nil {
		return
	}
	p.title.SetText(monthName(p.month))

	// What the board holds for this month, and what is saved here for it. Both
	// are read through the same doors the rest of the app uses, so a month
	// already fetched costs nothing and one that is not kicks its fetch off.
	byDay := map[string]int{}
	items, _ := p.ui.monthWorklogs(p.month)
	for _, it := range items {
		byDay[it.Date] += it.Minutes
	}
	drafts := map[string]int{}
	if from, to, err := monthBounds(p.month); err == nil {
		drafts = p.ui.draftMinutesByDay(from, to)
	}

	t, err := time.Parse("2006-01", p.month)
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
		ds := fmt.Sprintf("%s-%s", p.month, pad2(day))
		grid.Add(p.dayCell(ds, day, byDay[ds], drafts[ds]))
	}
	p.grid.Objects = []fyne.CanvasObject{grid}
	p.grid.Refresh()
}

// pickerCellMinHeight is a floor, not a size: the date, its score and what it
// still owes, with enough room left that a day is a comfortable target for the
// pointer.
const pickerCellMinHeight = 58

// dayOwedLine is the second caption under a date: what that day still owes,
// counting the drafts, because those are hours already worked.
//
// A day carrying drafts says so as well as saying what is left, since the two
// answer different questions — how much more to do, and how much of it is
// already sitting here unpushed.
func dayOwedLine(mins, draft int) string {
	left := target - mins - draft
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
func (p *worklogDatePicker) dayCell(ds string, day, mins, draft int) fyne.CanvasObject {
	state := dayState(mins, draft)

	num := canvas.NewText(strconv.Itoa(day), theme.Color(theme.ColorNameForeground))
	num.TextStyle = fyne.TextStyle{Bold: ds == p.date}
	num.Alignment = fyne.TextAlignCenter

	body := []fyne.CanvasObject{num}
	if mins > 0 || draft > 0 {
		caption := func(s string) *canvas.Text {
			c := canvas.NewText(s, theme.Color(state.textColor))
			c.TextSize = theme.CaptionTextSize()
			c.Alignment = fyne.TextAlignCenter
			return c
		}
		body = append(body, caption(fmt.Sprintf("%d/%d", mins, target)))
		// What the day still owes, which is the number the choice is actually
		// made on — "240/480" left you to work it out every time. Counted after
		// the drafts where there are any, since those are hours already worked.
		body = append(body, caption(dayOwedLine(mins, draft)))
	}

	wash := canvas.NewRectangle(state.fill)
	wash.CornerRadius = cellCornerRadius
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = cellCornerRadius
	border.StrokeWidth = 1
	border.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	if ds == p.date {
		border.StrokeColor = theme.Color(theme.ColorNamePrimary)
		border.StrokeWidth = 2
	}
	floor := canvas.NewRectangle(color.Transparent)
	floor.SetMinSize(fyne.NewSize(0, pickerCellMinHeight))

	stack := container.NewStack(floor, wash, border,
		container.NewPadded(container.NewVBox(body...)))
	return newTappable(stack, func() {
		p.date = ds
		p.entry.SetText(ds)
		p.refresh()
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
func (ui *UI) dayFillReadout(p *worklogDatePicker, minE *widget.Entry, excludeRowID string) fyne.CanvasObject {
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
		mins, state := ui.githubMinutesOn(date)
		switch state {
		case "ok":
			left.SetText(fmt.Sprintf("%s already on GitHub: %s", date, dayScoreLine(mins)))
			waiting := ui.draftMinutesOn(date, excludeRowID)
			entry := parseMins()
			var parts []string
			if waiting > 0 {
				parts = append(parts, fmt.Sprintf("%d min on pending log, then %s",
					waiting, dayScoreLine(mins+waiting)))
			}
			if entry > 0 {
				parts = append(parts, fmt.Sprintf("+%d min this entry, then %s",
					entry, dayScoreLine(mins+waiting+entry)))
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
func labelledPicker(name string, p *worklogDatePicker) fyne.CanvasObject {
	return container.NewVBox(
		widget.NewLabelWithStyle(name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		p,
		layout.NewSpacer(),
	)
}
