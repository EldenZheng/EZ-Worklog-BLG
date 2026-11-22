package backend

import (
	"encoding/json"
	"fmt"
)

// A project column can mirror an organisation-level issue field. Its project
// field ID is not the ID accepted by the issue-field mutations.
const issueFieldMetadata = `... on ProjectV2Field {
 isIssueField issueField { ... on IssueFieldNumber { id name } }
}`

const worklogMinutesFieldName = "Worklog Reported(mins)"

// The renamed issue field takes precedence when both names are returned.
// Keep the former name readable for organisations and historical issues that
// have not yet renamed it. Legacy project minutes are a separate field.
func worklogMinutesFieldPriority(name string) int {
	if !nameHas(name, "worklog", "min") || deprecatedWorklogField(name) {
		return 0
	}
	if nameHas(name, "reported") {
		return 2
	}
	if nameHas(name, "actual") {
		return 1
	}
	return 0
}

// Every push, including a retry, applies the type and the same absolute minutes
// total. setIssueFieldValue creates a missing value or replaces an existing one;
// it never adds minutes, so a partially completed push is safe to repeat.
func setWorklogIssueFields(org, issueID string, fields []field, mins int) (done, problems []string) {
	typeID, err := issueTypeID(org, "Worklog")
	if err == nil {
		err = setIssueType(issueID, typeID)
	}
	if err != nil {
		problems = append(problems, "Could not set the Worklog issue type: "+err.Error())
	} else {
		done = append(done, "Issue type = Worklog")
	}
	// If setting the type failed, a known field can still be written and its
	// result reported independently. Avoid a speculative lookup otherwise.
	if err != nil && !hasWorklogIssueColumn(fields) {
		return
	}
	id, name, err := worklogIssueField(org, fields)
	if err != nil {
		return done, append(problems, worklogMinutesFieldName+": "+err.Error())
	}
	_, err = Gh([]string{"api", "graphql", "-f",
		"query=mutation($issue:ID!,$field:ID!,$minutes:Float!){setIssueFieldValue(input:{issueId:$issue,issueFields:[{fieldId:$field,numberValue:$minutes}]}){issue{id}}}",
		"-f", "issue=" + issueID, "-f", "field=" + id,
		"-F", fmt.Sprintf("minutes=%d", mins),
	})
	if err != nil {
		problems = append(problems, name+": "+err.Error())
	} else {
		done = append(done, name)
	}
	return
}

func hasWorklogIssueColumn(fields []field) bool {
	for _, f := range fields {
		if f.IsIssueField && worklogMinutesFieldPriority(f.Name) > 0 {
			return true
		}
	}
	return false
}

func worklogIssueField(org string, fields []field) (string, string, error) {
	var best *field
	for i, f := range fields {
		if f.IsIssueField && worklogMinutesFieldPriority(f.Name) > 0 &&
			(best == nil || worklogMinutesFieldPriority(f.Name) > worklogMinutesFieldPriority(best.Name)) {
			best = &fields[i]
		}
	}
	if best != nil {
		if best.IssueField == nil || best.IssueField.ID == "" {
			return "", "", GhErr("GitHub did not return its issue field ID. Retry the push.")
		}
		return best.IssueField.ID, best.Name, nil
	}
	// Issue fields exist independently of project columns. A board need not
	// display Worklog Reported for its worklogs to receive the issue-level value.
	cursor := ""
	var fallbackID, fallbackName string
	for {
		args := []string{"api", "graphql", "-f",
			"query=query($org:String!,$after:String){organization(login:$org){issueFields(first:100,after:$after){nodes{... on IssueFieldNumber{id name}}pageInfo{hasNextPage endCursor}}}}",
			"-f", "org=" + org}
		if cursor != "" {
			args = append(args, "-f", "after="+cursor)
		}
		out, err := Gh(args)
		if err != nil {
			return "", "", err
		}
		var response struct {
			Data struct {
				Organization *struct {
					IssueFields struct {
						Nodes    []struct{ ID, Name string }
						PageInfo struct {
							HasNextPage bool
							EndCursor   string
						}
					}
				}
			}
		}
		if err := json.Unmarshal([]byte(out), &response); err != nil {
			return "", "", GhErr("Could not read issue fields: %v", err)
		}
		if response.Data.Organization == nil {
			return "", "", GhErr("Could not read issue fields for %s.", org)
		}
		page := response.Data.Organization.IssueFields
		for _, f := range page.Nodes {
			if f.ID == "" {
				continue
			}
			switch worklogMinutesFieldPriority(f.Name) {
			case 2:
				return f.ID, f.Name, nil
			case 1:
				fallbackID, fallbackName = f.ID, f.Name
			}
		}
		if !page.PageInfo.HasNextPage {
			if fallbackID != "" {
				return fallbackID, fallbackName, nil
			}
			return "", "", GhErr("%s has no numeric %s issue field.", org, worklogMinutesFieldName)
		}
		if page.PageInfo.EndCursor == "" || page.PageInfo.EndCursor == cursor {
			return "", "", GhErr("GitHub did not return the next page of issue fields.")
		}
		cursor = page.PageInfo.EndCursor
	}
}
