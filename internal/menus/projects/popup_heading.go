package projects

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// Fyne's dialog sets its content this far in from its sides (half of it each
// side) and this far below its title line.
const (
	dialogPadWidth  = float32(32)
	dialogPadHeight = float32(16)
)

// popupTitleBand is a Projects popup's title: gold, on a lighter
// grey band across the top of the popup from edge to edge, with note (a
// sprint's dates, say) after it in grey. It goes at the top of the content of
// a dialog made with an empty title, and is drawn over the room the dialog
// keeps for that title, so it takes no room of its own.
func popupTitleBand(title, note string) fyne.CanvasObject {
	base := theme.Color(theme.ColorNameOverlayBackground)
	band := canvas.NewRectangle(shell.BlendColor(base, color.White, 0.10))
	radius := theme.Size(theme.SizeNameDialogRadius)
	band.TopLeftCornerRadius, band.TopRightCornerRadius = radius, radius
	text := canvas.NewText(title, widgets.LogoGold)
	text.TextSize = theme.TextSize() + 2
	text.TextStyle = fyne.TextStyle{Bold: true}
	objects := []fyne.CanvasObject{band, text}
	l := &titleBandLayout{band: band, text: text, full: text.Text}
	if note != "" {
		l.note = canvas.NewText(note, currentPalette().soft)
		l.note.TextSize = theme.TextSize()
		l.noteFull = note
		objects = append(objects, l.note)
	}
	return container.New(l, objects...)
}

type titleBandLayout struct {
	band           *canvas.Rectangle
	text, note     *canvas.Text
	full, noteFull string
}

// MinSize is no height at all: the band lies over the dialog's title line.
func (l *titleBandLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(fyne.MeasureText("…", l.text.TextSize, l.text.TextStyle).Width, 0)
}

func (l *titleBandLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	// The dialog's own (empty) title line, measured as the dialog measures it.
	line := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}).MinSize().Height
	top := -(line + dialogPadHeight)
	height := line + dialogPadHeight - theme.Padding()
	l.band.Move(fyne.NewPos(-dialogPadWidth/2, top))
	l.band.Resize(fyne.NewSize(size.Width+dialogPadWidth, height))

	room := size.Width
	gap := theme.Padding() * 3
	if l.note != nil {
		// The note gives way first, then the title.
		l.note.Text = l.noteFull
		noteWidth := fyne.MeasureText(l.noteFull, l.note.TextSize, l.note.TextStyle).Width
		titleWidth := fyne.MeasureText(l.full, l.text.TextSize, l.text.TextStyle).Width
		if titleWidth+gap+noteWidth > room {
			l.note.Text = shell.FitText(l.noteFull, l.note.TextSize, l.note.TextStyle, max(room-titleWidth-gap, 0))
			if l.note.Text == "…" {
				l.note.Text = ""
			}
		}
	}
	noteWidth := float32(0)
	if l.note != nil && l.note.Text != "" {
		noteWidth = l.note.MinSize().Width + gap
	}
	l.text.Text = shell.FitText(l.full, l.text.TextSize, l.text.TextStyle, max(room-noteWidth, 0))
	m := l.text.MinSize()
	l.text.Move(fyne.NewPos(0, top+(height-m.Height)/2))
	l.text.Resize(m)
	l.text.Refresh()
	if l.note != nil {
		n := l.note.MinSize()
		l.note.Move(fyne.NewPos(m.Width+gap, top+(height-n.Height)/2))
		l.note.Resize(n)
		l.note.Refresh()
	}
}

// popupGroupBand heads a block of a Projects popup (Done, Open…): a gold band
// the width of the popup, the name on it in the popup's own dark ink.
func popupGroupBand(title string) fyne.CanvasObject {
	band := canvas.NewRectangle(widgets.LogoGold)
	band.CornerRadius = 4
	text := canvas.NewText(titleCase(title), theme.Color(theme.ColorNameOverlayBackground))
	text.TextStyle = fyne.TextStyle{Bold: true}
	name := container.New(&fitHeading{text: text, full: text.Text}, text)
	return container.NewStack(band, container.New(layout.NewCustomPaddedLayout(theme.Padding(), theme.Padding(), theme.Padding()*2, theme.Padding()*2), name))
}

// assigneeRule is the gold line between one person's issues and the next's.
func assigneeRule() fyne.CanvasObject {
	rule := canvas.NewRectangle(widgets.WithAlpha(widgets.LogoGold, 0.7))
	rule.SetMinSize(fyne.NewSize(0, 1))
	return container.New(layout.NewCustomPaddedLayout(theme.Padding()*2, theme.Padding(), 0, 0), rule)
}
