package tests

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/logwork"
	"worklog/internal/menus/settings"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// Buttons are styled only through the roles in shell/buttons.go. A bare
// HighImportance button, or the confirm button of a stock Fyne dialog, is
// painted Fyne's default blue, which matches nothing else in the app. This
// covers main.go and the shared code as well as the menus: the close prompt in
// main.go was the one that slipped through when only menus were checked.
func TestMenusStyleButtonsThroughShellRoles(t *testing.T) {
	banned := map[string]string{
		"widget.HighImportance":    "use shell.GitHubButton / shell.ConfirmButton, or a plain button (local)",
		"widget.DangerImportance":  "use shell.DestructiveButton or shell.RemovalButtons",
		"widget.WarningImportance": "use one of the roles in shell/buttons.go",
		"widget.SuccessImportance": "use shell.GitHubButton",
		"dialog.NewConfirm":        "build the buttons with shell.DialogButtons",
		"dialog.ShowConfirm":       "build the buttons with shell.DialogButtons",
		"dialog.NewCustomConfirm":  "build the buttons with shell.DialogButtons",
		"dialog.ShowCustomConfirm": "build the buttons with shell.DialogButtons",
		"dialog.NewForm":           "build the buttons with shell.DialogButtons",
		"dialog.ShowForm":          "build the buttons with shell.DialogButtons",
	}
	// The roles themselves, and the tab bar, are where Importance is set.
	roleFiles := map[string]bool{"buttons.go": true, "confirmation_buttons.go": true, "navigation.go": true}
	files := []string{filepath.Join("..", "main.go")}
	err := filepath.WalkDir(filepath.Join("..", "internal"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		inShell := strings.Contains(filepath.ToSlash(path), "internal/shared/shell/")
		for i, line := range strings.Split(text, "\n") {
			for pattern, fix := range banned {
				if !strings.Contains(line, pattern) {
					continue
				}
				if pattern == "widget.HighImportance" && inShell && roleFiles[filepath.Base(path)] {
					continue
				}
				t.Errorf("%s:%d uses %s: %s", path, i+1, pattern, fix)
			}
		}
		// Fyne's colour picker has no way to style its Confirm from outside.
		if strings.Contains(text, "dialog.NewColorPicker") && !strings.Contains(text, "shell.RestyleStockDialog") {
			t.Errorf("%s opens a colour picker without shell.RestyleStockDialog, so its Confirm is blue", path)
		}
	}
}

// primaryAround reports the primary colour the theme override nearest above
// the named button gives it, or nil when no override wraps it.
func primaryAround(o fyne.CanvasObject, label string, th fyne.Theme) (found bool, primary any) {
	if b, ok := o.(*widget.Button); ok && b.Text == label {
		if th == nil {
			return true, nil
		}
		return true, th.Color(theme.ColorNamePrimary, theme.VariantDark)
	}
	var kids []fyne.CanvasObject
	switch x := o.(type) {
	case *container.ThemeOverride:
		th = x.Theme
		kids = []fyne.CanvasObject{x.Content}
	case *fyne.Container:
		kids = x.Objects
	case fyne.Widget:
		kids = test.WidgetRenderer(x).Objects()
	}
	for _, k := range kids {
		if ok, c := primaryAround(k, label, th); ok {
			return true, c
		}
	}
	return false, nil
}

// The failed-push popup's Push again is a GitHub write, so it is the same
// green as every other push button rather than the stock dialog's blue.
func TestPushAgainIsGitHubGreen(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	if _, err := ui.Store.AppendRows([]backend.Row{{
		"date": "2026-08-10", "minutes": "60", "issue": "exampleorg/app#1",
		"mode": "issue", "remarks": "Fix the export", "owner": "team-alex",
	}}); err != nil {
		t.Fatal(err)
	}
	old := backend.PushEntry
	backend.PushEntry = func(backend.Config, string, string, string, int, string, string, string, string, string, ...func(backend.FetchProgress)) (backend.PushResult, error) {
		return backend.PushResult{}, errors.New("GitHub is down")
	}
	t.Cleanup(func() { backend.PushEntry = old })

	logwork.DrawRecent(ui)
	push := buttonNamed(ui.WeekBox, "Push")
	if push == nil {
		t.Fatalf("saved entry has no push button: %v", labels(ui.WeekBox))
	}
	test.Tap(push)
	again := waitForButton(t, ui.Win, "Push again")
	if again.Importance != widget.HighImportance {
		t.Fatalf("Push again importance = %v, want HighImportance", again.Importance)
	}
	for _, o := range ui.Win.Canvas().Overlays().List() {
		if ok, c := primaryAround(o, "Push again", nil); ok {
			if c != shell.GitHubGreen {
				t.Fatalf("Push again primary colour = %v, want GitHubGreen", c)
			}
			return
		}
	}
	t.Fatal("Push again vanished before its colour could be read")
}

// Fyne's colour picker builds its own buttons and marks Confirm
// HighImportance. Opened from Settings, that Confirm is the green pill.
func TestOrgColorPickerConfirmIsGreen(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	settings.OrgColorPicker(ui, "exampleorg", shell.GitHubGreen, nil)
	confirm := waitForButton(t, ui.Win, "Confirm")
	if confirm.Importance != widget.HighImportance {
		t.Fatalf("Confirm importance = %v, want HighImportance", confirm.Importance)
	}
	ok, c := primaryAround(ui.Win.Canvas().Overlays().Top(), "Confirm", nil)
	if !ok || c != shell.GitHubGreen {
		t.Fatalf("colour picker Confirm primary = %v (found %v), want GitHubGreen", c, ok)
	}
}
