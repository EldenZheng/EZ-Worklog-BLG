package tests

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"worklog/internal/shared/widgets"
)

func TestHoverPillSlidesFromCurrentFrameAndCancelsStaleFrames(t *testing.T) {
	newMotionTestApp(t)
	p := widgets.NewHoverPill()
	size := fyne.NewSize(120, 28)
	p.MoveTo(fyne.NewPos(0, 0), size, true)
	if p.Animation.Duration != 600*time.Millisecond {
		t.Fatal("hover transitions should take 600ms")
	}
	p.Animation.Tick(1)
	p.MoveTo(fyne.NewPos(0, 40), size, true)
	old := p.Animation
	old.Tick(0.5)
	y := p.Rect.Position().Y
	if y <= 0 || y >= 40 {
		t.Fatal("hover should slide between rows")
	}
	p.MoveTo(fyne.NewPos(0, 80), size, true)
	if p.Rect.Position().Y != y {
		t.Fatal("an interrupted slide must continue from its current position")
	}
	old.Tick(1)
	if p.Rect.Position().Y != y {
		t.Fatal("a cancelled frame must not move the highlight")
	}
	p.Animation.Tick(1)
	p.Fit(fyne.NewPos(0, 80), fyne.NewSize(240, 28))
	if p.Rect.Size().Width != 240 {
		t.Fatal("resize should keep the pill aligned")
	}
	p.MoveTo(fyne.Position{}, fyne.Size{}, false)
	p.Animation.Tick(1)
	if _, _, _, alpha := p.Rect.FillColor.RGBA(); alpha != 0 {
		t.Fatal("leaving must fade out")
	}
	p.MoveTo(fyne.NewPos(0, 0), size, true)
	stopped := p.Animation
	p.Stop()
	stopped.Tick(1)
	if _, _, _, alpha := p.Rect.FillColor.RGBA(); alpha != 0 {
		t.Fatal("destroyed feedback must ignore pending frames")
	}
}
