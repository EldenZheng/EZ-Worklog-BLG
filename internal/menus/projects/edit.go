package projects

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"worklog/internal/shared/backend"
)

// ---- changing an issue from the issue list ----
//
// These are the page's only writes to GitHub: an issue's Status, Priority and
// Sprint on its board, and who it is assigned to.

const setStatusMutation = `mutation($project:ID!,$item:ID!,$field:ID!,$value:String!){` +
	`updateProjectV2ItemFieldValue(input:{projectId:$project,itemId:$item,fieldId:$field,value:{singleSelectOptionId:$value}}){projectV2Item{id}}}`

// SetStatusArgs is the gh call that sets the issue's Status on its board to
// the option called name.
func SetStatusArgs(it Item, name string) ([]string, error) {
	if it.Board == nil {
		return nil, fmt.Errorf("#%d is on no project board with a Status field", it.Number)
	}
	id := ""
	for _, o := range it.Board.Options {
		if o.Name == name {
			id = o.ID
		}
	}
	if id == "" {
		return nil, fmt.Errorf("%q is not a Status on #%d's board", name, it.Number)
	}
	return []string{"api", "graphql", "-f", "query=" + setStatusMutation,
		"-f", "project=" + it.Board.ProjectID, "-f", "item=" + it.Board.ItemID,
		"-f", "field=" + it.Board.FieldID, "-f", "value=" + id}, nil
}

// SetStatus sets the issue's Status on its board.
func SetStatus(it Item, name string) error {
	args, err := SetStatusArgs(it, name)
	if err != nil {
		return err
	}
	_, err = backend.Gh(args)
	return err
}

// SetFieldArgs is the gh call that sets one of the board's fields on the
// issue: "priority" to the option called name, or "sprint" to the sprint with
// that title. An empty name clears the field.
func SetFieldArgs(it Item, kind, name string) ([]string, error) {
	if it.Board == nil {
		return nil, fmt.Errorf("#%d is on no project board", it.Number)
	}
	field, options, typ := it.Board.PriorityField, it.Board.Priorities, "singleSelectOptionId"
	if kind == "sprint" {
		field, options, typ = it.Board.SprintField, it.Board.Sprints, "iterationId"
	}
	if field == "" {
		return nil, fmt.Errorf("#%d's board has no %s field", it.Number, kind)
	}
	args := []string{"api", "graphql", "-f", "project=" + it.Board.ProjectID, "-f", "item=" + it.Board.ItemID, "-f", "field=" + field}
	if name == "" {
		return append(args, "-f", "query="+clearFieldMutation), nil
	}
	id := ""
	for _, o := range options {
		if o.Name == name {
			id = o.ID
		}
	}
	if id == "" {
		return nil, fmt.Errorf("%q is not a %s on #%d's board", name, kind, it.Number)
	}
	query := `mutation($project:ID!,$item:ID!,$field:ID!,$value:String!){` +
		`updateProjectV2ItemFieldValue(input:{projectId:$project,itemId:$item,fieldId:$field,value:{` + typ + `:$value}}){projectV2Item{id}}}`
	return append(args, "-f", "value="+id, "-f", "query="+query), nil
}

const clearFieldMutation = `mutation($project:ID!,$item:ID!,$field:ID!){` +
	`clearProjectV2ItemFieldValue(input:{projectId:$project,itemId:$item,fieldId:$field}){projectV2Item{id}}}`

// SetField sets the issue's Priority or Sprint on its board.
func SetField(it Item, kind, name string) error {
	args, err := SetFieldArgs(it, kind, name)
	if err != nil {
		return err
	}
	_, err = backend.Gh(args)
	return err
}

// SetAssigneeArgs is the gh call that makes who the issue's only assignee, or
// leaves it with none when who is "". Nil when nothing would change.
func SetAssigneeArgs(it Item, who string) []string {
	args := []string{"issue", "edit", it.URL}
	for _, a := range it.Assignees {
		if a != who {
			args = append(args, "--remove-assignee", a)
		}
	}
	if who != "" && !slices.Contains(it.Assignees, who) {
		args = append(args, "--add-assignee", who)
	}
	if len(args) == 3 {
		return nil
	}
	return args
}

// SetAssignee makes who the issue's only assignee ("" for none).
func SetAssignee(it Item, who string) error {
	args := SetAssigneeArgs(it, who)
	if args == nil {
		return nil
	}
	_, err := backend.Gh(args)
	return err
}

// Patch changes the issue with this URL in the snapshot, in place, so the
// page shows a change made from it without fetching everything again.
func (s Snapshot) Patch(url string, change func(*Item)) {
	for p := range s.Parents {
		for i := range s.Parents[p].Items {
			if s.Parents[p].Items[i].URL == url {
				change(&s.Parents[p].Items[i])
			}
		}
	}
}

// ---- milestones ----

// Milestone is one milestone's share of the scope.
type Milestone struct {
	Bucket
	Due time.Time
}

const noMilestone = "No milestone"

// milestones splits the scope by milestone: by due date, those with none
// after, and the issues in no milestone last. None at all when no issue has a
// milestone.
func milestones(items []Item) []Milestone {
	due := map[string]time.Time{}
	byName := bucket(items, func(it Item) string {
		if it.Milestone == "" {
			return noMilestone
		}
		due[it.Milestone] = it.MilestoneDue
		return it.Milestone
	})
	if len(byName) == 1 && byName[noMilestone] != nil {
		return nil
	}
	var out []Milestone
	for name, b := range byName {
		out = append(out, Milestone{Bucket: *b, Due: due[name]})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Name == noMilestone) != (b.Name == noMilestone) {
			return b.Name == noMilestone
		}
		if a.Due.IsZero() != b.Due.IsZero() {
			return b.Due.IsZero()
		}
		if !a.Due.Equal(b.Due) {
			return a.Due.Before(b.Due)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out
}

// Matches says whether the issue answers a search: every word appears in its
// number, title, repo, assignees, status, priority, sprint or milestone.
func (it Item) Matches(search string) bool {
	hay := strings.ToLower(strings.Join([]string{
		fmt.Sprintf("#%d", it.Number), it.Title, it.Repo, strings.Join(it.Assignees, " "),
		it.Status, it.Priority, it.Sprint, it.Milestone,
	}, " "))
	for _, word := range strings.Fields(strings.ToLower(search)) {
		if !strings.Contains(hay, word) {
			return false
		}
	}
	return true
}
