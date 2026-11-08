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
	onDelete                           func() // removes the block; nil: no Delete
	delFade                            *shell.SmallDelete
	glow                               shell.Glow
	lit, over                          bool
	marked                             bool          // edged in gold for its tray card
	top                                float32       // the inset above and below the text, set by Layout
	icon                               fyne.Resource // issue or PR mark; nil for the meeting
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
	if f.delFade != nil {
		f.delFade.FadeTo(on) // the Delete fades with the light
	}
}

func (f *PlannerTimedFace) paint() {
	if f.bg == nil {
		return
	}
	level := f.glow.Level()
	f.bg.FillColor = shell.MixColor(shell.BubbleFill(f.accent, false), shell.BubbleFill(f.accent, true), level)
	f.bg.StrokeColor = shell.MixColor(shell.BubbleStroke(f.accent, false), shell.BubbleStroke(f.accent, true), level)
	f.bg.StrokeWidth = 1
	if f.marked {
		// Gold through and through, so the issue's blocks stand out at a glance.
		f.bg.FillColor = shell.BlendColor(f.bg.FillColor, widgets.LogoGold, 0.6)
		f.bg.StrokeColor = widgets.LogoGold
		f.bg.StrokeWidth = 2
	}
	f.stripe.FillColor = shell.MixColor(color.Transparent, f.accent, level)
	f.placeStripe(level)
	f.bg.Refresh()
	f.stripe.Refresh()
}

// placeStripe slides the accent bar in from outside the left edge to its
// slot, as on Kanban's cards: inset, rounded, and as tall as the text area.
func (f *PlannerTimedFace) placeStripe(level float32) {
	pad := theme.Padding()
	size := f.Size()
	size.Height = fyne.Max(0, size.Height-plannerBlockGap)
	top := f.top
	if top <= 0 {
		top = plannerBlockTop(size.Height, false)
	}
	f.stripe.Move(fyne.NewPos(pad-(2*pad+plannerStripeWidth+8)*(1-level), top))
	f.stripe.Resize(fyne.NewSize(plannerStripeWidth, fyne.Max(0, size.Height-2*top)))
}

// plannerStripeWidth matches the hover bar of Kanban's cards.
const plannerStripeWidth = float32(4)

// plannerBlockTop is the inset above and below a block's text: 10, or what a
// block too short for it can spare around its lines, never under 4. header
// says the link has a line of its own above the title.
func plannerBlockTop(h float32, header bool) float32 {
	content := plannerLineHeight
	if header {
		content += plannerHeaderHeight + plannerHeaderGap
	}
	return fyne.Min(plannerMaxTop, fyne.Max(plannerMinTop, (h-content-plannerBottomExtra)/2))
}

// plannerBlockGap is the space under each block's bubble, between it and
// the next block.
const plannerBlockGap = float32(4)

// plannerBottomExtra is how much more room a block keeps under its text than
// above it, so the last line does not sit on the block's edge.
const plannerBottomExtra = float32(4)

// The inset above and below a block's text, at most and at least.
const (
	plannerMaxTop = float32(10)
	plannerMinTop = float32(4)
)

// plannerHeaderHeight is the line the link has to itself above the title.
const plannerHeaderHeight = float32(18)

// plannerHeaderGap is the space under the repo#number before the title, as
// on Kanban's cards.
const plannerHeaderGap = float32(6)

// SetMarked edges the block in gold while its issue's tray card is under the
// pointer, to show where the issue is already planned.
func (f *PlannerTimedFace) SetMarked(on bool) {
	if f.marked == on {
		return
	}
	f.marked = on
	f.paint()
}

func newPlannerTimedFace(title, project, issueURL string, minutes int, accent color.Color, icon fyne.Resource) *PlannerTimedFace {
	f := &PlannerTimedFace{title: title, project: project, duration: fmt.Sprintf("%dm", minutes), issueURL: issueURL, accent: accent, icon: icon}
	f.ExtendBaseWidget(f)
	return f
}

// plannerLinkSize is the block link's text size, a touch larger than the rest
// of the block so it is easy to hit.
const plannerLinkSize = shell.CardLinkSize

// plannerTitleLines is the most lines a block's title wraps to.
const plannerTitleLines = 4

