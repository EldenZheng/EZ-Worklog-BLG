package tests

import (
	"fmt"
	"strconv"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/commitlist"
	"worklog/internal/menus/loglist"
	"worklog/internal/menus/logwork"
	"worklog/internal/menus/meeting"
	"worklog/internal/menus/status"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

func TestCommitAndMeetingOrgFiltersHideWorkAndKeepTheirKeys(t *testing.T) {
	for _, name := range []string{"commits", "meeting"} {
		t.Run(name, func(t *testing.T) {
			a := test.NewApp()
			defer a.Quit()
			ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: a.NewWindow("test"),
				WeekStart: "2026-08-24", MeetingStart: "2026-08-20",
				Cfg: backend.Config{Repos: []string{"exampleorg", "Example-Support"}},
				IssueInfo: map[string]backend.IssueInfo{
					"exampleorg/repo#1":      {Title: "Platform issue"},
					"Example-Support/repo#2": {Title: "Support issue"},
				},
			}
			commits := []backend.Commit{
				{Date: "2026-08-25", Issue: "exampleorg/repo#1", Repo: "exampleorg/repo", Sha: "1111111", Message: "Platform commit"},
				{Date: "2026-08-25", Repo: "Example-Support/repo", Sha: "2222222", Message: "Support commit"},
			}
			var root *fyne.Container
			if name == "commits" {
				ui.Win.SetContent(commitlist.BuildCommitListTab(ui))
				ui.EnsureWeekCommitCache()
				ui.WeekCommits[ui.WeekStart] = commits
				commitlist.DrawCommitList(ui)
				root = ui.CommitBox
			} else {
				_, err := ui.Store.AppendRows([]backend.Row{
					{"date": "2026-08-25", "minutes": "30", "issue": "exampleorg/repo#1"},
					{"date": "2026-08-25", "minutes": "60", "issue": "Example-Support/repo#2"},
				})
				if err != nil {
					t.Fatal(err)
				}
				ui.Win.SetContent(meeting.BuildMeetingTab(ui))
				meeting.EnsureMeetingCommitCache(ui)
				ui.MeetingCommits[ui.MeetingStart] = commits
				meeting.DrawMeeting(ui)
				root = ui.MeetingBox
			}
			if !contains(labels(root), "2 commits across") {
				t.Fatal("the count should include both orgs before filtering")
			}
			count := "1"
			if name == "meeting" {
				count = "2" // one commit and one local row governed by the key
			}
			if !contains(labels(root), "exampleorg ("+count+")") {
				t.Fatalf("the key should show each org's unfiltered count: %v", labels(root))
			}
			tapKey(t, root, "exampleorg")
			got := labels(root)
			if contains(got, "Platform commit") || !contains(got, "Support commit") || !contains(got, "1 commit across") {
				t.Fatalf("the count and commit bubbles must follow the org filter: %v", got)
			}
			if name == "meeting" && contains(labels(ui.MeetingRowsHolder), "Platform issue") {
				t.Fatal("the Meeting Update sidebar must follow the same org filter")
			}
			reveals := 0
			walkCanvas(root, func(o fyne.CanvasObject) {
				if _, ok := o.(*shell.Reveal); ok {
					reveals++
				}
			})
			if reveals == 0 || !contains(got, "exampleorg ("+count+")") {
				t.Fatal("filtering should replay the entrance and retain the hidden org's original count")
			}
			tapKey(t, root, "exampleorg")
			if !contains(labels(root), "Platform commit") || !contains(labels(root), "2 commits across") {
				t.Fatal("the hidden org must keep its key and restore its commits")
			}
		})
	}
}

// The desktop driver attaches new widgets to a canvas during the next paint.
// Unlike Fyne's test driver, it cannot resolve a just-created filter yet.
type unpaintedFilterDriver struct {
	fyne.Driver
	painted map[fyne.CanvasObject]bool
}

