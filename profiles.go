package main

// Profiles are the per-person half of the settings.
//
// The Report and Status tabs answer "what did one person log, and what is that
// worth". Everything they need that differs between people is two values — the
// GitHub login to filter the boards by, and the salary the day rate comes from.
// Everything else (the project boards, the repos to scan, the currency, the
// exchange rate) is the same whoever is being looked at.
//
// Before this, changing who the app reported on meant editing those two fields
// in Settings and remembering what they used to be. A profile holds the pair
// under a name, and the picker above the tabs swaps them in one click.
//
// The list lives in its own profiles.json rather than in config.json. This is a
// fork of an upstream repo that still owns Config and store.go, so keeping the
// new state in a new file leaves nothing for a merge to collide with.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// settingsTabIndex is where Settings sits once "Log work" is off the bar. It
// sits here rather than next to statusTabIndex so the tab constants this fork
// changes are in one place.
const settingsTabIndex = 2

// Profile is one person's slice of the settings.
type Profile struct {
	Label      string  `json:"label"`
	Login      string  `json:"login"`
	BaseSalary float64 `json:"base_salary_per_21_days"`
}

// name is what the picker shows: the label when there is one, otherwise the
// login, so a half-filled profile is still identifiable.
func (p Profile) name() string {
	if s := strings.TrimSpace(p.Label); s != "" {
		return s
	}
	if s := strings.TrimSpace(p.Login); s != "" {
		return s
	}
	return "(unnamed)"
}

// apply lays a profile over the shared config. The resolved values go into the
// same Config fields the rest of the app already reads, so nothing downstream
// of here — the fetch filter, the day rate, the report header — needs to know
// profiles exist.
func (p Profile) apply(cfg Config) Config {
	cfg.WorklogOwner = p.Login
	cfg.GithubAuthor = p.Login
	cfg.BaseSalary = p.BaseSalary
	return cfg
}

// profileFile is profiles.json.
type profileFile struct {
	Active   string    `json:"active"` // login of the profile on screen
	Profiles []Profile `json:"profiles"`
}

func (s *Store) profilesPath() string { return filepath.Join(s.dir, "profiles.json") }

func (s *Store) LoadProfiles() profileFile {
	var pf profileFile
	if s == nil {
		return pf
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.profilesPath())
	if err != nil {
		return pf
	}
	_ = json.Unmarshal(data, &pf)
	return pf
}

func (s *Store) SaveProfiles(pf profileFile) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.profilesPath(), data, 0o644)
}

// ---- manager ----

// profileMgr owns the list, the picker above the tabs, and the editor in
// Settings.
type profileMgr struct {
	ui   *UI
	file profileFile

	sel            *widget.Select
	rows           *fyne.Container
	members        []string // org logins offered in the login dropdown
	membersLoading bool

	// loginBoxes are the login fields on screen, kept so the org member list
	// can fill them in when it lands after the editor was drawn.
	loginBoxes []*widget.SelectEntry
}

// newProfileMgr loads the list, seeds it from the existing single-person config
// on first run, and resolves the active profile into ui.cfg.
func newProfileMgr(ui *UI) *profileMgr {
	m := &profileMgr{ui: ui}
	m.file = ui.store.LoadProfiles()
	m.seed()
	ui.cfg = m.applyActive(ui.cfg)
	return m
}

// seed turns a config that predates profiles into the first profile, so an
// existing install opens on its own name rather than on a blank list.
func (m *profileMgr) seed() {
	if len(m.file.Profiles) > 0 {
		m.normalise()
		return
	}
	cfg := m.ui.cfg
	login := strings.TrimSpace(cfg.WorklogOwner)
	if login == "" {
		login = strings.TrimSpace(cfg.GithubAuthor)
	}
	// Nothing to carry over: a fresh install gets an empty list and the
	// Settings tab, not a nameless profile with somebody's zero salary in it.
	if login == "" && cfg.BaseSalary == 0 {
		return
	}
	m.file.Profiles = []Profile{{
		Label:      login,
		Login:      login,
		BaseSalary: cfg.BaseSalary,
	}}
	m.file.Active = login
	_ = m.ui.store.SaveProfiles(m.file)
}

