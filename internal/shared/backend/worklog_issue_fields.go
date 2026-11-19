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
	if err != nil && !hasActualIssueColumn(fields) {
		return
	}
	id, name, err := actualWorklogIssueField(org, fields)
	if err != nil {
		return done, append(problems, "Actual Worklog (mins): "+err.Error())
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

func hasActualIssueColumn(fields []field) bool {
	for _, f := range fields {
		if f.IsIssueField && nameHas(f.Name, "actual", "worklog", "min") {
			return true
		}
	}
	return false
}

func actualWorklogIssueField(org string, fields []field) (string, string, error) {
	for _, f := range fields {
		if f.IsIssueField && nameHas(f.Name, "actual", "worklog", "min") {
			if f.IssueField == nil || f.IssueField.ID == "" {
				return "", "", GhErr("GitHub did not return its issue field ID. Retry the push.")
			}
			return f.IssueField.ID, f.Name, nil
		}
	}
	// Issue fields exist independently of project columns. A board need not
	// display Actual Worklog for its worklogs to receive the issue-level value.
	cursor := ""
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
			if f.ID != "" && nameHas(f.Name, "actual", "worklog", "min") {
				return f.ID, f.Name, nil
			}
		}
		if !page.PageInfo.HasNextPage {
			return "", "", GhErr("%s has no numeric Actual Worklog (mins) issue field.", org)
		}
		if page.PageInfo.EndCursor == "" || page.PageInfo.EndCursor == cursor {
			return "", "", GhErr("GitHub did not return the next page of issue fields.")
		}
		cursor = page.PageInfo.EndCursor
	}
}
