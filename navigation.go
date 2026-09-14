package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var logoGold = color.NRGBA{R: 0xcd, G: 0xa2, B: 0x36, A: 0xff}

// Keep semantic colours (orgs, drafts, success and warnings) separate from
// the app's brand accent. All other theme choices follow the system theme.
type goldTheme struct{ fyne.Theme }

func (t goldTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if darkSurface(t.Theme, variant) {
		var gray uint8
		switch name {
		case theme.ColorNameBackground:
			gray = 0x18
		case theme.ColorNameOverlayBackground, theme.ColorNameInnerWindowBorder:
			gray = 0x24
		case theme.ColorNameMenuBackground, theme.ColorNameDisabledButton:
			gray = 0x28
		case theme.ColorNameInputBackground, theme.ColorNameScrollBarBackground:
			gray = 0x20
		case theme.ColorNameInputBorder:
			gray = 0x3a
		}
		if gray != 0 {
			return color.NRGBA{R: gray, G: gray, B: gray, A: 0xff}
		}
	}
	switch name {
	case theme.ColorNameButton, theme.ColorNameHyperlink:
		return logoGold
	case theme.ColorNameForegroundOnWarning:
		return color.NRGBA{R: 0x1c, G: 0x18, B: 0x0d, A: 0xff}
	case theme.ColorNameFocus, theme.ColorNameSelection:
		return withAlpha(logoGold, 0.30)
	}
	return t.Theme.Color(name, variant)
}

type pillTheme struct{ fyne.Theme }

func darkSurface(base fyne.Theme, variant fyne.ThemeVariant) bool {
	r, g, b, _ := base.Color(theme.ColorNameBackground, variant).RGBA()
	return r+g+b < 3*0x8000
}

type radioTheme struct{ fyne.Theme }

func (t radioTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNamePrimary {
		return logoGold
	}
	return t.Theme.Color(name, variant)
}

func (t pillTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		return logoGold
	case theme.ColorNameForegroundOnPrimary:
		return color.NRGBA{R: 0x1c, G: 0x18, B: 0x0d, A: 0xff}
	case theme.ColorNameButton:
		if darkSurface(t.Theme, variant) {
			return color.NRGBA{R: 0x28, G: 0x28, B: 0x28, A: 0xff}
		}
		return theme.DefaultTheme().Color(name, variant)
	case theme.ColorNameForegroundOnWarning:
		return t.Theme.Color(theme.ColorNameForeground, variant)
	}
	return t.Theme.Color(name, variant)
}

// Scope the green and rounded shape to push actions, including disabled ones.
type pushTheme struct {
	fyne.Theme
	compact bool
}

func (t pushTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 0x28, G: 0x80, B: 0x48, A: 0xff}
	case theme.ColorNameForegroundOnPrimary:
		return color.White
	}
	return t.Theme.Color(name, variant)
}

func (t pushTheme) Size(name fyne.ThemeSizeName) float32 {
	if t.compact && name == theme.SizeNameInnerPadding {
		return 6
	}
	if name == theme.SizeNameButtonRadius {
		return 100
	}
	return t.Theme.Size(name)
}

func ovalPush(button *widget.Button) fyne.CanvasObject {
	return container.NewThemeOverride(withPointerCursor(button), pushTheme{Theme: theme.Current()})
}

func compactPush(button *widget.Button) fyne.CanvasObject {
	styled := container.NewThemeOverride(withPointerCursor(button), pushTheme{Theme: theme.Current(), compact: true})
	// The taller Edit/Delete controls must not stretch the compact button.
	return container.New(compactPushLayout{}, styled)
}

type compactPushLayout struct{}

func (compactPushLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return objects[0].MinSize().AddWidthHeight(8, 0)
}

func (compactPushLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	button := objects[0]
	want := button.MinSize().AddWidthHeight(8, 0)
	button.Resize(want)
	button.Move(fyne.NewPos((size.Width-want.Width)/2, (size.Height-want.Height)/2))
}

