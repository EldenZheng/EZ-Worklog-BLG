package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func TestPillNavigationPreservesPagesAndSelection(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	entry := widget.NewEntry()
	entry.SetText("unsaved remark")
	tabs := newPillTabs(container.NewTabItem("Log Work", entry),
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
	test.Tap(tabs.buttons[1])
	if entry.Visible() || tabs.SelectedIndex() != 1 || tabs.buttons[1].Importance != widget.LowImportance || tabs.buttons[1].Text != "" {
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
	gold := goldTheme{Theme: base}
	for _, variant := range []fyne.ThemeVariant{theme.VariantDark, theme.VariantLight} {
		for _, name := range []fyne.ThemeColorName{theme.ColorNameSuccess, theme.ColorNameWarning, theme.ColorNameError} {
			if gold.Color(name, variant) != base.Color(name, variant) {
				t.Fatalf("brand accent changed status colour %s", name)
			}
		}
		if gold.Color(theme.ColorNamePrimary, variant) != base.Color(theme.ColorNamePrimary, variant) {
			t.Fatal("primary action buttons should retain their original accent")
		}
		if (pillTheme{Theme: gold}).Color(theme.ColorNamePrimary, variant) != logoGold {
			t.Fatal("active navigation pill should retain logo gold")
		}
	}
}
