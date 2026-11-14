package tests

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"worklog/internal/assemble"
	"worklog/internal/menus/kanban"
	"worklog/internal/menus/loglist"
	"worklog/internal/menus/logwork"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// dayLinkIn finds the heading that starts with text and the block laid over it.
func dayLinkIn(t *testing.T, root fyne.CanvasObject, text string) (*fyne.Container, *shell.Tappable) {
	t.Helper()
	var row *fyne.Container
	var link *shell.Tappable
	walk(root, func(o fyne.CanvasObject) {
		c, ok := o.(*fyne.Container)
		if !ok || row != nil || len(c.Objects) != 2 {
			return
		}
		tap, ok := c.Objects[1].(*shell.Tappable)
		if ok && len(labels(c.Objects[0])) == 1 && strings.HasPrefix(labels(c.Objects[0])[0], text) {
			row, link = c, tap
		}
	})
	if link == nil {
		t.Fatalf("no block over the heading %q: %v", text, labels(root))
	}
	return row, link
}

func TestDayHeadingsOpenThatDayOnStatus(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w, CalMonth: "2026-08", RepMonth: "2026-08", WeekStart: "2026-08-10"}
	ui.Cfg = backend.Config{Repos: []string{"exampleorg"}}
	assemble.BuildAllTabs(ui)
	w.Resize(fyne.NewSize(1600, 1000))
	logwork.DrawWeekStrip(ui)
	loglist.DrawLogList(ui)
	commits := ui.CommitDaysPanel([]string{"2026-08-12"}, map[string][]backend.Commit{"2026-08-12": {{Sha: "abc", Issue: "exampleorg/repo#1"}}})
	commits.Resize(fyne.NewSize(400, 400))

	for _, tc := range []struct {
		name, heading, date string
		root                fyne.CanvasObject
	}{
		{"Log Work", "Mon 10", "2026-08-10", ui.WeekBox},
		{"Log List", "Tue 11", "2026-08-11", ui.ListBox},
		{"Commit List and Meeting", "Wed 12", "2026-08-12", commits},
	} {
		ui.SelDay = ""
		_, link := dayLinkIn(t, tc.root, tc.heading)
		link.Tapped(&fyne.PointEvent{})
		if ui.SelDay != tc.date || ui.Tabs.SelectedIndex() != shell.StatusTabIndex {
			t.Fatalf("%s: opened %q on tab %d", tc.name, ui.SelDay, ui.Tabs.SelectedIndex())
		}
	}

	// Only the heading: the rest of the row is not part of the block. The text
	// keeps the whole row, so the block's width cannot elide it.
	row, link := dayLinkIn(t, commits, "Wed 12")
	if link.Size().Width <= 0 || link.Size().Width >= 300 {
		t.Fatalf("the block should hug its heading, is %g wide", link.Size().Width)
	}
	if heading := row.Objects[0]; heading.Size().Width != row.Size().Width {
		t.Fatalf("the heading got %g of its %g wide row", heading.Size().Width, row.Size().Width)
	}
}

// The date takes the hover from its day, and the day's Delete must not go with it.
func TestSprintCalendarDateKeepsTheDayLit(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	k.Week = backend.AddDays(k.Week, 7)
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: k.Week, Start: 540, Minutes: 60}}}
	kanban.Draw(k)
	w.Content().Refresh()

	var hover *shell.PlannerDayHover
	for _, d := range k.Dates {
		if d.Date == k.Week {
			hover = d.Object.(*shell.PlannerDayHover)
		}
	}
	if hover == nil {
		t.Fatal("the week's first day is missing")
	}
	link, ok := hover.Header.Objects[0].(*fyne.Container).Objects[1].(*shell.Tappable)
	if !ok {
		t.Fatalf("the date is not a block: %T", hover.Header.Objects[0])
	}
	link.MouseIn(nil)
	if !hover.ClearFade.Shown() {
		t.Fatal("Delete should stay in while the pointer is on the date")
	}
	link.MouseOut()
	if hover.ClearFade.Shown() {
		t.Fatal("Delete should go when the pointer leaves the date")
	}
}
