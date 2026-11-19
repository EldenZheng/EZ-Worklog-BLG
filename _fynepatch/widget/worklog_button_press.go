package widget

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/internal/cache"
	"fyne.io/fyne/v2/internal/svg"
	"fyne.io/fyne/v2/theme"
)

// A short compression then a soft return. Only the painted objects move: the
// button's hit area, minimum size and its neighbours keep their normal geometry.
const worklogButtonPressDuration = 600 * time.Millisecond
const worklogButtonPressedScale = float32(0.94)
const worklogButtonCompression = float32(0.25)

const (
	// A frame that took longer than worklogPressStall came late. It moves the
	// press on by worklogPressFrame, one ordinary frame, and the rest is timed
	// from there. After worklogPressHolds late frames the press follows the
	// clock, so a page that stays slow cannot drag it out.
	worklogPressStall = 50 * time.Millisecond
	worklogPressFrame = time.Second / 60
	worklogPressHolds = 4
	// A tap reaches the button straight after the release that made it.
	worklogPressTap = 150 * time.Millisecond
)

// worklogPress is a button's press: how far through it is, and the picture
// that stands in for the button meanwhile.
//
// Fyne times an animation by the clock from its Start. A button that redraws
// the page it is on (a menu at the top of the window) holds up the very frames
// that would have shown it shrinking, so the press turned up part-way back.
// The clock here waits for those frames.
//
// Fyne also sets every object down on whole pixels. Shrinking the fill, the
// icon and the label a few percent each moved them a pixel at a time, each on
// its own, so the label and icon hopped about inside the fill. While pressed,
// the three are hidden and drawn as one picture, shrunk about the button's
// centre by fractions of a pixel.
type worklogPress struct {
	scale, from float32 // the painted scale (0 is 1), and what the press began at
	share       float32 // how far through the press, 0 to 1
	holds       int
	last        time.Time
	// rest plays what is left after a late frame; held says the button's own
	// animation has handed over to it.
	rest *fyne.Animation
	held bool
	// target is how far the press plays before it stops: as far as shrunk
	// while the pointer is down on the button, and to the end once it is let
	// go. upAt is when that was.
	target float32
	down   bool
	upAt   time.Time

	shot *canvas.Raster
	// Where the button was in its window when it was pressed, and how many
	// pixels a point takes there; inset is where the picture starts inside the
	// button. Finding a button's place walks the window, so it is done once a
	// press, not on every frame: a popup's button is pressed and gone, and
	// looking for it then would walk everything the window shows, each frame.
	abs, inset fyne.Position
	pixels     float32
	// top is the label and icon at rest, as the painter draws them, on a plate
	// the size of the picture; made is what it was drawn for.
	top  *image.RGBA
	made string
}

// tapAnimation plays a whole press: for the keyboard, and for a tap that came
// with no pointer going down first. A click has already shrunk the button under
// the pointer and its release is playing the return, so that tap adds nothing.
func (b *Button) tapAnimation() {
	if b.tapAnim == nil {
		return
	}
	r, ok := cache.Renderer(b.super()).(*buttonRenderer)
	if ok && b.isAnimating && r.press.target == 1 && time.Since(r.press.upAt) < worklogPressTap {
		return
	}
	b.tapAnim.Stop()
	if !ok {
		return
	}
	r.press.stopRest()
	r.press.down = false
	if !fyne.CurrentApp().Settings().ShowAnimations() {
		b.isAnimating = false
		r.press.scale = 1
		r.tapBG.FillColor = color.Transparent
		r.Layout(b.Size())
		canvas.Refresh(b.super())
		return
	}
	r.worklogPressBegin()
	r.press.target = 1
	b.tapAnim.Start()
}

// MouseDown shrinks the button as the pointer goes down on it, and holds it
// there: the press is felt at once, not after the click has been let go.
func (b *Button) MouseDown(ev *desktop.MouseEvent) {
	if ev == nil || ev.Button != desktop.MouseButtonPrimary || b.Disabled() || b.tapAnim == nil ||
		!fyne.CurrentApp().Settings().ShowAnimations() {
		return
	}
	r, ok := cache.Renderer(b.super()).(*buttonRenderer)
	if !ok {
		return
	}
	b.tapAnim.Stop()
	r.worklogPressBegin()
	r.press.down, r.press.held, r.press.target = true, true, worklogButtonCompression
	r.worklogPressRest()
}

