package main

import (
	"encoding/json"
	"os"
	"testing"

	"fyne.io/fyne/v2/test"
)

// An install that predates profiles opens on its own name rather than on an
// empty list: the person and salary already in config.json become profile one.
func TestProfilesSeedFromAnExistingConfig(t *testing.T) {
	ui := &UI{store: newStore(t.TempDir())}
	ui.cfg = Config{WorklogOwner: "blg-elden", BaseSalary: 2800, Currency: "RM"}

	m := newProfileMgr(ui)

	if len(m.file.Profiles) != 1 {
		t.Fatalf("the existing config should have seeded one profile, got %d", len(m.file.Profiles))
	}
	got := m.file.Profiles[0]
	if got.Login != "blg-elden" || got.BaseSalary != 2800 {
		t.Fatalf("the seeded profile should carry the old settings, got %+v", got)
	}
	if m.file.Active != "blg-elden" {
		t.Fatalf("the seeded profile should be the active one, got %q", m.file.Active)
	}
	// And it is on disk, so the next run does not seed a second copy.
	if _, err := os.Stat(ui.store.profilesPath()); err != nil {
		t.Fatalf("the seeded list should have been saved: %v", err)
	}
}

// A fresh install has nothing to carry over, and must not invent a nameless
// profile on a zero salary — that reads as a real person earning nothing.
func TestAFreshInstallSeedsNoProfile(t *testing.T) {
	ui := &UI{store: newStore(t.TempDir())}
	ui.cfg = defaultConfig()

	m := newProfileMgr(ui)

	if len(m.file.Profiles) != 0 {
		t.Fatalf("nothing to carry over should mean no profiles, got %+v", m.file.Profiles)
	}
	if ui.cfg.WorklogOwner != "" || ui.cfg.BaseSalary != 0 {
		t.Fatalf("an empty list should leave the config untouched, got %+v", ui.cfg)
	}
}

// The active profile is resolved into the same Config fields the fetch filter
// and the day rate already read, so the rest of the app needs no changes.
func TestTheActiveProfileResolvesIntoTheConfig(t *testing.T) {
	dir := t.TempDir()
	writeProfiles(t, dir, profileFile{
		Active: "blg-sabrina",
		Profiles: []Profile{
			{Label: "Elden", Login: "blg-elden", BaseSalary: 2800},
			{Label: "Sabrina", Login: "blg-sabrina", BaseSalary: 3500},
		},
	})
	ui := &UI{store: newStore(dir)}
	ui.cfg = Config{Currency: "RM"}

	newProfileMgr(ui)

	if ui.cfg.WorklogOwner != "blg-sabrina" || ui.cfg.GithubAuthor != "blg-sabrina" {
		t.Fatalf("the active profile should own both login fields, got %+v", ui.cfg)
	}
	if ui.cfg.BaseSalary != 3500 {
		t.Fatalf("the active profile's salary should be the one in play, got %v", ui.cfg.BaseSalary)
	}
	if ui.cfg.Currency != "RM" {
		t.Fatalf("shared settings should survive the profile, got %q", ui.cfg.Currency)
	}
}

// An active login that no longer names a profile — deleted, or renamed by hand
// in the file — falls back to the first rather than filtering the boards to
// somebody who does not exist and reporting a blank month as a real one.
func TestAMissingActiveLoginFallsBackToTheFirstProfile(t *testing.T) {
	dir := t.TempDir()
	writeProfiles(t, dir, profileFile{
		Active:   "blg-gone",
		Profiles: []Profile{{Login: "blg-elden", BaseSalary: 2800}},
	})
	ui := &UI{store: newStore(dir)}

	m := newProfileMgr(ui)

	if m.file.Active != "blg-elden" {
		t.Fatalf("a dangling active login should fall back, got %q", m.file.Active)
	}
	if ui.cfg.WorklogOwner != "blg-elden" {
		t.Fatalf("the fallback should reach the config, got %q", ui.cfg.WorklogOwner)
	}
}

// Switching profiles has to throw the cached months away. Keeping them would
// put one person's hours on screen under another person's name and salary,
// which looks exactly like a real answer.
func TestSwitchingProfileDropsTheOtherPersonsMonths(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")

	dir := t.TempDir()
	writeProfiles(t, dir, profileFile{
		Active: "blg-elden",
		Profiles: []Profile{
			{Label: "Elden", Login: "blg-elden", BaseSalary: 2800},
			{Label: "Sabrina", Login: "blg-sabrina", BaseSalary: 3500},
		},
	})
	ui := &UI{store: newStore(dir), win: w, calMonth: "2026-08", repMonth: "2026-08"}
	ui.cfg = Config{Repos: []string{"bigledger"}, Currency: "RM"} // no project URL: stays offline
	ui.buildAllTabs()

	key := statusCacheKey("2026-08")
	ui.ensureProjectCache()
	ui.projItems[key] = []WorklogItem{{Date: "2026-08-03", Minutes: 480, Owner: "blg-elden"}}
	ui.projLoaded[key] = true

	ui.prof.switchTo("blg-sabrina")

	if ui.cfg.WorklogOwner != "blg-sabrina" || ui.cfg.BaseSalary != 3500 {
		t.Fatalf("the switch should have resolved the new profile, got %+v", ui.cfg)
	}
	if ui.projLoaded[key] {
		t.Fatalf("the previous person's month should not still be marked loaded")
	}
	if len(ui.projItems[key]) != 0 {
		t.Fatalf("the previous person's items should be gone, got %v", ui.projItems[key])
	}
	// And the choice survives a restart.
	if got := newStore(dir).LoadProfiles().Active; got != "blg-sabrina" {
		t.Fatalf("the switch should have been saved, got %q", got)
	}
}

