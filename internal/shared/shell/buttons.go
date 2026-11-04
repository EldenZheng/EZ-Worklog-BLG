package shell

// Button roles. A menu never colours or rounds a button itself: it builds a
// plain *widget.Button and hands it to the role that says what the button does.
// That keeps every screen on the same few looks, and the guard test in
// tests/button_roles_test.go fails if a menu sets an Importance colour or uses
// one of Fyne's stock confirm dialogs (whose confirm button falls back to
// Fyne's default blue, not one of these).
//
//	Role                  Look                    Use it for
//	GitHubButton          green pill              anything that writes to GitHub: push,
//	CompactGitHubButton   small green pill          re-push, push again, create an issue,
//	                      (cards and tiles)         apply a field to the project
//	ConfirmButton         green pill              accepting a popup's choice (Confirm) or
//	                                                starting an update; RestyleStockDialog
//	                                                for Fyne's own dialogs
//	DestructiveButton     red pill                removing something (Delete, Clear,
//	                                                Close anyway)
//	(plain button)        gold, rounded           everything local: Save, Cancel, Close,
//	                                                fetching or refreshing (reads only)
//	QuietButton           no fill                 icon buttons and Back
//
// Pick by what happens when it is pressed, not by how important it looks: a
// local Save is gold even when it is the main button on the screen, and a
// refresh that only reads from GitHub is gold too.

import (
	"image/color"
	"reflect"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// GitHubGreen is the fill of every GitHub-writing button and of the green
// tags that sit beside them.
var GitHubGreen = color.NRGBA{R: 0x28, G: 0x80, B: 0x48, A: 0xff}

// gitHubTheme scopes the green fill and pill shape to one button, disabled
// state included.
type gitHubTheme struct {
	fyne.Theme
	compact bool
}

func (t gitHubTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		return GitHubGreen
	case theme.ColorNameForegroundOnPrimary:
		return color.White
	}
	return t.Theme.Color(name, variant)
}

func (t gitHubTheme) Size(name fyne.ThemeSizeName) float32 {
	if t.compact && name == theme.SizeNameInnerPadding {
		return 6
	}
	if name == theme.SizeNameButtonRadius {
		return 100
	}
	return t.Theme.Size(name)
}

// GitHubButton styles a button that writes to GitHub: green, pill-shaped.
func GitHubButton(button *widget.Button) fyne.CanvasObject {
	button.Importance = widget.HighImportance
	return container.NewThemeOverride(widgets.WithPointerCursor(button), gitHubTheme{Theme: theme.Current()})
}

// CompactGitHubButton is GitHubButton with less padding, for a card or tile
// where it shares a line with icon buttons. It keeps its own height, so taller
// neighbours do not stretch it.
func CompactGitHubButton(button *widget.Button) fyne.CanvasObject {
	return compactGitHub(button, button)
}

// CompactGitHubHoverButton is CompactGitHubButton for a HoverButton, one that
// keeps the card it sits on lit while the pointer is on it.
func CompactGitHubHoverButton(button *HoverButton) fyne.CanvasObject {
	return compactGitHub(button, &button.Button)
}

func compactGitHub(control interface {
	fyne.CanvasObject
	Disabled() bool
}, button *widget.Button) fyne.CanvasObject {
	button.Importance = widget.HighImportance
	styled := container.NewThemeOverride(widgets.WithPointerCursor(control), gitHubTheme{Theme: theme.Current(), compact: true})
	return container.New(compactButtonLayout{}, styled)
}

type compactButtonLayout struct{}

func (compactButtonLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return objects[0].MinSize().AddWidthHeight(8, 0)
}

func (compactButtonLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	button := objects[0]
	want := button.MinSize().AddWidthHeight(8, 0)
	button.Resize(want)
	button.Move(fyne.NewPos((size.Width-want.Width)/2, (size.Height-want.Height)/2))
}

// ConfirmButton styles the button that accepts a popup's choice, such as the
// colour picker's Confirm. It wears the same green pill as GitHubButton, so
// every "go ahead" in the app looks alike.
func ConfirmButton(button *widget.Button) fyne.CanvasObject {
	return GitHubButton(button)
}

// RestyleStockDialog is for a dialog Fyne builds whole, such as the colour
// picker, whose buttons never pass through a role. Fyne marks their confirm
// button HighImportance, which paints it Fyne's default blue. Call it straight
// after the dialog's Show: it swaps each such button in the dialog on top of
// win for ConfirmButton, in place, and reports whether it found one.
func RestyleStockDialog(win fyne.Window) bool {
	top := win.Canvas().Overlays().Top()
	if top == nil {
		return false
	}
	return restyleHighButtons(top)
}

// overlayContent is what a popup wraps. A dialog sits in Fyne's internal
// OverlayContainer, which cannot be named from here but keeps its body in an
// exported Content field, as widget.PopUp does; reflection reads either.
func overlayContent(o fyne.CanvasObject) fyne.CanvasObject {
	v := reflect.ValueOf(o)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return nil
	}
	f := v.Elem().FieldByName("Content")
	if !f.IsValid() || !f.CanInterface() {
		return nil
	}
	c, _ := f.Interface().(fyne.CanvasObject)
	return c
}

func restyleHighButtons(o fyne.CanvasObject) bool {
	box, ok := o.(*fyne.Container)
	if !ok {
		if inner := overlayContent(o); inner != nil {
			return restyleHighButtons(inner)
		}
		return false
	}
	found := false
	for i, child := range box.Objects {
		if b, ok := child.(*widget.Button); ok && b.Importance == widget.HighImportance {
			box.Objects[i] = ConfirmButton(b)
			found = true
			continue
		}
		if restyleHighButtons(child) {
			found = true
		}
	}
	if found {
		box.Refresh()
	}
	return found
}

// DestructiveButton styles a button that removes something: red, pill-shaped.
// For a Cancel / Delete pair use RemovalButtons, which places both.
func DestructiveButton(button *widget.Button) fyne.CanvasObject {
	return removalButton(button, true)
}

// QuietButton strips a button's fill, for icon buttons and Back.
func QuietButton(button *widget.Button) fyne.CanvasObject {
	button.Importance = widget.LowImportance
	return widgets.WithPointerCursor(button)
}

// DialogButtons lines a popup's buttons up at its bottom right, in the order
// given. Put the way out (Close, Cancel) first and the action last.
func DialogButtons(buttons ...fyne.CanvasObject) fyne.CanvasObject {
	return container.NewHBox(append([]fyne.CanvasObject{layout.NewSpacer()}, buttons...)...)
}
