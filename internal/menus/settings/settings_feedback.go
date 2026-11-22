package settings

import (
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// A canvas overlay leaves the page interactive; it is not a modal dialog.
type settingsToast struct {
	layer   *fyne.Container
	version int
}

func newSettingsToast() *settingsToast {
	return &settingsToast{layer: container.New(toastCornerLayout{})}
}

// toastFade is how long the toast takes to fade in and out: the same as a
// Delete icon fading with its card.
const toastFade = 320 * time.Millisecond

func (t *settingsToast) show(text string) {
	t.version++
	version := t.version
	// Green, the colour of a change that went through, and at the top where the
	// eye already is after pressing Save. It fades in and out rather than
	// popping, its fill and its text together.
	alpha := float32(0)
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 8
	ink := widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: color.White}
	side := theme.IconInlineSize()
	checkSpace := canvas.NewRectangle(color.Transparent)
	checkSpace.SetMinSize(fyne.NewSquareSize(side))
	short, long := canvas.NewLine(color.White), canvas.NewLine(color.White)
	check := container.NewWithoutLayout(checkSpace, short, long)
	var drawn float32
	paintCheck := func() {
		widgets.DrawCheckMark(short, long, fyne.Position{}, side, widgets.WithAlpha(color.White, alpha), drawn)
	}
	// Centred on the words beside it, not at the top of the row.
	body := container.NewHBox(container.NewCenter(check),
		widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	over := container.NewThemeOverride(body, shell.FadedTheme{Theme: ink, Alpha: &alpha})
	paint := func(a float32) {
		alpha = a
		bg.FillColor = widgets.WithAlpha(shell.GitHubGreen, a)
		bg.Refresh()
		over.Theme = shell.FadedTheme{Theme: ink, Alpha: &alpha}
		over.Refresh()
		paintCheck()
	}
	paint(0)
	t.layer.Objects = []fyne.CanvasObject{container.NewStack(bg, container.NewPadded(over))}
	t.layer.Refresh()
	t.fade(version, 0, 1, paint, nil)
	if fyne.CurrentApp().Settings().ShowAnimations() {
		anim := fyne.NewAnimation(widgets.TickTime, func(p float32) {
			if t.version == version {
				drawn = p
				paintCheck()
			}
		})
		anim.Curve = fyne.AnimationEaseInOut
		anim.Start()
	} else {
		drawn = 1
		paintCheck()
	}
	app := fyne.CurrentApp()
	time.AfterFunc(3*time.Second, func() {
		if fyne.CurrentApp() != app {
			return
		}
		fyne.Do(func() {
			if t.version != version {
				return
			}
			t.fade(version, 1, 0, paint, func() {
				t.layer.Objects = nil
				t.layer.Refresh()
			})
		})
	})
}

// fade runs the toast from one strength to another, then done. A newer toast
// takes over, so a stale fade stops drawing.
func (t *settingsToast) fade(version int, from, to float32, paint func(float32), done func()) {
	finish := func() {
		if t.version == version && done != nil {
			done()
		}
	}
	if !fyne.CurrentApp().Settings().ShowAnimations() {
		paint(to)
		finish()
		return
	}
	anim := fyne.NewAnimation(toastFade, func(p float32) {
		if t.version != version {
			return
		}
		paint(from + (to-from)*p)
		if p >= 1 {
			finish()
		}
	})
	anim.Curve = fyne.AnimationEaseInOut
	anim.Start()
}

type toastCornerLayout struct{}

func (toastCornerLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.Size{} }

func (toastCornerLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		s := o.MinSize()
		o.Resize(s)
		o.Move(fyne.NewPos(fyne.Max(0, (size.Width-s.Width)/2), 12))
	}
}

func OrgColorPicker(ui *shell.UI, org string, initial color.Color, onSaved func()) *dialog.ColorPickerDialog {
	picker := dialog.NewColorPicker("Project color", org, func(picked color.Color) {
		if picked == nil {
			return
		}
		r, g, b, _ := picked.RGBA()
		chosen := color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 255}
		if err := SaveOrgColor(ui, org, chosen); err != nil {
			ui.Errf(err)
			return
		}
		if onSaved != nil {
			onSaved()
		}
		refreshColors(ui)
	}, ui.Win)
	picker.Advanced = true
	picker.Show()
	picker.SetColor(initial)
	// Fyne builds the picker's buttons itself and paints Confirm its default
	// blue; give it the app's green instead.
	shell.RestyleStockDialog(ui.Win)
	styleOrgColorPicker(picker, ui.Win)
	shell.AnimateStockDialog(ui.Win, "Cancel", "Confirm")
	return picker
}

func SaveOrgColor(ui *shell.UI, org string, chosen color.NRGBA) error {
	cfg := ui.Cfg
	cfg.OrgColors = map[string]string{}
	for key, value := range ui.Cfg.OrgColors {
		cfg.OrgColors[key] = value
	}
	cfg.OrgColors[strings.ToLower(org)] = shell.HexColor(chosen)
	if err := ui.Store.SaveConfig(cfg); err != nil {
		return err
	}
	ui.Cfg = cfg
	return nil
}

func refreshColors(ui *shell.UI) {
	if ui.CalBox != nil {
		shell.Hooks.DrawCalendar(ui)
	}
	if ui.RepBox != nil {
		shell.Hooks.DrawReport(ui)
	}
	if ui.PendingBox != nil && ui.PendingLoaded {
		shell.Hooks.RenderPending(ui)
	}
	if ui.RecentBox != nil {
		shell.Hooks.DrawRecent(ui)
	}
	if ui.Kanban != nil {
		shell.Hooks.KanbanUIDraw(ui.Kanban)
	}
}
