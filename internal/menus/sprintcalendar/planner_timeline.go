package sprintcalendar

import (
	"fmt"
	"image/color"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

const PlannerDayHeaderHeight = float32(116)

const plannerRulerWidth = float32(52)

// Cards and grid marks share a scale. Spacing between cards would distort it.
type plannerDayBlocksLayout struct{ scale *plannerScale }

func (l plannerDayBlocksLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objects {
		h += o.MinSize().Height
	}
	return fyne.NewSize(80, fyne.Max(480*l.scale.Pixels(), h)+8)
}

func (plannerDayBlocksLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	y := float32(0)
	for _, o := range objects {
		h := o.MinSize().Height
		o.Move(fyne.NewPos(3, y))
		o.Resize(fyne.NewSize(fyne.Max(1, size.Width-6), h))
		y += h
	}
}

type plannerDayContentsLayout struct{}

func (plannerDayContentsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(80, PlannerDayHeaderHeight+objects[1].MinSize().Height)
}

func (plannerDayContentsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.Position{})
	objects[0].Resize(fyne.NewSize(size.Width, PlannerDayHeaderHeight))
	objects[1].Move(fyne.NewPos(0, PlannerDayHeaderHeight))
	objects[1].Resize(fyne.NewSize(size.Width, fyne.Max(0, size.Height-PlannerDayHeaderHeight)))
}

type plannerWeekLayout struct {
	ruler  *DurationRuler
	scale  *plannerScale
	expand bool
}

func (l plannerWeekLayout) minutes(days fyne.CanvasObject) int {
	height := days.MinSize().Height
	return backend.PlannerDurationUp(int((height-PlannerDayHeaderHeight-8)/l.scale.Pixels() + 0.5))
}

func (l plannerWeekLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	s := objects[1].MinSize()
	return fyne.NewSize(plannerRulerWidth+s.Width, PlannerDayHeaderHeight+float32(l.minutes(objects[1]))*PlannerPixelsPerMinute+8)
}

func (l plannerWeekLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	minutes := l.minutes(objects[1])
	pixels := PlannerPixelsPerMinute
	if l.expand {
		pixels = fyne.Max(pixels, (size.Height-PlannerDayHeaderHeight-8)/float32(minutes))
	}
	l.scale.pixelsPerMinute = pixels
	l.ruler.SetMinutes(minutes)
	objects[0].Move(fyne.Position{})
	objects[0].Resize(size)
	objects[0].Refresh()
	objects[1].Move(fyne.NewPos(plannerRulerWidth, 0))
	objects[1].Resize(fyne.NewSize(fyne.Max(1, size.Width-plannerRulerWidth), size.Height))
	objects[1].Refresh()
}

func newPlannerWeek(days fyne.CanvasObject, scale *plannerScale, expand bool) fyne.CanvasObject {
	ruler := newDurationRuler(480)
	ruler.Scale = scale
	return container.New(plannerWeekLayout{ruler, scale, expand}, ruler, days)
}

// A 15-minute bubble occupies one grid increment. Details appear as space permits.
type PlannerTimedFace struct {
	widget.BaseWidget
	title, project, duration, issueURL string
	accent                             color.Color
	day                                *shell.PlannerDayHover
	finished                           bool
	glow                               shell.Glow
	lit, over                          bool
	inside                             int
	bg, stripe                         *canvas.Rectangle
}

// childHover keeps the block lit while the pointer is on its GH link.
func (f *PlannerTimedFace) childHover(on bool) {
	if on {
		f.inside++
	} else if f.inside > 0 {
		f.inside--
	}
	f.light()
}

// SetHover lights the block like every other bubble: lighter, its bar
// sliding in from the side. The card holding it hears the pointer and calls
// this.
func (f *PlannerTimedFace) SetHover(on bool) {
	f.over = on
	f.light()
}

func (f *PlannerTimedFace) light() {
	on := f.over || f.inside > 0
	if !shell.HoverAllowed(f, on) || on == f.lit {
		return
	}
	f.lit = on
	target := float32(0)
	if on {
		target = 1
	}
	f.glow.To(target, func(float32) { f.paint() })
}

func (f *PlannerTimedFace) paint() {
	if f.bg == nil {
		return
	}
	level := f.glow.Level()
	f.bg.FillColor = shell.MixColor(shell.BubbleFill(f.accent, false), shell.BubbleFill(f.accent, true), level)
	f.bg.StrokeColor = shell.MixColor(shell.BubbleStroke(f.accent, false), f.accent, level)
	f.stripe.FillColor = shell.MixColor(color.Transparent, f.accent, level)
	f.stripe.Move(fyne.NewPos(1-6*(1-level), 1))
	f.bg.Refresh()
	f.stripe.Refresh()
}

func newPlannerTimedFace(title, project, issueURL string, minutes int, accent color.Color) *PlannerTimedFace {
	f := &PlannerTimedFace{title: title, project: project, duration: fmt.Sprintf("%dm", minutes), issueURL: issueURL, accent: accent}
	f.ExtendBaseWidget(f)
	return f
}

func (f *PlannerTimedFace) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(shell.BubbleFill(f.accent, false))
	bg.CornerRadius = 5
	bg.StrokeColor = shell.BubbleStroke(f.accent, false)
	bg.StrokeWidth = 1
	stripe := canvas.NewRectangle(color.Transparent)
	f.bg, f.stripe = bg, stripe
	title := canvas.NewText("", theme.Color(theme.ColorNameForeground))
	title.TextStyle.Bold = true
	title.TextSize = 11
	second := canvas.NewText("", title.Color)
	second.TextSize = 11
	minutes := canvas.NewText("", title.Color)
	minutes.TextSize = 11
	grip := canvas.NewLine(widgets.WithAlpha(f.accent, 0.9))
	grip.StrokeWidth = 2
	link := newPlannerIssueLink(f.issueURL)
	link.day = f.day
	link.parent = f.childHover
	if f.issueURL == "" {
		link.Hide()
	}
	return &plannerTimedFaceRenderer{f, bg, stripe, title, second, minutes, grip, link}
}