// MouseUp lets the button spring back.
func (b *Button) MouseUp(*desktop.MouseEvent) { b.PressReleased() }

// PressReleased lets a held button spring back. The window calls it too when
// the pointer is let go somewhere else, which the button is not otherwise told.
func (b *Button) PressReleased() {
	r, ok := cache.Renderer(b.super()).(*buttonRenderer)
	if !ok || !r.press.down {
		return
	}
	p := &r.press
	p.down, p.upAt = false, time.Now()
	p.stopRest()
	p.holds, p.last, p.target = 0, p.upAt, 1
	r.worklogPressRest()
}

// WorklogPressScale is how far the button is painted shrunk now: 1 at rest.
func (b *Button) WorklogPressScale() float32 {
	if r, ok := cache.Renderer(b.super()).(*buttonRenderer); ok && r.press.scale != 0 {
		return r.press.scale
	}
	return 1
}

// worklogPressBegin readies a press that starts from the button as it is
// painted now.
func (r *buttonRenderer) worklogPressBegin() {
	p := &r.press
	p.stopRest()
	p.from = p.scale
	if p.from == 0 {
		p.from = 1
	}
	p.share, p.holds, p.held, p.last = 0, 0, false, time.Now()
	p.pixels, p.abs = 1, fyne.Position{}
	if driver := fyne.CurrentApp().Driver(); driver != nil {
		if c := driver.CanvasForObject(r.button.super()); c != nil {
			p.pixels = c.Scale()
			p.abs = driver.AbsolutePositionForObject(r.button.super())
		}
	}
	r.button.isAnimating = true
}

func newWorklogButtonPressAnimation(r *buttonRenderer) *fyne.Animation {
	r.press.shot = canvas.NewRaster(func(w, h int) image.Image { return r.worklogPressPicture(w, h) })
	r.press.shot.Hide()
	r.SetObjects(r.press.over(r.Objects()))
	animation := fyne.NewAnimation(worklogButtonPressDuration, func(p float32) {
		// A destroyed/replaced renderer must never repaint an old button.
		if !r.button.isAnimating || r.press.held {
			return
		}
		r.worklogPressFrame(0, p)
	})
	animation.Curve = fyne.AnimationLinear
	return animation
}

// over puts the press picture on top of a renderer's objects.
func (p *worklogPress) over(objects []fyne.CanvasObject) []fyne.CanvasObject {
	if p.shot == nil {
		return objects
	}
	return append(objects, p.shot)
}

func (p *worklogPress) stopRest() {
	if p.rest != nil {
		p.rest.Stop()
		p.rest = nil
	}
}

// worklogPressFrame is one frame of an animation playing the press from the
// share from as far as its target, done of the way along.
func (r *buttonRenderer) worklogPressFrame(from, done float32) {
	p := &r.press
	now := time.Now()
	late := now.Sub(p.last) > worklogPressStall
	p.last = now
	if late && p.holds < worklogPressHolds {
		p.holds++
		p.share = fyne.Min(p.target, p.share+float32(worklogPressFrame)/float32(worklogButtonPressDuration))
		p.held = true
		p.stopRest()
		if p.share < p.target {
			r.worklogPressRest()
		}
		r.worklogPressPaint(p.share)
		return
	}
	p.share = from + (p.target-from)*done
	if done >= 1 && p.target < 1 {
		p.rest = nil // held there until the pointer is let go
	}
	r.worklogPressPaint(p.share)
}

// worklogPressRest plays the press on from where it is to its target, timed
// from now.
func (r *buttonRenderer) worklogPressRest() {
	from := r.press.share
	if from >= r.press.target {
		r.worklogPressPaint(from)
		return
	}
	length := time.Duration(float64(worklogButtonPressDuration) * float64(r.press.target-from))
	var rest *fyne.Animation
	rest = fyne.NewAnimation(length, func(done float32) {
		if !r.button.isAnimating || r.press.rest != rest {
			return
		}
		r.worklogPressFrame(from, done)
	})
	rest.Curve = fyne.AnimationLinear
	r.press.rest = rest
	rest.Start()
}

