package tests

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"worklog/internal/assemble"
	"worklog/internal/menus/kanban"
	"worklog/internal/menus/logwork"
	"worklog/internal/menus/projects"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
	"worklog/internal/shared/widgets"
)

// Issues are pasted however they were copied: links, refs, one a line or run
// together. Each comes out once as a ref, and what is not an issue is named
// rather than dropped.
func TestProjectIssuesArePastedAsLinksOrRefs(t *testing.T) {
	refs, unread := projects.ParseIssues(`https://github.com/exampleorg/shop/issues/12
exampleorg/shop#40, github.com/exampleorg/api/issues/7#issuecomment-1
https://github.com/exampleorg/shop/issues/12?notification=1
not-an-issue https://github.com/exampleorg/shop/pull/9`)
	want := []string{"exampleorg/shop#12", "exampleorg/shop#40", "exampleorg/api#7"}
	if !slices.Equal(refs, want) {
		t.Fatalf("refs = %v, want %v", refs, want)
	}
	if !slices.Equal(unread, []string{"not-an-issue", "https://github.com/exampleorg/shop/pull/9"}) {
		t.Fatalf("unread = %v", unread)
	}
}

// Projects are kept in their own file beside the worklog, and a missing file
// is simply no projects yet.
func TestProjectsAreSavedBesideTheWorklog(t *testing.T) {
	store := backend.NewStore(tempDir(t))
	got, err := projects.Load(store)
	if err != nil || len(got.Projects) != 0 {
		t.Fatalf("a missing file should load as no projects: %v, %v", got, err)
	}
	saved := projects.Saved{Selected: "b", Projects: []projects.Project{
		{ID: "a", Name: "Checkout", Issues: []string{"exampleorg/shop#12"}},
		{ID: "b", Name: "Search", Issues: []string{"exampleorg/shop#40", "exampleorg/api#7"}},
	}}
	if err := projects.Save(store, saved); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Dir, "projects.json")); err != nil {
		t.Fatal(err)
	}
	got, err = projects.Load(store)
	if err != nil {
		t.Fatal(err)
	}
	if got.Current().Name != "Search" || len(got.Projects) != 2 || got.Projects[1].Issues[1] != "exampleorg/api#7" {
		t.Fatalf("round trip lost something: %+v", got)
	}
	// The tabs tell projects apart by name.
	if got.CheckName("search", "a") == "" || got.CheckName("+", "") == "" || got.CheckName(" ", "") == "" {
		t.Fatal("a taken, reserved or empty name should be refused")
	}
	if text := got.CheckName("Search", "b"); text != "" {
		t.Fatalf("a project may keep its own name: %s", text)
	}
}

func projectItem(n int, created, closed string, who ...string) projects.Item {
	at := func(s string) time.Time {
		if s == "" {
			return time.Time{}
		}
		d, err := time.ParseInLocation("2006-01-02", s, time.Local)
		if err != nil {
			panic(err)
		}
		return d.Add(12 * time.Hour)
	}
	return projects.Item{Number: n, Title: fmt.Sprintf("Task %d", n), URL: fmt.Sprintf("https://github.com/exampleorg/shop/issues/%d", n),
		Repo: "exampleorg/shop", Created: at(created), Closed: closed != "", ClosedAt: at(closed), Assignees: who}
}

// sampleProject is two parents, as of Thursday 12 November 2026.
func sampleProject() (projects.Snapshot, time.Time) {
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.Local)
	sprint := func(it projects.Item, name, start string, priority string) projects.Item {
		it.Sprint, it.SprintDays, it.Priority = name, 14, priority
		if start != "" {
			it.SprintStart, _ = time.ParseInLocation("2006-01-02", start, time.Local)
		}
		return it
	}
	inReview := sprint(projectItem(4, "2026-10-20", "", "bea"), "Sprint 2", "2026-11-02", "P1")
	inReview.Status = "In review"
	inProgress := sprint(projectItem(5, "2026-10-21", "", "ana", "bea"), "Sprint 2", "2026-11-02", "Low")
	inProgress.Status = "In Progress"
	dropped := projectItem(8, "2026-10-06", "2026-10-08")
	dropped.NotPlanned = true
	early := sprint(projectItem(2, "2026-10-05", "2026-10-09", "ana"), "Sprint 1", "2026-10-19", "High")
	return projects.Snapshot{Fetched: now, Parents: []projects.Parent{
		{Ref: "exampleorg/shop#1", Title: "Checkout epic", URL: "https://github.com/exampleorg/shop/issues/1", Items: []projects.Item{
			early,
			projectItem(3, "2026-10-05", "2026-11-03", "ana"),
			inReview, inProgress,
			projectItem(6, "2026-11-09", ""),
		}},
		{Ref: "exampleorg/shop#20", Title: "Search epic", Items: []projects.Item{
			projectItem(7, "2026-10-12", "2026-11-10", "bea"),
			dropped,
		}},
		{Ref: "exampleorg/shop#99", Err: "Issue not found."},
	}}, now
}

