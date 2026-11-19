package backend

import (
	"strings"
	"testing"
)

func TestHistoryRefreshUpdatesBackdatedPushAndRetainsOtherDays(t *testing.T) {
	cfg, a := archiveFixture(t)
	if err := SaveHistoryArchive(cfg, a); err != nil {
		t.Fatal(err)
	}
	old := Gh
	defer func() { Gh = old }()
	Gh = func(args []string, _ ...string) (string, error) {
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "items(first:") {
			return `{"data":{"owner":{"projectV2":{"fields":{"nodes":[]}}}}}`, nil
		}
		if !strings.Contains(joined, `"worklog-(date)":>=2025-07-21`) || !strings.Contains(joined, `"worklog-(date)":<=2025-07-21`) {
			t.Error("refresh must fetch only the pushed day")
		}
		return `{"data":{"owner":{"projectV2":{"items":{"nodes":[{"content":{"url":"https://github.com/archive-tests/repo/issues/1","body":"updated content"},"fieldValues":{"nodes":[{"text":"team-alex","field":{"name":"Worklog Owner (blg-xxxx)"}},{"date":"2025-07-21","field":{"name":"Worklog (Date)"}},{"number":120,"field":{"name":"Worklog (mins)"}}]}}],"pageInfo":{"hasNextPage":false}}}}}}`, nil
	}
	if err := RefreshHistoryDate(cfg, "2025-07-21"); err != nil {
		t.Fatal(err)
	}
	stored, err := LoadHistoryArchive(cfg)
	if err != nil || len(stored.Items) != 2 || stored.Items[0].Minutes != 120 || stored.Items[0].Body != "updated content" || stored.Items[1].ID != "draft" {
		t.Fatalf("backdated push lost or failed to update history: %+v %v", stored, err)
	}
}
