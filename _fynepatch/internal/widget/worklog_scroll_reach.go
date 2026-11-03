package widget

// Worklog patch, copied into the private Fyne build by build.ps1. The go tool
// skips folders that start with "_", so this file only builds there.

// CanScrollBy reports whether a wheel delta would move this scroller's content
// along the delta's own axes. The window uses it to send the wheel past a
// scroller that is already at its end, or that only scrolls the other way
// (such as a text field), to the scroller around it.
func (s *Scroll) CanScrollBy(dx, dy float32) bool {
	if s.Direction == ScrollNone || s.Content == nil {
		return false
	}
	size := s.Size()
	content := s.Content.MinSize()
	moves := func(offset, delta, outer, inner float32) bool {
		return delta != 0 && computeOffset(offset, -delta, outer, inner) != offset
	}
	return s.Direction != ScrollVerticalOnly && moves(s.Offset.X, dx, size.Width, content.Width) ||
		s.Direction != ScrollHorizontalOnly && moves(s.Offset.Y, dy, size.Height, content.Height)
}
