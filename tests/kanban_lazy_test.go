package tests

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"fyne.io/fyne/v2"

	"worklog/internal/menus/kanban"
	"worklog/internal/shared/backend"
	"worklog/internal/shared/shell"
)

// lazyOnItems, if set, runs as lazyGitHub is asked for items.
var lazyOnItems func()

// lazyGitHub stands in for GitHub on doneHistoryBoard: one open card in each
// sprint, and counts that say the older sprint holds 7. It records which
// sprints had their items fetched, and every items filter asked for.
func lazyGitHub(t *testing.T) (fetched func() []string, filters func() []string) {
	t.Helper()
	b := doneHistoryBoard()
	var mu sync.Mutex
	var sprints, asked []string
	old := backend.Gh
	t.Cleanup(func() { backend.Gh = old })
	scope := regexp.MustCompile(`sprint:"([^"]+)"`)
	backend.Gh = func(args []string, _ ...string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "projectV2(number") {
			raw, err := json.Marshal(map[string]any{"data": map[string]any{"owner": map[string]any{"projectV2": map[string]any{
				"id": b.ID, "title": "Lazy", "fields": map[string]any{"nodes": b.Fields},
			}}}})
			return string(raw), err
		}
		if strings.Contains(joined, "totalCount") {
			node := map[string]any{}
			for _, arg := range args {
				name, q, ok := strings.Cut(arg, "=")
				if !ok || !strings.HasPrefix(name, "q") {
					continue
				}
				n := 0
				if strings.Contains(q, `sprint:"Older sprint"`) {
					// 7 in all; searching "task", 3 of them.
					found, ownDone, done := 5, 1, 3
					if strings.Contains(q, " task") {
						found, ownDone, done = 2, 0, 1
					}
					switch {
					case strings.HasSuffix(q, " status:Done"): // the filter's own Done cards
						n = ownDone
					case strings.Contains(q, "is:open"): // what the filter finds
						n = found
					default: // the sprint's Done cards
						n = done
					}
				}
				node["c"+strings.TrimPrefix(name, "q")] = map[string]int{"totalCount": n}
			}
			raw, err := json.Marshal(map[string]any{"data": map[string]any{"node": node}})
			return string(raw), err
		}
		filter := ""
		for _, arg := range args {
			if strings.HasPrefix(arg, "filter=") {
				filter = strings.TrimPrefix(arg, "filter=")
			}
		}
		mu.Lock()
		asked = append(asked, filter)
		mu.Unlock()
		if lazyOnItems != nil {
			lazyOnItems()
		}
		var items []backend.SprintItem
		if m := scope.FindStringSubmatch(filter); m != nil && strings.Contains(filter, "is:open") {
			for _, s := range b.Iterations() {
				if s.Title == m[1] {
					mu.Lock()
					sprints = append(sprints, s.Title)
					mu.Unlock()
					items = append(items, backend.SprintItem{ID: "open-" + s.ID, Title: s.Title + " task", Status: "Created", SprintID: s.ID})
				}
			}
		}
		return doneHistoryPage(t, items, false), nil
	}
	sorted := func(list *[]string) func() []string {
		return func() []string {
			mu.Lock()
			defer mu.Unlock()
			out := append([]string{}, (*list)...)
			sort.Strings(out)
			return out
		}
	}
	return sorted(&sprints), sorted(&asked)
}

