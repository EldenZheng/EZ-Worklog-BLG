package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/menus/kanban"
	"worklog/internal/menus/sprintcalendar"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// Two meetings on a day read as MEETING#1 and MEETING#2, with their
// descriptions, on the calendar and in Log Work's planned tooltip; an issue
// reads as owner/repo#number.
func TestPlannedBlocksAreNumberedDescribedAndListed(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	k := ui.Kanban
	k.Plan.Boards[0].Items[0].URL = "https://github.com/acme/app/issues/7"
	key := k.Plan.Boards[0].Items[0].Key(k.Plan.Boards[0].ID) // the URL, now there is one
	// Next week: blocks on days already gone are pruned.
	k.Week = backend.AddDays(k.Week, 7)
	day := k.Week
	k.Plan.Tasks[shell.PlannerMeetingKey] = backend.SprintTask{Blocks: []backend.PlanBlock{
		{Date: day, Start: 660, Minutes: 60},
		{Date: day, Start: 540, Minutes: 30, Note: "Weekly meeting"},
	}}
	k.Plan.Tasks[key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: day, Start: 570, Minutes: 60}}}
	kanban.Draw(k)
	lines := sprintcalendar.PlannedLines(ui, day, nil, nil)
	var got []string
	for _, l := range lines {
		got = append(got, fmt.Sprintf("%s (%s) · %dm", l.Title, l.Detail, l.Minutes))
	}
	want := []string{"MEETING#1 (Weekly meeting) · 30m", "Plan me (app#7) · 60m", "MEETING#2 () · 60m"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tooltip lines %q, want %q", got, want)
	}
	if lines[1].DetailColor == nil {
		t.Fatal("repo#number should take its project's colour")
	}
	if got := sprintcalendar.PlannedLines(ui, backend.AddDays(day, 1), nil, nil); len(got) != 0 {
		t.Fatalf("a day with no plan has no tooltip: %v", got)
	}

	w.Content().Refresh()
	var texts []string
	var links []string
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		switch x := o.(type) {
		case *canvas.Text:
			texts = append(texts, x.Text)
		case *widget.Hyperlink:
			links = append(links, x.Text)
		}
	})
	all := strings.Join(texts, "|")
	if !strings.Contains(all, "MEETING#1") || !strings.Contains(all, "MEETING#2") {
		t.Fatalf("meeting blocks not numbered: %v", texts)
	}
	if joined := strings.Join(links, "|"); !strings.Contains(joined, "app#7") || strings.Contains(joined, "acme/") {
		t.Fatalf("issue link should name repo#number without the owner, got %v", links)
	}
	if !strings.Contains(all, "left") {
		t.Fatalf("day header should say how much is left: %v", texts)
	}
}

func TestMeetingNoteSavesAndFollowsAMove(t *testing.T) {
	_, _, c := plannerFixture(t)
	day := c.Sprint.StartDate
	block := backend.PlanBlock{Date: day, Start: 540, Minutes: 30}
	p := backend.SprintPlan{Tasks: map[string]backend.SprintTask{c.Key: {Blocks: []backend.PlanBlock{block}}}}
	if _, err := backend.SetPlannerBlockNote(p, c.Key, 0, backend.PlanBlock{Date: day, Start: 600, Minutes: 30}, "x"); err == nil {
		t.Fatal("a note for a block that changed was saved")
	}
	noted, err := backend.SetPlannerBlockNote(p, c.Key, 0, block, "  Standup ")
	if err != nil || noted.Tasks[c.Key].Blocks[0].Note != "Standup" || p.Tasks[c.Key].Blocks[0].Note != "" {
		t.Fatal("note not saved on a copy of the plan", err)
	}
	moved, err := backend.MovePlannerIssue(noted, c, 0, backend.AddDays(day, 1))
	if err != nil || moved.Tasks[c.Key].Blocks[0].Note != "Standup" {
		t.Fatal("a moved block lost its note", err)
	}
	resized, err := backend.ResizePlannerBlock(noted, c.Key, 0, 45)
	if err != nil || resized.Tasks[c.Key].Blocks[0].Note != "Standup" {
		t.Fatal("a resized block lost its note", err)
	}
}

