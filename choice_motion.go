package main

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
type choiceFeedback struct {
	widget.BaseWidget
	content   fyne.CanvasObject
	pulse     *canvas.Rectangle
	animation *fyne.Animation
}

func animatedChoice(content fyne.CanvasObject) fyne.CanvasObject {
	f := &choiceFeedback{content: content, pulse: canvas.NewRectangle(color.Transparent)}
	f.pulse.CornerRadius = 6
	f.ExtendBaseWidget(f)
	checkContent := content
	if group, ok := content.(*compactCheckGroup); ok {
		checkContent = &group.CheckGroup
	}
	switch check := checkContent.(type) {
	case *widget.Check:
		changed := check.OnChanged
		check.OnChanged = func(value bool) {
			f.flash()
			if changed != nil {
				changed(value)
			}
		}
	case *widget.CheckGroup:
		changed := check.OnChanged
		check.OnChanged = func(value []string) {
			f.flash()
			if changed != nil {
				changed(value)
			}
		}
	default:
		return content
	}
	return f
}
func (f *choiceFeedback) flash() {
	if f.animation != nil {
		f.animation.Stop()
	}
	f.pulse.FillColor = withAlpha(logoGold, 0.18)
	f.pulse.Refresh()
	app := fyne.CurrentApp()
	f.animation = fyne.NewAnimation(160*time.Millisecond, func(p float32) {
		if fyne.CurrentApp() != app {
			return
		}
		f.pulse.FillColor = withAlpha(logoGold, 0.18*(1-p))
		f.pulse.Refresh()
	})
	f.animation.Curve = fyne.AnimationEaseOut
	f.animation.Start()
}
func (f *choiceFeedback) CreateRenderer() fyne.WidgetRenderer {
	return &choiceRenderer{WidgetRenderer: widget.NewSimpleRenderer(container.NewStack(f.content, f.pulse)), feedback: f}
}

type choiceRenderer struct {
	fyne.WidgetRenderer
	feedback *choiceFeedback
}

func (r *choiceRenderer) Destroy() {
	if r.feedback.animation != nil {
		r.feedback.animation.Stop()
	}
	r.WidgetRenderer.Destroy()
}

type dropdownTheme struct {
	fyne.Theme
	current bool
}

func (t dropdownTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		return logoGold
	case theme.ColorNameFocus, theme.ColorNameSelection:
		return withAlpha(logoGold, 0.18)
	case theme.ColorNameForeground:
		if t.current {
			return logoGold
		}
	}
	return t.Theme.Color(name, v)
}
