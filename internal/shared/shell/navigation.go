package shell

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/widgets"
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
	case theme.ColorNamePrimary, theme.ColorNameButton, theme.ColorNameHyperlink:
		return widgets.LogoGold
	case theme.ColorNameSeparator:
		return BlendColor(widgets.LogoGold, color.Black, 0.25)
	case theme.ColorNameForegroundOnPrimary, theme.ColorNameForegroundOnWarning:
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

func GoldChoice(control fyne.CanvasObject) fyne.CanvasObject {
	return container.NewThemeOverride(widgets.AnimatedChoice(control), RadioTheme{Theme: theme.Current()})
}

func GoldTickChoice(check *widget.Check) fyne.CanvasObject {
	return container.NewThemeOverride(widgets.AnimatedTickChoice(check), RadioTheme{Theme: theme.Current()})
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
	return OutlineTag(text, widgets.LogoGold)
}

// CodeReviewTag identifies a single review wherever its work is displayed.
func CodeReviewTag() fyne.CanvasObject {
	return OutlineTag("Code review", PlannerPlanColor)
}

var MissingMeetingRecordingMaroon = color.NRGBA{R: 0xA6, G: 0x4B, B: 0x60, A: 0xff}

func MissingMeetingRecordingTag() fyne.CanvasObject {
	return OutlineTag("Missing Meeting Recording", MissingMeetingRecordingMaroon)
}

// LegacyTag marks an entry pending at the SOP cutover: its Worklog lists its
// commits instead of the commits naming the Worklog.
func LegacyTag() fyne.CanvasObject { return SavedTag("Legacy") }

// PendingPushTag marks a commit, or an entry of commits, still only on this
// machine: its Worklog's reference is added before it is pushed.
func PendingPushTag() fyne.CanvasObject { return SavedTag("Pending Push") }

// AlreadyMergedTag marks an entry whose commits were merged before they could
// name its Worklog.
func AlreadyMergedTag() fyne.CanvasObject {
	return OutlineTag("Already merged", MissingMeetingRecordingMaroon)
}

// OutlineTag is a compact caption with coloured ink and a rounded outline.
func OutlineTag(text string, ink color.Color) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 12
	bg.StrokeColor = ink
	bg.StrokeWidth = 1
	lbl := canvas.NewText(text, ink)
	lbl.TextSize = theme.CaptionTextSize()
	lbl.TextStyle = fyne.TextStyle{Bold: true}
	// Wider at the sides than above and below, so a short word such as
	// "Legacy" does not sit tight against the rounded ends.
	pad := layout.NewCustomPaddedLayout(theme.Padding(), theme.Padding(), theme.InnerPadding(), theme.InnerPadding())
	return container.NewStack(bg, container.New(pad, container.NewCenter(lbl)))
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
	// Views are the tabs as the bar holds them (the button styled is inside
	// each), for finding a tab on screen.
	Views []fyne.CanvasObject
	page  *fyne.Container
	body  *fyne.Container
	// badge wraps the gear and says when a newer release is waiting.
	badge    *updateBadge
	settings int
	// shown runs when the tab of that name is brought up (see WhenShown).
	shown map[string]func()
	// hover is the gold pill travelling over the tabs.
	hover *tabHover
	// looks are the tabs' surfaces, painted behind them in back; nil for the
	// gear. tapAt is where the last click on tab tapTab landed.
	looks  []*tabLook
	back   *fyne.Container
	tapTab int
	tapAt  fyne.Position
}

// WhenShown runs fn each time the tab called name is brought up, for a page
// that skips redraws while it is out of sight and catches up on its return.
func (t *pillTabs) WhenShown(name string, fn func()) {
	if t.shown == nil {
		t.shown = map[string]func(){}
	}
	t.shown[name] = fn
}

