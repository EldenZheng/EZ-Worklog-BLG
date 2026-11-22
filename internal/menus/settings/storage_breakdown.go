package settings

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// Stable category colours keep the legend and bar in sync without cycling
// through the six organisation colours when there are more storage categories.
var storageCategoryColours = map[backend.StorageKind]color.NRGBA{
	backend.StorageLogged:         {R: 0x3b, G: 0x82, B: 0xf6, A: 0xff},
	backend.StorageDrafts:         {R: 0xcd, G: 0xa2, B: 0x36, A: 0xff},
	backend.StorageIgnored:        {R: 0x9c, G: 0xa3, B: 0xaf, A: 0xff},
	backend.StorageProjectHistory: {R: 0xa7, G: 0x8b, B: 0xfa, A: 0xff},
	backend.StoragePlans:          {R: 0x22, G: 0xb8, B: 0xcf, A: 0xff},
	backend.StorageIssueCache:     {R: 0xf9, G: 0x73, B: 0x16, A: 0xff},
	backend.StorageSupport:        {R: 0x4a, G: 0xde, B: 0x80, A: 0xff},
	backend.StorageExports:        {R: 0xfb, G: 0x71, B: 0x85, A: 0xff},
	backend.StorageHistory:        {R: 0x14, G: 0xb8, B: 0xa6, A: 0xff},
	backend.StorageLegacyBackup:   {R: 0xc0, G: 0x84, B: 0x57, A: 0xff},
	backend.StorageOther:          {R: 0xe8, G: 0x79, B: 0xf9, A: 0xff},
}

func storageBreakdown(ui *shell.UI, cfg backend.Config, usage backend.StorageUsage, open bool, clear func(backend.Config, backend.StorageCategory), toggled func(bool), reflow func(bool)) fyne.CanvasObject {
	var parts []shell.MeterPart
	rows := newStorageRows()
	for _, category := range usage.Categories {
		ink := storageCategoryColours[category.Kind]
		share := float32(0)
		if usage.StoredDataBytes > 0 {
			share = float32(float64(category.Bytes) / float64(usage.StoredDataBytes))
		}
		if category.Bytes > 0 {
			parts = append(parts, shell.MeterPart{Key: string(category.Kind), Fill: ink, Weight: share})
		}
		detail := backend.FormatBytes(uint64(category.Bytes))
		if category.Unit != "" {
			detail += fmt.Sprintf(" · %d %s", category.Count, category.Unit)
		}
		entry := storageLegendEntry(ink, category.Name, fmt.Sprintf("%.1f%%", float64(share)*100), detail)
		var action fyne.CanvasObject
		var fade *shell.Fader
		var del *shell.FadeIconButton
		if category.CanClear {
			category := category
			if category.Bytes > 0 {
				// The Delete shows only while the row is lit, fading in and out
				// with the gold hover as on a saved Log Work card.
				del = shell.FadeDeleteIcon(func() { clear(cfg, category) })
				fade = shell.NewFader(del)
				action = container.NewCenter(fade.View)
			} else {
				// Nothing to clear; keep the icon's room so the rows line up.
				space := canvas.NewRectangle(color.Transparent)
				space.SetMinSize(shell.NewFadeIconButton(theme.DeleteIcon(), nil).MinSize())
				action = space
			}
		} else {
			kept := canvas.NewText("Kept", theme.Color(theme.ColorNamePlaceHolder))
			kept.TextSize = theme.CaptionTextSize()
			action = container.NewCenter(kept)
		}
		rows.add(container.NewBorder(nil, nil, nil, action, entry), fade, del)
	}
	caption := widget.NewLabel("Percentages of local data + backups (app executable excluded).")
	caption.Wrapping = fyne.TextWrapWord
	total := widget.NewLabel("Total: " + backend.FormatBytes(uint64(usage.StoredDataBytes)))
	total.Wrapping = fyne.TextWrapWord
	bar := ui.ProgressBarParts("settings/storage-breakdown", ui.Store.Dir, parts, 6)
	body := container.NewVBox(goldText(total), bar, caption, rows.View)
	return storageFold("Data + Backups", body, open, toggled, reflow)
}

// storageFold is a storage section under a gold heading that folds it away
// and back. The heading lights with the gold hover under the pointer and has
// its underline on its own line below it.
func storageFold(title string, body fyne.CanvasObject, open bool, toggled func(bool), reflow func(bool)) *fyne.Container {
	header := canvas.NewText(title, widgets.LogoGold)
	header.TextSize, header.TextStyle.Bold = theme.TextSize()+4, true
	head, area := shell.CollapseLine(header, nil, body, open, 0, toggled, reflow)
	// Room inside the gold hover, so the title and arrow do not crowd its edge,
	// and the usual gold ripple from a click.
	tap := head.(*shell.Tappable)
	pad := theme.Padding()
	tap.Content = container.New(layout.NewCustomPaddedLayout(pad, pad, pad*2, pad*2), tap.Content)
	tap.Ripple = true
	lit := newStorageRows()
	lit.light(tap, nil)
	heading := container.NewThemeOverride(container.NewVBox(lit.View, widget.NewSeparator()), chunkySettingsTheme{Theme: theme.Current()})
	return container.NewVBox(heading, area)
}

