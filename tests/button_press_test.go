//go:build cursors

package tests

import (
	"image"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func pressTestApp(t *testing.T) (fyne.App, *collectDriver) {
	t.Helper()
	base := test.NewApp()
	driver := &collectDriver{Driver: base.Driver()}
	a := &motionTestApp{App: base, driver: driver}
	fyne.SetCurrentApp(a)
	t.Cleanup(func() { fyne.SetCurrentApp(base); base.Quit() })
	return a, driver
}

// One renderer policy covers plain buttons, every shell role and buttons that
// embed widget.Button. Animating only the paint must not change page geometry.
// A pressed button hides its fill, icon and label and draws one picture of
// them, shrunk about its centre, so the three cannot land on different pixels.
func TestAllButtonRolesCompressAndReturnWithoutReflow(t *testing.T) {
	for _, role := range []string{"plain", "github", "compact", "confirm", "destructive", "quiet", "icon", "small delete", "extended"} {
		t.Run(role, func(t *testing.T) {
			a, driver := pressTestApp(t)
			w := a.NewWindow("button press")
			defer w.Close()
			calls := 0
			b := widget.NewButtonWithIcon("Action", theme.ViewRefreshIcon(), func() { calls++ })
			var control fyne.Widget = b
			var view fyne.CanvasObject = b
			switch role {
			case "github":
				view = shell.GitHubButton(b)
			case "compact":
				view = shell.CompactGitHubButton(b)
			case "confirm":
				view = shell.ConfirmButton(b)
			case "destructive":
				view = shell.DestructiveButton(b)
			case "quiet":
				view = shell.QuietButton(b)
			case "icon":
				b.SetText("")
				view = shell.QuietButton(b)
			case "small delete":
				styled := shell.SmallDeleteButton(b, b)
				view = styled.View
				styled.FadeTo(true)
				for _, anim := range driver.anims {
					anim.Tick(1)
				}
			case "extended":
				hover := shell.NewHoverButton("Action", theme.ViewRefreshIcon(), func() { calls++ })
				control, b = hover, &hover.Button
				view = shell.CompactGitHubHoverButton(hover)
			}
			w.SetContent(container.NewCenter(view))
			w.Resize(fyne.NewSize(420, 160))
			// The role was given after the button was built: paint it as that
			// role before measuring, as the first frame on screen does.
			b.Refresh()
			renderer := test.WidgetRenderer(control)
			background := renderer.Objects()[0].(*canvas.Rectangle)
			var text *canvas.Text
			var icon *canvas.Image
			renderedObjects(control, func(o fyne.CanvasObject) {
				if c, ok := o.(*canvas.Text); ok && c.Text == "Action" {
					text = c
				}
				if c, ok := o.(*canvas.Image); ok {
					icon = c
				}
			})
			size, pos, minimum := b.Size(), b.Position(), b.MinSize()
			viewMinimum, fill := view.MinSize(), background.FillColor
			var font float32
			if text != nil {
				font = text.TextSize
			}
			iconSize := icon.Size()
			driver.anims = nil
			b.Tapped(nil)
			if calls != 1 || len(driver.anims) != 1 {
				t.Fatalf("tap must act immediately and start one press animation: calls=%d animations=%d", calls, len(driver.anims))
			}
			anim := driver.anims[0]
			if anim.Duration != 600*time.Millisecond {
				t.Fatalf("button press takes %v, want 0.6s", anim.Duration)
			}
			anim.Tick(0.25)
			pressed := pressedBox(control)
			if pressed.Empty() || background.Visible() || icon.Visible() {
				t.Fatal("a pressed button should show one picture in place of its fill, icon and label")
			}
			// The picture reaches a pixel past the button on every side.
			middle := (pressed.Min.X + pressed.Max.X) / 2
			if d := float32(middle) - 1 - size.Width/2; d < -1.5 || d > 1.5 {
				t.Fatalf("paint should compress around the button's centre, not %v from it", d)
			}
			if icon.Size() != iconSize || text != nil && text.TextSize != font || background.Size() != size {
				t.Fatal("the button's own fill, icon and label should keep their size behind the picture")
			}
			if b.Size() != size || b.Position() != pos || b.MinSize() != minimum || view.MinSize() != viewMinimum || background.FillColor != fill {
				t.Fatal("press feedback changed layout, hit bounds or role colour")
			}
			// Repeated layout/refresh while pressed must not compound the scale.
			renderer.Layout(size)
			b.Refresh()
			if pressedBox(control) != pressed {
				t.Fatalf("layout while pressed compounded the scale: %v then %v", pressed, pressedBox(control))
			}
			anim.Tick(0.65)
			if back := pressedBox(control); back.Dx() <= pressed.Dx() {
				t.Fatalf("button should smoothly return towards its normal size: %v then %v", pressed, back)
			}
			anim.Tick(1)
			if !pressedBox(control).Empty() || !background.Visible() || !icon.Visible() {
				t.Fatal("a button at rest should show its own fill, icon and label, not the picture")
			}
			if background.Size() != size || background.Position() != (fyne.Position{}) || icon.Size() != iconSize || text != nil && text.TextSize != font {
				t.Fatal("button did not restore its full background, icon and label")
			}
		})
	}
}

func TestButtonPressSupportsRepeatResizeKeyboardAndDisabled(t *testing.T) {
	a, driver := pressTestApp(t)
	w := a.NewWindow("button press lifecycle")
	defer w.Close()
	calls := 0
	b := widget.NewButton("Save", func() { calls++ })
	w.SetContent(b)
	w.Resize(fyne.NewSize(260, 90))
	renderer := test.WidgetRenderer(b)
	background := renderer.Objects()[0].(*canvas.Rectangle)
	b.Tapped(nil)
	anim := driver.anims[len(driver.anims)-1]
	anim.Tick(0.4)
	before := pressedBox(b)
	b.Tapped(nil)
	anim.Tick(0)
	if before.Empty() || pressedBox(b) != before || calls != 2 {
		t.Fatal("a rapid repeat should restart from the current painted size")
	}
	b.Resize(fyne.NewSize(320, 100))
	anim.Tick(1)
	if background.Size() != b.Size() || !background.Visible() || !pressedBox(b).Empty() {
		t.Fatal("a resize during the press must settle at the new size")
	}
	driver.anims = nil
	b.Disable()
	b.Tapped(nil)
	if calls != 2 || len(driver.anims) != 0 {
		t.Fatal("disabled buttons must not animate or act")
	}
	b.Enable()
	b.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	if calls != 3 || len(driver.anims) != 1 {
		t.Fatal("keyboard activation should use the same press feedback")
	}
	anim = driver.anims[0]
	anim.Tick(0.25)
	renderer.Destroy()
	before = pressedBox(b)
	anim.Tick(1)
	if before.Empty() || pressedBox(b) != before {
		t.Fatal("a destroyed renderer must ignore a stale animation frame")
	}
}

type pressAnimationSettings struct{ fyne.Settings }

func (pressAnimationSettings) ShowAnimations() bool { return false }

type pressReducedMotionApp struct{ fyne.App }

func (a pressReducedMotionApp) Settings() fyne.Settings {
	return pressAnimationSettings{a.App.Settings()}
}

func TestButtonPressHonoursReducedMotion(t *testing.T) {
	a, driver := pressTestApp(t)
	w := a.NewWindow("reduced motion")
	defer w.Close()
	calls := 0
	b := widget.NewButton("Save", func() { calls++ })
	w.SetContent(b)
	w.Resize(fyne.NewSize(260, 90))
	background := test.WidgetRenderer(b).Objects()[0].(*canvas.Rectangle)
	b.Tapped(nil)
	driver.anims[len(driver.anims)-1].Tick(0.25)
	fyne.SetCurrentApp(pressReducedMotionApp{a})
	driver.anims = nil
	b.Tapped(nil)
	if calls != 2 || len(driver.anims) != 0 || background.Size() != b.Size() || background.Position() != (fyne.Position{}) ||
		!background.Visible() || !pressedBox(b).Empty() {
		t.Fatal("reduced motion must reset a pending press and run the action without animation")
	}
}

func TestCustomActionTagsAndSubtabsUseTheSamePressFeedback(t *testing.T) {
	for _, kind := range []string{"tag", "parent", "subtab"} {
		t.Run(kind, func(t *testing.T) {
			a, driver := pressTestApp(t)
			w := a.NewWindow("custom button press")
			defer w.Close()
			calls := 0
			var button fyne.Widget
			var tap fyne.Tappable
			switch kind {
			case "subtab":
				tabs := shell.NewUnderlineTabs([]string{"Commits", "Meeting"}, func(string) { calls++ })
				tabs.Selected = "Commits"
				w.SetContent(container.NewCenter(tabs))
				button = test.WidgetRenderer(tabs).Objects()[2].(fyne.Widget)
				tap = button.(fyne.Tappable)
			case "parent":
				row := shell.ParentTag("Long parent issue title that must fit inside the Status sidebar", "https://github.com/org/repo/issues/42").(*fyne.Container)
				tag := row.Objects[0].(*shell.HoverTag)
				tag.OnTapped = func() { calls++ }
				button, tap = tag, tag
				w.SetContent(row)
			default:
				tag := shell.NewGoldTag("Show ignored", func() { calls++ })
				tag.Wide = true
				button, tap = tag, tag
				w.SetContent(container.NewCenter(tag))
			}
			w.Resize(fyne.NewSize(180, 160))
			renderer := test.WidgetRenderer(button)
			var label *canvas.Text
			var fill *canvas.Rectangle
			var picture *canvas.Image
			for _, o := range renderer.Objects() {
				switch object := o.(type) {
				case *canvas.Text:
					label = object
				case *canvas.Rectangle:
					fill = object
				case *canvas.Image:
					picture = object
				}
			}
			minimum, size, font := button.MinSize(), button.Size(), label.TextSize
			shown := label.Text
			if kind == "parent" && shown == button.(*shell.HoverTag).Text {
				t.Fatal("the parent title should be shortened to fit the sidebar")
			}
			drawn := func() float32 {
				if kind == "subtab" {
					return float32(pressedInk(button))
				}
				if picture != nil && picture.Visible() {
					return picture.Size().Width
				}
				return 0
			}
			driver.anims = nil
			tap.Tapped(nil)
			if calls != 1 || len(driver.anims) == 0 {
				t.Fatal("custom button should act immediately and animate")
			}
			press := driver.anims[0]
			if press.Duration != 600*time.Millisecond {
				t.Fatalf("custom button press takes %v, want 0.6s", press.Duration)
			}
			press.Tick(0.25)
			pressed := drawn()
			if pressed <= 0 || button.MinSize() != minimum || button.Size() != size {
				t.Fatal("custom button's paint should shrink while layout stays fixed")
			}
			if label.Visible() || label.TextSize != font || label.Text != shown {
				t.Fatal("the press should scale the picture of the label at its original font size and fit")
			}
			if kind != "subtab" {
				if fill.Visible() || picture.Image == nil || pressed >= size.Width || picture.Position().X != (size.Width-pressed)/2 {
					t.Fatal("the tag should shrink the fill and label together around its centre")
				}
				img := picture.Image
				bounds := img.Bounds()
				hasInk := false
				for y := bounds.Min.Y; y < bounds.Max.Y && !hasInk; y++ {
					for x := bounds.Min.X; x < bounds.Max.X; x++ {
						if _, _, _, alpha := img.At(x, y).RGBA(); alpha>>8 > 200 {
							hasInk = true // the translucent fill alone cannot reach this alpha
							break
						}
					}
				}
				if !hasInk {
					t.Fatal("the pressed tag picture should contain the label ink")
				}
			}
			renderer.Layout(size)
			if drawn() != pressed {
				t.Fatal("relayout should preserve the pressed size")
			}
			press.Tick(0.95)
			if drawn() <= pressed {
				t.Fatal("custom button should grow back from its pressed size")
			}
			press.Tick(1)
			if label.TextSize != font || !label.Visible() || button.MinSize() != minimum {
				t.Fatal("custom button should return to its original size")
			}
			if drawn() != 0 {
				t.Fatal("the press picture should disappear at rest")
			}
		})
	}
}

func TestLogWorkBackFadesHoverInAndOut(t *testing.T) {
	ui := editorUI(t)
	defer fyne.CurrentApp().Quit()
	fyne.CurrentApp().Settings().SetTheme(shell.GoldTheme{Theme: theme.DefaultTheme()})
	logwork.OpenGroupEditor(ui, backend.Group{Date: shell.Today()})
	back := waitForButton(t, ui.Win, "Back")
	driver := collectMotion(t)
	back.Refresh()
	fill := test.WidgetRenderer(back).Objects()[0].(*canvas.Rectangle)
	rest := fill.FillColor
	back.MouseIn(&desktop.MouseEvent{})
	if len(driver.anims) != 1 || driver.anims[0].Duration != 600*time.Millisecond {
		t.Fatal("Back should fade in over 600 ms")
	}
	driver.anims[0].Tick(1)
	hover := fill.FillColor
	back.MouseOut()
	if sameColor(hover, rest) || len(driver.anims) != 2 || driver.anims[1].Duration != 600*time.Millisecond {
		t.Fatal("Back should fade out over 600 ms")
	}
	driver.anims[1].Tick(.5)
	if sameColor(fill.FillColor, hover) || sameColor(fill.FillColor, rest) {
		t.Fatal("Back should remain partly highlighted halfway through its fade out")
	}
	driver.anims[1].Tick(1)
	if !sameColor(fill.FillColor, rest) {
		t.Fatal("Back should return to its resting fill")
	}
}

// A click is felt as the pointer goes down: the button shrinks and stays shrunk
// while it is held, and springs back when it is let go. The tap that follows
// the release acts, and does not play the press a second time.
func TestButtonShrinksUnderThePointerAndReturnsOnRelease(t *testing.T) {
	a, driver := pressTestApp(t)
	w := a.NewWindow("pointer down")
	defer w.Close()
	calls := 0
	b := widget.NewButton("Save", func() { calls++ })
	w.SetContent(container.NewCenter(b))
	w.Resize(fyne.NewSize(260, 90))
	primary := &desktop.MouseEvent{Button: desktop.MouseButtonPrimary}

	driver.anims = nil
	b.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonSecondary})
	if len(driver.anims) != 0 || !pressedBox(b).Empty() {
		t.Fatal("only the main mouse button presses a button")
	}
	b.MouseDown(primary)
	if calls != 0 || len(driver.anims) != 1 || driver.anims[0].Duration != 150*time.Millisecond {
		t.Fatalf("pointer down should start the 0.15s shrink and nothing else: calls=%d animations=%d", calls, len(driver.anims))
	}
	driver.anims[0].Tick(1)
	held := pressedBox(b)
	if held.Empty() {
		t.Fatal("the button should stay shrunk for as long as the pointer is held down")
	}
	b.MouseUp(primary)
	if len(driver.anims) != 2 || driver.anims[1].Duration != 450*time.Millisecond {
		t.Fatalf("letting go should play the 0.45s return: %d animations", len(driver.anims))
	}
	b.Tapped(nil)
	if calls != 1 || len(driver.anims) != 2 {
		t.Fatalf("the tap after a release should act without pressing again: calls=%d animations=%d", calls, len(driver.anims))
	}
	back := driver.anims[1]
	back.Tick(0.5)
	if box := pressedBox(b); box.Dx() <= held.Dx() {
		t.Fatalf("the button should be springing back: %v then %v", held, box)
	}
	back.Tick(1)
	if !pressedBox(b).Empty() {
		t.Fatal("the button should end at rest")
	}

	// Let go somewhere else, the window tells the button, which springs back
	// without acting.
	driver.anims = nil
	b.MouseDown(primary)
	driver.anims[0].Tick(1)
	b.PressReleased()
	if calls != 1 || len(driver.anims) != 2 {
		t.Fatal("a pointer let go off the button should release it without a tap")
	}
	driver.anims[1].Tick(1)
	if !pressedBox(b).Empty() {
		t.Fatal("a button released off itself should end at rest")
	}

	// A tap with no pointer down before it, as from the keyboard, still plays
	// the whole press.
	driver.anims = nil
	time.Sleep(160 * time.Millisecond) // past the moment a release's own tap arrives in
	b.Tapped(nil)
	if calls != 2 || len(driver.anims) != 1 || driver.anims[0].Duration != 600*time.Millisecond {
		t.Fatal("a tap on its own should play the whole 0.6s press")
	}
	driver.anims[0].Tick(1)
}

