package shell

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/widgets"
)

// Rounded panels are the card look the Settings page introduced: a rounded
// rectangle a shade off the background, so a group of controls reads as its
// own surface. The fill blends toward the foreground rather than using the
// input background, which would swallow the text fields inside.

// panelSide is the inset at a panel's left and right; panelEnd at its top and
// bottom.
func panelSide() float32 { return theme.InnerPadding() * 2.5 }
func panelEnd() float32  { return theme.InnerPadding() * 1.5 }

func panelBackground() *canvas.Rectangle {
	bg := canvas.NewRectangle(BlendColor(
		theme.Color(theme.ColorNameBackground),
		theme.Color(theme.ColorNameForeground),
		0.08,
	))
	bg.CornerRadius = CellCornerRadius
	return bg
}

// noShadowTheme drops the shade a scroll paints at an edge it can scroll
// past. Inside a panel the scroll bar already says there is more, and the
// shade smudged the panel's flat fill.
type noShadowTheme struct{ fyne.Theme }

func (t noShadowTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameShadow {
		return color.Transparent
	}
	return t.Theme.Color(name, v)
}

// NoScrollShadow drops scroll shades inside content. A theme override only
// reaches what is under it when it is applied, so content swapped into a
// panel later (a new scroll) needs wrapping itself.
func NoScrollShadow(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewThemeOverride(content, noShadowTheme{Theme: theme.Current()})
}

func insetPanel(content fyne.CanvasObject, top, bottom, side float32) fyne.CanvasObject {
	return container.NewStack(panelBackground(),
		container.New(layout.NewCustomPaddedLayout(top, bottom, side, side), NoScrollShadow(content)))
}

// Panel draws content inset inside a rounded panel. Content that scrolls
// itself scrolls inside the panel, which stays put.
func Panel(content fyne.CanvasObject) fyne.CanvasObject {
	return insetPanel(content, panelEnd(), panelEnd(), panelSide())
}

// PanelFrame is a panel's background with no inset: for content that brings
// its own, such as a PanelScroll.
func PanelFrame(content fyne.CanvasObject) fyne.CanvasObject {
	return insetPanel(content, 0, 0, 0)
}

// PanelScroll scrolls content inside a PanelFrame. The panel's insets sit
// inside the scroll: at rest the content keeps its margin, and once it runs
// over, it scrolls right to the panel's edge instead of being cut short a
// margin before it. The scroll bar runs along the panel's own edge. tight
// narrows the side inset, for a narrow column.
func PanelScroll(content fyne.CanvasObject, tight bool) *container.Scroll {
	side, end := panelSide(), panelEnd()
	if tight {
		side = theme.InnerPadding()
	}
	return container.NewVScroll(
		container.New(layout.NewCustomPaddedLayout(end, end, side, side), content))
}

// PanelHead insets a panel's fixed heading, over a PanelScroll that starts
// under it.
func PanelHead(content fyne.CanvasObject, tight bool) fyne.CanvasObject {
	side := panelSide()
	if tight {
		side = theme.InnerPadding()
	}
	return container.New(layout.NewCustomPaddedLayout(panelEnd(), 0, side, side), content)
}

// ScrollPanel is a panel whose content scrolls vertically while the panel
// stays on screen.
func ScrollPanel(content fyne.CanvasObject) (fyne.CanvasObject, *container.Scroll) {
	scroll := PanelScroll(content, false)
	return PanelFrame(scroll), scroll
}

// SectionTitle is a gold heading with a separator under it, used to name a
// panel or a group inside one.
func SectionTitle(title string) fyne.CanvasObject {
	t := canvas.NewText(title, widgets.LogoGold)
	t.TextSize = theme.TextSize() + 4
	t.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewVBox(t, widget.NewSeparator())
}

// PanelDivider is a line in the panels' fill colour, for dividing parts of a
// page outside any panel. Inside a panel keep widget.NewSeparator.
func PanelDivider() fyne.CanvasObject {
	line := panelBackground()
	line.CornerRadius = 0
	line.SetMinSize(fyne.NewSize(0, 2))
	return line
}

// PageInset keeps a tab's content, panels included, off the window's edges.
func PageInset(content fyne.CanvasObject) *fyne.Container {
	side := theme.InnerPadding() * 1.5
	return container.New(layout.NewCustomPaddedLayout(theme.InnerPadding(), theme.InnerPadding(), side, side), content)
}

// VGap is empty vertical space of the given height.
func VGap(height float32) fyne.CanvasObject {
	gap := canvas.NewRectangle(color.Transparent)
	gap.SetMinSize(fyne.NewSize(0, height))
	return gap
}

// OrgBubble is the card look of a worklog (see StripeBubble), lighting up
// under the pointer on its own. Log Work's day cards and the Status day
// panel share it.
func OrgBubble(accent color.Color, body fyne.CanvasObject) fyne.CanvasObject {
	return HoverBubble(accent, body)
}

// LeadGap puts a gap before obj, such as between a search field and the
// button beside it.
func LeadGap(obj fyne.CanvasObject) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(0, 0, theme.InnerPadding(), 0), obj)
}
