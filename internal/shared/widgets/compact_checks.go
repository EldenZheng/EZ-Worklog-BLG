package widgets

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Native CheckGroup gives every horizontal item the longest label's width.
// Keep native checks but pack them at their own widths in the column footer.
type CompactCheckGroup struct{ widget.CheckGroup }

func NewCompactCheckGroup(options []string, changed func([]string)) *CompactCheckGroup {
	g := &CompactCheckGroup{}
	g.Options = options
	g.Horizontal = true
	g.OnChanged = changed
	g.ExtendBaseWidget(g)
	return g
}

func (g *CompactCheckGroup) CreateRenderer() fyne.WidgetRenderer {
	return &compactChecksRenderer{WidgetRenderer: g.CheckGroup.CreateRenderer(), group: g}
}

type compactChecksRenderer struct {
	fyne.WidgetRenderer
	group *CompactCheckGroup
}

func (r *compactChecksRenderer) MinSize() fyne.Size {
	s := fyne.Size{}
	objects := r.Objects()
	for _, o := range objects {
		m := o.MinSize()
		s.Width += m.Width
		s.Height = fyne.Max(s.Height, m.Height)
	}
	if len(objects) > 1 {
		s.Width += float32(len(objects)-1) * theme.Padding()
	}
	return s
}

func (r *compactChecksRenderer) Layout(size fyne.Size) {
	x := float32(0)
	for _, o := range r.Objects() {
		m := o.MinSize()
		o.Move(fyne.NewPos(x, (size.Height-m.Height)/2))
		o.Resize(m)
		x += m.Width + theme.Padding()
	}
}

func (r *compactChecksRenderer) Refresh() { r.WidgetRenderer.Refresh(); r.Layout(r.group.Size()) }