func (d *unpaintedFilterDriver) CanvasForObject(o fyne.CanvasObject) fyne.Canvas {
	if chip, ok := o.(*shell.Tappable); ok && chip.FadeOnTap && !d.painted[o] {
		return nil
	}
	return d.Driver.CanvasForObject(o)
}

func (d *unpaintedFilterDriver) AbsolutePositionForObject(o fyne.CanvasObject) fyne.Position {
	if d.CanvasForObject(o) == nil {
		return fyne.Position{}
	}
	return d.Driver.AbsolutePositionForObject(o)
}

func TestFilterHoverSurvivesReplacementAndClearsOnLeave(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")
	holder := container.NewStack()
	var current *shell.Tappable
	var redraw func()
	redraw = func() {
		current = shell.NewFilterTappable(widget.NewLabel("Org (2)"), redraw)
		holder.Objects = []fyne.CanvasObject{current}
		holder.Refresh()
	}
	redraw()
	w.SetContent(container.NewPadded(container.NewBorder(widget.NewLabel("Toolbar"), nil,
		widget.NewLabel("Before filters"), nil, holder)))
	w.Resize(fyne.NewSize(440, 100))
	first := current
	driver := &unpaintedFilterDriver{Driver: a.Driver(), painted: map[fyne.CanvasObject]bool{first: true}}
	fyne.SetCurrentApp(&motionTestApp{App: a, driver: driver})
	defer fyne.SetCurrentApp(a)
	first.MouseIn(nil)
	first.Tapped(nil)
	second := current
	if second == first || second.Feedback.Level != .24 {
		t.Fatal("the replacement should keep the hover without another mouse movement")
	}
	driver.painted[second] = true
	second.Tapped(nil)
	third := current
	if third == second || third.Feedback.Level != .24 {
		t.Fatal("repeated clicks should preserve the replacement's hover")
	}
	first.MouseOut()
	if third.Feedback.Level != 0 {
		t.Fatal("leaving the original hover target should clear the replacement highlight")
	}
}

func TestOrgIssueAndPRFiltersKeepHoverBeforeNextPaint(t *testing.T) {
	for _, label := range []string{"org (2)", "Issues", "Pull Request"} {
		t.Run(label, func(t *testing.T) {
			a := test.NewApp()
			defer a.Quit()
			w := a.NewWindow("test")
			holder := container.NewStack()
			hidden := map[string]bool{}
			var redraw func()
			redraw = func() {
				var row fyne.CanvasObject
				if label == "org (2)" {
					row = shell.OrgFilter(backend.Config{}, map[string]int{"org": 2}, hidden, strconv.Itoa, func(org string) {
						hidden[org] = !hidden[org]
						redraw()
					})
				} else {
					row = shell.ItemTypeControls(func(it backend.SprintItem) bool { return !hidden[it.ItemType()] }, func(selected []string) {
						hidden[backend.KanbanIssues], hidden[backend.KanbanPullRequests] = true, true
						for _, kind := range selected {
							hidden[kind] = false
						}
						redraw()
					})
				}
				holder.Objects = []fyne.CanvasObject{container.NewHScroll(row)}
				holder.Refresh()
			}
			redraw()
			page := container.NewBorder(widget.NewLabel("Toolbar"), nil, widget.NewLabel("Before filters"), nil, holder)
			w.SetContent(shell.NewPillTabs(container.NewTabItem("Filters", page)))
			w.Resize(fyne.NewSize(800, 150))
			find := func() *shell.Tappable {
				var hit *shell.Tappable
				walkCanvas(holder, func(o fyne.CanvasObject) {
					if chip, ok := o.(*shell.Tappable); ok && contains(labels(chip.Content), label) {
						hit = chip
					}
				})
				return hit
			}
			first := find()
			if first == nil {
				t.Fatal("filter button missing")
			}
			driver := &unpaintedFilterDriver{Driver: a.Driver(), painted: map[fyne.CanvasObject]bool{first: true}}
			fyne.SetCurrentApp(&motionTestApp{App: a, driver: driver})
			defer fyne.SetCurrentApp(a)
			first.MouseIn(nil)
			at := driver.AbsolutePositionForObject(first).Add(fyne.NewPos(first.Size().Width/2, first.Size().Height/2))
			first.Tapped(&fyne.PointEvent{AbsolutePosition: at})
			next := find()
			if next == nil || next == first || driver.CanvasForObject(next) != nil || next.Feedback.Level != .24 {
				t.Fatal("the real filter control must retain its hover before the desktop driver knows its new canvas")
			}
			first.MouseOut()
			if next.Feedback.Level != 0 {
				t.Fatal("the filter highlight must clear normally when the pointer leaves")
			}
		})
	}
}

