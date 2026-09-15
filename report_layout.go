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

var (
	reportSupportSurface          = color.NRGBA{R: 0x0e, G: 0x29, B: 0x47, A: 0xff}
	reportSupportHeader           = color.NRGBA{R: 0x16, G: 0x3a, B: 0x63, A: 0xff}
	reportSupportBorder           = color.NRGBA{R: 0x2c, G: 0x5e, B: 0x91, A: 0xff}
	reportSupportConversionAccent = color.NRGBA{R: 0x68, G: 0xd3, B: 0x91, A: 0xff}
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
	return reportPanel(label, reportMetricContent(value, note, "", "", theme.Color(theme.ColorNameForeground), nil))
}

func reportMetricWithAside(value, label, note, aside, conversion string) fyne.CanvasObject {
	return reportPanel(label, reportMetricContent(value, note, aside, conversion,
		theme.Color(theme.ColorNameForeground), reportConversionAccent()))
}

func reportMetricContent(value, note, aside, conversion string, amountColor, conversionColor color.Color) fyne.CanvasObject {
	amount := canvas.NewText(value, amountColor)
	amount.TextSize = theme.Size(theme.SizeNameHeadingText)
	amount.TextStyle = fyne.TextStyle{Bold: true}
	// Match the labels' inset so the large figure shares their left edge.
	var figure fyne.CanvasObject = container.New(
		layout.NewCustomPaddedLayout(0, 0, theme.InnerPadding(), theme.InnerPadding()), amount)
	if aside != "" {
		bonus := canvas.NewText(aside, reportSupportAccent())
		bonus.TextSize = theme.Size(theme.SizeNameCaptionText)
		bonus.TextStyle = fyne.TextStyle{Bold: true}
		bonus.Alignment = fyne.TextAlignTrailing
		figure = container.NewBorder(nil, nil, nil, container.NewCenter(bonus), figure)
	}
	content := []fyne.CanvasObject{figure}
	if conversion != "" {
		converted := canvas.NewText(conversion, conversionColor)
		converted.TextSize = theme.Size(theme.SizeNameSubHeadingText)
		converted.TextStyle = fyne.TextStyle{Bold: true}
		converted.Alignment = fyne.TextAlignTrailing
		content = append(content, container.NewBorder(nil, nil, nil,
			container.NewCenter(converted), reportText(note)))
	} else {
		content = append(content, reportText(note))
	}
	return container.NewVBox(content...)
}

// reportMetricTinted is reportMetric on a panel painted with the given tint —
// used to mark the projected receivable in draft yellow, so the "once every
// draft is pushed" figure reads as provisional beside the settled one.
func reportMetricTinted(value, label, note string, surface, header, border color.NRGBA) fyne.CanvasObject {
	return reportPanelTinted(label,
		reportMetricContent(value, note, "", "", theme.Color(theme.ColorNameForeground), nil), surface, header, border)
}

func reportMetricTintedWithAside(value, label, note, aside, conversion string, surface, header, border color.NRGBA) fyne.CanvasObject {
	return reportPanelTinted(label,
		reportMetricContent(value, note, aside, conversion,
			theme.Color(theme.ColorNameForeground), reportConversionAccent()), surface, header, border)
}

// reportMetricDarkBlue keeps the support bonus visually distinct from both
// ordinary payable work and the yellow draft projection.
func reportMetricDarkBlue(value, label, note, conversion string) fyne.CanvasObject {
	panel := reportPanelTinted(label,
		reportMetricContent(value, note, "", conversion, color.White, reportSupportConversionAccent),
		reportSupportSurface, reportSupportHeader, reportSupportBorder)
	return container.NewThemeOverride(panel, darkBlueReportTheme{Theme: theme.Current()})
}

func reportSupportAccent() color.NRGBA {
	r, g, b, _ := theme.Color(theme.ColorNameBackground).RGBA()
	if r+g+b < 3*0x8000 {
		return color.NRGBA{R: 0x64, G: 0xb5, B: 0xf6, A: 0xff}
	}
	return color.NRGBA{R: 0x15, G: 0x65, B: 0xc0, A: 0xff}
}

func reportConversionAccent() color.NRGBA {
	r, g, b, _ := theme.Color(theme.ColorNameBackground).RGBA()
	if r+g+b < 3*0x8000 {
		return color.NRGBA{R: 0x68, G: 0xd3, B: 0x91, A: 0xff}
	}
	return color.NRGBA{R: 0x16, G: 0x83, B: 0x4f, A: 0xff}
}

type darkBlueReportTheme struct{ fyne.Theme }

func (t darkBlueReportTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameForeground, theme.ColorNameForegroundOnPrimary:
		return color.White
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 0xd5, G: 0xe4, B: 0xf3, A: 0xff}
	}
	return t.Theme.Color(name, variant)
}

func reportFact(label, value string) fyne.CanvasObject {
	amount := widget.NewLabelWithStyle(value, fyne.TextAlignTrailing, fyne.TextStyle{Bold: true, Monospace: true})
	return container.NewBorder(nil, nil, nil, amount, reportText(label))
}

// reportFactColored is reportFact with the value drawn in a specific colour —
// used to highlight the converted receivable in gold, so a glance at the panel
// picks up the display-currency total without reading each row.
func reportFactColored(label, value string, c color.Color) fyne.CanvasObject {
	text := canvas.NewText(value, c)
	text.TextSize = theme.TextSize()
	text.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	text.Alignment = fyne.TextAlignTrailing
	return container.NewBorder(nil, nil, nil, container.NewHBox(layout.NewSpacer(), text), reportText(label))
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
	return reportPanelTinted(title, content, surface, header, border)
}

// reportPanelTinted is reportPanel painted with the three explicit tones — the
// projected-receivable card uses the draft yellow so it reads as provisional
// against the settled green panels beside it.
func reportPanelTinted(title string, content fyne.CanvasObject, surface, header, border color.NRGBA) fyne.CanvasObject {
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

// draftPanelTones is the yellow tinting used for the projected receivable
// card — surface pale, header a touch stronger, border strong enough to read
// on both light and dark backgrounds.
func draftPanelTones() (surface, header, border color.NRGBA) {
	r, g, b, _ := theme.Color(theme.ColorNameBackground).RGBA()
	if r+g+b < 3*0x8000 {
		return color.NRGBA{R: 0x2a, G: 0x24, B: 0x0f, A: 0xff},
			color.NRGBA{R: 0x3d, G: 0x33, B: 0x12, A: 0xff},
			color.NRGBA{R: 0x8a, G: 0x6f, B: 0x2a, A: 0xff}
	}
	return color.NRGBA{R: 0xff, G: 0xf6, B: 0xd6, A: 0xff},
		color.NRGBA{R: 0xff, G: 0xe8, B: 0xa0, A: 0xff},
		color.NRGBA{R: 0xc9, G: 0xa5, B: 0x3a, A: 0xff}
}
