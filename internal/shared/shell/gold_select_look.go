package shell

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

const dropdownCorner = float32(8)

// DropdownFrame is the gold outline and inset shared by option menus.
func DropdownFrame(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameOverlayBackground))
	bg.CornerRadius = dropdownCorner + theme.Padding()
	bg.StrokeColor, bg.StrokeWidth = widgets.WithAlpha(widgets.LogoGold, .85), 1.2
	pad := 2 * theme.Padding()
	return container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(pad, pad, pad, pad), NoScrollShadow(content)))
}

func (s *GoldSelect) MouseIn(e *desktop.MouseEvent) {
	if s.PlainField {
		s.Select.MouseIn(e)
		return
	}
	if !s.Disabled() && s.fieldHover != nil {
		s.fieldHover.MoveTo(fyne.Position{}, s.Size(), true)
	}
}
func (s *GoldSelect) MouseMoved(e *desktop.MouseEvent) {
	if s.PlainField {
		s.Select.MouseMoved(e)
	}
}
func (s *GoldSelect) MouseOut() {
	if s.PlainField {
		s.Select.MouseOut()
		return
	}
	if s.fieldHover != nil {
		s.fieldHover.MoveTo(fyne.Position{}, fyne.Size{}, false)
	}
}
func (s *GoldSelect) FocusGained() { s.fieldFocused = true; s.Select.FocusGained() }
func (s *GoldSelect) FocusLost()   { s.fieldFocused = false; s.Select.FocusLost() }

func (s *GoldSelect) CreateRenderer() fyne.WidgetRenderer {
	if s.PlainField {
		// Fyne's own field, with its arrow swapped for the turning triangle.
		base := s.Select.CreateRenderer()
		r := &plainSelectRenderer{WidgetRenderer: base, s: s, arrow: NewSpinTriangle(plainSelectInk(s), s.Popup != nil)}
		for _, o := range base.Objects() {
			if icon, ok := o.(*widget.Icon); ok {
				r.icon = icon
			}
		}
		s.arrow = r.arrow
		r.Refresh()
		return r
	}
	s.fieldHover = widgets.NewHoverPill()
	label := widget.NewLabel("")
	label.Truncation = fyne.TextTruncateEllipsis
	r := &goldSelectRenderer{s: s, hover: s.fieldHover, bg: canvas.NewRectangle(nil), label: label,
		arrow: NewSpinTriangle(widgets.LogoGold, s.Popup != nil)}
	s.arrow = r.arrow
	r.words = container.NewThemeOverride(label, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	r.Refresh()
	return r
}

type goldSelectRenderer struct {
	s     *GoldSelect
	hover *widgets.HoverPill
	bg    *canvas.Rectangle
	label *widget.Label
	words *container.ThemeOverride
	arrow *SpinTriangle
}

func (r *goldSelectRenderer) MinSize() fyne.Size {
	placeholder := r.s.PlaceHolder
	if placeholder == "" {
		placeholder = "(Select one)"
	}
	text := fyne.MeasureText(placeholder, theme.TextSize(), fyne.TextStyle{})
	pad := theme.InnerPadding()
	return fyne.NewSize(text.Width+4*pad+theme.IconInlineSize(), text.Height+2*pad)
}
func (r *goldSelectRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	pad, icon := theme.InnerPadding(), theme.IconInlineSize()
	r.arrow.Move(fyne.NewPos(max(0, size.Width-pad-icon), (size.Height-icon)/2))
	r.arrow.Resize(fyne.NewSquareSize(icon))
	// A row taller than the field stretches it; keep the words centred with
	// the arrow rather than riding at the top.
	textH := min(size.Height, r.words.MinSize().Height)
	r.words.Move(fyne.NewPos(pad/2, (size.Height-textH)/2))
	r.words.Resize(fyne.NewSize(max(0, size.Width-2*pad-icon), textH))
	r.hover.Fit(fyne.Position{}, size)
}
func (r *goldSelectRenderer) Refresh() {
	s := r.s
	ink := color.Color(widgets.LogoGold)
	alpha := float32(.65)
	if s.fieldFocused {
		alpha = 1
	}
	r.bg.FillColor = BlendColor(theme.Color(theme.ColorNameInputBackground), widgets.LogoGold, .10)
	if s.Disabled() {
		ink = theme.Color(theme.ColorNameDisabled)
		r.bg.FillColor = theme.Color(theme.ColorNameDisabledButton)
		r.hover.Stop()
		r.hover.Rect.Hide()
	} else {
		r.hover.Rect.Show()
	}
	r.bg.CornerRadius, r.bg.StrokeWidth = dropdownCorner, 1.2
	r.bg.StrokeColor = widgets.WithAlpha(ink, alpha)
	text := s.Selected
	if text == "" {
		text = s.PlaceHolder
	}
	if text == "" {
		text = "(Select one)"
	}
	r.label.Alignment = s.Alignment
	r.label.SetText(text)
	r.words.Theme = widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: ink}
	r.words.Refresh()
	r.arrow.SetInk(ink)
	r.bg.Refresh()
	r.Layout(s.Size())
}
func (r *goldSelectRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.hover.Rect, r.words, r.arrow}
}
func (r *goldSelectRenderer) Destroy() {
	r.hover.Stop()
	r.arrow.Stop()
}

