package shell

import (
	"image"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/software"

	"worklog/internal/shared/widgets"
)

// tabPress draws a tab's label and arrow while the tab is pressed. A button
// shrinks its fill, which is big enough to read as a press. A tab is only its
// label, and shrinking that by a few percent is about one pixel: drawn as text
// and an image, each lands on whole pixels on its own, so the label hops down
// while the arrow hops up. Here both are one picture, shrunk about the tab's
// centre by fractions of a pixel, and the live label and arrow hide meanwhile.
type tabPress struct {
	item   *underlineTab
	raster *canvas.Raster
	// Where the tab was in its window when it was pressed, and how many pixels
	// a point takes there; inset is where the picture starts inside the tab, on
	// a whole pixel so it is drawn as it was made. Finding a tab's place walks
	// the window, so it is done once a press, not on every frame.
	abs, inset fyne.Position
	scale      float32
	// The label and the arrow as the painter draws them, each on a plate the
	// size of the picture, and what they were made for.
	label, arrow *image.Alpha
	made         tabPlates
}

// tabPlates is everything the plates depend on.
type tabPlates struct {
	w, h           int
	pixels, size   float32
	text           string
	lx, ly, ax, ay int
	menu           bool
	arrowRes       string // the arrow as turned, so a turned arrow is drawn anew
}

func newTabPress(item *underlineTab) *tabPress {
	p := &tabPress{item: item, scale: 1}
	p.raster = canvas.NewRaster(p.draw)
	p.raster.Hide()
	return p
}

// locate notes where the tab is, as it is pressed.
func (p *tabPress) locate() {
	p.scale, p.abs = 1, fyne.Position{}
	if driver := fyne.CurrentApp().Driver(); driver != nil {
		if c := driver.CanvasForObject(p.item); c != nil {
			p.scale = c.Scale()
			p.abs = driver.AbsolutePositionForObject(p.item)
		}
	}
}

// place shows the picture over the tab while it is pressed, and hides it
// otherwise.
func (p *tabPress) place(size fyne.Size, pressed bool) {
	if !pressed {
		p.raster.Hide()
		return
	}
	up := func(v float32) float32 { return float32(math.Ceil(float64(v*p.scale-0.001))) / p.scale }
	p.inset = fyne.NewPos(up(p.abs.X)-p.abs.X, up(p.abs.Y)-p.abs.Y)
	whole := func(v float32) float32 { return float32(math.Floor(float64(v*p.scale+0.001))) / p.scale }
	p.raster.Move(p.inset)
	p.raster.Resize(fyne.NewSize(whole(size.Width-p.inset.X), whole(size.Height-p.inset.Y)))
	p.raster.Show()
	p.raster.Refresh()
}

// repaint draws the picture again, for a label that changes colour mid-press.
func (p *tabPress) repaint() {
	if p.raster.Visible() {
		p.raster.Refresh()
	}
}

// draw is the picture, w by h pixels: the plates shrunk about the tab's centre
// and inked as the label and the arrow are now.
func (p *tabPress) draw(w, h int) image.Image {
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	i, size := p.item, p.raster.Size()
	if w <= 0 || h <= 0 || size.Width <= 0 {
		return out
	}
	// A screen that draws two pixels a point doubles the canvas's own scale.
	pixels := p.scale
	if dense := float32(math.Round(float64(float32(w) / (size.Width * pixels)))); dense > 1 {
		pixels *= dense
	}
	p.makePlates(w, h, pixels)

	ink := color.NRGBAModel.Convert(i.text.Color).(color.NRGBA)
	gold := widgets.LogoGold
	gold.A = uint8(float64(gold.A) * (1 - i.arrow.Translucency))
	shrink := i.press.Scale()
	// The tab's centre, in the picture's pixels.
	cx := (i.Size().Width/2 - p.inset.X) * pixels
	cy := (i.Size().Height/2 - p.inset.Y) * pixels
	for y := 0; y < h; y++ {
		sy := cy + (float32(y)+0.5-cy)/shrink - 0.5
		row := out.Pix[y*out.Stride:]
		for x := 0; x < w; x++ {
			sx := cx + (float32(x)+0.5-cx)/shrink - 0.5
			paint, cover := ink, plateAt(p.label, sx, sy)
			if p.made.menu {
				if a := plateAt(p.arrow, sx, sy); a > cover {
					paint, cover = gold, a
				}
			}
			if cover <= 0 {
				continue
			}
			row[x*4], row[x*4+1], row[x*4+2] = paint.R, paint.G, paint.B
			row[x*4+3] = uint8(float32(paint.A)*cover + 0.5)
		}
	}
	return out
}

