package tests

import (
	"fmt"
	"image/color"
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

func TestSprintIssuesScrollbarReachesTheBottomBelowTheHeader(t *testing.T) {
	ui, _, issue := plannerFixture(t)
	k := ui.Kanban
	if _, patched := any(shell.PlannerScroll(k.CalendarIssueClip)).(interface{ WorklogSetVerticalBarInset(float32) }); !patched {
		t.Skip("uses the app scrollbar inset")
	}
	for i := range 24 {
		k.Plan.Boards[0].Items = append(k.Plan.Boards[0].Items, backend.SprintItem{
			ID: fmt.Sprint(i), Title: "Issue in the tray", SprintID: issue.Sprint.ID, Effort: 60,
		})
	}
	kanban.Draw(k)
	scroll := shell.PlannerScroll(k.CalendarIssueClip)
	if scroll.Content.Size().Height <= scroll.Size().Height {
		t.Fatal("the issue tray should overflow")
	}
	var thumb fyne.Draggable
	for _, object := range test.WidgetRenderer(scroll).Objects() {
		if widget, ok := object.(fyne.Widget); ok && object.Position().Y > 0 {
			for _, child := range test.WidgetRenderer(widget).Objects() {
				if bar, ok := child.(fyne.Draggable); ok {
					thumb = bar
				}
			}
		}
	}
	if thumb == nil {
		t.Fatal("the tray needs a draggable thumb below its header")
	}
	thumb.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(0, scroll.Size().Height)})
	thumb.DragEnd()
	if want := scroll.Content.Size().Height - scroll.Size().Height; scroll.Offset.Y != want {
		t.Fatalf("dragged scrollbar offset %g, want bottom at %g", scroll.Offset.Y, want)
	}
}

func TestSprintKanbanColumnsFillRightViewportAndKeepSidebarWidth(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	k := ui.Kanban
	k.Plan.Boards[0].Fields = append(k.Plan.Boards[0].Fields, backend.SprintField{Name: "Status", Options: []backend.FieldOption{{Name: "Created"}, {Name: "In Progress"}}})
	w.SetContent(k.Root)
	kanban.Draw(k)
	sideWidth := float32(0)
	for _, width := range []float32{1400, 1800} {
		w.Resize(fyne.NewSize(width, 950))
		w.Content().Refresh()
		var cols []*shell.KanbanTarget
		for _, target := range k.Targets {
			if target.Kind == "status" {
				cols = append(cols, target)
			}
		}
		if len(cols) != 2 {
			t.Fatal("fixture status columns missing")
		}
		last := cols[len(cols)-1].Object
		end := fyne.CurrentApp().Driver().AbsolutePositionForObject(last).X + last.Size().Width
		clipEnd := fyne.CurrentApp().Driver().AbsolutePositionForObject(k.BoardClip).X + k.BoardClip.Size().Width
		if clipEnd-end > 3 || end > clipEnd {
			t.Fatal("status columns do not fill the right viewport", end, clipEnd)
		}
		if width == 1400 {
			sideWidth = k.SideClip.Size().Width
			continue
		}
		// Sidebar sizing comes from the same existing container at either width.
		if k.SideClip.Size().Width != sideWidth {
			t.Fatal("columns stretched the sidebar")
		}
	}
	objects := []fyne.CanvasObject{canvas.NewRectangle(nil), canvas.NewRectangle(nil), canvas.NewRectangle(nil)}
	l := kanban.KanbanColumnsLayout{}
	l.Layout(objects, fyne.NewSize(500, 600))
	if objects[0].Size().Width < 260 || l.MinSize(objects).Width <= 500 {
		t.Fatal("narrow boards lost horizontal overflow")
	}
}