// The figures behind the page: scope and progress, who holds what, the
// weekly history, and a finish date from the recent pace.
func TestProjectSummaryCountsProgressPeopleAndPace(t *testing.T) {
	snap, now := sampleProject()
	s := projects.Summarize(snap, now)

	if s.Total != 6 || s.Done != 3 || s.Open != 3 || s.NotPlanned != 1 || s.Percent() != 50 {
		t.Fatalf("scope: %+v", s)
	}
	if s.UnassignedOpen != 1 || s.ClosedLast7 != 1 || s.OpenedLast7 != 1 {
		t.Fatalf("unassigned %d, closed %d, opened %d in the last week", s.UnassignedOpen, s.ClosedLast7, s.OpenedLast7)
	}

	// Finished first, then open states furthest along first, dropped last.
	var order []string
	for _, slice := range s.Statuses {
		order = append(order, fmt.Sprintf("%s=%d", slice.Label, slice.Count))
	}
	if got := strings.Join(order, " "); got != "Done=3 In Progress=1 In review=1 No status=1 Not planned=1" {
		t.Fatalf("status slices: %s", got)
	}

	// An issue with two assignees counts for both of them.
	if len(s.People) != 2 || s.People[0] != (projects.PersonLoad{Name: "ana", Done: 2, Open: 1}) ||
		s.People[1] != (projects.PersonLoad{Name: "bea", Done: 1, Open: 2}) {
		t.Fatalf("people: %+v", s.People)
	}
	if s.Unassigned.Open != 1 || s.Unassigned.Done != 0 {
		t.Fatalf("unassigned: %+v", s.Unassigned)
	}

	// Six weeks, Monday 5 October to this one, counted cumulatively.
	if len(s.Weeks) != 6 || s.Weeks[0].Start.Format("2006-01-02") != "2026-10-05" {
		t.Fatalf("weeks: %+v", s.Weeks)
	}
	first, last := s.Weeks[0], s.Weeks[5]
	if first.Scope != 2 || first.Done != 1 || first.Closed != 1 {
		t.Fatalf("first week: %+v", first)
	}
	if last.Scope != 6 || last.Done != 3 || last.Closed != 1 {
		t.Fatalf("this week: %+v", last)
	}

	// Two finished in the last four weeks: half an issue a week, so the three
	// still open take six weeks.
	if s.Velocity != 0.5 {
		t.Fatalf("velocity = %v", s.Velocity)
	}
	if got := s.Finish.Format("2006-01-02"); got != "2026-12-24" {
		t.Fatalf("finish = %s", got)
	}
	if got := s.ProjectionWeeks(now); got != 7 {
		t.Fatalf("projection = %d weeks", got)
	}

	if len(s.Epics) != 3 || s.Epics[0].Done != 2 || s.Epics[0].Total != 5 || s.Epics[1].Done != 1 || s.Epics[1].Total != 1 {
		t.Fatalf("epics: %+v", s.Epics)
	}
	if len(s.Oldest) != 3 || s.Oldest[0].Number != 4 || s.Oldest[2].Number != 6 {
		t.Fatalf("longest open: %+v", s.Oldest)
	}

	// Sprints in date order, the one running now marked, no sprint last.
	var sprints []string
	for _, b := range s.Sprints {
		sprints = append(sprints, fmt.Sprintf("%s=%d/%d now=%v", b.Name, b.Done, b.Total(), b.Current))
	}
	if got := strings.Join(sprints, " "); got != "Sprint 1=1/1 now=false Sprint 2=0/2 now=true No sprint=2/3 now=false" {
		t.Fatalf("sprints: %s", got)
	}
	// Most urgent first, whatever the board calls them.
	var priorities []string
	for _, b := range s.Priorities {
		priorities = append(priorities, fmt.Sprintf("%s=%d", b.Name, b.Total()))
	}
	if got := strings.Join(priorities, " "); got != "High=1 P1=1 Low=1 No priority=3" {
		t.Fatalf("priorities: %s", got)
	}

	// Who finished what each week, by first assignee, with the pace across it.
	c := s.Credit
	if len(c.Weeks) != 6 || !slices.Equal(c.Names, []string{"ana", "bea"}) {
		t.Fatalf("credit: %+v", c)
	}
	if !slices.Equal(c.Counts[0], []int{1, 0}) || !slices.Equal(c.Counts[4], []int{1, 0}) || !slices.Equal(c.Counts[5], []int{0, 1}) {
		t.Fatalf("credit counts: %v", c.Counts)
	}
	if c.Pace[0] != 1 || c.Pace[5] != 0.5 {
		t.Fatalf("pace: %v", c.Pace)
	}

	// The four weeks before the last four: one issue, so the pace is level.
	if !s.HasPrev || s.PrevVelocity != 0.25 {
		t.Fatalf("previous pace: %v, %v", s.HasPrev, s.PrevVelocity)
	}
	// Three issues opened in the last four weeks.
	if s.OpenedPace != 0.75 {
		t.Fatalf("opened pace: %v", s.OpenedPace)
	}
	scenarios := s.Scenarios(now)
	if len(scenarios) != 2 || scenarios[0].Pace != 0.5 || !scenarios[0].Finish.Equal(s.Finish) || scenarios[1].Pace != 0.5 {
		t.Fatalf("scenarios: %+v", scenarios)
	}
	// Three open by Christmas Eve, six weeks away: half an issue a week.
	if got := s.NeededPace(now.AddDate(0, 0, 42), now); got != 0.5 {
		t.Fatalf("needed pace = %v", got)
	}
}

// Labels stand in for a priority when no board has one.
func TestProjectPriorityLabels(t *testing.T) {
	for label, want := range map[string]string{"P1": "P1", "p0": "P0", "priority: high": "High", "Priority/Low": "Low", "bug": "", "p9": ""} {
		got, ok := projects.PriorityLabel(label)
		if got != want || ok != (want != "") {
			t.Errorf("%q = %q, %v", label, got, ok)
		}
	}
}

