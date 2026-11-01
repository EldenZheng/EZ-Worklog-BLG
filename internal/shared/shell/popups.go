package shell

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

// PopupSize fills as much of the window as a dialog is allowed to take. Fyne
// clamps a dialog to its parent, so this is measured from the live canvas
// instead of hard-coded — resize the window and the next popup follows.
func (ui *UI) PopupSize() fyne.Size {
	const minW, minH = 780, 540
	w, h := float32(minW), float32(minH)
	if ui.Win != nil && ui.Win.Canvas() != nil {
		if sz := ui.Win.Canvas().Size(); sz.Width > 0 && sz.Height > 0 {
			w = sz.Width * 0.80
			h = sz.Height * 0.80
		}
	}
	// A freshly built window can measure smaller than its own content; keep a
	// usable floor so the form is never cramped.
	if w < minW {
		w = minW
	}
	if h < minH {
		h = minH
	}
	return fyne.NewSize(w, h)
}

// compactPopupSize uses the editor popup's responsive width without borrowing
// its tall form height. Confirmations need room for a sentence and two buttons,
// but should still read as a compact decision rather than a second workspace.
func (ui *UI) compactPopupSize(content fyne.CanvasObject) fyne.Size {
	h := content.MinSize().Height
	if h < 180 {
		h = 180
	}
	return fyne.NewSize(ui.PopupSize().Width, h)
}
