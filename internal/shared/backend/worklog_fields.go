package backend

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

func deprecatedWorklogField(name string) bool {
	return nameHas(name, "worklog", "deprecated")
}

// Remember names from the latest fetch so exported board links use the same
// fields as the totals, without making network requests while drawing links.
var knownWorklogFields sync.Map

func worklogFieldsKey(ref projectRef) string {
	return fmt.Sprintf("%s/%s/%d", ref.OwnerKind, strings.ToLower(ref.Login), ref.Number)
}

func fetchWorklogFields(ref projectRef, root string) ([]field, error) {
	query := `query($login:String!,$number:Int!){owner:OWNER(login:$login){projectV2(number:$number){fields(first:100){nodes{... on ProjectV2FieldCommon{id name} ` + issueFieldMetadata + `}}}}}`
	out, err := Gh([]string{"api", "graphql", "-f", "query=" + strings.Replace(query, "OWNER", root, 1),
		"-f", "login=" + ref.Login, "-F", fmt.Sprintf("number=%d", ref.Number)})
	if err != nil {
		return nil, err
	}
	var response struct {
		Data struct {
			Owner *struct {
				Project *project `json:"projectV2"`
			} `json:"owner"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		return nil, GhErr("Could not decode project fields: %v", err)
	}
	if response.Data.Owner == nil || response.Data.Owner.Project == nil {
		return nil, GhErr("Could not find project %d for %s.", ref.Number, ref.Login)
	}
	fields := response.Data.Owner.Project.Fields.Nodes
	knownWorklogFields.Store(worklogFieldsKey(ref), fields)
	return fields, nil
}

func worklogFilterForFields(filter string, fields []field) string {
	for _, spec := range []struct{ token, part string }{
		{"worklog-owner-(blg-xxxx)", "owner"},
		{"worklog-(date)", "date"},
	} {
		if f := findField(fields, "worklog", spec.part); f != nil {
			// GitHub's filter syntax turns spaces in field names into hyphens.
			name := strings.ReplaceAll(strings.ToLower(f.Name), " ", "-")
			filter = strings.ReplaceAll(filter, `"`+spec.token+`":`, `"`+name+`":`)
		}
	}
	return filter
}
