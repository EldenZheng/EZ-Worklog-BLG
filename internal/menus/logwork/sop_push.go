package logwork

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// PushDay is the day entries may be pushed on: today. Tests that are not
// about the rule stand in a day of their own.
var PushDay = shell.Today

// WatchDay refreshes the saved cards and open forms across midnight. The
// minute check also catches a sleeping laptop or a change to its clock.
func WatchDay(ui *shell.UI, previous string, closed <-chan struct{}) {
	for {
		now := time.Now().In(backend.MalaysiaTime)
		if today := backend.WorkDate(now); today != previous {
			was := previous
			previous = today
			fyne.Do(func() {
				if ui.WeekStart == backend.WeekStartOf(was) {
					ui.WeekStart = backend.WeekStartOf(today)
				}
				for _, refresh := range ui.LogDayListeners {
					refresh()
				}
				DrawRecent(ui)
			})
		}
		midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		timer := time.NewTimer(min(time.Minute, midnight.Sub(now)))
		select {
		case <-closed:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// showPushOnItsDay keeps an editor's Save & push to the day its entry is for.
// Under the worklog SOP a Worklog's day is the day it is created, so pushing on
// any other day would log the work on the wrong one. sent is an entry already
// pushed, wholly or in part: pushing it again creates nothing and is allowed
// any day. Call it after anything else that sets the picker's OnChanged.
func showPushOnItsDay(push fyne.CanvasObject, date *WorklogDatePicker, sent bool) {
	update := func(iso string) {
		if sent || PushDay == nil || iso == PushDay() {
			push.Show()
		} else {
			push.Hide()
		}
	}
	prev := date.OnChanged
	date.OnChanged = func(iso string) {
		if prev != nil {
			prev(iso)
		}
		update(iso)
	}
	update(date.ISO())
}

// rowSent reports whether r has been pushed, wholly or in part.
func rowSent(r backend.Row) bool { return r["pushed_at"] != "" || r["issue_url"] != "" }

// copyRefButton copies the reference a commit uses to name r's Worklog, as
// the SOP has commits do: push the Worklog, copy its ref, then commit. It is
// nil for an entry with no Worklog to name.
func copyRefButton(r backend.Row) *shell.HoverButton {
	ref := backend.WorklogRef(r["issue_url"])
	if ref == "" || !backend.SOPApplies(r["date"]) || backend.RowIsLegacy(r) {
		return nil
	}
	var b *shell.HoverButton
	b = shell.NewHoverButton("", theme.ContentCopyIcon(), func() {
		fyne.CurrentApp().Clipboard().SetContent(ref)
		b.SetIcon(theme.ConfirmIcon())
	})
	b.Importance = widget.LowImportance
	return b
}
