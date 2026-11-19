//go:build live

package tests

import (
	"os"
	"testing"

	"worklog/internal/shared/backend"
)

func TestLiveHistoryDateRangeReadOnly(t *testing.T) {
	dir := os.Getenv("WORKLOG_HISTORY_LIVE_DIR")
	if dir == "" {
		t.Skip("opt-in read-only historical worklog check")
	}
	cfg, ok := backend.NewStore(dir).LoadConfig()
	if !ok {
		t.Fatal("live configuration unavailable")
	}
	a, err := backend.FetchHistoryArchive(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Items) == 0 {
		t.Fatal("no historical records found")
	}
	t.Logf("Read %d records; earliest %s, latest %s; fetched through %s", len(a.Items), a.Items[0].Date, a.Items[len(a.Items)-1].Date, a.Through)
}
