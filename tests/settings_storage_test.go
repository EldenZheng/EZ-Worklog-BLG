package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/settings"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func storageSettingsFixture(t *testing.T) (*shell.UI, fyne.CanvasObject) {
	t.Helper()
	a := test.NewApp()
	t.Cleanup(a.Quit)
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: a.NewWindow("settings"), HasCfg: true, Cfg: backend.Config{GithubAuthor: "sample-user", WorklogOwner: "team-alex", ArchiveDir: tempDir(t), ProjectURL: "https://github.com/orgs/storage-settings-tests/projects/1"}}
	archive := backend.HistoryArchive{Version: 1, Owner: "team-alex", Projects: []string{"orgs/storage-settings-tests/1"}, Through: backend.HistoryCutoff, Items: []backend.WorklogItem{{Owner: "team-alex", Date: "2025-07-21", URL: "saved", Minutes: 90}}}
	if err := backend.SaveHistoryArchive(ui.Cfg, archive); err != nil {
		t.Fatal(err)
	}
	view := settings.BuildSettingsTab(ui)
	ui.Win.SetContent(view)
	ui.Win.Resize(fyne.NewSize(1400, 1000))
	view.Resize(fyne.NewSize(1400, 1000))
	return ui, view
}

func hasRenderedText(o fyne.CanvasObject, text string) bool {
	found := false
	walkRendered(o, func(x fyne.CanvasObject) {
		switch v := x.(type) {
		case *widget.Label:
			if v.Text == text {
				found = true
			}
		case *canvas.Text:
			if v.Text == text {
				found = true
			}
		}
	})
	return found
}

func tapStoragePopupButton(t *testing.T, popup fyne.CanvasObject, name string) {
	t.Helper()
	var button *widget.Button
	walkRendered(popup, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == name {
			button = b
		}
	})
	if button == nil {
		t.Fatalf("popup button %q missing", name)
	}
	button.OnTapped()
}

func TestSettingsIdentityAndUpdatesStayFixedWhileStorageScrolls(t *testing.T) {
	ui, view := storageSettingsFixture(t)
	var sidebar *container.Scroll
	walk(view, func(o fyne.CanvasObject) {
		if s, ok := o.(*container.Scroll); ok && hasRenderedText(s.Content, "Storage") {
			sidebar = s
		}
	})
	if sidebar == nil {
		t.Fatal("storage needs its sidebar scroll")
	}
	if hasRenderedText(sidebar.Content, "Identity") || hasRenderedText(sidebar.Content, "Worklog owner") {
		t.Fatal("identity fields must stay outside the scrolling area")
	}
	if !hasRenderedText(sidebar.Content, "History backup") {
		t.Fatal("backup must sit above updates")
	}
	if hasRenderedText(sidebar.Content, "Updates") {
		t.Fatal("Updates must stay outside the storage scroll")
	}
	before := fyne.CurrentApp().Driver().AbsolutePositionForObject(ui.Update.Check)
	sidebar.ScrollToBottom()
	if after := fyne.CurrentApp().Driver().AbsolutePositionForObject(ui.Update.Check); after != before {
		t.Fatal("scrolling storage moved Updates")
	}
	frameBottom := fyne.CurrentApp().Driver().AbsolutePositionForObject(ui.Update.Frame).Y + ui.Update.Frame.Size().Height
	if bottom := before.Y + ui.Update.Check.Size().Height; frameBottom-bottom > 1 {
		t.Fatalf("Updates is not pinned to the sidebar bottom: gap %v", frameBottom-bottom)
	}
	var worklog, projects, storage, updates *canvas.Text
	walkRendered(view, func(o fyne.CanvasObject) {
		if text, ok := o.(*canvas.Text); ok {
			switch text.Text {
			case "Worklog":
				if text.TextSize != 26 {
					worklog = text
				}
			case "Projects":
				projects = text
			case "Storage":
				storage = text
			case "Updates":
				updates = text
			}
		}
	})
	if worklog == nil || projects == nil || storage == nil || updates == nil {
		t.Fatal("missing Settings sections")
	}
	driver := fyne.CurrentApp().Driver()
	wpos, ppos := driver.AbsolutePositionForObject(worklog), driver.AbsolutePositionForObject(projects)
	if wpos.Y != ppos.Y || ppos.X <= wpos.X {
		t.Fatalf("Worklog and Projects must sit side by side: %v %v", wpos, ppos)
	}
	if !sameColor(updates.Color, widgets.LogoGold) {
		t.Fatal("Updates heading must be gold")
	}
	for _, text := range []string{"Default push mode", "Anthropic API key"} {
		if hasRenderedText(view, text) {
			t.Fatalf("obsolete setting still shown: %s", text)
		}
	}
}