// makePlates draws the label and the arrow at rest, each where the painter
// puts it: on the whole pixel nearest its place in the window.
func (p *tabPress) makePlates(w, h int, pixels float32) {
	i := p.item
	origin := func(v float32) int { return int(math.Round(float64(v * pixels))) }
	left, top := origin(p.abs.X+p.inset.X), origin(p.abs.Y+p.inset.Y)
	want := tabPlates{
		w: w, h: h, pixels: pixels, size: i.text.TextSize, text: i.text.Text, menu: i.hasMenu(),
		arrowRes: i.arrow.Resource.Name(),
		lx:       origin(p.abs.X+i.labelAt.X) - left, ly: origin(p.abs.Y+i.labelAt.Y) - top,
		ax: origin(p.abs.X+i.arrowAt.X) - left, ay: origin(p.abs.Y+i.arrowAt.Y) - top,
	}
	if want == p.made && p.label != nil {
		return
	}
	p.made = want
	label := canvas.NewText(i.text.Text, color.White)
	label.TextSize, label.TextStyle = i.text.TextSize, i.text.TextStyle
	label.Resize(label.MinSize())
	// Room for what the painter adds below the line and to its right.
	p.label = plate(w, h, want.lx, want.ly, coverage(label, label.MinSize().AddWidthHeight(2, 2), pixels))
	p.arrow = nil
	if want.menu {
		arrow := canvas.NewImageFromResource(i.arrow.Resource)
		arrow.FillMode = canvas.ImageFillContain
		arrow.Resize(fyne.NewSquareSize(tabArrowSize))
		p.arrow = plate(w, h, want.ax, want.ay, coverage(arrow, arrow.Size(), pixels))
	}
}

// coverage is how much of each pixel o covers, drawn alone at pixels a point,
// at the top left of room of the given size.
func coverage(o fyne.CanvasObject, room fyne.Size, pixels float32) *image.Alpha {
	c := software.NewTransparentCanvas()
	c.SetPadded(false)
	c.SetScale(pixels)
	c.SetContent(container.NewWithoutLayout(o))
	c.Resize(room)
	shot := c.Capture()
	b := shot.Bounds()
	mask := image.NewAlpha(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			_, _, _, a := shot.At(b.Min.X+x, b.Min.Y+y).RGBA()
			mask.Pix[y*mask.Stride+x] = uint8(a >> 8)
		}
	}
	return mask
}

// plate is mask set down at x, y on an empty plate w by h.
func plate(w, h, x, y int, mask *image.Alpha) *image.Alpha {
	out := image.NewAlpha(image.Rect(0, 0, w, h))
	b := mask.Bounds()
	for my := 0; my < b.Dy(); my++ {
		oy := y + my
		if oy < 0 || oy >= h {
			continue
		}
		for mx := 0; mx < b.Dx(); mx++ {
			if ox := x + mx; ox >= 0 && ox < w {
				out.Pix[oy*out.Stride+ox] = mask.Pix[my*mask.Stride+mx]
			}
		}
	}
	return out
}

// plateAt is the plate's cover, 0 to 1, at a point between its pixels.
func plateAt(p *image.Alpha, x, y float32) float32 {
	if p == nil {
		return 0
	}
	x0, y0 := int(math.Floor(float64(x))), int(math.Floor(float64(y)))
	fx, fy := x-float32(x0), y-float32(y0)
	b := p.Bounds()
	at := func(px, py int) float32 {
		if px < 0 || py < 0 || px >= b.Dx() || py >= b.Dy() {
			return 0
		}
		return float32(p.Pix[py*p.Stride+px])
	}
	top := at(x0, y0)*(1-fx) + at(x0+1, y0)*fx
	bottom := at(x0, y0+1)*(1-fx) + at(x0+1, y0+1)*fx
	return (top*(1-fy) + bottom*fy) / 255
}