// Subtabs and action tags take the pointer the same way.
func TestSubtabShrinksUnderThePointerAndReturnsOnRelease(t *testing.T) {
	a, driver := pressTestApp(t)
	w := a.NewWindow("pointer down on a subtab")
	defer w.Close()
	var picked []string
	tabs := shell.NewUnderlineTabs([]string{"Commits", "Meeting"}, func(s string) { picked = append(picked, s) })
	tabs.Selected = "Commits"
	w.SetContent(container.NewCenter(tabs))
	w.Resize(fyne.NewSize(420, 160))
	tab := test.WidgetRenderer(tabs).Objects()[2].(fyne.Widget)
	mouse := tab.(desktop.Mouseable)
	primary := &desktop.MouseEvent{Button: desktop.MouseButtonPrimary}

	driver.anims = nil
	mouse.MouseDown(primary)
	if len(picked) != 0 || len(driver.anims) != 1 || driver.anims[0].Duration != 150*time.Millisecond {
		t.Fatalf("pointer down should start the 0.15s shrink and pick nothing: %v, %d animations", picked, len(driver.anims))
	}
	driver.anims[0].Tick(1)
	held := pressedInk(tab)
	if held == 0 {
		t.Fatal("the subtab should stay shrunk while the pointer is held down")
	}
	mouse.MouseUp(primary)
	started := len(driver.anims)
	back := driver.anims[started-1]
	if back.Duration != 450*time.Millisecond {
		t.Fatalf("letting go should play the 0.45s return, not %v", back.Duration)
	}
	tab.(fyne.Tappable).Tapped(nil)
	if len(picked) != 1 || picked[0] != "Meeting" {
		t.Fatalf("the tap after the release should pick the subtab: %v", picked)
	}
	for _, anim := range driver.anims[started:] {
		if anim.Duration == 600*time.Millisecond {
			t.Fatal("the tap after a release should not press the subtab again")
		}
	}
	back.Tick(0.5)
	if now := pressedInk(tab); now <= held {
		t.Fatalf("the subtab should be springing back: %d px then %d px", held, now)
	}
	back.Tick(1)
	if pressedInk(tab) != 0 {
		t.Fatal("the subtab should end at rest")
	}
}