// plannerTitleSize and plannerLineHeight are the block title's text size and
// line spacing, near a Kanban card's title.
const (
	plannerTitleSize  = float32(13)
	plannerLineHeight = float32(16)
)

func (f *PlannerTimedFace) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(shell.BubbleFill(f.accent, false))
	bg.CornerRadius = 5
	bg.StrokeColor = shell.BubbleStroke(f.accent, false)
	bg.StrokeWidth = 1
	stripe := canvas.NewRectangle(color.Transparent)
	stripe.CornerRadius = 2
	f.bg, f.stripe = bg, stripe
	var titles []*canvas.Text
	for i := 0; i < plannerTitleLines; i++ {
		line := canvas.NewText("", theme.Color(theme.ColorNameForeground))
		line.TextStyle.Bold = true
		line.TextSize = plannerTitleSize
		titles = append(titles, line)
	}
	minutes := canvas.NewText("", theme.Color(theme.ColorNameForeground))
	minutes.TextSize = 11
	grip := canvas.NewLine(widgets.WithAlpha(f.accent, 0.9))
	grip.StrokeWidth = 2
	link := newPlannerIssueLink(f.issueURL)
	link.day = f.day
	link.parent = f.childHover
	if f.issueURL == "" {
		link.Hide()
	}
	r := &plannerTimedFaceRenderer{face: f, bg: bg, stripe: stripe, titles: titles, minutes: minutes, grip: grip, link: link}
	if f.icon != nil {
		// The issue or pull request mark, in gold as on Kanban's cards.
		r.icon = container.NewThemeOverride(widget.NewIcon(f.icon), widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	}
	if f.onDelete != nil {
		r.del, f.delFade = newPlannerBlockDelete(f)
		r.delView = f.delFade.View
		f.delFade.OnChange = f.Refresh // placed when it comes, dropped when gone
	}
	return r
}

type plannerTimedFaceRenderer struct {
	face       *PlannerTimedFace
	bg, stripe *canvas.Rectangle
	titles     []*canvas.Text // the title's wrapped lines
	minutes    *canvas.Text
	grip       *canvas.Line
	icon       fyne.CanvasObject // nil for the meeting
	link       fyne.CanvasObject
	del        *PlannerBlockDelete // nil when the block has no Delete
	delView    fyne.CanvasObject
}

func (r *plannerTimedFaceRenderer) MinSize() fyne.Size { return fyne.NewSize(80, 0) }

