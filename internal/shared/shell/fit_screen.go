package shell

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
)

// FitScreen is the window's content: everything inside one scroller, as a
// last line of defence.
//
// Fyne will not let a window be smaller than its content, so a page that needs
// more than the screen has would grow the window past the screen's edge, where
// it cannot be reached. Inside a scroller the window can always be as small as
// the screen, and anything that still does not fit scrolls. With room to spare
// the scroller draws nothing and the page is laid out as if it were not there.
//
// The window's padding moves inside the scroller, so it clips at the window's
// edge and not a padding short of it, where it would shave the corners of
// whatever reaches the page's edge.
//
// Over it lies an empty layer where a closing popup fades away (see
// popup_motion.go): under any popup still open, over the page.
func FitScreen(w fyne.Window, content fyne.CanvasObject) {
	w.SetPadded(false)
	layer := container.NewWithoutLayout()
	closingLayers[w.Canvas()] = layer
	page := container.NewScroll(container.NewPadded(content))
	w.SetContent(container.New(&widthReflow{page: page}, page, layer))
}

// reflowSettle is how long the width must hold before the page is measured
// again, so dragging the window's edge costs one extra pass and not one a frame.
const reflowSettle = 80 * time.Millisecond

// widthReflow measures the page a second time after the window changes width.
//
// A parent asks a wrapped child how tall it is before giving it the new width,
// so the first pass after a resize answers with the height for the old width:
// text moved to a narrower screen spills over what is below it, and text moved
// to a wider one leaves a gap. By the second pass every child has its width. A
// scroller only re-measures content that has outgrown it, so a page that fits
// the window never gets one without this.
type widthReflow struct {
	page  fyne.CanvasObject
	width float32
	timer *time.Timer
}

func (l *widthReflow) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return layout.NewStackLayout().MinSize(objects)
}

func (l *widthReflow) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	layout.NewStackLayout().Layout(objects, size)
	if size.Width == l.width {
		return
	}
	first := l.width == 0
	l.width = size.Width
	if first {
		return
	}
	if l.timer != nil {
		l.timer.Stop()
	}
	l.timer = time.AfterFunc(reflowSettle, func() { fyne.Do(l.page.Refresh) })
}

// FitPopup is the same for a popup's content: a popup bigger than the window
// would hang off it, with no way to reach what is past the edge.
func FitPopup(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewScroll(content)
}
