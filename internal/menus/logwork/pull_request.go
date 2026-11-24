package logwork

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// offerPullRequests asks, one branch at a time, about a pull request for each
// branch whose commits now name their Worklogs. A branch with commits of yours
// still unlogged, or with nothing to offer, is passed over without a popup.
func offerPullRequests(ui *shell.UI, batches []backend.RewriteBatch) {
	var plans []backend.PullRequestPlan
	ui.Async(func() error {
		plans = planPullRequests(ui, batches)
		return nil
	}, func() { offerPlans(ui, plans) })
}

// planPullRequests works out, off the UI thread, the pull request each
// branch could have, once per branch. A branch with commits of yours still
// unlogged, or that cannot be planned, has none.
func planPullRequests(ui *shell.UI, batches []backend.RewriteBatch) []backend.PullRequestPlan {
	seen := map[string]bool{}
	var plans []backend.PullRequestPlan
	for _, b := range batches {
		k := strings.ToLower(b.Repo + "\n" + b.Branch)
		if b.Dir == "" || b.Branch == "" || seen[k] {
			continue
		}
		seen[k] = true
		issue, err := batchIssue(ui, b)
		if err != nil {
			continue
		}
		logged, err := ui.Store.LoggedTest()
		if err != nil {
			continue
		}
		plan, err := backend.PlanPullRequest(b.Dir, b.Repo, b.Branch, issue, logged)
		if err != nil || plan.Unlogged > 0 {
			continue
		}
		plans = append(plans, plan)
	}
	return plans
}

// offerPlans asks about each planned pull request in turn.
func offerPlans(ui *shell.UI, plans []backend.PullRequestPlan) {
	if len(plans) == 0 {
		return
	}
	showPullRequestOffer(ui, plans[0], func() { offerPlans(ui, plans[1:]) })
}

// newPullRequests are the plans with no pull request yet.
func newPullRequests(plans []backend.PullRequestPlan) []backend.PullRequestPlan {
	var out []backend.PullRequestPlan
	for _, p := range plans {
		if p.Existing == "" {
			out = append(out, p)
		}
	}
	return out
}

// batchIssue is the issue a batch's entries are filed under: the pull
// request's title and the ref its description opens with.
func batchIssue(ui *shell.UI, b backend.RewriteBatch) (string, error) {
	rows, err := ui.Store.ReadRows()
	if err != nil {
		return "", err
	}
	want := map[string]bool{}
	for _, id := range b.RowIDs {
		want[id] = true
	}
	for _, r := range rows {
		if want[r["id"]] && strings.TrimSpace(r["issue"]) != "" {
			return strings.TrimSpace(r["issue"]), nil
		}
	}
	return "", errors.New("no issue to name the pull request after")
}

func showPullRequestOffer(ui *shell.UI, p backend.PullRequestPlan, next func()) {
	var popup *widget.PopUp
	ok := widget.NewButton("OK", func() {
		shell.ClosePopUp(popup)
		next()
	})
	if p.Existing != "" {
		msg := wrapped(fmt.Sprintf("Every commit of yours on %s is logged. It already has a pull request:", p.Branch))
		popup = ui.ShowConfirmation("Pull request", container.NewVBox(msg, linkWithCopy(p.Existing)),
			shell.DialogButtons(ok))
		return
	}
	msg := wrapped(fmt.Sprintf("Every commit of yours on %s is logged. Open a pull request into %s on %s?",
		p.Branch, p.Base, p.Repo))
	title := widget.NewLabelWithStyle(p.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	body := wrapped(p.Body)
	// Opening the page by hand leaves this popup up, to come back to.
	var page fyne.CanvasObject = widget.NewLabel("")
	if u, err := url.Parse(p.CompareURL()); err == nil {
		page = widget.NewHyperlink("Open PR page", u)
	}
	create := widget.NewButton("Create PR", func() {
		shell.ClosePopUp(popup)
		createPullRequest(ui, p, next)
	})
	popup = ui.ShowConfirmation("Pull request", container.NewVBox(msg, title, body, page),
		shell.DialogButtons(ok, shell.GitHubButton(create)))
}

func createPullRequest(ui *shell.UI, p backend.PullRequestPlan, next func()) {
	_, stop := ui.PushSpinner("Creating the pull request…")
	var made string
	var err error
	ui.Async(func() error {
		made, err = backend.CreatePullRequest(p)
		return nil
	}, func() {
		stop()
		var popup *widget.PopUp
		ok := widget.NewButton("OK", func() {
			shell.ClosePopUp(popup)
			next()
		})
		if err != nil {
			popup = ui.ShowConfirmation("Pull request not created",
				wrapped(err.Error()+"\n\nNothing was opened on GitHub."), shell.DialogButtons(ok))
			return
		}
		popup = ui.ShowConfirmation("Pull request created", linkWithCopy(made), shell.DialogButtons(ok))
	})
}

// linkWithCopy is a link and, beside it, a button copying it.
func linkWithCopy(link string) fyne.CanvasObject {
	var shown fyne.CanvasObject = widget.NewLabel(link)
	if u, err := url.Parse(link); err == nil {
		shown = widget.NewHyperlink(link, u)
	}
	var copyBtn *shell.HoverButton
	copyBtn = shell.NewHoverButton("Copy", theme.ContentCopyIcon(), func() {
		fyne.CurrentApp().Clipboard().SetContent(link)
		copyBtn.SetText("Copied")
		copyBtn.SetIcon(theme.ConfirmIcon())
	})
	copyBtn.Importance = widget.LowImportance
	return container.NewHBox(shown, copyBtn)
}

func wrapped(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapWord
	return l
}