// normalise makes sure Active names a profile that is actually in the list.
// A profile deleted while it was showing would otherwise leave the picker on a
// name that no longer exists, and the tabs filtered to a login nobody has.
func (m *profileMgr) normalise() {
	if len(m.file.Profiles) == 0 {
		m.file.Active = ""
		return
	}
	for _, p := range m.file.Profiles {
		if p.Login == m.file.Active {
			return
		}
	}
	m.file.Active = m.file.Profiles[0].Login
}

func (m *profileMgr) active() (Profile, bool) {
	for _, p := range m.file.Profiles {
		if p.Login == m.file.Active {
			return p, true
		}
	}
	return Profile{}, false
}

// applyActive resolves the active profile into a config. With no profiles it
// returns the config untouched, so the app behaves exactly as it did before
// this file existed.
func (m *profileMgr) applyActive(cfg Config) Config {
	if m == nil {
		return cfg
	}
	m.normalise()
	if p, ok := m.active(); ok {
		return p.apply(cfg)
	}
	return cfg
}

func (m *profileMgr) names() []string {
	out := make([]string, 0, len(m.file.Profiles))
	for _, p := range m.file.Profiles {
		out = append(out, p.name())
	}
	return out
}

func (m *profileMgr) byName(name string) (Profile, bool) {
	for _, p := range m.file.Profiles {
		if p.name() == name {
			return p, true
		}
	}
	return Profile{}, false
}

func (m *profileMgr) save() { _ = m.ui.store.SaveProfiles(m.file) }

// ---- the picker above the tabs ----

// wrap puts the profile picker above the tab bar, so it is in reach from both
// Status and Report rather than being buried in Settings.
func (m *profileMgr) wrap(tabs fyne.CanvasObject) fyne.CanvasObject {
	if m == nil {
		return tabs
	}
	m.sel = widget.NewSelect(m.names(), func(name string) {
		p, ok := m.byName(name)
		if !ok || p.Login == m.file.Active {
			return
		}
		m.switchTo(p.Login)
	})
	m.sel.PlaceHolder = "No profiles — add one in Settings"
	m.syncPicker()
	bar := container.NewBorder(nil, nil, bold("Profile"), nil, wideSelect(m.sel))
	return container.NewBorder(container.NewPadded(bar), nil, nil, nil, tabs)
}

// syncPicker points the picker at the active profile without the change
// handler treating it as a click.
func (m *profileMgr) syncPicker() {
	if m.sel == nil {
		return
	}
	m.sel.Options = m.names()
	p, ok := m.active()
	if !ok {
		m.sel.Selected = ""
		m.sel.Refresh()
		return
	}
	// Assigning Selected rather than calling SetSelected: SetSelected fires
	// OnChanged, which would re-enter switchTo and re-fetch the month that is
	// already on screen.
	m.sel.Selected = p.name()
	m.sel.Refresh()
}

// switchTo makes another profile the one on screen.
func (m *profileMgr) switchTo(login string) {
	m.file.Active = login
	m.normalise()
	m.save()
	m.ui.cfg = m.applyActive(m.ui.cfg)
	m.syncPicker()

	// The cached months belong to whoever was showing a moment ago. Keeping
	// them would put one person's hours under another person's name, which
	// reads as a real answer — so they go, and the tabs ask again.
	m.ui.projItems = nil
	m.ui.ensureProjectCache()
	m.ui.drawCalendar()
	m.ui.drawReport()
	m.ui.loadStatus(false)
	m.ui.loadReport(false)
}

// ---- the editor in Settings ----

func (m *profileMgr) settingsSection() fyne.CanvasObject {
	m.rows = container.NewVBox()
	m.redrawRows()
	add := widget.NewButton("Add profile", func() {
		m.file.Profiles = append(m.file.Profiles, Profile{})
		m.save()
		m.redrawRows()
		m.syncPicker()
	})
	hint := widget.NewLabel(
		"Who the Status and Report tabs are about. The repos, project boards and " +
			"currency below are shared by every profile.")
	hint.Wrapping = fyne.TextWrapWord
	return container.NewVBox(bold("Profiles"), hint, m.rows, container.NewHBox(add))
}

func (m *profileMgr) redrawRows() {
	if m.rows == nil {
		return
	}
	m.rows.Objects = nil
	m.loginBoxes = nil
	if len(m.file.Profiles) == 0 {
		m.rows.Objects = append(m.rows.Objects,
			widget.NewLabel("No profiles yet — add one to filter the tabs by a person."))
	}
	for i := range m.file.Profiles {
		m.rows.Objects = append(m.rows.Objects, m.row(i))
	}
	m.rows.Refresh()
}