// storageRows is a column of rows with the Projects lists' hover: a soft gold
// pill fades in behind the row under the pointer and slides from row to row
// as the pointer moves down. View is what to place.
type storageRows struct {
	View    *fyne.Container
	rows    *fyne.Container
	pill    *widgets.HoverPill
	hovered int
}

func newStorageRows() *storageRows {
	l := &storageRows{rows: container.NewVBox(), pill: widgets.NewHoverPill(), hovered: -1}
	l.View = container.New(&storageRowsLayout{l: l}, l.pill.Rect, l.rows)
	return l
}

// add puts content in as a row that lights under the pointer. fade, when set,
// shows the row's Delete while it is lit; del keeps the row lit while the
// pointer is on it.
func (l *storageRows) add(content fyne.CanvasObject, fade *shell.Fader, del *shell.FadeIconButton) {
	row := shell.NewTappable(container.New(layout.NewCustomPaddedLayout(0, 0, 4, 4), content), nil)
	row.Look = desktop.DefaultCursor
	on := l.light(row, func(in bool) {
		if fade != nil {
			fade.FadeTo(in)
		}
	})
	if del != nil {
		del.SetHoverParent(on)
	}
}

// light adds row to the column, lit under the pointer; also, when set, hears
// the light come and go. It returns the row's hover, for controls on the row
// that keep it lit.
func (l *storageRows) light(row *shell.Tappable, also func(bool)) func(bool) {
	i := len(l.rows.Objects)
	on := func(in bool) {
		l.hover(i, in)
		if also != nil {
			also(in)
		}
	}
	row.OnHover = on
	l.rows.Add(row)
	return on
}

func (l *storageRows) hover(i int, in bool) {
	switch {
	case in:
		l.moveTo(i)
	case l.hovered == i:
		l.moveTo(-1)
	}
}

func (l *storageRows) moveTo(i int) {
	if i >= len(l.rows.Objects) {
		return
	}
	l.hovered = i
	var pos fyne.Position
	var size fyne.Size
	if i >= 0 {
		row := l.rows.Objects[i]
		pos, size = row.Position(), row.Size()
	}
	l.pill.MoveTo(pos, size, i >= 0)
}

// storageRowsLayout gives the rows the whole area and keeps the pill on the
// lit row; the pill places itself as it moves.
type storageRowsLayout struct{ l *storageRows }

func (s *storageRowsLayout) MinSize([]fyne.CanvasObject) fyne.Size { return s.l.rows.MinSize() }
func (s *storageRowsLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	l := s.l
	l.rows.Move(fyne.Position{})
	l.rows.Resize(size)
	if l.hovered >= 0 && l.hovered < len(l.rows.Objects) {
		row := l.rows.Objects[l.hovered]
		l.pill.Fit(row.Position(), row.Size())
	}
}

// storageLegendEntry follows the Projects status legend: a colour square, the
// name and its figures on one line. Every row keeps the room of two lines, so
// the rows stand apart, the gold hover has space round the text, and a row
// whose figures must drop under its name is no taller than the rest.
func storageLegendEntry(ink color.Color, name, percent, detail string) fyne.CanvasObject {
	label := canvas.NewText(name, theme.Color(theme.ColorNameForeground))
	label.TextStyle.Bold = true
	share := canvas.NewText(percent, widgets.LogoGold)
	share.TextSize, share.TextStyle.Bold = theme.CaptionTextSize(), true
	readout := canvas.NewText(detail, theme.Color(theme.ColorNamePlaceHolder))
	readout.TextSize = theme.CaptionTextSize()
	figures := container.NewHBox(container.NewCenter(share), container.NewCenter(readout))
	dot := canvas.NewRectangle(ink)
	dot.CornerRadius = 3
	dot.SetMinSize(fyne.NewSquareSize(10))
	return container.New(&storageLegendLayout{name: name, label: label, figures: figures, dot: dot}, dot, label, figures)
}

type storageLegendLayout struct {
	name    string
	label   *canvas.Text
	figures fyne.CanvasObject
	dot     *canvas.Rectangle
}

func (l *storageLegendLayout) textHeight() float32 {
	return fyne.MeasureText("M", l.label.TextSize, l.label.TextStyle).Height
}

func (l *storageLegendLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	height := l.textHeight() + 2 + l.figures.MinSize().Height
	return fyne.NewSize(max(l.figures.MinSize().Width+22, 100), height+theme.Padding()*2)
}

