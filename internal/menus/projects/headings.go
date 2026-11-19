package projects

import (
	"image/color"
	"strings"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// wrapHeading adds a second line only when this name exceeds its own width.
func wrapHeading(first *canvas.Text, full string) *fyne.Container {
	second := canvas.NewText("", first.Color)
	second.TextSize, second.TextStyle = first.TextSize, first.TextStyle
	return container.New(&twoLineHeading{full: full, lines: [2]*canvas.Text{first, second}}, first, second)
}

type twoLineHeading struct {
	full  string
	lines [2]*canvas.Text
	width float32
	// Space reserved for figures alongside the second line, when needed.
	tailWidth float32
}

func (l *twoLineHeading) MinSize([]fyne.CanvasObject) fyne.Size {
	t := l.lines[0]
	line := fyne.MeasureText("M", t.TextSize, t.TextStyle)
	height := line.Height
	if l.width > 0 && fyne.MeasureText(l.full, t.TextSize, t.TextStyle).Width > l.width {
		height = height*2 + 2
	}
	return fyne.NewSize(line.Width, height)
}

func (l *twoLineHeading) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	first := l.lines[0]
	l.width = size.Width
	full := strings.Join(strings.Fields(l.full), " ")
	parts := [2]string{full, ""}
	if fyne.MeasureText(full, first.TextSize, first.TextStyle).Width > size.Width {
		runes := []rune(full)
		end, word := 0, 0
		for i, r := range runes {
			if fyne.MeasureText(string(runes[:i+1]), first.TextSize, first.TextStyle).Width > size.Width {
				break
			}
			end = i + 1
			if unicode.IsSpace(r) {
				word = i
			}
		}
		if word > 0 && end < len(runes) && !unicode.IsSpace(runes[end]) {
			end = word
		}
		parts[0] = strings.TrimSpace(string(runes[:end]))
		parts[1] = shell.FitText(strings.TrimSpace(string(runes[end:])), first.TextSize, first.TextStyle, max(size.Width-l.tailWidth, 0))
	}
	y := float32(0)
	for i, line := range l.lines {
		if line.Text != parts[i] {
			line.Text = parts[i]
			line.Refresh()
		}
		line.Move(fyne.NewPos(0, y))
		line.Resize(line.MinSize())
		y += fyne.MeasureText("M", line.TextSize, line.TextStyle).Height + 2
	}
}

// Each entry stays on one row while its name and figures fit together.
func statusLegendEntry(p palette, c color.Color, slice Slice, total int) fyne.CanvasObject {
	name := caption(slice.Label, p.ink)
	name.TextSize = theme.TextSize()
	percent := boldText(share(slice.Count, total), widgets.LogoGold)
	percent.TextSize = theme.CaptionTextSize()
	figures := container.NewHBox(shell.CountBadge(slice.Count), container.NewCenter(percent))
	dot := canvas.NewRectangle(c)
	dot.CornerRadius = 3
	dot.SetMinSize(fyne.NewSquareSize(10))
	label := wrapHeading(name, slice.Label)
	return container.New(&statusLegendLayout{label: label, figures: figures, dot: dot}, dot, label, figures)
}

type statusLegendLayout struct {
	label   *fyne.Container
	figures fyne.CanvasObject
	dot     *canvas.Rectangle
	wrapped bool
}

func (l *statusLegendLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	line := l.label.Layout.(*twoLineHeading).lines[0]
	textHeight := fyne.MeasureText("M", line.TextSize, line.TextStyle).Height
	height := max(textHeight, l.figures.MinSize().Height)
	if l.wrapped {
		height += textHeight + 2
	}
	return fyne.NewSize(max(l.figures.MinSize().Width+22, 100), height+4)
}

func (l *statusLegendLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	width := max(size.Width-22, 1)
	name := l.label.Layout.(*twoLineHeading)
	line := name.lines[0]
	textSize := fyne.MeasureText(name.full, line.TextSize, line.TextStyle)
	figures := l.figures.MinSize()
	l.wrapped = textSize.Width+4+figures.Width > width
	if !l.wrapped {
		height := max(textSize.Height, figures.Height)
		name.tailWidth = 0
		l.label.Move(fyne.NewPos(18, 2+(height-textSize.Height)/2))
		l.label.Resize(fyne.NewSize(max(width-figures.Width-4, 1), textSize.Height))
		l.figures.Move(fyne.NewPos(22+textSize.Width, 2+(height-figures.Height)/2))
		l.dot.Move(fyne.NewPos(4, 2+(height-10)/2))
	} else {
		name.tailWidth = figures.Width + 4
		l.label.Move(fyne.NewPos(18, 2))
		l.label.Resize(fyne.NewSize(width, textSize.Height*2+2))
		x := float32(18)
		if name.lines[1].Text != "" {
			x += max(width-figures.Width, 0)
		}
		l.figures.Move(fyne.NewPos(x, 4+textSize.Height))
		l.dot.Move(fyne.NewPos(4, 2+(textSize.Height-10)/2))
	}
	l.figures.Resize(figures)
	l.dot.Resize(fyne.NewSquareSize(10))
}

// Measure each entry at the available width, then use its individual height.
// One long description must not give every other description a blank line.
type statusLegendColumn struct{}

func (*statusLegendColumn) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for _, o := range objects {
		if o.Visible() {
			m := o.MinSize()
			size.Width = max(size.Width, m.Width)
			size.Height += m.Height
		}
	}
	return size
}

func (l *statusLegendColumn) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		if o.Visible() {
			o.Resize(fyne.NewSize(size.Width, o.MinSize().Height))
		}
	}
	y := max((size.Height-l.MinSize(objects).Height)/2, 0)
	for _, o := range objects {
		if o.Visible() {
			height := o.MinSize().Height
			o.Move(fyne.NewPos(0, y))
			o.Resize(fyne.NewSize(size.Width, height))
			y += height
		}
	}
}
