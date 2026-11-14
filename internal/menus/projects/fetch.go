package projects

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"worklog/internal/shared/backend"
)

// Item is one child issue of a project's parent issue.
type Item struct {
	Number int
	Title  string
	URL    string
	Repo   string // owner/repo
	Closed bool
	// NotPlanned is closed without being done (not planned, or a duplicate):
	// it leaves the project's scope rather than counting as finished.
	NotPlanned bool
	// Status is the issue's Status on its project board, "" when it is on none.
	Status string
	// Sprint is the board's Sprint iteration the issue is in, "" for none;
	// SprintStart and SprintDays place it in time.
	Sprint      string
	SprintStart time.Time
	SprintDays  int
	// Priority is the board's Priority, or failing that a priority label
	// (P1, priority: high), "" for none.
	Priority string
	// Milestone is the issue's milestone, "" for none, and MilestoneDue its
	// due date when it has one.
	Milestone    string
	MilestoneDue time.Time
	// Board is where the issue's Status can be changed: the first board it is
	// on that has a Status field. Nil when it is on none.
	Board        *Board
	Created      time.Time
	ClosedAt     time.Time
	Assignees    []string
	Parent       string // the pasted issue it belongs to, owner/repo#number
	Blockers     []Dependency
	BlockerCount int
}

// Dependency is an actual GitHub blocked-by relationship, not an inferred order.
type Dependency struct {
	Number     int
	Title, URL string
	Closed     bool
}

// Board is an issue's place on a project board, with the board's Status
// choices, and its Priority and Sprint choices when it has those fields.
type Board struct {
	ProjectID, ItemID, FieldID string
	Options                    []Option
	// PriorityField is the board's Priority field, "" when it has none.
	PriorityField string
	Priorities    []Option
	// SprintField is the board's Sprint field, "" when it has none, and
	// Sprints its sprints running now and to come.
	SprintField string
	Sprints     []Option
}

// Option is one choice of a board's Status, Priority or Sprint. A sprint
// also has its first day and length.
type Option struct {
	ID, Name string
	Start    time.Time
	Days     int
}

// Parent is one of the project's pasted issues with its children, or why it
// could not be read.
type Parent struct {
	Ref    string
	Title  string
	URL    string
	Closed bool
	Items  []Item
	Err    string
}

// Snapshot is a project as GitHub held it when fetched.
type Snapshot struct {
	Parents []Parent
	Fetched time.Time
	// Assignable is who each repo (owner/repo) lets issues be given to.
	Assignable map[string][]string
}

// Items is every child issue across the parents.
func (s Snapshot) Items() []Item {
	var all []Item
	for _, p := range s.Parents {
		all = append(all, p.Items...)
	}
	return all
}

// One page asks for 50 children. Each brings its assignees and board status
// with it, so the page is kept well under the 100 GitHub allows to stay inside
// the query's node budget.
const childrenQuery = `
query($owner:String!,$repo:String!,$num:Int!,$after:String){
 repository(owner:$owner,name:$repo){
  issue(number:$num){ title url state
   subIssues(first:50,after:$after){
    pageInfo{ hasNextPage endCursor }
    nodes{ number title url state stateReason createdAt closedAt
     repository{ nameWithOwner }
     assignees(first:10){ nodes{ login } }
     labels(first:20){ nodes{ name } }
     milestone{ title dueOn }
     blockedBy(first:100){ totalCount nodes{ number title url closed } }
     projectItems(first:5){ nodes{ id
      project{ id field(name:"Status"){ ... on ProjectV2SingleSelectField{ id options{ id name } } }
       priorityField: field(name:"Priority"){ ... on ProjectV2SingleSelectField{ id options{ id name } } }
       sprintField: field(name:"Sprint"){ ... on ProjectV2IterationField{ id configuration{ iterations{ id title startDate duration } } } } }
      status: fieldValueByName(name:"Status"){ ... on ProjectV2ItemFieldSingleSelectValue{ name } }
      priority: fieldValueByName(name:"Priority"){ ... on ProjectV2ItemFieldSingleSelectValue{ name } }
      sprint: fieldValueByName(name:"Sprint"){ ... on ProjectV2ItemFieldIterationValue{ title startDate duration } } } } } } } } }
`

