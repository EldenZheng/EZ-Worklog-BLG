package tests

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"worklog/internal/shared/backend"
)

func TestDeprecatedWorklogFieldsReadAndPreferCurrentValues(t *testing.T) {
	var values []backend.PvFieldValue
	if err := json.Unmarshal([]byte(`[
		{"text":"old-owner","field":{"name":"ZZZ (DEPRECATED) Worklog Owner (blg-xxxx)"}},
		{"date":"2026-11-16","field":{"name":"ZZZ (DEPRECATED) Worklog (Date)"}},
		{"number":90,"field":{"name":"ZZZ (DEPRECATED) Worklog (mins)"}},
		{"text":"old remarks","field":{"name":"ZZZ (DEPRECATED) Worklog (Remarks)"}}
	]`), &values); err != nil {
		t.Fatal(err)
	}
	got, ok := backend.WorklogFromNode(nil, values)
	if !ok || got.Owner != "old-owner" || got.Date != "2026-11-16" || got.Minutes != 90 || got.Remarks != "old remarks" {
		t.Fatalf("deprecated fields lost: %+v", got)
	}
	var current []backend.PvFieldValue
	if err := json.Unmarshal([]byte(`[
		{"text":"new-owner","field":{"name":"Worklog Owner (blg-xxxx)"}},
		{"date":"2026-11-17","field":{"name":"Worklog (Date)"}},
		{"number":0,"field":{"name":"Worklog (mins)"}},
		{"text":"","field":{"name":"Worklog (Remarks)"}}
	]`), &current); err != nil {
		t.Fatal(err)
	}
	for _, combined := range [][]backend.PvFieldValue{append(append([]backend.PvFieldValue{}, values...), current...), append(append([]backend.PvFieldValue{}, current...), values...)} {
		got, ok = backend.WorklogFromNode(nil, combined)
		if !ok || got.Owner != "new-owner" || got.Date != "2026-11-17" || got.Minutes != 0 || got.Remarks != "old remarks" {
			t.Fatalf("current values must win in either order, with blank remarks falling back: %+v", got)
		}
	}
}

func TestProjectWorklogsFilterUsesActualBoardFields(t *testing.T) {
	old := backend.Gh
	defer func() { backend.Gh = old }()
	for i, prefix := range []string{"", "ZZZ (DEPRECATED) "} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			url := fmt.Sprintf("https://github.com/orgs/field-fallback-test/projects/%d", i+1)
			pages := 0
			backend.Gh = func(args []string, _ ...string) (string, error) {
				if len(args) < 2 || args[0] != "api" || args[1] != "graphql" {
					t.Fatalf("unexpected command: %v", args)
				}
				joined := strings.Join(args, " ")
				if !strings.Contains(joined, "items(first:") {
					return fmt.Sprintf(`{"data":{"owner":{"projectV2":{"fields":{"nodes":[{"name":%q},{"name":%q}]}}}}}`, prefix+"Worklog Owner (blg-xxxx)", prefix+"Worklog (Date)"), nil
				}
				filter := ""
				for _, arg := range args {
					if strings.HasPrefix(arg, "filter=") {
						filter = strings.TrimPrefix(arg, "filter=")
					}
				}
				fieldPrefix := strings.ReplaceAll(strings.ToLower(prefix), " ", "-")
				want := fmt.Sprintf(`"%sworklog-owner-(blg-xxxx)":team-alex "%sworklog-(date)":>=2026-11-16 "%sworklog-(date)":<=2026-11-22`, fieldPrefix, fieldPrefix, fieldPrefix)
				if filter != want {
					t.Fatalf("filter = %q, want %q", filter, want)
				}
				pages++
				return fmt.Sprintf(`{"data":{"owner":{"projectV2":{"items":{"nodes":[{"content":{"url":"issue"},"fieldValues":{"nodes":[{"text":"team-alex","field":{"name":%q}},{"date":"2026-11-16","field":{"name":%q}},{"number":90,"field":{"name":%q}}]}}],"pageInfo":{"hasNextPage":false}}}}}}`, prefix+"Worklog Owner (blg-xxxx)", prefix+"Worklog (Date)", prefix+"Worklog (mins)"), nil
			}
			cfg := backend.Config{ProjectURL: url}
			items, err := backend.FetchProjectWorklogs(cfg, "team-alex", "2026-11-16", "2026-11-22")
			if err != nil || len(items) != 1 || items[0].Minutes != 90 || pages != 1 {
				t.Fatalf("items=%+v pages=%d err=%v", items, pages, err)
			}
			links := backend.WeekBoardLinks(cfg, "team-alex", "2026-11-16", "2026-11-22")
			if len(links) != 1 || (prefix != "" && !strings.Contains(links[0], "zzz-")) {
				t.Fatalf("export link lost actual field names: %v", links)
			}
		})
	}
}
