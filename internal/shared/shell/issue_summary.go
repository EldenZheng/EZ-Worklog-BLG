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
	name := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	name.Truncation = fyne.TextTruncateEllipsis
	name.Wrapping = fyne.TextWrapWord
	accent := OrgColor(cfg, OrgOf(issue))
	value := canvas.NewText(amount, widgets.LogoGold)
	value.TextSize = theme.CaptionTextSize()
	value.TextStyle = fyne.TextStyle{Bold: true}
	pill := canvas.NewRectangle(TagRestFill)
	pill.CornerRadius = 12
	badge := container.NewStack(pill, container.NewPadded(value))
	meta := widget.NewLabel(ref)
	meta.Truncation = fyne.TextTruncateEllipsis
	footer := container.NewBorder(nil, nil, nil, badge, meta)
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
	return container.New(widgets.NewFlowGrid(320, 0, 112), cards...)
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
	anim   *fyne.Animation
	title  string
	tip    *titleTooltip
	canvas fyne.Canvas
	parent func(bool)
}

var activeSummaryTitle *SummaryTitle

func DismissSummaryTooltip() {
	if activeSummaryTitle != nil {
		activeSummaryTitle.hideTitle()
	}
}

func newSummaryTitle(label *widget.Label, title string, tapped func()) *SummaryTitle {
	b := &SummaryTitle{Label: *label, title: title, onTap: tapped}
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

func (b *SummaryTitle) showTitle() {
	c := fyne.CurrentApp().Driver().CanvasForObject(b)
	if c == nil {
		return
	}
	if b.tip != nil {
		return
	}
	DismissSummaryTooltip()
	activeSummaryTitle = b
	label := widget.NewLabel(b.title)
	label.Wrapping = fyne.TextWrapWord
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameMenuBackground))
	bg.CornerRadius = CellCornerRadius
	bg.StrokeWidth = 1
	bg.StrokeColor = widgets.LogoGold
	box := container.NewStack(bg, container.NewPadded(label))
	width := max(min(420, c.Size().Width-16), 1)
	box.Resize(fyne.NewSize(width, 0))
	box.Resize(fyne.NewSize(width, box.MinSize().Height))
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(b)
	x := max(min(at.X, c.Size().Width-width-8), 0)
	y := at.Y + b.Size().Height + 4
	if y+box.Size().Height > c.Size().Height {
		y = max(at.Y-box.Size().Height-4, 0)
	}
	box.Move(fyne.NewPos(x, y))
	// Fyne routes all hover events to the top overlay. Its hover catcher keeps
	// this tooltip up over the trigger and dismisses it as soon as we leave.
	b.tip = &titleTooltip{body: container.NewWithoutLayout(box), button: b, at: at}
	b.tip.ExtendBaseWidget(b.tip)
	b.canvas = c
	c.Overlays().Add(b.tip)
	b.anim = fyne.NewAnimation(140*time.Millisecond, func(done float32) {
		box.Move(fyne.NewPos(x, y+6*(1-done)))
		bg.FillColor = MixColor(color.Transparent, theme.Color(theme.ColorNameMenuBackground), done)
		bg.Refresh()
	})
	b.anim.Curve = fyne.AnimationEaseOut
	b.anim.Start()
}

func (b *SummaryTitle) MouseIn(e *desktop.MouseEvent) {
	if b.parent != nil {
		b.parent(true)
	}
	b.showTitle()
}

func (b *SummaryTitle) MouseMoved(*desktop.MouseEvent) {}

func (b *SummaryTitle) MouseOut() {
	if b.tip == nil && b.parent != nil {
		b.parent(false)
	}
}

func (b *SummaryTitle) hideTitle() {
	if b.tip != nil {
		if b.anim != nil {
			b.anim.Stop()
			b.anim = nil
		}
		b.canvas.Overlays().Remove(b.tip)
		b.tip = nil
		if activeSummaryTitle == b {
			activeSummaryTitle = nil
		}
		if b.parent != nil {
			b.parent(false)
		}
	}
}

type titleTooltip struct {
	widget.BaseWidget
	body   *fyne.Container
	button *SummaryTitle
	at     fyne.Position
}

func (t *titleTooltip) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(t.body) }
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
