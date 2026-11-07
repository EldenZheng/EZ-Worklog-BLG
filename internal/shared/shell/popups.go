package shell

import (
	_ "embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
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
	// The floor is for comfort; the window's own edge comes first.
	if ui.Win != nil && ui.Win.Canvas() != nil {
		if sz := ui.Win.Canvas().Size(); sz.Width > 0 && sz.Height > 0 {
			w = min(w, sz.Width*0.96)
			h = min(h, sz.Height*0.96)
		}
	}
	return fyne.NewSize(w, h)
}

// PopupSizeFor is PopupSize grown to what content needs, as far as the window
// allows. Content inside FitPopup hides its needs from the dialog, which would
// otherwise scroll it sideways with room to spare.
func (ui *UI) PopupSizeFor(content fyne.CanvasObject) fyne.Size {
	size := ui.PopupSize()
	if ui.Win == nil || ui.Win.Canvas() == nil {
		return size
	}
	const dialogEdges = 40
	window, need := ui.Win.Canvas().Size(), content.MinSize()
	if need.Width+dialogEdges > size.Width {
		size.Width = need.Width + dialogEdges
		if window.Width > 0 {
			size.Width = min(size.Width, window.Width*0.96)
		}
	}
	if need.Height+dialogEdges > size.Height {
		size.Height = need.Height + dialogEdges
		if window.Height > 0 {
			size.Height = min(size.Height, window.Height*0.96)
		}
	}
	return size
}

// PopupTitle cuts a popup's heading to fit the window: a dialog is at least as
// wide as its title, so a long issue title could push it off a small screen.
func (ui *UI) PopupTitle(title string) string {
	if ui.Win == nil || ui.Win.Canvas() == nil || ui.Win.Canvas().Size().Width <= 0 {
		return title
	}
	return FitText(title, theme.TextHeadingSize(), fyne.TextStyle{Bold: true}, ui.Win.Canvas().Size().Width*0.8)
}

// FitText is the longest start of text that fits width, ellipsis included.
func FitText(text string, size float32, style fyne.TextStyle, width float32) string {
	measure := func(s string) float32 { return fyne.MeasureText(s, size, style).Width }
	if measure(text) <= width {
		return text
	}
	runes := []rune(text)
	low, high := 0, len(runes)
	for low < high {
		mid := (low + high + 1) / 2
		if measure(string(runes[:mid])+"…") <= width {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return string(runes[:low]) + "…"
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
