package logwork

import (
	"errors"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

var errReviewPushCancelled = errors.New("code review was not combined; the entry remains saved locally")

// All manual push entry points use this gate, including saved rows and retries.
// The confirmation is on the UI thread; GitHub reads and writes stay off it.
func pushSavedEntry(ui *shell.UI, r backend.Row, done func(backend.Row, backend.PushResult, error)) {
	progress, stop := ui.PushSpinner("Checking GitHub before pushing...")
	var res backend.PushResult
	var plan *backend.DailyReviewPush
	var pushErr error
	ui.Async(func() error {
		r, pushErr = resolveForPush(ui, r)
		if pushErr != nil {
			return nil
		}
		if backend.RowIsBulkCodeReview(r) {
			plan, pushErr = backend.PrepareDailyReviewPush(r)
			return nil
		}
		// Merged commits are linked from the Worklog rather than rewritten.
		r = ui.Store.CheckRowMerged(r)
		body := strings.TrimSpace(backend.OrDefault(r["remarks"], r["description"]))
		if r["type"] == backend.KindMeeting && (r["meeting_title"] != "" || r["meeting_recording"] != "") {
			res, pushErr = backend.PushMeetingEntry(ui.Cfg, r, progress)
			return nil
		}
		res, pushErr = backend.PushEntry(ui.Cfg, r["issue"], r["date"], r["owner"],
			r.Minutes(), body, backend.WorklogIssueBody(r, body), backend.RowPushMode(r), r["issue_url"],
			backend.SubTitleBaseFor(r), progress)
		return nil
	}, func() {
		stop()
		if pushErr != nil || plan == nil {
			done(r, res, pushErr)
			return
		}
		push := func() {
			pushProgress, finish := ui.PushSpinner("Pushing code reviews to GitHub...")
			ui.Async(func() error {
				res, pushErr = backend.PushDailyReview(ui.Cfg, plan, pushProgress)
				return nil
			}, func() {
				finish()
				done(r, res, pushErr)
			})
		}
		if !plan.NeedsCombine {
			push()
			return
		}
		var popup *widget.PopUp
		cancel := widget.NewButton("Cancel", func() {
			shell.ClosePopUp(popup)
			done(r, backend.PushResult{}, errReviewPushCancelled)
		})
		combine := widget.NewButton("Combine & push", func() {
			shell.ClosePopUp(popup)
			push()
		})
		prs := "PR"
		if plan.OtherCount != 1 {
			prs = "PRs"
		}
		message := widget.NewLabel(fmt.Sprintf("GitHub already lists %d other %s for %s that this entry does not (%d minutes).\nCombine them with this entry?\n\nCombined total: %d minutes\n\nThe description and worklog remarks will contain the combined PR list.\nPRs in both lists count once.", plan.OtherCount, prs, r["date"], plan.OtherMinutes, plan.Minutes))
		message.Wrapping = fyne.TextWrapWord
		content := container.NewVBox(shell.IssueHyperlink(r["issue"]), message)
		popup = ui.ShowConfirmation("Combine code reviews?", content, shell.DialogButtons(cancel, shell.GitHubButton(combine)))
	})
}