// A popup's button clicked with the mouse has shrunk before the click is let
// go, so the popup does not wait to show a press: it sinks and fades at once,
// as it always did, and the button springs back on its way down.
func TestPopupClosedByAClickGoesAtOnce(t *testing.T) {
	a, driver := pressTestApp(t)
	w := a.NewWindow("popup closed by a click")
	defer w.Close()
	shell.FitScreen(w, widget.NewLabel("behind"))
	w.Resize(fyne.NewSize(900, 700))
	// See TestPopupDismissButtonsFinishTheirPressFeedback: FitScreen's timer.
	t.Cleanup(func() { time.Sleep(120 * time.Millisecond) })
	layer := w.Content().(*fyne.Container).Objects[1].(*fyne.Container)
	var d dialog.Dialog
	button := widget.NewButton("Close", func() { shell.CloseDialog(d, w) })
	d = dialog.NewCustomWithoutButtons("Example", shell.DialogButtons(button), w)
	shell.ShowDialog(d, w)
	for _, animation := range driver.anims {
		animation.Tick(1)
	}
	primary := &desktop.MouseEvent{Button: desktop.MouseButtonPrimary}
	driver.anims = nil
	button.MouseDown(primary)
	driver.anims[0].Tick(1) // held, fully shrunk
	button.MouseUp(primary)
	button.Tapped(nil)
	if w.Canvas().Overlays().Top() != nil {
		t.Fatal("the click should close the popup")
	}
	closing := driver.anims[len(driver.anims)-1]
	if closing.Duration != 600*time.Millisecond {
		t.Fatalf("a popup closed by a click takes %v, want its usual 0.34s with nothing held first", closing.Duration)
	}
	var press *canvas.Image
	for _, o := range layer.Objects {
		if image, ok := o.(*canvas.Image); ok {
			press = image // the button is placed after the popup's picture
		}
	}
	if press == nil {
		t.Fatal("the clicked button has no picture of its own in the closing popup")
	}
	start, startPos := press.Size(), press.Position()
	closing.Tick(0.5)
	if press.Size().Width <= start.Width || press.Translucency == 0 || press.Position().Y <= startPos.Y {
		t.Fatalf("the button should spring back while it sinks and fades with the popup: %v then %v", start, press.Size())
	}
	closing.Tick(1)
	if len(layer.Objects) != 0 {
		t.Fatal("the closing picture should be gone at the end")
	}
}