func TestSprintCalendarTraySearch(t *testing.T) {
	_, _, c := plannerFixture(t)
	c.Item.URL = "https://github.com/acme/app/issues/7"
	for query, want := range map[string]bool{"": true, "plan": true, "ACME/APP#7": true, "nothing": false} {
		if got := sprintcalendar.TrayMatches(c, query); got != want {
			t.Errorf("TrayMatches(%q) = %v, want %v", query, got, want)
		}
	}
}

// A pull request on the calendar links to the issue it works on, looked up
// once and remembered in the plan.
func TestPullRequestLinksToItsIssue(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	k := ui.Kanban
	pr := "https://github.com/acme/app/pull/9"
	k.Plan.Boards[0].Items[0].URL = pr
	k.Plan.Boards[0].Items[0].Kind = "PullRequest"
	asked := 0
	release := make(chan struct{})
	old := backend.FetchCodeReviewLookup
	backend.FetchCodeReviewLookup = func(url string) (backend.CodeReviewLookup, error) {
		asked++
		<-release // answered only once the draw that asked is over
		return backend.CodeReviewLookup{URL: url, IssueRefs: []string{"acme/app#7"}}, nil
	}
	oldInfo := backend.FetchIssueInfo
	backend.FetchIssueInfo = func(ref string) (backend.IssueInfo, error) {
		return backend.IssueInfo{Title: "Fix login"}, nil
	}
	t.Cleanup(func() { backend.FetchCodeReviewLookup, backend.FetchIssueInfo = old, oldInfo })
	k.CalendarActive = true
	kanban.Draw(k)
	// The test driver runs fyne.Do on the lookup's goroutine rather than the
	// UI thread: keep that goroutine from redrawing alongside this one.
	k.CalendarActive = false
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for k.Plan.PRIssues[pr].URL == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if k.Plan.PRIssues[pr] != (backend.PRIssue{URL: "https://github.com/acme/app/issues/7", Title: "Fix login"}) {
		t.Fatalf("PR issue not remembered: %v", k.Plan.PRIssues)
	}
	saved, _ := ui.Store.LoadSprintPlan()
	if saved.PRIssues[pr].URL == "" {
		t.Fatal("PR issue not saved")
	}
	kanban.Draw(k)
	w.Content().Refresh()
	var links []string
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		switch x := o.(type) {
		case *widget.Hyperlink:
			links = append(links, x.Text)
		case *shell.HoverLink:
			links = append(links, x.Text)
		}
	})
	if !strings.Contains(strings.Join(links, "|"), "app#7") || asked != 1 {
		t.Fatalf("PR should link to app#7 after one lookup: %v, asked %d", links, asked)
	}
	// The tray card and the block are named after the issue, with the PR mark.
	k.Plan.Tasks[pr] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: k.Week, Start: 540, Minutes: 60}}}
	kanban.Draw(k)
	w.Content().Refresh()
	var texts []string
	icons := 0
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		switch x := o.(type) {
		case *canvas.Text:
			texts = append(texts, x.Text)
		case *widget.Label:
			texts = append(texts, x.Text)
		case *widget.Icon:
			if x.Resource == k.Plan.Boards[0].Items[0].TypeIcon() {
				icons++
			}
		}
	})
	joined := strings.Join(texts, "|")
	if strings.Count(joined, "Fix login") < 2 || strings.Contains(joined, "Plan me") {
		t.Fatalf("PR bubbles should carry the issue's title: %v", texts)
	}
	if icons < 2 {
		t.Fatalf("tray card and block should both show the PR mark, found %d", icons)
	}
	// Log Work's tooltip names the issue, not the PR.
	k.Plan.Tasks[pr] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: backend.AddDays(k.Week, 7), Start: 540, Minutes: 60}}}
	if l := sprintcalendar.PlannedLines(ui, backend.AddDays(k.Week, 7), nil, nil); len(l) != 1 || l[0].Title != "Fix login" || l[0].Detail != "app#7" {
		t.Fatalf("PR tooltip line %+v", l)
	}
}

