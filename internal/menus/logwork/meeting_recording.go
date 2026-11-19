package logwork

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func meetingRecordingField(entry *widget.Entry, text *commitText) fyne.CanvasObject {
	badge := shell.MissingMeetingRecordingTag()
	caption := widget.NewLabelWithStyle("Meeting recording link (optional)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	head := container.New(&widgets.WrapRowLayout{}, caption, badge)
	update := func() {
		remarks, body := text.Values()
		shell.ShowIf(badge, backend.MissingMeetingRecording(backend.Row{"type": backend.KindMeeting, "meeting_recording": entry.Text, "issue_body": body, "remarks": remarks}))
		head.Refresh()
	}
	for _, field := range []*widget.Entry{entry, text.desc, text.Remarks} {
		previous := field.OnChanged
		field.OnChanged = func(text string) {
			if previous != nil {
				previous(text)
			}
			update()
		}
	}
	previous := text.Separate.OnChanged
	text.Separate.OnChanged = func(on bool) {
		if previous != nil {
			previous(on)
		}
		update()
	}
	update()
	return container.NewVBox(head, entry)
}