// worklogPressPaint draws the button p of the way through its press.
func (r *buttonRenderer) worklogPressPaint(p float32) {
	if p <= worklogButtonCompression {
		t := p / worklogButtonCompression
		eased := 1 - (1-t)*(1-t)
		r.press.scale = r.press.from + (worklogButtonPressedScale-r.press.from)*eased
	} else {
		t := (p - worklogButtonCompression) / (1 - worklogButtonCompression)
		eased := 1 - (1-t)*(1-t)*(1-t)
		r.press.scale = worklogButtonPressedScale + (1-worklogButtonPressedScale)*eased
	}
	th := r.button.Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	ink := color.NRGBAModel.Convert(th.Color(theme.ColorNamePressed, v)).(color.NRGBA)
	ink.A = uint8(float32(ink.A) * (1 - p))
	r.tapBG.FillColor = ink
	if p >= 1 {
		r.press.scale = 1
		r.press.rest = nil
		r.button.isAnimating = false
		r.tapBG.FillColor = color.Transparent
	}
	r.Layout(r.button.Size())
	canvas.Refresh(r.button.super())
}

func (r *buttonRenderer) Layout(size fyne.Size) {
	// The stock layout passes over hidden objects, so the button's own come
	// back for it, pressed or not.
	r.worklogPressHide(false)
	r.background.Move(fyne.Position{})
	r.tapBG.Move(fyne.Position{})
	r.worklogUnscaledLayout(size)
	r.worklogPressPlace(size)
}

// worklogPressHide hides the button's own objects behind the press picture, or
// brings them back as the button keeps them at rest. It sets the flags alone:
// Show and Hide would refresh each object, on every frame of the press.
func (r *buttonRenderer) worklogPressHide(hide bool) {
	r.background.Hidden, r.tapBG.Hidden = hide, hide
	r.label.Hidden = hide || r.button.Text == ""
	if r.icon != nil {
		r.icon.Hidden = hide || r.button.Icon == nil || r.button.Hidden
	}
	if r.press.shot != nil {
		r.press.shot.Hidden = !hide
	}
}

// worklogPressPlace swaps the button's own objects for the picture while it is
// pressed.
func (r *buttonRenderer) worklogPressPlace(size fyne.Size) {
	p := &r.press
	if p.shot == nil || p.scale == 0 || p.scale == 1 {
		return
	}
	r.worklogPressHide(true)
	if p.pixels == 0 {
		p.pixels = 1
	}
	// The picture starts and ends on whole pixels, one beyond the fill each
	// way, so it is drawn exactly as it was made.
	px := func(v float32) float32 { return float32(math.Round(float64(v * p.pixels))) }
	left, top := px(p.abs.X)-1, px(p.abs.Y)-1
	right, bottom := px(p.abs.X)+px(size.Width)+1, px(p.abs.Y)+px(size.Height)+1
	p.inset = fyne.NewPos(left/p.pixels-p.abs.X, top/p.pixels-p.abs.Y)
	p.shot.Move(p.inset)
	p.shot.Resize(fyne.NewSize((right-left)/p.pixels, (bottom-top)/p.pixels))
	p.shot.Refresh()
}

