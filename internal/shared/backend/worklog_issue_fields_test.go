package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func pushFieldArg(args []string, name string) string {
	for i, arg := range args {
		if strings.HasPrefix(arg, name+"=") {
			return strings.TrimPrefix(arg, name+"=")
		}
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// The project field ID and organisation issue field ID intentionally differ.
// Sending the mirrored column's ID to either wrong API must fail this test.
func TestWorklogPushWritesIssueAndProjectMinutesOnCreateDirectAndRetry(t *testing.T) {
	for _, mode := range []string{"create", "direct", "retry", "late"} {
		t.Run(mode, func(t *testing.T) {
			previous := Gh
			t.Cleanup(func() { Gh = previous })
			org := "dualwrite-tests-" + mode
			issueURL := "https://github.com/" + org + "/repo/issues/2"
			creates, types, actualWrites, projectWrites := 0, 0, 0, 0
			failActual := mode == "retry"
			date := "2026-11-19"
			if mode == "late" {
				date = AddDays(WorkDate(time.Now()), -1)
				if !SOPApplies(date) {
					t.Skip("SOP has not started")
				}
			}
			target := "worklog-issue"
			if mode == "direct" {
				target = "parent"
			}
			Gh = func(args []string, _ ...string) (string, error) {
				q := strings.Join(args, " ")
				switch {
				case strings.Contains(q, "subIssues(first:"):
					if !strings.Contains(q, "isIssueField") || !strings.Contains(q, "IssueFieldNumber") {
						t.Fatal("issue context must discover the backing issue-field ID")
					}
					return fmt.Sprintf(`{"data":{"repository":{"issue":{"id":"parent","url":"https://github.com/%s/repo/issues/1","subIssues":{"nodes":[{"id":"worklog-issue","number":2,"url":%q}]},"projectItems":{"nodes":[{"id":"board-item","project":{"id":"board","fields":{"nodes":[
 {"id":"date","name":"ZZZ (DEPRECATED) Worklog (Date)"},
 {"id":"owner","name":"ZZZ (DEPRECATED) Worklog Owner (blg-xxxx)"},
 {"id":"legacy-minutes","name":"ZZZ (DEPRECATED) Worklog (mins)"},
 {"id":"remarks","name":"ZZZ (DEPRECATED) Worklog (Remarks)"},
 {"id":"actual-project-column","name":"Worklog Reported(mins)","isIssueField":true,"issueField":{"id":"actual-issue-field","name":"Worklog Reported(mins)"}},
 {"id":"status","name":"Status","options":[{"id":"done","name":"Done"}]}
]}}}]}}}}}`, org, issueURL), nil
				case len(args) > 1 && args[0] == "issue" && args[1] == "create":
					if mode == "late" && !strings.HasPrefix(pushFieldArg(args, "--body"), "Logged for "+date+"\n\n") {
						t.Fatal("past Worklog creation must start with its intended date")
					}
					creates++
					return issueURL, nil
				case len(args) > 1 && args[0] == "issue" && args[1] == "view":
					return `{"id":"worklog-issue"}`, nil
				case len(args) > 1 && args[0] == "issue" && args[1] == "edit":
					if !strings.HasPrefix(pushFieldArg(args, "--body"), "Logged for "+date) {
						t.Fatal("direct worklog must start with its logged-for date")
					}
					return `{}`, nil
				case strings.Contains(q, "issueTypes(first:"):
					return `{"data":{"organization":{"issueTypes":{"nodes":[{"id":"worklog-type","name":"Worklog"}]}}}}`, nil
				case strings.Contains(q, "updateIssueIssueType"):
					if pushFieldArg(args, "i") != target || pushFieldArg(args, "t") != "worklog-type" {
						t.Fatal("wrong issue or type", args)
					}
					types++
					return `{}`, nil
				case strings.Contains(q, "setIssueFieldValue"):
					if types == 0 || pushFieldArg(args, "issue") != target || pushFieldArg(args, "field") != "actual-issue-field" || pushFieldArg(args, "minutes") != "90" {
						t.Fatal("actual minutes must use the issue API after setting Worklog", args)
					}
					actualWrites++
					if failActual {
						return "", errors.New("actual field temporarily unavailable")
					}
					return `{}`, nil
				case strings.Contains(q, "addProjectV2ItemById"):
					return `{"data":{"addProjectV2ItemById":{"item":{"id":"board-item"}}}}`, nil
				case strings.Contains(q, "addSubIssue"):
					return `{}`, nil
				case len(args) > 1 && args[0] == "project" && args[1] == "item-edit":
					id := pushFieldArg(args, "--field-id")
					if id == "actual-project-column" || id == "actual-issue-field" {
						t.Fatal("an issue field was sent to the project update API")
					}
					if id == "legacy-minutes" {
						if pushFieldArg(args, "--number") != "90" {
							t.Fatal("legacy project minutes differ from actual minutes")
						}
						projectWrites++
					}
					return `{}`, nil
				case strings.Contains(q, "fieldValueByName"):
					return `{"data":{"node":{"fieldValueByName":{"name":"Done"}}}}`, nil
				default:
					t.Fatalf("unexpected GitHub command: %v", args)
					return "", errors.New("unexpected call")
				}
			}
			pushMode, resume := "subissue", ""
			if mode == "direct" {
				pushMode = "issue"
			}
			res, err := pushEntry(Config{}, org+"/repo#1", date, "team-alex", 90, "- notes", "", pushMode, resume, "")
			if err != nil || types != 1 || actualWrites != 1 || projectWrites != 1 {
				t.Fatalf("push failed: %+v %v; types=%d actual=%d project=%d", res, err, types, actualWrites, projectWrites)
			}
			if mode == "retry" {
				if res.Complete() || res.URL != issueURL || len(res.Problems) != 1 {
					t.Fatal("partial push must preserve its issue and remain retryable", res)
				}
				failActual = false
				res, err = pushEntry(Config{}, org+"/repo#1", "2026-11-19", "team-alex", 90, "- notes", "", pushMode, res.URL, "")
				if types != 2 || actualWrites != 2 || projectWrites != 2 || creates != 1 {
					t.Fatal("retry must reapply both absolute totals without creating another issue")
				}
			}
			if err != nil || !res.Complete() {
				t.Fatalf("push not complete: %+v %v", res, err)
			}
			if mode == "direct" && creates != 0 {
				t.Fatal("direct worklog created another issue")
			}
		})
	}
}

func TestMissingWorklogTypeOrReportedFieldIDRemainsRetryable(t *testing.T) {
	previous := Gh
	t.Cleanup(func() { Gh = previous })
	Gh = func(args []string, _ ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "issueTypes(first:") {
			return `{"data":{"organization":{"issueTypes":{"nodes":[]}}}}`, nil
		}
		t.Fatal("must not guess a missing type or issue field ID", args)
		return "", nil
	}
	var fields []field
	if err := json.Unmarshal([]byte(`[{"id":"column","name":"Worklog Reported(mins)","isIssueField":true}]`), &fields); err != nil {
		t.Fatal(err)
	}
	done, problems := setWorklogIssueFields("missing-type-and-id-tests", "issue", fields, 45)
	if len(done) != 0 || len(problems) != 2 {
		t.Fatalf("must expose both incomplete writes: %v %v", done, problems)
	}
}

func TestReportedWorklogIsSetEvenWhenNotDisplayedOnTheProject(t *testing.T) {
	previous := Gh
	t.Cleanup(func() { Gh = previous })
	pages, writes := 0, 0
	Gh = func(args []string, _ ...string) (string, error) {
		query := strings.Join(args, " ")
		switch {
		case strings.Contains(query, "issueTypes(first:"):
			return `{"data":{"organization":{"issueTypes":{"nodes":[{"id":"worklog-type","name":"Worklog"}]}}}}`, nil
		case strings.Contains(query, "updateIssueIssueType"):
			return `{}`, nil
		case strings.Contains(query, "issueFields(first:"):
			pages++
			if pages == 1 {
				return `{"data":{"organization":{"issueFields":{"nodes":[{"id":"unrelated","name":"Estimate"},{"id":"old-field","name":"Actual Worklog (mins)"}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}}`, nil
			}
			if pushFieldArg(args, "after") != "next" {
				t.Fatal("issue-field discovery lost its pagination cursor")
			}
			return `{"data":{"organization":{"issueFields":{"nodes":[{"id":"org-field","name":"Worklog Reported(mins)"}],"pageInfo":{"hasNextPage":false}}}}}`, nil
		case strings.Contains(query, "setIssueFieldValue"):
			if pushFieldArg(args, "issue") != "entry" || pushFieldArg(args, "field") != "org-field" || pushFieldArg(args, "minutes") != "0" {
				t.Fatal("must overwrite the issue field with the given total, including zero")
			}
			writes++
			return `{}`, nil
		default:
			t.Fatal("unexpected call", args)
			return "", nil
		}
	}
	done, problems := setWorklogIssueFields("actual-without-column-tests", "entry", []field{{Name: "ZZZ (DEPRECATED) Worklog (mins)"}}, 0)
	if len(problems) != 0 || len(done) != 2 || pages != 2 || writes != 1 {
		t.Fatalf("issue field was lost when not displayed: %v %v pages=%d writes=%d", done, problems, pages, writes)
	}
}

func TestWorklogIssueFieldPrefersReportedAndKeepsFormerNameCompatible(t *testing.T) {
	oldGh := Gh
	t.Cleanup(func() { Gh = oldGh })
	Gh = func(args []string, _ ...string) (string, error) {
		t.Fatalf("a mirrored issue field should not need another lookup: %v", args)
		return "", nil
	}
	for _, tc := range []struct {
		name, fields, want string
	}{
		{"reported first", `[
 {"name":"Worklog Reported(mins)","isIssueField":true,"issueField":{"id":"reported"}},
 {"name":"Actual Worklog (mins)","isIssueField":true,"issueField":{"id":"old"}}
]`, "reported"},
		{"reported last with other minute fields", `[
 {"name":"Worklog Reported(mins)","id":"project-only"},
 {"name":"Worklog Estimated(mins)","isIssueField":true,"issueField":{"id":"estimate"}},
 {"name":"Actual Worklog (mins)","isIssueField":true,"issueField":{"id":"old"}},
 {"name":"worklog reported (MINS)","isIssueField":true,"issueField":{"id":"reported"}}
]`, "reported"},
		{"former name", `[{"name":"Actual Worklog (mins)","isIssueField":true,"issueField":{"id":"old"}}]`, "old"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fields []field
			if err := json.Unmarshal([]byte(tc.fields), &fields); err != nil {
				t.Fatal(err)
			}
			id, _, err := worklogIssueField("field-rename-tests", fields)
			if err != nil || id != tc.want {
				t.Fatalf("picked %q, %v; want %q", id, err, tc.want)
			}
		})
	}
}