func TestUpdatesDividerComesBeforeItsGoldHeading(t *testing.T) {
	ui, view := storageSettingsFixture(t)
	var block *fyne.Container
	walk(view, func(o fyne.CanvasObject) {
		if c, ok := o.(*fyne.Container); ok && len(c.Objects) > 1 && c.Objects[len(c.Objects)-1] == ui.Update.Check {
			block = c
		}
	})
	if block == nil {
		t.Fatal("missing Updates section")
	}
	title, ok := block.Objects[1].(*canvas.Text)
	if !ok || title.Text != "Updates" || !sameColor(title.Color, widgets.LogoGold) {
		t.Fatal("gold Updates title must follow the divider")
	}
	var divider *widget.Separator
	walk(block.Objects[0], func(o fyne.CanvasObject) {
		if s, ok := o.(*widget.Separator); ok {
			divider = s
		}
	})
	if divider == nil {
		t.Fatal("Updates needs its line before the title")
	}
	driver := fyne.CurrentApp().Driver()
	if driver.AbsolutePositionForObject(divider).Y >= driver.AbsolutePositionForObject(title).Y {
		t.Fatal("divider rendered after the Updates title")
	}
}

func openStorageAction(t *testing.T, ui *shell.UI, view fyne.CanvasObject, name string) {
	t.Helper()
	tapButton(t, view, "…")
	popup := ui.Win.Canvas().Overlays().Top()
	if popup == nil {
		t.Fatal("backup options did not open")
	}
	tapStoragePopupButton(t, popup, name)
}

func TestSettingsClearBackupRequiresConfirmationAndKeepsLocalData(t *testing.T) {
	ui, view := storageSettingsFixture(t)
	protected := filepath.Join(ui.Store.Dir, "worklog.csv")
	if err := os.WriteFile(protected, []byte("keep local data"), 0o600); err != nil {
		t.Fatal(err)
	}
	openStorageAction(t, ui, view, "Clear backup")
	popup := ui.Win.Canvas().Overlays().Top()
	if popup == nil {
		t.Fatal("clear must ask for confirmation")
	}
	if _, err := backend.LoadHistoryArchive(ui.Cfg); err != nil {
		t.Fatal("opening confirmation deleted backup", err)
	}
	tapStoragePopupButton(t, popup, "Cancel")
	if _, err := backend.LoadHistoryArchive(ui.Cfg); err != nil {
		t.Fatal("cancel deleted backup", err)
	}
	openStorageAction(t, ui, view, "Clear backup")
	tapStoragePopupButton(t, ui.Win.Canvas().Overlays().Top(), "Clear backup")
	if _, err := os.Stat(backend.HistoryArchivePath(ui.Cfg)); !os.IsNotExist(err) {
		t.Fatal("confirmed clear did not remove archive", err)
	}
	if raw, err := os.ReadFile(protected); err != nil || string(raw) != "keep local data" {
		t.Fatal("clearing changed local worklogs", err)
	}
}

func TestSettingsFailedBackupReleasesControlsAndPreservesArchive(t *testing.T) {
	ui, view := storageSettingsFixture(t)
	before, err := os.ReadFile(backend.HistoryArchivePath(ui.Cfg))
	if err != nil {
		t.Fatal(err)
	}
	tapButton(t, view, "Back up from GitHub") // TestMain supplies the offline failure.
	after, err := os.ReadFile(backend.HistoryArchivePath(ui.Cfg))
	if err != nil || string(before) != string(after) {
		t.Fatal("failed fetch replaced old backup", err)
	}
	found := false
	walkRendered(view, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Back up from GitHub" {
			found = true
			if b.Disabled() {
				t.Error("failed fetch left backup disabled")
			}
		}
		if l, ok := o.(*widget.Label); ok && strings.HasPrefix(l.Text, "Backup failed:") {
			found = true
		}
	})
	if !found {
		t.Fatal("missing backup feedback")
	}
}

