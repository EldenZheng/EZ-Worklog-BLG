package shell

import (
	"image"
	"image/color"
	"image/draw"
	"reflect"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Every popup opens and closes the same way. Opening, it rises the last few
// pixels into place while the dimmed, blurred window behind it fades in.
// Closing, it sinks and fades out while the backdrop clears.
//
// Fyne cannot fade a widget, so opening moves the popup itself and fades only
// the backdrop. Closing works on a picture: the popup is captured from the
// window, hidden at once, and the picture fades in a layer FitScreen keeps over
// the window's content. Hiding at once matters: Fyne hides a popup by dropping
// it and everything opened above it, so a popup kept on screen to animate would
// take with it whatever opened next, such as the editor a list popup opens.
//
// Open popups with ShowDialog (or ShowStockDialog, ShowInformation, ShowError),
// close them with CloseDialog or ClosePopUp, and call AnimateOpening straight
// after showing and placing a widget.PopUp. tests/popup_motion_test.go fails on
// code that opens or closes one some other way.

// Closing mirrors opening: as long, as far, on the same curve, so neither
// reads as the slower of the two.
const (
	popupOpenTime  = 340 * time.Millisecond
	popupCloseTime = popupOpenTime
	popupRise      = float32(28)
	popupSink      = popupRise
)

// easeOutCubic slows into place more gently than Fyne's own ease-out.
func easeOutCubic(p float32) float32 {
	q := 1 - p
	return 1 - q*q*q
}

// EaseInOutCubic starts and ends gently, fastest in the middle.
func EaseInOutCubic(p float32) float32 {
	if p < 0.5 {
		return 4 * p * p * p
	}
	q := -2*p + 2
	return 1 - q*q*q/2
}

// popupMotion is what an open popup's closing needs from its opening: the
// backdrop at full strength, and the opening, if it is still running.
type popupMotion struct {
	blurRadius float32
	shade      color.NRGBA
	modal      bool
	opening    *fyne.Animation
}

var (
	motions       = map[*widget.PopUp]*popupMotion{}
	dialogPopups  = map[dialog.Dialog]*widget.PopUp{}
	closingLayers = map[fyne.Canvas]*fyne.Container{}
)

func animationsOn() bool { return fyne.CurrentApp().Settings().ShowAnimations() }

// ShowDialog shows d over win and animates it in. Close it with CloseDialog.
func ShowDialog(d dialog.Dialog, win fyne.Window) {
	FinishDropdownClosings()
	d.Show()
	if popup := topPopUp(win.Canvas()); popup != nil && AnimateOpening(win.Canvas()) != nil {
		dialogPopups[d] = popup
	}
}

// ShowStockDialog is ShowDialog for a dialog Fyne builds whole, whose own
// buttons close it. dismiss names those buttons, so they close it with the
// motion too.
func ShowStockDialog(d dialog.Dialog, win fyne.Window, dismiss ...string) {
	FinishDropdownClosings()
	d.Show()
	AnimateStockDialog(win, dismiss...)
}

// AnimateStockDialog animates in the stock dialog just shown on win, as
// ShowStockDialog does, for one that has to be shown some other way.
func AnimateStockDialog(win fyne.Window, dismiss ...string) {
	c := win.Canvas()
	popup := topPopUp(c)
	if popup == nil || AnimateOpening(c) == nil {
		return
	}
	names := map[string]bool{}
	for _, name := range dismiss {
		names[name] = true
	}
	eachButton(popup, func(b *widget.Button) {
		if !names[b.Text] || b.OnTapped == nil {
			return
		}
		press := b.OnTapped
		b.OnTapped = func() { closeAround(c, popup, press) }
	})
}

// ShowInformation is dialog.ShowInformation with the motion.
func ShowInformation(title, message string, win fyne.Window) {
	ShowStockDialog(dialog.NewInformation(title, message, win), win, "OK")
}

// ShowError is dialog.ShowError with the motion.
func ShowError(err error, win fyne.Window) {
	ShowStockDialog(dialog.NewError(err, win), win, "OK")
}

// CloseDialog hides d, shown with ShowDialog, and plays the closing motion. It
// returns the closing animation, or nil when there is none.
func CloseDialog(d dialog.Dialog, win fyne.Window) *fyne.Animation {
	popup := dialogPopups[d]
	delete(dialogPopups, d)
	if popup == nil {
		d.Hide()
		return nil
	}
	return closeAround(win.Canvas(), popup, d.Hide)
}

// ClosePopUp hides p and plays the closing motion.
func ClosePopUp(p *widget.PopUp) *fyne.Animation {
	return closeAround(p.Canvas, p, p.Hide)
}

// AnimateOpening plays the opening motion on the popup shown last on c, ending
// where it stands now. It returns the animation, already started, or nil when
// there is no popup or animations are switched off. Fyne's test driver runs an
// animation straight to its end, so tests see the popup in place.
func AnimateOpening(c fyne.Canvas) *fyne.Animation {
	if c == nil || !animationsOn() {
		return nil
	}
	top := c.Overlays().Top()
	popup := topPopUp(c)
	if popup == nil {
		return nil
	}
	final := popup.Position()
	blur, shade := overlayBackdrop(top)
	m := &popupMotion{}
	if blur != nil {
		m.blurRadius, m.modal = blur.Radius, true
	}
	if shade != nil {
		m.shade, m.modal = color.NRGBAModel.Convert(shade.FillColor).(color.NRGBA), true
	}
	step := func(p float32) {
		popup.Move(final.AddXY(0, popupRise*(1-p)))
		if blur != nil {
			blur.Radius = max(0.5, m.blurRadius*p) // a blur needs a radius above zero
			blur.Refresh()
		}
		if shade != nil {
			shade.FillColor = fadeTo(m.shade, p)
			shade.Refresh()
		}
		canvas.Refresh(popup)
	}
	// The first frame is the start, not the popup already in place.
	step(0)
	m.opening = fyne.NewAnimation(popupOpenTime, step)
	m.opening.Curve = easeOutCubic
	motions[popup] = m
	m.opening.Start()
	return m.opening
}

// dropTime is how long a drop-down takes to unfold or fold.
const dropTime = 240 * time.Millisecond

var dropdownOpening = map[*widget.PopUp]*fyne.Animation{}
var dropdownClosing = map[*widget.PopUp]*fyne.Animation{}

// dropdownBottoms holds the bottom edge of each drop-down that opens upward,
// above what opened it: such a drop-down grows and shrinks from that edge.
var dropdownBottoms = map[*widget.PopUp]float32{}

// OpenUpward pins p's bottom edge at bottom while it unfolds and folds, for a
// drop-down with no room below what opened it.
func OpenUpward(p *widget.PopUp, bottom float32) { dropdownBottoms[p] = bottom }

// keepBottom moves an upward drop-down so its bottom edge stays put as its
// height changes.
func keepBottom(p *widget.PopUp) {
	if bottom, ok := dropdownBottoms[p]; ok {
		p.Move(fyne.NewPos(p.Position().X, bottom-p.Size().Height))
	}
}

// Complete old folds before adding an overlay: hiding an old Fyne popup later
// would also remove any new overlay opened above it.
func FinishDropdownClosings() {
	for p, a := range dropdownClosing {
		a.Stop()
		delete(dropdownClosing, p)
		delete(dropdownBottoms, p)
		p.Hide()
	}
}

// UnfoldPopUp opens p, a drop-down already shown at its place, down from its top
// edge to size, uncovering r, its content, as it goes. r must have Fold set.
func UnfoldPopUp(p *widget.PopUp, r *Reveal, size fyne.Size) *fyne.Animation {
	step := func(share float32) {
		r.SetShare(share)
		p.Resize(fyne.NewSize(size.Width, size.Height*share))
		keepBottom(p)
	}
	if !animationsOn() {
		step(1)
		return nil
	}
	step(0)
	var anim *fyne.Animation
	anim = fyne.NewAnimation(dropTime, func(share float32) {
		if dropdownOpening[p] != anim {
			return
		}
		step(share)
		if share >= 1 {
			delete(dropdownOpening, p)
		}
	})
	anim.Curve = easeOutQuart
	dropdownOpening[p] = anim
	anim.Start()
	return anim
}

// FoldPopUp folds a drop-down opened by UnfoldPopUp back up into its top edge
// and hides it.
func FoldPopUp(p *widget.PopUp, r *Reveal) *fyne.Animation {
	if a := dropdownOpening[p]; a != nil {
		a.Stop()
		delete(dropdownOpening, p)
	}
	size := p.Size()
	if r == nil || !animationsOn() {
		delete(dropdownBottoms, p)
		p.Hide()
		return nil
	}
	startShare := r.Share()
	var anim *fyne.Animation
	anim = fyne.NewAnimation(dropTime, func(done float32) {
		if dropdownClosing[p] != anim {
			return
		}
		share := 1 - done
		r.SetShare(startShare * share)
		p.Resize(fyne.NewSize(size.Width, size.Height*share))
		keepBottom(p)
		if done >= 1 {
			delete(dropdownClosing, p)
			delete(dropdownBottoms, p)
			p.Hide()
		}
	})
	anim.Curve = easeOutQuart
	dropdownClosing[p] = anim
	anim.Start()
	return anim
}

// closeAround runs hide, which takes popup off c, with the closing motion: the
// popup as it looks now fades away where it stood.
func closeAround(c fyne.Canvas, popup *widget.PopUp, hide func()) *fyne.Animation {
	m := motions[popup]
	delete(motions, popup)
	layer := closingLayers[c]
	if m == nil || layer == nil || !animationsOn() || !popup.Visible() {
		hide()
		return nil
	}
	if m.opening != nil {
		m.opening.Stop() // closed while still arriving
	}
	pos, size := popup.Position(), popup.Size()
	picture := capturePopUp(c, pos, size)
	hide()
	if picture == nil || popup.Visible() {
		return nil // nothing to show, or it did not close after all
	}
	ghost := canvas.NewImageFromImage(picture)
	ghost.FillMode = canvas.ImageFillStretch
	ghost.Resize(size)
	ghost.Move(pos)
	parts := []fyne.CanvasObject{ghost}
	var blur *canvas.Blur
	var shade *canvas.Rectangle
	if m.modal {
		blur = canvas.NewBlur(max(0.5, m.blurRadius))
		shade = canvas.NewRectangle(m.shade)
		blur.Resize(layer.Size())
		shade.Resize(layer.Size())
		parts = []fyne.CanvasObject{blur, shade, ghost}
	}
	layer.Objects = append(layer.Objects, parts...)
	layer.Refresh()
	anim := fyne.NewAnimation(popupCloseTime, func(p float32) {
		ghost.Move(pos.AddXY(0, popupSink*p))
		ghost.Translucency = float64(p)
		ghost.Refresh()
		if blur != nil {
			blur.Radius = max(0.5, m.blurRadius*(1-p))
			blur.Refresh()
			shade.FillColor = fadeTo(m.shade, 1-p)
			shade.Refresh()
		}
		if p == 1 {
			kept := layer.Objects[:0]
			for _, o := range layer.Objects {
				if o != ghost && o != blur && o != shade {
					kept = append(kept, o)
				}
			}
			layer.Objects = kept
			layer.Refresh()
		}
	})
	anim.Curve = easeOutCubic
	anim.Start()
	return anim
}

// capturePopUp copies the popup at pos and size out of what c shows now, with
// its rounded corners cut clear.
func capturePopUp(c fyne.Canvas, pos fyne.Position, size fyne.Size) *image.NRGBA {
	picture := captureArea(c, pos, size)
	if picture == nil {
		return nil
	}
	_, radius := c.PixelCoordinateForPosition(fyne.NewPos(0, theme.Size(theme.SizeNamePopupRadius)))
	clearCorners(picture, radius)
	return picture
}

// captureArea copies the area at pos and size out of what c shows now, or nil
// when any of it is off the canvas: a cut-off picture would be stretched.
func captureArea(c fyne.Canvas, pos fyne.Position, size fyne.Size) *image.NRGBA {
	if c == nil || size.IsZero() {
		return nil
	}
	shot := c.Capture()
	if shot == nil {
		return nil
	}
	x0, y0 := c.PixelCoordinateForPosition(pos)
	x1, y1 := c.PixelCoordinateForPosition(pos.Add(size))
	area := image.Rect(x0, y0, x1, y1)
	if area.Empty() || !area.In(shot.Bounds()) {
		return nil
	}
	picture := image.NewNRGBA(image.Rect(0, 0, area.Dx(), area.Dy()))
	draw.Draw(picture, picture.Bounds(), shot, area.Min, draw.Src)
	return picture
}

// clearCorners makes the pixels outside a rounded rectangle's corners of the
// given radius transparent: they are the backdrop, not the popup.
func clearCorners(img *image.NRGBA, radius int) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	r := float64(radius)
	for y := 0; y < radius && y < h; y++ {
		for x := 0; x < radius && x < w; x++ {
			dx, dy := r-float64(x)-0.5, r-float64(y)-0.5
			if dx*dx+dy*dy <= r*r {
				continue
			}
			for _, p := range [][2]int{{x, y}, {w - 1 - x, y}, {x, h - 1 - y}, {w - 1 - x, h - 1 - y}} {
				img.SetNRGBA(p[0], p[1], color.NRGBA{})
			}
		}
	}
}

