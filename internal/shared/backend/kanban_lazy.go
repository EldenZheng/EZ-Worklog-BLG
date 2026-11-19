package backend

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// A board is fetched a sprint at a time. Fetching every item at once meant
// paging through the whole Done history of the project as well (thousands of
// items, a request per hundred, one after another), and the board stood empty
// until the last page came in. Now a load reads the board's sprints and how
// many items each holds, then fetches only the sprints in view; the rest are
// fetched when they are opened.

// sprintParallel is how many sprints are fetched at once.
const sprintParallel = 6

// SequentialFetch runs a board's requests one after another on the calling
// goroutine, for tests that run loads in step (shell.RunAsyncInline).
var SequentialFetch bool

// eachAtOnce runs work(i) for i below n, up to limit at a time, and returns
// when all are done; in order on this goroutine under SequentialFetch.
func eachAtOnce(n, limit int, work func(i int)) {
	if SequentialFetch {
		for i := 0; i < n; i++ {
			work(i)
		}
		return
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, max(limit, 1))
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			work(i)
		}(i)
	}
	wg.Wait()
}

// countAliases is how many counts go in one request. GitHub refused a request
// asking for 150.
const countAliases = 45

// SprintLoaded says the items of sprint id ("" for no sprint) are on the board.
func (b SprintBoard) SprintLoaded(id string) bool {
	return b.Loaded == nil || b.Loaded[id]
}

// Complete says every item of the board has been fetched.
func (b SprintBoard) Complete() bool {
	if b.Loaded == nil {
		return true
	}
	for _, s := range b.Iterations() {
		if !b.Loaded[s.ID] {
			return false
		}
	}
	return b.Loaded[""]
}

// SprintCount is how many items sprint id holds: those on the board once it
// is fetched. Before, GitHub's count, plus any card moved into it since.
func (b SprintBoard) SprintCount(id string, counted func(SprintItem) bool) int {
	n := 0
	if !b.SprintLoaded(id) {
		n = b.Counts[id]
	}
	for _, it := range b.Items {
		if it.SprintID == id && (counted == nil || counted(it)) {
			n++
		}
	}
	return n
}

// NeighbourSprints are the sprint that ended last and the next one to start.
func (b SprintBoard) NeighbourSprints(date string) (previous, next SprintIteration) {
	for _, s := range b.Iterations() {
		end := AddDays(s.StartDate, s.Duration)
		switch {
		case end <= date:
			if previous.ID == "" || end > AddDays(previous.StartDate, previous.Duration) {
				previous = s
			}
		case s.StartDate > date:
			if next.ID == "" || s.StartDate < next.StartDate {
				next = s
			}
		}
	}
	return previous, next
}

// FetchSprintBoardLazy reads a board's details and each sprint's count, and
// fetches the items of the sprints want names (by SprintKey, "" for no
// sprint). The others are left for RefreshSprints.
func FetchSprintBoardLazy(rawURL, filter string, want func(SprintBoard) []string, step func(done, more int)) (SprintBoard, error) {
	if step == nil {
		step = func(int, int) {}
	}
	b, err := fetchSprintMeta(rawURL, filter, step)
	if err != nil {
		return b, err
	}
	step(0, -1) // the first page of items the caller counted on is now per sprint
	b.Loaded = map[string]bool{}
	var counts map[string]int
	var countErr error
	board, keys := b, want(b)
	eachAtOnce(2, 2, func(i int) {
		if i == 0 {
			counts, countErr = fetchSprintCounts(board, step)
		} else {
			b, err = RefreshSprints(board, keys, step)
		}
	})
	if countErr == nil {
		b.Counts = counts
	}
	return b, err
}

// RefreshSprints fetches the sprints keys names (by SprintKey, "" for no
// sprint), a few at a time, and puts their items on the board. A sprint GitHub
// counted as empty is marked fetched without asking again.
func RefreshSprints(b SprintBoard, keys []string, step func(done, more int)) (SprintBoard, error) {
	if step == nil {
		step = func(int, int) {}
	}
	type job struct {
		key    string
		sprint SprintIteration
		items  []SprintItem
		err    error
	}
	var jobs []*job
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		sprint := b.SprintByKey(key)
		if key != "" && sprint.ID == "" {
			continue
		}
		jobs = append(jobs, &job{key: key, sprint: sprint})
	}
	var fetch []*job
	for _, j := range jobs {
		if n, ok := b.Counts[j.sprint.ID]; !ok || n > 0 {
			fetch = append(fetch, j)
			step(0, 1)
		}
	}
	eachAtOnce(len(fetch), sprintParallel, func(i int) {
		j := fetch[i]
		j.items, j.err = fetchSprintScope(b, j.key, j.sprint, step)
	})
	var failed []string
	for _, j := range jobs {
		if j.err != nil {
			name := j.sprint.Title
			if j.key == "" {
				name = "No Sprint"
			}
			failed = append(failed, name+": "+j.err.Error())
			continue
		}
		b = mergeSprint(b, j.sprint.ID, j.items)
	}
	if len(failed) > 0 {
		return b, fmt.Errorf("%s", strings.Join(failed, "; "))
	}
	return b, nil
}