// Hovering a tray card edges its blocks on the calendar in gold; the
// Issues | Pull Request key filters the tray and is saved.
func TestTrayHoverMarksBlocksAndTypesFilter(t *testing.T) {
	ui, w, c := plannerFixture(t)
	k := ui.Kanban
	k.Week = backend.AddDays(k.Week, 7)
	k.Plan.Tasks[c.Key] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: k.Week, Start: 540, Minutes: 60}}}
	kanban.Draw(k)
	w.Content().Refresh()
	var tray *shell.PlannerDragCard
	var face *sprintcalendar.PlannerTimedFace
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		switch x := o.(type) {
		case *shell.PlannerDragCard:
			if x.Index < 0 && x.Candidate.Key == c.Key {
				tray = x
			}
		case *sprintcalendar.PlannerTimedFace:
			face = x
		}
	})
	if tray == nil || face == nil {
		t.Fatal("tray card or block missing")
	}
	stroke := func() float32 {
		for _, o := range test.WidgetRenderer(face).Objects() {
			if r, ok := o.(*canvas.Rectangle); ok {
				return r.StrokeWidth
			}
		}
		return 0
	}
	tray.MouseIn(nil)
	if stroke() != 2 {
		t.Fatal("block not edged while its tray card is hovered")
	}
	tray.MouseOut()
	if stroke() != 1 {
		t.Fatal("block still edged after the pointer left")
	}
	types := []string{backend.KanbanPullRequests}
	k.Plan.CalendarTypes = &types
	if k.VisibleCalendarType(c.Item) || !k.VisibleItemType(c.Item) {
		t.Fatal("calendar type choice should hide issues in the tray only")
	}
}

// The small Delete's icon is drawn under a new name at each step of its fade:
// Fyne caches a drawn icon by name, so one name kept it as faint as the frame
// it was first drawn in.
func TestSmallDeleteIconFollowsTheFade(t *testing.T) {
	plannerFixture(t)
	b := widget.NewButtonWithIcon("Delete", theme.DeleteIcon(), nil)
	d := shell.SmallDeleteButton(b, b)
	d.FadeTo(true)
	deadline := time.Now().Add(2 * time.Second)
	for !strings.HasPrefix(b.Icon.Name(), "fade24_") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.HasPrefix(b.Icon.Name(), "fade24_") || !strings.HasSuffix(b.Icon.Name(), theme.DeleteIcon().Name()) {
		t.Fatalf("faded-in icon named %q", b.Icon.Name())
	}
}

// The Sprint Issues tray keeps its width however long a card's title or link.
func TestTrayWidthIgnoresItsCards(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	k := ui.Kanban
	kanban.Draw(k)
	w.Content().Refresh()
	before := k.CalendarIssueClip.MinSize().Width
	k.Plan.Boards[0].Items[0].Title = strings.Repeat("Unbreakable", 20)
	k.Plan.Boards[0].Items[0].URL = "https://github.com/acme/" + strings.Repeat("long-repository-name", 5) + "/issues/7"
	kanban.Draw(k)
	w.Content().Refresh()
	if after := k.CalendarIssueClip.MinSize().Width; after != before {
		t.Fatalf("tray width followed its cards: %v then %v", before, after)
	}
}