func NewPillTabs(items ...*container.TabItem) *pillTabs {
	t := &pillTabs{Items: items, tapTab: -1}
	t.ExtendBaseWidget(t)
	// The bar's gaps are fixed from the pill theme rather than read while
	// laying out, so the update badge can relayout the bar on every frame of
	// its animation without the gaps snapping to the app theme's.
	pill := PillTheme{Theme: GoldTheme{Theme: theme.DefaultTheme()}}
	gap := pill.Size(theme.SizeNamePadding)
	row := container.New(layout.NewCustomPaddedHBoxLayout(gap))
	right := container.New(layout.NewCustomPaddedHBoxLayout(gap))
	t.hover = &tabHover{pill: widgets.NewHoverPill()}
	var settings fyne.CanvasObject
	for i, item := range items {
		i := i
		var button *widget.Button
		// view is what the bar holds: the tab itself, which hears the
		// pointer, around the button it styles.
		var view fyne.CanvasObject
		if item.Text == SettingsTabName {
			gear := newGearButton(item.Icon, func() { t.SelectIndex(i) })
			button = &gear.Button
			view = gear
			t.badge = newUpdateBadge(gear, tabBarTheme{Theme: pill})
			t.badge.onTap = func() { t.SelectIndex(i) }
			t.badge.onInk = func() { t.styleButton(i) }
			t.settings = i
			// The same travelling hover, except while the update capsule is up:
			// it brightens itself. Its click keeps its own animation.
			badgeHover := gear.hover
			gear.hover = func(on bool) {
				badgeHover(on)
				t.hover.over(gear, on && !t.badge.shown)
			}
		} else {
			tab := newTabButton(item.Text, item.Icon, func() { t.SelectIndex(i) })
			tab.hover = func(on bool) { t.hover.over(tab, on) }
			tab.tap = func(at fyne.Position) { t.tapTab, t.tapAt = i, at }
			button = &tab.Button
			view = tab
		}
		var look *tabLook
		if item.Text != SettingsTabName {
			look = newTabLook(view, pill)
		}
		t.looks = append(t.looks, look)
		if hover, ok := any(button).(interface{ WorklogSetHoverFade(time.Duration) }); ok {
			hover.WorklogSetHoverFade(SlowFade)
		}
		t.Buttons = append(t.Buttons, button)
		t.Views = append(t.Views, view)
		t.styleButton(i)
		if item.Text == SettingsTabName {
			settings = container.NewCenter(t.badge)
		} else {
			if item.Text == KanbanTabName || item.Text == PlannerTabName || item.Text == ProjectsTabName {
				right.Add(look.over)
			} else {
				row.Add(look.over)
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
	rowScroll := container.NewHScroll(row)
	bar := container.New(tabBarLayout{gap: gap}, rowScroll, right)
	// The tabs' surfaces are painted behind the bar; the hover pill is drawn
	// over the tabs.
	var surfaces []fyne.CanvasObject
	for _, l := range t.looks {
		if l != nil {
			surfaces = append(surfaces, l.bg, l.fill)
		}
	}
	t.back = container.New(lookLayer{t: t}, surfaces...)
	layer := container.New(pillLayer{h: t.hover}, t.hover.pill.Rect)
	t.hover.layer = layer
	rowScroll.OnScrolled = func(fyne.Position) { t.placeLooks() }
	if t.badge != nil {
		t.badge.relayout = func(final bool) {
			Reflow(final, bar, right, settings)
			t.placeLooks()
		}
	}
	for i, l := range t.looks {
		if l != nil && i == t.current {
			l.set(1)
		}
	}
	header := container.NewPadded(container.NewThemeOverride(container.New(barStack{}, t.back, bar, layer), tabBarTheme{Theme: pill}))
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
		if t.badge != nil {
			t.badge.setSelected(index == t.current)
		}
		if t.badge != nil && t.badge.inked {
			name = theme.ColorNameForegroundOnPrimary
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

// SelectName opens the tab called name.
func (t *pillTabs) SelectName(name string) {
	for i, item := range t.Items {
		if item.Text == name {
			t.SelectIndex(i)
			return
		}
	}
}

// ShowPlannerDate opens the Sprint Calendar on the week and month holding date.
func (ui *UI) ShowPlannerDate(date string) {
	if k := ui.Kanban; k != nil && backend.ValidPlannerDate(date) {
		k.Week = backend.WeekStartOf(date)
		k.Month = date[:7]
	}
	if ui.Tabs != nil {
		ui.Tabs.SelectName(PlannerTabName)
	}
}

func (t *pillTabs) SelectIndex(index int) {
	if index < 0 || index >= len(t.Items) || index == t.current || t.Items[index].Disabled() {
		return
	}
	DismissSummaryTooltip()
	if c := fyne.CurrentApp().Driver().CanvasForObject(t); c != nil {
		stopFilterFade(c)
	}
	if old := t.Selected(); old != nil {
		old.Content.Hide()
	}
	previous := t.current
	t.current = index
	// The gold drains from the tab let go of and spreads over the one picked,
	// from where the click landed.
	if l := t.looks[previous]; l != nil {
		l.to(0, nil)
	}
	if l := t.looks[index]; l != nil {
		var centre *fyne.Position
		if t.tapTab == index {
			at := t.tapAt.Subtract(fyne.CurrentApp().Driver().AbsolutePositionForObject(l.tab))
			centre = &at
		}
		l.to(1, centre)
	}
	t.tapTab = -1
	t.styleButton(previous)
	item := t.Items[index]
	t.page.Objects = []fyne.CanvasObject{item.Content}
	item.Content.Show()
	t.page.Refresh()
	t.styleButton(index)
	if fn := t.shown[item.Text]; fn != nil {
		fn()
	}
	if t.OnSelected != nil {
		t.OnSelected(item)
	}
}

// tabBarLayout puts the scrolling row of tabs on the left, filling what the
// right-hand group (Kanban, Sprint Calendar, Projects, the gear) leaves of the
// width.
type tabBarLayout struct{ gap float32 }

func (l tabBarLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	row, right := objects[0].MinSize(), objects[1].MinSize()
	return fyne.NewSize(row.Width+l.gap+right.Width, max(row.Height, right.Height))
}

func (l tabBarLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	width := objects[1].MinSize().Width
	objects[1].Move(fyne.NewPos(size.Width-width, 0))
	objects[1].Resize(fyne.NewSize(width, size.Height))
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(max(size.Width-width-l.gap, 0), size.Height))
}

// ShowUpdate lights the gear and opens its "Update Available" capsule while
// a newer release is waiting, so it is seen from any tab, not only Settings.
func (t *pillTabs) ShowUpdate(show bool) {
	if t.badge != nil {
		t.badge.Set(show)
	}
}

// UpdateShown says whether the gear is announcing an update.
func (t *pillTabs) UpdateShown() bool { return t.badge != nil && t.badge.Shown() }