// fetchSprintCounts asks GitHub how many items each sprint holds, as a sprint
// fetch would find them: what the filter finds, less its Done items while the
// sprint has not started (they are hidden then), plus the sprint's Done items
// once it has.
func fetchSprintCounts(b SprintBoard, step func(done, more int)) (map[string]int, error) {
	return countSprints(b, "", func(string) bool { return true }, step)
}

// SearchSprintCounts counts, in each sprint of b not fetched yet, the items
// whose title holds the words of text.
func SearchSprintCounts(b SprintBoard, text string) (map[string]int, error) {
	words := searchWords(text)
	if words == "" {
		return map[string]int{}, nil
	}
	return countSprints(b, words, func(id string) bool { return !b.SprintLoaded(id) }, nil)
}

// searchWords is typed text as plain words for a project filter: anything
// that would read as a qualifier (status:, -label, a quote) is taken out, so a
// search finds titles and never changes the filter.
func searchWords(text string) string {
	clean := strings.NewReplacer("\"", " ", ":", " ").Replace(text)
	var words []string
	for _, w := range strings.Fields(clean) {
		if w = strings.TrimLeft(w, "-"); w != "" {
			words = append(words, w)
		}
	}
	return strings.Join(words, " ")
}

// countSprints counts the sprints of b that want names, as a sprint fetch
// would find them, with extra words added to every search.
func countSprints(b SprintBoard, extra string, want func(id string) bool, step func(done, more int)) (map[string]int, error) {
	if step == nil {
		step = func(int, int) {}
	}
	if extra != "" {
		extra = " " + extra
	}
	type term struct {
		id, query string
		sign      int
	}
	today := WorkDate(time.Now())
	var terms []term
	add := func(key string, sprint SprintIteration) {
		if !want(sprint.ID) {
			return
		}
		filter, scope := sprintScope(b, key, sprint)
		terms = append(terms, term{sprint.ID, filter + extra, 1}, term{sprint.ID, filter + extra + " status:Done", -1})
		if key != "" && !b.HiddenDone(SprintItem{Status: "Done", SprintID: sprint.ID}, today) {
			terms = append(terms, term{sprint.ID, DoneFilter(b.Filter) + " " + scope + extra, 1})
		}
	}
	add("", SprintIteration{})
	for _, s := range b.Iterations() {
		add(SprintKey(s), s)
	}
	counts := map[string]int{}
	var mu sync.Mutex
	var firstErr error
	var batches [][]term
	for start := 0; start < len(terms); start += countAliases {
		batches = append(batches, terms[start:min(start+countAliases, len(terms))])
		step(0, 1)
	}
	eachAtOnce(len(batches), len(batches), func(n int) {
		batch := batches[n]
		var vars, fields []string
		args := []string{"api", "graphql"}
		for i, t := range batch {
			vars = append(vars, fmt.Sprintf("$q%d:String!", i))
			fields = append(fields, fmt.Sprintf("c%d:items(first:0,query:$q%d){totalCount}", i, i))
			args = append(args, "-f", fmt.Sprintf("q%d=%s", i, t.query))
		}
		query := fmt.Sprintf("query($id:ID!,%s){node(id:$id){... on ProjectV2{%s}}}", strings.Join(vars, ","), strings.Join(fields, " "))
		args = append(args, "-f", "query="+query, "-f", "id="+b.ID)
		out, err := Gh(args)
		step(1, 0)
		var page struct {
			Data struct {
				Node map[string]struct{ TotalCount int }
			}
		}
		if err == nil {
			err = json.Unmarshal([]byte(out), &page)
		}
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return
		}
		for i, t := range batch {
			counts[t.id] += t.sign * page.Data.Node[fmt.Sprintf("c%d", i)].TotalCount
		}
	})
	if firstErr != nil {
		return nil, firstErr
	}
	for id, n := range counts {
		counts[id] = max(n, 0)
	}
	return counts, nil
}