// plainSelectRenderer is Fyne's field (PlainField) with the turning triangle
// where Fyne puts its arrow, inked as Fyne inks its own.
type plainSelectRenderer struct {
	fyne.WidgetRenderer
	s     *GoldSelect
	icon  *widget.Icon
	arrow *SpinTriangle
}

func plainSelectInk(s *GoldSelect) color.Color {
	if s.Disabled() {
		return theme.ColorForWidget(theme.ColorNameDisabled, s)
	}
	return theme.ColorForWidget(theme.ColorNameForeground, s)
}

func (r *plainSelectRenderer) place() {
	if r.icon == nil {
		return
	}
	r.icon.Hide()
	r.arrow.Move(r.icon.Position())
	r.arrow.Resize(r.icon.Size())
}
func (r *plainSelectRenderer) Layout(size fyne.Size) { r.WidgetRenderer.Layout(size); r.place() }
func (r *plainSelectRenderer) Refresh() {
	r.WidgetRenderer.Refresh()
	r.arrow.SetInk(plainSelectInk(r.s))
	r.place()
}
func (r *plainSelectRenderer) Objects() []fyne.CanvasObject {
	return append(append([]fyne.CanvasObject{}, r.WidgetRenderer.Objects()...), r.arrow)
}
func (r *plainSelectRenderer) Destroy() { r.arrow.Stop(); r.WidgetRenderer.Destroy() }

type dropdownChoice struct {
	row    *Tappable
	look   *tabLook
	mark   *dropdownMark
	centre *fyne.Position
}

func newDropdownChoice(text fyne.CanvasObject, fill, ink color.Color, selected bool, chosen func()) *dropdownChoice {
	mark := &dropdownMark{short: canvas.NewLine(tabSelectedInk), long: canvas.NewLine(tabSelectedInk)}
	mark.ExtendBaseWidget(mark)
	body := container.NewPadded(container.NewBorder(nil, nil, nil, mark, text))
	look := newTabLook(body, dropdownRowTheme{Theme: theme.Current(), fill: fill, ink: ink})
	look.radius = dropdownCorner
	look.onShare = func(share float32) { mark.share = min(1, share*1.5); mark.Refresh() }
	look.set(boolShare(selected))
	surface := container.New(&dropdownChoiceLayout{look: look}, look.bg, look.fill, look.over)
	choice := &dropdownChoice{look: look, mark: mark}
	choice.row = NewTappable(surface, chosen)
	choice.row.OnTapAt = func(at fyne.Position) {
		local := at.Subtract(fyne.CurrentApp().Driver().AbsolutePositionForObject(choice.row))
		choice.centre = &local
	}
	return choice
}

func boolShare(on bool) float32 {
	if on {
		return 1
	}
	return 0
}

type dropdownRowTheme struct {
	fyne.Theme
	fill, ink color.Color
}

func (t dropdownRowTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameButton {
		return t.fill
	}
	if name == theme.ColorNameForeground {
		return t.ink
	}
	return t.Theme.Color(name, v)
}

type dropdownChoiceLayout struct{ look *tabLook }

func (l *dropdownChoiceLayout) MinSize([]fyne.CanvasObject) fyne.Size { return l.look.over.MinSize() }
func (l *dropdownChoiceLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	l.look.over.Resize(size)
	l.look.place(fyne.Position{}, size)
}

// Draw the same two check strokes as the app's animated ticks, without a box.
type dropdownMark struct {
	widget.BaseWidget
	short, long *canvas.Line
	share       float32
}

func (m *dropdownMark) CreateRenderer() fyne.WidgetRenderer { return &dropdownMarkRenderer{mark: m} }

type dropdownMarkRenderer struct{ mark *dropdownMark }

func (r *dropdownMarkRenderer) MinSize() fyne.Size { return fyne.NewSquareSize(theme.IconInlineSize()) }
func (r *dropdownMarkRenderer) Layout(size fyne.Size) {
	side := min(size.Width, size.Height)
	widgets.DrawCheckMark(r.mark.short, r.mark.long, fyne.NewPos((size.Width-side)/2, (size.Height-side)/2), side, tabSelectedInk, r.mark.share)
}
func (r *dropdownMarkRenderer) Refresh() { r.Layout(r.mark.Size()); Repaint(r.mark) }
func (r *dropdownMarkRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.mark.short, r.mark.long}
}
func (r *dropdownMarkRenderer) Destroy() {}
