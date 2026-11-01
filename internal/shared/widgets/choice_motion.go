package widgets

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
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
	return &choiceRenderer{WidgetRenderer: widget.NewSimpleRenderer(container.NewStack(f.Content, f.Pulse)), feedback: f}
}

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