type childrenResponse struct {
	Data struct {
		Repository struct {
			Issue *struct {
				Title, URL, State string
				SubIssues         struct {
					PageInfo struct {
						HasNextPage bool
						EndCursor   string
					}
					Nodes []struct {
						Number              int
						Title, URL          string
						State, StateReason  string
						CreatedAt, ClosedAt string
						Repository          struct{ NameWithOwner string }
						Assignees           struct{ Nodes []struct{ Login string } }
						Labels              struct{ Nodes []struct{ Name string } }
						Milestone           *struct{ Title, DueOn string }
						BlockedBy           struct {
							TotalCount int
							Nodes      []Dependency
						}
						ProjectItems struct {
							Nodes []struct {
								ID      string
								Project struct {
									ID    string
									Field *struct {
										ID      string
										Options []struct{ ID, Name string }
									}
									PriorityField *struct {
										ID      string
										Options []struct{ ID, Name string }
									}
									SprintField *struct {
										ID            string
										Configuration struct {
											Iterations []struct {
												ID, Title, StartDate string
												Duration             int
											}
										}
									}
								}
								Status, Priority *struct{ Name string }
								Sprint           *struct {
									Title, StartDate string
									Duration         int
								}
							}
						}
					}
				}
			}
		}
	}
	Errors []struct{ Message string }
}

// fetchParent reads one pasted issue and all its children.
func fetchParent(ref string) Parent {
	parent := Parent{Ref: ref}
	owner, repo, num, err := backend.SplitIssue(ref)
	if err != nil {
		parent.Err = err.Error()
		return parent
	}
	after := ""
	for {
		args := []string{"api", "graphql", "-f", "query=" + childrenQuery,
			"-F", "owner=" + owner, "-F", "repo=" + repo, "-F", fmt.Sprintf("num=%d", num)}
		if after != "" {
			args = append(args, "-f", "after="+after)
		}
		out, err := backend.Gh(args, "GraphQL-Features: sub_issues")
		if err != nil {
			parent.Err = err.Error()
			return parent
		}
		var page childrenResponse
		if err := json.Unmarshal([]byte(out), &page); err != nil {
			parent.Err = "GitHub's answer could not be read: " + err.Error()
			return parent
		}
		issue := page.Data.Repository.Issue
		if issue == nil {
			parent.Err = "Issue not found."
			if len(page.Errors) > 0 {
				parent.Err = page.Errors[0].Message
			}
			return parent
		}
		parent.Title, parent.URL = issue.Title, issue.URL
		parent.Closed = issue.State != "" && issue.State != "OPEN"
		for _, n := range issue.SubIssues.Nodes {
			item := Item{
				Number: n.Number, Title: n.Title, URL: n.URL, Repo: n.Repository.NameWithOwner,
				Closed:  n.State != "" && n.State != "OPEN",
				Created: parseTime(n.CreatedAt), ClosedAt: parseTime(n.ClosedAt),
				Parent: ref,
			}
			item.NotPlanned = item.Closed && (n.StateReason == "NOT_PLANNED" || n.StateReason == "DUPLICATE")
			item.Blockers, item.BlockerCount = n.BlockedBy.Nodes, n.BlockedBy.TotalCount
			for _, a := range n.Assignees.Nodes {
				if a.Login != "" {
					item.Assignees = append(item.Assignees, a.Login)
				}
			}
			// An issue on several boards takes each field from the first board
			// that sets it.
			if m := n.Milestone; m != nil {
				item.Milestone, item.MilestoneDue = strings.TrimSpace(m.Title), parseTime(m.DueOn)
			}
			for _, board := range n.ProjectItems.Nodes {
				if f := board.Project.Field; f != nil && f.ID != "" && item.Board == nil {
					b := &Board{ProjectID: board.Project.ID, ItemID: board.ID, FieldID: f.ID}
					for _, o := range f.Options {
						b.Options = append(b.Options, Option{ID: o.ID, Name: o.Name})
					}
					if pf := board.Project.PriorityField; pf != nil && pf.ID != "" {
						b.PriorityField = pf.ID
						for _, o := range pf.Options {
							b.Priorities = append(b.Priorities, Option{ID: o.ID, Name: o.Name})
						}
					}
					if sf := board.Project.SprintField; sf != nil && sf.ID != "" {
						b.SprintField = sf.ID
						for _, it := range sf.Configuration.Iterations {
							sprint := Option{ID: it.ID, Name: strings.TrimSpace(it.Title), Days: it.Duration}
							sprint.Start, _ = time.ParseInLocation("2006-01-02", it.StartDate, time.Local)
							b.Sprints = append(b.Sprints, sprint)
						}
					}
					item.Board = b
				}
				if v := board.Status; v != nil && item.Status == "" {
					item.Status = strings.TrimSpace(v.Name)
				}
				if v := board.Priority; v != nil && item.Priority == "" {
					item.Priority = strings.TrimSpace(v.Name)
				}
				if v := board.Sprint; v != nil && item.Sprint == "" && strings.TrimSpace(v.Title) != "" {
					item.Sprint, item.SprintDays = strings.TrimSpace(v.Title), v.Duration
					if start, err := time.ParseInLocation("2006-01-02", v.StartDate, time.Local); err == nil {
						item.SprintStart = start
					}
				}
			}
			if item.Priority == "" {
				for _, label := range n.Labels.Nodes {
					if priority, ok := PriorityLabel(label.Name); ok {
						item.Priority = priority
						break
					}
				}
			}
			parent.Items = append(parent.Items, item)
		}
		info := issue.SubIssues.PageInfo
		if !info.HasNextPage {
			return parent
		}
		if info.EndCursor == "" || info.EndCursor == after {
			parent.Err = "GitHub's list of child issues did not advance."
			return parent
		}
		after = info.EndCursor
	}
}