func TestSprintCalendarBubbleEdgeHitTestResizesTheBubble(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: k.Week, Start: 540, Minutes: 60}, {Date: k.Week, Start: 720, Minutes: 30}}}
	kanban.Draw(k)
	w.Content().Refresh()
	var cards []*shell.PlannerDragCard
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*shell.PlannerDragCard); ok && card.Index >= 0 {
			cards = append(cards, card)
		}
	})
	if len(cards) != 2 {
		t.Fatal("scheduled bubbles missing")
	}
	card := cards[0]
	pixels := card.Content.(*fyne.Container).Layout.(*sprintcalendar.PlannerDurationLayout).Pixels()
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(card)
	if card.Size().Height != 60*pixels {
		t.Fatal("bubble height does not match the ruler")
	}
	oldNextY := fyne.CurrentApp().Driver().AbsolutePositionForObject(cards[1]).Y
	test.Drag(w.Canvas(), pos.Add(fyne.NewPos(card.Size().Width/2, card.Size().Height-2)), 0, 30*pixels)
	if k.Plan.Tasks[c.Key].Blocks[0].Minutes != 90 || len(k.Plan.Tasks[c.Key].Blocks) != 2 {
		t.Fatal("dragging the bubble's bottom edge did not resize it")
	}
	var resized, next *shell.PlannerDragCard
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*shell.PlannerDragCard); ok {
			if card.Index == 0 {
				resized = card
			}
			if card.Index == 1 {
				next = card
			}
		}
	})
	if resized.Size().Height != 90*pixels || fyne.CurrentApp().Driver().AbsolutePositionForObject(next).Y != oldNextY+30*pixels {
		t.Fatal("resized bubble and following block did not reflow against the ruler")
	}
	// The card owns only its face and an overlaid edge, with no added panel/ruler.
	content := resized.Content.(*fyne.Container)
	if len(content.Objects) != 2 {
		t.Fatal("extra resize panel remains")
	}
	edge := content.Objects[1]
	if edge.Position().Y+edge.Size().Height != resized.Size().Height || edge.Size().Height > 6 {
		t.Fatal("resize edge extends outside bubble")
	}
}

func TestSprintCalendarClearPreservesOtherSprintsAndMetadata(t *testing.T) {
	s := backend.SprintIteration{StartDate: "2026-11-02", Duration: 14}
	inside := backend.PlanBlock{Date: s.StartDate, Start: 540, Minutes: 60}
	outside := backend.PlanBlock{Date: "2026-12-01", Start: 540, Minutes: 60}
	original := backend.SprintPlan{Tasks: map[string]backend.SprintTask{
		"mine":   {Estimate: 90, Notes: "keep me", Complexity: "M", Blocks: []backend.PlanBlock{inside, outside}},
		"hidden": {Blocks: []backend.PlanBlock{inside}},
	}}
	p, count := backend.ClearSprintSchedule(original, []backend.SprintCandidate{{Key: "mine", Sprint: s}})
	if count != 1 || !reflect.DeepEqual(p.Tasks["mine"].Blocks, []backend.PlanBlock{outside}) {
		t.Fatal("clear did not limit itself to selected sprint")
	}
	if p.Tasks["mine"].Estimate != 90 || p.Tasks["mine"].Notes != "keep me" || p.Tasks["mine"].Complexity != "M" || !reflect.DeepEqual(p.Tasks["hidden"], original.Tasks["hidden"]) || len(original.Tasks["mine"].Blocks) != 2 {
		t.Fatal("clear lost other data or mutated original")
	}
}

