package backend

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLoggedForDescriptionIsIdempotentAndKeepsRemarksAndMarkers(t *testing.T) {
	date := AddDays(WorkDate(time.Now()), -1)
	body := "- work done\n\n" + worklogAppSection + "\nRef https://github.com/o/r/commit/abc\n" + entryMarker("entry")
	got := loggedForBody(body, date)
	if !strings.HasPrefix(got, "Logged for "+date+"\n\n- work done") || WorklogRemarks(got) != "- work done" || !strings.Contains(got, entryMarker("entry")) || !strings.Contains(got, "Ref https://github.com/o/r/commit/abc") {
		t.Fatal("logged-for note must preserve the user content and retry metadata", got)
	}
	if again := loggedForBody(got, date); again != got || strings.Count(again, "Logged for ") != 1 {
		t.Fatal("retry duplicated or changed the logged-for note", again)
	}
	changed := loggedForBody(got, AddDays(date, -1))
	if strings.Count(changed, "Logged for ") != 1 || !strings.Contains(changed, "Logged for "+AddDays(date, -1)) {
		t.Fatal("changed date duplicated its note")
	}
	for _, day := range []string{WorkDate(time.Now()), AddDays(WorkDate(time.Now()), 1), "bad"} {
		if result := loggedForBody("- work done", day); result != "- work done" {
			t.Fatalf("date %s unexpectedly changed the body: %s", day, result)
		}
	}
	large := loggedForBody(strings.Repeat("x", BodyCap)+"\n\n"+worklogAppSection+"\n"+entryMarker("entry"), date)
	if len(large) > BodyCap || !strings.Contains(large, "Logged for "+date) || !strings.Contains(large, entryMarker("entry")) {
		t.Fatal("description cap cut off the logged-for date or retry marker")
	}
}

func TestLoggedForHeaderMatchesPlainIssuesAndMovesOldFooterOnRetry(t *testing.T) {
	date := AddDays(WorkDate(time.Now()), -1)
	header := "Logged for " + date
	metadata := worklogAppSection + "\nRef https://github.com/o/r/commit/abc\n" + entryMarker("entry")
	for _, tc := range []struct{ body, want string }{
		{"- work done", header + "\n\n- work done"},
		{header + "\n\n- work done", header + "\n\n- work done"},
		{"- work done\n\n" + worklogAppSection + "\n" + header + "\n\nRef https://github.com/o/r/commit/abc\n" + entryMarker("entry"), header + "\n\n- work done\n\n" + metadata},
		{header + "\n\n- work done\n\n" + worklogAppSection + "\n" + header, header + "\n\n- work done\n\n" + worklogAppSection},
		{"", header},
	} {
		got := loggedForBody(tc.body, date)
		if got != tc.want || loggedForBody(got, date) != got || strings.Count(got, "Logged for ") != 1 {
			t.Errorf("header normalization for %q = %q; want %q", tc.body, got, tc.want)
		}
		wantRemarks := "- work done"
		if tc.body == "" {
			wantRemarks = ""
		}
		if remarks := WorklogRemarks(got); remarks != wantRemarks {
			t.Errorf("header leaked into remarks: %q", remarks)
		}
	}
	// The linked example has a plain first line and no hidden app marker.
	created, _ := time.Parse(time.RFC3339, "2026-11-22T16:53:07Z")
	plain := "Logged for 2026-11-22\n\n- investigated the issue\n- posted findings"
	if worklogDay(plain, created) != "2026-11-22" || WorklogRemarks(plain) != "- investigated the issue\n- posted findings" {
		t.Fatal("plain first-line date was not read like the example issue")
	}
	changed := loggedForBody(header+"\n\n- work done", AddDays(date, -1))
	if !strings.HasPrefix(changed, "Logged for "+AddDays(date, -1)+"\n\n- work done") || strings.Count(changed, "Logged for ") != 1 {
		t.Fatal("changing a leading date duplicated it", changed)
	}
}

func TestLatePushCountsOnceOnItsRequestedDayWhileSearchCatchesUp(t *testing.T) {
	row := Row{"date": "2026-11-22", "minutes": "60", "issue_url": "https://github.com/o/r/issues/9", "pushed_at": "2026-11-23T10:00:00"}
	if got := MergePushedRows(nil, []Row{row}, "owner", "2026-11-22", "2026-11-22"); len(got) != 1 || got[0].Minutes != 60 || got[0].Date != row["date"] || got[0].ApprovedMinutes != 0 {
		t.Fatal("late push must appear immediately on its logged-for day, without inventing approval", got)
	}
	fetched := WorklogItem{Date: row["date"], Minutes: 60, ApprovedMinutes: 45, URL: row["issue_url"]}
	if got := MergePushedRows([]WorklogItem{fetched}, []Row{row}, "owner", "2026-11-22", "2026-11-22"); len(got) != 1 || got[0].ApprovedMinutes != 45 {
		t.Fatal("GitHub's result must replace the local fallback without double counting", got)
	}
	if got := MergePushedRows(nil, []Row{row}, "owner", "2026-11-23", "2026-11-23"); len(got) != 0 {
		t.Fatal("a late push must not also count on its creation day", got)
	}
}

