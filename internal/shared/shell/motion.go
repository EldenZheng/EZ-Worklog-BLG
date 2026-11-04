package shell

import (
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/widgets"
)

// Feedback paints only a neutral outline; the card's semantic fill and accent
// never change. Every transition finishes, including when a card is removed.
type cardFeedback struct {
	outline   *canvas.Rectangle
	Level     float32
	hovered   bool
	Animation *fyne.Animation
}

func NewCardTappable(content fyne.CanvasObject, onTap func()) *Tappable {
	outline := canvas.NewRectangle(color.Transparent)
	outline.CornerRadius = 10
	outline.StrokeWidth = 1.5
	outline.StrokeColor = color.Transparent
	t := NewTappable(container.NewStack(content, outline), onTap)
	t.Feedback = &cardFeedback{outline: outline}
	return t
}

// NewBubbleTappable is a clickable StripeBubble: the shared card look, lit
// under the pointer, with links and buttons inside it keeping it lit.
func NewBubbleTappable(bubble *StripeBubble, onTap func()) *Tappable {
	t := NewTappable(bubble, onTap)
	t.Bubble = bubble
	return t
}

func (t *Tappable) MouseIn(*desktop.MouseEvent) {
	if t.Bubble != nil {
		t.Bubble.SetHover(true)
	}
	if t.Feedback != nil {
		t.Feedback.hovered = true
		t.Feedback.fade(0.24)
	}
}

func (t *Tappable) MouseMoved(*desktop.MouseEvent) {}

func (t *Tappable) MouseOut() {
	if t.Bubble != nil {
		t.Bubble.SetHover(false)
	}
	if t.Feedback != nil {
		t.Feedback.hovered = false
		t.Feedback.fade(0)
	}
}

func (t *Tappable) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (t *Tappable) Hide() {
	if t.Feedback != nil {
		t.Feedback.stop()
		t.Feedback.hovered = false
		t.Feedback.paint(0)
	}
	t.BaseWidget.Hide()
}

func (f *cardFeedback) paint(level float32) {
	f.Level = level
	f.outline.StrokeColor = widgets.WithAlpha(theme.Color(theme.ColorNameForeground), level)
	// A faint wash fades in with the outline, so the hover reads as the card
	// lighting up rather than only being edged.
	f.outline.FillColor = widgets.WithAlpha(theme.Color(theme.ColorNameForeground), level*0.22)
	f.outline.Refresh()
}

func (f *cardFeedback) stop() {
	if f.Animation != nil {
		f.Animation.Stop()
		f.Animation = nil
	}
}

func (f *cardFeedback) fade(to float32) {
	f.stop()
	from := f.Level
	if from == to {
		return
	}
	f.Animation = fyne.NewAnimation(180*time.Millisecond, func(p float32) {
		f.paint(from + (to-from)*p)
		if p == 1 {
			f.Animation = nil
		}
	})
	f.Animation.Start()
}

func (f *cardFeedback) press() {
	f.stop()
	f.paint(0.44)
	to := float32(0)
	if f.hovered {
		to = 0.24
	}
	f.fade(to)
}

type feedbackRenderer struct {
	fyne.WidgetRenderer
	feedback *cardFeedback
}

func (r *feedbackRenderer) Destroy() {
	r.feedback.stop()
	r.WidgetRenderer.Destroy()
}

type MeterPart struct {
	Key    string
	Fill   color.Color
	Weight float32
}

// A fixed slot retains only the displayed day/week, so browsing history does
// not accumulate widgets. First display is immediate; updates interpolate from
// the currently painted shares, including an update arriving mid-animation.
type meterMotion struct {
	identity  string
	bar       *fyne.Container
	Fill      *fyne.Container
	track     *canvas.Rectangle
	Parts     []MeterPart
	Target    []MeterPart
	Animation *fyne.Animation
}

func (ui *UI) ProgressBar(slot, identity string, byOrg, draft map[string]int, goal int, height float32) fyne.CanvasObject {
	return ui.progressBarWithConfig(ui.Cfg, slot, identity, byOrg, draft, goal, height)
}

func (ui *UI) progressBarWithConfig(cfg backend.Config, slot, identity string, byOrg, draft map[string]int, goal int, height float32) fyne.CanvasObject {
	return ui.ProgressBarParts(slot, identity, MeterParts(cfg, byOrg, draft, goal), height)
}

