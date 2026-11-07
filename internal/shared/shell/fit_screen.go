package shell

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
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
func FitScreen(w fyne.Window, content fyne.CanvasObject) {
	w.SetPadded(false)
	w.SetContent(container.NewScroll(container.NewPadded(content)))
}

// FitPopup is the same for a popup's content: a popup bigger than the window
// would hang off it, with no way to reach what is past the edge.
func FitPopup(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewScroll(content)
}
