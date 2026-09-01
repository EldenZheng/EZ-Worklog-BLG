package main

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// showIf is Show/Hide as one call, so a form that reveals one row per mode
// reads as a list of conditions rather than a stack of if/else.
func showIf(o fyne.CanvasObject, show bool) {
	if show {
		o.Show()
		return
	}
	o.Hide()
}

// repoPicker is the org-then-repo pair used to choose where a new parent issue
// goes.
//
// Two lists rather than one: an org has hundreds of repos, and a single
// flattened dropdown of every repo across every org is a list nobody can find
// anything in. Picking the org first cuts it to the handful that could be meant.
//
// The repo list is fetched when an org is chosen, not at startup — the picker is
// on a pane most sessions never open, and a hundred repos is a request nobody
// asked for. Each org's answer is kept for the session, so switching back and
// forth costs nothing.
type repoPicker struct {
	ui    *UI
	org   *widget.Select
	repo  *widget.Select
	cache map[string][]string
}

func (ui *UI) newRepoPicker() *repoPicker {
	p := &repoPicker{ui: ui, cache: map[string][]string{}}

	p.repo = widget.NewSelect(nil, nil)
	p.repo.PlaceHolder = "Pick an org first"
	p.repo.Disable()

	orgs := orgOrder(ui.cfg)
	p.org = widget.NewSelect(orgs, func(org string) { p.loadRepos(org) })
	p.org.PlaceHolder = "Organisation"
	if len(orgs) == 1 {
		// One configured org is not a choice, so it is made for you.
		p.org.SetSelected(orgs[0])
	}
	if len(orgs) == 0 {
		p.org.PlaceHolder = "Add an org under Repos in Settings"
		p.org.Disable()
	}
	return p
}

func (p *repoPicker) widget() fyne.CanvasObject {
	return container.New(newRatioRow(0.4, 0.6), p.org, p.repo)
}

// value is the chosen owner/repo, empty until both halves are set.
func (p *repoPicker) value() string {
	if p.repo.Selected == "" || p.repo.Disabled() {
		return ""
	}
	return p.repo.Selected
}

func (p *repoPicker) loadRepos(org string) {
	org = strings.TrimSpace(org)
	if org == "" {
		return
	}
	if repos, ok := p.cache[org]; ok {
		p.setRepos(repos)
		return
	}
	p.repo.Selected = ""
	p.repo.PlaceHolder = "Loading " + org + "…"
	p.repo.Disable()
	p.repo.Refresh()

	var repos []string
	var err error
	p.ui.async(func() error {
		repos, err = listOrgRepos(org)
		return nil // reported inline; a failed list must not take the pane down
	}, func() {
		if err != nil {
			p.repo.PlaceHolder = "Could not list " + org + "'s repos"
			p.repo.Refresh()
			p.ui.errf(err)
			return
		}
		p.cache[org] = repos
		p.setRepos(repos)
	})
}

func (p *repoPicker) setRepos(repos []string) {
	p.repo.Options = repos
	if len(repos) == 0 {
		p.repo.PlaceHolder = "No repos found"
		p.repo.Disable()
	} else {
		p.repo.PlaceHolder = "Repository"
		p.repo.Enable()
	}
	p.repo.Refresh()
}