// The CSV holds every field the charts are drawn from, so a spreadsheet can
// draw them again.
func TestProjectIssuesExportAsCSV(t *testing.T) {
	snap, now := sampleProject()
	dir := tempDir(t)
	path, err := projects.ExportIssues(dir, "Checkout / Web", "By sprint: Sprint 2", snap.Items(), now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "project-checkout-web-by-sprint-sprint-2-2026-11-12-1500.csv" {
		t.Fatalf("file name: %s", path)
	}
	rows, err := csv.NewReader(mustOpen(t, path)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 8 || rows[0][0] != "number" || rows[0][len(rows[0])-1] != "days_open" {
		t.Fatalf("rows: %v", rows)
	}
	row := strings.Join(rows[4], "|")
	if row != "5|Task 5|https://github.com/exampleorg/shop/issues/5|exampleorg/shop||open|In Progress|Sprint 2|2026-11-02|Low|ana, bea|2026-10-21||22" {
		t.Fatalf("an open issue: %s", row)
	}
	if got := rows[7][5]; got != "not planned" {
		t.Fatalf("a dropped issue: %v", rows[7])
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// stubChildren answers the child-issue query for one parent, a page at a time.
func stubChildren(t *testing.T, pages map[string]string) func() {
	t.Helper()
	old := backend.Gh
	backend.Gh = func(args []string, _ ...string) (string, error) {
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "subIssues") {
			return old(args)
		}
		if strings.Contains(joined, "num=99") {
			return "", backend.GhErr("Could not resolve to an Issue with the number of 99.")
		}
		after := ""
		for _, a := range args {
			if rest, ok := strings.CutPrefix(a, "after="); ok {
				after = rest
			}
		}
		page, ok := pages[after]
		if !ok {
			t.Errorf("unexpected page %q", after)
		}
		return page, nil
	}
	return func() { backend.Gh = old }
}

const childrenPageOne = `{"data":{"repository":{"issue":{"title":"Checkout epic","url":"https://github.com/exampleorg/shop/issues/1","state":"OPEN",
 "subIssues":{"pageInfo":{"hasNextPage":true,"endCursor":"c1"},"nodes":[
  {"number":2,"title":"Cart","url":"https://github.com/exampleorg/shop/issues/2","state":"CLOSED","stateReason":"COMPLETED",
   "createdAt":"2026-10-05T03:00:00Z","closedAt":"2026-10-09T03:00:00Z","repository":{"nameWithOwner":"exampleorg/shop"},
   "assignees":{"nodes":[{"login":"ana"}]},"labels":{"nodes":[{"name":"bug"},{"name":"P2"}]},
   "projectItems":{"nodes":[{"status":{"name":"Done"},"priority":null,"sprint":{"title":"Sprint 1","startDate":"2026-10-05","duration":14}}]}},
  {"number":3,"title":"Old idea","url":"https://github.com/exampleorg/shop/issues/3","state":"CLOSED","stateReason":"NOT_PLANNED",
   "createdAt":"2026-10-05T03:00:00Z","closedAt":"2026-10-06T03:00:00Z","repository":{"nameWithOwner":"exampleorg/shop"},
   "assignees":{"nodes":[]},"projectItems":{"nodes":[]}}]}}}}}`

const childrenPageTwo = `{"data":{"repository":{"issue":{"title":"Checkout epic","url":"https://github.com/exampleorg/shop/issues/1","state":"OPEN",
 "subIssues":{"pageInfo":{"hasNextPage":false,"endCursor":"c2"},"nodes":[
  {"number":4,"title":"Payment","url":"https://github.com/exampleorg/shop/issues/4","state":"OPEN","stateReason":null,
   "createdAt":"2026-10-20T03:00:00Z","closedAt":null,"repository":{"nameWithOwner":"exampleorg/shop"},
   "assignees":{"nodes":[{"login":"ana"},{"login":"bea"}]},
   "labels":{"nodes":[{"name":"P3"}]},"milestone":{"title":"Beta","dueOn":"2026-12-01T00:00:00Z"},
   "projectItems":{"nodes":[{"id":"PVTI_0","project":{"id":"PVT_0","field":null},"status":null,"priority":null,"sprint":null},
    {"id":"PVTI_4","project":{"id":"PVT_1","field":{"id":"PVTSSF_1","options":[{"id":"o1","name":"Todo"},{"id":"o2","name":"In Progress"},{"id":"o3","name":"Done"}]}},
     "status":{"name":"In Progress"},"priority":{"name":"High"},"sprint":null}]}}]}}}}}`

// A parent's children are read a page at a time, with who holds each and its
// board status. A parent GitHub refuses carries the reason; the others still
// arrive.
func TestProjectFetchReadsEveryPageAndKeepsGoingPastAFailure(t *testing.T) {
	defer stubChildren(t, map[string]string{"": childrenPageOne, "c1": childrenPageTwo})()

	var steps []string
	snap := projects.Fetch([]string{"exampleorg/shop#1", "exampleorg/shop#99"}, func(p backend.FetchProgress) {
		steps = append(steps, fmt.Sprintf("%d/%d", p.Done, p.Total))
	})
	if got := strings.Join(steps, " "); got != "0/2 1/2 2/2" {
		t.Fatalf("progress: %s", got)
	}
	epic, missing := snap.Parents[0], snap.Parents[1]
	if epic.Title != "Checkout epic" || epic.Err != "" || len(epic.Items) != 3 {
		t.Fatalf("epic: %+v", epic)
	}
	cart, idea, payment := epic.Items[0], epic.Items[1], epic.Items[2]
	if !cart.Closed || cart.NotPlanned || cart.ClosedAt.IsZero() || cart.Parent != "exampleorg/shop#1" {
		t.Fatalf("a finished child: %+v", cart)
	}
	if !idea.NotPlanned {
		t.Fatalf("closed as not planned should be marked: %+v", idea)
	}
	if payment.Closed || payment.Status != "In Progress" || !slices.Equal(payment.Assignees, []string{"ana", "bea"}) {
		t.Fatalf("an open child: %+v", payment)
	}
	// The board's Priority wins over a label; a label fills in when there is none.
	if payment.Priority != "High" || payment.Sprint != "" || cart.Priority != "P2" {
		t.Fatalf("priorities: payment %q, cart %q", payment.Priority, cart.Priority)
	}
	if cart.Sprint != "Sprint 1" || cart.SprintDays != 14 || cart.SprintStart.Format("2006-01-02") != "2026-10-05" {
		t.Fatalf("sprint: %+v", cart)
	}
	// The board whose Status can be changed: the first one that has the field.
	if b := payment.Board; b == nil || b.ProjectID != "PVT_1" || b.ItemID != "PVTI_4" || b.FieldID != "PVTSSF_1" || len(b.Options) != 3 {
		t.Fatalf("board: %+v", payment.Board)
	}
	if payment.Milestone != "Beta" || payment.MilestoneDue.IsZero() || cart.Milestone != "" {
		t.Fatalf("milestones: %q %v, %q", payment.Milestone, payment.MilestoneDue, cart.Milestone)
	}
	if !strings.Contains(missing.Err, "Could not resolve") || len(missing.Items) != 0 {
		t.Fatalf("the failed parent should say why: %+v", missing)
	}
}

func projectsUI(t *testing.T, saved projects.Saved) *shell.UI {
	t.Helper()
	w := test.NewApp().NewWindow("t")
	w.Resize(fyne.NewSize(1400, 900))
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w, CalMonth: "2026-08", RepMonth: "2026-08"}
	ui.Cfg = backend.Config{Repos: []string{"exampleorg"}}
	if len(saved.Projects) > 0 {
		if err := projects.Save(ui.Store, saved); err != nil {
			t.Fatal(err)
		}
	}
	assemble.BuildAllTabs(ui)
	w.SetContent(ui.Tabs)
	return ui
}

// openProjects selects the tab as main does: the bar shows it, then the page
// is told it was opened.
func openProjects(ui *shell.UI) {
	ui.Tabs.SelectName(shell.ProjectsTabName)
	projects.Open(ui)
}

func projectsPage(ui *shell.UI) fyne.CanvasObject {
	for _, item := range ui.Tabs.Items {
		if item.Text == shell.ProjectsTabName {
			return item.Content
		}
	}
	return nil
}

// tappableWith finds the clickable piece of the page whose own text holds
// text; with "" it finds one that has no text at all.
func tappableWith(o fyne.CanvasObject, text string) *shell.Tappable {
	var found *shell.Tappable
	walk(o, func(c fyne.CanvasObject) {
		tap, ok := c.(*shell.Tappable)
		if !ok || found != nil {
			return
		}
		own := labels(tap.Content)
		if (text == "" && len(own) == 0) || (text != "" && contains(own, text)) {
			found = tap
		}
	})
	return found
}

// overlayLabels reads the text of whatever popup is on top.
func overlayLabels(ui *shell.UI) []string {
	var out []string
	top := ui.Win.Canvas().Overlays().Top()
	if top == nil {
		return nil
	}
	walkPopup(top, func(o fyne.CanvasObject) {
		switch o.(type) {
		case *fyne.Container, *container.Scroll, *container.ThemeOverride:
			return // read through their children
		}
		out = append(out, labels(o)...)
	})
	return out
}

// underlineTabs finds the project tabs, which labels() does not read into.
func underlineTabs(o fyne.CanvasObject) *shell.UnderlineTabs {
	var found *shell.UnderlineTabs
	walk(o, func(c fyne.CanvasObject) {
		if tabs, ok := c.(*shell.UnderlineTabs); ok {
			found = tabs
		}
	})
	return found
}

// Opening the tab fetches the project last open and draws its charts; the
// projects stand in a row of tabs that ends with the + that adds one.
func TestProjectsTabChartsTheOpenProject(t *testing.T) {
	defer stubChildren(t, map[string]string{"": childrenPageOne, "c1": childrenPageTwo})()
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.Local)
	projects.Now = func() time.Time { return now }
	defer func() { projects.Now = time.Now }()

	ui := projectsUI(t, projects.Saved{Selected: "b", Projects: []projects.Project{
		{ID: "a", Name: "Search", Issues: []string{"exampleorg/shop#99"}},
		{ID: "b", Name: "Checkout", Issues: []string{"exampleorg/shop#1"}},
	}})
	defer fyne.CurrentApp().Quit()

	page := projectsPage(ui)
	tabs := underlineTabs(page)
	if tabs == nil || !slices.Equal(tabs.Options, []string{"Search", "Checkout", "+"}) || tabs.Selected != "Checkout" {
		t.Fatalf("project tabs: %+v", tabs)
	}
	if got := labels(page); contains(got, "Workload By Person") {
		t.Fatalf("nothing should be fetched before the tab is opened: %v", got)
	}

	openProjects(ui)
	got := labels(page)
	for _, want := range []string{"Status", "Workload By Person", "Burn-up", "Finished Report", "Week", "Parent Issues", "Milestones", "Issues",
		"By Sprint", "No sprint", "By Priority", "High", "P2", "4-week pace",
		"50%", "issues done", "ana", "bea", "#4  Payment", "Checkout epic"} {
		if !contains(got, want) {
			t.Fatalf("%q is missing from the page: %v", want, got)
		}
	}

	// The project's name opens its menu: Edit, Refresh and Delete live there.
	tabs.OnMenu("Checkout", tabs)
	if got := overlayLabels(ui); !contains(got, "Edit project") || !contains(got, "Refresh from GitHub") || !contains(got, "Delete project") {
		t.Fatalf("the project's menu: %v", got)
	}
	ui.Win.Canvas().Overlays().Remove(ui.Win.Canvas().Overlays().Top())

	// A figure opens the issues behind it, a bubble each. One is open here.
	test.Tap(tappableWith(page, "with nobody assigned"))
	got = overlayLabels(ui)
	if !contains(got, "Open  ·  1 issue") || !contains(got, "#4  Payment") || !contains(got, "ana, bea") || !contains(got, "In Progress") {
		t.Fatalf("the Open tile should list the open issue: %v", got)
	}
	if contains(got, "#2  Cart") {
		t.Fatalf("a finished issue is not open: %v", got)
	}
	ui.Win.Canvas().Overlays().Remove(ui.Win.Canvas().Overlays().Top())

	// A priority's column opens its issues under a heading per assignee.
	var byPriority *projects.ColumnChart
	walk(page, func(o fyne.CanvasObject) {
		if c, ok := o.(*projects.ColumnChart); ok && contains(labels(c.Content), "High") {
			byPriority = c
		}
	})
	if byPriority == nil {
		t.Fatal("the By Priority chart should be on the page")
	}
	byPriority.Tapped(&fyne.PointEvent{Position: fyne.NewPos(1, 10)})
	if got = overlayLabels(ui); !contains(got, "ana  ·  1") || !contains(got, "#4  Payment") {
		t.Fatalf("the priority's issues should be grouped by assignee: %v", got)
	}
	ui.Win.Canvas().Overlays().Remove(ui.Win.Canvas().Overlays().Top())

	// The Forecast tile opens its workings and the calculator.
	test.Tap(tappableWith(page, "Forecast"))
	got = overlayLabels(ui)
	if !contains(got, "At Other Paces") || !contains(got, "Calculator") || !contains(got, "Finish by") {
		t.Fatalf("the forecast popup: %v", got)
	}
	var paceField *widget.Entry
	walkPopup(ui.Win.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if e, ok := o.(*widget.Entry); ok && strings.HasPrefix(e.PlaceHolder, "issues a week") {
			paceField = e
		}
	})
	paceField.SetText("1")
	if got = overlayLabels(ui); !contains(got, "Finishes on ") {
		t.Fatalf("a pace typed in should give its date: %v", got)
	}
	ui.Win.Canvas().Overlays().Remove(ui.Win.Canvas().Overlays().Top())

	// So does a person's line in the workload list.
	test.Tap(tappableWith(page, ""))
	// (the click areas over the workload lines carry no text of their own)
	if got = overlayLabels(ui); !contains(got, "issue") {
		t.Fatalf("a workload line should open that person's issues: %v", got)
	}
	ui.Win.Canvas().Overlays().Remove(ui.Win.Canvas().Overlays().Top())

	// The other project's parent cannot be read: the page says why.
	tabs.SetSelected("Search")
	got = labels(page)
	if !contains(got, "No child issues") || !contains(got, "Could not resolve to an Issue with the number of 99.") {
		t.Fatalf("a project that cannot be read should say so: %v", got)
	}
	saved, err := projects.Load(ui.Store)
	if err != nil || saved.Selected != "a" {
		t.Fatalf("the open project should be remembered: %+v, %v", saved, err)
	}
	// Each refresh keeps this week's in review and in progress, which GitHub
	// does not remember, for the week panel to show later.
	if got := saved.Find("b").History["2026-11-09"]; got != (projects.WeekCounts{Review: 0, Progress: 1}) {
		t.Fatalf("this week's counts: %+v", saved.Find("b").History)
	}
}

