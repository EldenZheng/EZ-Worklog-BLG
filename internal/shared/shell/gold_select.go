package shell

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// Keep the native Select renderer and selection API, with explicit gold rows
// in its popup so selected and current options remain distinguishable.
type GoldSelect struct {
	widget.Select
	unfold *Reveal
	Popup  *widget.PopUp
	Rows   *fyne.Container
	active int
	Flash  *widgets.ChoiceFeedback
	Scroll *container.Scroll
	// Searchable tops the popup with a search field that narrows its rows, for
	// a list too long to scroll through.
	Searchable bool
	Find       *choiceSearch // the open popup's search field
	shown      []int         // the Options the popup lists; active indexes this
}

func NewGoldSelect(options []string, changed func(string)) *GoldSelect {
	s := &GoldSelect{}
	s.Options = options
	s.OnChanged = changed
	s.ExtendBaseWidget(s)
	return s
}

// choiceSearch is the popup's search field. Typing filters; the keys that work
// the list go to the dropdown, since the field holds the focus while it is open.
type choiceSearch struct {
	widget.Entry
	owner *GoldSelect
}

func newChoiceSearch(owner *GoldSelect) *choiceSearch {
	f := &choiceSearch{owner: owner}
	f.Wrapping = fyne.TextWrapOff
	f.ExtendBaseWidget(f)
	makeSearchEntry(&f.Entry, "Search", "", func(string) { owner.searchChanged() })
	return f
}

func (f *choiceSearch) TypedKey(e *fyne.KeyEvent) {
	switch e.Name {
	case fyne.KeyDown, fyne.KeyUp, fyne.KeyReturn, fyne.KeyEnter, fyne.KeyEscape:
		f.owner.TypedKey(e)
	default:
		f.Entry.TypedKey(e)
	}
}

// filterChoices works out which Options the popup lists. Without a search that
// is all of them, so active and an option's index are the same number.
func (s *GoldSelect) filterChoices() {
	query := ""
	if s.Find != nil {
		query = strings.ToLower(strings.TrimSpace(s.Find.Text))
	}
	s.shown = s.shown[:0]
	for i, option := range s.Options {
		if query == "" || strings.Contains(strings.ToLower(option), query) {
			s.shown = append(s.shown, i)
		}
	}
}

func (s *GoldSelect) searchChanged() {
	if s.Scroll == nil {
		return
	}
	s.filterChoices()
	s.active = 0
	s.renderChoices()
	s.Scroll.Refresh() // the list changed length under the scroller
	s.Scroll.ScrollToTop()
	// The field's X takes the focus when pressed. Back to the field, so the
	// keys still work the list.
	if c := fyne.CurrentApp().Driver().CanvasForObject(s); c != nil {
		c.Focus(s.Find)
	}
}

func (s *GoldSelect) Tapped(*fyne.PointEvent) {
	if !s.Disabled() {
		s.showChoices()
	}
}

func (s *GoldSelect) TypedKey(e *fyne.KeyEvent) {
	if s.Disabled() {
		return
	}
	switch e.Name {
	case fyne.KeyEscape:
		s.CloseChoices()
	case fyne.KeyReturn, fyne.KeyEnter, fyne.KeySpace:
		if s.Popup != nil && s.Popup.Visible() {
			if s.active < len(s.shown) {
				s.Choose(s.shown[s.active])
			}
		} else {
			s.showChoices()
		}
	case fyne.KeyDown, fyne.KeyUp:
		if s.Popup == nil || !s.Popup.Visible() {
			s.showChoices()
			return
		}
		if e.Name == fyne.KeyDown && s.active < len(s.shown)-1 {
			s.active++
		}
		if e.Name == fyne.KeyUp && s.active > 0 {
			s.active--
		}
		s.renderChoices()
	default:
		s.Select.TypedKey(e)
	}
}

func (s *GoldSelect) Choose(index int) {
	if index < 0 || index >= len(s.Options) {
		return
	}
	value := s.Options[index]
	s.CloseChoices()
	s.SetSelected(value)
}

func (s *GoldSelect) CloseChoices() {
	if s.Flash != nil && s.Flash.Animation != nil {
		s.Flash.Animation.Stop()
	}
	if s.Popup != nil {
		FoldPopUp(s.Popup, s.unfold)
		s.Popup = nil
	}
	s.Scroll = nil
	s.Find = nil
}

func (s *GoldSelect) Hide() { s.CloseChoices(); s.Select.Hide() }

