package backend

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
)

var SprintBoardURLs = []string{
	"https://github.com/orgs/bigledger/projects/9",
	"https://github.com/orgs/BigLedger-Support/projects/1",
}

func SameSprintProject(a, b string) bool {
	x, e := ParseProjectRef(a)
	y, f := ParseProjectRef(b)
	return e == nil && f == nil && strings.EqualFold(x.Login, y.Login) && x.Number == y.Number && x.OwnerKind == y.OwnerKind
}

type SprintIteration struct {
	ID, Title, StartDate string
	Duration             int
}

func (s SprintIteration) Contains(date string) bool {
	start, err := time.Parse("2006-01-02", s.StartDate)
	return err == nil && date >= s.StartDate && date < start.AddDate(0, 0, s.Duration).Format("2006-01-02")
}

type SprintField struct {
	ID, Name      string
	Options       []FieldOption
	Configuration struct{ Iterations, CompletedIterations []SprintIteration }
}

type SprintItem struct {
	ID, Title, URL, Status, SprintID, Complexity string
	Effort                                       int
	Kind                                         string `json:",omitempty"`
}

func (it SprintItem) Key(projectID string) string {
	if it.URL != "" {
		return it.URL
	}
	return projectID + "/" + it.ID
}

type SprintBoard struct {
	ID, Title, URL, Filter string
	Fields                 []SprintField
	Items                  []SprintItem
}

func (b SprintBoard) Field(name string) SprintField {
	for _, f := range b.Fields {
		if strings.EqualFold(f.Name, name) {
			return f
		}
	}
	return SprintField{}
}

func (b SprintBoard) Iterations() []SprintIteration {
	f := b.Field("Sprint")
	return append(append([]SprintIteration{}, f.Configuration.CompletedIterations...), f.Configuration.Iterations...)
}

func (b SprintBoard) Current(date string) SprintIteration {
	for _, s := range b.Iterations() {
		if s.Contains(date) {
			return s
		}
	}
	return SprintIteration{}
}

// Personal open issues are an app-owned filter, independent of shared views.
const SprintProjectFilter = "assignee:@me is:open"

const SprintMetadataQuery = `query($login:String!,$number:Int!){
 owner:OWNER(login:$login){projectV2(number:$number){id title
 fields(first:100){nodes{
 ... on ProjectV2SingleSelectField{id name options{id name}}
 ... on ProjectV2IterationField{id name configuration{iterations{id title startDate duration} completedIterations{id title startDate duration}}}
 }}}}}`

const sprintItemsQuery = `query($id:ID!,$filter:String!,$after:String){node(id:$id){... on ProjectV2{
 items(first:100,after:$after,query:$filter){nodes{id
 content{__typename ... on Issue{title url} ... on PullRequest{title url} ... on DraftIssue{title}}
 fieldValues(first:100){nodes{
 ... on ProjectV2ItemFieldSingleSelectValue{name field{... on ProjectV2FieldCommon{name}}}
 ... on ProjectV2ItemFieldNumberValue{number field{... on ProjectV2FieldCommon{name}}}
 ... on ProjectV2ItemFieldIterationValue{iterationId field{... on ProjectV2FieldCommon{name}}}
 }}}
 pageInfo{hasNextPage endCursor}
 }}}}`

func FetchSprintBoard(rawURL string) (SprintBoard, error) {
	return FetchSprintBoardWithFilter(rawURL, SprintProjectFilter)
}