// The week panel shows the picked week; a past week has its in review and in
// progress only when a refresh that week kept them.
func TestProjectWeekPanelWalksBack(t *testing.T) {
	defer stubChildren(t, map[string]string{"": childrenPageOne, "c1": childrenPageTwo})()
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.Local)
	projects.Now = func() time.Time { return now }
	defer func() { projects.Now = time.Now }()
	ui := projectsUI(t, projects.Saved{Selected: "b", Projects: []projects.Project{
		{ID: "b", Name: "Checkout", Issues: []string{"exampleorg/shop#1"},
			History: map[string]projects.WeekCounts{"2026-10-05": {Review: 3, Progress: 7}}},
	}})
	defer fyne.CurrentApp().Quit()
	openProjects(ui)
	page := projectsPage(ui)
	if got := labels(page); !contains(got, "This week") || !contains(got, "Finished by") {
		t.Fatalf("the week panel: %v", got)
	}
	// Back to the first week: Cart was finished, and that week's counts kept.
	for range 5 {
		test.Tap(weekStepButton(page, theme.NavigateBackIcon()))
	}
	got := labels(page)
	if !contains(got, "5 – 11 Oct") || !contains(got, "7") || !contains(got, "as the last refresh that week saw them") {
		t.Fatalf("the first week: %v", got)
	}
	// The week before it was never refreshed.
	test.Tap(weekStepButton(page, theme.NavigateNextIcon()))
	if got = labels(page); !contains(got, "none was made this week") {
		t.Fatalf("an unrecorded week: %v", got)
	}
}

// weekStepButton is the week panel's arrow: the last of its kind on the page,
// after the month arrows on the finished report.
func weekStepButton(o fyne.CanvasObject, icon fyne.Resource) *widget.Button {
	var found *widget.Button
	walk(o, func(c fyne.CanvasObject) {
		if b, ok := c.(*widget.Button); ok && b.Icon != nil && b.Icon.Name() == icon.Name() {
			found = b
		}
	})
	return found
}

// The finished report shows a month at a time: this month to begin with,
// the arrows stepping back and forth, and picking a week takes the columns to
// that week's month. Clicking the month shows every week; clicking again
// comes back to this month.
func TestProjectSinceStartPagesByMonth(t *testing.T) {
	defer stubChildren(t, map[string]string{"": childrenPageOne, "c1": childrenPageTwo})()
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.Local)
	projects.Now = func() time.Time { return now }
	defer func() { projects.Now = time.Now }()
	ui := projectsUI(t, projects.Saved{Selected: "b", Projects: []projects.Project{
		{ID: "b", Name: "Checkout", Issues: []string{"exampleorg/shop#1"}},
	}})
	defer fyne.CurrentApp().Quit()
	openProjects(ui)
	page := projectsPage(ui)
	if got := labels(page); !contains(got, "Nov 2026") || contains(got, "Oct 2026") {
		t.Fatalf("should open on this month: %v", got)
	}
	monthBack := buttonWithIcon(page, theme.NavigateBackIcon())
	monthNext := buttonWithIcon(page, theme.NavigateNextIcon())
	if monthBack.Disabled() || !monthNext.Disabled() {
		t.Fatal("this month can only step back")
	}
	test.Tap(monthBack)
	got := labels(page)
	if !contains(got, "Oct 2026") || !contains(got, "5 Oct") || contains(got, "Nov 2026") {
		t.Fatalf("back a month: %v", got)
	}
	if !buttonWithIcon(page, theme.NavigateBackIcon()).Disabled() {
		t.Fatal("the first month cannot step back")
	}
	// Picking a week puts the columns on its month.
	test.Tap(buttonWithIcon(page, theme.NavigateNextIcon()))
	for range 5 {
		test.Tap(weekStepButton(page, theme.NavigateBackIcon()))
	}
	if got = labels(page); !contains(got, "Oct 2026") {
		t.Fatalf("the columns should follow the week to October: %v", got)
	}

	// The month opens every week since the start, and closes on this month.
	reportTag(t, page, "Oct 2026").Tapped(nil)
	if got = labels(page); !contains(got, "All") || !contains(got, "5 Oct") || contains(got, "12 Oct") {
		t.Fatalf("all weeks, the ends named: %v", got)
	}
	if !buttonWithIcon(page, theme.NavigateBackIcon()).Disabled() || !buttonWithIcon(page, theme.NavigateNextIcon()).Disabled() {
		t.Fatal("with every week showing there is nowhere to step")
	}
	// Leaving the tab and coming back drops All for the Settings range, on the
	// month of the week still picked.
	projects.Open(ui)
	reportTag(t, page, "Oct 2026").Tapped(nil)
	reportTag(t, page, "All").Tapped(nil)
	if got = labels(page); !contains(got, "Nov 2026") || reportTag(t, page, "Nov 2026") == nil {
		t.Fatalf("back to this month: %v", got)
	}
}