func TestManualIssueKindsAreCreatedAsWorklog(t *testing.T) {
	previous := Gh
	t.Cleanup(func() { Gh = previous })
	for _, kind := range []string{KindMeeting, KindBulkReview, KindIndependent} {
		t.Run(kind, func(t *testing.T) {
			owner := "manual-worklog-type-tests-" + strings.ReplaceAll(kind, "_", "-")
			typed := false
			Gh = func(args []string, _ ...string) (string, error) {
				query := strings.Join(args, " ")
				switch {
				case len(args) > 1 && args[0] == "issue" && args[1] == "list":
					return `[]`, nil
				case len(args) > 1 && args[0] == "issue" && args[1] == "create":
					return "https://github.com/" + owner + "/repo/issues/1", nil
				case len(args) > 1 && args[0] == "issue" && args[1] == "view":
					return `{"id":"entry"}`, nil
				case strings.Contains(query, "issueTypes(first:"):
					return `{"data":{"organization":{"issueTypes":{"nodes":[{"id":"worklog","name":"Worklog"},{"id":"task","name":"Task"},{"id":"meeting","name":"Meeting / Training"}]}}}}`, nil
				case strings.Contains(query, "updateIssueIssueType"):
					if pushFieldArg(args, "t") != "worklog" {
						t.Fatal("manual worklogs must not use Task or Meeting / Training")
					}
					typed = true
					return `{}`, nil
				default:
					t.Fatal("unexpected call", args)
					return "", nil
				}
			}
			cfg := Config{MeetingRepo: owner + "/repo", BulkCodeReviewRepo: owner + "/repo", WorklogOwner: "team-alex"}
			patch, err := EnsureIssueRef(cfg, Row{"id": "row", "type": kind, "date": "2026-11-19", "remarks": "- work",
				"parent_repo": owner + "/repo", "parent_title": "Independent work", "meeting_title": "Planning"})
			if err != nil || patch["issue"] == "" || !typed {
				t.Fatalf("manual issue type was not Worklog: %v %v", patch, err)
			}
		})
	}
}