func TestSprintCalendarSidebarAndDropColorsWithTopProgressMeters(t *testing.T) {
	ui, _, c := plannerFixture(t)
	k := ui.Kanban
	var tray *shell.PlannerDragCard
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if card, ok := o.(*shell.PlannerDragCard); ok && card.Index < 0 {
			tray = card
		}
		if _, ok := o.(*widget.ProgressBar); ok {
			t.Fatal("date progress bar remains")
		}
	})
	if tray == nil {
		t.Fatal("sprint issues sidebar missing")
	}
	trayScroll := shell.PlannerScroll(k.CalendarIssueClip)
	var trayHeading *shell.Tappable
	walkRendered(k.CalendarBody, func(o fyne.CanvasObject) {
		if heading, ok := o.(*shell.Tappable); ok && contains(labels(heading.Content), "Sprint Issues") {
			trayHeading = heading
		}
	})
	if trayHeading == nil {
		t.Fatal("sprint issues heading missing")
	}
	display := fyne.CurrentApp().Driver()
	scrollPos := display.AbsolutePositionForObject(trayScroll)
	headPos := display.AbsolutePositionForObject(trayHeading)
	if headPos.X+trayHeading.Size().Width != scrollPos.X+trayScroll.Size().Width || headPos.Y+trayHeading.Size().Height <= scrollPos.Y {
		t.Fatal("the heading should fill the tray's original width and overlap the cards")
	}
	if _, patched := any(trayScroll).(interface{ WorklogSetVerticalBarInset(float32) }); patched {
		barTop := scrollPos.Y
		for _, object := range test.WidgetRenderer(trayScroll).Objects() {
			if _, bar := object.(fyne.Tappable); bar && object.Size().Height > 0 {
				barTop = scrollPos.Y + object.Position().Y
			}
		}
		if want := headPos.Y + trayHeading.Size().Height; barTop != want {
			t.Fatalf("the scrollbar must start below the heading at %g, got %g", want, barTop)
		}
	}
	trayTheme := theme.CurrentForWidget(trayScroll)
	for _, name := range []fyne.ThemeColorName{theme.ColorNameShadow, theme.ColorNameScrollBarBackground} {
		if !sameColor(trayTheme.Color(name, theme.VariantDark), color.Transparent) {
			t.Fatalf("the issue tray should suppress %s behind its header", name)
		}
	}
	trayPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(tray)
	gridPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(k.CalendarClip)
	if trayPos.X >= gridPos.X || tray.Size().Width > 300 {
		t.Fatal("issues are not in a left sidebar")
	}
	driver := collectMotion(t)
	tray.MouseIn(nil)
	var target *shell.PlannerDateTarget
	for _, date := range k.Dates {
		if date.Date == c.Sprint.StartDate {
			target = date
		}
	}
	if target == nil {
		t.Fatal("target missing")
	}
	for _, anim := range driver.anims {
		if anim.Duration == shell.SlowFade {
			anim.Tick(0.5)
		}
	}
	if sameColor(target.Frame.FillColor, target.Base) || sameColor(target.Frame.FillColor, widgets.WithAlpha(widgets.LogoGold, 0.13)) {
		t.Fatal("the gold background should interpolate during the 600 ms fade")
	}
	shell.SettleFades()
	if target.Frame.StrokeColor != widgets.LogoGold || target.Frame.FillColor != widgets.WithAlpha(widgets.LogoGold, 0.13) {
		t.Fatal("an available date should have a gold outline and tint")
	}
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(target.Object).Add(fyne.NewPos(10, 80))
	driver.anims = nil
	tray.HighlightDates(&at)
	for _, anim := range driver.anims {
		if anim.Duration == shell.SlowFade {
			anim.Tick(0.4)
		}
	}
	if target.Frame.StrokeWidth != 4 || sameColor(target.Frame.FillColor, widgets.WithAlpha(widgets.LogoGold, 0.25)) {
		t.Fatal("the outline should reach full width after 240 ms while the gold tint is still fading")
	}
	shell.SettleFades()
	if target.Frame.StrokeColor != widgets.LogoGold || target.Frame.FillColor != widgets.WithAlpha(widgets.LogoGold, 0.25) {
		t.Fatal("the active target should deepen the gold tint")
	}
	tray.MouseOut()
	shell.SettleFades()
	if target.Frame.FillColor != target.Base {
		t.Fatal("hover wash did not reset")
	}
	target.Base = shell.DayFill(backend.Target, 0)
	shell.SetFrame(target.Frame, target.Base, target.StrokeColor, target.StrokeWidth)
	tray.HighlightDates(&at)
	shell.SettleFades()
	if !sameColor(target.Frame.StrokeColor, theme.Color(theme.ColorNameSuccess)) || !sameColor(target.Frame.FillColor, target.Base) {
		t.Fatal("a green day should keep its background and use a green outline")
	}
	tray.ResetDates()
	foundMeter := false
	for key := range ui.Meters {
		if strings.HasPrefix(key, "planner/") {
			foundMeter = true
		}
	}
	if !foundMeter {
		t.Fatal("calendar day progress meters missing")
	}
	k.CalendarMode = "Month"
	kanban.Draw(k)
	scroll := shell.PlannerScroll(k.CalendarClip)
	scroll.ScrollToOffset(fyne.NewPos(0, 300))
	sprintcalendar.DrawKeepingCalendarScroll(k)
	if shell.PlannerScroll(k.CalendarClip).Offset.Y != 300 {
		t.Fatal("calendar jumped to the top after a planning change")
	}
}

func TestSprintCalendarClearButtonConfirmsAndSavesLocally(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	k.Plan.Tasks[c.Key] = backend.SprintTask{Estimate: 90, Notes: "keep", Blocks: []backend.PlanBlock{{Date: c.Sprint.StartDate, Start: 540, Minutes: 60}}}
	kanban.Draw(k)
	var clear *widget.Button
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Clear" {
			clear = b
		}
	})
	if clear == nil {
		t.Fatal("Clear action missing")
	}
	test.Tap(clear)
	if w.Canvas().Overlays().Top() == nil || len(k.Plan.Tasks[c.Key].Blocks) != 1 {
		t.Fatal("clear must wait for confirmation")
	}
	var confirm *widget.Button
	walkPopup(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Text == "Clear" {
			confirm = b
		}
	})
	if confirm == nil {
		t.Fatal("clear confirmation missing")
	}
	test.Tap(confirm)
	p, err := ui.Store.LoadSprintPlan()
	if err != nil || len(p.Tasks[c.Key].Blocks) != 0 || p.Tasks[c.Key].Notes != "keep" || k.Plan.Boards[0].Items[0].Effort != 90 {
		t.Fatal("clear did not preserve local metadata and GitHub fields", err)
	}
}