// The months the finished report shows at a time come from Settings.
func TestProjectSinceStartPageLength(t *testing.T) {
	defer stubChildren(t, map[string]string{"": childrenPageOne, "c1": childrenPageTwo})()
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.Local)
	projects.Now = func() time.Time { return now }
	defer func() { projects.Now = time.Now }()
	ui := projectsUI(t, projects.Saved{Selected: "b", Projects: []projects.Project{
		{ID: "b", Name: "Checkout", Issues: []string{"exampleorg/shop#1"}},
	}})
	defer fyne.CurrentApp().Quit()
	ui.Cfg.ReportMonths = 3
	openProjects(ui)
	page := projectsPage(ui)
	if got := labels(page); !contains(got, "Oct – Nov 2026") || !contains(got, "5 Oct") {
		t.Fatalf("three months a page: %v", got)
	}
	if !buttonWithIcon(page, theme.NavigateBackIcon()).Disabled() {
		t.Fatal("one page holds the whole project; there is nowhere to step")
	}
}

// reportTag is the finished report's month tag reading text.
func reportTag(t *testing.T, o fyne.CanvasObject, text string) *shell.HoverTag {
	t.Helper()
	var found *shell.HoverTag
	walk(o, func(c fyne.CanvasObject) {
		if tag, ok := c.(*shell.HoverTag); ok && tag.Text == text {
			found = tag
		}
	})
	if found == nil {
		t.Fatalf("no %q tag", text)
	}
	return found
}

// Tapping the picked tab drops its menu; tapping another picks it.
func TestUnderlineTabsMenuOnThePickedTab(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	var menus []string
	tabs := shell.NewUnderlineTabs([]string{"One", "Two", "+"}, nil)
	tabs.Action = "+"
	tabs.OnMenu = func(option string, _ fyne.CanvasObject) { menus = append(menus, option) }
	tabs.SetSelected("One")
	w := a.NewWindow("t")
	w.SetContent(tabs)
	var items []fyne.Tappable
	for _, o := range test.WidgetRenderer(tabs).Objects() {
		if tap, ok := o.(fyne.Tappable); ok {
			items = append(items, tap)
		}
	}
	test.Tap(items[1])
	if tabs.Selected != "Two" || len(menus) != 0 {
		t.Fatalf("another tab should be picked: %q, %v", tabs.Selected, menus)
	}
	test.Tap(items[1])
	if !slices.Equal(menus, []string{"Two"}) {
		t.Fatalf("the picked tab should drop its menu: %v", menus)
	}
}

// The + is an action, not a project: tapping it opens the form and leaves
// the open project picked. Saving the form adds the tab and opens it.
func TestProjectsPlusTabAddsAProject(t *testing.T) {
	defer stubChildren(t, map[string]string{"": childrenPageOne, "c1": childrenPageTwo})()
	ui := projectsUI(t, projects.Saved{Projects: []projects.Project{
		{ID: "a", Name: "Search", Issues: []string{"exampleorg/shop#99"}},
	}})
	defer fyne.CurrentApp().Quit()
	openProjects(ui)
	page := projectsPage(ui)

	tabs := underlineTabs(page)
	tabs.OnAction()
	if tabs.Selected != "Search" {
		t.Fatalf("the + should not become the picked tab, got %q", tabs.Selected)
	}
	overlay := ui.Win.Canvas().Overlays().Top()
	if overlay == nil {
		t.Fatal("the + should open the new-project form")
	}
	var entries []*widget.Entry
	var save *widget.Button
	walkPopup(overlay, func(o fyne.CanvasObject) {
		switch v := o.(type) {
		case *widget.Entry:
			entries = append(entries, v)
		case *widget.Button:
			if v.Text == "Save" {
				save = v
			}
		}
	})
	if len(entries) != 2 || save == nil {
		t.Fatalf("expected a name field, an issues field and Save, got %d fields", len(entries))
	}

	// A name already taken, then nothing pasted, are both refused.
	entries[0].SetText("search")
	entries[1].SetText("exampleorg/shop#1")
	test.Tap(save)
	entries[0].SetText("Checkout")
	entries[1].SetText("")
	test.Tap(save)
	if saved, _ := projects.Load(ui.Store); len(saved.Projects) != 1 {
		t.Fatalf("a refused form should save nothing: %+v", saved)
	}

	entries[1].SetText("https://github.com/exampleorg/shop/issues/1")
	test.Tap(save)
	saved, err := projects.Load(ui.Store)
	if err != nil || len(saved.Projects) != 2 || saved.Current().Name != "Checkout" ||
		!slices.Equal(saved.Projects[1].Issues, []string{"exampleorg/shop#1"}) {
		t.Fatalf("the new project should be saved and open: %+v, %v", saved, err)
	}
	tabs = underlineTabs(page)
	if !slices.Equal(tabs.Options, []string{"Search", "Checkout", "+"}) || tabs.Selected != "Checkout" {
		t.Fatalf("project tabs after adding: %v, picked %q", tabs.Options, tabs.Selected)
	}
	if got := labels(page); !contains(got, "Workload By Person") {
		t.Fatalf("the new project should be fetched and charted: %v", got)
	}
}

// With nothing saved the page explains itself and offers to add a project.
func TestProjectsTabStartsEmpty(t *testing.T) {
	ui := projectsUI(t, projects.Saved{})
	defer fyne.CurrentApp().Quit()
	openProjects(ui)
	got := labels(projectsPage(ui))
	if !contains(got, "No projects yet") || !contains(got, "Add a project") {
		t.Fatalf("an empty Projects tab should offer to add one: %v", got)
	}
	if _, err := os.Stat(filepath.Join(ui.Store.Dir, "projects.json")); !os.IsNotExist(err) {
		t.Fatalf("opening the tab should not write a file: %v", err)
	}
}

// On Commits, a grouped issue's name is gold, "Other commits" too, and so is
// the rule between one group and the next.
func TestPendingSectionHeadingsAndRulesAreGold(t *testing.T) {
	ui := pendingUI(t)
	defer fyne.CurrentApp().Quit()

	const issue = "exampleorg/sample-intranet#42"
	ui.IssueInfo = map[string]backend.IssueInfo{issue: {Org: "exampleorg", Repo: "sample-intranet", Number: 42, Title: "DOC ITEM ENHANCEMENT"}}
	commit := func(sha string) []backend.Commit {
		return []backend.Commit{{Sha: sha, Repo: "exampleorg/sample-intranet", Message: sha, Full: sha}}
	}
	ui.Pending = backend.PendingResult{Count: 3, Groups: []backend.Group{
		{Date: "2026-08-11", Issue: issue, Commits: commit("aaa1111")},
		{Date: "2026-08-10", Issue: issue, Commits: commit("bbb2222")},
		{Date: "2026-08-09", Commits: commit("ccc3333")},
	}}
	logwork.RenderPending(ui)

	variant := fyne.CurrentApp().Settings().ThemeVariant()
	gold := map[string]bool{}
	rules := 0
	walk(ui.PendingBox, func(c fyne.CanvasObject) {
		themed, ok := c.(*container.ThemeOverride)
		if !ok {
			return
		}
		switch inner := themed.Content.(type) {
		case *widget.Label:
			if themed.Theme.Color(theme.ColorNameForeground, variant) == widgets.LogoGold {
				gold[inner.Text] = true
			}
		case *widget.Separator:
			if themed.Theme.Color(theme.ColorNameSeparator, variant) == widgets.LogoGold {
				rules++
			}
		}
	})
	if !gold["DOC ITEM ENHANCEMENT"] || !gold["Other commits"] {
		t.Fatalf("both headings should be gold, got %v", gold)
	}
	if rules != 1 {
		t.Fatalf("expected one gold rule between the two groups, got %d", rules)
	}
}