// worklogPressPicture is the pressed button, w by h pixels: its fill and press
// flash worked out point by point, and the label and icon from their plate,
// all shrunk about the button's centre.
func (r *buttonRenderer) worklogPressPicture(w, h int) image.Image {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	p := &r.press
	room := p.shot.Size()
	if w <= 0 || h <= 0 || room.Width <= 0 {
		return out
	}
	// A screen that draws two pixels a point doubles the canvas's own scale.
	pixels := p.pixels
	if dense := float32(math.Round(float64(float32(w) / (room.Width * pixels)))); dense > 1 {
		pixels *= dense
	}
	px := func(v float32) float32 { return float32(math.Round(float64(v * pixels))) }
	// The picture's own corner, and the fill as the painter rounds it, in pixels.
	left, top := px(p.abs.X+p.inset.X), px(p.abs.Y+p.inset.Y)
	size := r.button.Size()
	fx, fy := px(p.abs.X)-left, px(p.abs.Y)-top
	fw, fh := px(size.Width), px(size.Height)
	cx, cy := fx+fw/2, fy+fh/2
	radius := px(fyne.Min(r.background.CornerRadius, fyne.Min(size.Width, size.Height)/2))
	soft := fyne.Max(px(0.5), 0.01)
	r.worklogPressPlate(w, h, pixels, int(left), int(top))

	fill, flash := worklogPremultiplied(r.background.FillColor), worklogPremultiplied(r.tapBG.FillColor)
	shrink := p.scale
	if shrink == 0 {
		shrink = 1
	}
	for y := 0; y < h; y++ {
		sy := cy + (float32(y)+0.5-cy)/shrink
		row := out.Pix[y*out.Stride:]
		for x := 0; x < w; x++ {
			sx := cx + (float32(x)+0.5-cx)/shrink
			// The painter's own rounded rectangle, measured in shrunk pixels.
			dx := float32(math.Abs(float64(sx-cx))) - fw/2 + radius
			dy := float32(math.Abs(float64(sy-cy))) - fh/2 + radius
			far := fyne.Min(fyne.Max(dx, dy), 0) + float32(math.Hypot(float64(fyne.Max(dx, 0)), float64(fyne.Max(dy, 0)))) - radius
			t := fyne.Min(fyne.Max((far*shrink+soft)/(2*soft), 0), 1)
			cover := 1 - t*t*(3-2*t)
			var c [4]float32
			if cover > 0 {
				for i := range c {
					c[i] = fill[i] * cover
				}
				for i := range c {
					c[i] = flash[i]*cover + c[i]*(1-flash[3]*cover)
				}
			}
			if ink := worklogPlateAt(p.top, sx-0.5, sy-0.5); ink[3] > 0 {
				for i := range c {
					c[i] = ink[i] + c[i]*(1-ink[3])
				}
			}
			if c[3] <= 0 {
				continue
			}
			for i := range c {
				row[x*4+i] = uint8(fyne.Min(c[i], 1)*255 + 0.5)
			}
		}
	}
	return out
}

