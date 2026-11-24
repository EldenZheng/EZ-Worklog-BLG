package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/projects"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func refreshStorageMeasurement(t *testing.T, view fyne.CanvasObject) {
	t.Helper()
	var refresh *widget.Button
	walkRendered(view, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "" && b.Icon == theme.ViewRefreshIcon() {
			refresh = b
		}
	})
	if refresh == nil {
		t.Fatal("storage refresh button missing")
	}
	refresh.OnTapped()
}

func storageCategoryClearButton(t *testing.T, view fyne.CanvasObject, name string) *shell.FadeIconButton {
	t.Helper()
	found := findStorageClear(t, view, name)
	if found == nil {
		t.Fatalf("clear action for %q missing", name)
	}
	return found
}

// findStorageClear is the Delete on the storage row named name, or nil when
// the row has nothing to clear.
func findStorageClear(t *testing.T, view fyne.CanvasObject, name string) *shell.FadeIconButton {
	t.Helper()
	heading, fold := storageDetailsFold(t, view)
	if !fold.Open() {
		heading.Tapped(nil)
	}
	var found *shell.FadeIconButton
	walkRendered(view, func(o fyne.CanvasObject) {
		if r, ok := o.(*shell.Tappable); ok && r.OnHover != nil && hasRenderedText(r, name) {
			walkRendered(r, func(child fyne.CanvasObject) {
				if b, ok := child.(*shell.FadeIconButton); ok {
					found = b
				}
			})
		}
	})
	return found
}

func TestStorageBreakdownShowsBytesCountsAndPercentages(t *testing.T) {
	ui, view := storageSettingsFixture(t)
	if _, err := ui.Store.AppendRows([]backend.Row{{"date": shell.Today(), "pushed_at": "2026-11-19T12:00:00", "remarks": "logged notes"}, {"date": shell.Today(), "remarks": "draft notes"}}); err != nil {
		t.Fatal(err)
	}
	if err := ui.Store.SetIgnored("ignored-one", true); err != nil {
		t.Fatal(err)
	}
	refreshStorageMeasurement(t, view)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	usage, err := ui.Store.MeasureStorage(ui.Cfg, executable)
	if err != nil {
		t.Fatal(err)
	}
	for _, category := range usage.Categories {
		if !hasRenderedText(view, category.Name) {
			t.Fatalf("category %q missing", category.Name)
		}
		percentage := float32(0)
		if usage.StoredDataBytes > 0 {
			percentage = float32(float64(category.Bytes) / float64(usage.StoredDataBytes))
		}
		text := backend.FormatBytes(uint64(category.Bytes))
		if category.Unit != "" {
			text += fmt.Sprintf(" · %d %s", category.Count, category.Unit)
		}
		if !hasRenderedText(view, text) || !hasRenderedText(view, fmt.Sprintf("%.1f%%", float64(percentage)*100)) {
			t.Fatalf("category %q size/count/percentage missing: %s", category.Name, text)
		}
		var name *canvas.Text
		walkRendered(view, func(o fyne.CanvasObject) {
			if v, ok := o.(*canvas.Text); ok && v.Text == category.Name {
				name = v
			}
		})
		if name == nil {
			t.Fatalf("category %q name should be a legend text", category.Name)
		}
		height, figuresY := storageEntryHeight(view, name, 420)
		if height >= name.MinSize().Height*3 || height <= name.MinSize().Height*1.5 {
			t.Fatalf("category %q row should keep two lines of room, got %v", category.Name, height)
		}
		if d := figuresY - name.Position().Y; d > name.MinSize().Height || d < -name.MinSize().Height {
			t.Fatalf("category %q figures should sit on the name's line like the Projects status legend", category.Name)
		}
	}
}

