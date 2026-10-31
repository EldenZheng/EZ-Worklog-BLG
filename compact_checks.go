package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Native CheckGroup gives every horizontal item the longest label's width.
// Keep native checks but pack them at their own widths in the column footer.
type compactCheckGroup struct{ widget.CheckGroup }

func newCompactCheckGroup(options []string, changed func([]string)) *compactCheckGroup {
	g := &compactCheckGroup{}
	g.Options = options
	g.Horizontal = true
	g.OnChanged = changed
	g.ExtendBaseWidget(g)
	return g
}
func (g *compactCheckGroup) CreateRenderer() fyne.WidgetRenderer {
	return &compactChecksRenderer{WidgetRenderer: g.CheckGroup.CreateRenderer(), group: g}
}

type compactChecksRenderer struct {
	fyne.WidgetRenderer
	group *compactCheckGroup
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
