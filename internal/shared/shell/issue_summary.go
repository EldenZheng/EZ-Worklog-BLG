package shell

import (
	"fmt"
	"image/color"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/widgets"
)

// IssueSummaryCard uses the same tinted, animated bubble as the other menus.
// A title peek keeps long names available without growing every summary row.
// issue can be a GitHub URL, an owner/repo#number reference, or an unfiled label.
func IssueSummaryCard(cfg backend.Config, issue, title, amount string, actions ...func()) fyne.CanvasObject {
	return IssueSummaryCardForRows(cfg, issue, title, amount, nil, actions...)
}

// IssueSummaryCardForRows retains the Log Work accent for locally known entries.
func IssueSummaryCardForRows(cfg backend.Config, issue, title, amount string, rows []backend.Row, actions ...func()) fyne.CanvasObject {
	var target *url.URL
	ref := ""
	canonical, _ := backend.IssueRefFromAny(issue)
	if owner, repo, num, err := backend.SplitIssue(canonical); err == nil {
		ref = fmt.Sprintf("%s#%d", repo, num)
		target, _ = url.Parse(fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, num))
	}
	if strings.TrimSpace(title) == "" {
		title = backend.OrDefault(issue, "(untitled)")
	}
	if title == issue && ref != "" {
		title = ref
	}
	name := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	name.Truncation = fyne.TextTruncateEllipsis
	name.Wrapping = fyne.TextWrapWord
	accent := IssueSummaryAccent(cfg, issue, title, rows)
	value := canvas.NewText(amount, widgets.LogoGold)
	value.TextSize = theme.CaptionTextSize()
	value.TextStyle = fyne.TextStyle{Bold: true}
	pill := canvas.NewRectangle(TagRestFill)
	pill.CornerRadius = 12
	badge := container.NewStack(pill, container.NewPadded(value))
	meta := widget.NewLabel(ref)
	meta.Truncation = fyne.TextTruncateEllipsis
	// The repo#number in gold behind the issue mark, as Kanban's cards show it.
	var refLine fyne.CanvasObject = meta
	if ref != "" {
		gold := widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold}
		mark := container.NewThemeOverride(widget.NewIcon(backend.SprintItem{}.TypeIcon()), gold)
		refLine = container.NewBorder(nil, nil, container.NewCenter(container.NewGridWrap(fyne.NewSize(16, 16), mark)), nil,
			container.NewThemeOverride(meta, gold))
	}
	footer := container.New(widgets.NewSidesLayout(refLine, badge, 0), refLine, badge)
	var onTap func()
	if target != nil {
		onTap = func() { _ = fyne.CurrentApp().OpenURL(target) }
	}
	if len(actions) > 0 {
		onTap = actions[0]
	}
	peek := newSummaryTitle(name, title, onTap)
	body := container.NewBorder(nil, footer, nil, nil, peek)
	bubble := NewStripeBubble(accent, body)
	bubble.Compact = true
	if onTap == nil {
		area := &HoverArea{Content: bubble, bubble: bubble}
		area.ExtendBaseWidget(area)
		return area
	}
	return NewBubbleTappable(bubble, onTap)
}

// IssueSummaryGrid fits several compact bubbles across a wide summary panel.
func IssueSummaryGrid(cards ...fyne.CanvasObject) *fyne.Container {
	return container.New(widgets.NewFlowGrid(320, 0, 112).CompactColumns(976, 120, 3), cards...)
}

// IssueSummaryPanel keeps its gold heading visible above its own scroll area.
func IssueSummaryPanel(title, summary string, content fyne.CanvasObject, actions ...fyne.CanvasObject) fyne.CanvasObject {
	head := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	head.Wrapping = fyne.TextWrapWord
	caption := widget.NewLabel(summary)
	caption.Wrapping = fyne.TextWrapWord
	heading := container.NewThemeOverride(head, widgets.StatusHeaderTheme{Theme: theme.Current(), Ink: widgets.LogoGold})
	header := container.NewVBox(container.NewBorder(nil, nil, nil, container.NewHBox(actions...), heading), caption)
	return PanelFrame(container.NewBorder(PanelHead(header, false), nil, nil, nil,
		NoScrollShadow(PanelScroll(content, false))))
}

// SummaryTitle shows the complete title while the top of a bubble is hovered.
type SummaryTitle struct {
	widget.Label
	onTap  func()
	title  string
	state  tipState
	parent func(bool)
}

// activeSummaryTitle is the trigger whose tooltip is up, if any.
var activeSummaryTitle tipOwner

// tipOwner is what a tooltip belongs to: the hover catcher closes it when the
// pointer leaves the owner and passes a click on to it.
type tipOwner interface {
	fyne.CanvasObject
	hideTitle()
	Tapped(*fyne.PointEvent)
}

func DismissSummaryTooltip() {
	if activeSummaryTitle != nil {
		activeSummaryTitle.hideTitle()
	}
}

// tipState is a shown tooltip: the overlay, its rise, and the canvas it sits on.
type tipState struct {
	tip    *titleTooltip
	anim   *fyne.Animation
	canvas fyne.Canvas
}

// showTip raises the Meeting update tooltip (a gold-edged box rising into
// place) under owner, or above it when there is no room below. It returns
// false when owner is not on a canvas yet.
func (s *tipState) show(owner tipOwner, text string) bool {
	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapWord
	return s.showBody(owner, label, true)
}

