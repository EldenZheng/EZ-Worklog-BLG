package widgets

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Native checks keep their normal focus, keyboard and data-binding behavior.
// A short gold pulse supplies the same lightweight feedback as Log Work cards.
type ChoiceFeedback struct {
	widget.BaseWidget
	Content   fyne.CanvasObject
	Pulse     *canvas.Rectangle
	Animation *fyne.Animation
	tick      *TickMotion
	pill      *HoverPill
}

// AnimatedTickChoice gives a single native check the Kanban filter's hover,
// ripple and drawn tick while preserving its keyboard and SetChecked behavior.
func AnimatedTickChoice(check *widget.Check) fyne.CanvasObject {
	f := &ChoiceFeedback{Content: check, tick: NewTickMotion(LogoGold), pill: NewHoverPill()}
	f.ExtendBaseWidget(f)
	changed := check.OnChanged
	check.OnChanged = func(on bool) {
		f.tick.Play(on)
		if changed != nil {
			changed(on)
		}
	}
	return f
}

func AnimatedChoice(content fyne.CanvasObject) fyne.CanvasObject {
	f := &ChoiceFeedback{Content: content, Pulse: canvas.NewRectangle(color.Transparent)}
	f.Pulse.CornerRadius = 6
	f.ExtendBaseWidget(f)
	checkContent := content
	if group, ok := content.(*CompactCheckGroup); ok {
		checkContent = &group.CheckGroup
	}
	switch check := checkContent.(type) {
	case *widget.Check:
		changed := check.OnChanged
		check.OnChanged = func(value bool) {
			f.Flash()
			if changed != nil {
				changed(value)
			}
		}
	case *widget.CheckGroup:
		changed := check.OnChanged
		check.OnChanged = func(value []string) {
			f.Flash()
			if changed != nil {
				changed(value)
			}
		}
	default:
		return content
	}
	return f
}

func (f *ChoiceFeedback) Flash() {
	if f.Animation != nil {
		f.Animation.Stop()
	}
	f.Pulse.FillColor = WithAlpha(LogoGold, 0.18)
	f.Pulse.Refresh()
	app := fyne.CurrentApp()
	f.Animation = fyne.NewAnimation(160*time.Millisecond, func(p float32) {
		if fyne.CurrentApp() != app {
			return
		}
		f.Pulse.FillColor = WithAlpha(LogoGold, 0.18*(1-p))
		f.Pulse.Refresh()
	})
	f.Animation.Curve = fyne.AnimationEaseOut
	f.Animation.Start()
}

func (f *ChoiceFeedback) CreateRenderer() fyne.WidgetRenderer {
	if f.tick != nil {
		catcher := &tickChoiceCatcher{f: f}
		catcher.ExtendBaseWidget(catcher)
		return &tickChoiceRenderer{f: f, catcher: catcher}
	}
	return &choiceRenderer{WidgetRenderer: widget.NewSimpleRenderer(container.NewStack(f.Content, f.Pulse)), feedback: f}
}

type tickChoiceCatcher struct {
	widget.BaseWidget
	f *ChoiceFeedback
}

func (c *tickChoiceCatcher) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

func (c *tickChoiceCatcher) Tapped(e *fyne.PointEvent) {
	check := c.f.Content.(*widget.Check)
	if check.Disabled() {
		return
	}
	box := theme.InnerPadding()/2 + theme.Size(theme.SizeNameInputBorder) + theme.Size(theme.SizeNameInlineIcon) + theme.Padding()
	ripple(c.f, fyne.NewPos(box, 1), fyne.NewSize(max(0, c.Size().Width-box), max(0, c.Size().Height-2)), e)
	check.SetChecked(!check.Checked)
}

func (c *tickChoiceCatcher) MouseIn(*desktop.MouseEvent) {
	if !c.f.Content.(*widget.Check).Disabled() {
		c.f.pill.MoveTo(fyne.NewPos(0, 1), fyne.NewSize(c.Size().Width, max(0, c.Size().Height-2)), true)
	}
}
func (c *tickChoiceCatcher) MouseMoved(*desktop.MouseEvent) {}
func (c *tickChoiceCatcher) MouseOut() {
	c.f.pill.MoveTo(fyne.Position{}, fyne.Size{}, false)
}
func (c *tickChoiceCatcher) Cursor() desktop.Cursor { return desktop.PointerCursor }

type tickChoiceRenderer struct {
	f       *ChoiceFeedback
	catcher *tickChoiceCatcher
}

func (r *tickChoiceRenderer) Layout(size fyne.Size) {
	r.f.Content.Move(fyne.Position{})
	r.f.Content.Resize(size)
	r.catcher.Move(fyne.Position{})
	r.catcher.Resize(size)
	r.f.tick.PlaceOnCheck(fyne.Position{}, size)
	r.f.pill.Fit(fyne.NewPos(0, 1), fyne.NewSize(size.Width, max(0, size.Height-2)))
	if r.f.Content.Visible() {
		r.catcher.Show()
		r.f.pill.Rect.Show()
	} else {
		r.catcher.Hide()
		r.f.tick.Stop()
		r.f.pill.Stop()
		r.f.pill.Rect.Hide()
	}
}
func (r *tickChoiceRenderer) MinSize() fyne.Size {
	if !r.f.Content.Visible() {
		return fyne.Size{}
	}
	return r.f.Content.MinSize()
}
func (r *tickChoiceRenderer) Objects() []fyne.CanvasObject {
	objects := append([]fyne.CanvasObject{r.f.pill.Rect, r.f.Content}, r.f.tick.Objects()...)
	return append(objects, r.catcher)
}
func (r *tickChoiceRenderer) Refresh() { r.f.Content.Refresh(); r.Layout(r.f.Size()) }
func (r *tickChoiceRenderer) Destroy() { r.f.tick.Stop(); r.f.pill.Stop() }

type choiceRenderer struct {
	fyne.WidgetRenderer
	feedback *ChoiceFeedback
}

func (r *choiceRenderer) Destroy() {
	if r.feedback.Animation != nil {
		r.feedback.Animation.Stop()
	}
	r.WidgetRenderer.Destroy()
}

type DropdownTheme struct {
	fyne.Theme
	Current bool
}

func (t DropdownTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		return LogoGold
	case theme.ColorNameFocus, theme.ColorNameSelection:
		return WithAlpha(LogoGold, 0.18)
	case theme.ColorNameForeground:
		if t.Current {
			return LogoGold
		}
	}
	return t.Theme.Color(name, v)
}
