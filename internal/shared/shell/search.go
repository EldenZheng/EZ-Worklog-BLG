package shell

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// NewSearchEntry is a search field with an X at its right edge that empties
// it. The X only shows while there is something to clear, and clearing runs
// onChanged like any other edit, so the list behind it redraws in full.
// text is the starting search; setting it does not call onChanged.
func NewSearchEntry(placeholder, text string, onChanged func(string)) *widget.Entry {
	search := widget.NewEntry()
	makeSearchEntry(search, placeholder, text, onChanged)
	return search
}

// makeSearchEntry gives an entry the search field's X, for a type built on
// widget.Entry as well as for a plain one.
func makeSearchEntry(search *widget.Entry, placeholder, text string, onChanged func(string)) {
	clearBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		search.SetText("")
		// Back in the field, ready for the next search.
		if c := fyne.CurrentApp().Driver().CanvasForObject(search); c != nil {
			c.Focus(search)
		}
	})
	// The X has to be in place before anything else touches the entry.
	// SetPlaceHolder and SetText refresh it, which builds its renderer, and the
	// renderer only draws an action item that was there when it was built: one
	// set afterwards is laid out but never painted.
	search.ActionItem = QuietButton(clearBtn)
	search.SetPlaceHolder(placeholder)
	search.SetText(text)
	showClear := func(show bool) {
		ShowIf(search.ActionItem, show)
		// Showing a container does not repaint it; refresh so the X appears
		// with the first character rather than at the next unrelated redraw.
		search.ActionItem.Refresh()
	}
	showClear(text != "")
	search.OnChanged = func(value string) {
		showClear(value != "")
		if onChanged != nil {
			onChanged(value)
		}
	}
}
