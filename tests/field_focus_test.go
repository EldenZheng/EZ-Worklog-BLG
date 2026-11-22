package tests

import (
	"errors"
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestFieldFocusBorderCaretAndSelectionAreGold(t *testing.T) {
	for _, variant := range []fyne.ThemeVariant{theme.VariantDark, theme.VariantLight} {
		for _, kind := range []string{"text", "multiline", "password", "search", "editable-select", "date"} {
			t.Run(kind+"/"+string(rune('0'+variant)), func(t *testing.T) {
				base := test.NewApp()
				defer base.Quit()
				fyne.SetCurrentApp(pressReducedMotionApp{App: base})
				defer fyne.SetCurrentApp(base)
				base.Settings().SetTheme(focusVariantTheme{GoldTheme: shell.GoldTheme{Theme: theme.DefaultTheme()}, variant: variant})
				var field fyne.Widget
				switch kind {
				case "text":
					field = widget.NewEntry()
				case "multiline":
					field = widget.NewMultiLineEntry()
				case "password":
					field = widget.NewPasswordEntry()
				case "search":
					field = shell.NewSearchEntry("Search", "", nil)
				case "editable-select":
					field = widget.NewSelectEntry([]string{"Example"})
				case "date":
					field = widget.NewDateEntry()
				}
				field.(interface{ SetText(string) }).SetText("2026-11-22")
				w := base.NewWindow("focus")
				w.SetContent(field)
				w.Resize(fyne.NewSize(400, 120))
				border := test.WidgetRenderer(field).Objects()[1].(*canvas.Rectangle)
				w.Canvas().Focus(field.(fyne.Focusable))
				if !sameColor(border.StrokeColor, widgets.LogoGold) {
					t.Fatalf("focused field border = %v, want gold", border.StrokeColor)
				}
				caret := false
				walkRendered(field, func(o fyne.CanvasObject) {
					if r, ok := o.(*canvas.Rectangle); ok && r.Visible() && sameColor(r.FillColor, widgets.LogoGold) {
						caret = true
					}
				})
				if !caret {
					t.Fatal("focused field caret is not gold")
				}
				field.(fyne.Shortcutable).TypedShortcut(&fyne.ShortcutSelectAll{})
				selected := false
				walkRendered(field, func(o fyne.CanvasObject) {
					if r, ok := o.(*canvas.Rectangle); ok && r.Visible() && sameColor(r.FillColor, widgets.WithAlpha(widgets.LogoGold, .30)) {
						selected = true
					}
				})
				if !selected {
					t.Fatal("selected text is not highlighted in gold")
				}
				w.Canvas().Unfocus()
			})
		}
	}
}

// Pin the requested variant even if the test driver uses the system default.
type focusVariantTheme struct {
	shell.GoldTheme
	variant fyne.ThemeVariant
}

func (t focusVariantTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return t.GoldTheme.Color(name, t.variant)
}

func TestFieldFocusPreservesValidationErrors(t *testing.T) {
	base := test.NewApp()
	defer base.Quit()
	fyne.SetCurrentApp(pressReducedMotionApp{App: base})
	defer fyne.SetCurrentApp(base)
	base.Settings().SetTheme(shell.GoldTheme{Theme: theme.DefaultTheme()})
	entry := widget.NewEntry()
	entry.Validator = func(string) error { return errors.New("invalid value") }
	w := base.NewWindow("validation")
	w.SetContent(entry)
	entry.SetText("invalid")
	w.Canvas().Focus(entry)
	border := test.WidgetRenderer(entry).Objects()[1].(*canvas.Rectangle)
	if !sameColor(border.StrokeColor, widgets.LogoGold) {
		t.Fatal("focused invalid entry should still show gold focus")
	}
	w.Canvas().Unfocus()
	if !sameColor(border.StrokeColor, theme.Color(theme.ColorNameError)) {
		t.Fatal("validation error colour was lost after focus left")
	}
}
