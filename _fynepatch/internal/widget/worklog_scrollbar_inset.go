package widget

// WorklogSetVerticalBarInset moves the native scrollbar down while the
// scrolling content still extends behind a floating header.
func (s *Scroll) WorklogSetVerticalBarInset(top float32) {
	top = max(0, top)
	if s.worklogScrollBarTop == top {
		return
	}
	s.worklogScrollBarTop = top
	s.Refresh()
}