func goldChoice(control fyne.CanvasObject) fyne.CanvasObject {
	return container.NewThemeOverride(control, radioTheme{Theme: theme.Current()})
}

func (t pillTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameButtonRadius:
		return 24
	case theme.SizeNameInnerPadding:
		return 12
	case theme.SizeNamePadding:
		return 8
	}
	return t.Theme.Size(name)
}

// savedTag is a small pill-shaped label. Used on saved-row cards to mark that
// the row's issue still carries other unlogged commits — one glance says
// "there is more work here" without opening the pending list above.
func savedTag(text string) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.NRGBA{R: 0x28, G: 0x80, B: 0x48, A: 0xff})
	bg.CornerRadius = 8
	lbl := canvas.NewText(text, color.White)
	lbl.TextSize = theme.CaptionTextSize()
	lbl.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewStack(bg, container.NewPadded(container.NewCenter(lbl)))
}

func appLogo(size float32) fyne.CanvasObject {
	logo := canvas.NewImageFromResource(fyne.NewStaticResource("icon.png", appIconPNG))
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSquareSize(size))
	return container.NewCenter(logo)
}

// pillTabs keeps the app's existing tab selection contract, while native
// buttons retain keyboard focus, hover feedback and pointer cursors.
type pillTabs struct {
	widget.BaseWidget
	Items      []*container.TabItem
	OnSelected func(*container.TabItem)
	current    int
	buttons    []*widget.Button
	page       *fyne.Container
	body       *fyne.Container
}

func newPillTabs(items ...*container.TabItem) *pillTabs {
	t := &pillTabs{Items: items}
	t.ExtendBaseWidget(t)
	row := container.NewHBox()
	var settings fyne.CanvasObject
	for i, item := range items {
		i := i
		button := widget.NewButtonWithIcon(item.Text, item.Icon, func() { t.SelectIndex(i) })
		t.buttons = append(t.buttons, button)
		t.styleButton(i)
		if item.Text == settingsTabName {
			button.SetText("")
			settings = container.NewCenter(button)
		} else {
			row.Add(button)
		}
		item.Content.Hide()
	}
	t.page = container.NewStack()
	if len(items) > 0 {
		items[0].Content.Show()
		t.page.Add(items[0].Content)
	}
	// Keep every page reachable on a narrow window without compressing labels.
	bar := container.NewBorder(nil, nil, nil, settings, container.NewHScroll(row))
	header := container.NewPadded(container.NewThemeOverride(bar, pillTheme{Theme: goldTheme{Theme: theme.DefaultTheme()}}))
	t.body = container.NewBorder(header, nil, nil, nil, t.page)
	return t
}

func (t *pillTabs) styleButton(index int) {
	button := t.buttons[index]
	if t.Items[index].Text == settingsTabName {
		button.Importance = widget.LowImportance
		name := theme.ColorNameForeground
		if index == t.current {
			name = theme.ColorNamePrimary
		}
		button.SetIcon(theme.NewColoredResource(theme.SettingsIcon(), name))
	} else if index == t.current {
		button.Importance = widget.HighImportance
	} else {
		button.Importance = widget.MediumImportance
	}
	button.Refresh()
}

func (t *pillTabs) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.body)
}

func (t *pillTabs) SelectedIndex() int { return t.current }

func (t *pillTabs) Selected() *container.TabItem {
	if t.current < 0 || t.current >= len(t.Items) {
		return nil
	}
	return t.Items[t.current]
}

func (t *pillTabs) SelectIndex(index int) {
	if index < 0 || index >= len(t.Items) || index == t.current || t.Items[index].Disabled() {
		return
	}
	if old := t.Selected(); old != nil {
		old.Content.Hide()
	}
	previous := t.current
	t.current = index
	t.styleButton(previous)
	item := t.Items[index]
	t.page.Objects = []fyne.CanvasObject{item.Content}
	item.Content.Show()
	t.page.Refresh()
	t.styleButton(index)
	if t.OnSelected != nil {
		t.OnSelected(item)
	}
}