func TestMenuPillsAndSettingsGearFadeInAndOut(t *testing.T) {
	a, driver := pressTestApp(t)
	w := a.NewWindow("menu hover")
	defer w.Close()
	tabs := shell.NewPillTabs(
		container.NewTabItem(shell.LogListTabName, widget.NewLabel("log list")),
		container.NewTabItem(shell.KanbanTabName, widget.NewLabel("kanban")),
		container.NewTabItemWithIcon(shell.SettingsTabName, theme.SettingsIcon(), widget.NewLabel("settings")),
	)
	w.SetContent(tabs)
	w.Resize(fyne.NewSize(600, 200))
	var controls []fyne.Widget
	walkRendered(tabs, func(o fyne.CanvasObject) {
		if button, ok := o.(interface {
			fyne.Widget
			SetIcon(fyne.Resource)
		}); ok {
			controls = append(controls, button)
		}
	})
	if len(controls) != len(tabs.Items) {
		t.Fatal("menu pills or Settings gear missing")
	}
	// The tabs, like the Projects list, share one gold pill that fades in on
	// the tab under the pointer, glides from tab to tab and fades out.
	pillFill := widgets.WithAlpha(widgets.LogoGold, 0.16)
	pill := func() *canvas.Rectangle {
		var found *canvas.Rectangle
		walkRendered(tabs, func(o fyne.CanvasObject) {
			if r, ok := o.(*canvas.Rectangle); ok && r.StrokeWidth == 1 && r.CornerRadius > 0 {
				if _, _, _, alpha := r.FillColor.RGBA(); alpha > 0 && found == nil {
					found = r
				}
			}
		})
		return found
	}
	centre := func(o fyne.CanvasObject) fyne.Position {
		return a.Driver().AbsolutePositionForObject(o).Add(fyne.NewPos(o.Size().Width/2, o.Size().Height/2))
	}
	last := func(name string) *fyne.Animation {
		t.Helper()
		if len(driver.anims) == 0 {
			t.Fatalf("%s: nothing animated", name)
		}
		anim := driver.anims[len(driver.anims)-1]
		if anim.Duration != 600*time.Millisecond {
			t.Fatalf("%s takes %v, want 600 ms", name, anim.Duration)
		}
		return anim
	}
	logList, kanban := tabs.Views[0], tabs.Views[1]
	ownFill := test.WidgetRenderer(controls[0]).Objects()[0].(*canvas.Rectangle)
	rest := ownFill.FillColor
	driver.anims = nil
	test.MoveMouse(w.Canvas(), centre(logList))
	enter := last("the pill's fade-in")
	enter.Tick(0.5)
	if p := pill(); p == nil || sameColor(p.FillColor, pillFill) {
		t.Fatal("the pill should be part way faded in")
	}
	enter.Tick(1)
	p := pill()
	if p == nil || !sameColor(p.FillColor, pillFill) {
		t.Fatal("the gold pill should light the tab under the pointer")
	}
	if !sameColor(ownFill.FillColor, rest) {
		t.Fatal("the tab's own hover wash should be off: the pill is the hover")
	}
	at := func(o fyne.CanvasObject) bool {
		return a.Driver().AbsolutePositionForObject(p) == a.Driver().AbsolutePositionForObject(o) && p.Size() == o.Size()
	}
	if !at(logList) {
		t.Fatal("the pill should stand on Log List")
	}
	driver.anims = nil
	test.MoveMouse(w.Canvas(), centre(kanban))
	glide := last("the glide to Kanban")
	glide.Tick(0.5)
	if x := a.Driver().AbsolutePositionForObject(p).X; x <= a.Driver().AbsolutePositionForObject(logList).X || x >= a.Driver().AbsolutePositionForObject(kanban).X {
		t.Fatal("the pill should glide from Log List towards Kanban")
	}
	glide.Tick(1)
	if !at(kanban) || !sameColor(p.FillColor, pillFill) {
		t.Fatal("the pill should settle on Kanban, still lit")
	}
	// The gear takes the same hover.
	gear := tabs.Views[2]
	driver.anims = nil
	test.MoveMouse(w.Canvas(), centre(gear))
	last("the glide to the gear").Tick(1)
	if !at(gear) {
		t.Fatal("the pill should light the gear too")
	}
	driver.anims = nil
	test.MoveMouse(w.Canvas(), fyne.Position{})
	leave := last("the pill's fade-out")
	leave.Tick(1)
	if _, _, _, alpha := p.FillColor.RGBA(); alpha != 0 {
		t.Fatal("the pill should fade out when the pointer leaves the bar")
	}
	checkFade := func(name string, control fyne.Widget, background *canvas.Rectangle) {
		t.Helper()
		rest := background.FillColor
		driver.anims = nil
		test.MoveMouse(w.Canvas(), centre(control))
		enter := last(name + " fade-in")
		enter.Tick(0.5)
		middle := background.FillColor
		enter.Tick(1)
		hover := background.FillColor
		if sameColor(middle, rest) || sameColor(middle, hover) {
			t.Fatalf("%q should blend its hover colour", name)
		}
		driver.anims = nil
		test.MoveMouse(w.Canvas(), fyne.Position{})
		last(name + " fade-out").Tick(1)
		if !sameColor(background.FillColor, rest) {
			t.Fatalf("%q should return to its resting colour", name)
		}
	}
	driver.anims = nil
	tabs.ShowUpdate(true)
	for _, anim := range driver.anims {
		anim.Tick(1)
	}
	var badge *canvas.Rectangle
	walkRendered(tabs, func(o fyne.CanvasObject) {
		if r, ok := o.(*canvas.Rectangle); ok && sameColor(r.FillColor, shell.TagRestFill) {
			badge = r
		}
	})
	if badge == nil {
		t.Fatal("Update Available capsule is missing")
	}
	checkFade("Settings with an update", controls[len(controls)-1], badge)
}