// A plan saved by the build that kept PR lookups as bare links still opens,
// and a setting this version cannot read no longer shuts the whole planner.
func TestSprintPlanReadsOldAndOddSettings(t *testing.T) {
	dir := tempDir(t)
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(dir, "sprint-planner.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store := backend.NewStore(dir)
	write(`{"Tasks":{"a":{"Estimate":30,"Blocks":[{"Date":"2026-11-09","Start":540,"Minutes":60}]}},"PRIssues":{"https://github.com/o/r/pull/3":"https://github.com/o/r/issues/5"}}`)
	p, err := store.LoadSprintPlan()
	if err != nil || p.PRIssues["https://github.com/o/r/pull/3"].URL != "https://github.com/o/r/issues/5" || p.Tasks["a"].Estimate != 30 {
		t.Fatalf("old PR lookups should still read: %v %+v", err, p)
	}
	write(`{"Tasks":{"a":{"Estimate":30}},"CalendarTypes":"oops","CalendarMode":"Month"}`)
	p, err = store.LoadSprintPlan()
	if err != nil || p.Tasks["a"].Estimate != 30 || p.CalendarMode != "Month" || p.CalendarTypes != nil {
		t.Fatalf("an unreadable setting should be dropped, not block the plan: %v %+v", err, p)
	}
	write(`{"Tasks":"broken"}`)
	if _, err = store.LoadSprintPlan(); err == nil {
		t.Fatal("unreadable tasks must still be refused so the file is preserved")
	}
}

// A block's repo#number is underlined under the pointer, as Kanban's links
// are; Log List and Meeting update cards show theirs in gold behind the issue
// mark.
func TestBlockLinkUnderlinesAndSummaryRefIsMarked(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	k := ui.Kanban
	issue := "https://github.com/acme/app/issues/5"
	k.Plan.Boards[0].Items[0].URL = issue
	k.Week = backend.AddDays(k.Week, 7)
	k.Plan.Tasks[issue] = backend.SprintTask{Blocks: []backend.PlanBlock{{Date: k.Week, Start: 540, Minutes: 60}}}
	kanban.Draw(k)
	w.Content().Refresh()
	var link *sprintcalendar.PlannerIssueLink
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		if f, ok := o.(*sprintcalendar.PlannerTimedFace); ok {
			for _, child := range test.WidgetRenderer(f).Objects() {
				if l, ok := child.(*sprintcalendar.PlannerIssueLink); ok {
					link = l
				}
			}
		}
	})
	if link == nil || link.Text != "app#5" {
		t.Fatal("block link missing", link)
	}
	underlined := func() bool {
		for _, o := range test.WidgetRenderer(link).Objects() {
			if r, ok := o.(*canvas.Rectangle); ok {
				return r.Visible()
			}
		}
		return false
	}
	if underlined() {
		t.Fatal("link underlined at rest")
	}
	link.MouseIn(nil)
	if !underlined() {
		t.Fatal("link not underlined under the pointer")
	}
	link.MouseOut()
	if underlined() {
		t.Fatal("underline stayed after the pointer left")
	}

	card := shell.IssueSummaryCard(backend.Config{}, "org/repo#1", "Title", "60m")
	test.NewTempWindow(t, card)
	marked, ref := false, false
	walkCanvas(card, func(o fyne.CanvasObject) {
		switch x := o.(type) {
		case *widget.Icon:
			marked = marked || x.Resource == (backend.SprintItem{}).TypeIcon()
		case *widget.Label:
			ref = ref || x.Text == "repo#1"
		}
	})
	if !marked || !ref {
		t.Fatal("summary card should show repo#1 behind the issue mark", marked, ref)
	}
}

// A block's text stays inside it, however short the block or long the title.
func TestBlockTextStaysInsideTheBlock(t *testing.T) {
	ui, w, _ := plannerFixture(t)
	k := ui.Kanban
	issue := "https://github.com/acme/app/issues/6"
	k.Plan.Boards[0].Items[0].URL = issue
	k.Plan.Boards[0].Items[0].Title = "Averyveryverylongsingleword-that-cannot-wrap-anywhere and more words after it to wrap"
	k.Week = backend.AddDays(k.Week, 7)
	k.Plan.Tasks[issue] = backend.SprintTask{Blocks: []backend.PlanBlock{
		{Date: k.Week, Start: 540, Minutes: 15},
		{Date: k.Week, Start: 600, Minutes: 30},
		{Date: k.Week, Start: 660, Minutes: 45},
		{Date: k.Week, Start: 720, Minutes: 120},
	}}
	kanban.Draw(k)
	w.Content().Refresh()
	faces := 0
	walkCanvas(k.CalendarBody, func(o fyne.CanvasObject) {
		f, ok := o.(*sprintcalendar.PlannerTimedFace)
		if !ok {
			return
		}
		faces++
		size := f.Size()
		for _, child := range test.WidgetRenderer(f).Objects() {
			if !child.Visible() {
				continue
			}
			if _, bg := child.(*canvas.Rectangle); bg {
				continue // the fill and the sliding stripe
			}
			p, s := child.Position(), child.Size()
			if _, isText := child.(*canvas.Text); isText {
				s = child.MinSize()
			}
			if p.X < 0 || p.Y < 0 || p.X+s.Width > size.Width+0.5 || p.Y+s.Height > size.Height+0.5 {
				t.Fatalf("%T at %v size %v leaves a %v block", child, p, s, size)
			}
		}
	})
	if faces != 4 {
		t.Fatal("blocks missing", faces)
	}
}

func TestPlannedTooltipPutsReferenceBeforeTitleAndTime(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	body := shell.PlannedTipBody([]shell.PlannedLine{{Title: "Fix login", Detail: "app#7", Minutes: 60}})
	var text string
	walk(body, func(o fyne.CanvasObject) {
		if line, ok := o.(*canvas.Text); ok {
			text += line.Text
		}
	})
	if !strings.HasPrefix(text, "(app#7) Fix login") || !strings.HasSuffix(text, "60m") {
		t.Fatalf("tooltip order: %q", text)
	}
}
