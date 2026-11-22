package backend

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The Worklog columns, current and ZZZ (DEPRECATED), taken off the board: the
// push says each is gone without failing, and still sets Status. A Status that
// fails leaves the push partly saved and retryable on the same Worklog.
func TestPushWithWorklogColumnsRemovedNotesThemAndStillSetsStatus(t *testing.T) {
	for _, statusFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("statusFails=%v", statusFails), func(t *testing.T) {
			previous := Gh
			t.Cleanup(func() { Gh = previous })
			org := fmt.Sprintf("removed-fields-%v", statusFails)
			issueURL := "https://github.com/" + org + "/repo/issues/2"
			statusSet := 0
			Gh = func(args []string, _ ...string) (string, error) {
				q := strings.Join(args, " ")
				switch {
				case strings.Contains(q, "subIssues(first:"):
					return fmt.Sprintf(`{"data":{"repository":{"issue":{"id":"parent","url":"https://github.com/%s/repo/issues/1","subIssues":{"nodes":[]},"projectItems":{"nodes":[{"id":"board-item","project":{"id":"board","fields":{"nodes":[
 {"id":"actual-project-column","name":"Actual Worklog (mins)","isIssueField":true,"issueField":{"id":"actual-issue-field","name":"Actual Worklog (mins)"}},
 {"id":"status","name":"Status","options":[{"id":"done","name":"Done"}]}
]}}}]}}}}}`, org), nil
				case len(args) > 1 && args[0] == "issue" && args[1] == "create":
					return issueURL, nil
				case len(args) > 1 && args[0] == "issue" && args[1] == "view":
					return `{"id":"worklog-issue"}`, nil
				case strings.Contains(q, "issueTypes(first:"):
					return `{"data":{"organization":{"issueTypes":{"nodes":[{"id":"worklog-type","name":"Worklog"}]}}}}`, nil
				case strings.Contains(q, "updateIssueIssueType"), strings.Contains(q, "setIssueFieldValue"), strings.Contains(q, "addSubIssue"):
					return `{}`, nil
				case strings.Contains(q, "addProjectV2ItemById"):
					return `{"data":{"addProjectV2ItemById":{"item":{"id":"board-item"}}}}`, nil
				case len(args) > 1 && args[0] == "project" && args[1] == "item-edit":
					if pushFieldArg(args, "--field-id") != "status" {
						t.Fatalf("wrote a column that is not on the board: %v", args)
					}
					statusSet++
					if statusFails {
						return "", errors.New("Status is read-only")
					}
					return `{}`, nil
				case strings.Contains(q, "fieldValueByName"):
					return `{"data":{"node":{"fieldValueByName":{"name":"Done"}}}}`, nil
				default:
					t.Fatalf("unexpected GitHub command: %v", args)
					return "", errors.New("unexpected call")
				}
			}
			res, err := pushEntry(Config{}, org+"/repo#1", WorkDate(time.Now()), "team-alex", 60, "- notes", "", "subissue", "", "")
			if err != nil || statusSet != 1 || res.URL != issueURL {
				t.Fatalf("push: %+v %v; status writes=%d", res, err, statusSet)
			}
			gone := 0
			for _, n := range res.Notes {
				if strings.Contains(n, "no longer exists on the board") {
					gone++
				}
			}
			if gone != 4 {
				t.Fatalf("want a note for each of the 4 removed columns: %v", res.Notes)
			}
			if statusFails {
				if res.Complete() || len(res.Problems) != 1 || !strings.Contains(res.Problems[0], "Status") {
					t.Fatalf("a failed Status must leave the push partly saved: %+v", res)
				}
			} else if !res.Complete() {
				t.Fatalf("removed columns must not fail the push: %+v", res.Problems)
			}
		})
	}
}
