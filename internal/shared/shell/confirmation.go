package shell

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// ShowConfirmation displays a decision with a fixed heading and action row.
// Callers supply DialogButtons or RemovalButtons and close it with ClosePopUp.
func (ui *UI) ShowConfirmation(title string, body fyne.CanvasObject, buttons *fyne.Container) *widget.PopUp {
	buttons.Layout = layout.NewCustomPaddedHBoxLayout(3 * theme.Padding())
	return ui.showConfirmation(title, body, buttons, fyne.Size{})
}

// ShowLongNotice gives multi-section notices a taller reading area, with the
// same frame and fixed footer as a confirmation. The body scrolls on its own.
func (ui *UI) ShowLongNotice(title string, body, footer fyne.CanvasObject) *widget.PopUp {
	window := ui.Win.Canvas().Size()
	size := fyne.NewSize(min(760, window.Width*0.96), min(840, window.Height*0.96))
	return ui.showConfirmation(title, body, footer, size)
}

// ShowEditorPopup uses the shared popup frame around a form with its own
// fields, actions and scrolling layout.
func (ui *UI) ShowEditorPopup(title string, back, actions, editor fyne.CanvasObject) *widget.PopUp {
	heading := container.NewBorder(nil, nil, container.NewCenter(back), container.NewCenter(actions), popupHeading(title))
	body := framedPopupPanel(FitPopup(editor), 0)
	return ui.showFramedPopup(popupFrame(heading, body, nil), ui.PopupSizeFor(editor))
}

// A zero size fits the message. Longer notices can supply a reading viewport.
func (ui *UI) showConfirmation(title string, body, footer fyne.CanvasObject, size fyne.Size) *widget.PopUp {
	scroll := container.NewScroll(body)
	content := popupFrame(popupHeading(title), framedPopupPanel(NoScrollShadow(scroll), theme.InnerPadding()), footer)
	if size.IsZero() {
		size = ui.RemovalPopupSize(content)
		// Wrapped labels know their height only after the body has its final width.
		content.Resize(size)
		needed := content.MinSize().Height + max(0, body.MinSize().Height-scroll.MinSize().Height)
		size.Height = min(max(size.Height, needed), ui.Win.Canvas().Size().Height*0.92)
	}
	return ui.showFramedPopup(content, size)
}

func popupHeading(title string) fyne.CanvasObject {
	heading := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	heading.SizeName = theme.SizeNameHeadingText
	heading.Truncation = fyne.TextTruncateEllipsis
	return container.NewThemeOverride(heading, goldTextTheme{Theme: theme.Current()})
}

func popupFrame(heading, body, footer fyne.CanvasObject) fyne.CanvasObject {
	surface := theme.Color(theme.ColorNameOverlayBackground)
	frame := canvas.NewRectangle(surface)
	frame.CornerRadius = theme.Size(theme.SizeNameDialogRadius)
	frame.StrokeWidth = 1
	frame.StrokeColor = BlendColor(surface, widgets.LogoGold, 0.35)

	inset := theme.InnerPadding()
	rule := canvas.NewRectangle(widgets.LogoGold)
	rule.SetMinSize(fyne.NewSize(0, 2))
	header := container.NewVBox(heading, container.New(layout.NewCustomPaddedLayout(0, 0, inset, inset), rule))

	center := container.New(layout.NewCustomPaddedLayout(inset, 2*inset, 0, 0), body)
	card := container.NewBorder(header, footer, nil, nil, center)
	return container.NewStack(frame,
		container.New(layout.NewCustomPaddedLayout(12, 12, 20, 20), card))
}

func framedPopupPanel(content fyne.CanvasObject, topInset float32) fyne.CanvasObject {
	panel := panelBackground()
	panel.StrokeWidth = 1
	panel.StrokeColor = BlendColor(panel.FillColor, theme.Color(theme.ColorNameForeground), 0.13)
	inset := theme.InnerPadding()
	return container.NewStack(panel,
		container.New(layout.NewCustomPaddedLayout(topInset, inset, inset, inset), content))
}

func (ui *UI) showFramedPopup(content fyne.CanvasObject, size fyne.Size) *widget.PopUp {
	win := ui.Win.Canvas()
	popup := widget.NewModalPopUp(content, win)
	popup.Resize(size)
	FinishDropdownClosings()
	popup.Show()
	AnimateOpening(win)
	return popup
}

func (ui *UI) RemovalPopupSize(content fyne.CanvasObject) fyne.Size {
	window := ui.Win.Canvas().Size()
	return fyne.NewSize(min(640, window.Width*0.92), min(max(180, content.MinSize().Height), window.Height*0.92))
}

// ConfirmRemoval asks before running a destructive action with a plain-text message.
func (ui *UI) ConfirmRemoval(title, message, action string, apply func()) {
	body := widget.NewLabel(message)
	body.Wrapping = fyne.TextWrapWord
	var popup *widget.PopUp
	cancel := widget.NewButton("Cancel", func() { ClosePopUp(popup) })
	confirm := widget.NewButton(action, func() {
		ClosePopUp(popup)
		apply()
	})
	popup = ui.ShowConfirmation(title, body, RemovalButtons(cancel, confirm))
}
