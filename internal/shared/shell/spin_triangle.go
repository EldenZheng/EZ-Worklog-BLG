package shell

import (
	"fmt"
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// SpinTriangle is the solid ▼ on dropdowns and fold headings. It points down
// while its menu or section is shut and turns half a circle to point up when
// it opens, and back when it closes.
type SpinTriangle struct {
	widget.BaseWidget
	img  *canvas.Image
	ink  color.Color
	turn float32 // 0 points down, 1 points up
	goal float32 // where a running turn ends
	anim *fyne.Animation
}

// NewSpinTriangle starts pointing up when up is set, without turning.
func NewSpinTriangle(ink color.Color, up bool) *SpinTriangle {
	t := &SpinTriangle{ink: ink, turn: boolShare(up)}
	t.img = canvas.NewImageFromResource(triangleResource(ink, t.turn))
	t.img.FillMode = canvas.ImageFillContain
	t.ExtendBaseWidget(t)
	return t
}

// SetInk recolours it, as when its field is disabled.
func (t *SpinTriangle) SetInk(ink color.Color) {
	t.ink = ink
	t.paint()
}

// Up reports where it points, or is turning to.
func (t *SpinTriangle) Up() bool {
	if t.anim != nil {
		return t.goal == 1
	}
	return t.turn == 1
}

// Turn spins it to point up or down over MorphTime, from wherever it is, so
// a quick reversal turns back rather than jumping.
func (t *SpinTriangle) Turn(up bool) {
	goal := boolShare(up)
	if t.anim != nil {
		t.anim.Stop()
		t.anim = nil
	}
	if t.turn == goal {
		return
	}
	from := t.turn
	t.goal = goal
	t.anim = fyne.NewAnimation(MorphTime, func(p float32) {
		t.turn = from + (goal-from)*EaseInOutCubic(p)
		t.paint()
		if p >= 1 {
			t.anim = nil
		}
	})
	t.anim.Curve = fyne.AnimationLinear
	t.anim.Start()
}

// Stop ends a turn where it is.
func (t *SpinTriangle) Stop() {
	if t.anim != nil {
		t.anim.Stop()
		t.anim = nil
	}
}

func (t *SpinTriangle) paint() {
	if res := triangleResource(t.ink, t.turn); t.img.Resource != res {
		t.img.Resource = res
		t.img.Refresh()
	}
}

func (t *SpinTriangle) CreateRenderer() fyne.WidgetRenderer {
	return &spinTriangleRenderer{t: t}
}

type spinTriangleRenderer struct{ t *SpinTriangle }

func (r *spinTriangleRenderer) MinSize() fyne.Size { return fyne.NewSquareSize(theme.IconInlineSize()) }
func (r *spinTriangleRenderer) Layout(size fyne.Size) {
	side := min(size.Width, size.Height)
	r.t.img.Move(fyne.NewPos((size.Width-side)/2, (size.Height-side)/2))
	r.t.img.Resize(fyne.NewSquareSize(side))
}
func (r *spinTriangleRenderer) Refresh()                     { r.t.paint(); r.Layout(r.t.Size()) }
func (r *spinTriangleRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.t.img} }
func (r *spinTriangleRenderer) Destroy()                     { r.t.Stop() }

var (
	triangleMu    sync.Mutex
	triangleCache = map[triangleKey]fyne.Resource{}
)

type triangleKey struct {
	ink  color.NRGBA
	step int
}

// triangleSteps is how finely the turn is drawn: 30 frames for half a turn.
const triangleSteps = 30

// triangleResource is the ▼ in ink, a solid shape with softened corners,
// turned by turn (0 points down, 1 points up). One resource per colour and
// step, so neither a refresh nor a spin draws one twice.
func triangleResource(ink color.Color, turn float32) fyne.Resource {
	key := triangleKey{color.NRGBAModel.Convert(ink).(color.NRGBA), int(turn*triangleSteps + .5)}
	triangleMu.Lock()
	defer triangleMu.Unlock()
	if res, ok := triangleCache[key]; ok {
		return res
	}
	c := key.ink
	fill := fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	angle := 180 * float64(key.step) / triangleSteps
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g transform="rotate(%.1f 12 12)">`+
		`<path d="M6.5 9.5h11L12 15.5z" fill="%s" fill-opacity="%.3f" stroke="%s" stroke-opacity="%.3f" stroke-width="1.6" stroke-linejoin="round"/></g></svg>`,
		angle, fill, float64(c.A)/255, fill, float64(c.A)/255)
	res := fyne.NewStaticResource(fmt.Sprintf("triangle-%s-%d-%d.svg", fill[1:], c.A, key.step), []byte(svg))
	triangleCache[key] = res
	return res
}
