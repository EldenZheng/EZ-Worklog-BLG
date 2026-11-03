package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/settings"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// A release is offered only when it is newer than the running build. A build
// with no version in it is older than any release; a build a few commits past
// a release is not offered that release again.
func TestNewerRelease(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		want            bool
	}{
		{"v1.4.0", "v1.3.9", true},
		{"v1.10.0", "v1.9.0", true}, // compared as numbers, not text
		{"v2.0.0", "v1.99.99", true},
		{"v1.3.0", "v1.3.0", false},
		{"v1.3.0", "v1.4.0", false},
		{"v1.3.0", "v1.3.0-4-gabc1234-dirty", false},
		{"v1.3.1", "v1.3.0-4-gabc1234", true},
		{"v1.0.0", "dev", true},
		{"v1.0.0", "abc1234-dirty", true},
		{"nightly", "v1.0.0", false},
	} {
		if got := backend.NewerRelease(c.latest, c.current); got != c.want {
			t.Errorf("NewerRelease(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
	for tag, want := range map[string]bool{"v1.2.3": true, "1.2.3": false, "v1.2": false, "v1.2.3-rc1": false} {
		if backend.ValidReleaseTag(tag) != want {
			t.Errorf("ValidReleaseTag(%q) != %v", tag, want)
		}
	}
}

// Windows will not overwrite a running exe, but it will rename one: the
// running copy steps aside, the download takes its name, and the next start
// clears the old one away.
func TestInstallUpdateSwapsTheBinary(t *testing.T) {
	dir := tempDir(t)
	exe := filepath.Join(dir, "Worklog.exe")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	dl := filepath.Join(tempDir(t), "Worklog.exe")
	if err := os.WriteFile(dl, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := backend.InstallUpdate(dl, exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new" {
		t.Fatalf("installed binary = %q, want the download", got)
	}
	if got, _ := os.ReadFile(exe + ".previous"); string(got) != "old" {
		t.Fatalf("previous binary = %q, want the old one kept until restart", got)
	}
	backend.RemovePreviousBinary(exe)
	if _, err := os.Stat(exe + ".previous"); !os.IsNotExist(err) {
		t.Fatal("the previous binary should be gone after start-up")
	}

	// A download that is not there leaves the running binary where it was.
	if err := backend.InstallUpdate(filepath.Join(dir, "missing.exe"), exe); err == nil {
		t.Fatal("installing a missing download should fail")
	}
	if got, _ := os.ReadFile(exe); string(got) != "new" {
		t.Fatalf("a failed update must leave the binary alone, got %q", got)
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Check for updates finds the newer release, the gear gets its dot, and
// Update downloads it, swaps it in and restarts into it.
func TestSettingsUpdatePanelInstallsTheLatestRelease(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	ui.Tabs = shell.NewPillTabs(container.NewTabItem(shell.SettingsTabName, widget.NewLabel("settings")))

	dir := tempDir(t)
	exe := filepath.Join(dir, "Worklog.exe")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldVersion, oldLatest, oldDownload, oldExe, oldRelaunch, oldQuit :=
		backend.Version, backend.LatestRelease, backend.DownloadRelease, backend.ExecutablePath, backend.Relaunch, settings.QuitForUpdate
	t.Cleanup(func() {
		backend.Version, backend.LatestRelease, backend.DownloadRelease, backend.ExecutablePath, backend.Relaunch, settings.QuitForUpdate =
			oldVersion, oldLatest, oldDownload, oldExe, oldRelaunch, oldQuit
	})
	backend.Version = "v1.0.0"
	backend.LatestRelease = func() (backend.Release, error) {
		return backend.Release{Tag: "v1.1.0", Published: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)}, nil
	}
	backend.DownloadRelease = func(tag string) (string, error) {
		path := filepath.Join(tempDir(t), "Worklog.exe")
		return path, os.WriteFile(path, []byte("new "+tag), 0o755)
	}
	backend.ExecutablePath = func() (string, error) { return exe, nil }
	var relaunched string
	backend.Relaunch = func(path string) error { relaunched = path; return nil }
	quit := false
	settings.QuitForUpdate = func() { quit = true }

	view := settings.BuildSettingsTab(ui)
	if !contains(labels(view), "Version v1.0.0") {
		t.Fatalf("the sidebar should name the running version, labels %v", labels(view))
	}
	check := buttonNamed(view, "Check for updates")
	if check == nil {
		t.Fatal("no Check for updates button")
	}
	if ui.Update.InstallView.Visible() || ui.Tabs.UpdateDot.Visible() {
		t.Fatal("nothing should be offered before a check")
	}
	test.Tap(check)
	waitFor(t, "the check", func() bool { return ui.Update.Available })
	if !strings.HasPrefix(ui.Update.Status.Text, "v1.1.0 is available") {
		t.Fatalf("status = %q", ui.Update.Status.Text)
	}
	if !ui.Tabs.UpdateDot.Visible() || !ui.Update.InstallView.Visible() || ui.Update.Install.Text != "Update to v1.1.0" {
		t.Fatalf("an update should light the gear and show Update to v1.1.0 (dot %v, button %v %q)",
			ui.Tabs.UpdateDot.Visible(), ui.Update.InstallView.Visible(), ui.Update.Install.Text)
	}
	ok, c := primaryAround(view, "Update to v1.1.0", nil)
	if !ok || c != shell.GitHubGreen {
		t.Fatalf("Update should be the green pill, got %v", c)
	}

	test.Tap(ui.Update.Install)
	waitFor(t, "the restart", func() bool { return quit })
	if got, _ := os.ReadFile(exe); string(got) != "new v1.1.0" {
		t.Fatalf("installed binary = %q", got)
	}
	if relaunched != exe {
		t.Fatalf("relaunched %q, want %q", relaunched, exe)
	}
}

// The quiet check at start-up says nothing when there is nothing to offer.
func TestStartupCheckIsQuietWhenUpToDate(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	oldVersion, oldLatest := backend.Version, backend.LatestRelease
	t.Cleanup(func() { backend.Version, backend.LatestRelease = oldVersion, oldLatest })
	backend.Version = "v1.1.0"
	backend.LatestRelease = func() (backend.Release, error) { return backend.Release{Tag: "v1.1.0"}, nil }
	settings.BuildSettingsTab(ui)
	settings.CheckForUpdates(ui, false)
	waitFor(t, "the check", func() bool { return !ui.Update.Busy })
	if ui.Update.Available || ui.Update.InstallView.Visible() {
		t.Fatal("an up-to-date build should not offer an update")
	}
}