// Dragging a project's tab along the row reorders the projects; the + stays
// at the end, and the order is kept.
func TestProjectTabsReorderByDragging(t *testing.T) {
	ui := projectsUI(t, projects.Saved{Selected: "a", Projects: []projects.Project{
		{ID: "a", Name: "One", Issues: []string{"exampleorg/shop#99"}},
		{ID: "b", Name: "Two", Issues: []string{"exampleorg/shop#99"}},
		{ID: "c", Name: "Three", Issues: []string{"exampleorg/shop#99"}},
	}})
	defer fyne.CurrentApp().Quit()
	page := projectsPage(ui)
	tabs := underlineTabs(page)
	tabs.Resize(fyne.NewSize(600, 60))
	test.WidgetRenderer(tabs).Layout(tabs.Size())
	var items []fyne.CanvasObject
	for _, o := range test.WidgetRenderer(tabs).Objects() {
		if _, ok := o.(fyne.Draggable); ok {
			items = append(items, o)
		}
	}
	// Drag One past the middle of Three.
	one, three := items[0], items[2]
	x := three.Position().X + three.Size().Width*0.75 - one.Position().X
	one.(fyne.Draggable).Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(x, 10)}})
	one.(fyne.Draggable).DragEnd()

	saved, err := projects.Load(ui.Store)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range saved.Projects {
		names = append(names, p.Name)
	}
	if !slices.Equal(names, []string{"Two", "Three", "One"}) {
		t.Fatalf("saved order: %v", names)
	}
	if got := underlineTabs(page).Options; !slices.Equal(got, []string{"Two", "Three", "One", "+"}) {
		t.Fatalf("tabs: %v", got)
	}
}

// The Kanban's columns keep the order they were dragged to; a column the
// order does not name keeps its usual place after those it does.
func TestKanbanColumnOrderIsKept(t *testing.T) {
	if got := kanban.OrderColumns([]string{"Done", "Created"}, []string{"Created", "In Progress", "Done"}); !slices.Equal(got, []string{"Done", "Created", "In Progress"}) {
		t.Fatalf("ordered: %v", got)
	}
	// Hidden columns stay after the shown column they followed.
	all := []string{"Created", "Analysis", "In Progress", "Done", "No status"}
	if got := kanban.MovedColumns(all, []string{"Done", "Created", "In Progress"}); !slices.Equal(got, []string{"Done", "No status", "Created", "Analysis", "In Progress"}) {
		t.Fatalf("moved: %v", got)
	}
}

// Milestones split the scope by due date, the issues in none last.
func TestProjectMilestones(t *testing.T) {
	snap, now := sampleProject()
	snap.Patch("https://github.com/exampleorg/shop/issues/4", func(it *projects.Item) {
		it.Milestone, it.MilestoneDue = "Beta", time.Date(2026, 12, 1, 0, 0, 0, 0, time.Local)
	})
	snap.Patch("https://github.com/exampleorg/shop/issues/2", func(it *projects.Item) { it.Milestone = "Alpha" })
	snap.Patch("https://github.com/exampleorg/shop/issues/3", func(it *projects.Item) {
		it.Milestone, it.MilestoneDue = "Alpha", time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local)
	})
	s := projects.Summarize(snap, now)
	var got []string
	for _, m := range s.Milestones {
		got = append(got, fmt.Sprintf("%s=%d/%d", m.Name, m.Done, m.Total()))
	}
	if strings.Join(got, " ") != "Alpha=2/2 Beta=0/1 No milestone=1/3" {
		t.Fatalf("milestones: %v", got)
	}
	if none := projects.Summarize(sampleProjectOnly(), now).Milestones; none != nil {
		t.Fatalf("no milestones should be none: %v", none)
	}
}

func sampleProjectOnly() projects.Snapshot {
	snap, _ := sampleProject()
	return snap
}

// The list's drop-downs change the issue on GitHub: Status on its board, and
// who holds it.
func TestProjectIssueChangesGoToGitHub(t *testing.T) {
	it := projects.Item{Number: 4, URL: "https://github.com/exampleorg/shop/issues/4", Assignees: []string{"ana", "bea"},
		Board: &projects.Board{ProjectID: "PVT_1", ItemID: "PVTI_4", FieldID: "F", Options: []projects.Option{{ID: "o3", Name: "Done"}}}}
	args, err := projects.SetStatusArgs(it, "Done")
	if err != nil || !slices.Contains(args, "value=o3") || !slices.Contains(args, "item=PVTI_4") || !slices.Contains(args, "project=PVT_1") {
		t.Fatalf("status: %v, %v", args, err)
	}
	if _, err := projects.SetStatusArgs(it, "Nope"); err == nil {
		t.Fatal("a status the board lacks should be refused")
	}
	if got := strings.Join(projects.SetAssigneeArgs(it, "bea"), " "); got != "issue edit https://github.com/exampleorg/shop/issues/4 --remove-assignee ana" {
		t.Fatalf("assignee: %s", got)
	}
	if got := strings.Join(projects.SetAssigneeArgs(it, ""), " "); !strings.Contains(got, "--remove-assignee ana --remove-assignee bea") {
		t.Fatalf("unassign: %s", got)
	}
	if projects.SetAssigneeArgs(projects.Item{Assignees: []string{"ana"}}, "ana") != nil {
		t.Fatal("no change should make no call")
	}
}

// The issue list under the charts: a search narrows it, and changing a Status
// there saves it to GitHub and shows it.
func TestProjectIssueListSearchAndChange(t *testing.T) {
	defer stubChildren(t, map[string]string{"": childrenPageOne, "c1": childrenPageTwo})()
	var calls []string
	old := backend.Gh
	backend.Gh = func(args []string, h ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "updateProjectV2ItemFieldValue") {
			calls = append(calls, strings.Join(args, " "))
			return "{}", nil
		}
		return old(args, h...)
	}
	defer func() { backend.Gh = old }()
	ui := projectsUI(t, projects.Saved{Selected: "b", Projects: []projects.Project{
		{ID: "b", Name: "Checkout", Issues: []string{"exampleorg/shop#1"}},
	}})
	defer fyne.CurrentApp().Quit()
	openProjects(ui)
	page := projectsPage(ui)

	// Payment shows once, in the list, folded under the charts to start with.
	payments := func() int {
		n := 0
		for _, l := range labels(page) {
			if l == "#4  Payment" {
				n++
			}
		}
		return n
	}
	got := labels(page)
	if !contains(got, "Issues") || !contains(got, "Milestones") || payments() != 1 || contains(got, "#2  Cart") ||
		contains(got, "Longest Open") {
		t.Fatalf("the open issues should be listed: %v", got)
	}
	var fold *shell.Fold
	walk(page, func(o fyne.CanvasObject) {
		if f, ok := o.(*shell.Fold); ok {
			fold = f
		}
	})
	if fold == nil || fold.Open() {
		t.Fatal("the issue list should start folded")
	}
	var search *widget.Entry
	walk(page, func(o fyne.CanvasObject) {
		if e, ok := o.(*widget.Entry); ok && strings.HasPrefix(e.PlaceHolder, "Search issues") {
			search = e
		}
	})
	search.SetText("nothing like this")
	if got = labels(page); payments() != 0 || !contains(got, "No issues match.") {
		t.Fatalf("a search should narrow the list: %v", got)
	}
	search.SetText("payment")

	var status *shell.GoldSelect
	walk(page, func(o fyne.CanvasObject) {
		if s, ok := o.(*shell.GoldSelect); ok && s.Selected == "In Progress" {
			status = s
		}
	})
	if status == nil {
		t.Fatal("the issue's Status should be a drop-down")
	}
	status.SetSelected("Done")
	if len(calls) != 1 || !strings.Contains(calls[0], "value=o3") {
		t.Fatalf("the change should go to GitHub: %v", calls)
	}
	// The page keeps the change, and the search.
	found := false
	walk(projectsPage(ui), func(o fyne.CanvasObject) {
		if s, ok := o.(*shell.GoldSelect); ok && s.Selected == "Done" {
			found = true
		}
	})
	if !found {
		t.Fatal("the list should show the new Status")
	}
}

