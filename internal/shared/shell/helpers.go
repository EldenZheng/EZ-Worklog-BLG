package shell

import (
	_ "embed"
	"image/color"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ============================ helpers ============================

func Bold(s string) fyne.CanvasObject {
	return widget.NewLabelWithStyle(s, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
}

func ColorLabel(s string, name fyne.ThemeColorName) fyne.CanvasObject {
	l := canvas.NewText(s, theme.Color(name))
	l.TextSize = theme.TextSize()
	return l
}

// WideSelect holds a dropdown open wide enough for its longest option.
//
// fyne sizes a Select from its *placeholder*, not from what it displays:
// selectRenderer.MinSize overwrites the label width with the placeholder's, so
// the default "(Select one)" decides the box and any longer option spills out
// under the arrow. Measure the options and pin the width to the widest.
func WideSelect(sel fyne.CanvasObject) fyne.CanvasObject {
	var options []string
	switch control := sel.(type) {
	case *widget.Select:
		options = control.Options
	case *GoldSelect:
		options = control.Options
	}
	w := float32(0)
	for _, o := range options {
		if m := fyne.MeasureText(o, theme.TextSize(), fyne.TextStyle{}); m.Width > w {
			w = m.Width
		}
	}
	// The same allowance the renderer adds around its label: inner padding on
	// both sides plus room for the drop-down arrow.
	w += theme.InnerPadding()*4 + theme.IconInlineSize()
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(w, 0))
	return container.NewStack(spacer, sel)
}

// Truncate elides to n characters. It counts runes, not bytes: cutting an issue
// title mid-codepoint would render the tail as a replacement glyph.
func Truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimRight(string(r[:n-1]), " ") + "…"
}

func OrDash(s string) string {
	if s == "" {
		return "other"
	}
	return s
}

func openURL(raw string) {
	if u, err := url.Parse(raw); err == nil {
		_ = fyne.CurrentApp().OpenURL(u)
	}
}