// The gear stands under the menu bar's own theme, where the accent is gold. Its
// pressed picture is drawn apart from the window, and once took the accent from
// the app's theme instead: the open Settings gear flashed Fyne's blue.
func TestPressedGearKeepsTheMenuBarsGold(t *testing.T) {
	a, driver := pressTestApp(t)
	w := a.NewWindow("gear")
	defer w.Close()
	tabs := shell.NewPillTabs(
		container.NewTabItemWithIcon("Log Work", theme.DocumentCreateIcon(), widget.NewLabel("one")),
		container.NewTabItemWithIcon(shell.SettingsTabName, theme.SettingsIcon(), widget.NewLabel("two")),
	)
	w.SetContent(tabs)
	w.Resize(fyne.NewSize(420, 120))
	tabs.SelectName(shell.SettingsTabName) // the open tab's gear wears the accent
	for _, animation := range driver.anims {
		animation.Tick(1)
	}
	driver.anims = nil
	gear := tabs.Buttons[1]
	gear.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	if len(driver.anims) == 0 {
		t.Fatal("pointer down should press the gear")
	}
	driver.anims[len(driver.anims)-1].Tick(1)
	gold, blue := 0, 0
	renderedObjects(tabs, func(o fyne.CanvasObject) {
		picture, ok := o.(*canvas.Raster)
		if !ok || !picture.Visible() {
			return
		}
		width, height := int(picture.Size().Width), int(picture.Size().Height)
		img := picture.Generator(width, height)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				r, g, b, alpha := img.At(x, y).RGBA()
				if alpha>>8 < 200 {
					continue
				}
				switch {
				case b > r+0x3000 && b > g:
					blue++
				case r > 0xa000 && g > 0x7000 && b < 0x6000:
					gold++
				}
			}
		}
	})
	if gold == 0 || blue != 0 {
		t.Fatalf("the pressed gear should be the menu bar's gold: %d gold pixels, %d blue", gold, blue)
	}
}