// A load fetches the sprints in view and counts the rest; it never pages
// through the whole Done history.
func TestKanbanLoadFetchesOnlyTheSprintsInView(t *testing.T) {
	fetched, filters := lazyGitHub(t)
	b := doneHistoryBoard()
	got, err := backend.FetchSprintBoardLazy(b.URL, b.Filter, func(backend.SprintBoard) []string {
		return []string{backend.SprintKey(b.Iterations()[2])} // the current sprint
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f := fetched(); fmt.Sprint(f) != "[Current sprint]" {
		t.Fatalf("only the current sprint should be fetched, got %v", f)
	}
	for _, f := range filters() {
		if !strings.Contains(f, "sprint:") {
			t.Fatalf("every fetch should be scoped to a sprint, got %q", f)
		}
	}
	if !got.SprintLoaded("current") || got.SprintLoaded("older") || got.Complete() {
		t.Fatalf("the board should know which sprints it holds: %v", got.Loaded)
	}
	// 5 found, less the 1 Done the filter found, plus 3 Done in the sprint.
	if n := got.SprintCount("older", nil); n != 7 {
		t.Fatalf("the older sprint should count 7 before it is fetched, got %d", n)
	}
}

// Kanban loads the sprints either side of the current one with it, and
// fetches any other sprint once it is opened.
func TestOpeningASprintFetchesIt(t *testing.T) {
	fetched, _ := lazyGitHub(t)
	old := shell.RunAsyncInline
	shell.RunAsyncInline = true
	t.Cleanup(func() { shell.RunAsyncInline = old })
	k := longBoardUI(t, 0)
	b := doneHistoryBoard()
	k.Ui.Cfg.KanbanBoards = []backend.KanbanSource{{URL: b.URL, Filter: b.Filter}}
	k.Plan.Boards = nil
	k.Load(true)
	if f := fetched(); fmt.Sprint(f) != "[Current sprint Future sprint Previous sprint]" {
		t.Fatalf("a load should fetch the previous, current and next sprints, got %v", f)
	}
	older := ""
	for _, g := range k.CombinedSprints(true) {
		if g.Sprint.ID == "older" {
			older, _ = g.Key, g.Count
			if g.Count != 7 {
				t.Fatalf("the older sprint should show GitHub's count, got %d", g.Count)
			}
		}
	}
	if older == "" {
		t.Fatal("the older sprint should be listed before it is fetched")
	}
	kanban.ToggleSprint(k, older)
	kanban.Draw(k)
	if f := fetched(); fmt.Sprint(f) != "[Current sprint Future sprint Older sprint Previous sprint]" {
		t.Fatalf("opening the older sprint should fetch it, got %v", f)
	}
	if cards, _ := boardCards(k); cards != 1 {
		t.Fatalf("the older sprint's card should be on the board, got %d", cards)
	}
}

// A board holding only some sprints cannot say an issue has left it, so the
// plans for issues in sprints not fetched yet are kept.
func TestPlansSurviveSprintsNotFetchedYet(t *testing.T) {
	b := doneHistoryBoard()
	b.Loaded = map[string]bool{"current": true}
	p := backend.SprintPlan{Boards: []backend.SprintBoard{b}, Tasks: map[string]backend.SprintTask{
		"https://github.com/exampleorg/repo/issues/9": {Estimate: 30},
	}}
	if got, _ := backend.PruneSprintPlan(p, shell.Today()); len(got.Tasks) != 1 {
		t.Fatal("a plan for an issue in a sprint not fetched yet should be kept")
	}
	b.Loaded = nil
	p.Boards = []backend.SprintBoard{b}
	if got, _ := backend.PruneSprintPlan(p, shell.Today()); len(got.Tasks) != 0 {
		t.Fatal("once every sprint is fetched, a plan for an issue gone from the board goes")
	}
}

// A sprint opened before it is fetched shows no cards while GitHub is asked;
// they arrive, one after another, once it lands.
func TestCardsWaitForTheirSprint(t *testing.T) {
	lazyGitHub(t)
	old := shell.RunAsyncInline
	shell.RunAsyncInline = true
	t.Cleanup(func() { shell.RunAsyncInline = old; lazyOnItems = nil })
	k := longBoardUI(t, 0)
	b := doneHistoryBoard()
	k.Ui.Cfg.KanbanBoards = []backend.KanbanSource{{URL: b.URL, Filter: b.Filter}}
	k.Plan.Boards = nil
	k.Load(true)
	older := backend.SprintKey(b.Iterations()[0])
	during := -1
	lazyOnItems = func() { during, _ = boardCards(k) }
	kanban.ToggleSprint(k, older)
	kanban.Draw(k)
	lazyOnItems = nil
	if during != 0 {
		t.Fatalf("no cards should show while the sprint is fetched, got %d", during)
	}
	arriving := 0
	everything(k.Body, func(o fyne.CanvasObject) {
		if _, ok := o.(*shell.Wipe); ok {
			arriving++
		}
	})
	if cards, _ := boardCards(k); cards != 1 || arriving != 1 {
		t.Fatalf("the fetched card should arrive: %d cards, %d arriving", cards, arriving)
	}
}

// Searching counts matches in every sprint: those fetched on the board, the
// rest by asking GitHub, with "…" until it answers.
func TestSearchCountsMatchesInEverySprint(t *testing.T) {
	lazyGitHub(t)
	old := shell.RunAsyncInline
	shell.RunAsyncInline = true
	t.Cleanup(func() { shell.RunAsyncInline = old })
	k := longBoardUI(t, 0)
	b := doneHistoryBoard()
	k.Ui.Cfg.KanbanBoards = []backend.KanbanSource{{URL: b.URL, Filter: b.Filter}}
	k.Plan.Boards = nil
	k.Load(true)
	oldPause := shell.SearchCountPause
	shell.SearchCountPause = 0
	t.Cleanup(func() { shell.SearchCountPause = oldPause })
	k.Search = "task"
	kanban.Draw(k)
	matches := map[string]int{}
	for _, g := range k.CombinedSprints(true) {
		matches[g.Sprint.ID] = g.Matches
	}
	// Each fetched sprint has one card, "<sprint> task"; the older one, not
	// fetched, has 3 matches by GitHub's count.
	if matches["current"] != 1 || matches["previous"] != 1 || matches["older"] != 3 {
		t.Fatalf("search counts: %v", matches)
	}
	k.Search = "nothing like it"
	if len(k.CombinedSprints(true)) == 0 {
		t.Fatal("the sprints stay listed while searching")
	}
	for _, g := range k.CombinedSprints(true) {
		if g.Sprint.ID == "older" && g.Matches != -1 {
			t.Fatalf("a new search should wait for GitHub's count, got %d", g.Matches)
		}
		if g.Sprint.ID == "current" && g.Matches != 0 {
			t.Fatalf("a fetched sprint counts its own matches, got %d", g.Matches)
		}
	}
}