func (s *GoldSelect) showChoices() {
	if len(s.Options) == 0 {
		return
	}
	s.CloseChoices()
	FinishDropdownClosings()
	c := fyne.CurrentApp().Driver().CanvasForObject(s)
	if c == nil {
		return
	}
	c.Focus(s)
	s.active = s.SelectedIndex()
	if s.active < 0 {
		s.active = 0
	}
	if s.Searchable {
		s.Find = newChoiceSearch(s)
	}
	s.filterChoices()
	s.Rows = container.NewVBox()
	s.renderChoices()
	height := fyne.Min(s.Rows.MinSize().Height+8, 320)
	var content fyne.CanvasObject = s.Rows
	// The blank ends let the first and last rows reach the middle of the popup.
	// A search leaves them out: they would float a short result mid-list.
	if !s.Searchable && (s.Rows.MinSize().Height > height || strings.Contains(s.Options[0], " | ")) {
		top, bottom := canvas.NewRectangle(color.Transparent), canvas.NewRectangle(color.Transparent)
		top.SetMinSize(fyne.NewSize(0, height/2))
		bottom.SetMinSize(fyne.NewSize(0, height/2))
		content = container.NewVBox(top, s.Rows, bottom)
	}
	s.Scroll = container.NewVScroll(content)
	var body fyne.CanvasObject = s.Scroll
	if s.Find != nil {
		// Sized from the full list and kept there, so the popup does not jump
		// about as the search narrows it.
		height += s.Find.MinSize().Height + theme.Padding()
		body = container.NewBorder(s.Find, nil, nil, nil, s.Scroll)
	}
	s.Flash = &widgets.ChoiceFeedback{Content: body, Pulse: canvas.NewRectangle(color.Transparent)}
	s.Flash.Pulse.CornerRadius = 6
	s.Flash.ExtendBaseWidget(s.Flash)
	s.unfold = NewReveal(s.Flash)
	s.unfold.Fold = true
	s.unfold.OnDismiss = s.CloseChoices
	s.Popup = widget.NewPopUp(s.unfold, c)
	width := fyne.Max(s.Size().Width, 200)
	s.Popup.Resize(fyne.NewSize(width, height))
	origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(s)
	s.Popup.ShowAtPosition(origin.Add(fyne.NewPos(0, s.Size().Height)))
	s.centerActive()
	UnfoldPopUp(s.Popup, s.unfold, s.Popup.Size())
	if s.Find != nil {
		c.Focus(s.Find) // type straight away, without clicking the field first
	}
}

func (s *GoldSelect) renderChoices() {
	s.Rows.Objects = nil
	if len(s.shown) == 0 {
		s.Rows.Add(widget.NewLabel("No matches"))
	}
	for position, i := range s.shown {
		i, option := i, s.Options[i]
		selected := option == s.Selected
		current := strings.HasSuffix(option, "- Current")
		ink := theme.Color(theme.ColorNameForeground)
		fill := theme.Color(theme.ColorNameOverlayBackground)
		if current {
			ink = widgets.LogoGold
		}
		if selected {
			fill = widgets.LogoGold
			ink = color.NRGBA{R: 24, G: 24, B: 24, A: 255}
		} else if position == s.active {
			fill = BlendColor(fill, widgets.LogoGold, 0.14)
		}
		label := widget.NewLabel(option)
		label.Wrapping = fyne.TextWrapWord
		bg := canvas.NewRectangle(fill)
		bg.CornerRadius = 5
		row := container.NewStack(bg, container.NewPadded(container.NewThemeOverride(label, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: ink})))
		s.Rows.Add(NewCardTappable(row, func() { s.Choose(i) }))
	}
	s.Rows.Refresh()
	s.revealActive()
}

func (s *GoldSelect) revealActive() {
	if s.Scroll == nil || s.active < 0 || s.active >= len(s.Rows.Objects) {
		return
	}
	row := s.Rows.Objects[s.active]
	top := s.Rows.Position().Y + row.Position().Y
	bottom := top + row.Size().Height
	offset := s.Scroll.Offset
	if top < offset.Y {
		offset.Y = top
	} else if bottom > offset.Y+s.Scroll.Size().Height {
		offset.Y = bottom - s.Scroll.Size().Height
	}
	s.Scroll.ScrollToOffset(offset)
}

func (s *GoldSelect) centerActive() {
	if s.Scroll == nil || s.active < 0 || s.active >= len(s.Rows.Objects) {
		return
	}
	s.Scroll.Refresh()
	row := s.Rows.Objects[s.active]
	y := s.Rows.Position().Y + row.Position().Y + row.Size().Height/2 - s.Scroll.Size().Height/2
	s.Scroll.ScrollToOffset(fyne.NewPos(0, fyne.Max(0, y)))
}
