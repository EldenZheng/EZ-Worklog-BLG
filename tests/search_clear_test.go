package tests

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/shell"
)

// clearButton digs the X out of a search field's action item.
func clearButton(t *testing.T, e *widget.Entry) *widget.Button {
	t.Helper()
	if e.ActionItem == nil {
		t.Fatal("search field has no clear button")
	}
	var found *widget.Button
	walk(e.ActionItem, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok {
			found = b
		}
	})
	if found == nil {
		t.Fatal("search field's action item holds no button")
	}
	return found
}

// The X shows only while there is a search, and clearing it is an edit like
// any other: the list behind hears about it and redraws.
func TestSearchClearButtonEmptiesTheField(t *testing.T) {
	test.NewApp()
	defer fyne.CurrentApp().Quit()
	var heard []string
	search := shell.NewSearchEntry("Search", "", func(s string) { heard = append(heard, s) })
	// Fyne paints only the action item that was set when the entry's renderer
	// was built. Set it after SetPlaceHolder or SetText and it is laid out,
	// reports itself visible, and never appears on screen.
	drawn := false
	for _, o := range test.WidgetRenderer(search).Objects() {
		if o == search.ActionItem {
			drawn = true
		}
	}
	if !drawn {
		t.Fatal("the X is not among the objects the entry draws, so it never shows")
	}
	if search.ActionItem.Visible() {
		t.Fatal("an empty search should not show the X")
	}
	search.SetText("invoice")
	if !search.ActionItem.Visible() {
		t.Fatal("a search with text should show the X")
	}
	test.Tap(clearButton(t, search))
	if search.Text != "" || search.ActionItem.Visible() {
		t.Fatalf("after X: text %q, X visible %v", search.Text, search.ActionItem.Visible())
	}
	if len(heard) != 2 || heard[1] != "" {
		t.Fatalf("onChanged heard %q, want the search then the clear", heard)
	}

	// A search carried over from before starts with its X showing.
	if kept := shell.NewSearchEntry("Search", "kept", nil); !kept.ActionItem.Visible() {
		t.Fatal("a starting search should show the X")
	}
}

// Log Work's search is the clearable one, and clearing it clears the search
// the lists filter on.
func TestLogWorkSearchClears(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	ui.LogSearch = "invoice"
	tab := logwork.BuildLogTab(ui)
	search := entryByPlaceholder(tab, "Search issues, titles or commits")
	if search == nil {
		t.Fatal("Log Work has no search field")
	}
	test.Tap(clearButton(t, search))
	if ui.LogSearch != "" {
		t.Fatalf("LogSearch = %q after clearing", ui.LogSearch)
	}
}