// Layout draws the block as a Kanban card without its footer: the issue or
// PR mark and its repo#number on the top line, the title under it, wrapping
// as far as the block's height allows, then the minutes when there is room.
// A quarter-hour block keeps the mark, the title and the link on one line.
// Delete floats over the top right, drawn above the rest, so nothing moves
// when it fades in.
func (r *plannerTimedFaceRenderer) Layout(size fyne.Size) {
	// The bubble stops short of its slot's foot, so back-to-back blocks read as
	// separate bubbles; the slot itself still runs to the block's end time.
	size.Height = fyne.Max(0, size.Height-plannerBlockGap)
	link := r.link.(*PlannerIssueLink)
	link.day = r.face.day
	r.bg.Resize(size)
	// Kanban's card insets and corners; a block too short for them keeps what
	// fits, so a quarter hour still shows its line.
	left, right := float32(18), float32(10)
	hasLink := link.URL != nil
	// The link has the top line to itself once the title still fits under it.
	header := hasLink && size.Height >= 2*plannerMinTop+plannerBottomExtra+plannerHeaderHeight+plannerHeaderGap+plannerLineHeight
	top := plannerBlockTop(size.Height, header)
	if !header && size.Height < 2*plannerMinTop+plannerBottomExtra+plannerLineHeight {
		// A quarter hour: its one line centred in what there is.
		top = fyne.Max(1, (size.Height-plannerLineHeight)/2)
	}
	r.face.top = top
	r.bg.CornerRadius = fyne.Min(10, size.Height/2)
	r.face.paint()
	showIf := func(o fyne.CanvasObject, show bool) {
		if show {
			o.Show()
		} else {
			o.Hide()
		}
	}
	if r.delView != nil && r.delView.Visible() && size.Height >= 16 {
		m := r.delView.MinSize()
		h := fyne.Min(m.Height, size.Height-2)
		r.delView.Move(fyne.NewPos(fyne.Max(0, size.Width-m.Width-4), fyne.Max(1, fyne.Min(top-2, size.Height-h-1))))
		r.delView.Resize(fyne.NewSize(m.Width, h))
	}
	x := left
	if r.icon != nil {
		r.icon.Move(fyne.NewPos(left, top+1))
		r.icon.Resize(fyne.NewSize(14, 14))
		showIf(r.icon, size.Height >= 14)
		x = left + 18
	}
	fitLink := func(limit float32) float32 {
		label := link.label
		if limit < plannerMinTextWidth {
			label = "" // too narrow for even a letter and an ellipsis
		} else if plannerLinkWidth(label) > limit {
			label = plannerFitText(label, limit, plannerLinkSize)
		}
		link.setText(label)
		return plannerLinkWidth(label)
	}
	minutes := r.face.duration
	if r.face.project != "" {
		minutes += " · " + r.face.project
	}
	if r.face.finished {
		minutes = r.face.duration + " · Done"
	}
	titleX, titleY := x, top
	titleRight := size.Width - right
	if header {
		w := fitLink(fyne.Max(0, size.Width-x-right))
		r.link.Move(fyne.NewPos(x-1, top-1))
		r.link.Resize(fyne.NewSize(w+2, plannerHeaderHeight))
		titleX, titleY = left, top+plannerHeaderHeight+plannerHeaderGap
	} else if hasLink {
		w := fitLink(fyne.Max(0, (size.Width-x-right)/2))
		r.link.Move(fyne.NewPos(fyne.Max(0, size.Width-w-right), top-1))
		r.link.Resize(fyne.NewSize(w+2, fyne.Min(plannerHeaderHeight, size.Height-top)))
		titleRight -= w + 6
	}
	showIf(r.link, hasLink && size.Height >= 16 && link.Text != "")
	// The minutes take the bottom line once a line is left after the title's
	// first.
	timeLine := size.Height >= titleY+plannerLineHeight+16+top+plannerBottomExtra
	bottom := size.Height - top - plannerBottomExtra
	if timeLine {
		bottom -= 16
	}
	lines := int((bottom - titleY) / plannerLineHeight)
	lines = max(1, min(lines, plannerTitleLines))
	titleWidth := titleRight - titleX
	var wrapped []string
	if titleWidth >= plannerMinTextWidth {
		wrapped = plannerWrapLines(r.face.title, titleWidth, plannerTitleSize, lines)
	}
	textHeight := fyne.MeasureText("Ag", plannerTitleSize, fyne.TextStyle{Bold: true}).Height
	for i, t := range r.titles {
		// Never past the block's edge: a line that would cross it is left out.
		y := titleY - 1 + float32(i)*plannerLineHeight
		if i < len(wrapped) && y+textHeight <= size.Height+1 {
			t.Text = wrapped[i]
			t.Move(fyne.NewPos(titleX, y))
			t.Resize(t.MinSize())
			t.Show()
		} else {
			t.Hide()
		}
	}
	timeLine = timeLine && size.Width-left-right >= plannerMinTextWidth
	if timeLine {
		r.minutes.Text = plannerFitText(minutes, size.Width-left-right, 11)
		r.minutes.Move(fyne.NewPos(left, titleY+float32(len(wrapped))*plannerLineHeight+1))
		r.minutes.Resize(r.minutes.MinSize())
	}
	showIf(r.minutes, timeLine)
	r.grip.Position1 = fyne.NewPos(size.Width/2-10, size.Height-3)
	r.grip.Position2 = fyne.NewPos(size.Width/2+10, size.Height-3)
	showIf(r.grip, size.Height >= 18 && !r.face.finished)
}

func (r *plannerTimedFaceRenderer) Refresh() {
	r.Layout(r.face.Size())
	canvas.Refresh(r.face)
}

func (r *plannerTimedFaceRenderer) Objects() []fyne.CanvasObject {
	objects := []fyne.CanvasObject{r.bg, r.stripe}
	if r.icon != nil {
		objects = append(objects, r.icon)
	}
	for _, t := range r.titles {
		objects = append(objects, t)
	}
	objects = append(objects, r.link, r.minutes, r.grip)
	if r.delView != nil {
		objects = append(objects, r.delView) // last: drawn above the title
	}
	return objects
}

func (*plannerTimedFaceRenderer) Destroy() {}

