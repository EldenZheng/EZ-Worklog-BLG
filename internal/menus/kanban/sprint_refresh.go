package kanban

import (
	"fmt"
	"math"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
)

// sprintRefreshButton stays still on hover. A click turns the existing refresh
// glyph until loading ends, then eases it back to rest without a tap pulse.
type sprintRefreshButton struct {
	widget.Button
	key    string
	spin   *fyne.Animation
	frames []fyne.Resource
	phase  float32
	ending bool
}

func newSprintRefreshButton(k *shell.KanbanUI, key string) *sprintRefreshButton {
	b := &sprintRefreshButton{key: key}
	b.Icon = theme.ViewRefreshIcon()
	b.ExtendBaseWidget(b)
	b.OnTapped = func() {
		if k.Loading || k.Busy || len(k.Queue) > 0 {
			return
		}
		b.turn()
		b.Disable()
		k.RefreshSprint(key)
	}
	return b
}

func (*sprintRefreshButton) MouseIn(*desktop.MouseEvent) {}
func (*sprintRefreshButton) MouseOut()                   {}

func (b *sprintRefreshButton) Tapped(*fyne.PointEvent) {
	if !b.Disabled() && b.OnTapped != nil {
		b.OnTapped()
	}
}

func (b *sprintRefreshButton) TypedKey(e *fyne.KeyEvent) {
	if e.Name == fyne.KeySpace {
		b.Tapped(nil)
	}
}

func (b *sprintRefreshButton) stop() {
	if b.spin != nil {
		b.spin.Stop()
		b.spin = nil
	}
	b.SetIcon(theme.ViewRefreshIcon())
	b.phase = 0
	b.ending = false
}

func (b *sprintRefreshButton) turn() {
	if b.spin != nil {
		b.spin.Stop()
		b.spin = nil
	}
	b.ending = false
	if !fyne.CurrentApp().Settings().ShowAnimations() {
		return
	}
	if b.frames == nil {
		// Rotate the existing vector glyph about its 24px viewbox centre. Cache
		// the frames so later clicks reuse Fyne's rendered icon resources.
		svg := string(theme.ViewRefreshIcon().Content())
		start, end := strings.IndexByte(svg, '>')+1, strings.LastIndex(svg, "</svg>")
		if start == 0 || end < start {
			return
		}
		for frame := 0; frame < 120; frame++ {
			rotated := fmt.Sprintf(`%s<g transform="rotate(%d 12 12)">%s</g></svg>`, svg[:start], frame*3, svg[start:end])
			b.frames = append(b.frames, theme.NewThemedResource(fyne.NewStaticResource(fmt.Sprintf("sprint-refresh-%02d.svg", frame), []byte(rotated))))
		}
	}
	var spin *fyne.Animation
	from := b.phase
	spin = fyne.NewAnimation(600*time.Millisecond, func(p float32) {
		if b.spin != spin {
			return
		}
		b.paintTurn(from + p)
	})
	spin.Curve = fyne.AnimationLinear
	spin.RepeatCount = fyne.AnimationRepeatForever
	b.spin = spin
	spin.Start()
}

// FinishRefresh keeps the current angle and speed, then decelerates to a whole
// turn. SyncActivity calls it after the data has finished loading, even on error.
func (b *sprintRefreshButton) FinishRefresh() {
	if b.spin == nil || b.ending {
		return
	}
	b.spin.Stop()
	b.ending = true
	from := b.phase
	left := 1 - from
	if left < .25 {
		left++
	}
	var settle *fyne.Animation
	// The quadratic ease starts at the loop's speed and reaches zero at rest.
	settle = fyne.NewAnimation(time.Duration(float32(1200*time.Millisecond)*left), func(p float32) {
		if b.spin != settle {
			return
		}
		if p >= 1 {
			b.stop()
			return
		}
		b.paintTurn(from + left*p)
	})
	settle.Curve = func(p float32) float32 { return p * (2 - p) }
	b.spin = settle
	settle.Start()
}

func (b *sprintRefreshButton) paintTurn(turn float32) {
	b.phase = turn - float32(math.Floor(float64(turn)))
	icon := b.frames[int(b.phase*float32(len(b.frames)))]
	if b.Icon != icon {
		b.SetIcon(icon)
	}
}

func sprintRefreshControl(k *shell.KanbanUI, key string) fyne.CanvasObject {
	refresh, ok := k.SprintRefresh.(*sprintRefreshButton)
	if !ok || refresh.key != key {
		if ok {
			refresh.stop()
		}
		refresh = newSprintRefreshButton(k, key)
		k.SprintRefresh = refresh
	}
	if k.Loading || k.Busy || len(k.Queue) > 0 {
		refresh.Disable()
	} else {
		refresh.Enable()
	}
	return shell.QuietButtonControl(refresh, &refresh.Button)
}