// storageEntryHeight lays the legend entry holding name out at width and
// returns the height it then asks for and where its figures stand.
func storageEntryHeight(root fyne.CanvasObject, name *canvas.Text, width float32) (height, figuresY float32) {
	walkRendered(root, func(o fyne.CanvasObject) {
		if c, ok := o.(*fyne.Container); ok && len(c.Objects) == 3 && c.Objects[1] == name {
			c.Resize(fyne.NewSize(width, c.MinSize().Height))
			height, figuresY = c.MinSize().Height, c.Objects[2].Position().Y
		}
	})
	return height, figuresY
}

func TestStorageRowsLightGoldAndShowDeleteOnlyUnderThePointer(t *testing.T) {
	_, view := storageSettingsFixture(t)
	clear := storageCategoryClearButton(t, view, "GitHub history backup")
	if renderedVisible(view, clear) {
		t.Fatal("a row's Delete should stay hidden until the row is lit")
	}
	row := hoverStorageRow(t, view, "GitHub history backup")
	if !renderedVisible(view, clear) {
		t.Fatal("hovering a row should show its Delete")
	}
	var pill *canvas.Rectangle
	walkRendered(view, func(o fyne.CanvasObject) {
		if r, ok := o.(*canvas.Rectangle); ok && r.Visible() && r.Position() == row.Position() && r.Size() == row.Size() {
			pill = r
		}
	})
	if pill == nil {
		t.Fatal("hovering a row should slide the gold pill behind it")
	}
}

// hoverStorageRow puts the pointer on the storage row named name.
func hoverStorageRow(t *testing.T, view fyne.CanvasObject, name string) *shell.Tappable {
	t.Helper()
	var row *shell.Tappable
	walkRendered(view, func(o fyne.CanvasObject) {
		if r, ok := o.(*shell.Tappable); ok && r.OnHover != nil && hasRenderedText(r, name) {
			row = r
		}
	})
	if row == nil {
		t.Fatalf("storage row %q missing", name)
	}
	row.MouseIn(&desktop.MouseEvent{})
	return row
}

// renderedVisible says target is drawn: it and everything holding it show.
func renderedVisible(root, target fyne.CanvasObject) bool {
	var visit func(o fyne.CanvasObject, shown bool) (bool, bool)
	visit = func(o fyne.CanvasObject, shown bool) (bool, bool) {
		shown = shown && o.Visible()
		if o == target {
			return true, shown
		}
		var children []fyne.CanvasObject
		switch x := o.(type) {
		case *fyne.Container:
			children = x.Objects
		case fyne.Widget:
			children = test.WidgetRenderer(x).Objects()
		}
		for _, child := range children {
			if found, ok := visit(child, shown); found {
				return true, ok
			}
		}
		return false, false
	}
	_, shown := visit(root, true)
	return shown
}

func TestStorageClearLoggedRequiresConfirmationAndPreservesDraftsAndBackup(t *testing.T) {
	ui, view := storageSettingsFixture(t)
	rows, err := ui.Store.AppendRows([]backend.Row{{"date": shell.Today(), "pushed_at": "2026-11-19T12:00:00", "remarks": "logged notes"}, {"date": shell.Today(), "remarks": "keep this draft"}})
	if err != nil {
		t.Fatal(err)
	}
	refreshStorageMeasurement(t, view)
	button := storageCategoryClearButton(t, view, "Logged worklogs")
	if button.Disabled() {
		t.Fatal("logged clear should be enabled")
	}
	button.OnTapped()
	popup := ui.Win.Canvas().Overlays().Top()
	if popup == nil {
		t.Fatal("clear must ask for confirmation")
	}
	before, _ := ui.Store.ReadRows()
	if len(before) != 2 {
		t.Fatal("opening confirmation deleted rows")
	}
	tapStoragePopupButton(t, popup, "Cancel")
	before, _ = ui.Store.ReadRows()
	if len(before) != 2 {
		t.Fatal("cancel deleted rows")
	}
	button.OnTapped()
	popup = ui.Win.Canvas().Overlays().Top()
	tapStoragePopupButton(t, popup, "Clear Logged worklogs")
	after, err := ui.Store.ReadRows()
	if err != nil || len(after) != 1 || after[0]["id"] != rows[1]["id"] || after[0]["remarks"] != "keep this draft" {
		t.Fatalf("draft lost while clearing logged entries: %v %v", after, err)
	}
	if _, err := backend.LoadHistoryArchive(ui.Cfg); err != nil {
		t.Fatal("worklog clearing removed the backup", err)
	}
	if findStorageClear(t, view, "Logged worklogs") != nil {
		t.Fatal("an empty category should offer no Delete after measurement refresh")
	}
}

