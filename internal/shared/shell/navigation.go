package shell

import (
	"image/color"
	"worklog/internal/shared/widgets"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Keep semantic colours (orgs, drafts, success and warnings) separate from
// the app's brand accent. All other theme choices follow the system theme.
type GoldTheme struct{ fyne.Theme }

func (t GoldTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if DarkSurface(t.Theme, variant) {
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
		return widgets.LogoGold
	case theme.ColorNameForegroundOnWarning:
		return color.NRGBA{R: 0x1c, G: 0x18, B: 0x0d, A: 0xff}
	case theme.ColorNameFocus, theme.ColorNameSelection:
		return widgets.WithAlpha(widgets.LogoGold, 0.30)
	}
	return t.Theme.Color(name, variant)
}

type PillTheme struct{ fyne.Theme }

func DarkSurface(base fyne.Theme, variant fyne.ThemeVariant) bool {
	r, g, b, _ := base.Color(theme.ColorNameBackground, variant).RGBA()
	return r+g+b < 3*0x8000
}

type RadioTheme struct{ fyne.Theme }

func (t RadioTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNamePrimary {
		return widgets.LogoGold
	}
	return t.Theme.Color(name, variant)
}

func (t PillTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		return widgets.LogoGold
	case theme.ColorNameForegroundOnPrimary:
		return color.NRGBA{R: 0x1c, G: 0x18, B: 0x0d, A: 0xff}
	case theme.ColorNameButton:
		if DarkSurface(t.Theme, variant) {
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

func OvalPush(button *widget.Button) fyne.CanvasObject {
	return container.NewThemeOverride(widgets.WithPointerCursor(button), pushTheme{Theme: theme.Current()})
}

func CompactPush(button *widget.Button) fyne.CanvasObject {
	styled := container.NewThemeOverride(widgets.WithPointerCursor(button), pushTheme{Theme: theme.Current(), compact: true})
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

func GoldChoice(control fyne.CanvasObject) fyne.CanvasObject {
	return container.NewThemeOverride(widgets.AnimatedChoice(control), RadioTheme{Theme: theme.Current()})
}

func (t PillTheme) Size(name fyne.ThemeSizeName) float32 {
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

// SavedTag is a small pill-shaped label. Used on saved-row cards to mark that
// the row's issue still carries other unlogged commits — one glance says
// "there is more work here" without opening the pending list above.
func SavedTag(text string) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.NRGBA{R: 0x28, G: 0x80, B: 0x48, A: 0xff})
	bg.CornerRadius = 8
	lbl := canvas.NewText(text, color.White)
	lbl.TextSize = theme.CaptionTextSize()
	lbl.TextStyle = fyne.TextStyle{Bold: true}
	return container.NewStack(bg, container.NewPadded(container.NewCenter(lbl)))
}

func AppLogo(size float32) fyne.CanvasObject {
	logo := canvas.NewImageFromResource(fyne.NewStaticResource("icon.png", AppIconPNG))
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
	Buttons    []*widget.Button
	page       *fyne.Container
	body       *fyne.Container
}

func NewPillTabs(items ...*container.TabItem) *pillTabs {
	t := &pillTabs{Items: items}
	t.ExtendBaseWidget(t)
	row := container.NewHBox()
	right := container.NewHBox()
	var settings fyne.CanvasObject
	for i, item := range items {
		i := i
		button := widget.NewButtonWithIcon(item.Text, item.Icon, func() { t.SelectIndex(i) })
		t.Buttons = append(t.Buttons, button)
		t.styleButton(i)
		if item.Text == SettingsTabName {
			button.SetText("")
			settings = container.NewCenter(button)
		} else {
			if item.Text == KanbanTabName || item.Text == PlannerTabName {
				right.Add(button)
			} else {
				row.Add(button)
			}
		}
		item.Content.Hide()
	}
	t.page = container.NewStack()
	if len(items) > 0 {
		items[0].Content.Show()
		t.page.Add(items[0].Content)
	}
	// Keep every page reachable on a narrow window without compressing labels.
	if settings != nil {
		right.Add(settings)
	}
	bar := container.NewBorder(nil, nil, nil, right, container.NewHScroll(row))
	header := container.NewPadded(container.NewThemeOverride(bar, PillTheme{Theme: GoldTheme{Theme: theme.DefaultTheme()}}))
	t.body = container.NewBorder(header, nil, nil, nil, t.page)
	return t
}

func (t *pillTabs) styleButton(index int) {
	button := t.Buttons[index]
	if t.Items[index].Text == SettingsTabName {
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
