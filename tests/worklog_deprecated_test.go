package tests

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"worklog/internal/shared/backend"
)

func TestDeprecatedWorklogFieldsReadAndPreferCurrentValues(t *testing.T) {
	var values []backend.PvFieldValue
	if err := json.Unmarshal([]byte(`[
		{"text":"old-owner","field":{"name":"ZZZ (DEPRECATED) Worklog Owner (blg-xxxx)"}},
		{"date":"2026-11-09","field":{"name":"ZZZ (DEPRECATED) Worklog (Date)"}},
		{"number":90,"field":{"name":"ZZZ (DEPRECATED) Worklog (mins)"}},
		{"text":"old remarks","field":{"name":"ZZZ (DEPRECATED) Worklog (Remarks)"}}
	]`), &values); err != nil {
		t.Fatal(err)
	}
	got, ok := backend.WorklogFromNode(nil, values)
	if !ok || got.Owner != "old-owner" || got.Date != "2026-11-09" || got.Minutes != 90 || got.Remarks != "old remarks" {
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
	for i, fields := range []struct {
		owner, date, historicalOwner string
	}{
		{"Worklog Owner (blg-xxxx)", "Worklog (Date)", "Worklog Owner (blg-xxxx)"},
		{"ZZZ (DEPRECATED) Worklog Owner (blg-xxxx)", "ZZZ (DEPRECATED) Worklog (Date)", "ZZZ (DEPRECATED) Worklog Owner (blg-xxxx)"},
		{"Worklog Owner (blg-xxxx)", "Worklog (Date)", "ZZZ (DEPRECATED) Worklog Owner (blg-xxxx)"},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			board := fmt.Sprintf("https://github.com/orgs/field-fallback-test/projects/%d", i+1)
			pages := 0
			filterFor := func(owner, date string) string {
				fieldKey := func(name string) string { return strings.ReplaceAll(strings.ToLower(name), " ", "-") }
				return fmt.Sprintf(`"%s":team-alex "%s":>=2026-11-09 "%s":<=2026-11-15`, fieldKey(owner), fieldKey(date), fieldKey(date))
			}
			backend.Gh = func(args []string, _ ...string) (string, error) {
				if len(args) < 2 || args[0] != "api" || args[1] != "graphql" {
					t.Fatalf("unexpected command: %v", args)
				}
				joined := strings.Join(args, " ")
				if !strings.Contains(joined, "items(first:") {
					return fmt.Sprintf(`{"data":{"owner":{"projectV2":{"fields":{"nodes":[{"name":%q},{"name":%q},{"name":%q}]}}}}}`, fields.owner, fields.date, fields.historicalOwner), nil
				}
				filter := ""
				for _, arg := range args {
					if strings.HasPrefix(arg, "filter=") {
						filter = strings.TrimPrefix(arg, "filter=")
					}
				}
				want := filterFor(fields.owner, fields.date)
				if filter != want {
					t.Fatalf("filter = %q, want %q", filter, want)
				}
				pages++
				return fmt.Sprintf(`{"data":{"owner":{"projectV2":{"items":{"nodes":[{"content":{"url":"issue"},"fieldValues":{"nodes":[{"text":"team-alex","field":{"name":%q}},{"date":"2026-11-09","field":{"name":%q}},{"number":90,"field":{"name":"Worklog (mins)"}}]}}],"pageInfo":{"hasNextPage":false}}}}}}`, fields.owner, fields.date), nil
			}
			cfg := backend.Config{ProjectURL: board}
			items, err := backend.FetchProjectWorklogs(cfg, "team-alex", "2026-11-09", "2026-11-15")
			if err != nil || len(items) != 1 || items[0].Minutes != 90 || pages != 1 {
				t.Fatalf("items=%+v pages=%d err=%v", items, pages, err)
			}
			links := backend.WeekBoardLinks(cfg, "team-alex", "2026-11-09", "2026-11-15")
			if len(links) != 2 {
				t.Fatalf("export link lost actual field names: %v", links)
			}
			link, err := url.Parse(links[0])
			if err != nil || link.Query().Get("filterQuery") != filterFor(fields.historicalOwner, fields.date) {
				t.Fatalf("export link should prefer the historical field where available: %v", links)
			}
		})
	}
}