func (ui *UI) ProgressBarParts(slot, identity string, parts []MeterPart, height float32) fyne.CanvasObject {
	if ui.Meters == nil {
		ui.Meters = map[string]*meterMotion{}
	}
	m := ui.Meters[slot]
	if m == nil || m.identity != identity {
		if m != nil {
			m.finish()
		}
		track := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
		track.CornerRadius = height / 2
		track.SetMinSize(fyne.NewSize(0, height))
		m = &meterMotion{identity: identity, track: track}
		m.Fill = container.New(m)
		m.bar = container.NewStack(track, m.Fill)
		m.setParts(parts)
		m.Target = append([]MeterPart(nil), parts...)
		ui.Meters[slot] = m
		return m.bar
	}
	m.track.FillColor = theme.Color(theme.ColorNameInputBackground)
	m.track.Refresh()
	m.update(parts)
	// Data refreshes can redraw another tab in the background. Paint its final
	// reading directly rather than spending frames on a hidden view.
	if ui.Tabs != nil && ui.Tabs.Selected() != nil {
		owner := LogListTabName
		if strings.HasPrefix(slot, "strip/") {
			owner = LogWorkTabName
		}
		if strings.HasPrefix(slot, "planner/") {
			owner = PlannerTabName
		}
		if slot == "detail" {
			owner = StatusTabName
		}
		if ui.Tabs.Selected().Text != owner {
			m.finish()
		}
	}
	return m.bar
}

func sameMeterParts(a, b []MeterPart) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (m *meterMotion) setParts(parts []MeterPart) {
	m.Parts = append([]MeterPart(nil), parts...)
	objects := make([]fyne.CanvasObject, len(parts))
	for i, part := range parts {
		r := canvas.NewRectangle(part.Fill)
		r.CornerRadius = m.track.CornerRadius
		objects[i] = r
	}
	m.Fill.Objects = objects
	m.Fill.Refresh()
}

func (m *meterMotion) finish() {
	if m.Animation != nil {
		m.Animation.Stop()
		m.Animation = nil
		m.setParts(m.Target)
	}
}

func (ui *UI) FinishProgress() {
	for _, m := range ui.Meters {
		m.finish()
	}
}

func (m *meterMotion) update(next []MeterPart) {
	if sameMeterParts(m.Target, next) {
		return
	}
	if m.Animation != nil {
		m.Animation.Stop()
		m.Animation = nil
	}
	m.Target = append([]MeterPart(nil), next...)
	from := make(map[string]MeterPart, len(m.Parts))
	for _, part := range m.Parts {
		from[part.Key] = part
	}
	start := make([]MeterPart, 0, len(next)+len(m.Parts))
	start = append(start, next...)
	// Insert departing segments before their old neighbour, so the remaining
	// segments slide into the space instead of jumping as an org is hidden.
	nextKeys := make(map[string]bool, len(next))
	for _, part := range next {
		nextKeys[part.Key] = true
	}
	for i := len(m.Parts) - 1; i >= 0; i-- {
		part := m.Parts[i]
		if nextKeys[part.Key] {
			continue
		}
		at := len(start)
		if i+1 < len(m.Parts) {
			for j, candidate := range start {
				if candidate.Key == m.Parts[i+1].Key {
					at = j
					break
				}
			}
		}
		part.Weight = 0
		start = append(start, MeterPart{})
		copy(start[at+1:], start[at:])
		start[at] = part
	}
	end := make([]float32, len(start))
	for i, part := range start {
		end[i] = part.Weight
		start[i].Weight = from[part.Key].Weight
	}
	m.setParts(start)
	m.Animation = fyne.NewAnimation(250*time.Millisecond, func(p float32) {
		for i := range m.Parts {
			m.Parts[i].Weight = start[i].Weight + (end[i]-start[i].Weight)*p
		}
		m.Fill.Refresh()
		if p == 1 {
			m.Animation = nil
			m.setParts(m.Target)
		}
	})
	m.Animation.Start()
}

func (m *meterMotion) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	x := float32(0)
	for i, o := range objects {
		width := size.Width * m.Parts[i].Weight
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(width, size.Height))
		x += width
	}
}

func (m *meterMotion) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.Size{} }