func TestWorklogDayUsesAValidHeaderOrLegacyFooterDate(t *testing.T) {
	created, _ := time.Parse(time.RFC3339, "2026-11-23T16:30:00Z") // Malaysia: 24 Nov
	for _, tc := range []struct{ body, want string }{
		{"ordinary remarks", "2026-11-24"},
		{"Logged for 2026-11-21\n\n" + worklogAppSection, "2026-11-21"},
		{"Logged for 2026-11-21\n\nordinary remarks", "2026-11-21"},
		{"\r\nLogged for 2026-11-22 \t\r\n\r\nordinary remarks", "2026-11-22"},
		{"ordinary remarks\nLogged for 2026-11-21", "2026-11-24"},
		{"Logged for 2026-02-30\n\nordinary remarks", "2026-11-24"},
		{"Logged for 2026-11-25\n\nordinary remarks", "2026-11-24"},
		{worklogAppSection + "\nLogged for 2026-11-21\n" + entryMarker("id"), "2026-11-21"},
		{worklogAppSection + "\r\nLogged for 2026-11-22 \t\r\n", "2026-11-22"},
		{worklogAppSection + "\nLogged for 2026-02-30", "2026-11-24"},
		{worklogAppSection + "\nLogged for 2026-11-25", "2026-11-24"},
	} {
		if got := worklogDay(tc.body, created); got != tc.want {
			t.Errorf("day for %q = %s; want %s", tc.body, got, tc.want)
		}
	}
}

func TestFetchSOPWorklogsFindsLateIssuesBeyondTheRequestedRange(t *testing.T) {
	old := Gh
	t.Cleanup(func() { Gh = old })
	today := WorkDate(time.Now())
	day := AddDays(today, -2)
	node := func(id, created, body string, reported, approved int) map[string]any {
		return map[string]any{
			"id": id, "url": "https://github.com/o/r/issues/" + id, "createdAt": created + "T03:00:00Z", "body": body,
			"issueFieldValues": map[string]any{"nodes": []any{
				map[string]any{"value": reported, "field": map[string]string{"name": "Worklog Reported(mins)"}},
				map[string]any{"value": approved, "field": map[string]string{"name": "Worklog Approved(mins)"}},
			}},
		}
	}
	regular := node("1", day, "regular", 60, 30)
	late := node("2", today, loggedForBody("late work", day), 120, 90)
	var queries []string
	Gh = func(args []string, _ ...string) (string, error) {
		q, cursor := pushFieldArg(args, "q"), pushFieldArg(args, "after")
		queries = append(queries, q)
		nodes, more := []any{regular}, false
		if strings.Contains(q, `in:body "Logged for"`) {
			if !strings.Contains(q, "created:"+today+".."+today) {
				t.Fatalf("late search must start beyond the padded creation window: %s", q)
			}
			nodes, more = []any{late}, cursor == ""
			if cursor != "" {
				// A repeated result is counted once, and another day's late log
				// does not leak into this day's totals.
				nodes = append(nodes, node("3", today, loggedForBody("other work", AddDays(day, 1)), 240, 180))
			}
		}
		response, _ := json.Marshal(map[string]any{"data": map[string]any{"search": map[string]any{
			"nodes": nodes, "issueCount": 3, "pageInfo": map[string]any{"hasNextPage": more, "endCursor": "next"},
		}}})
		return string(response), nil
	}
	items, err := FetchSOPWorklogs("owner", day, day)
	if err != nil || len(items) != 2 || items[1].Date != day || items[1].Minutes != 120 || items[1].ApprovedMinutes != 90 || items[1].Remarks != "late work" {
		t.Fatalf("late Worklog missing, duplicated or counted on the wrong day: %+v, %v", items, err)
	}
	if len(queries) != 3 || !strings.Contains(queries[0], "created:"+AddDays(day, -1)+".."+AddDays(day, 1)) {
		t.Fatalf("expected the creation window plus paginated late search: %v", queries)
	}
	// The same fetched issue must disappear from the day it was pushed.
	Gh = func(_ []string, _ ...string) (string, error) {
		response, _ := json.Marshal(map[string]any{"data": map[string]any{"search": map[string]any{"nodes": []any{late}}}})
		return string(response), nil
	}
	if got, err := FetchSOPWorklogs("owner", today, today); err != nil || len(got) != 0 {
		t.Fatalf("late Worklog was counted again on its creation day: %+v, %v", got, err)
	}
	// A failed late search must not turn a partial result into an understated total.
	Gh = func(args []string, _ ...string) (string, error) {
		if strings.Contains(pushFieldArg(args, "q"), "in:body") {
			return "", errors.New("late search failed")
		}
		return `{"data":{"search":{"nodes":[]}}}`, nil
	}
	if got, err := FetchSOPWorklogs("owner", day, day); err == nil || got != nil {
		t.Fatalf("late search failure returned partial totals: %+v, %v", got, err)
	}
}

func TestLateWorklogSearchKeepsItsFilterWhenSplittingAtTheResultCap(t *testing.T) {
	old := Gh
	t.Cleanup(func() { Gh = old })
	var queries []string
	Gh = func(args []string, _ ...string) (string, error) {
		q := pushFieldArg(args, "q")
		queries = append(queries, q)
		if !strings.HasSuffix(q, `in:body "Logged for"`) {
			t.Fatalf("split lost its late-worklog filter: %s", q)
		}
		if strings.Contains(q, "created:2026-11-21..2026-11-23") {
			return `{"data":{"search":{"issueCount":1001}}}`, nil
		}
		return `{"data":{"search":{"issueCount":0,"nodes":[]}}}`, nil
	}
	if _, err := searchSOPWorklogs("owner", "2026-11-21", "2026-11-23", true); err != nil || len(queries) != 3 ||
		!strings.Contains(queries[1], "created:2026-11-21..2026-11-22") || !strings.Contains(queries[2], "created:2026-11-23..2026-11-23") {
		t.Fatalf("late creation ranges did not split without a gap: %v, %v", queries, err)
	}
}
