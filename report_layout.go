package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func reportText(text string) *widget.Label {
	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapWord
	return label
}

func reportWarning(text string) fyne.CanvasObject {
	note := widget.NewRichText(&widget.TextSegment{Text: text, Style: widget.RichTextStyle{
		ColorName: theme.ColorNameWarning, SizeName: theme.SizeNameText,
	}})
	note.Wrapping = fyne.TextWrapWord
	return note
}

func reportMetric(value, label, note string) fyne.CanvasObject {
	amount := canvas.NewText(value, theme.Color(theme.ColorNameForeground))
	amount.TextSize = theme.Size(theme.SizeNameHeadingText)
	amount.TextStyle = fyne.TextStyle{Bold: true}
	// Match the labels' inset so the large figure shares their left edge.
	figure := container.New(layout.NewCustomPaddedLayout(0, 0, theme.InnerPadding(), theme.InnerPadding()), amount)
	return reportPanel(label, container.NewVBox(figure, reportText(note)))
}

func reportFact(label, value string) fyne.CanvasObject {
	amount := widget.NewLabelWithStyle(value, fyne.TextAlignTrailing, fyne.TextStyle{Bold: true, Monospace: true})
	return container.NewBorder(nil, nil, nil, amount, reportText(label))
}

func reportSection(title string, content ...fyne.CanvasObject) fyne.CanvasObject {
	return reportPanel(title, container.NewVBox(content...))
}

// Report panels need a visible surface and edge: Fyne's default dark cards
// blend into the window, leaving unrelated figures looking like one block.
func reportPanel(title string, content fyne.CanvasObject) fyne.CanvasObject {
	surface := color.NRGBA{R: 0xed, G: 0xf3, B: 0xef, A: 0xff}
	header := color.NRGBA{R: 0xda, G: 0xe7, B: 0xdf, A: 0xff}
	border := color.NRGBA{R: 0xa3, G: 0xbc, B: 0xae, A: 0xff}
	r, g, b, _ := theme.Color(theme.ColorNameBackground).RGBA()
	if r+g+b < 3*0x8000 {
		surface = color.NRGBA{R: 0x12, G: 0x24, B: 0x1d, A: 0xff}
		header = color.NRGBA{R: 0x1b, G: 0x32, B: 0x27, A: 0xff}
		border = color.NRGBA{R: 0x32, G: 0x50, B: 0x40, A: 0xff}
	}
	background := canvas.NewRectangle(surface)
	background.CornerRadius = 10
	background.StrokeColor = border
	background.StrokeWidth = 1
	band := canvas.NewRectangle(header)
	band.CornerRadius = 6
	heading := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	heading.Wrapping = fyne.TextWrapWord
	return container.NewStack(background, container.NewPadded(container.NewVBox(
		container.NewStack(band, heading), container.NewPadded(content))))
}