func TestStatusFilterFollowsMonthArrowAndKeepsItsScroll(t *testing.T) {
	ui := statusUI(t)
	defer fyne.CurrentApp().Quit()
	key := shell.StatusCacheKey(ui.CalMonth)
	for i := 0; i < 12; i++ {
		ui.ProjItems[key] = append(ui.ProjItems[key], backend.WorklogItem{
			Date: "2026-08-25", Minutes: 1, URL: fmt.Sprintf("https://github.com/long-organisation-%d/repo/issues/1", i),
		})
	}
	ui.Win.Resize(fyne.NewSize(1400, 900))
	status.DrawCalendar(ui)
	var next fyne.CanvasObject
	var filter *widget.Label
	walk(ui.CalBody, func(o fyne.CanvasObject) {
		if row, ok := o.(*fyne.Container); ok && len(row.Objects) == 3 && row.Objects[1] == ui.CalTitle {
			next = row.Objects[2]
		}
	})
	walk(ui.CalLegend, func(o fyne.CanvasObject) {
		if label, ok := o.(*widget.Label); ok && label.Text == "Filter" {
			filter = label
		}
	})
	if next == nil || filter == nil {
		t.Fatal("the Status toolbar should contain its month arrows and filter")
	}
	driver := fyne.CurrentApp().Driver()
	ar, fr := driver.AbsolutePositionForObject(next), driver.AbsolutePositionForObject(filter)
	if gap := fr.X - ar.X - next.Size().Width; gap < 0 || gap > 12 || fr.Y < ar.Y-4 || fr.Y > ar.Y+4 {
		t.Fatalf("the filter should immediately follow the next-month arrow on the same row: arrow %v, filter %v", ar, fr)
	}
	scroll := ui.CalLegend.Objects[0].(*container.Scroll)
	if scroll.Content.MinSize().Width <= scroll.Size().Width {
		t.Fatal("a wide filter key should scroll inside the toolbar")
	}
	scroll.ScrollToOffset(fyne.NewPos(200, 0))
	ui.ToggleOrg("long-organisation-0")
	if ui.CalLegend.Objects[0].(*container.Scroll).Offset.X != 200 {
		t.Fatal("filtering should retain the key's horizontal scroll position")
	}
}

func TestLogListOrgFilterCoversLoggedAndPendingWork(t *testing.T) {
	ui := logListUI(t)
	defer fyne.CurrentApp().Quit()
	key := shell.StatusCacheKey("2026-08")
	ui.ProjLoaded[key] = true
	ui.ProjItems[key] = []backend.WorklogItem{
		{Date: "2026-08-25", Minutes: 60, URL: "https://github.com/exampleorg/repo/issues/1", Title: "Logged platform"},
		{Date: "2026-08-25", Minutes: 60, URL: "https://github.com/Example-Support/repo/issues/2", Title: "Logged support"},
	}
	ui.PendingLoaded = true
	ui.Pending.Groups = []backend.Group{{Issue: "exampleorg/repo#3", Commits: []backend.Commit{{Sha: "1234567"}}}}
	ui.IssueInfo["exampleorg/repo#3"] = backend.IssueInfo{Title: "Pending platform"}
	loglist.DrawLogList(ui)
	// The key trails its available space, immediately before Refresh.
	ui.Win.Resize(fyne.NewSize(2400, 1000))
	ui.ListBody.Refresh()
	refresh := buttonNamed(ui.ListHead, "Refresh from GitHub")
	driver := fyne.CurrentApp().Driver()
	legendRight := driver.AbsolutePositionForObject(ui.ListLegend).X + ui.ListLegend.Size().Width
	if refresh == nil || driver.AbsolutePositionForObject(refresh).X < legendRight || driver.AbsolutePositionForObject(refresh).X-legendRight > 12 {
		t.Fatal("the Log List filter should stand immediately before Refresh from GitHub")
	}
	tapKey(t, ui.ListLegend, "exampleorg")
	got := labels(ui.ListBox)
	if contains(got, "Logged platform") || contains(got, "Pending platform") || !contains(got, "Logged support") {
		t.Fatalf("both Log List panels must follow the filter: %v", got)
	}
	tapKey(t, ui.ListLegend, "exampleorg")
	if !contains(labels(ui.ListBox), "Logged platform") || !contains(labels(ui.ListBox), "Pending platform") {
		t.Fatal("the key must restore both logged and pending work")
	}
}

