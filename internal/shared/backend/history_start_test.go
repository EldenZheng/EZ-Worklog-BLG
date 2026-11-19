package backend

import (
	"strings"
	"testing"
)

func TestHistoryFetchIncludesFirstOfJuly2025(t *testing.T) {
	cfg, _ := archiveFixture(t)
	previous := Gh
	defer func() { Gh = previous }()
	Gh = func(args []string, _ ...string) (string, error) {
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "items(first:") {
			return `{"data":{"owner":{"projectV2":{"fields":{"nodes":[]}}}}}`, nil
		}
		if !strings.Contains(joined, `"worklog-(date)":>=2025-07-01`) {
			t.Error("backup must include all of July")
		}
		return `{"data":{"owner":{"projectV2":{"items":{"nodes":[{"content":{"url":"first-of-july"},"fieldValues":{"nodes":[{"text":"team-alex","field":{"name":"Worklog Owner (blg-xxxx)"}},{"date":"2025-07-01","field":{"name":"Worklog (Date)"}},{"number":30,"field":{"name":"Worklog (mins)"}}]}}],"pageInfo":{"hasNextPage":false}}}}}}`, nil
	}
	a, err := FetchHistoryArchive(cfg)
	if err != nil || a.From != HistoryStartDate || len(a.Items) != 1 || a.Items[0].Date != HistoryStartDate || a.Through > HistoryCutoff {
		t.Fatalf("July coverage lost: %+v %v", a, err)
	}
}

func TestHistoryDoesNotTreatDatesBeforeJulyAsArchived(t *testing.T) {
	cfg, a := archiveFixture(t)
	a.From = HistoryStartDate
	if err := SaveHistoryArchive(cfg, a); err != nil {
		t.Fatal(err)
	}
	previous := Gh
	defer func() { Gh = previous }()
	pages := 0
	Gh = func(args []string, _ ...string) (string, error) {
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "items(first:") {
			return `{"data":{"owner":{"projectV2":{"fields":{"nodes":[]}}}}}`, nil
		}
		pages++
		if !strings.Contains(joined, `"worklog-(date)":>=2025-06-01`) || !strings.Contains(joined, `"worklog-(date)":<=2025-06-30`) {
			t.Error("only the range before July should be requested live")
		}
		return `{"data":{"owner":{"projectV2":{"items":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`, nil
	}
	items, err := FetchWorklogsWithHistory(cfg, "2025-06-01", "2025-07-31")
	if err != nil || pages != 1 || len(items) != 1 || items[0].Date != "2025-07-21" {
		t.Fatalf("incorrect coverage split: %v %v pages=%d", items, err, pages)
	}
}