func TestSettingsReallocatesBackupAfterConfirmation(t *testing.T) {
	ui, view := storageSettingsFixture(t)
	previous := ui.Cfg
	destination := tempDir(t)
	openStorageAction(t, ui, view, "Reallocate backup")
	popup := ui.Win.Canvas().Overlays().Top()
	if popup == nil {
		t.Fatal("reallocation must open a confirmation")
	}
	var folder *widget.Entry
	walkRendered(popup, func(o fyne.CanvasObject) {
		if e, ok := o.(*widget.Entry); ok && e.Text == backend.HistoryDirectory(previous) {
			folder = e
		}
	})
	if folder == nil {
		t.Fatal("backup location is not editable")
	}
	folder.SetText(destination)
	if _, err := backend.LoadHistoryArchive(previous); err != nil {
		t.Fatal("choosing a folder moved the backup before confirmation", err)
	}
	tapStoragePopupButton(t, popup, "Reallocate")
	if ui.Cfg.ArchiveDir != destination {
		t.Fatalf("new folder not applied: %q", ui.Cfg.ArchiveDir)
	}
	saved, ok := ui.Store.LoadConfig()
	if !ok || saved.ArchiveDir != destination {
		t.Fatal("new location was not saved")
	}
	if _, err := backend.LoadHistoryArchive(ui.Cfg); err != nil {
		t.Fatal("new backup is unreadable", err)
	}
	if _, err := os.Stat(backend.HistoryArchivePath(previous)); !os.IsNotExist(err) {
		t.Fatal("old archive was not removed after the new copy was verified", err)
	}
}

func TestBackupOptionsAreInEllipsisAndStorageUsesSlimGoldMeter(t *testing.T) {
	ui, view := storageSettingsFixture(t)
	for _, name := range []string{"Reallocate backup", "Clear backup"} {
		if hasRenderedText(view, name) {
			t.Fatalf("%s should only appear inside the backup options menu", name)
		}
	}
	var storage *container.Scroll
	walk(view, func(o fyne.CanvasObject) {
		if s, ok := o.(*container.Scroll); ok && hasRenderedText(s.Content, "Storage") {
			storage = s
		}
	})
	if storage == nil {
		t.Fatal("missing storage scroll")
	}
	slimGold := false
	walkRendered(storage.Content, func(o fyne.CanvasObject) {
		if _, ok := o.(*widget.ProgressBar); ok {
			t.Error("storage should use the shared meter, not a stock progress bar")
		}
		if rect, ok := o.(*canvas.Rectangle); ok && sameColor(rect.FillColor, widgets.LogoGold) && rect.CornerRadius == 3 {
			slimGold = true
		}
	})
	if !slimGold {
		t.Fatal("missing the app's slim gold progress fill")
	}
	tapButton(t, view, "…")
	popup := ui.Win.Canvas().Overlays().Top()
	if popup == nil || !hasRenderedText(popup, "Reallocate backup") || !hasRenderedText(popup, "Clear backup") {
		t.Fatal("backup menu is missing its actions")
	}
}

func TestReallocateUsesPlatformFolderPickerWithoutMovingUntilConfirmed(t *testing.T) {
	ui, view := storageSettingsFixture(t)
	previous := settings.PickBackupFolder
	t.Cleanup(func() { settings.PickBackupFolder = previous })
	destination := tempDir(t)
	calls := 0
	settings.PickBackupFolder = func(got *shell.UI, initial string, done func(string, error)) {
		calls++
		if got != ui || initial != backend.HistoryDirectory(ui.Cfg) {
			t.Error("picker did not start in the current backup folder")
		}
		done(destination, nil)
	}
	openStorageAction(t, ui, view, "Reallocate backup")
	popup := ui.Win.Canvas().Overlays().Top()
	tapStoragePopupButton(t, popup, "Choose folder")
	if calls != 1 {
		t.Fatal("native picker was not requested")
	}
	var selected *widget.Entry
	walkRendered(popup, func(o fyne.CanvasObject) {
		if e, ok := o.(*widget.Entry); ok && e.Text == destination {
			selected = e
		}
	})
	if selected == nil {
		t.Fatal("chosen folder did not update the relocation form")
	}
	if ui.Cfg.ArchiveDir == destination {
		t.Fatal("choosing a folder moved the backup before confirmation")
	}
	tapStoragePopupButton(t, popup, "Cancel")
	if _, err := backend.LoadHistoryArchive(ui.Cfg); err != nil {
		t.Fatal("cancel changed the backup", err)
	}
}