func TestKanbanAndSprintCalendarProjectKeysUseButtonFades(t *testing.T) {
	ui, _, _ := plannerFixture(t)
	k := ui.Kanban
	for _, calendar := range []bool{false, true} {
		legend := k.ProjectLegend(calendar)
		var chip *shell.ProjectChip
		walkCanvas(legend, func(o fyne.CanvasObject) {
			if c, ok := o.(*shell.ProjectChip); ok {
				chip = c
			}
		})
		if chip == nil || !chip.Chip.FadeOnTap {
			t.Fatalf("calendar=%v: project toggle must fade its button", calendar)
		}
		chip.Chip.Tapped(nil)
		if k.VisibleBoard(chip.Board) {
			t.Fatalf("calendar=%v: the project key must hide its board", calendar)
		}
		chip.Chip.Tapped(nil)
		if !k.VisibleBoard(chip.Board) {
			t.Fatalf("calendar=%v: the project key must restore its board", calendar)
		}
	}
}

func TestLogWorkFilterStartsImmediatelyAfterThisWeekAndScrolls(t *testing.T) {
	ui := weekUI(t)
	defer fyne.CurrentApp().Quit()
	for i := 0; i < 12; i++ {
		ui.Pending.Groups = append(ui.Pending.Groups, backend.Group{
			Issue: fmt.Sprintf("long-organisation-%d/repo#1", i), Commits: []backend.Commit{{Sha: "1234567"}},
		})
	}
	logwork.DrawRecent(ui)
	head := ui.WeekBox.Objects[0]
	head.Resize(fyne.NewSize(1500, head.MinSize().Height))
	head.Refresh()
	here := buttonNamed(head, "This week")
	var filter *widget.Label
	var scroll *container.Scroll
	walk(ui.LogLegend, func(o fyne.CanvasObject) {
		if label, ok := o.(*widget.Label); ok && label.Text == "Filter" {
			filter = label
		}
		if s, ok := o.(*container.Scroll); ok {
			scroll = s
		}
	})
	if filter == nil || here == nil || scroll == nil {
		t.Fatal("the week toolbar should contain its navigation and scrollable key")
	}
	driver := fyne.CurrentApp().Driver()
	gap := driver.AbsolutePositionForObject(filter).X - driver.AbsolutePositionForObject(here).X - here.Size().Width
	if gap < 0 || gap > 12 {
		t.Fatalf("Filter must immediately follow This week, gap=%g", gap)
	}
	if scroll.Content.MinSize().Width <= scroll.Size().Width {
		t.Fatal("a long org key should overflow into a horizontal scroll")
	}
	scroll.ScrollToOffset(fyne.NewPos(200, 0))
	ui.ToggleOrg("long-organisation-0")
	if current := ui.LogLegend.Objects[0].(*container.Scroll); current.Offset.X != 200 {
		t.Fatalf("toggling an org must keep the key scrolled to the same place, got %v", current.Offset)
	}
}