// Keep direct issue access even on a single-increment bubble, without button
// padding or a font's emoji treatment of the external-link arrow.
type PlannerIssueLink struct {
	widget.Hyperlink
	day    *shell.PlannerDayHover
	parent func(on bool) // the block it sits on, kept lit while hovered
	label  string        // repo#number in full; Text may be shortened
	text   *canvas.Text
	under  *canvas.Rectangle // the underline under the pointer, as on Kanban's links
}

func newPlannerIssueLink(raw string) *PlannerIssueLink {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		u = nil
	}
	label := backend.OrDefault(shell.ShortIssueRef(raw), "GH")
	l := &PlannerIssueLink{Hyperlink: widget.Hyperlink{Text: label, URL: u}, label: label}
	l.ExtendBaseWidget(l)
	return l
}

func (l *PlannerIssueLink) CreateRenderer() fyne.WidgetRenderer {
	l.text = canvas.NewText(l.Text, widgets.LogoGold)
	l.text.TextSize = plannerLinkSize
	l.under = canvas.NewRectangle(widgets.LogoGold)
	l.under.Hide()
	return &plannerIssueLinkRenderer{l}
}

type plannerIssueLinkRenderer struct{ l *PlannerIssueLink }

func (r *plannerIssueLinkRenderer) Layout(size fyne.Size) {
	l := r.l
	m := l.text.MinSize()
	l.text.Move(fyne.NewPos(1, 0))
	l.text.Resize(m)
	l.under.Move(fyne.NewPos(1, fyne.Min(size.Height-1, m.Height-1)))
	l.under.Resize(fyne.NewSize(plannerLinkWidth(l.text.Text), 1))
}

func (r *plannerIssueLinkRenderer) MinSize() fyne.Size {
	return r.l.text.MinSize().AddWidthHeight(2, 0)
}

func (r *plannerIssueLinkRenderer) Refresh() {
	r.Layout(r.l.Size())
	r.l.text.Refresh()
	r.l.under.Refresh()
}

func (r *plannerIssueLinkRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.l.text, r.l.under}
}

func (*plannerIssueLinkRenderer) Destroy() {}

// underline shows or hides the link's underline.
func (l *PlannerIssueLink) underline(on bool) {
	if l.under == nil {
		return
	}
	if on {
		l.under.Show()
	} else {
		l.under.Hide()
	}
	l.under.Refresh()
}

// setText shows s, the label as it fits the block.
func (l *PlannerIssueLink) setText(s string) {
	l.Text = s
	if l.text != nil && l.text.Text != s {
		l.text.Text = s
		l.Refresh()
	}
}

func plannerLinkWidth(s string) float32 {
	return fyne.MeasureText(s, plannerLinkSize, fyne.TextStyle{}).Width
}

func (*PlannerIssueLink) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (l *PlannerIssueLink) MouseIn(*desktop.MouseEvent) {
	l.underline(true)
	if l.day != nil {
		l.day.Enter(l)
	}
	if l.parent != nil {
		l.parent(true)
	}
}

func (*PlannerIssueLink) MouseMoved(*desktop.MouseEvent) {}

func (l *PlannerIssueLink) MouseOut() {
	l.underline(false)
	if l.day != nil {
		l.day.Leave(l)
	}
	if l.parent != nil {
		l.parent(false)
	}
}

// plannerMinTextWidth is the narrowest space a block puts text in.
const plannerMinTextWidth = float32(16)

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

// plannerWrapLines wraps s to at most max lines of width, at word breaks; the
// last line ends in an ellipsis when the title runs on past it.
func plannerWrapLines(s string, width, size float32, max int) []string {
	words := strings.Fields(s)
	var lines []string
	line := ""
	for i := 0; i < len(words); i++ {
		next := strings.TrimSpace(line + " " + words[i])
		if line != "" && fyne.MeasureText(next, size, fyne.TextStyle{Bold: true}).Width > width {
			if len(lines) == max-1 {
				return append(lines, plannerFitText(strings.Join(append([]string{line}, words[i:]...), " "), width, size))
			}
			lines = append(lines, plannerFitText(line, width, size)) // a word too long for a line
			line = words[i]
			continue
		}
		line = next
	}
	if line != "" || len(lines) == 0 {
		lines = append(lines, plannerFitText(line, width, size))
	}
	return lines
}
