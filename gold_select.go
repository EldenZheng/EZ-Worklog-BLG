package main

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Keep the native Select renderer and selection API, with explicit gold rows
// in its popup so selected and current options remain distinguishable.
type goldSelect struct {
	widget.Select
	popup  *widget.PopUp
	rows   *fyne.Container
	active int
	flash  *choiceFeedback
	scroll *container.Scroll
}

func newGoldSelect(options []string, changed func(string)) *goldSelect {
	s := &goldSelect{}
	s.Options = options
	s.OnChanged = changed
	s.ExtendBaseWidget(s)
	return s
}
func (s *goldSelect) Tapped(*fyne.PointEvent) {
	if !s.Disabled() {
		s.showChoices()
	}
}
func (s *goldSelect) TypedKey(e *fyne.KeyEvent) {
	if s.Disabled() {
		return
	}
	switch e.Name {
	case fyne.KeyEscape:
		s.closeChoices()
	case fyne.KeyReturn, fyne.KeyEnter, fyne.KeySpace:
		if s.popup != nil && s.popup.Visible() {
			s.choose(s.active)
		} else {
			s.showChoices()
		}
	case fyne.KeyDown, fyne.KeyUp:
		if s.popup == nil || !s.popup.Visible() {
			s.showChoices()
			return
		}
		if e.Name == fyne.KeyDown && s.active < len(s.Options)-1 {
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
func (s *goldSelect) choose(index int) {
	if index < 0 || index >= len(s.Options) {
		return
	}
	value := s.Options[index]
	s.closeChoices()
	s.SetSelected(value)
}
func (s *goldSelect) closeChoices() {
	if s.flash != nil && s.flash.animation != nil {
		s.flash.animation.Stop()
	}
	if s.popup != nil {
		s.popup.Hide()
		s.popup = nil
	}
	s.scroll = nil
}
func (s *goldSelect) Hide() { s.closeChoices(); s.Select.Hide() }
func (s *goldSelect) showChoices() {
	if len(s.Options) == 0 {
		return
	}
	s.closeChoices()
	c := fyne.CurrentApp().Driver().CanvasForObject(s)
	if c == nil {
		return
	}
	c.Focus(s)
	s.active = s.SelectedIndex()
	if s.active < 0 {
		s.active = 0
	}
	s.rows = container.NewVBox()
	s.renderChoices()
	height := fyne.Min(s.rows.MinSize().Height+8, 320)
	var content fyne.CanvasObject = s.rows
	if s.rows.MinSize().Height > height || strings.Contains(s.Options[0], " | ") {
		top, bottom := canvas.NewRectangle(color.Transparent), canvas.NewRectangle(color.Transparent)
		top.SetMinSize(fyne.NewSize(0, height/2))
		bottom.SetMinSize(fyne.NewSize(0, height/2))
		content = container.NewVBox(top, s.rows, bottom)
	}
	s.scroll = container.NewVScroll(content)
	s.flash = &choiceFeedback{content: s.scroll, pulse: canvas.NewRectangle(color.Transparent)}
	s.flash.pulse.CornerRadius = 6
	s.flash.ExtendBaseWidget(s.flash)
	s.popup = widget.NewPopUp(s.flash, c)
	width := fyne.Max(s.Size().Width, 200)
	s.popup.Resize(fyne.NewSize(width, height))
	origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(s)
	s.popup.ShowAtPosition(origin.Add(fyne.NewPos(0, s.Size().Height)))
	s.centerActive()
	s.flash.flash()
}
func (s *goldSelect) renderChoices() {
	s.rows.Objects = nil
	for i, option := range s.Options {
		i, option := i, option
		selected := option == s.Selected
		current := strings.HasSuffix(option, "- Current")
		ink := theme.Color(theme.ColorNameForeground)
		fill := theme.Color(theme.ColorNameOverlayBackground)
		if current {
			ink = logoGold
		}
		if selected {
			fill = logoGold
			ink = color.NRGBA{R: 24, G: 24, B: 24, A: 255}
		} else if i == s.active {
			fill = blendColor(fill, logoGold, 0.14)
		}
		label := widget.NewLabel(option)
		label.Wrapping = fyne.TextWrapWord
		bg := canvas.NewRectangle(fill)
		bg.CornerRadius = 5
		row := container.NewStack(bg, container.NewPadded(container.NewThemeOverride(label, statusHeaderTheme{Theme: theme.Current(), ink: ink})))
		s.rows.Add(newCardTappable(row, func() { s.choose(i) }))
	}
	s.rows.Refresh()
	s.revealActive()
}

func (s *goldSelect) revealActive() {
	if s.scroll == nil || s.active < 0 || s.active >= len(s.rows.Objects) {
		return
	}
	row := s.rows.Objects[s.active]
	top := s.rows.Position().Y + row.Position().Y
	bottom := top + row.Size().Height
	offset := s.scroll.Offset
	if top < offset.Y {
		offset.Y = top
	} else if bottom > offset.Y+s.scroll.Size().Height {
		offset.Y = bottom - s.scroll.Size().Height
	}
	s.scroll.ScrollToOffset(offset)
}
func (s *goldSelect) centerActive() {
	if s.scroll == nil || s.active < 0 || s.active >= len(s.rows.Objects) {
		return
	}
	s.scroll.Refresh()
	row := s.rows.Objects[s.active]
	y := s.rows.Position().Y + row.Position().Y + row.Size().Height/2 - s.scroll.Size().Height/2
	s.scroll.ScrollToOffset(fyne.NewPos(0, fyne.Max(0, y)))
}
