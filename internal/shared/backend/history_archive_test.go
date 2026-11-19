package backend

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func archiveFixture(t *testing.T) (Config, HistoryArchive) {
	t.Helper()
	cfg := Config{WorklogOwner: "team-alex", ArchiveDir: t.TempDir(), ProjectURL: "https://github.com/orgs/archive-tests/projects/1"}
	a := HistoryArchive{Version: 1, Owner: cfg.WorklogOwner, Projects: historyProjects(cfg), Through: HistoryCutoff, FetchedAt: "2026-11-20T12:00:00Z", Items: []WorklogItem{
		{Date: "2025-07-21", Owner: cfg.WorklogOwner, URL: "https://github.com/archive-tests/repo/issues/1", Minutes: 90, Body: strings.Repeat("full issue body\n", 150), Remarks: "- oldest work"},
		{ID: "draft", Date: "2026-11-20", Owner: cfg.WorklogOwner, Minutes: 60, Title: "Draft without an issue URL"},
	}}
	return cfg, a
}

func TestHistoryArchiveRoundTripRefreshAndOwnerIsolation(t *testing.T) {
	cfg, a := archiveFixture(t)
	if err := SaveHistoryArchive(cfg, a); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadHistoryArchive(cfg)
	if err != nil || !reflect.DeepEqual(a, loaded) {
		t.Fatalf("round trip lost records: %+v %v", loaded, err)
	}
	a.Items = nil
	if err := SaveHistoryArchive(cfg, a); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadHistoryArchive(cfg)
	if err != nil || len(loaded.Items) != 2 || loaded.Items[0].Body == "" || loaded.Items[1].ID != "draft" {
		t.Fatalf("refresh discarded preserved history: %+v %v", loaded, err)
	}
	other := cfg
	other.WorklogOwner = "another-owner"
	if _, err := LoadHistoryArchive(other); !os.IsNotExist(err) {
		t.Fatalf("another owner's archive leaked: %v", err)
	}
	other = cfg
	other.ProjectURL = "https://github.com/orgs/archive-tests/projects/2"
	if _, err := LoadHistoryArchive(other); !os.IsNotExist(err) {
		t.Fatalf("another board's archive leaked: %v", err)
	}
	before, _ := os.ReadFile(HistoryArchivePath(cfg))
	a.Items = []WorklogItem{{Owner: cfg.WorklogOwner, Date: "2026-11-21"}}
	if err := SaveHistoryArchive(cfg, a); err == nil {
		t.Fatal("post-cutoff record accepted")
	}
	after, _ := os.ReadFile(HistoryArchivePath(cfg))
	if !bytes.Equal(before, after) {
		t.Fatal("failed save changed the existing archive")
	}
}