func (m *profileMgr) row(i int) fyne.CanvasObject {
	p := m.file.Profiles[i]

	label := widget.NewEntry()
	label.SetPlaceHolder("Elden")
	label.SetText(p.Label)
	label.OnChanged = func(s string) { m.edit(i, func(p *Profile) { p.Label = s }) }

	// A dropdown that still takes typing: the org list covers everyone normally
	// wanted, but a login it cannot see must not be unenterable.
	login := widget.NewSelectEntry(m.members)
	login.SetPlaceHolder("blg-elden")
	login.SetText(p.Login)
	login.OnChanged = func(s string) {
		s = strings.TrimSpace(s)
		m.edit(i, func(p *Profile) { p.Login = s })
	}
	m.loginBoxes = append(m.loginBoxes, login)

	sal := widget.NewEntry()
	sal.SetPlaceHolder("0")
	if p.BaseSalary != 0 {
		sal.SetText(strconv.FormatFloat(p.BaseSalary, 'f', -1, 64))
	}
	sal.OnChanged = func(s string) {
		v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
		m.edit(i, func(p *Profile) { p.BaseSalary = v })
	}

	del := widget.NewButton("Remove", func() { m.remove(i) })

	return container.NewGridWithColumns(4,
		labeled("Name", label),
		labeled("GitHub login", login),
		labeled("Base salary per 21 days", sal),
		labeled(" ", del))
}

// edit changes one profile in place and keeps the picker and the live config in
// step with it. Renaming or repaying the profile that is showing has to reach
// the tabs, or the Report goes on quoting the old salary until a restart.
func (m *profileMgr) edit(i int, f func(*Profile)) {
	if i < 0 || i >= len(m.file.Profiles) {
		return
	}
	wasActive := m.file.Profiles[i].Login == m.file.Active
	f(&m.file.Profiles[i])
	if wasActive {
		m.file.Active = m.file.Profiles[i].Login
	}
	m.normalise()
	m.save()
	m.syncPicker()
	if wasActive {
		m.ui.cfg = m.applyActive(m.ui.cfg)
	}
}

func (m *profileMgr) remove(i int) {
	if i < 0 || i >= len(m.file.Profiles) {
		return
	}
	gone := m.file.Profiles[i].Login
	m.file.Profiles = append(m.file.Profiles[:i], m.file.Profiles[i+1:]...)
	m.normalise()
	m.save()
	m.redrawRows()
	m.syncPicker()
	// Removing the profile on screen leaves the tabs showing its data under
	// somebody else's name, so the switch is made properly rather than just
	// repointing the picker.
	if gone == m.ui.cfg.WorklogOwner {
		m.switchTo(m.file.Active)
	}
}

// ---- the Settings tab ----