// pressedBox is what is drawn in the picture a pressed button or subtab shows
// in place of its own objects, in the picture's pixels; empty at rest.
func pressedBox(control fyne.Widget) image.Rectangle {
	box := image.Rectangle{}
	for _, o := range test.WidgetRenderer(control).Objects() {
		picture, ok := o.(*canvas.Raster)
		if !ok || !picture.Visible() {
			continue
		}
		w, h := int(picture.Size().Width), int(picture.Size().Height)
		img := picture.Generator(w, h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				// Faint enough to catch the press flash on a button with no fill.
				if _, _, _, a := img.At(x, y).RGBA(); a>>8 > 8 {
					box = box.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
	}
	return box
}

// pressedInk is how wide that is, in pixels; 0 at rest.
func pressedInk(control fyne.Widget) int { return pressedBox(control).Dx() }

// A subtab that redraws its page holds the next frame up. The press must wait
// for that frame and play from there, not turn up where the clock has got to.
func TestSubtabPressWaitsForALateFrame(t *testing.T) {
	a, driver := pressTestApp(t)
	w := a.NewWindow("late frame")
	defer w.Close()
	tabs := shell.NewUnderlineTabs([]string{"Commits", "Meeting"}, nil)
	tabs.Selected = "Commits"
	w.SetContent(container.NewCenter(tabs))
	w.Resize(fyne.NewSize(420, 160))
	tab := test.WidgetRenderer(tabs).Objects()[2].(fyne.Widget)
	driver.anims = nil
	tab.(fyne.Tappable).Tapped(nil)
	press := driver.anims[0]
	time.Sleep(80 * time.Millisecond) // the page being rebuilt
	started := len(driver.anims)
	press.Tick(0.25) // by the clock, already at its smallest
	held := pressedInk(tab)
	if held == 0 {
		t.Fatal("a late first frame should still begin the press")
	}
	if len(driver.anims) != started+1 {
		t.Fatalf("the rest of the press should be timed from the late frame: %d animations", len(driver.anims)-started)
	}
	rest := driver.anims[len(driver.anims)-1]
	if rest.Duration >= 600*time.Millisecond || rest.Duration < 550*time.Millisecond {
		t.Fatalf("the rest of the press takes %v, want just under 0.6s", rest.Duration)
	}
	press.Tick(1)
	if pressedInk(tab) != held {
		t.Fatal("the clock's own animation must not finish the press early")
	}
	rest.Tick(0.25)
	if smallest := pressedInk(tab); smallest >= held {
		t.Fatalf("a late first frame should only begin the press: %d px held, %d px at its smallest", held, smallest)
	}
	rest.Tick(1)
	if pressedInk(tab) != 0 {
		t.Fatal("the press should end with the subtab's own label back")
	}
}

// Use the actual push-result path, plus custom-dialog and direct-PopUp close
// actions. Their real callbacks must run immediately while the closing layer
// preserves visible press feedback; a subsequent popup must survive it.
func TestPopupDismissButtonsFinishTheirPressFeedback(t *testing.T) {
	for _, kind := range []string{"pushed", "custom", "extended", "popup"} {
		t.Run(kind, func(t *testing.T) {
			a, driver := pressTestApp(t)
			w := a.NewWindow("popup button feedback")
			defer w.Close()
			shell.FitScreen(w, widget.NewLabel("behind"))
			w.Resize(fyne.NewSize(900, 700))
			// FitScreen measures the page again a moment after a resize, from a
			// timer. Let that land here, not in the middle of the next test,
			// where it measures text while that test does too.
			t.Cleanup(func() { time.Sleep(120 * time.Millisecond) })
			layer := w.Content().(*fyne.Container).Objects[1].(*fyne.Container)
			actionRan := false
			var button *widget.Button
			var control fyne.CanvasObject
			if kind == "pushed" {
				ui := &shell.UI{Win: w, Store: backend.NewStore(tempDir(t)),
					Cfg: backend.Config{WorklogOwner: "test"}}
				oldPush := backend.PushEntry
				backend.PushEntry = func(backend.Config, string, string, string, int, string, string, string, string, string, ...func(backend.FetchProgress)) (backend.PushResult, error) {
					actionRan = true
					return backend.PushResult{URL: "https://github.com/exampleorg/app/issues/2"}, nil
				}
				defer func() { backend.PushEntry = oldPush }()
				rows, err := ui.Store.AppendRows([]backend.Row{{"date": "2026-08-10", "minutes": "60", "issue": "exampleorg/app#1", "mode": "issue", "remarks": "Example", "owner": "test"}})
				if err != nil {
					t.Fatal(err)
				}
				tile := logwork.RowTile(ui, rows[0], func() {})
				buttonNamed(tile, "Push").Tapped(nil)
				button = waitForButton(t, w, "OK")
				control = button
				if !actionRan {
					t.Fatal("fictional push did not run")
				}
			} else if kind == "popup" {
				var popup *widget.PopUp
				button = widget.NewButton("Close", func() { actionRan = true; shell.ClosePopUp(popup) })
				control = button
				popup = widget.NewModalPopUp(shell.DialogButtons(button), w.Canvas())
				popup.Show()
				shell.AnimateOpening(w.Canvas())
			} else {
				var d dialog.Dialog
				close := func() { actionRan = true; shell.CloseDialog(d, w) }
				button = widget.NewButton("Close", close)
				control = button
				if kind == "extended" {
					extended := shell.NewHoverButton("Close", nil, close)
					button, control = &extended.Button, extended
				}
				d = dialog.NewCustomWithoutButtons("Example", shell.DialogButtons(control), w)
				shell.ShowDialog(d, w)
			}
			// Settle the opening, then capture only the tap and close animations.
			for _, animation := range driver.anims {
				animation.Tick(1)
			}
			driver.anims = nil
			button.Tapped(nil)
			if !actionRan || w.Canvas().Overlays().Top() != nil {
				t.Fatal("dismissal must act and hide the actual popup immediately")
			}
			var press *canvas.Image
			for _, o := range layer.Objects {
				if image, ok := o.(*canvas.Image); ok {
					press = image // the button is placed after the popup's picture
				}
			}
			if press == nil || press.Size() != control.Size() {
				t.Fatalf("clicked button has no separate closing picture: %v", layer.Objects)
			}
			initial, initialPos := press.Size(), press.Position()
			closing := driver.anims[len(driver.anims)-1]
			// As long as a button takes to shrink, then the popup's own closing.
			if closing.Duration != 750*time.Millisecond {
				t.Fatalf("dismissal feedback takes %v, want 0.15s of press then the 0.6s closing", closing.Duration)
			}
			pictures := func() (solid, faded int) {
				for _, o := range layer.Objects {
					if image, ok := o.(*canvas.Image); ok {
						if image.Translucency == 0 {
							solid++
						} else {
							faded++
						}
					}
				}
				return solid, faded
			}
			closing.Tick(0.15) // inside the 0.15s of 0.75s the popup holds
			compressed := press.Size()
			if compressed.Width >= initial.Width || press.Position().X <= initialPos.X {
				t.Fatal("dismiss button should visibly compress before the popup goes")
			}
			if _, faded := pictures(); faded != 0 || press.Position().Y-(initial.Height-compressed.Height)/2 != initialPos.Y {
				t.Fatal("the popup should stand solid and still while its button shrinks")
			}
			closing.Tick(0.6)
			if press.Size().Width <= compressed.Width {
				t.Fatal("dismiss button should spring back as the popup goes")
			}
			if solid, _ := pictures(); solid != 0 || press.Position().Y <= initialPos.Y+10 {
				t.Fatal("the popup should then sink and fade as it always does, its button with it")
			}
			next := dialog.NewCustomWithoutButtons("Next", widget.NewLabel("Keep open"), w)
			shell.ShowDialog(next, w)
			closing.Tick(1)
			if len(layer.Objects) != 0 || w.Canvas().Overlays().Top() == nil {
				t.Fatal("closing feedback should clean up without dismissing the next popup")
			}
		})
	}
}