func FetchSprintBoardWithFilter(rawURL, filter string) (SprintBoard, error) {
	b := SprintBoard{URL: rawURL}
	ref, err := ParseProjectRef(rawURL)
	if err != nil {
		return b, err
	}
	out, err := Gh([]string{"api", "graphql", "-f", "query=" + strings.Replace(SprintMetadataQuery, "OWNER", map[string]string{"orgs": "organization", "users": "user"}[ref.OwnerKind], 1), "-f", "login=" + ref.Login, "-F", fmt.Sprintf("number=%d", ref.Number)})
	if err != nil {
		return b, err
	}
	var meta struct {
		Data struct {
			Owner struct {
				Project *struct {
					ID, Title string
					Fields    struct{ Nodes []SprintField }
				} `json:"projectV2"`
			}
		}
	}
	if err = json.Unmarshal([]byte(out), &meta); err != nil {
		return b, err
	}
	p := meta.Data.Owner.Project
	if p == nil {
		return b, fmt.Errorf("project unavailable: %s", rawURL)
	}
	b.ID, b.Title, b.Filter, b.Fields = p.ID, p.Title, filter, p.Fields.Nodes
	after := ""
	for {
		args := []string{"api", "graphql", "-f", "query=" + sprintItemsQuery, "-f", "id=" + b.ID, "-f", "filter=" + b.Filter}
		if after != "" {
			args = append(args, "-f", "after="+after)
		}
		out, err = Gh(args)
		if err != nil {
			return b, err
		}
		var page struct {
			Data struct {
				Node struct {
					Items struct {
						Nodes []struct {
							ID      string
							Content *struct {
								Title, URL string
								Kind       string `json:"__typename"`
							}
							FieldValues struct {
								Nodes []struct {
									Name, IterationID string
									Number            *float64
									Field             struct{ Name string }
								}
							}
						}
						PageInfo struct {
							HasNextPage bool
							EndCursor   string
						}
					}
				}
			}
		}
		if err = json.Unmarshal([]byte(out), &page); err != nil {
			return b, err
		}
		for _, n := range page.Data.Node.Items.Nodes {
			if n.Content == nil || n.Content.Kind == "DraftIssue" {
				continue
			}
			it := SprintItem{ID: n.ID, Title: n.Content.Title, URL: n.Content.URL, Kind: n.Content.Kind}
			for _, v := range n.FieldValues.Nodes {
				switch v.Field.Name {
				case "Status":
					it.Status = v.Name
				case "Sprint":
					it.SprintID = v.IterationID
				case "Complexity":
					it.Complexity = v.Name
				case "Estimated Effort (Mins)":
					if v.Number != nil {
						it.Effort = int(*v.Number)
					}
				}
			}
			b.Items = append(b.Items, it)
		}
		pi := page.Data.Node.Items.PageInfo
		if !pi.HasNextPage {
			break
		}
		if pi.EndCursor == "" || pi.EndCursor == after {
			return b, fmt.Errorf("project pagination did not advance")
		}
		after = pi.EndCursor
	}
	return b, nil
}

// IDs are always resolved against the source project; identical sprint titles
// and status names on the two boards have different GitHub IDs.
func updateSprintItem(b SprintBoard, it SprintItem, kind, value string) error {
	args, err := SprintItemMutation(b, it, kind, value)
	if err != nil {
		return err
	}
	_, err = Gh(args)
	return err
}

func SprintItemMutation(b SprintBoard, it SprintItem, kind, value string) ([]string, error) {
	if kind != "status" && kind != "sprint" {
		return nil, fmt.Errorf("unknown project field %s", kind)
	}
	name := "Status"
	if kind == "sprint" {
		name = "Sprint"
	}
	f := b.Field(name)
	if f.ID == "" {
		return nil, fmt.Errorf("%s has no %s field", b.Title, name)
	}
	var id string
	if kind == "status" {
		for _, o := range f.Options {
			if o.Name == value {
				id = o.ID
			}
		}
	} else {
		for _, s := range b.Iterations() {
			if s.ID == value {
				id = s.ID
			}
		}
	}
	if value != "" && id == "" {
		return nil, fmt.Errorf("%s is not available on %s", value, b.Title)
	}
	query := `mutation($project:ID!,$item:ID!,$field:ID!){clearProjectV2ItemFieldValue(input:{projectId:$project,itemId:$item,fieldId:$field}){projectV2Item{id}}}`
	args := []string{"api", "graphql", "-f", "project=" + b.ID, "-f", "item=" + it.ID, "-f", "field=" + f.ID}
	if value != "" {
		typ := "singleSelectOptionId"
		if kind == "sprint" {
			typ = "iterationId"
		}
		query = `mutation($project:ID!,$item:ID!,$field:ID!,$value:String!){updateProjectV2ItemFieldValue(input:{projectId:$project,itemId:$item,fieldId:$field,value:{` + typ + `:$value}}){projectV2Item{id}}}`
		args = append(args, "-f", "value="+id)
	}
	return append(args, "-f", "query="+query), nil
}

func (it SprintItem) ItemType() string {
	if it.Kind == "PullRequest" {
		return KanbanPullRequests
	}
	// Older local snapshots predate the GraphQL type field.
	if u, err := url.Parse(it.URL); err == nil {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 4 && parts[2] == "pull" {
			return KanbanPullRequests
		}
	}
	return KanbanIssues
}

func (it SprintItem) TypeIcon() fyne.Resource {
	if it.ItemType() == KanbanPullRequests {
		return kanbanPullRequestIcon
	}
	return kanbanIssueIcon
}

func (it SprintItem) RepositoryNumber(b SprintBoard) string {
	if u, err := url.Parse(it.URL); err == nil {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 4 && (parts[2] == "issues" || parts[2] == "pull") {
			return parts[0] + "/" + parts[1] + "#" + parts[3]
		}
	}
	return OrDefault((WorklogItem{URL: it.URL}).Repo(), ProjectShortName(b))
}

// ---- making GitHub writes from the Kanban visible while they run ----

// PushSprintItem sends one field change to GitHub. A variable so tests can
// stand in for the gh CLI.
var PushSprintItem = updateSprintItem

func (b SprintBoard) SprintByKey(key string) SprintIteration {
	for _, s := range b.Iterations() {
		if SprintKey(s) == key {
			return s
		}
	}
	return SprintIteration{}
}
