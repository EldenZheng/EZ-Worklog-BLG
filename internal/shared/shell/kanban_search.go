package shell

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"

	"worklog/internal/shared/backend"
)

// MatchesSearch says an item answers Kanban's search: its title, link or board
// name holds the text typed, whatever the case.
func (k *KanbanUI) MatchesSearch(b backend.SprintBoard, it backend.SprintItem) bool {
	return k.Search == "" || strings.Contains(strings.ToLower(it.Title+" "+it.URL+" "+b.Title), k.Search)
}

// SelectionReady says the sprints Kanban shows are on the boards, or were
// asked for and could not be had. Until then the board draws no cards, so
// they arrive once fetched instead of turning up after the arrival played.
func (k *KanbanUI) SelectionReady() bool {
	if len(k.Plan.Boards) == 0 {
		return !k.Loading
	}
	keys := k.kanbanKeys()
	for _, b := range k.Plan.Boards {
		if !k.VisibleBoard(b) {
			continue
		}
		for _, key := range keys(b) {
			id := ""
			if key != "" {
				if id = b.SprintByKey(key).ID; id == "" {
					continue
				}
			}
			if !b.SprintLoaded(id) && (k.Loading || !k.requested[key]) {
				return false
			}
		}
	}
	return true
}

// SprintMatches is how many of a sprint's items (by sprint ID, "" for none)
// answer the search on board b, counting only those counted says to. A sprint
// not fetched yet takes GitHub's count for the search, and -1 while that is
// still to come.
func (k *KanbanUI) SprintMatches(b backend.SprintBoard, id string, counted func(backend.SprintItem) bool) int {
	n := 0
	if !b.SprintLoaded(id) {
		if b.SearchFor != k.Search {
			return -1
		}
		n = b.SearchCounts[id]
	}
	for _, it := range b.Items {
		if it.SprintID == id && k.MatchesSearch(b, it) && (counted == nil || counted(it)) {
			n++
		}
	}
	return n
}

// SearchCountPause is how long a search stands unchanged before GitHub is
// asked to count its matches: long enough that typing a phrase sends one
// request, not one per word. Tests set it to 0 to ask at once.
var SearchCountPause = time.Second

// ScheduleSearchCounts asks GitHub to count the search's matches (see
// EnsureSearchCounts) once the search has stood for SearchCountPause.
func (k *KanbanUI) ScheduleSearchCounts() {
	if k.searchTimer != nil {
		k.searchTimer.Stop()
		k.searchTimer = nil
	}
	if k.Search == "" {
		return
	}
	if SearchCountPause <= 0 {
		k.EnsureSearchCounts()
		return
	}
	query := k.Search
	k.searchTimer = time.AfterFunc(SearchCountPause, func() {
		fyne.Do(func() {
			if k.Search == query {
				k.EnsureSearchCounts()
			}
		})
	})
}

// EnsureSearchCounts asks GitHub, once per search and load, how many items in
// each sprint not fetched yet answer the search, so the sprint list can count
// matches before those sprints are opened.
func (k *KanbanUI) EnsureSearchCounts() {
	query := k.Search
	if query == "" || len(k.Plan.Boards) == 0 {
		return
	}
	asked := fmt.Sprint(query, "|", k.Plan.Fetched)
	if k.searchAsked == asked {
		return
	}
	var boards []backend.SprintBoard
	for _, b := range k.Plan.Boards {
		if !b.Complete() && b.SearchFor != query {
			boards = append(boards, b)
		}
	}
	if len(boards) == 0 {
		return
	}
	k.searchAsked = asked
	results := make([]map[string]int, len(boards))
	k.Ui.Async(func() error {
		for i, b := range boards {
			if counts, err := backend.SearchSprintCounts(b, query); err == nil {
				results[i] = counts
			}
		}
		return nil
	}, func() {
		if k.Search != query {
			return // the search moved on while GitHub counted
		}
		changed := false
		for i, b := range boards {
			if results[i] == nil {
				continue
			}
			for j := range k.Plan.Boards {
				if backend.SameSprintProject(k.Plan.Boards[j].URL, b.URL) {
					k.Plan.Boards[j].SearchCounts, k.Plan.Boards[j].SearchFor = results[i], query
					changed = true
				}
			}
		}
		if changed {
			Hooks.KanbanUIDraw(k)
		}
	})
}