// Removing the profile that is showing switches away from it, rather than
// leaving its data on screen with no name against it.
func TestRemovingTheActiveProfileSwitchesAway(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")

	dir := t.TempDir()
	writeProfiles(t, dir, profileFile{
		Active: "blg-elden",
		Profiles: []Profile{
			{Label: "Elden", Login: "blg-elden", BaseSalary: 2800},
			{Label: "Sabrina", Login: "blg-sabrina", BaseSalary: 3500},
		},
	})
	ui := &UI{store: newStore(dir), win: w, calMonth: "2026-08", repMonth: "2026-08"}
	ui.cfg = Config{Repos: []string{"bigledger"}}
	ui.buildAllTabs()
	ui.prof.settingsSection() // the editor has to be drawn for a row to be removable

	ui.prof.remove(0)

	if len(ui.prof.file.Profiles) != 1 {
		t.Fatalf("one profile should be left, got %+v", ui.prof.file.Profiles)
	}
	if ui.cfg.WorklogOwner != "blg-sabrina" || ui.cfg.BaseSalary != 3500 {
		t.Fatalf("the remaining profile should have taken over, got %+v", ui.cfg)
	}
}

// Editing the salary of the profile on screen has to reach the Report, or it
// goes on quoting the old figure until the app is restarted.
func TestEditingTheActiveProfileReachesTheConfig(t *testing.T) {
	dir := t.TempDir()
	writeProfiles(t, dir, profileFile{
		Active:   "blg-elden",
		Profiles: []Profile{{Label: "Elden", Login: "blg-elden", BaseSalary: 2800}},
	})
	ui := &UI{store: newStore(dir)}
	m := newProfileMgr(ui)

	m.edit(0, func(p *Profile) { p.BaseSalary = 3100 })

	if ui.cfg.BaseSalary != 3100 {
		t.Fatalf("the edited salary should be in play, got %v", ui.cfg.BaseSalary)
	}
	if got := newStore(dir).LoadProfiles().Profiles[0].BaseSalary; got != 3100 {
		t.Fatalf("the edit should have been saved, got %v", got)
	}
}

// Renaming the login of the active profile has to move Active with it,
// otherwise the list points at a login that is no longer in it.
func TestRenamingTheActiveLoginKeepsItActive(t *testing.T) {
	dir := t.TempDir()
	writeProfiles(t, dir, profileFile{
		Active:   "blg-elden",
		Profiles: []Profile{{Label: "Elden", Login: "blg-elden", BaseSalary: 2800}},
	})
	ui := &UI{store: newStore(dir)}
	m := newProfileMgr(ui)

	m.edit(0, func(p *Profile) { p.Login = "blg-elden2" })

	if m.file.Active != "blg-elden2" {
		t.Fatalf("Active should have followed the rename, got %q", m.file.Active)
	}
	if ui.cfg.WorklogOwner != "blg-elden2" {
		t.Fatalf("the rename should reach the config, got %q", ui.cfg.WorklogOwner)
	}
}

// The login dropdown is filled from the orgs already configured, so this fork
// carries no hard-coded org or list of people.
func TestOrgsFromRepos(t *testing.T) {
	got := orgsFromRepos(Config{Repos: []string{
		"bigledger", "bigledger/blg-intranet", " BigLedger ", "", "otherorg/repo",
	}})
	want := []string{"bigledger", "otherorg"}
	if len(got) != len(want) {
		t.Fatalf("owners should be deduplicated case-insensitively, got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// A profile with no label is still pickable: the picker falls back to the
// login rather than showing a blank entry that cannot be told from its
// neighbour.
func TestAnUnlabelledProfileShowsItsLogin(t *testing.T) {
	if got := (Profile{Login: "blg-elden"}).name(); got != "blg-elden" {
		t.Fatalf("got %q", got)
	}
	if got := (Profile{Label: "Elden", Login: "blg-elden"}).name(); got != "Elden" {
		t.Fatalf("got %q", got)
	}
	if got := (Profile{}).name(); got != "(unnamed)" {
		t.Fatalf("got %q", got)
	}
}

func writeProfiles(t *testing.T, dir string, pf profileFile) {
	t.Helper()
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newStore(dir).profilesPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
