package main

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

func (ui *UI) orgColorPicker(org string, initial color.Color, onSaved func()) *dialog.ColorPickerDialog {
	picker := dialog.NewColorPicker("Project color", org, func(picked color.Color) {
		if picked == nil {
			return
		}
		r, g, b, _ := picked.RGBA()
		chosen := color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 255}
		if err := ui.saveOrgColor(org, chosen); err != nil {
			ui.errf(err)
			return
		}
		if onSaved != nil {
			onSaved()
		}
		ui.refreshColors()
	}, ui.win)
	picker.Advanced = true
	picker.Show()
	picker.SetColor(initial)
	return picker
}

func (ui *UI) saveOrgColor(org string, chosen color.NRGBA) error {
	cfg := ui.cfg
	cfg.OrgColors = map[string]string{}
	for key, value := range ui.cfg.OrgColors {
		cfg.OrgColors[key] = value
	}
	cfg.OrgColors[strings.ToLower(org)] = hexColor(chosen)
	if err := ui.store.SaveConfig(cfg); err != nil {
		return err
	}
	ui.cfg = cfg
	return nil
}

func (ui *UI) refreshColors() {
	if ui.calBox != nil {
		ui.drawCalendar()
	}
	if ui.repBox != nil {
		ui.drawReport()
	}
	if ui.pendingBox != nil && ui.pendingLoaded {
		ui.renderPending()
	}
	if ui.recentBox != nil {
		ui.drawRecent()
	}
	if ui.kanban != nil {
		ui.kanban.draw()
	}
}