// showBody is show with content of its own in the box. wraps says content
// wraps to the box (420 wide at most); otherwise the box fits content.
func (s *tipState) showBody(owner tipOwner, content fyne.CanvasObject, wraps bool) bool {
	c := fyne.CurrentApp().Driver().CanvasForObject(owner)
	if c == nil || s.tip != nil {
		return false
	}
	DismissSummaryTooltip()
	activeSummaryTitle = owner
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameMenuBackground))
	bg.CornerRadius = CellCornerRadius
	bg.StrokeWidth = 1
	bg.StrokeColor = widgets.LogoGold
	box := container.NewStack(bg, container.NewPadded(content))
	width := max(min(420, c.Size().Width-16), 1)
	if !wraps {
		width = max(min(box.MinSize().Width, c.Size().Width-16), 1)
	}
	box.Resize(fyne.NewSize(width, 0))
	box.Resize(fyne.NewSize(width, box.MinSize().Height))
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(owner)
	x := max(min(at.X, c.Size().Width-width-8), 0)
	y := at.Y + owner.Size().Height + 4
	if y+box.Size().Height > c.Size().Height {
		y = max(at.Y-box.Size().Height-4, 0)
	}
	box.Move(fyne.NewPos(x, y))
	// Fyne routes all hover events to the top overlay. Its hover catcher keeps
	// this tooltip up over the trigger and dismisses it as soon as we leave.
	s.tip = &titleTooltip{body: container.NewWithoutLayout(box), button: owner, at: at}
	s.tip.ExtendBaseWidget(s.tip)
	s.canvas = c
	c.Overlays().Add(s.tip)
	s.anim = fyne.NewAnimation(140*time.Millisecond, func(done float32) {
		box.Move(fyne.NewPos(x, y+6*(1-done)))
		bg.FillColor = MixColor(color.Transparent, theme.Color(theme.ColorNameMenuBackground), done)
		bg.Refresh()
	})
	s.anim.Curve = easeOutQuart
	s.anim.Start()
	return true
}

// hide takes the tooltip down, reporting whether one was up.
func (s *tipState) hide(owner tipOwner) bool {
	if s.tip == nil {
		return false
	}
	if s.anim != nil {
		s.anim.Stop()
		s.anim = nil
	}
	s.canvas.Overlays().Remove(s.tip)
	s.tip = nil
	if activeSummaryTitle == owner {
		activeSummaryTitle = nil
	}
	return true
}

// TipCaption is a caption that shows text in the Meeting update tooltip while
// the pointer is on it, and runs onTap when clicked.
type TipCaption struct {
	widget.BaseWidget
	Content fyne.CanvasObject
	Text    string
	// Body, when set, is shown in the tooltip in place of Text.
	Body  fyne.CanvasObject
	onTap func()
	state tipState
}

// NewTipCaption wraps content (a caption) with a tooltip and a click.
func NewTipCaption(content fyne.CanvasObject, text string, onTap func()) *TipCaption {
	t := &TipCaption{Content: content, Text: text, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *TipCaption) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.Content)
}

func (t *TipCaption) Cursor() desktop.Cursor {
	if t.onTap == nil {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

func (t *TipCaption) MouseIn(*desktop.MouseEvent) {
	if t.Body != nil {
		t.state.showBody(t, t.Body, false)
	} else if t.Text != "" {
		t.state.show(t, t.Text)
	}
}

func (t *TipCaption) MouseMoved(*desktop.MouseEvent) {}
func (t *TipCaption) MouseOut()                      {}

func (t *TipCaption) Tapped(*fyne.PointEvent) {
	t.hideTitle()
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *TipCaption) hideTitle() { t.state.hide(t) }

func newSummaryTitle(label *widget.Label, title string, tapped func()) *SummaryTitle {
	// Copy presentation fields only. An initialized Label's BaseWidget points
	// at that original widget; copying it routes resizes to a different renderer.
	b := &SummaryTitle{Label: widget.Label{
		Text: label.Text, Alignment: label.Alignment, TextStyle: label.TextStyle,
		Wrapping: label.Wrapping, Truncation: label.Truncation,
		Importance: label.Importance, SizeName: label.SizeName,
	}, title: title, onTap: tapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *SummaryTitle) Tapped(e *fyne.PointEvent) {
	b.hideTitle()
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *SummaryTitle) SetHoverParent(parent func(bool)) { b.parent = parent }

func (b *SummaryTitle) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (b *SummaryTitle) MinSize() fyne.Size { return fyne.NewSize(0, 52) }

func (b *SummaryTitle) showTitle() { b.state.show(b, b.title) }

func (b *SummaryTitle) MouseIn(e *desktop.MouseEvent) {
	if b.parent != nil {
		b.parent(true)
	}
	b.showTitle()
}

func (b *SummaryTitle) MouseMoved(*desktop.MouseEvent) {}

func (b *SummaryTitle) MouseOut() {
	if b.state.tip == nil && b.parent != nil {
		b.parent(false)
	}
}

func (b *SummaryTitle) hideTitle() {
	if b.state.hide(b) && b.parent != nil {
		b.parent(false)
	}
}

type titleTooltip struct {
	widget.BaseWidget
	body   *fyne.Container
	button tipOwner
	at     fyne.Position
}

func (t *titleTooltip) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(t.body) }
func (t *titleTooltip) Cursor() desktop.Cursor              { return desktop.PointerCursor }
func (t *titleTooltip) MouseIn(e *desktop.MouseEvent)       { t.MouseMoved(e) }
func (t *titleTooltip) MouseMoved(e *desktop.MouseEvent) {
	p, s := e.AbsolutePosition, t.button.Size()
	if p.X < t.at.X || p.Y < t.at.Y || p.X >= t.at.X+s.Width || p.Y >= t.at.Y+s.Height {
		t.button.hideTitle()
	}
}
func (t *titleTooltip) MouseOut()                 { t.button.hideTitle() }
func (t *titleTooltip) Tapped(e *fyne.PointEvent) { t.button.Tapped(e) }
func (t *titleTooltip) DismissForScroll()         { t.button.hideTitle() }