func fadeTo(full color.NRGBA, p float32) color.NRGBA {
	full.A = uint8(float32(full.A) * p)
	return full
}

// topPopUp is the popup on top of c, if the top overlay is one.
func topPopUp(c fyne.Canvas) *widget.PopUp {
	if c == nil {
		return nil
	}
	popup, _ := overlayContent(c.Overlays().Top()).(*widget.PopUp)
	return popup
}

// eachButton visits the buttons in a popup, through the containers, theme
// overrides and popups that hold them.
func eachButton(o fyne.CanvasObject, visit func(*widget.Button)) {
	switch x := o.(type) {
	case *widget.Button:
		visit(x)
	case *fyne.Container:
		for _, child := range x.Objects {
			eachButton(child, visit)
		}
	case *container.ThemeOverride:
		eachButton(x.Content, visit)
	case *widget.PopUp:
		eachButton(x.Content, visit)
	}
}

// overlayBackdrop finds the blur and the shade a modal popup lays over the
// window. Fyne keeps them in its OverlayContainer's exported Background field,
// as a blur under a rectangle; anything else is left alone.
func overlayBackdrop(o fyne.CanvasObject) (*canvas.Blur, *canvas.Rectangle) {
	v := reflect.ValueOf(o)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return nil, nil
	}
	f := v.Elem().FieldByName("Background")
	if !f.IsValid() || !f.CanInterface() {
		return nil, nil
	}
	box, ok := f.Interface().(*fyne.Container)
	if !ok || box == nil {
		return nil, nil
	}
	var blur *canvas.Blur
	var shade *canvas.Rectangle
	for _, child := range box.Objects {
		switch c := child.(type) {
		case *canvas.Blur:
			blur = c
		case *canvas.Rectangle:
			shade = c
		}
	}
	return blur, shade
}