// The list sorts by any column, either way round, with the issues that have
// nothing in that column last.
func TestProjectIssueListSorts(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.Local) }
	items := []projects.Item{
		{Number: 1, Created: day(1), Priority: "Low", Sprint: "Sprint 2", SprintStart: day(14), Assignees: []string{"bea"}},
		{Number: 2, Created: day(3), Priority: "P0"},
		{Number: 3, Created: day(2), Sprint: "Sprint 1", SprintStart: day(1), Assignees: []string{"Ana"}},
	}
	order := func(by string, desc bool) string {
		list := slices.Clone(items)
		projects.SortIssues(list, by, desc)
		var out []string
		for _, it := range list {
			out = append(out, fmt.Sprint(it.Number))
		}
		return strings.Join(out, " ")
	}
	for _, c := range []struct {
		by   string
		desc bool
		want string
	}{
		{"Age", false, "2 3 1"}, {"Age", true, "1 3 2"},
		{"Priority", false, "2 1 3"}, {"Priority", true, "1 2 3"},
		{"Sprint", false, "3 1 2"}, {"Sprint", true, "1 3 2"},
		{"Assignee", false, "3 1 2"}, {"Issue", true, "3 2 1"},
		{"", false, "2 3 1"},
	} {
		if got := order(c.by, c.desc); got != c.want {
			t.Errorf("%s desc=%v: %s, want %s", c.by, c.desc, got, c.want)
		}
	}
}

// Priority and Sprint change on the issue's board, or clear with None.
func TestProjectIssueFieldChanges(t *testing.T) {
	it := projects.Item{Number: 4, Board: &projects.Board{ProjectID: "PVT_1", ItemID: "PVTI_4",
		PriorityField: "PF", Priorities: []projects.Option{{ID: "p1", Name: "High"}},
		SprintField: "SF", Sprints: []projects.Option{{ID: "s9", Name: "Sprint 9"}}}}
	args, err := projects.SetFieldArgs(it, "sprint", "Sprint 9")
	joined := strings.Join(args, " ")
	if err != nil || !strings.Contains(joined, "value=s9") || !strings.Contains(joined, "field=SF") || !strings.Contains(joined, "iterationId") {
		t.Fatalf("sprint: %v, %v", args, err)
	}
	args, err = projects.SetFieldArgs(it, "priority", "High")
	joined = strings.Join(args, " ")
	if err != nil || !strings.Contains(joined, "value=p1") || !strings.Contains(joined, "singleSelectOptionId") {
		t.Fatalf("priority: %v, %v", args, err)
	}
	if args, err = projects.SetFieldArgs(it, "priority", ""); err != nil || !strings.Contains(strings.Join(args, " "), "clearProjectV2ItemFieldValue") {
		t.Fatalf("clear: %v, %v", args, err)
	}
	if _, err := projects.SetFieldArgs(it, "priority", "Nope"); err == nil {
		t.Fatal("a priority the board lacks should be refused")
	}
	if _, err := projects.SetFieldArgs(projects.Item{Board: &projects.Board{}}, "sprint", "Sprint 9"); err == nil {
		t.Fatal("a board with no Sprint field should be refused")
	}
}

// collectDriver keeps every animation started, so a test can play them frame
// by frame as the app does, rather than straight to their end.
type collectDriver struct {
	fyne.Driver
	anims []*fyne.Animation
}

func (d *collectDriver) StartAnimation(a *fyne.Animation) { d.anims = append(d.anims, a) }
func (d *collectDriver) StopAnimation(*fyne.Animation)    {}

// The page's arrival is played one frame at a time, as on screen. The column
// charts once let go of their state when the last column rounded to fully
// there a frame early, and the next frame crashed the app.
func TestProjectsPageSurvivesEveryAnimationFrame(t *testing.T) {
	defer stubChildren(t, map[string]string{"": childrenPageOne, "c1": childrenPageTwo})()
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.Local)
	projects.Now = func() time.Time { return now }
	defer func() { projects.Now = time.Now }()
	base := test.NewApp()
	d := &collectDriver{Driver: base.Driver()}
	fyne.SetCurrentApp(&motionTestApp{App: base, driver: d})
	defer func() { fyne.SetCurrentApp(base); base.Quit() }()
	w := fyne.CurrentApp().NewWindow("t")
	ui := &shell.UI{Store: backend.NewStore(tempDir(t)), Win: w, CalMonth: "2026-08", RepMonth: "2026-08"}
	if err := projects.Save(ui.Store, projects.Saved{Selected: "b", Projects: []projects.Project{
		{ID: "b", Name: "Checkout", Issues: []string{"exampleorg/shop#1"}},
	}}); err != nil {
		t.Fatal(err)
	}
	assemble.BuildAllTabs(ui)
	w.SetContent(ui.Tabs)
	w.Resize(fyne.NewSize(1440, 860))
	openProjects(ui)
	if len(d.anims) == 0 {
		t.Fatal("the page should arrive")
	}
	for step := 0; step <= 100; step++ {
		for _, a := range d.anims {
			a.Tick(float32(step) / 100)
		}
	}
}

// The Kanban's column filter reorders by dragging a check along the row, and
// a new order is saved as the board's.
func TestKanbanFilterReordersColumns(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	var got []string
	group := widgets.NewCompactCheckGroup([]string{"Created", "In Progress", "Done"}, nil)
	group.OnReorder = func(order []string) { got = order }
	w := a.NewWindow("t")
	w.SetContent(group)
	w.Resize(fyne.NewSize(600, 60))
	r := test.WidgetRenderer(group)
	r.Layout(group.Size())
	var checks []fyne.CanvasObject
	for _, o := range r.Objects() {
		if _, ok := o.(*canvas.Rectangle); !ok {
			checks = append(checks, o)
		}
	}
	first, last := checks[0], checks[2]
	start := fyne.NewPos(first.Position().X+5, 10)
	end := fyne.NewPos(last.Position().X+last.Size().Width-2, 10)
	group.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: end}, Dragged: fyne.NewDelta(end.X-start.X, 0)})
	group.DragEnd()
	if !slices.Equal(got, []string{"In Progress", "Done", "Created"}) {
		t.Fatalf("filter order: %v", got)
	}

	ui := projectsUI(t, projects.Saved{})
	if !kanban.MoveColumns(ui.Kanban, []string{"Done", "Created"}) {
		t.Fatal("the order should save")
	}
	plan, err := ui.Store.LoadSprintPlan()
	if err != nil || !slices.Equal(plan.KanbanColumnOrder, []string{"Done", "Created"}) {
		t.Fatalf("saved order: %v, %v", plan.KanbanColumnOrder, err)
	}
}

