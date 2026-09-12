package main

import (
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
)

// Feedback paints only a neutral outline; the card's semantic fill and accent
// never change. Every transition finishes, including when a card is removed.
type cardFeedback struct {
	outline   *canvas.Rectangle
	level     float32
	hovered   bool
	animation *fyne.Animation
}

func newCardTappable(content fyne.CanvasObject, onTap func()) *tappable {
	outline := canvas.NewRectangle(color.Transparent)
	outline.CornerRadius = 10
	outline.StrokeWidth = 1.5
	outline.StrokeColor = color.Transparent
	t := newTappable(container.NewStack(content, outline), onTap)
	t.feedback = &cardFeedback{outline: outline}
	return t
}

func (t *tappable) MouseIn(*desktop.MouseEvent) {
	if t.feedback != nil {
		t.feedback.hovered = true
		t.feedback.fade(0.24)
	}
}

func (t *tappable) MouseMoved(*desktop.MouseEvent) {}

func (t *tappable) MouseOut() {
	if t.feedback != nil {
		t.feedback.hovered = false
		t.feedback.fade(0)
	}
}

func (t *tappable) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (t *tappable) Hide() {
	if t.feedback != nil {
		t.feedback.stop()
		t.feedback.hovered = false
		t.feedback.paint(0)
	}
	t.BaseWidget.Hide()
}

func (f *cardFeedback) paint(level float32) {
	f.level = level
	f.outline.StrokeColor = withAlpha(theme.Color(theme.ColorNameForeground), level)
	f.outline.Refresh()
}

func (f *cardFeedback) stop() {
	if f.animation != nil {
		f.animation.Stop()
		f.animation = nil
	}
}

func (f *cardFeedback) fade(to float32) {
	f.stop()
	from := f.level
	if from == to {
		return
	}
	f.animation = fyne.NewAnimation(140*time.Millisecond, func(p float32) {
		f.paint(from + (to-from)*p)
		if p == 1 {
			f.animation = nil
		}
	})
	f.animation.Start()
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

type meterPart struct {
	key    string
	fill   color.Color
	weight float32
}

// A fixed slot retains only the displayed day/week, so browsing history does
// not accumulate widgets. First display is immediate; updates interpolate from
// the currently painted shares, including an update arriving mid-animation.
type meterMotion struct {
	identity  string
	bar       *fyne.Container
	fill      *fyne.Container
	track     *canvas.Rectangle
	parts     []meterPart
	target    []meterPart
	animation *fyne.Animation
}

func (ui *UI) progressBar(slot, identity string, byOrg, draft map[string]int, goal int, height float32) fyne.CanvasObject {
	if ui.meters == nil {
		ui.meters = map[string]*meterMotion{}
	}
	parts := meterParts(ui.cfg, byOrg, draft, goal)
	m := ui.meters[slot]
	if m == nil || m.identity != identity {
		if m != nil {
			m.finish()
		}
		track := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
		track.CornerRadius = height / 2
		track.SetMinSize(fyne.NewSize(0, height))
		m = &meterMotion{identity: identity, track: track}
		m.fill = container.New(m)
		m.bar = container.NewStack(track, m.fill)
		m.setParts(parts)
		m.target = append([]meterPart(nil), parts...)
		ui.meters[slot] = m
		return m.bar
	}
	m.track.FillColor = theme.Color(theme.ColorNameInputBackground)
	m.track.Refresh()
	m.update(parts)
	// Data refreshes can redraw another tab in the background. Paint its final
	// reading directly rather than spending frames on a hidden view.
	if ui.tabs != nil && ui.tabs.Selected() != nil {
		owner := logListTabName
		if strings.HasPrefix(slot, "strip/") {
			owner = logWorkTabName
		}
		if slot == "detail" {
			owner = statusTabName
		}
		if ui.tabs.Selected().Text != owner {
			m.finish()
		}
	}
	return m.bar
}

func sameMeterParts(a, b []meterPart) bool {
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

func (m *meterMotion) setParts(parts []meterPart) {
	m.parts = append([]meterPart(nil), parts...)
	objects := make([]fyne.CanvasObject, len(parts))
	for i, part := range parts {
		r := canvas.NewRectangle(part.fill)
		r.CornerRadius = m.track.CornerRadius
		objects[i] = r
	}
	m.fill.Objects = objects
	m.fill.Refresh()
}

func (m *meterMotion) finish() {
	if m.animation != nil {
		m.animation.Stop()
		m.animation = nil
		m.setParts(m.target)
	}
}

func (ui *UI) finishProgress() {
	for _, m := range ui.meters {
		m.finish()
	}
}

func (m *meterMotion) update(next []meterPart) {
	if sameMeterParts(m.target, next) {
		return
	}
	if m.animation != nil {
		m.animation.Stop()
		m.animation = nil
	}
	m.target = append([]meterPart(nil), next...)
	from := make(map[string]meterPart, len(m.parts))
	for _, part := range m.parts {
		from[part.key] = part
	}
	start := make([]meterPart, 0, len(next)+len(m.parts))
	start = append(start, next...)
	// Insert departing segments before their old neighbour, so the remaining
	// segments slide into the space instead of jumping as an org is hidden.
	nextKeys := make(map[string]bool, len(next))
	for _, part := range next {
		nextKeys[part.key] = true
	}
	for i := len(m.parts) - 1; i >= 0; i-- {
		part := m.parts[i]
		if nextKeys[part.key] {
			continue
		}
		at := len(start)
		if i+1 < len(m.parts) {
			for j, candidate := range start {
				if candidate.key == m.parts[i+1].key {
					at = j
					break
				}
			}
		}
		part.weight = 0
		start = append(start, meterPart{})
		copy(start[at+1:], start[at:])
		start[at] = part
	}
	end := make([]float32, len(start))
	for i, part := range start {
		end[i] = part.weight
		start[i].weight = from[part.key].weight
	}
	m.setParts(start)
	m.animation = fyne.NewAnimation(250*time.Millisecond, func(p float32) {
		for i := range m.parts {
			m.parts[i].weight = start[i].weight + (end[i]-start[i].weight)*p
		}
		m.fill.Refresh()
		if p == 1 {
			m.animation = nil
			m.setParts(m.target)
		}
	})
	m.animation.Start()
}

func (m *meterMotion) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	x := float32(0)
	for i, o := range objects {
		width := size.Width * m.parts[i].weight
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(width, size.Height))
		x += width
	}
}

func (m *meterMotion) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.Size{} }