func TestStorageClearIgnoredOnlyResetsIgnoredState(t *testing.T) {
	ui, view := storageSettingsFixture(t)
	if err := ui.Store.SetIgnored("one", true); err != nil {
		t.Fatal(err)
	}
	if _, err := ui.Store.AppendRows([]backend.Row{{"remarks": "keep worklog"}}); err != nil {
		t.Fatal(err)
	}
	ui.Pending = backend.PendingResult{Count: 0, Groups: []backend.Group{{Ignored: true, Commits: []backend.Commit{{Sha: "one"}}}}}
	refreshStorageMeasurement(t, view)
	storageCategoryClearButton(t, view, "Ignored commits").OnTapped()
	tapStoragePopupButton(t, ui.Win.Canvas().Overlays().Top(), "Clear Ignored commits")
	ignored, err := ui.Store.IgnoredShas()
	if err != nil || len(ignored) != 0 {
		t.Fatal("ignored state was not cleared", ignored, err)
	}
	if ui.Pending.Groups[0].Ignored || ui.Pending.Count != 1 {
		t.Fatal("pending list retained stale ignored flags")
	}
	rows, err := ui.Store.ReadRows()
	if err != nil || len(rows) != 1 || rows[0]["remarks"] != "keep worklog" {
		t.Fatal("clearing ignored commits changed worklogs")
	}
}

func TestStorageClearProjectHistoryKeepsProjectConfiguration(t *testing.T) {
	ui, view := storageSettingsFixture(t)
	saved := projects.Saved{Projects: []projects.Project{{ID: "project-one", Name: "Keep this project", Issues: []string{"org/repo#1"}, Excluded: []string{"org/repo#1"}, History: map[string]projects.WeekCounts{"2026-11-16": {Review: 2}}}}}
	if err := projects.Save(ui.Store, saved); err != nil {
		t.Fatal(err)
	}
	refreshStorageMeasurement(t, view)
	storageCategoryClearButton(t, view, "Project report history").OnTapped()
	popup := ui.Win.Canvas().Overlays().Top()
	var explanation string
	walkRendered(popup, func(o fyne.CanvasObject) {
		if label, ok := o.(*widget.Label); ok {
			explanation += " " + label.Text
		}
	})
	if !strings.Contains(explanation, "not included in the worklog backup") {
		t.Fatal("confirmation must explain the history is not backed up")
	}
	tapStoragePopupButton(t, popup, "Clear Project report history")
	got, err := projects.Load(ui.Store)
	if err != nil || len(got.Projects) != 1 || got.Projects[0].Name != "Keep this project" || len(got.Projects[0].Issues) != 1 || len(got.Projects[0].Excluded) != 1 || len(got.Projects[0].History) != 0 {
		t.Fatalf("project configuration lost or report history retained: %+v %v", got, err)
	}
	if findStorageClear(t, view, "Project report history") != nil {
		t.Fatal("cleared history should offer no Delete")
	}
	if _, err := os.Stat(filepath.Join(ui.Store.Dir, "projects.json")); err != nil {
		t.Fatal("clearing history removed project definitions", err)
	}
}

