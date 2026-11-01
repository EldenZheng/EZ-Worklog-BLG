package settings

import (
	"image/color"
	"strings"
	"time"
	"worklog/internal/shared/shell"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// A canvas overlay leaves the page interactive; it is not a modal dialog.
type settingsToast struct {
	layer   *fyne.Container
	version int
}

func newSettingsToast() *settingsToast {
	return &settingsToast{layer: container.New(toastCornerLayout{})}
}

func (t *settingsToast) show(text string) {
	t.version++
	version := t.version
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameOverlayBackground))
	bg.CornerRadius = 8
	bg.StrokeWidth = 1
	bg.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	t.layer.Objects = []fyne.CanvasObject{container.NewStack(bg, container.NewPadded(container.NewHBox(widget.NewIcon(theme.ConfirmIcon()), widget.NewLabel(text))))}
	t.layer.Refresh()
	app := fyne.CurrentApp()
	time.AfterFunc(3*time.Second, func() {
		if fyne.CurrentApp() != app {
			return
		}
		fyne.Do(func() {
			if t.version == version {
				t.layer.Objects = nil
				t.layer.Refresh()
			}
		})
	})
}

type toastCornerLayout struct{}

func (toastCornerLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.Size{} }

func (toastCornerLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		s := o.MinSize()
		o.Resize(s)
		o.Move(fyne.NewPos(12, fyne.Max(0, size.Height-s.Height-12)))
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