// buildForkSettingsTab replaces upstream's buildSettingsTab, which is left in
// main.go untouched and unused.
//
// It is a whole function rather than a patch to that one for the sake of
// merges: upstream owns its version and can rewrite it freely, and this file
// never has to be reconciled with what it did.
//
// Only two things are editable here. The login and the salary moved into the
// profiles list, and the currency, look-back window, push mode and Anthropic
// key all belong to the writing half of the app, which this fork does not show.
// Whatever config.json holds for those is carried through a save untouched
// rather than blanked, so un-hiding the Log work tab needs no re-configuring.
func (ui *UI) buildForkSettingsTab() fyne.CanvasObject {
	repos := widget.NewEntry()
	repos.SetPlaceHolder("bigledger  (whole org)  or  bigledger/blg-intranet")
	// One board per org, one per line. Status and Report read the union of
	// them, so work logged against a second org's project still counts.
	projURL := widget.NewMultiLineEntry()
	projURL.SetMinRowsVisible(3)
	projURL.SetPlaceHolder("https://github.com/orgs/bigledger/projects/9")
	msg := widget.NewLabel("")

	c := ui.cfg
	repos.SetText(strings.Join(c.Repos, ", "))
	projURL.SetText(strings.Join(projectURLs(c), "\n"))
	if !ui.hasCfg {
		msg.SetText("Add a profile and a project board to get started.")
	}

	save := widget.NewButton("Save settings", func() {
		var repoList []string
		for _, x := range strings.Split(repos.Text, ",") {
			if s := strings.TrimSpace(x); s != "" {
				repoList = append(repoList, s)
			}
		}
		newCfg := ui.cfg
		newCfg.Repos = repoList
		// First line stays the main board so an older build of this app, and
		// anything reading project_url, still finds the one it expects.
		var projList []string
		for _, line := range strings.FieldsFunc(projURL.Text, func(r rune) bool {
			return r == '\n' || r == '\r' || r == ',' || r == ' '
		}) {
			if line = strings.TrimSpace(line); line != "" {
				projList = append(projList, line)
			}
		}
		newCfg.ProjectURL, newCfg.ExtraProjectURLs = "", nil
		if len(projList) > 0 {
			newCfg.ProjectURL, newCfg.ExtraProjectURLs = projList[0], projList[1:]
		}
		// The profile owns the login and the salary; this keeps the saved config
		// agreeing with whichever one is showing.
		newCfg = ui.prof.applyActive(newCfg)
		if err := ui.store.SaveConfig(newCfg); err != nil {
			ui.errf(err)
			return
		}
		ui.cfg = newCfg
		ui.hasCfg = true
		ui.projItems = nil // reload against the saved project and owner
		ui.ensureProjectCache()
		msg.SetText("Saved.")
	})
	save.Importance = widget.HighImportance

	form := container.NewVBox(
		widget.NewLabelWithStyle("Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		ui.prof.settingsSection(),
		widget.NewSeparator(),
		bold("Shared by every profile"),
		labeled("Repos or orgs to scan (comma-separated; a bare org name scans all its repos)", repos),
		labeled("Worklog project URLs — one per line (Status + Report read all of them, filtered to the profile above)", projURL),
		container.NewHBox(save),
		msg,
	)
	return container.NewVScroll(widget.NewCard("", "", form))
}

// ---- org members ----

// loadMembers fills the login dropdown from the orgs already configured, so the
// list is neither typed by hand nor hard-coded into this fork.
//
// Called when the Settings tab is opened rather than at startup: it is a
// network round trip for a dropdown nobody looking at Status or Report can see,
// and it is fetched once per run.
func (m *profileMgr) loadMembers() {
	if m == nil || m.membersLoading || len(m.members) > 0 {
		return
	}
	m.membersLoading = true
	go func() {
		defer func() { recover() }() // a dropdown that will not load must not take the app down
		logins := listOrgMembers(orgsFromRepos(m.ui.cfg))
		fyne.Do(func() {
			m.membersLoading = false
			if len(logins) == 0 {
				return
			}
			m.members = logins
			for _, b := range m.loginBoxes {
				b.SetOptions(logins)
			}
		})
	}()
}

// orgsFromRepos is the owner side of every configured entry: "bigledger" from
// both "bigledger" and "bigledger/blg-intranet".
func orgsFromRepos(cfg Config) []string {
	var out []string
	seen := map[string]bool{}
	for _, e := range cfg.Repos {
		e = strings.TrimSpace(e)
		if i := strings.IndexByte(e, '/'); i >= 0 {
			e = e[:i]
		}
		if e == "" || seen[strings.ToLower(e)] {
			continue
		}
		seen[strings.ToLower(e)] = true
		out = append(out, e)
	}
	return out
}

// listOrgMembers returns every login in the given orgs, sorted and deduplicated.
//
// This reads membership rather than searching GitHub for logins by prefix:
// search/users has no prefix operator, so "blg- in:login" matches the substring
// anywhere and pulls in a hundred unrelated accounts from other orgs. Who is on
// the team is exactly the org's member list.
func listOrgMembers(orgs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, org := range orgs {
		res, err := gh([]string{
			"api", "orgs/" + org + "/members", "--paginate",
			"-X", "GET", "-f", "per_page=100", "--jq", ".[].login",
		})
		if err != nil {
			continue // a personal account has no members, and a missing scope is not fatal here
		}
		for _, line := range strings.Split(strings.TrimSpace(res), "\n") {
			s := strings.TrimSpace(line)
			if s == "" || seen[strings.ToLower(s)] {
				continue
			}
			seen[strings.ToLower(s)] = true
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}