func storageDetailsFold(t *testing.T, view fyne.CanvasObject) (*shell.Tappable, *shell.Fold) {
	t.Helper()
	var heading *shell.Tappable
	var fold *shell.Fold
	walk(view, func(o fyne.CanvasObject) {
		c, ok := o.(*fyne.Container)
		if !ok || len(c.Objects) != 2 {
			return
		}
		f, ok := c.Objects[1].(*shell.Fold)
		if !ok {
			return
		}
		walk(c.Objects[0], func(o fyne.CanvasObject) {
			if tap, ok := o.(*shell.Tappable); ok {
				for _, text := range labels(tap.Content) {
					if text == "Data + Backups" {
						heading, fold = tap, f
					}
				}
			}
		})
	})
	if heading == nil || fold == nil {
		t.Fatal("Data + backups must have a collapsible heading")
	}
	return heading, fold
}

func TestStorageBreakdownCollapsesWithLogWorkMotionAndStaysCollapsedOnRefresh(t *testing.T) {
	_, view := storageSettingsFixture(t)
	heading, fold := storageDetailsFold(t, view)
	if fold.Open() || fold.Visible() {
		t.Fatal("Data + backups must start collapsed")
	}
	driver := collectMotion(t)
	heading.Tapped(nil)
	// The heading's triangle turns first; the fold's slide is the last.
	if len(driver.anims) != 2 || !fold.Open() {
		t.Fatal("collapsed storage details must animate open")
	}
	open := driver.anims[1]
	open.Tick(.5)
	if height := fold.MinSize().Height; height <= 0 || height >= fold.Content.MinSize().Height {
		t.Fatal("storage opening must have an intermediate height")
	}
	open.Tick(1)
	driver.anims = nil
	height := fold.MinSize().Height
	heading.Tapped(nil)
	last := len(driver.anims) - 1
	if last < 0 || driver.anims[last].Duration != widgets.HoverDuration || fold.Open() {
		t.Fatal("storage collapse must use the shared fold motion")
	}
	slide := driver.anims[last]
	slide.Tick(.5)
	if got := fold.MinSize().Height; got <= 0 || got >= height || !fold.Visible() {
		t.Fatalf("fold snapped instead of sliding: %v of %v", got, height)
	}
	slide.Tick(1)
	if fold.Visible() {
		t.Fatal("fold must hide only after the closing motion")
	}
	refreshStorageMeasurement(t, view)
	heading, fold = storageDetailsFold(t, view)
	if fold.Open() || fold.Visible() {
		t.Fatal("storage measurement reopened the collapsed breakdown")
	}
	driver.anims = nil
	heading.Tapped(nil)
	driver.anims[len(driver.anims)-1].Tick(1)
	if !fold.Open() || !fold.Visible() || fold.MinSize().Height == 0 {
		t.Fatal("storage details did not reopen")
	}
}

func TestStorageCategoryColoursDoNotRepeatAndRefreshIconIsGold(t *testing.T) {
	_, view := storageSettingsFixture(t)
	_, fold := storageDetailsFold(t, view)
	colours := map[[4]uint32]bool{}
	dots := 0
	walkRendered(fold.Content, func(o fyne.CanvasObject) {
		if r, ok := o.(*canvas.Rectangle); ok && r.MinSize() == fyne.NewSquareSize(10) && r.CornerRadius == 3 {
			red, green, blue, alpha := r.FillColor.RGBA()
			key := [4]uint32{red, green, blue, alpha}
			if colours[key] {
				t.Error("storage categories repeat a colour")
			}
			colours[key] = true
			dots++
		}
	})
	if dots != 11 {
		t.Fatalf("expected 11 category colours, got %d", dots)
	}
	found := false
	walkRendered(view, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "" && b.Icon == theme.ViewRefreshIcon() {
			found = true
			if !sameColor(theme.ColorForWidget(theme.ColorNameForeground, b), widgets.LogoGold) {
				t.Error("storage refresh icon must be gold")
			}
		}
	})
	if !found {
		t.Fatal("storage refresh missing")
	}
}

