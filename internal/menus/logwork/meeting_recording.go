package logwork

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// The reminder belongs on the saved bubble; the optional input sits below
// the remarks in both the new-entry form and the saved-entry popup.
func meetingRecordingField(entry *widget.Entry) fyne.CanvasObject {
	caption := widget.NewLabelWithStyle("Meeting recording link (optional)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return container.NewVBox(caption, entry)
}
