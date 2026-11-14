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
//	SmallDeleteButton     small red rounded       a Delete that fades in while the
//	                      rectangle                 pointer is on a calendar day or block
//	(plain button)        gold, rounded           everything local: Save, Cancel, Close,
//	                                                fetching or refreshing (reads only)
//	QuietButton           no fill                 icon buttons and Back
//
// Pick by what happens when it is pressed, not by how important it looks: a
// local Save is gold even when it is the main button on the screen, and a
// refresh that only reads from GitHub is gold too.

import (
	"fmt"
	"image/color"
	"reflect"
	"time"

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

// SmallDeleteButton is a Delete that shows while the pointer is on a Sprint
// Calendar day or block: DestructiveButton's red at caption size, as a rounded
// rectangle short enough for a quarter-hour block, fading in and out rather
// than popping. control is the widget that holds button: the button itself, or
// a type built on it that also follows the hover. Place View, and show and hide
// it with FadeTo.
func SmallDeleteButton(control interface {
	fyne.CanvasObject
	Disabled() bool
}, button *widget.Button) *SmallDelete {
	button.Importance = widget.HighImportance
	d := &SmallDelete{control: control, button: button, icon: button.Icon}
	d.View = container.NewThemeOverride(widgets.WithPointerCursor(control),
		smallRemovalTheme{removalButtonTheme: removalButtonTheme{Theme: theme.Current(), destructive: true}, alpha: &d.alpha})
	control.Hide()
	d.View.Hide()
	return d
}

// SmallDelete is a SmallDeleteButton and its fade.
type SmallDelete struct {
	View    fyne.CanvasObject
	control fyne.CanvasObject
	button  *widget.Button
	icon    fyne.Resource // the button's own icon, before any fade step
	alpha   float32
	shown   bool
	fade    *fyne.Animation
	// leaving is set while a fade-out waits out smallDeleteLinger; outGen
	// tells a stale wait from the current one.
	leaving bool
	outGen  int
	// OnChange runs when the button comes onto the screen and when it has
	// faded off it, for whatever has to make room for it.
	OnChange func()
}

// smallDeleteFade is how long the Delete takes to fade in or out.
const smallDeleteFade = 320 * time.Millisecond

// smallDeleteLinger is how long the Delete stays after the pointer leaves
// before it starts to fade: crossing from a block onto its own link or
// Delete, or between them, then never sets the fade going backwards and
// forwards.
const smallDeleteLinger = 150 * time.Millisecond

// Shown reports whether the button is showing or fading in, and not about to
// fade out.
func (d *SmallDelete) Shown() bool { return d.shown && !d.leaving }

// FadeTo fades the button in (show) or out, then hides it. A fade-out waits a
// moment first, and a fade-in in that moment keeps the button as it is.
func (d *SmallDelete) FadeTo(show bool) {
	if d.leaving {
		if !show {
			return // already on its way out
		}
		d.leaving = false
		d.outGen++ // the wait under way no longer applies
	}
	if show == d.shown {
		return
	}
	if !show && animationsOn() {
		d.leaving = true
		gen := d.outGen
		// Waited out as an animation, so it ends on the same thread as the
		// fades themselves.
		fyne.NewAnimation(smallDeleteLinger, func(p float32) {
			if p >= 1 && d.leaving && d.outGen == gen {
				d.leaving = false
				d.fadeNow(false)
			}
		}).Start()
		return
	}
	d.fadeNow(show)
}

// fadeNow starts the fade in or out at once.
func (d *SmallDelete) fadeNow(show bool) {
	if show == d.shown {
		return
	}
	d.shown = show
	if d.fade != nil {
		d.fade.Stop()
		d.fade = nil
	}
	if show {
		d.control.Show()
		d.View.Show()
		if d.OnChange != nil {
			d.OnChange()
		}
	}
	to := float32(0)
	if show {
		to = 1
	}
	if !animationsOn() {
		d.setAlpha(to)
		d.settle()
		return
	}
	from := d.alpha
	var fade *fyne.Animation
	// Turning back part way takes only the share of the fade left to undo.
	length := time.Duration(float32(smallDeleteFade) * max(0.2, abs32(to-from)))
	fade = fyne.NewAnimation(length, func(p float32) {
		d.setAlpha(from + (to-from)*p)
		if p >= 1 && d.fade == fade {
			d.fade = nil
			d.settle()
		}
	})
	fade.Curve = fyne.AnimationEaseInOut
	d.fade = fade
	fade.Start()
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func (d *SmallDelete) setAlpha(alpha float32) {
	d.alpha = alpha
	if d.icon != nil {
		// Fyne keeps a drawn SVG under its resource's name, whatever colour it
		// was drawn in, so an icon first painted as the fade began stayed that
		// faint for good while the text faded in around it. Each step of the
		// fade gets a name of its own and is drawn in its own alpha.
		d.button.Icon = fadedIcon{source: d.icon, step: int(max(0, min(1, alpha))*smallDeleteFadeSteps + 0.5)}
	}
	d.button.Refresh()
}

// smallDeleteFadeSteps is how many alphas the Delete's icon is drawn in.
const smallDeleteFadeSteps = 24

// fadedIcon is an icon at one step of a fade. Its content and colour are the
// source's; only its name, which Fyne caches the drawn image under, differs.
type fadedIcon struct {
	source fyne.Resource
	step   int
}

func (f fadedIcon) Name() string {
	return fmt.Sprintf("fade%d_%s", f.step, f.source.Name())
}

func (f fadedIcon) Content() []byte { return f.source.Content() }

func (f fadedIcon) ThemeColorName() fyne.ThemeColorName {
	if t, ok := f.source.(fyne.ThemedResource); ok {
		return t.ThemeColorName()
	}
	return theme.ColorNameForeground
}

// settle hides the button once it has faded out.
func (d *SmallDelete) settle() {
	if d.shown {
		return
	}
	d.control.Hide()
	d.View.Hide()
	if d.OnChange != nil {
		d.OnChange()
	}
}

// smallRemovalTheme draws the small Delete: caption-sized, gently rounded, and
// as opaque as its fade has got to.
type smallRemovalTheme struct {
	removalButtonTheme
	alpha *float32
}

func (t smallRemovalTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	c := t.removalButtonTheme.Color(name, variant)
	if t.alpha == nil || *t.alpha >= 1 {
		return c
	}
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	n.A = uint8(float32(n.A) * max(0, *t.alpha))
	return n
}

func (t smallRemovalTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return 11
	case theme.SizeNameInlineIcon:
		return 11
	case theme.SizeNameInnerPadding:
		// Taller than the text needs, so it reads as a button. A quarter-hour
		// block squeezes it back to fit.
		return 4
	case theme.SizeNamePadding:
		return 2
	case theme.SizeNameButtonRadius:
		return 6 // a rounded rectangle, not a pill
	}
	return t.removalButtonTheme.Size(name)
}

// QuietButton strips a button's fill, for icon buttons and Back.
func QuietButton(button *widget.Button) fyne.CanvasObject {
	return QuietButtonControl(button, button)
}

// QuietButtonControl applies the quiet role to an extended button while keeping
// its own event handlers, such as a refresh icon that spins only when clicked.
func QuietButtonControl(control interface {
	fyne.CanvasObject
	Disabled() bool
}, button *widget.Button) fyne.CanvasObject {
	button.Importance = widget.LowImportance
	return widgets.WithPointerCursor(control)
}

// DialogButtons lines a popup's buttons up at its bottom right, in the order
// given. Put the way out (Close, Cancel) first and the action last.
func DialogButtons(buttons ...fyne.CanvasObject) fyne.CanvasObject {
	return container.NewHBox(append([]fyne.CanvasObject{layout.NewSpacer()}, buttons...)...)
}
