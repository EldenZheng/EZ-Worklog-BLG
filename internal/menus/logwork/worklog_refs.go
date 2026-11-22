package logwork

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// rowNeedsRefRun reports whether pushing r should go on to give its commits
// their Worklog's reference.
func rowNeedsRefRun(r backend.Row) bool {
	return backend.SOPApplies(r["date"]) && strings.TrimSpace(r["refs"]) != "" && !backend.RowIsLegacy(r) &&
		len(backend.UnmergedRefs(r)) > 0
}

// checkDraftsMerged looks up, off the UI thread, whether saved entries'
// commits have been merged, and redraws them if any now are.
func checkDraftsMerged(ui *shell.UI) {
	changed := false
	ui.Async(func() error {
		changed, _ = ui.Store.RefreshMergedRefs()
		return nil
	}, func() {
		if changed {
			DrawRecent(ui)
			DrawWeekStrip(ui)
		}
	})
}

// addWorklogRefs gives every pushed SOP entry's commits its Worklog's
// reference. It first finishes any rewrite left part-done, then plans the rest
// branch by branch and asks before each force-push. Entries that cannot be
// done are marked incomplete, with why, and keep a retry on their tile.
func addWorklogRefs(ui *shell.UI, refresh func()) {
	var ready, finished []backend.RewriteBatch
	var loadErr error
	ui.Async(func() error {
		if journal, err := ui.Store.RewriteJournal(); err != nil {
			loadErr = err
			return nil
		} else {
			for _, b := range journal {
				if _, err := ui.Store.ResumeWorklogRefs(b); err != nil {
					_ = ui.Store.MarkRefsIncomplete(b, err.Error())
				}
			}
		}
		rows, err := ui.Store.ReadRows()
		if err != nil {
			loadErr = err
			return nil
		}
		for _, b := range backend.PlanWorklogRefs(ui.Cfg, rows) {
			switch {
			case b.Done:
				_ = ui.Store.MarkRefsComplete(b)
				finished = append(finished, b)
			case b.Problem != "":
				_ = ui.Store.MarkRefsIncomplete(b, b.Problem)
			default:
				ready = append(ready, b)
			}
		}
		return nil
	}, func() {
		if loadErr != nil {
			ui.Errf(loadErr)
		}
		refresh()
		confirmRewrites(ui, ready, refresh, finished)
	})
}

// confirmRewrites asks about one branch at a time: which commits gain which
// reference, and that the branch is force-pushed. finished gathers the
// branches whose commits now name their Worklogs; once every branch is asked
// about, those are offered a pull request.
func confirmRewrites(ui *shell.UI, batches []backend.RewriteBatch, refresh func(), finished []backend.RewriteBatch) {
	if len(batches) == 0 {
		offerPullRequests(ui, finished)
		return
	}
	b, rest := batches[0], batches[1:]
	next := func() { confirmRewrites(ui, rest, refresh, finished) }
	var lines []string
	for _, c := range b.Commits {
		line := backend.Short(c.Old) + "  " + shell.Truncate(c.Subject, 70)
		if len(c.Refs) > 0 {
			line += "\n          + Refs " + strings.Join(c.Refs, ", Refs ")
		}
		lines = append(lines, line)
	}
	text := fmt.Sprintf(
		"These commits on %s, branch %s, get their Worklog's reference. The branch is rewritten from the first of them and force-pushed; your files and uncommitted changes are not touched.",
		b.Repo, b.Branch)
	runLabel := "Rewrite & force-push"
	if b.Unpushed() {
		// Nothing on GitHub changes: the commits go up with their references.
		text = fmt.Sprintf(
			"These commits on %s, branch %s, are only on this machine. They get their Worklog's reference, then %s is pushed up to the last of them: nothing on GitHub is rewritten, and your files and uncommitted changes are not touched.",
			b.Repo, b.Branch, b.Branch)
		runLabel = "Add refs & push"
	}
	intro := widget.NewLabel(text)
	intro.Wrapping = fyne.TextWrapWord
	list := widget.NewLabel(strings.Join(lines, "\n"))
	list.TextStyle.Monospace = true
	list.Wrapping = fyne.TextWrapWord
	var popup *widget.PopUp
	later := widget.NewButton("Not now", func() {
		shell.ClosePopUp(popup)
		_ = ui.Store.MarkRefsIncomplete(b, "Not added yet.")
		refresh()
		next()
	})
	run := widget.NewButton(runLabel, func() {
		shell.ClosePopUp(popup)
		progress, stop := ui.PushSpinner("Adding Worklog references to " + b.Branch + "…")
		_ = progress
		var done backend.RewriteBatch
		var err error
		ui.Async(func() error {
			done, err = ui.Store.RunWorklogRefs(b)
			if err != nil {
				_ = ui.Store.MarkRefsIncomplete(b, err.Error())
			}
			return nil
		}, func() {
			stop()
			refresh()
			ui.RefreshLoggedShas()
			switch {
			case err != nil:
				shell.ShowInformation("References not added", err.Error()+"\n\nThe entry keeps an Add refs button to try again.", ui.Win)
			case done.Stage == "local-sync":
				shell.ShowInformation("References added on GitHub",
					"GitHub has the new commits. Your local "+b.Branch+" moved meanwhile, so it was left as it is; Add refs on the entry moves it across, or pull it with --rebase.", ui.Win)
			}
			if err == nil && done.Stage != "local-sync" {
				finished = append(finished, done)
			}
			next()
		})
	})
	popup = ui.ShowConfirmation("Add Worklog references", container.NewVBox(intro, list),
		shell.DialogButtons(later, shell.GitHubButton(run)))
}

// refStateLine says where an entry's commit references stand, or "".
func refStateLine(r backend.Row) string {
	switch r["ref_state"] {
	case backend.RefIncomplete:
		return "Worklog reference not on the commits yet: " + backend.OrDefault(r["ref_note"], "try Add refs.")
	case backend.RefLocalSync:
		return "Reference on GitHub; local branch not synced: " + r["ref_note"]
	case backend.RefComplete:
		return "Commits reference the Worklog."
	}
	if backend.NeedsWorklogRefs(r) {
		return "Worklog reference not on the commits yet."
	}
	return ""
}

// addRefsButton retries the references for an entry still missing them.
func addRefsButton(ui *shell.UI, r backend.Row, refresh func()) *shell.HoverButton {
	if backend.RefsLocked(r) || (!backend.NeedsWorklogRefs(r) && r["ref_state"] != backend.RefLocalSync) {
		return nil
	}
	return shell.NewHoverButton("Add refs", nil, func() { addWorklogRefs(ui, refresh) })
}
