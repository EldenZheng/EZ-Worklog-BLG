//go:build cursors

package main

import (
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func renderedObjects(root fyne.CanvasObject, visit func(fyne.CanvasObject)) {
	seen := map[fyne.CanvasObject]bool{}
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || seen[o] {
			return
		}
		seen[o] = true
		visit(o)
		if c, ok := o.(*fyne.Container); ok {
			for _, child := range c.Objects {
				walk(child)
			}
		}
		if w, ok := o.(fyne.Widget); ok {
			for _, child := range test.WidgetRenderer(w).Objects() {
				walk(child)
			}
		}
	}
	walk(root)
}

// Exercise controls that Fyne creates itself, including modal actions and
// private radio/menu/tab widgets that application-level wrappers cannot reach.
func TestCursorPolicyIncludesNativePopupAndNavigationControls(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("cursor policy")
	button := widget.NewButton("Save", nil)
	check := widget.NewCheck("Show pushed", nil)
	selectBox := widget.NewSelect([]string{"Sub-issue", "Issue"}, nil)
	radio := widget.NewRadioGroup([]string{"Commits", "Meeting"}, nil)
	tabs := container.NewAppTabs(container.NewTabItem("Log work", container.NewVBox(button, check, selectBox, radio)), container.NewTabItem("Report", widget.NewLabel("Report")))
	w.SetContent(tabs)
	w.Resize(fyne.NewSize(1000, 700))
	counts := map[string]int{}
	verify := func(root fyne.CanvasObject) {
		renderedObjects(root, func(o fyne.CanvasObject) {
			name := fmt.Sprintf("%T", o)
			for _, kind := range []string{".Button", ".Check", ".Select", ".radioItem", ".tabButton", ".menuItem"} {
				if !strings.HasSuffix(name, kind) {
					continue
				}
				c, ok := o.(desktop.Cursorable)
				if !ok || c.Cursor() != desktop.PointerCursor {
					t.Errorf("%s lacks its hand cursor", name)
				}
				counts[kind]++
			}
		})
	}
	verify(tabs)
	dialog.ShowConfirm("Delete entry", "Delete this entry?", func(bool) {}, w)
	for _, overlay := range w.Canvas().Overlays().List() {
		verify(overlay)
	}
	verify(widget.NewMenu(fyne.NewMenu("Choices", fyne.NewMenuItem("Sub-issue", func() {}))))
	for _, kind := range []string{".Button", ".Check", ".Select", ".radioItem", ".tabButton", ".menuItem"} {
		if counts[kind] == 0 {
			t.Errorf("did not inspect %s", kind)
		}
	}
	for _, control := range []interface {
		fyne.Disableable
		desktop.Cursorable
	}{button, check, selectBox} {
		control.Disable()
		if control.Cursor() != desktop.DefaultCursor {
			t.Error("disabled control advertises a click")
		}
	}
	entry := widget.NewEntry()
	if entry.Cursor() != desktop.TextCursor {
		t.Fatal("text editing lost its text cursor")
	}
}