// worklogPressPlate draws the label and the icon at rest onto a plate the size
// of the picture, each on the whole pixel the painter gives it. left and top
// are the picture's corner in the window, in pixels.
func (r *buttonRenderer) worklogPressPlate(w, h int, pixels float32, left, top int) {
	p := &r.press
	px := func(v float32) int { return int(math.Round(float64(v * pixels))) }
	type piece struct {
		object fyne.CanvasObject
		room   fyne.Size
		x, y   int
	}
	var pieces []piece
	// colour gives the icon's copy its colour, once it is known the plate has
	// to be drawn again.
	var colour func()
	var made strings.Builder
	fmt.Fprint(&made, w, h, pixels)
	if r.button.Text != "" {
		at := p.abs.Add(r.label.Position())
		for _, visual := range r.label.visualCache {
			for _, object := range visual.obj {
				text, ok := object.(*canvas.Text)
				if !ok || !text.Visible() || text.Text == "" {
					continue
				}
				// Where the painter starts the text within its own room.
				least, pos := text.MinSize(), at.Add(text.Position())
				switch text.Alignment {
				case fyne.TextAlignTrailing:
					pos.X += text.Size().Width - least.Width
				case fyne.TextAlignCenter:
					pos.X += (text.Size().Width - least.Width) / 2
				}
				if text.Size().Height > least.Height {
					pos.Y += (text.Size().Height - least.Height) / 2
				}
				ink := color.NRGBAModel.Convert(text.Color).(color.NRGBA)
				same := canvas.NewText(text.Text, ink)
				same.TextSize, same.TextStyle, same.FontSource = text.TextSize, text.TextStyle, text.FontSource
				same.Resize(least)
				pieces = append(pieces, piece{same, least.AddWidthHeight(2, 2), px(pos.X) - left, px(pos.Y) - top})
				fmt.Fprint(&made, "|", text.Text, ink, text.TextSize, text.TextStyle, px(pos.X)-left, px(pos.Y)-top)
			}
		}
	}
	if r.icon != nil && r.icon.Resource != nil && r.button.Icon != nil {
		pos := p.abs.Add(r.icon.Position())
		same := canvas.NewImageFromResource(r.icon.Resource)
		same.FillMode, same.Translucency = r.icon.FillMode, r.icon.Translucency
		same.Resize(r.icon.Size())
		// An icon that takes its colour from the theme asks the theme of its
		// place in the window. This copy has no place there, so it would ask
		// the app's theme, not the one the button stands under: the gear on
		// the gold menu bar came out in Fyne's own blue. Colour it here, by
		// the button's theme.
		tint := ""
		if themed, ok := r.icon.Resource.(fyne.ThemedResource); ok && svg.IsResourceSVG(r.icon.Resource) {
			v := fyne.CurrentApp().Settings().ThemeVariant()
			ink := color.NRGBAModel.Convert(r.button.Theme().Color(themed.ThemeColorName(), v)).(color.NRGBA)
			tint = fmt.Sprint(ink)
			colour = func() {
				if content, err := svg.Colorize(themed.Content(), ink); err == nil {
					same.Resource = fyne.NewStaticResource(tint+"-"+themed.Name(), content)
					same.Refresh() // it had read the uncoloured one when it was sized
				}
			}
		}
		pieces = append(pieces, piece{same, r.icon.Size(), px(pos.X) - left, px(pos.Y) - top})
		fmt.Fprint(&made, "|", r.icon.Resource.Name(), tint, r.icon.Size(), r.icon.Translucency, px(pos.X)-left, px(pos.Y)-top)
	}
	if p.top != nil && p.made == made.String() {
		return
	}
	if colour != nil {
		colour()
	}
	p.made = made.String()
	p.top = image.NewRGBA(image.Rect(0, 0, w, h))
	for _, piece := range pieces {
		c := software.NewTransparentCanvas()
		c.SetPadded(false)
		c.SetScale(pixels)
		holder := &fyne.Container{Objects: []fyne.CanvasObject{piece.object}}
		c.SetContent(holder)
		c.Resize(piece.room)
		shot := c.Capture()
		draw.Draw(p.top, shot.Bounds().Add(image.Pt(piece.x, piece.y)), shot, shot.Bounds().Min, draw.Over)
	}
}

// worklogPremultiplied is c as red, green, blue and alpha from 0 to 1, the
// colours already scaled by the alpha.
func worklogPremultiplied(c color.Color) [4]float32 {
	if c == nil {
		return [4]float32{}
	}
	r, g, b, a := c.RGBA()
	return [4]float32{float32(r) / 0xffff, float32(g) / 0xffff, float32(b) / 0xffff, float32(a) / 0xffff}
}

// worklogPlateAt is the plate's colour at a point between its pixels.
func worklogPlateAt(plate *image.RGBA, x, y float32) (c [4]float32) {
	if plate == nil {
		return c
	}
	x0, y0 := int(math.Floor(float64(x))), int(math.Floor(float64(y)))
	fx, fy := x-float32(x0), y-float32(y0)
	b := plate.Bounds()
	for _, corner := range [4]struct {
		x, y   int
		weight float32
	}{{x0, y0, (1 - fx) * (1 - fy)}, {x0 + 1, y0, fx * (1 - fy)}, {x0, y0 + 1, (1 - fx) * fy}, {x0 + 1, y0 + 1, fx * fy}} {
		if corner.weight == 0 || corner.x < 0 || corner.y < 0 || corner.x >= b.Dx() || corner.y >= b.Dy() {
			continue
		}
		pix := plate.Pix[corner.y*plate.Stride+corner.x*4:]
		if pix[3] == 0 {
			continue
		}
		for i := range c {
			c[i] += float32(pix[i]) / 255 * corner.weight
		}
	}
	return c
}

func (r *buttonRenderer) Destroy() {
	r.hover.stop()
	r.button.isAnimating = false
	r.press.stopRest()
	if r.button.tapAnim != nil {
		r.button.tapAnim.Stop()
		r.button.tapAnim = nil
	}
	r.BaseRenderer.Destroy()
}