// The Reorder button by the column filter lists every column with up and
// down; a move is saved at once.
func TestKanbanColumnOrderPopup(t *testing.T) {
	ui := projectsUI(t, projects.Saved{})
	defer fyne.CurrentApp().Quit()
	kanban.ShowColumnOrder(ui.Kanban)
	overlay := ui.Win.Canvas().Overlays().Top()
	if overlay == nil {
		t.Fatal("the popup should open")
	}
	var downs []*widget.Button
	walkPopup(overlay, func(o fyne.CanvasObject) {
		if b, ok := o.(*widget.Button); ok && b.Icon != nil && b.Icon.Name() == theme.MoveDownIcon().Name() {
			downs = append(downs, b)
		}
	})
	if len(downs) < 2 {
		t.Fatalf("each column should have a move down, got %d", len(downs))
	}
	test.Tap(downs[0])
	plan, err := ui.Store.LoadSprintPlan()
	if err != nil || len(plan.KanbanColumnOrder) < 2 || plan.KanbanColumnOrder[1] != kanban.PrimaryKanbanStatuses[0] {
		t.Fatalf("the first column should move down one: %v, %v", plan.KanbanColumnOrder, err)
	}
}

// A Status takes the Kanban column's colour; the Kanban keeps its own.
func TestStatusHueMatchesKanban(t *testing.T) {
	for _, name := range kanban.PrimaryKanbanStatuses {
		hue, ok := shell.StatusHue(name)
		if !ok || hue != kanban.KanbanStatusColor(name) {
			t.Fatalf("%s: %v %v", name, hue, ok)
		}
	}
	if _, ok := shell.StatusHue("Someday"); ok {
		t.Fatal("an unknown status has no Kanban colour")
	}
}

// The column filter's checks light up under the pointer and still tick when
// clicked; a drag lifts the check and hands the page a ghost to draw.
func TestColumnFilterHoverAndLift(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	var picked []string
	group := widgets.NewCompactCheckGroup([]string{"Created", "In Progress", "Done"}, func(s []string) { picked = s })
	group.OnReorder = func([]string) {}
	var lifted string
	landed := false
	group.OnLift = func(name string, _ fyne.Position) { lifted = name }
	group.OnLand = func(bool) { landed = true }
	w := a.NewWindow("t")
	w.SetContent(group)
	w.Resize(fyne.NewSize(600, 60))
	r := test.WidgetRenderer(group)
	r.Layout(group.Size())
	var catchers []fyne.CanvasObject
	var pill *canvas.Rectangle
	for _, o := range r.Objects() {
		if _, ok := o.(desktop.Hoverable); ok {
			if _, isCheck := o.(*widget.Check); !isCheck {
				catchers = append(catchers, o)
			}
		}
		if rect, ok := o.(*canvas.Rectangle); ok && pill == nil {
			pill = rect
		}
	}
	if len(catchers) != 3 {
		t.Fatalf("each check should have its pointer catcher: %d", len(catchers))
	}
	catchers[1].(desktop.Hoverable).MouseIn(&desktop.MouseEvent{})
	if _, _, _, alpha := pill.FillColor.RGBA(); alpha == 0 || pill.Position().X > catchers[1].Position().X {
		t.Fatalf("the pill should light the hovered check: %v at %v", pill.FillColor, pill.Position())
	}
	catchers[1].(fyne.Tappable).Tapped(&fyne.PointEvent{})
	if !slices.Contains(picked, "In Progress") {
		t.Fatalf("a click should still tick the check: %v", picked)
	}
	from := catchers[0].Position().Add(fyne.NewPos(5, 10))
	catchers[0].(fyne.Draggable).Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(25, 10), AbsolutePosition: from.Add(fyne.NewPos(20, 0))}, Dragged: fyne.NewDelta(20, 0)})
	catchers[0].(fyne.Draggable).DragEnd()
	if lifted != "Created" || !landed {
		t.Fatalf("the drag should lift and land the check: %q %v", lifted, landed)
	}
}

// A parent issue left out counts for nothing; an open issue its board calls
// Done fills the status ring's Done with the closed ones; each week knows how
// many issues it brought.
func TestProjectExcludedParentsBoardDoneAndWeekOpened(t *testing.T) {
	snap, now := sampleProject()
	project := projects.Project{Issues: []string{"exampleorg/shop#1", "exampleorg/shop#20", "exampleorg/shop#99"}}
	project.SetCounts("exampleorg/shop#20", false)
	if project.Counts("EXAMPLEORG/shop#20") || !project.Counts("exampleorg/shop#1") {
		t.Fatalf("counts: %+v", project.Excluded)
	}
	s := projects.Summarize(project.Counted(snap), now)
	if s.Total != 5 || s.Done != 2 || s.NotPlanned != 0 || len(s.Epics) != 2 {
		t.Fatalf("with #20 left out: total %d done %d not planned %d epics %d", s.Total, s.Done, s.NotPlanned, len(s.Epics))
	}
	project.SetCounts("exampleorg/shop#20", true)
	if project.Excluded != nil || len(project.Counted(snap).Parents) != 3 {
		t.Fatalf("ticked again: %+v", project.Excluded)
	}

	snap.Parents[0].Items[4].Status = "Done"
	s = projects.Summarize(snap, now)
	if s.Statuses[0].Label != "Done" || s.Statuses[0].Count != 4 || s.BoardDone != 1 || s.Done != 3 || s.RingPercent() != 67 {
		t.Fatalf("board Done in the ring: %+v, board done %d, ring %d%%", s.Statuses[0], s.BoardDone, s.RingPercent())
	}

	opened := 0
	for _, w := range s.Weeks {
		opened += w.Opened
	}
	if opened != s.Total || s.Weeks[0].Opened != 2 {
		t.Fatalf("issues opened by week: %+v", s.Weeks)
	}
}

// A tick box ticks and unticks on a click, telling its owner at once while
// the tick goes on drawing itself.
func TestTickCheckTicksAndTells(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	var heard []bool
	tick := widgets.NewTickCheck(false, nil, func(on bool) { heard = append(heard, on) })
	w := a.NewWindow("t")
	w.SetContent(tick)
	w.Resize(fyne.NewSize(60, 60))
	var box fyne.Tappable
	var middle fyne.Position
	for _, o := range test.WidgetRenderer(tick).Objects() {
		if tappable, ok := o.(fyne.Tappable); ok {
			box, middle = tappable, fyne.NewPos(8, o.Size().Height/2)
		}
	}
	if box == nil {
		t.Fatal("the tick box should hold a check to click")
	}
	box.Tapped(&fyne.PointEvent{Position: middle})
	box.Tapped(&fyne.PointEvent{Position: middle})
	if !slices.Equal(heard, []bool{true, false}) || tick.Checked() {
		t.Fatalf("ticked then unticked: heard %v, checked %v", heard, tick.Checked())
	}
	tick.ShowTick(0.5)
	tick.ShowTick(1)
}

// The issue list stands its issues under their milestones, by due date,
// with those in none last.
func TestIssueListGroupsByMilestone(t *testing.T) {
	snap, now := sampleProject()
	early, late := now.AddDate(0, 0, 10), now.AddDate(0, 1, 0)
	snap.Parents[0].Items[0].Milestone, snap.Parents[0].Items[0].MilestoneDue = "Launch", late
	snap.Parents[0].Items[1].Milestone, snap.Parents[0].Items[1].MilestoneDue = "Beta", early
	if got := projects.MilestoneGroups(snap.Items()); !slices.Equal(got, []string{"Beta", "Launch", "No milestone"}) {
		t.Fatalf("milestone groups: %v", got)
	}
	plain := []projects.Item{{Number: 1}, {Number: 2}}
	if got := projects.MilestoneGroups(plain); !slices.Equal(got, []string{""}) {
		t.Fatalf("no milestones, one group with no heading: %v", got)
	}
}
