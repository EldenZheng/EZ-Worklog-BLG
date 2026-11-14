package logwork

import (
	"errors"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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
		body := strings.TrimSpace(backend.OrDefault(r["remarks"], r["description"]))
		res, pushErr = backend.PushEntry(ui.Cfg, r["issue"], r["date"], r["owner"],
			r.Minutes(), body, r["issue_body"], backend.OrDefault(r["mode"], "issue"), r["issue_url"],
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
		var d dialog.Dialog
		cancel := widget.NewButton("Cancel", func() {
			shell.CloseDialog(d, ui.Win)
			done(r, backend.PushResult{}, errReviewPushCancelled)
		})
		combine := widget.NewButton("Combine & push", func() {
			shell.CloseDialog(d, ui.Win)
			push()
		})
		message := widget.NewLabel(fmt.Sprintf("Code reviews already exist for %s.\nCombine this entry with the reviews on GitHub?\n\nExisting PR lists: %d minutes\nCombined total: %d minutes\n\nThe description and worklog remarks will contain the combined PR list.\nPRs already included with the same minutes count once.", r["date"], plan.ExistingMinutes, plan.Minutes))
		message.Wrapping = fyne.TextWrapWord
		content := container.NewVBox(shell.IssueHyperlink(r["issue"]), message,
			shell.DialogButtons(cancel, shell.GitHubButton(combine)))
		d = dialog.NewCustomWithoutButtons("Combine code reviews?", shell.FitPopup(content), ui.Win)
		d.Resize(ui.PopupSizeFor(content))
		shell.ShowDialog(d, ui.Win)
	})
}
