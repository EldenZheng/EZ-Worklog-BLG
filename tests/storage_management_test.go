package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
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

func storageCategoryClearButton(t *testing.T, view fyne.CanvasObject, name string) *widget.Button {
	t.Helper()
	heading, fold := storageDetailsFold(t, view)
	if !fold.Open() {
		heading.Tapped(nil)
	}
	var found *widget.Button
	walk(view, func(o fyne.CanvasObject) {
		if c, ok := o.(*fyne.Container); ok && hasRenderedText(c, name) {
			var candidate *widget.Button
			walkRendered(c, func(child fyne.CanvasObject) {
				if b, ok := child.(*widget.Button); ok && b.Text == "Clear" {
					candidate = b
				}
			})
			if candidate != nil {
				found = candidate
			}
		}
	})
	if found == nil {
		t.Fatalf("clear action for %q missing", name)
	}
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
		text := fmt.Sprintf("%s · %.1f%%", backend.FormatBytes(uint64(category.Bytes)), float64(percentage)*100)
		if category.Unit != "" {
			text += fmt.Sprintf(" · %d %s", category.Count, category.Unit)
		}
		if !hasRenderedText(view, text) {
			t.Fatalf("category %q size/count/percentage missing: %s", category.Name, text)
		}
	}
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
	if !storageCategoryClearButton(t, view, "Logged worklogs").Disabled() {
		t.Fatal("empty category should be disabled after measurement refresh")
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
	if !storageCategoryClearButton(t, view, "Project report history").Disabled() {
		t.Fatal("cleared history action should be disabled")
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
					if text == "Data + backups" {
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
	if len(driver.anims) != 1 || !fold.Open() {
		t.Fatal("collapsed storage details must animate open")
	}
	open := driver.anims[0]
	open.Tick(.5)
	if height := fold.MinSize().Height; height <= 0 || height >= fold.Content.MinSize().Height {
		t.Fatal("storage opening must have an intermediate height")
	}
	open.Tick(1)
	driver.anims = nil
	height := fold.MinSize().Height
	heading.Tapped(nil)
	if len(driver.anims) != 1 || driver.anims[0].Duration != widgets.HoverDuration || fold.Open() {
		t.Fatal("storage collapse must use the shared fold motion")
	}
	slide := driver.anims[0]
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
		if r, ok := o.(*canvas.Rectangle); ok && r.MinSize() == fyne.NewSize(6, 6) && r.CornerRadius == 3 {
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
	var arrow *widget.Icon
	var divider *widget.Separator
	walkRendered(head.Content, func(o fyne.CanvasObject) {
		if text, ok := o.(*canvas.Text); ok && text.Text == "Data + backups" {
			title = text
		}
		if icon, ok := o.(*widget.Icon); ok && icon.Resource == theme.MenuExpandIcon() {
			arrow = icon
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
	if right-arrowRight > 1 || arrowRight > right+1 {
		t.Fatalf("arrow is not at the right edge: heading %v, arrow %v", right, arrowRight)
	}
	for _, o := range test.WidgetRenderer(divider).Objects() {
		if line, ok := o.(*canvas.Rectangle); ok && !sameColor(line.FillColor, widgets.LogoGold) {
			t.Fatal("heading underline must be gold")
		}
	}
}