func TestStorageBreakdownHeadingIsGoldUnderlinedWithArrowAtRightEdge(t *testing.T) {
	_, view := storageSettingsFixture(t)
	head, fold := storageDetailsFold(t, view)
	var title *canvas.Text
	var arrow *shell.SpinTriangle
	var divider *widget.Separator
	walkRendered(head.Content, func(o fyne.CanvasObject) {
		if text, ok := o.(*canvas.Text); ok && text.Text == "Data + Backups" {
			title = text
		}
		if spin, ok := o.(*shell.SpinTriangle); ok && !spin.Up() {
			arrow = spin
		}
	})
	walk(view, func(o fyne.CanvasObject) {
		if c, ok := o.(*fyne.Container); ok && len(c.Objects) == 2 && c.Objects[1] == fold {
			walkRendered(c.Objects[0], func(o fyne.CanvasObject) {
				if rule, ok := o.(*widget.Separator); ok {
					divider = rule
				}
			})
		}
	})
	if title == nil || arrow == nil || divider == nil || !sameColor(title.Color, widgets.LogoGold) {
		t.Fatal("missing gold title, disclosure arrow or underline")
	}
	driver := fyne.CurrentApp().Driver()
	titleBottom := driver.AbsolutePositionForObject(title).Y + title.Size().Height
	if driver.AbsolutePositionForObject(divider).Y < titleBottom {
		t.Fatal("underline must occupy its own line below the title")
	}
	right := driver.AbsolutePositionForObject(head).X + head.Size().Width
	arrowRight := driver.AbsolutePositionForObject(arrow).X + arrow.Size().Width
	// The arrow keeps the hover's inner padding from the heading's right edge.
	if inset := theme.Padding() * 2; right-arrowRight > inset+1 || arrowRight > right-inset+1 {
		t.Fatalf("arrow is not at the right edge: heading %v, arrow %v", right, arrowRight)
	}
	for _, o := range test.WidgetRenderer(divider).Objects() {
		if line, ok := o.(*canvas.Rectangle); ok && !sameColor(line.FillColor, widgets.LogoGold) {
			t.Fatal("heading underline must be gold")
		}
	}
}

func TestStorageHeadingsFoldAndLightGold(t *testing.T) {
	_, view := storageSettingsFixture(t)
	for _, title := range []string{"Storage", "Data + Backups", "History Backup"} {
		var heading *shell.Tappable
		var fold *shell.Fold
		walk(view, func(o fyne.CanvasObject) {
			c, ok := o.(*fyne.Container)
			if !ok || len(c.Objects) != 2 {
				return
			}
			f, ok := c.Objects[1].(*shell.Fold)
			if !ok {
				return
			}
			walk(c.Objects[0], func(o fyne.CanvasObject) {
				if tap, ok := o.(*shell.Tappable); ok && hasRenderedText(tap, title) {
					heading, fold = tap, f
				}
			})
		})
		if heading == nil || fold == nil {
			t.Fatalf("%s must have a collapsible heading", title)
		}
		if heading.OnHover == nil {
			t.Fatalf("%s heading must light under the pointer", title)
		}
		heading.MouseIn(&desktop.MouseEvent{})
		var pill *canvas.Rectangle
		walkRendered(view, func(o fyne.CanvasObject) {
			if r, ok := o.(*canvas.Rectangle); ok && r.Visible() && r.Position() == heading.Position() && r.Size() == heading.Size() && heading.Size().Width > 0 {
				pill = r
			}
		})
		if pill == nil {
			t.Fatalf("%s heading should slide the gold pill behind it", title)
		}
		heading.MouseOut()
		was := fold.Open()
		heading.Tapped(nil)
		if fold.Open() == was {
			t.Fatalf("%s heading should fold its section", title)
		}
	}
}