func TestHistoryArchiveMoveAndClearOnlyOwnedFile(t *testing.T) {
	cfg, a := archiveFixture(t)
	if err := SaveHistoryArchive(cfg, a); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "worklog.csv", "keep.txt"} {
		if err := os.WriteFile(filepath.Join(cfg.ArchiveDir, name), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	next, err := CopyHistoryArchive(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHistoryArchive(next); err != nil {
		t.Fatal("new copy is unreadable", err)
	}
	if _, err := LoadHistoryArchive(cfg); err != nil {
		t.Fatal("old copy removed before config could be saved", err)
	}
	if _, err := CopyHistoryArchive(cfg, HistoryDirectory(next)); err == nil {
		t.Fatal("move overwrote an existing destination")
	}
	if err := ClearHistoryArchive(cfg); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "worklog.csv", "keep.txt"} {
		if data, err := os.ReadFile(filepath.Join(cfg.ArchiveDir, name)); err != nil || string(data) != "keep" {
			t.Fatal("clearing touched unrelated data", name, err)
		}
	}
	if _, err := LoadHistoryArchive(next); err != nil {
		t.Fatal("clearing old location affected the new one", err)
	}
}

func TestHistoryArchiveCorruptionIsReportedAndKept(t *testing.T) {
	cfg, a := archiveFixture(t)
	if err := SaveHistoryArchive(cfg, a); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(HistoryArchivePath(cfg))
	raw[len(raw)-5] ^= 0xff
	if err := os.WriteFile(HistoryArchivePath(cfg), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHistoryArchive(cfg); err == nil {
		t.Fatal("corrupt archive was accepted")
	}
	if err := SaveHistoryArchive(cfg, a); err == nil {
		t.Fatal("corrupt archive silently overwritten")
	}
	after, _ := os.ReadFile(HistoryArchivePath(cfg))
	if !bytes.Equal(raw, after) {
		t.Fatal("failed refresh changed corrupt backup")
	}
}

func TestHistoryArchiveReadsOfflineAndFetchesOnlyAfterCutoff(t *testing.T) {
	cfg, a := archiveFixture(t)
	a.Through = "2025-07-22"
	a.Items[1].Date = "2025-07-22"
	if err := SaveHistoryArchive(cfg, a); err != nil {
		t.Fatal(err)
	}
	oldGh := Gh
	defer func() { Gh = oldGh }()
	calls := 0
	Gh = func(args []string, _ ...string) (string, error) {
		calls++
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "items(first:") {
			return `{"data":{"owner":{"projectV2":{"fields":{"nodes":[{"name":"Worklog Owner (blg-xxxx)"},{"name":"Worklog (Date)"}]}}}}}`, nil
		}
		if !strings.Contains(joined, `"worklog-(date)":>=2025-07-23`) {
			t.Fatalf("historical dates requested again: %v", args)
		}
		return `{"data":{"owner":{"projectV2":{"items":{"nodes":[{"content":{"url":"new"},"fieldValues":{"nodes":[{"text":"team-alex","field":{"name":"Worklog Owner (blg-xxxx)"}},{"date":"2025-07-23","field":{"name":"Worklog (Date)"}},{"number":30,"field":{"name":"Worklog (mins)"}}]}}],"pageInfo":{"hasNextPage":false}}}}}}`, nil
	}
	items, err := FetchWorklogsWithHistory(cfg, "2025-07-01", "2025-07-21")
	if err != nil || len(items) != 1 || calls != 0 {
		t.Fatalf("offline history failed: %v %v calls=%d", items, err, calls)
	}
	items, err = FetchWorklogsWithHistory(cfg, "2025-07-22", "2025-07-24")
	if err != nil || len(items) != 2 || calls != 2 {
		t.Fatalf("cutoff merge failed: %v %v calls=%d", items, err, calls)
	}
}

func TestHistoryFetchRejectsPartialBoardResults(t *testing.T) {
	cfg, _ := archiveFixture(t)
	cfg.ExtraProjectURLs = []string{"https://github.com/orgs/failing-archive-board/projects/2"}
	oldGh := Gh
	defer func() { Gh = oldGh }()
	want := errors.New("board offline")
	Gh = func(args []string, _ ...string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "failing-archive-board") {
			return "", want
		}
		if !strings.Contains(joined, "items(first:") {
			return `{"data":{"owner":{"projectV2":{"fields":{"nodes":[]}}}}}`, nil
		}
		if !strings.Contains(joined, `"worklog-(date)":>=2025-07-01`) {
			t.Fatal("history must be fetched from the beginning of July 2025")
		}
		return `{"data":{"owner":{"projectV2":{"items":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`, nil
	}
	if _, err := FetchHistoryArchive(cfg); !errors.Is(err, want) {
		t.Fatalf("partial archive accepted: %v", err)
	}
	if _, err := os.Stat(HistoryArchivePath(cfg)); !os.IsNotExist(err) {
		t.Fatal("failed fetch wrote an archive")
	}
}

func TestStorageUsageMeasuresAppDataArchiveAndFreeSpace(t *testing.T) {
	cfg, a := archiveFixture(t)
	if err := SaveHistoryArchive(cfg, a); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "Worklog.exe")
	data := filepath.Join(dir, "data")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("app"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "worklog.csv"), []byte("records"), 0o600); err != nil {
		t.Fatal(err)
	}
	usage, err := StorageForApp(cfg, exe, data)
	info, _ := os.Stat(HistoryArchivePath(cfg))
	if err != nil || usage.AppBytes != 3 || usage.DataBytes != 7 || usage.ArchiveBytes != info.Size() || usage.TotalBytes == 0 || usage.FreeBytes > usage.TotalBytes {
		t.Fatalf("bad usage: %+v %v", usage, err)
	}
	if got := FormatBytes(8 * 1024 * 1024 * 1024); got != "8.0 GiB" {
		t.Fatal(got)
	}
	if free, _, err := DiskSpace(filepath.Join(dir, "not-created", "yet")); err != nil || free == 0 {
		t.Fatal(fmt.Sprint(free, err))
	}
}
