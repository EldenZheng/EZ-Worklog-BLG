package settings

import (
	"image/color"
	"reflect"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// styleOrgColorPicker keeps Fyne's colour controls and selection callbacks,
// replacing only its individual hovers and its size-retaining accordion.
func styleOrgColorPicker(picker *dialog.ColorPickerDialog, win fyne.Window) {
	// Fyne's internal overlay exposes Content, as the shared stock-dialog
	// styling helpers read it. No private dialog state is changed.
	top := reflect.ValueOf(win.Canvas().Overlays().Top())
	if top.Kind() != reflect.Pointer || top.IsNil() {
		return
	}
	field := top.Elem().FieldByName("Content")
	if !field.IsValid() || !field.CanInterface() {
		return
	}
	popup, ok := field.Interface().(*widget.PopUp)
	if !ok {
		return
	}

	var body *fyne.Container
	var accordion *widget.Accordion
	var find func(fyne.CanvasObject)
	find = func(o fyne.CanvasObject) {
		if box, ok := o.(*fyne.Container); ok {
			for _, child := range box.Objects {
				if a, ok := child.(*widget.Accordion); ok && len(a.Items) == 1 {
					body, accordion = box, a
					return
				}
				find(child)
			}
		}
	}
	find(popup.Content)
	if body == nil {
		return
	}

	var rows []*shell.Tappable
	var wrapSwatches func(*fyne.Container)
	wrapSwatches = func(box *fyne.Container) {
		for i, child := range box.Objects {
			if swatch, ok := child.(stockColorSwatch); ok {
				// Paint the stock swatch without making it a nested hit target:
				// the app's row owns both the travelling hover and popup closing.
				view := &orgColorSwatch{source: swatch}
				view.ExtendBaseWidget(view)
				row := shell.NewTappable(container.NewPadded(view), func() {
					shell.ClosePopUp(popup)
					swatch.Tapped(nil)
				})
				box.Objects[i] = row
				rows = append(rows, row)
			} else if inner, ok := child.(*fyne.Container); ok {
				wrapSwatches(inner)
			}
		}
	}
	wrapSwatches(body)

	advanced := accordion.Items[0]
	// The stock accordion includes its full detail width even when shut.
	// Keep that width while Fold animates only the popup's height.
	fullWidth := popup.Content.MinSize().Width
	// Measuring the stock accordion can hide its detail again. Restore the
	// controls after that final measurement, before moving them into our Fold.
	advanced.Detail.Show()
	label := canvas.NewText(advanced.Title, widgets.LogoGold)
	label.TextStyle = fyne.TextStyle{Bold: true}
	label.TextSize = theme.TextSize()
	resize := func(final bool) {
		// Fold's live MinSize drives the dialog on every frame, just as the
		// ignored-commit fold drives its surrounding card through Reflow.
		need := popup.Content.MinSize()
		need.Width = max(need.Width, fullWidth)
		picker.Resize(need)
		size, window := popup.Size(), win.Canvas().Size()
		popup.Move(fyne.NewPos(max(0, (window.Width-size.Width)/2), max(0, (window.Height-size.Height)/2)))
		shell.Reflow(final, popup.Content, body)
	}
	head, area := shell.CollapseLine(label, nil, advanced.Detail, advanced.Open, 0,
		func(open bool) { advanced.Open = open }, resize)
	advancedRow := head.(*shell.Tappable)
	advancedRow.Content = container.NewPadded(advancedRow.Content)
	rows = append(rows, advancedRow)
	for i, child := range body.Objects {
		if child == accordion {
			body.Objects[i] = container.NewVBox(head, area)
			break
		}
	}
	// One overlay spans the palette, recents and Advanced. It leaves the
	// advanced sliders and text fields as their normal interactive controls.
	group := shell.HoverRows(container.NewVBox(body.Objects...), rows)
	body.Objects = []fyne.CanvasObject{group}
	body.Refresh()
	resize(true)
}

type stockColorSwatch interface {
	fyne.Widget
	fyne.Tappable
	SetColor(color.Color)
}

// orgColorSwatch delegates the stock colour/checkerboard drawing, without its
// stock hover and tap handlers. Selection still calls the original swatch.
type orgColorSwatch struct {
	widget.BaseWidget
	source stockColorSwatch
}

func (s *orgColorSwatch) CreateRenderer() fyne.WidgetRenderer {
	return s.source.CreateRenderer()
}