func (l *storageLegendLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	width := max(size.Width-22, 1)
	textHeight := l.textHeight()
	figures := l.figures.MinSize()
	full := fyne.MeasureText(l.name, l.label.TextSize, l.label.TextStyle).Width
	// As in the Projects status legend, the figures sit beside the name and
	// move under it only when both do not fit. A folded section lays out at
	// zero width; keep one line until it opens.
	wrapped := size.Width > 22 && full+4+figures.Width > width
	text := l.name
	if wrapped {
		text = shell.FitText(l.name, l.label.TextSize, l.label.TextStyle, width)
	}
	if l.label.Text != text {
		l.label.Text = text
		l.label.Refresh()
	}
	if !wrapped {
		row := max(textHeight, figures.Height)
		top := max((size.Height-row)/2, 0)
		l.label.Move(fyne.NewPos(18, top+(row-textHeight)/2))
		l.figures.Move(fyne.NewPos(22+full, top+(row-figures.Height)/2))
		l.dot.Move(fyne.NewPos(4, top+(row-10)/2))
	} else {
		top := max((size.Height-textHeight-2-figures.Height)/2, 0)
		l.label.Move(fyne.NewPos(18, top))
		l.figures.Move(fyne.NewPos(18, top+textHeight+2))
		l.dot.Move(fyne.NewPos(4, top+(textHeight-10)/2))
	}
	l.label.Resize(fyne.NewSize(width, textHeight))
	l.figures.Resize(figures)
	l.dot.Resize(fyne.NewSquareSize(10))
}

func confirmClearStorage(ui *shell.UI, cfg backend.Config, category backend.StorageCategory, busy func() bool, setBusy func(bool), status *widget.Label, measure func()) {
	if busy() {
		return
	}
	var popup *widget.PopUp
	cancel := settingsButton("Cancel", func() { shell.ClosePopUp(popup) })
	confirm := settingsButton("Clear "+category.Name, func() {
		if busy() {
			return
		}
		shell.ClosePopUp(popup)
		setBusy(true)
		status.SetText("Clearing " + category.Name + "…")
		var operationErr error
		ui.Async(func() error {
			if category.Kind == backend.StorageProjectHistory {
				if shell.Hooks.ClearProjectHistory == nil {
					operationErr = fmt.Errorf("project history clearing is unavailable")
					return nil
				}
				operationErr = shell.Hooks.ClearProjectHistory(ui.Store)
			} else {
				operationErr = ui.Store.ClearStorageCategory(cfg, category.Kind)
			}
			return nil
		}, func() {
			setBusy(false)
			if operationErr != nil {
				status.SetText("Could not clear " + category.Name + ": " + operationErr.Error())
				measure()
				return
			}
			refreshAfterStorageClear(ui, category.Kind)
			status.SetText(category.Name + " cleared locally.")
			measure()
		})
	})
	amountText := fmt.Sprintf("Clear %s?\n%s · %d %s", category.Name, backend.FormatBytes(uint64(category.Bytes)), category.Count, category.Unit)
	if category.Kind == backend.StorageHistory {
		amountText += "\nOwner: " + cfg.WorklogOwner
	}
	amount := widget.NewLabel(amountText)
	amount.Wrapping = fyne.TextWrapWord
	message := widget.NewLabel(category.Consequence)
	message.Wrapping = fyne.TextWrapWord
	popup = ui.ShowConfirmation("Clear local data", container.NewVBox(amount, message), shell.RemovalButtons(cancel, confirm))
}

func refreshAfterStorageClear(ui *shell.UI, kind backend.StorageKind) {
	if kind == backend.StorageProjectHistory && shell.Hooks.ReloadProjectHistory != nil {
		shell.Hooks.ReloadProjectHistory(ui)
	}
	if kind == backend.StorageProjectHistory || kind == backend.StorageExports || kind == backend.StorageLegacyBackup {
		return
	}
	if kind == backend.StorageSupport {
		ui.SupportWeeks = map[string]bool{}
	}
	if ui.Kanban != nil {
		if kind == backend.StoragePlans {
			ui.Kanban.Plan.Tasks = map[string]backend.SprintTask{}
		}
		if kind == backend.StorageIssueCache {
			ui.Kanban.Plan.PRIssues = nil
			ui.Kanban.PRAsked = map[string]bool{}
		}
		if (kind == backend.StoragePlans || kind == backend.StorageIssueCache) && ui.Kanban.Body != nil && shell.Hooks.KanbanUIDraw != nil {
			shell.Hooks.KanbanUIDraw(ui.Kanban)
		}
	}
	if kind == backend.StorageIgnored {
		ui.Pending.Count = 0
		for i := range ui.Pending.Groups {
			ui.Pending.Groups[i].Ignored = false
			ui.Pending.Count += len(ui.Pending.Groups[i].Commits)
		}
		ui.ShowIgnored = false
	}
	if kind == backend.StorageLogged || kind == backend.StorageDrafts || kind == backend.StorageHistory {
		ui.ProjItems = nil
		ui.EnsureProjectCache()
		ui.RefreshLoggedShas()
	}
	if ui.RecentBox != nil && shell.Hooks.DrawRecent != nil {
		shell.Hooks.DrawRecent(ui)
	}
	if ui.PendingLoaded && ui.PendingBox != nil && shell.Hooks.RenderPending != nil {
		shell.Hooks.RenderPending(ui)
	}
	if ui.ListBox != nil && shell.Hooks.DrawLogList != nil {
		shell.Hooks.DrawLogList(ui)
	}
}
