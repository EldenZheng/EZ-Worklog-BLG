package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

var sprintBoardURLs = []string{
	"https://github.com/orgs/bigledger/projects/9",
	"https://github.com/orgs/BigLedger-Support/projects/1",
}

func sameSprintProject(a, b string) bool {
	x, e := parseProjectRef(a)
	y, f := parseProjectRef(b)
	return e == nil && f == nil && strings.EqualFold(x.login, y.login) && x.number == y.number && x.ownerKind == y.ownerKind
}

type sprintIteration struct {
	ID, Title, StartDate string
	Duration             int
}

func (s sprintIteration) contains(date string) bool {
	start, err := time.Parse("2006-01-02", s.StartDate)
	return err == nil && date >= s.StartDate && date < start.AddDate(0, 0, s.Duration).Format("2006-01-02")
}

type sprintField struct {
	ID, Name      string
	Options       []fieldOption
	Configuration struct{ Iterations, CompletedIterations []sprintIteration }
}

type sprintItem struct {
	ID, Title, URL, Status, SprintID, Complexity string
	Effort                                       int
	Kind                                         string `json:",omitempty"`
}

func (it sprintItem) key(projectID string) string {
	if it.URL != "" {
		return it.URL
	}
	return projectID + "/" + it.ID
}

type sprintBoard struct {
	ID, Title, URL, Filter string
	Fields                 []sprintField
	Items                  []sprintItem
}

func (b sprintBoard) field(name string) sprintField {
	for _, f := range b.Fields {
		if strings.EqualFold(f.Name, name) {
			return f
		}
	}
	return sprintField{}
}

func (b sprintBoard) iterations() []sprintIteration {
	f := b.field("Sprint")
	return append(append([]sprintIteration{}, f.Configuration.CompletedIterations...), f.Configuration.Iterations...)
}

func (b sprintBoard) current(date string) sprintIteration {
	for _, s := range b.iterations() {
		if s.contains(date) {
			return s
		}
	}
	return sprintIteration{}
}

// Personal open issues are an app-owned filter, independent of shared views.
const sprintProjectFilter = "assignee:@me is:open"

const sprintMetadataQuery = `query($login:String!,$number:Int!){
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

func fetchSprintBoard(rawURL string) (sprintBoard, error) {
	return fetchSprintBoardWithFilter(rawURL, sprintProjectFilter)
}

func fetchSprintBoardWithFilter(rawURL, filter string) (sprintBoard, error) {
	b := sprintBoard{URL: rawURL}
	ref, err := parseProjectRef(rawURL)
	if err != nil {
		return b, err
	}
	out, err := gh([]string{"api", "graphql", "-f", "query=" + strings.Replace(sprintMetadataQuery, "OWNER", map[string]string{"orgs": "organization", "users": "user"}[ref.ownerKind], 1), "-f", "login=" + ref.login, "-F", fmt.Sprintf("number=%d", ref.number)})
	if err != nil {
		return b, err
	}
	var meta struct {
		Data struct {
			Owner struct {
				Project *struct {
					ID, Title string
					Fields    struct{ Nodes []sprintField }
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
		out, err = gh(args)
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
			it := sprintItem{ID: n.ID, Title: n.Content.Title, URL: n.Content.URL, Kind: n.Content.Kind}
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
func updateSprintItem(b sprintBoard, it sprintItem, kind, value string) error {
	args, err := sprintItemMutation(b, it, kind, value)
	if err != nil {
		return err
	}
	_, err = gh(args)
	return err
}

func sprintItemMutation(b sprintBoard, it sprintItem, kind, value string) ([]string, error) {
	if kind != "status" && kind != "sprint" {
		return nil, fmt.Errorf("unknown project field %s", kind)
	}
	name := "Status"
	if kind == "sprint" {
		name = "Sprint"
	}
	f := b.field(name)
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
		for _, s := range b.iterations() {
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