var priorityLabel = regexp.MustCompile(`(?i)^\s*(?:(p[0-4])|priority\s*[:/\-]?\s*(.+))\s*$`)

// PriorityLabel reads a label as a priority: "P1" is P1, and "priority: high"
// or "Priority/High" is High. Any other label is not one.
func PriorityLabel(label string) (string, bool) {
	m := priorityLabel.FindStringSubmatch(label)
	switch {
	case m == nil:
		return "", false
	case m[1] != "":
		return strings.ToUpper(m[1]), true
	}
	name := strings.TrimSpace(m[2])
	if name == "" {
		return "", false
	}
	return strings.ToUpper(name[:1]) + name[1:], true
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.Local()
}

// fetchAtOnce is how many parents are read at the same time.
const fetchAtOnce = 4

// Fetch reads every parent issue of a project. One that fails carries its own
// error and the rest are still returned. progress runs, on the calling
// goroutine, as each one lands.
func Fetch(refs []string, progress func(backend.FetchProgress)) Snapshot {
	report := func(done int) {
		if progress != nil {
			progress(backend.FetchProgress{Done: done, Total: len(refs), Stage: "Reading child issues"})
		}
	}
	report(0)
	type landed struct {
		index  int
		parent Parent
	}
	results := make(chan landed, len(refs))
	slots := make(chan struct{}, fetchAtOnce)
	for i, ref := range refs {
		go func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			results <- landed{i, fetchParent(ref)}
		}()
	}
	parents := make([]Parent, len(refs))
	for done := 1; done <= len(refs); done++ {
		r := <-results
		parents[r.index] = r.parent
		report(done)
	}
	snap := Snapshot{Parents: parents, Fetched: time.Now()}
	snap.Assignable = assignable(snap.Items())
	return snap
}

const assignableQuery = `query($owner:String!,$repo:String!){repository(owner:$owner,name:$repo){` +
	`assignableUsers(first:100){ nodes{ login } } } }`

// assignable reads who can be given issues in each repo the items are in, by
// repo (owner/repo). A repo that cannot be read is left out, and the list
// offers the people already on the project instead.
func assignable(items []Item) map[string][]string {
	var repos []string
	seen := map[string]bool{}
	for _, it := range items {
		if it.Repo != "" && !seen[it.Repo] {
			seen[it.Repo] = true
			repos = append(repos, it.Repo)
		}
	}
	out := map[string][]string{}
	var lock sync.Mutex
	var wait sync.WaitGroup
	slots := make(chan struct{}, fetchAtOnce)
	for _, repo := range repos {
		owner, name, ok := strings.Cut(repo, "/")
		if !ok {
			continue
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			raw, err := backend.Gh([]string{"api", "graphql", "-f", "query=" + assignableQuery,
				"-F", "owner=" + owner, "-F", "repo=" + name})
			if err != nil {
				return
			}
			var answer struct {
				Data struct {
					Repository *struct {
						AssignableUsers struct{ Nodes []struct{ Login string } }
					}
				}
			}
			if json.Unmarshal([]byte(raw), &answer) != nil || answer.Data.Repository == nil {
				return
			}
			var logins []string
			for _, n := range answer.Data.Repository.AssignableUsers.Nodes {
				if n.Login != "" {
					logins = append(logins, n.Login)
				}
			}
			lock.Lock()
			out[repo] = logins
			lock.Unlock()
		}()
	}
	wait.Wait()
	return out
}
