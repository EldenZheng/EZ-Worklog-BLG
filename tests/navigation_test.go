package tests

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
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

// An update lights the gear and opens an "Update Available" capsule to its
// left that pushes the tabs beside it along; a click on it opens Settings.
func TestUpdateBadgeOpensBesideTheGear(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	tabs := shell.NewPillTabs(container.NewTabItem("Log Work", widget.NewLabel("log")),
		container.NewTabItem(shell.KanbanTabName, widget.NewLabel("kanban")),
		container.NewTabItemWithIcon(shell.SettingsTabName, theme.SettingsIcon(), widget.NewLabel("settings")))
	w := a.NewWindow("update badge")
	w.SetContent(tabs)
	w.Resize(fyne.NewSize(700, 300))
	kanban := tabs.Buttons[1]
	before := fyne.CurrentApp().Driver().AbsolutePositionForObject(kanban).X

	tabs.ShowUpdate(true)
	if !tabs.UpdateShown() {
		t.Fatal("the gear should announce the update")
	}
	var text *canvas.Text
	walkRendered(tabs, func(o fyne.CanvasObject) {
		if x, ok := o.(*canvas.Text); ok && x.Text == shell.UpdateBadgeText {
			text = x
		}
	})
	if text == nil {
		t.Fatal("no Update Available text by the gear")
	}
	if _, _, _, alpha := text.Color.RGBA(); alpha == 0 {
		t.Fatal("Update Available should be showing once the badge is open")
	}
	after := fyne.CurrentApp().Driver().AbsolutePositionForObject(kanban).X
	if after >= before {
		t.Fatalf("the open badge should push Kanban left (%v, then %v)", before, after)
	}

	tabs.ShowUpdate(false)
	if tabs.UpdateShown() {
		t.Fatal("the badge should go once there is nothing to update")
	}
	if back := fyne.CurrentApp().Driver().AbsolutePositionForObject(kanban).X; back != before {
		t.Fatalf("Kanban should return to %v, is at %v", before, back)
	}
}

// walkRendered visits o and everything drawn inside it, widgets' insides
// included.
func walkRendered(o fyne.CanvasObject, visit func(fyne.CanvasObject)) {
	visit(o)
	switch x := o.(type) {
	case *fyne.Container:
		for _, child := range x.Objects {
			walkRendered(child, visit)
		}
	case fyne.Widget:
		for _, child := range test.WidgetRenderer(x).Objects() {
			walkRendered(child, visit)
		}
	}
}
