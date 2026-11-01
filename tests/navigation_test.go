package tests

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestPillNavigationPreservesPagesAndSelection(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	entry := widget.NewEntry()
	entry.SetText("unsaved remark")
	tabs := shell.NewPillTabs(container.NewTabItem("Log Work", entry),
		container.NewTabItemWithIcon("Settings", theme.SettingsIcon(), widget.NewLabel("settings")))
	w := a.NewWindow("navigation")
	w.SetContent(tabs)
	w.Resize(fyne.NewSize(420, 300))
	calls := 0
	tabs.OnSelected = func(item *container.TabItem) {
		calls++
		if item != tabs.Selected() || !item.Content.Visible() {
			t.Fatal("callback must see the selected, visible page")
		}
	}
	test.Tap(tabs.Buttons[1])
	if entry.Visible() || tabs.SelectedIndex() != 1 || tabs.Buttons[1].Importance != widget.LowImportance || tabs.Buttons[1].Text != "" {
		t.Fatal("click should activate Settings and hide Log Work")
	}
	tabs.SelectIndex(0)
	if entry.Text != "unsaved remark" || !entry.Visible() || tabs.Items[1].Content.Visible() {
		t.Fatal("returning to a page must preserve its input and visibility")
	}
	tabs.SelectIndex(0)
	tabs.SelectIndex(-1)
	tabs.SelectIndex(7)
	if calls != 2 {
		t.Fatalf("expected exactly two page-change callbacks, got %d", calls)
	}
}

func TestGoldAccentPreservesStatusColours(t *testing.T) {
	base := theme.DefaultTheme()
	gold := shell.GoldTheme{Theme: base}
	for _, variant := range []fyne.ThemeVariant{theme.VariantDark, theme.VariantLight} {
		for _, name := range []fyne.ThemeColorName{theme.ColorNameSuccess, theme.ColorNameWarning, theme.ColorNameError} {
			if gold.Color(name, variant) != base.Color(name, variant) {
				t.Fatalf("brand accent changed status colour %s", name)
			}
		}
		if gold.Color(theme.ColorNamePrimary, variant) != base.Color(theme.ColorNamePrimary, variant) {
			t.Fatal("primary action buttons should retain their original accent")
		}
		if (shell.PillTheme{Theme: gold}).Color(theme.ColorNamePrimary, variant) != widgets.LogoGold {
			t.Fatal("active navigation pill should retain logo gold")
		}
	}
}