type plannerTimedFaceRenderer struct {
	face                   *PlannerTimedFace
	bg, stripe             *canvas.Rectangle
	title, second, minutes *canvas.Text
	grip                   *canvas.Line
	link                   fyne.CanvasObject
}

func (r *plannerTimedFaceRenderer) MinSize() fyne.Size { return fyne.NewSize(80, 0) }

func (r *plannerTimedFaceRenderer) Layout(size fyne.Size) {
	r.link.(*PlannerIssueLink).day = r.face.day
	r.bg.Resize(size)
	r.stripe.Resize(fyne.NewSize(3, fyne.Max(0, size.Height-2)))
	r.face.paint()
	width := fyne.Max(1, size.Width-27)
	first, rest := plannerTitleLine(r.face.title, width, r.title.TextSize)
	r.title.Text = first
	r.title.Move(fyne.NewPos(7, 1))
	r.title.Resize(r.title.MinSize())
	r.second.Text = ""
	showIf := func(o fyne.CanvasObject, show bool) {
		if show {
			o.Show()
		} else {
			o.Hide()
		}
	}
	showIf(r.title, size.Height >= 14)
	showIf(r.second, size.Height >= 52)
	if size.Height >= 52 {
		if rest == "" {
			rest = r.face.project
		}
		r.second.Text = plannerFitText(rest, width, 11)
		r.second.Move(fyne.NewPos(7, 17))
		r.second.Resize(r.second.MinSize())
	}
	minutes := r.face.duration
	if r.face.project != "" {
		minutes += " · " + r.face.project
	}
	r.minutes.Text = plannerFitText(minutes, size.Width-14, 11)
	if r.face.finished {
		r.minutes.Text = plannerFitText(r.face.duration+" · Done", size.Width-14, 11)
	}
	showIf(r.minutes, size.Height >= 36)
	if size.Height >= 36 {
		y := float32(17)
		if size.Height >= 52 {
			y = 33
		}
		r.minutes.Move(fyne.NewPos(7, y))
		r.minutes.Resize(r.minutes.MinSize())
	}
	r.link.Move(fyne.NewPos(fyne.Max(0, size.Width-24), 1))
	r.link.Resize(fyne.NewSize(22, 15))
	showIf(r.link, r.link.(*PlannerIssueLink).URL != nil && size.Height >= 16)
	r.grip.Position1 = fyne.NewPos(size.Width/2-10, size.Height-3)
	r.grip.Position2 = fyne.NewPos(size.Width/2+10, size.Height-3)
	showIf(r.grip, size.Height >= 18 && !r.face.finished)
}

func (r *plannerTimedFaceRenderer) Refresh() {
	r.Layout(r.face.Size())
	canvas.Refresh(r.face)
}

func (r *plannerTimedFaceRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.stripe, r.title, r.second, r.minutes, r.link, r.grip}
}

func (*plannerTimedFaceRenderer) Destroy() {}

// Keep direct issue access even on a single-increment bubble, without button
// padding or a font's emoji treatment of the external-link arrow.
type PlannerIssueLink struct {
	widget.Hyperlink
	day    *shell.PlannerDayHover
	parent func(on bool) // the block it sits on, kept lit while hovered
}

func newPlannerIssueLink(raw string) *PlannerIssueLink {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		u = nil
	}
	l := &PlannerIssueLink{Hyperlink: widget.Hyperlink{Text: "GH", URL: u}}
	l.ExtendBaseWidget(l)
	return l
}

func (l *PlannerIssueLink) CreateRenderer() fyne.WidgetRenderer {
	text := canvas.NewText("GH", widgets.LogoGold)
	text.TextSize = 11
	return widget.NewSimpleRenderer(text)
}

func (*PlannerIssueLink) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (l *PlannerIssueLink) MouseIn(*desktop.MouseEvent) {
	if l.day != nil {
		l.day.Enter(l)
	}
	if l.parent != nil {
		l.parent(true)
	}
}

func (*PlannerIssueLink) MouseMoved(*desktop.MouseEvent) {}

func (l *PlannerIssueLink) MouseOut() {
	if l.day != nil {
		l.day.Leave(l)
	}
	if l.parent != nil {
		l.parent(false)
	}
}

func plannerFitText(s string, width, size float32) string {
	if fyne.MeasureText(s, size, fyne.TextStyle{Bold: true}).Width <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && fyne.MeasureText(string(runes)+"…", size, fyne.TextStyle{Bold: true}).Width > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func plannerTitleLine(s string, width, size float32) (string, string) {
	if fyne.MeasureText(s, size, fyne.TextStyle{Bold: true}).Width <= width {
		return s, ""
	}
	words := strings.Fields(s)
	line := ""
	for i, word := range words {
		next := strings.TrimSpace(line + " " + word)
		if fyne.MeasureText(next, size, fyne.TextStyle{Bold: true}).Width > width {
			if i == 0 {
				return plannerFitText(word, width, size), strings.Join(words[1:], " ")
			}
			return line, strings.Join(words[i:], " ")
		}
		line = next
	}
	return line, ""
}
