package logwork

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
)

// CommitTextPane keeps the description in place while the remarks enter from
// the right and take half the width. Scrollable supplies clipping only.
type CommitTextPane struct {
	widget.BaseWidget
	Description, Remarks fyne.CanvasObject
	share                float32
	split                bool
	move                 *fyne.Animation
}

func newCommitTextPane(description, remarks fyne.CanvasObject) *CommitTextPane {
	p := &CommitTextPane{Description: description, Remarks: remarks}
	remarks.Hide()
	p.ExtendBaseWidget(p)
	return p
}

func (p *CommitTextPane) setSplit(split, animate bool) {
	if p.split == split {
		return
	}
	p.split = split
	if p.move != nil {
		p.move.Stop()
		p.move = nil
	}
	from, to := p.share, float32(0)
	if split {
		to = 1
		p.Remarks.Show()
	}
	step := func(share float32) {
		p.share = share
		if share == 0 {
			p.Remarks.Hide()
		}
		p.layout(p.Size())
		shell.Repaint(p)
	}
	if !animate || !fyne.CurrentApp().Settings().ShowAnimations() {
		step(to)
		p.Refresh()
		return
	}
	var move *fyne.Animation
	move = fyne.NewAnimation(shell.MorphTime, func(done float32) {
		if p.move != move {
			return
		}
		step(from + (to-from)*done)
		if done >= 1 {
			p.move = nil
			p.Refresh()
		}
	})
	move.Curve = shell.EaseInOutCubic
	p.move = move
	move.Start()
}

func (p *CommitTextPane) layout(size fyne.Size) {
	half := max(0, (size.Width-theme.Padding())/2)
	p.Description.Move(fyne.Position{})
	p.Description.Resize(fyne.NewSize(size.Width-(half+theme.Padding())*p.share, size.Height))
	p.Remarks.Move(fyne.NewPos(size.Width-(half+theme.Padding())*p.share+theme.Padding(), 0))
	p.Remarks.Resize(fyne.NewSize(half, size.Height))
}

func (p *CommitTextPane) Scrolled(*fyne.ScrollEvent)          {}
func (p *CommitTextPane) CanScrollBy(_, _ float32) bool       { return false }
func (p *CommitTextPane) CreateRenderer() fyne.WidgetRenderer { return &commitTextRenderer{p} }

type commitTextRenderer struct{ p *CommitTextPane }

func (r *commitTextRenderer) Layout(size fyne.Size) { r.p.layout(size) }
func (r *commitTextRenderer) MinSize() fyne.Size {
	m := r.p.Description.MinSize()
	if r.p.Remarks.Visible() {
		n := r.p.Remarks.MinSize()
		m.Width = max(m.Width, n.Width)*2 + theme.Padding()
		m.Height = max(m.Height, n.Height)
	}
	return m
}
func (r *commitTextRenderer) Refresh() {
	r.Layout(r.p.Size())
	r.p.Description.Refresh()
	r.p.Remarks.Refresh()
}
func (r *commitTextRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.p.Description, r.p.Remarks}
}
func (r *commitTextRenderer) Destroy() {
	if r.p.move != nil {
		r.p.move.Stop()
	}
}
