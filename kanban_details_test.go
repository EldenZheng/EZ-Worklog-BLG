package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestSprintCardKeepsFullTitleRepositoryAndConditionalSprint(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.kanban
	b := k.plan.Boards[0]
	it := b.Items[0]
	it.URL = "https://github.com/bigledger/invoice-engine/issues/25"
	it.Title = strings.Repeat("Full issue title including important details ", 8)
	it.Complexity = "L - 2 hours"
	for _, filter := range []string{"current", "sprint", "none", "all"} {
		k.filter = filter
		card := k.card(b, it).(*sprintDragCard)
		labels := map[string]bool{}
		walkCanvas(card.content, func(o fyne.CanvasObject) {
			if label, ok := o.(*widget.Label); ok {
				labels[label.Text] = true
			}
		})
		if !labels[it.Title] || !labels["bigledger/invoice-engine#25"] || !labels["Project estimate: 90m"] || !labels["Complexity: L - 2 hours"] {
			t.Fatal("full title/repository/details missing", labels)
		}
		if labels["– Sprint"] != (filter == "all") {
			t.Fatal("sprint label should appear only without a sprint filter", filter, labels)
		}
	}
}

func TestSprintEmptyColumnSelectionSurvivesReload(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.kanban
	k.plan.CalendarMode = "Month"
	k.statusChoices.SetSelected(nil)
	ui.buildKanbanTab()
	if ui.kanban.plan.KanbanColumns == nil || len(*ui.kanban.plan.KanbanColumns) != 0 || ui.kanban.calendarMode != "Month" {
		t.Fatal("empty column selection or view preference was not restored")
	}
}

func TestSprintDropdownCentersSelectedFirstMiddleAndLast(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("dropdown")
	defer w.Close()
	var options []string
	for i := 0; i < 30; i++ {
		label := fmt.Sprintf("Sprint %d | Oct 01 – Oct 14", i)
		if i%3 == 0 {
			label += " – extra sprint details – Current"
		}
		options = append(options, label)
	}
	s := newGoldSelect(options, nil)
	w.SetContent(container.NewVBox(container.NewGridWrap(fyne.NewSize(360, 40), s)))
	w.Resize(fyne.NewSize(700, 650))
	for _, count := range []int{3, 30} {
		s.Options = options[:count]
		for _, index := range []int{0, count / 2, count - 1} {
			s.SetSelected(options[index])
			s.Tapped(nil)
			row := s.rows.Objects[index]
			center := s.rows.Position().Y + row.Position().Y + row.Size().Height/2 - s.scroll.Offset.Y
			if math.Abs(float64(center-s.scroll.Size().Height/2)) > 1 {
				t.Fatal("selected option was not centered", index, center, s.scroll.Size())
			}
			s.closeChoices()
		}
	}
}

func TestSprintAndLogDeleteFootersStayAtBottom(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	for _, show := range []func(){func() { ui.kanban.confirmClear("Clear", "Clear these local time blocks?", func() {}) }, func() { ui.confirmDelete(Row{"id": "unused"}, func() {}) }} {
		show()
		var popup *widget.PopUp
		var cancel, confirm *widget.Button
		walkPopup(w.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
			if p, ok := o.(*widget.PopUp); ok {
				popup = p
			}
			if b, ok := o.(*widget.Button); ok {
				if b.Text == "Cancel" {
					cancel = b
				}
				if b.Text == "Clear" || b.Text == "Delete" {
					confirm = b
				}
			}
		})
		if popup == nil || cancel == nil || confirm == nil {
			t.Fatal("confirmation controls missing")
		}
		bottom := fyne.CurrentApp().Driver().AbsolutePositionForObject(popup.Content).Y + popup.Content.Size().Height
		for _, b := range []*widget.Button{cancel, confirm} {
			gap := bottom - fyne.CurrentApp().Driver().AbsolutePositionForObject(b).Y - b.Size().Height
			if gap < 0 || gap > 12 {
				t.Fatal("confirmation footer is not at the bottom", gap)
			}
		}
		test.Tap(cancel)
	}
}

func TestLiveSprintPlannerWorklogReadOnly(t *testing.T) {
	dir := os.Getenv("WORKLOG_PLANNER_LIVE_DIR")
	if dir == "" {
		t.Skip("opt-in read-only worklog check")
	}
	cfg, ok := newStore(dir).LoadConfig()
	if !ok {
		t.Fatal("live configuration unavailable")
	}
	from := weekStartOf(today())
	to := addDays(from, 6)
	items, err := FetchProjectWorklogs(cfg, cfg.WorklogOwner, from, to)
	if err != nil {
		t.Fatal(err)
	}
	minutes := 0
	for _, it := range items {
		minutes += it.Minutes
		if it.Date < from || it.Date > to {
			t.Fatal("worklog outside requested dates")
		}
	}
	t.Logf("Read %d GitHub worklogs (%d minutes) for %s to %s", len(items), minutes, from, to)
}
